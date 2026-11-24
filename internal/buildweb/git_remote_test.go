package buildweb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kube-env/internal/appinfo"
)

func remoteGitFixture(t *testing.T) (*Server, Options, string, string, []byte) {
	t.Helper()
	_, opts := fixture(t)
	raw := []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, "releases/SWD-42.yaml")))
	repo, head := sourceGit(t, raw)
	opts.ArtifactSource = &ArtifactSource{Storage: "git", Remote: &GitRemote{URL: (&url.URL{Scheme: "file", Path: repo}).String()}, Tag: "release/{release_id}", Path: "releases/{release_id}.yaml"}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	return s, opts, repo, head, raw
}
func capturedGit(t *testing.T, s *Server) remoteRelease {
	t.Helper()
	w := postConfigured(s, `{"release_id":"SWD-42"}`)
	if w.Code != 200 {
		t.Fatalf("read: %d %s", w.Code, w.Body.String())
	}
	var captured remoteRelease
	if err := json.Unmarshal(w.Body.Bytes(), &captured); err != nil {
		t.Fatal(err)
	}
	return captured
}
func advanceGit(t *testing.T, repo string, raw []byte) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "releases/SWD-42.yaml"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "Advance synthetic release"}, {"tag", "-f", "release/SWD-42"}} {
		if _, err := gitCommand(repo, "", nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	head, err := gitCommand(repo, "", nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(head)
}

func TestManagedGitRefreshPinsOriginAndRefusesStaleReads(t *testing.T) {
	s, opts, repo, head, raw := remoteGitFixture(t)
	remote := s.opts.ArtifactSource.Remote
	if _, err := os.Stat(remote.cache); !os.IsNotExist(err) {
		t.Fatal("New fetched source")
	}
	first := capturedGit(t, s)
	if first.GitOrigin == nil || first.GitOrigin.URL != opts.ArtifactSource.Remote.URL || first.GitOrigin.Repository != "" || first.GitOrigin.Commit != head || first.GitOrigin.Verify(raw) != nil {
		t.Fatalf("wrong remote origin: %+v", first.GitOrigin)
	}
	status := s.sourceRefreshStatus()
	if status.Error != "" || status.SucceededAt.IsZero() {
		t.Fatalf("refresh status: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(remote.cache, "repository.git/releases")); !os.IsNotExist(err) {
		t.Fatal("managed copy has working files")
	}
	updated := []byte(strings.ReplaceAll(string(raw), strings.Repeat("a", 64), strings.Repeat("c", 64)))
	newHead := advanceGit(t, repo, updated)
	second := capturedGit(t, s)
	if second.GitOrigin.Commit != newHead {
		t.Fatal("moved tag not refreshed")
	}
	if _, err := gitCommand(repo, "", nil, "tag", "-d", "release/SWD-42"); err != nil {
		t.Fatal(err)
	}
	if w := postConfigured(s, `{"release_id":"SWD-42"}`); w.Code != 400 {
		t.Fatal("deleted remote tag read from old cache")
	}
	if _, err := gitCommand(repo, "", nil, "tag", "release/SWD-42"); err != nil {
		t.Fatal(err)
	}
	capturedGit(t, s)
	if err := os.Rename(repo, repo+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(repo+"-unavailable", repo) })
	if w := postConfigured(s, `{"release_id":"SWD-42"}`); w.Code != 400 {
		t.Fatal("unreachable remote used stale tag")
	}
	status = s.sourceRefreshStatus()
	if status.Error == "" || status.SucceededAt.IsZero() {
		t.Fatalf("failure status: %+v", status)
	}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	w := postBuild(t, s, selection{Environment: "dev", RemoteRelease: first.Token, ManifestSHA256: first.Release.SHA256})
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	var accepted Run
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	run := waitBuild(t, s, accepted.ID)
	if run.Status != "succeeded" || run.ReleaseGitSource == nil || run.ReleaseGitSource.Commit != head || run.ReleaseGitSource.URL != opts.ArtifactSource.Remote.URL {
		t.Fatalf("offline build: %+v", run)
	}
}

func TestManagedGitHTTPSUsesPrivateCredentialsAndCA(t *testing.T) {
	_, opts, repo, _, _ := remoteGitFixture(t)
	if _, err := gitCommand(repo, "", nil, "update-server-info"); err != nil {
		t.Fatal(err)
	}
	files := http.FileServer(http.Dir(filepath.Join(repo, ".git")))
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "reader" || password != "synthetic-git-password" {
			http.Error(w, "synthetic-git-password", 403)
			return
		}
		files.ServeHTTP(w, r)
	}))
	defer server.Close()
	passwordFile := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(passwordFile, []byte("synthetic-git-password\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opts.ArtifactSource.Remote = &GitRemote{URL: server.URL, Username: "reader", PasswordFile: passwordFile, CAFile: releaseCA(t, server)}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	captured := capturedGit(t, s)
	if captured.GitOrigin.URL != server.URL || captured.GitOrigin.Repository != "" {
		t.Fatal("private cache exposed as source")
	}
	// Explicit credentials are injected per process; no Git config or status stores them.
	if err := filepath.WalkDir(opts.WorkDir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		for _, secret := range []string{"synthetic-git-password", base64.StdEncoding.EncodeToString([]byte("reader:synthetic-git-password")), "Authorization:"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("credential persisted in %s", name)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwordFile, []byte("wrong-synthetic-password"), 0600); err != nil {
		t.Fatal(err)
	}
	w := postConfigured(s, `{"release_id":"SWD-42"}`)
	if w.Code != 400 || strings.Contains(w.Body.String(), "synthetic-git-password") || s.sourceRefreshStatus().Error == "" {
		t.Fatal("invalid credentials accepted or reflected")
	}
	// A caller's inherited Git trace setting must not receive an authorization header.
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_TRACE_CURL", trace)
	_ = postConfigured(s, `{"release_id":"SWD-42"}`)
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatal("Git trace inherited")
	}
}

func TestManagedGitOwnershipAndConcurrentAccess(t *testing.T) {
	for _, kind := range []string{"unowned", "symlink", "working-copy"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _, _, _ := remoteGitFixture(t)
			remote := s.opts.ArtifactSource.Remote
			if err := os.MkdirAll(filepath.Dir(remote.cache), 0700); err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" {
				if err := os.Symlink(t.TempDir(), remote.cache); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(remote.cache, 0700); err != nil {
					t.Fatal(err)
				}
				if kind == "working-copy" {
					owner, _ := json.Marshal(map[string]string{"identity": remote.identity()})
					if err := os.WriteFile(filepath.Join(remote.cache, "owner.json"), owner, 0600); err != nil {
						t.Fatal(err)
					}
					if _, err := gitCommand(remote.cache, "", nil, "init", "repository.git"); err != nil {
						t.Fatal(err)
					}
				}
			}
			if w := postConfigured(s, `{"release_id":"SWD-42"}`); w.Code != 400 {
				t.Fatalf("accepted %s", kind)
			}
		})
	}
	s, _, _, _, _ := remoteGitFixture(t)
	unlock, err := gitSourceLocks.TryAcquire(s.opts.ArtifactSource.Remote.cache, "test")
	if err != nil {
		t.Fatal(err)
	}
	w := postConfigured(s, `{"release_id":"SWD-42"}`)
	unlock()
	if w.Code != 400 || !strings.Contains(w.Body.String(), "busy") {
		t.Fatal("unlocked concurrent access")
	}
	capturedGit(t, s)
}

