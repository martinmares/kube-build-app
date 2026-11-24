package buildweb

import (
	"bytes"
	"context"
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
	Remote     *GitRemote `yaml:"remote,omitempty"`
	Storage    string     `yaml:"storage"`
	Repository string     `yaml:"repository"`
	Tag        string     `yaml:"tag"`
	BaseURL    string     `yaml:"base_url"`
	Path       string     `yaml:"path"`
	SHA256     string     `yaml:"sha256"`
	TokenFile  string     `yaml:"token_file"`
	CAFile     string     `yaml:"ca_file"`
}

type ServeConfig struct {
	Source        *ArtifactSource `yaml:"artifact_source"`
	PackageOutput *PackageOutput  `yaml:"package_output"`
}

func LoadServeConfig(filename string) (*ServeConfig, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot open builder server configuration")
	}
	defer file.Close()
	var config ServeConfig
	raw, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return nil, fmt.Errorf("builder server configuration exceeds 64 KiB or cannot be read")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if decoder.Decode(&config) != nil || (config.Source == nil && config.PackageOutput == nil) {
		return nil, fmt.Errorf("expected a server YAML with artifact_source or package_output")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("expected one configuration document")
	}
	base, err := filepath.Abs(filepath.Dir(filename))
	if err != nil {
		return nil, err
	}
	if config.Source != nil {
		for _, value := range []*string{&config.Source.TokenFile, &config.Source.CAFile, &config.Source.Repository} {
			if *value != "" && !filepath.IsAbs(*value) {
				*value = filepath.Join(base, *value)
			}
		}
		if config.Source.Remote != nil {
			for _, value := range []*string{&config.Source.Remote.PasswordFile, &config.Source.Remote.CAFile} {
				if *value != "" && !filepath.IsAbs(*value) {
					*value = filepath.Join(base, *value)
				}
			}
		}
	}
	if config.PackageOutput != nil {
		for _, value := range []*string{&config.PackageOutput.Directory, &config.PackageOutput.TokenFile, &config.PackageOutput.CAFile} {
			if *value != "" && !filepath.IsAbs(*value) {
				*value = filepath.Join(base, *value)
			}
		}
	}
	// Validate after CLI overrides have been applied.
	return &config, nil
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
	switch s.Storage {
	case "artifactory":
		if s.Repository != "" || s.Tag != "" || s.Remote != nil {
			return fmt.Errorf("artifactory does not use repository or tag")
		}
		if err := buildapp.ValidateDocumentURL(s.BaseURL); err != nil {
			return fmt.Errorf("source base_url must be direct HTTPS without credentials, query or fragment")
		}
	case "git":
		if (s.Repository == "") == (s.Remote == nil) || s.Tag == "" || s.BaseURL != "" || s.TokenFile != "" || s.CAFile != "" {
			return fmt.Errorf("git requires either repository or remote, plus tag, without top-level HTTPS settings")
		}
		if s.Remote != nil {
			if err := s.Remote.Validate(); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("configured source storage must be git or artifactory")
	}
	values := map[string]string{}
	for _, name := range sourceVariables {
		values[name] = "example"
	}
	if _, err := expandSourcePath(s.Path, values); err != nil {
		return err
	}
	if s.Storage == "git" {
		if _, err := expandSourcePath(s.Tag, values); err != nil {
			return fmt.Errorf("invalid source tag template")
		}
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
		if name != "release_id" && name != "environment" && strings.Contains(source.Path+source.Tag, "{"+name+"}") {
			variables = append(variables, name)
		}
	}
	return struct {
		Storage          string         `json:"storage"`
		Variables        []string       `json:"variables"`
		FixedSHA256      bool           `json:"fixed_sha256"`
		ChecksumRequired bool           `json:"checksum_required"`
		Managed          bool           `json:"managed"`
		Refresh          *RefreshStatus `json:"refresh,omitempty"`
	}{source.Storage, variables, source.SHA256 != "", source.Storage == "artifactory", source.Remote != nil, s.sourceRefreshStatus()}
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
	raw, origin, err := s.captureConfiguredRelease(r.Context(), input)
	if err != nil {
		failure(w, 400, err)
		return
	}
	s.storeRelease(w, raw, origin)
}

