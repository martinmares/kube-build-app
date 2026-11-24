package buildweb

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"kube-env/internal/appinfo"
)

func postConfigured(s *Server, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/releases/configured", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestConfiguredSourceSnapshotAndCredentialIsolation(t *testing.T) {
	s, opts := fixture(t)
	raw := []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, "releases/SWD-42.yaml")))
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	var reads atomic.Int32
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		if r.URL.Path != "/artifactory/releases/dev/SWD-42/release.yaml" || r.Header.Get("Authorization") != "Bearer synthetic-configured-secret" {
			t.Errorf("unexpected configured download: %s", r.URL.Path)
		}
		_, _ = w.Write(raw)
	}))
	defer remote.Close()
	token := filepath.Join(t.TempDir(), "private-token-location")
	if err := os.WriteFile(token, []byte("synthetic-configured-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opts.ArtifactSource = &ArtifactSource{Storage: "artifactory", BaseURL: remote.URL + "/artifactory", Path: "releases/{environment}/{release_id}/release.yaml", TokenFile: token, CAFile: releaseCA(t, remote)}
	var err error
	s, err = New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	// Server startup and catalog inspection never contact the upstream server.
	catalog := httptest.NewRecorder()
	s.Handler().ServeHTTP(catalog, httptest.NewRequest("GET", "/api/catalog", nil))
	if catalog.Code != 200 || !strings.Contains(catalog.Body.String(), `"fixed_sha256":false`) || reads.Load() != 0 {
		t.Fatal(catalog.Body.String())
	}
	for _, secret := range []string{token, "synthetic-configured-secret", opts.ArtifactSource.CAFile} {
		if strings.Contains(catalog.Body.String(), secret) {
			t.Fatal("private config leaked to catalog")
		}
	}
	body := fmt.Sprintf(`{"release_id":"SWD-42","environment":"dev","sha256":%q}`, hash)
	w := postConfigured(s, body)
	if w.Code != 200 {
		t.Fatalf("read: %d %s", w.Code, w.Body.String())
	}
	var staged remoteRelease
	if err := json.Unmarshal(w.Body.Bytes(), &staged); err != nil {
		t.Fatal(err)
	}
	if staged.Origin.ExpectedSHA256 != hash || staged.Origin.Verify(raw) != nil {
		t.Fatal("wrong origin")
	}
	remote.Close()
	// A restart and a missing credential file must not break an already reviewed snapshot.
	if err := os.Remove(token); err != nil {
		t.Fatal(err)
	}
	s, err = New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	w = postBuild(t, s, selection{Environment: "dev", RemoteRelease: staged.Token, ManifestSHA256: staged.Release.SHA256})
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Body.String())
	}
	var run Run
	if err := json.Unmarshal(w.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	run = waitBuild(t, s, run.ID)
	if run.Status != "succeeded" || run.ReleaseSource == nil || run.ReleaseSource.Verify(raw) != nil || reads.Load() != 1 {
		t.Fatalf("build: %+v", run)
	}
	if err := filepath.WalkDir(opts.WorkDir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			content, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			if bytes.Contains(content, []byte("synthetic-configured-secret")) || bytes.Contains(content, []byte(token)) {
				t.Errorf("credentials persisted: %s", name)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredSourceRejectsOverridesAndWrongRelease(t *testing.T) {
	s, opts := fixture(t)
	raw := []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, "releases/SWD-42.yaml")))
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	var reads atomic.Int32
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reads.Add(1); _, _ = w.Write(raw) }))
	defer remote.Close()
	s.opts.ArtifactSource = &ArtifactSource{Storage: "artifactory", BaseURL: remote.URL, Path: "releases/{release_id}.yaml", SHA256: hash, CAFile: releaseCA(t, remote)}
	for _, body := range []string{
		`{"release_id":"../secret"}`,
		`{"release_id":"SWD-42","url":"https://other.example.invalid/"}`,
		`{"release_id":"SWD-42","token_file":"/private/file"}`,
		`{"release_id":"SWD-42","sha256":"` + strings.Repeat("0", 64) + `"}`,
		`{"release_id":"SWD-42"} {}`,
	} {
		w := postConfigured(s, body)
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	if reads.Load() != 0 {
		t.Fatal("invalid request contacted upstream")
	}
	w := postConfigured(s, `{"release_id":"OTHER"}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "does not match") {
		t.Fatal(w.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(opts.WorkDir, ".url-releases"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("wrong release was staged")
	}
	w = postConfigured(s, `{"release_id":"SWD-42"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	// Configured credentials are never reused by the one-off URL mode.
	w = postURL(s, urlRequest{URL: remote.URL + "/release.yaml"})
	if w.Code == 200 {
		t.Fatal("one-off URL unexpectedly trusted configured source CA")
	}
	request := httptest.NewRequest("POST", "/api/releases/configured", strings.NewReader(`{"release_id":"SWD-42"}`))
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, request)
	if w.Code != 403 {
		t.Fatalf("cross-site: %d", w.Code)
	}
}

func TestConfiguredSourceRequiresChecksumAndTemplateValues(t *testing.T) {
	s, _ := fixture(t)
	s.opts.ArtifactSource = &ArtifactSource{Storage: "artifactory", BaseURL: "https://artifacts.example.invalid", Path: "{release_id}/{distribution}/{revision}.yaml"}
	for _, body := range []string{
		`{"release_id":"SWD-42"}`,
		`{"release_id":"SWD-42","distribution":"registry-a","revision":"1"}`,
		`{"release_id":"SWD-42","distribution":"../registry","revision":"1","sha256":"` + strings.Repeat("a", 64) + `"}`,
	} {
		if w := postConfigured(s, body); w.Code != 400 {
			t.Fatal(w.Body.String())
		}
	}
}

func TestArtifactSourceConfigValidation(t *testing.T) {
	base := t.TempDir()
	filename := filepath.Join(base, "source.yaml")
	if err := os.WriteFile(filename, []byte("artifact_source:\n  storage: artifactory\n  base_url: https://artifacts.example.invalid/manifests\n  path: release/{release_id}.yaml\n  token_file: private/token\n  ca_file: ca.pem\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadServeConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	source := config.Source
	if err != nil || source.Validate() != nil {
		t.Fatalf("config: %+v %v", source, err)
	}
	if source.TokenFile != filepath.Join(base, "private/token") || source.CAFile != filepath.Join(base, "ca.pem") {
		t.Fatal("relative config paths")
	}
	for _, content := range []string{"artifact_source:\n  token: do-not-print-this-value\n", "artifact_source: {}\n---\nartifact_source: {}\n", "artifact_source: {}\nartifact_source: {}\n"} {
		if err := os.WriteFile(filename, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadServeConfig(filename)
		if err == nil || strings.Contains(err.Error(), "do-not-print-this-value") {
			t.Fatalf("invalid config: %v", err)
		}
	}
	for _, pattern := range []string{"../x.yaml", "x/../y.yaml", "/x.yaml", "{unknown}.yaml", "{release_id}/{oops}.yaml", "x\\y.yaml", "{{release_id}}", "."} {
		copy := *source
		copy.Path = pattern
		if copy.Validate() == nil {
			t.Errorf("accepted path: %s", pattern)
		}
	}
	gitConfig := "artifact_source:\n  storage: git\n  repository: ./repo\n  tag: release/{release_id}\n  path: releases/{release_id}.yaml\n"
	if err := os.WriteFile(filename, []byte(gitConfig), 0600); err != nil {
		t.Fatal(err)
	}
	config, err = LoadServeConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	gitSource := config.Source
	if err != nil || gitSource.Repository != filepath.Join(base, "repo") || gitSource.Validate() != nil {
		t.Fatalf("Git source YAML: %+v %v", gitSource, err)
	}
	copy := *source
	copy.Storage = "git"
	if copy.Validate() == nil {
		t.Fatal("incomplete Git source accepted")
	}
}
