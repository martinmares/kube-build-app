package buildweb

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kube-env/internal/appinfo"
	"kube-env/internal/buildapp"
)

func postURL(s *Server, input urlRequest) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(input)
	r := httptest.NewRequest(http.MethodPost, "/api/releases/url", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func releaseCA(t *testing.T, server *httptest.Server) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(filename, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestURLReleaseAuthenticationAndCredentialIsolation(t *testing.T) {
	for _, auth := range []string{"none", "bearer", "basic"} {
		t.Run(auth, func(t *testing.T) {
			s, opts := fixture(t)
			raw := []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, "releases/SWD-42.yaml")))
			remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch auth {
				case "none":
					if r.Header.Get("Authorization") != "" {
						t.Error("unexpected credentials")
					}
				case "bearer":
					if r.Header.Get("Authorization") != "Bearer synthetic-url-token" {
						t.Error("wrong token")
					}
				case "basic":
					u, p, ok := r.BasicAuth()
					if !ok || u != "reader" || p != " synthetic-url-password " {
						t.Error("wrong Basic credentials")
					}
				}
				w.Header().Set("Content-Type", "application/yaml")
				_, _ = w.Write(raw)
			}))
			defer remote.Close()
			s.opts.ReleaseCAFile = releaseCA(t, remote)
			input := urlRequest{URL: remote.URL + "/manifest.yaml", Auth: auth, SHA256: fmt.Sprintf("%x", sha256.Sum256(raw))}
			if auth == "bearer" {
				input.Token = "synthetic-url-token"
			}
			if auth == "basic" {
				input.Username = "reader"
				input.Password = " synthetic-url-password "
			}
			response := postURL(s, input)
			if response.Code != 200 {
				t.Fatalf("download: %d %s", response.Code, response.Body.String())
			}
			var staged remoteRelease
			if err := json.Unmarshal(response.Body.Bytes(), &staged); err != nil {
				t.Fatal(err)
			}
			if staged.Release.ID != "SWD-42" || staged.Origin.Verify(raw) != nil || staged.Origin.ExpectedSHA256 == "" {
				t.Fatalf("metadata: %+v", staged)
			}
			if strings.Contains(response.Body.String(), "synthetic-url-token") || strings.Contains(response.Body.String(), "synthetic-url-password") {
				t.Fatal("credentials in response")
			}
			if err := filepath.WalkDir(opts.WorkDir, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.Type().IsRegular() {
					content, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					if bytes.Contains(content, []byte("synthetic-url-token")) || bytes.Contains(content, []byte("synthetic-url-password")) {
						t.Errorf("credentials stored in %s", path)
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestURLReleaseBuildRestartEvidenceAndPublication(t *testing.T) {
	s, opts := fixture(t)
	// The private working directory deliberately has a Git ancestor. It must not
	// be presented as the Git source of the downloaded manifest.
	base := filepath.Dir(opts.WorkDir)
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "."}, {"commit", "-m", "Fixture inputs"}} {
		if _, err := gitCommand(base, "", nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	repo := gitFixture(t, s)
	raw := []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, "releases/SWD-42.yaml")))
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(raw) }))
	defer remote.Close()
	s.opts.ReleaseCAFile = releaseCA(t, remote)
	w := postURL(s, urlRequest{URL: remote.URL + "/release.yaml"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var staged remoteRelease
	if err := json.Unmarshal(w.Body.Bytes(), &staged); err != nil {
		t.Fatal(err)
	}
	remote.Close()
	restarted, err := New(s.opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	w = postBuild(t, restarted, selection{Environment: "dev", RemoteRelease: staged.Token, ManifestSHA256: staged.Release.SHA256})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var accepted Run
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	run := waitBuild(t, restarted, accepted.ID)
	if run.Status != "succeeded" || run.ReleaseSource == nil {
		t.Fatalf("build: %+v", run)
	}
	found := false
	for _, file := range run.Evidence.Files {
		if file.URLOrigin != nil {
			found = true
			if file.GitRoot != "" || file.LocalState != "downloaded snapshot" || file.URLOrigin.Verify(raw) != nil {
				t.Fatalf("invented origin: %+v", file)
			}
		}
	}
	if !found {
		t.Fatal("missing URL input evidence")
	}
	cli := opts.Build
	cli.Root = opts.Root
	cli.Environment = "dev"
	cli.Target = t.TempDir()
	cli.ReleaseManifests = []string{filepath.Join(opts.WorkDir, run.ID, "release.yaml")}
	if _, err := buildapp.Build(cli); err != nil {
		t.Fatal(err)
	}
	for _, name := range run.Files {
		if mustRead(t, filepath.Join(cli.Target, name)) != mustRead(t, filepath.Join(opts.WorkDir, run.ID, "output", name)) {
			t.Fatalf("CLI/UI output differs: %s", name)
		}
	}
	plan, err := restarted.preparePublication(run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publishReview(plan); err != nil {
		t.Fatal(err)
	}
	receipt := mustRead(t, filepath.Join(repo, "revisions", plan.Tag+".json"))
	if !strings.Contains(receipt, staged.Origin.URL) || !strings.Contains(receipt, staged.Origin.SHA256) || strings.Contains(receipt, "synthetic-url-token") {
		t.Fatal("publication lost safe source metadata")
	}
	if err := os.WriteFile(filepath.Join(opts.WorkDir, ".url-releases", staged.Token, "release.yaml"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := postBuild(t, restarted, selection{Environment: "dev", RemoteRelease: staged.Token, ManifestSHA256: staged.Release.SHA256}); w.Code != 409 {
		t.Fatalf("tampered download: %d", w.Code)
	}
}

func TestURLReleaseErrorsNeverOfferInvalidInput(t *testing.T) {
	s, opts := fixture(t)
	raw := []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, "releases/SWD-42.yaml")))
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			w.Header().Set("Location", "/ok")
			w.WriteHeader(307)
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("login"))
		case "/invalid":
			_, _ = w.Write([]byte("wrong: document"))
		case "/large":
			_, _ = w.Write(bytes.Repeat([]byte("a"), (8<<20)+1))
		case "/error":
			w.WriteHeader(403)
			_, _ = w.Write([]byte("synthetic-url-token"))
		default:
			_, _ = w.Write(raw)
		}
	}))
	defer remote.Close()
	s.opts.ReleaseCAFile = releaseCA(t, remote)
	for _, test := range []struct{ path, checksum string }{{"/redirect", ""}, {"/html", ""}, {"/invalid", ""}, {"/large", ""}, {"/error", ""}, {"/ok", strings.Repeat("0", 64)}} {
		w := postURL(s, urlRequest{URL: remote.URL + test.path, SHA256: test.checksum})
		if w.Code == 200 || strings.Contains(w.Body.String(), "synthetic-url-token") {
			t.Fatalf("invalid input %s: %d %s", test.path, w.Code, w.Body.String())
		}
	}
	for _, address := range []string{"http://example.invalid/release.yaml", "https://user:password@example.invalid/release.yaml", "https://example.invalid/release.yaml?token=private"} {
		if w := postURL(s, urlRequest{URL: address}); w.Code == 200 {
			t.Fatalf("accepted %s", address)
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/releases/url", strings.NewReader("{}"))
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("cross-site: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(opts.WorkDir, ".url-releases")); !os.IsNotExist(err) {
		t.Fatal("invalid download created a reusable selection")
	}
}