// CaptureConfiguredRelease is the one-shot CLI form of the configured web source.
func CaptureConfiguredRelease(ctx context.Context, source ArtifactSource, workDir, releaseID, environment, revision, distribution, checksum string) ([]byte, *buildapp.GitOrigin, *buildapp.URLOrigin, error) {
	if err := source.Validate(); err != nil {
		return nil, nil, nil, err
	}
	if source.Remote != nil {
		remote := *source.Remote
		remote.cache = filepath.Join(workDir, ".git-readers", remote.identity())
		source.Remote = &remote
	}
	s := &Server{opts: Options{ArtifactSource: &source, WorkDir: workDir}}
	raw, origin, err := s.captureConfiguredRelease(ctx, sourceReadRequest{ReleaseID: releaseID, Environment: environment, Revision: revision, Distribution: distribution, SHA256: checksum})
	if err != nil {
		return nil, nil, nil, err
	}
	if origin.GitOrigin != nil {
		return raw, origin.GitOrigin, nil, nil
	}
	return raw, nil, &origin.Origin, nil
}

func (s *Server) captureConfiguredRelease(ctx context.Context, input sourceReadRequest) ([]byte, remoteRelease, error) {
	if !sourceSegment.MatchString(input.ReleaseID) {
		return nil, remoteRelease{}, fmt.Errorf("provide a valid release ID")
	}
	values := map[string]string{"release_id": input.ReleaseID, "environment": input.Environment, "revision": input.Revision, "distribution": input.Distribution}
	source := s.opts.ArtifactSource
	document, err := expandSourcePath(source.Path, values)
	if err != nil {
		return nil, remoteRelease{}, err
	}
	checksum := source.SHA256
	if checksum == "" {
		checksum = input.SHA256
	} else if input.SHA256 != "" && !strings.EqualFold(input.SHA256, checksum) {
		return nil, remoteRelease{}, fmt.Errorf("configured checksum cannot be overridden")
	}
	var raw []byte
	var result remoteRelease
	if source.Storage == "git" {
		tag, err := expandSourcePath(source.Tag, values)
		if err != nil {
			return nil, remoteRelease{}, err
		}
		var origin buildapp.GitOrigin
		if source.Remote != nil {
			raw, origin, err = s.readRemoteGit(ctx, tag, document, checksum)
		} else {
			raw, origin, err = readGitRelease(ctx, source.Repository, tag, document, checksum)
		}
		if err != nil {
			return nil, remoteRelease{}, err
		}
		result.GitOrigin = &origin
	} else {
		if checksum == "" {
			return nil, remoteRelease{}, fmt.Errorf("expected SHA256 is required for the configured source")
		}
		base, _ := url.Parse(source.BaseURL)
		base.Path = strings.TrimRight(base.Path, "/") + "/" + document
		base.RawPath = ""
		request := urlRequest{URL: base.String(), SHA256: checksum, Auth: "none"}
		if source.TokenFile != "" {
			token, err := readArtifactToken(source.TokenFile)
			if err != nil {
				return nil, remoteRelease{}, err
			}
			request.Auth, request.Token = "bearer", token
		}
		raw, result.Origin, err = s.downloadReleaseWithCA(ctx, request, source.CAFile)
		if err != nil {
			return nil, remoteRelease{}, err
		}
	}
	id, _, err := buildapp.ReleaseMetadata(raw)
	if err != nil || id != input.ReleaseID {
		return nil, remoteRelease{}, fmt.Errorf("captured ImageRelease does not match the requested release ID")
	}
	return raw, result, nil
}

func readArtifactToken(filename string) (string, error) {
	if filename == "" {
		return "", nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("cannot read configured token file")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (16<<10)+1))
	token := strings.TrimSpace(string(raw))
	if err != nil || len(raw) > 16<<10 || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", fmt.Errorf("cannot read a valid configured bearer token")
	}
	return token, nil
}
