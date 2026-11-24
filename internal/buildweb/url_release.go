package buildweb

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kube-env/internal/buildapp"
)

type urlRequest struct {
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Auth     string `json:"auth"`
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type remoteRelease struct {
	GitOrigin *buildapp.GitOrigin `json:"git_origin,omitempty"`
	Token     string              `json:"token"`
	Release   Release             `json:"release"`
	Origin    buildapp.URLOrigin  `json:"origin"`
}

func (s *Server) downloadRelease(ctx context.Context, input urlRequest) ([]byte, buildapp.URLOrigin, error) {
	return s.downloadReleaseWithCA(ctx, input, s.opts.ReleaseCAFile)
}

func (s *Server) downloadReleaseWithCA(ctx context.Context, input urlRequest, caFile string) ([]byte, buildapp.URLOrigin, error) {
	if err := buildapp.ValidateDocumentURL(input.URL); err != nil {
		return nil, buildapp.URLOrigin{}, err
	}
	if input.SHA256 != "" {
		raw, err := hex.DecodeString(input.SHA256)
		if err != nil || len(raw) != sha256.Size {
			return nil, buildapp.URLOrigin{}, fmt.Errorf("expected SHA256 must contain 64 hexadecimal characters")
		}
	}
	switch input.Auth {
	case "", "none":
		if input.Token != "" || input.Username != "" || input.Password != "" {
			return nil, buildapp.URLOrigin{}, fmt.Errorf("choose authentication explicitly")
		}
	case "bearer":
		if input.Token == "" || strings.ContainsAny(input.Token, " \t\r\n") || input.Username != "" || input.Password != "" {
			return nil, buildapp.URLOrigin{}, fmt.Errorf("provide one valid bearer token")
		}
	case "basic":
		if input.Username == "" || strings.ContainsAny(input.Username, ":\r\n") || input.Token != "" {
			return nil, buildapp.URLOrigin{}, fmt.Errorf("provide a username and password without bearer credentials")
		}
	default:
		return nil, buildapp.URLOrigin{}, fmt.Errorf("unknown URL authentication")
	}
	client, err := artifactHTTPClient(caFile)
	if err != nil {
		return nil, buildapp.URLOrigin{}, err
	}
	defer client.CloseIdleConnections()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, input.URL, nil)
	if err != nil {
		return nil, buildapp.URLOrigin{}, fmt.Errorf("invalid manifest URL")
	}
	if input.Auth == "bearer" {
		r.Header.Set("Authorization", "Bearer "+input.Token)
	}
	if input.Auth == "basic" {
		r.SetBasicAuth(input.Username, input.Password)
	}
	response, err := client.Do(r)
	if err != nil {
		return nil, buildapp.URLOrigin{}, fmt.Errorf("release download failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, buildapp.URLOrigin{}, fmt.Errorf("release download returned HTTP %d", response.StatusCode)
	}
	if strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/html") {
		return nil, buildapp.URLOrigin{}, fmt.Errorf("URL returned an HTML page; use the direct manifest download link")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil || len(raw) == 0 || len(raw) > 8<<20 {
		return nil, buildapp.URLOrigin{}, fmt.Errorf("release must be nonempty and at most 8 MiB")
	}
	origin := buildapp.URLOrigin{URL: input.URL, SHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), ExpectedSHA256: strings.ToLower(input.SHA256)}
	if err := origin.Verify(raw); err != nil {
		return nil, buildapp.URLOrigin{}, err
	}
	return raw, origin, nil
}

func (s *Server) readURLRelease(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input urlRequest
	if err := decoder.Decode(&input); err != nil {
		failure(w, 400, fmt.Errorf("invalid URL request"))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		failure(w, 400, fmt.Errorf("expected one URL request"))
		return
	}
	raw, origin, err := s.downloadRelease(r.Context(), input)
	input.Token, input.Password = "", ""
	if err != nil {
		failure(w, 400, err)
		return
	}
	s.storeURLRelease(w, raw, origin)
}

func (s *Server) storeURLRelease(w http.ResponseWriter, raw []byte, origin buildapp.URLOrigin) {
	s.storeRelease(w, raw, remoteRelease{Origin: origin})
}

func (s *Server) storeRelease(w http.ResponseWriter, raw []byte, result remoteRelease) {
	id, count, err := buildapp.ReleaseMetadata(raw)
	if err != nil || id == "" {
		failure(w, 400, fmt.Errorf("source did not return a valid ImageRelease"))
		return
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		failure(w, 500, err)
		return
	}
	result.Token = hex.EncodeToString(token[:])
	result.Release = Release{ID: id, Images: count, SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(raw))}
	directory := filepath.Join(s.opts.WorkDir, ".url-releases", result.Token)
	if err := os.MkdirAll(filepath.Dir(directory), 0700); err != nil {
		failure(w, 500, err)
		return
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		failure(w, 500, err)
		return
	}
	stored := false
	defer func() {
		if !stored {
			_ = os.RemoveAll(directory)
		}
	}()
	if err := os.WriteFile(filepath.Join(directory, "release.yaml"), raw, 0600); err != nil {
		failure(w, 500, err)
		return
	}
	metadata, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(directory, "origin.json"), metadata, 0600); err != nil {
		failure(w, 500, err)
		return
	}
	stored = true
	respond(w, 200, struct {
		remoteRelease
		Content string `json:"content"`
	}{result, string(raw)})
}

func (s *Server) remoteRelease(token string) (remoteRelease, []byte, error) {
	if !validID(token) {
		return remoteRelease{}, nil, fmt.Errorf("choose a downloaded release")
	}
	root, err := os.OpenRoot(s.opts.WorkDir)
	if err != nil {
		return remoteRelease{}, nil, err
	}
	defer root.Close()
	directory := ".url-releases/" + token
	metadata, err := readBounded(root, directory+"/origin.json", 16<<10)
	if err != nil {
		return remoteRelease{}, nil, fmt.Errorf("downloaded release is unavailable; read the URL again")
	}
	var result remoteRelease
	if json.Unmarshal(metadata, &result) != nil || result.Token != token {
		return remoteRelease{}, nil, fmt.Errorf("invalid downloaded release record")
	}
	raw, err := readBounded(root, directory+"/release.yaml", 8<<20)
	if err != nil {
		return remoteRelease{}, nil, err
	}
	checksum := result.Origin.SHA256
	if result.GitOrigin != nil {
		if result.Origin != (buildapp.URLOrigin{}) {
			return remoteRelease{}, nil, fmt.Errorf("ambiguous captured source")
		}
		if err := result.GitOrigin.Verify(raw); err != nil {
			return remoteRelease{}, nil, err
		}
		checksum = result.GitOrigin.SHA256
	} else if err := result.Origin.Verify(raw); err != nil {
		return remoteRelease{}, nil, err
	}
	id, count, err := buildapp.ReleaseMetadata(raw)
	if err != nil || id != result.Release.ID || count != result.Release.Images || result.Release.SHA256 != "sha256:"+checksum {
		return remoteRelease{}, nil, fmt.Errorf("downloaded release changed; read the URL again")
	}
	return result, raw, nil
}

func artifactHTTPClient(caFile string) (*http.Client, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unsupported HTTP transport")
	}
	transport := base.Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		cert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read HTTP CA file")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(cert) {
			return nil, fmt.Errorf("HTTP CA file has no certificates")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, nil
}