func TestManagedGitScheduleRefreshAndCancellation(t *testing.T) {
	s, _, repo, _, raw := remoteGitFixture(t)
	s.opts.ArtifactSource.Remote.RefreshInterval = "1s"
	events := make(chan error, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartSourceRefresh(ctx, func(err error) { events <- err })
	select {
	case err := <-events:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no initial scheduled refresh")
	}
	updated := []byte(strings.ReplaceAll(string(raw), strings.Repeat("a", 64), strings.Repeat("d", 64)))
	head := advanceGit(t, repo, updated)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case err := <-events:
			if err != nil {
				t.Fatal(err)
			}
			actual, err := gitCommand(filepath.Join(s.opts.ArtifactSource.Remote.cache, "repository.git"), "", nil, "rev-parse", "refs/tags/release/SWD-42")
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(actual) == head {
				cancel()
				before := s.sourceRefreshStatus().AttemptedAt
				select {
				case <-events:
					t.Fatal("scheduled after cancellation")
				case <-time.After(1200 * time.Millisecond):
				}
				if !s.sourceRefreshStatus().AttemptedAt.Equal(before) {
					t.Fatal("refresh continued after cancellation")
				}
				return
			}
		case <-deadline:
			t.Fatal("scheduled refresh did not update tag")
		}
	}
}

func TestRemoteConfigValidationAndPaths(t *testing.T) {
	for _, remote := range []GitRemote{
		{URL: "https://user:secret@example.invalid/repo"}, {URL: "https://example.invalid/repo?token=secret"}, {URL: "http://example.invalid/repo"},
		{URL: "ssh://git@example.invalid/repo", Username: "reader", PasswordFile: "password"}, {URL: "file:///tmp/repo", CAFile: "ca.pem"},
		{URL: "https://example.invalid/repo", Username: "reader"}, {URL: "https://example.invalid/repo", RefreshInterval: "0s"},
	} {
		if remote.Validate() == nil {
			t.Errorf("accepted invalid remote config")
		}
	}
	base := t.TempDir()
	filename := filepath.Join(base, "source.yaml")
	raw := "artifact_source:\n  storage: git\n  tag: release/{release_id}\n  path: releases/{release_id}.yaml\n  remote:\n    url: https://git.example.invalid/project.git\n    username: reader\n    password_file: secrets/password\n    ca_file: ca.pem\n    refresh_interval: 5m\n"
	if err := os.WriteFile(filename, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadServeConfig(filename)
	if err != nil {
		t.Fatal(err)
	}
	source := config.Source
	if err != nil || source.Validate() != nil {
		t.Fatalf("remote YAML: %+v %v", source, err)
	}
	if source.Remote.PasswordFile != filepath.Join(base, "secrets/password") || source.Remote.CAFile != filepath.Join(base, "ca.pem") {
		t.Fatal("remote credential paths")
	}
	source.Repository = "attached"
	if source.Validate() == nil {
		t.Fatal("remote and attached allowed together")
	}
}

func TestManagedGitRejectsUntrustedTLSAndRedirects(t *testing.T) {
	_, opts, _, _, _ := remoteGitFixture(t)
	var redirected atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		http.Error(w, "unexpected redirect", 500)
	}))
	defer target.Close()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/info/refs", http.StatusFound)
	}))
	defer upstream.Close()
	opts.ArtifactSource.Remote = &GitRemote{URL: upstream.URL}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	if w := postConfigured(s, `{"release_id":"SWD-42"}`); w.Code != 400 {
		t.Fatal("untrusted TLS accepted")
	}
	s.opts.ArtifactSource.Remote.CAFile = releaseCA(t, upstream)
	if w := postConfigured(s, `{"release_id":"SWD-42"}`); w.Code != 400 {
		t.Fatal("redirect accepted")
	}
	if redirected.Load() != 0 {
		t.Fatal("Git followed a redirect")
	}
}
