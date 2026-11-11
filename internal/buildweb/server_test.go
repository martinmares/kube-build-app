package buildweb

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kube-env/internal/appinfo"
	"kube-env/internal/buildapp"
)

func fixture(t *testing.T) (*Server, Options) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "environments")
	releases := filepath.Join(base, "images")
	source := filepath.Join("..", "..", "fixtures", "readme-smoke", "environments")
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(releases, "releases"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: oci-toolbox/v2
kind: ImageRelease
release_id: SWD-42
images:
  - id: api
    app_name: api
    container_name: api
    repository: registry.example.invalid/team/api
    digest: sha256:` + strings.Repeat("a", 64) + `
  - id: worker
    app_name: worker
    container_name: worker
    repository: registry.example.invalid/team/worker
    digest: sha256:` + strings.Repeat("b", 64) + "\n"
	if err := os.WriteFile(filepath.Join(releases, "releases", "SWD-42.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	opts := Options{Root: root, ReleasesRoot: releases, WorkDir: filepath.Join(base, "builds"), Build: buildapp.Options{ImagePolicy: "strict", ImageReference: "digest"}}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	return s, opts
}

func postBuild(t *testing.T, s *Server, chosen selection) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(chosen)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/builds", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func waitBuild(t *testing.T, s *Server, id string) Run {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, ok := s.snapshot(id)
		if !ok {
			t.Fatal("missing run")
		}
		if run.Status != "running" {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("build did not finish")
	return Run{}
}

func startFixtureBuild(t *testing.T, s *Server) Run {
	t.Helper()
	_, releases, _, err := s.catalog()
	if err != nil || len(releases) != 1 {
		t.Fatalf("catalog: %v %v", releases, err)
	}
	w := postBuild(t, s, selection{Environment: "dev", Manifest: releases[0].Path, ManifestSHA256: releases[0].SHA256})
	if w.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var started Run
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	return waitBuild(t, s, started.ID)
}

func TestBuildUIUsesSameRendererAndSummaryAsCLI(t *testing.T) {
	s, opts := fixture(t)
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Synthetic Builder"}, {"config", "user.email", "builder@example.invalid"}, {"config", "commit.gpgsign", "false"}, {"add", "."}, {"commit", "-m", "Synthetic release"}} {
		if _, err := gitCommand(opts.ReleasesRoot, "", nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	manifestPath := filepath.Join(opts.ReleasesRoot, "releases", "SWD-42.yaml")
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	run := startFixtureBuild(t, s)
	if run.Status != "succeeded" {
		t.Fatalf("build: %+v", run)
	}
	if len(run.Events) == 0 || run.Summary == nil || run.Summary.Totals.Apps != 2 || run.Summary.Totals.Replicas != 3 {
		t.Fatalf("missing real progress or summary: %+v", run)
	}
	if run.Evidence == nil || len(run.Evidence.Outputs) != len(run.Files) {
		t.Fatal("build lacks input/output evidence")
	}
	var releaseOrigin bool
	for _, file := range run.Evidence.Files {
		if file.GitPath == "releases/SWD-42.yaml" {
			releaseOrigin = file.GitRoot != "" && file.ReadPath != "" && file.LocalState == "matches-commit" && file.SHA256 == run.Selection.ManifestSHA256
		}
	}
	if !releaseOrigin {
		t.Fatal("staged release lost its original Git provenance")
	}
	cli := opts.Build
	cli.Root = opts.Root
	cli.Environment = "dev"
	cli.Target = t.TempDir()
	cli.ReleaseManifests = []string{manifestPath}
	var observed []buildapp.BuildEvent
	result, err := buildapp.BuildWithObserver(cli, func(event buildapp.BuildEvent) {
		observed = append(observed, event)
		if event.Type == "deployment" {
			if _, err := os.Stat(event.Path); err != nil {
				t.Errorf("event preceded its actual output: %v", err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(observed, result.Events) || !reflect.DeepEqual(result.Summary, *run.Summary) {
		t.Fatal("UI and CLI render context differ")
	}
	for _, name := range run.Files {
		actual, err := os.ReadFile(filepath.Join(opts.WorkDir, run.ID, "output", filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		expected, err := os.ReadFile(filepath.Join(cli.Target, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("different output %s: %v", name, err)
		}
	}
	deployment, err := os.ReadFile(filepath.Join(opts.WorkDir, run.ID, "output/deployments/api-deployment.yml"))
	if err != nil || !strings.Contains(string(deployment), "@sha256:"+strings.Repeat("a", 64)) {
		t.Fatal("lost release digest")
	}
	snapshot, err := os.ReadFile(filepath.Join(opts.WorkDir, run.ID, "release.yaml"))
	if err != nil || !bytes.Equal(snapshot, original) {
		t.Fatal("lost exact release snapshot")
	}
	current, _ := os.ReadFile(manifestPath)
	if !bytes.Equal(current, original) {
		t.Fatal("modified release repository")
	}
	archive := httptest.NewRecorder()
	s.Handler().ServeHTTP(archive, httptest.NewRequest(http.MethodGet, "/api/builds/"+run.ID+"/archive", nil))
	reader, err := zip.NewReader(bytes.NewReader(archive.Body.Bytes()), int64(archive.Body.Len()))
	if archive.Code != 200 || err != nil {
		t.Fatalf("archive: %d %v", archive.Code, err)
	}
	if len(reader.File) != len(run.Files) {
		t.Fatal("archive omitted generated files")
	}
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(opened)
		opened.Close()
		expected, _ := os.ReadFile(filepath.Join(cli.Target, filepath.FromSlash(file.Name)))
		if err != nil || !bytes.Equal(raw, expected) {
			t.Fatalf("archive differs: %s", file.Name)
		}
	}
	restarted, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	restored, ok := restarted.snapshot(run.ID)
	if !ok || restored.Status != "succeeded" || !reflect.DeepEqual(restored.Summary, run.Summary) {
		t.Fatal("lost completed build after restart")
	}
}

func TestBuildUIRejectsChangedReleaseAndArbitraryInputs(t *testing.T) {
	s, opts := fixture(t)
	_, releases, _, err := s.catalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, chosen := range []selection{
		{Environment: "../escape", Manifest: releases[0].Path, ManifestSHA256: releases[0].SHA256},
		{Environment: "dev", Manifest: "../outside.yaml", ManifestSHA256: releases[0].SHA256},
		{Environment: "dev", Manifest: releases[0].Path, ManifestSHA256: "stale"},
	} {
		if w := postBuild(t, s, chosen); w.Code != 400 && w.Code != 409 {
			t.Fatalf("accepted invalid input: %d", w.Code)
		}
	}
	if err := os.WriteFile(filepath.Join(opts.ReleasesRoot, releases[0].Path), append([]byte("# changed\n"), []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, releases[0].Path)))...), 0644); err != nil {
		t.Fatal(err)
	}
	if w := postBuild(t, s, selection{Environment: "dev", Manifest: releases[0].Path, ManifestSHA256: releases[0].SHA256}); w.Code != 409 {
		t.Fatal("built changed release without refreshing")
	}
	if len(s.runs) != 0 {
		t.Fatal("invalid selection created a build")
	}
	r := httptest.NewRequest(http.MethodPost, "/api/builds", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://outside.example.invalid")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("accepted cross-origin build")
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestBuildUIReportsFailureAndInterruptedBuild(t *testing.T) {
	s, opts := fixture(t)
	if err := os.WriteFile(filepath.Join(opts.Root, "dev/apps/api.yml"), []byte("containers: [\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run := startFixtureBuild(t, s)
	if run.Status != "failed" || run.Error == "" || run.Summary != nil {
		t.Fatalf("false success: %+v", run)
	}
	run.Status = "running"
	run.Error = ""
	if err := s.save(&run); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	restored, _ := restarted.snapshot(run.ID)
	if restored.Status != "interrupted" || restored.Error == "" {
		t.Fatal("restart pretended build completed")
	}
}

func TestBuildUIConfinesGeneratedFileAccess(t *testing.T) {
	s, opts := fixture(t)
	run := startFixtureBuild(t, s)
	if run.Status != "succeeded" {
		t.Fatal(run.Error)
	}
	name := run.Files[0]
	path := filepath.Join(opts.WorkDir, run.ID, "output", filepath.FromSlash(name))
	file := httptest.NewRecorder()
	s.Handler().ServeHTTP(file, httptest.NewRequest(http.MethodGet, "/api/builds/"+run.ID+"/file/"+name, nil))
	if file.Code != 200 {
		t.Fatalf("file: %d", file.Code)
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("synthetic outside content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"/file/" + name, "/archive"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/builds/"+run.ID+suffix, nil))
		if w.Code == 200 || strings.Contains(w.Body.String(), "synthetic outside content") {
			t.Fatal("followed output symlink outside build")
		}
	}
	other := opts
	other.Root = t.TempDir()
	if _, err := New(other, appinfo.For(appinfo.BuildAppName)); err == nil || !strings.Contains(err.Error(), "another environment repository") {
		t.Fatalf("mixed domain build history: %v", err)
	}
	if _, err := New(Options{Root: opts.Root, ReleasesRoot: opts.ReleasesRoot, WorkDir: filepath.Join(opts.Root, "outputs")}, appinfo.For(appinfo.BuildAppName)); err == nil {
		t.Fatal("accepted outputs in input repository")
	}
}

func TestBuildUIReadsVariablesOnceAndRejectsConcurrentBuild(t *testing.T) {
	_, opts := fixture(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var unblock sync.Once
	var calls atomic.Int32
	variables := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		io.WriteString(w, "NAMESPACE=readme-dev\nTSM_REGISTRY_URL=registry.example.test/tsm\nTSM_RELEASE_ID=fixture\nPUBLIC_API_HOST=api.example.test\n")
	}))
	defer func() { unblock.Do(func() { close(release) }); variables.Close() }()
	opts.Build.EnvURL = variables.URL
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	_, releases, _, err := s.catalog()
	if err != nil {
		t.Fatal(err)
	}
	chosen := selection{Environment: "dev", Manifest: releases[0].Path, ManifestSHA256: releases[0].SHA256}
	w := postBuild(t, s, chosen)
	if w.Code != 202 {
		t.Fatalf("start: %s", w.Body.String())
	}
	var started Run
	if err := json.Unmarshal(w.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("build did not read variables")
	}
	if duplicate := postBuild(t, s, chosen); duplicate.Code != 409 {
		t.Fatal("accepted concurrent build")
	}
	path := filepath.Join(opts.ReleasesRoot, chosen.Manifest)
	original := mustRead(t, path)
	if err := os.WriteFile(path, []byte("changed: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	unblock.Do(func() { close(release) })
	run := waitBuild(t, s, started.ID)
	if run.Status != "succeeded" || calls.Load() != 1 || run.Summary == nil {
		t.Fatalf("variable reads=%d run=%+v", calls.Load(), run)
	}
	if snapshot := mustRead(t, filepath.Join(opts.WorkDir, run.ID, "release.yaml")); snapshot != original {
		t.Fatal("build used changed source manifest")
	}
	if run.Context.EnvURLHeaderCount != 0 || !run.Context.EnvURLConfigured || run.EnvironmentRoot != s.opts.Root {
		t.Fatal("missing safe build context")
	}
}
