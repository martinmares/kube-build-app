package buildweb

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
	"kube-env/internal/buildapp"
)

// ArtifactSource is server configuration. Credential paths never go to the browser.
type ArtifactSource struct {
	Storage   string `yaml:"storage"`
	BaseURL   string `yaml:"base_url"`
	Path      string `yaml:"path"`
	SHA256    string `yaml:"sha256"`
	TokenFile string `yaml:"token_file"`
	CAFile    string `yaml:"ca_file"`
}

func LoadArtifactSource(filename string) (*ArtifactSource, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open release source configuration")
	}
	defer file.Close()
	var config struct {
		Source *ArtifactSource `yaml:"artifact_source"`
	}
	raw, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return nil, fmt.Errorf("release source configuration exceeds 64 KiB or cannot be read")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if decoder.Decode(&config) != nil || config.Source == nil {
		return nil, fmt.Errorf("expected a release source YAML with one artifact_source section")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("expected one configuration document")
	}
	base, err := filepath.Abs(filepath.Dir(filename))
	if err != nil {
		return nil, err
	}
	for _, value := range []*string{&config.Source.TokenFile, &config.Source.CAFile} {
		if *value != "" && !filepath.IsAbs(*value) {
			*value = filepath.Join(base, *value)
		}
	}
	// Validate after CLI overrides have been applied.
	return config.Source, nil
}

var sourceVariable = regexp.MustCompile(`\{([^{}]+)\}`)
var sourceSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var sourceVariables = []string{"release_id", "environment", "revision", "distribution"}

func expandSourcePath(pattern string, values map[string]string) (string, error) {
	var invalid bool
	result := sourceVariable.ReplaceAllStringFunc(pattern, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
		value, known := values[name]
		if !known || !sourceSegment.MatchString(value) {
			invalid = true
		}
		return value
	})
	if invalid || strings.ContainsAny(result, "{}\\\x00\r\n") || result == "" || result == "." || result == ".." || strings.HasPrefix(result, "/") || strings.HasPrefix(result, "../") || path.Clean(result) != result {
		return "", fmt.Errorf("source path needs a clean relative path and valid single-segment template values")
	}
	return result, nil
}

func (s ArtifactSource) Validate() error {
	if s.Storage != "artifactory" {
		return fmt.Errorf("configured release source currently supports storage: artifactory only")
	}
	if err := buildapp.ValidateDocumentURL(s.BaseURL); err != nil {
		return fmt.Errorf("source base_url must be direct HTTPS without credentials, query or fragment")
	}
	values := map[string]string{}
	for _, name := range sourceVariables {
		values[name] = "example"
	}
	if _, err := expandSourcePath(s.Path, values); err != nil {
		return err
	}
	if s.SHA256 != "" {
		raw, err := hex.DecodeString(s.SHA256)
		if err != nil || len(raw) != 32 {
			return fmt.Errorf("source sha256 must contain 64 hexadecimal characters")
		}
	}
	return nil
}

type sourceReadRequest struct {
	ReleaseID    string `json:"release_id"`
	Environment  string `json:"environment"`
	Revision     string `json:"revision"`
	Distribution string `json:"distribution"`
	SHA256       string `json:"sha256"`
}

func (s *Server) configuredSourceView() any {
	if s.opts.ArtifactSource == nil {
		return nil
	}
	source := s.opts.ArtifactSource
	variables := []string{}
	for _, name := range sourceVariables {
		if name != "release_id" && name != "environment" && strings.Contains(source.Path, "{"+name+"}") {
			variables = append(variables, name)
		}
	}
	return struct {
		Variables   []string `json:"variables"`
		FixedSHA256 bool     `json:"fixed_sha256"`
	}{variables, source.SHA256 != ""}
}

func (s *Server) readConfiguredRelease(w http.ResponseWriter, r *http.Request) {
	if s.opts.ArtifactSource == nil {
		failure(w, 400, fmt.Errorf("no release source configured"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var input sourceReadRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		failure(w, 400, fmt.Errorf("invalid configured source request"))
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		failure(w, 400, fmt.Errorf("expected one configured source request"))
		return
	}
	if !sourceSegment.MatchString(input.ReleaseID) {
		failure(w, 400, fmt.Errorf("provide a valid release ID"))
		return
	}
	values := map[string]string{"release_id": input.ReleaseID, "environment": input.Environment, "revision": input.Revision, "distribution": input.Distribution}
	source := s.opts.ArtifactSource
	document, err := expandSourcePath(source.Path, values)
	if err != nil {
		failure(w, 400, err)
		return
	}
	checksum := source.SHA256
	if checksum == "" {
		checksum = input.SHA256
	} else if input.SHA256 != "" && !strings.EqualFold(input.SHA256, checksum) {
		failure(w, 400, fmt.Errorf("configured checksum cannot be overridden"))
		return
	}
	if checksum == "" {
		failure(w, 400, fmt.Errorf("expected SHA256 is required for the configured source"))
		return
	}
	base, _ := url.Parse(source.BaseURL) // Validated when the server starts.
	base.Path = strings.TrimRight(base.Path, "/") + "/" + document
	base.RawPath = ""
	request := urlRequest{URL: base.String(), SHA256: checksum, Auth: "none"}
	if source.TokenFile != "" {
		file, err := os.Open(source.TokenFile)
		if err != nil {
			failure(w, 400, fmt.Errorf("cannot read configured token file"))
			return
		}
		raw, readErr := io.ReadAll(io.LimitReader(file, (16<<10)+1))
		_ = file.Close()
		if readErr != nil || len(raw) > 16<<10 {
			failure(w, 400, fmt.Errorf("cannot read configured token file"))
			return
		}
		request.Auth, request.Token = "bearer", strings.TrimSpace(string(raw))
	}
	raw, origin, err := s.downloadReleaseWithCA(r.Context(), request, source.CAFile)
	request.Token = ""
	if err != nil {
		failure(w, 400, err)
		return
	}
	id, _, err := buildapp.ReleaseMetadata(raw)
	if err != nil || id != input.ReleaseID {
		failure(w, 400, fmt.Errorf("downloaded ImageRelease does not match the requested release ID"))
		return
	}
	s.storeURLRelease(w, raw, origin)
}
