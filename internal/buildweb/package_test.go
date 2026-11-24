package buildweb

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
	"kube-env/internal/appinfo"
	"kube-env/internal/buildapp"
)

func packageRequest(s *Server, method, address string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(method, address, nil))
	return w
}
func preparePackage(t *testing.T, s *Server, id string) packageReview {
	t.Helper()
	w := packageRequest(s, "POST", "/api/builds/"+id+"/package-review")
	if w.Code != 200 {
		t.Fatalf("package review: %d %s", w.Code, w.Body.String())
	}
	var review packageReview
	if err := json.Unmarshal(w.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	return review
}
func unzipPackage(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, file := range reader.File {
		if _, exists := files[file.Name]; exists {
			t.Fatal("duplicate archive entry")
		}
		input, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(input)
		input.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = content
	}
	return files
}
func TestPackageContainsExactOutputsAndPublicEvidenceOnly(t *testing.T) {
	s, opts := fixture(t)
	run := startFixtureBuild(t, s)
	if run.Status != "succeeded" {
		t.Fatal(run.Error)
	}
	// Private input data and its low-entropy value hashes must not enter the package.
	run.Evidence.Variables = append(run.Evidence.Variables, buildapp.PrivateInput{Kind: "synthetic", SHA256: "private-value-hash-marker", Snapshot: "private-snapshot-marker"})
	private := filepath.Join(opts.WorkDir, run.ID, "inputs", "synthetic-private.txt")
	if err := os.WriteFile(private, []byte("synthetic-private-password"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, inventory, err := s.buildPackage(run)
	if err != nil {
		t.Fatal(err)
	}
	repeated, _, err := s.buildPackage(run)
	if err != nil || !bytes.Equal(raw, repeated) {
		t.Fatal("package is not deterministic")
	}
	files := unzipPackage(t, raw)
	if len(files) != len(run.Files)+2 {
		t.Fatalf("unexpected archive entries: %v", files)
	}
	var manifest PackageManifest
	if err := yaml.Unmarshal(files["package.yaml"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Kind != "DeploymentPackage" || manifest.APIVersion != "kube-build-app/v1" || manifest.ReleaseID != run.ReleaseID || manifest.Environment != "dev" || manifest.BuildID != run.ID || manifest.ReleaseSHA256 != run.Selection.ManifestSHA256 || manifest.PrivateVariableInputs != len(run.Evidence.Variables) {
		t.Fatalf("manifest: %+v", manifest)
	}
	if manifest.Evidence.SHA256 != "sha256:"+packageHash(files["build-evidence.json"]) || manifest.Evidence.Bytes != len(files["build-evidence.json"]) {
		t.Fatal("evidence checksum")
	}
	for _, entry := range inventory {
		if entry.SHA256 != "sha256:"+packageHash(files[entry.Path]) || entry.Bytes != len(files[entry.Path]) {
			t.Fatalf("file checksum: %s", entry.Path)
		}
		original := mustRead(t, filepath.Join(opts.WorkDir, run.ID, "output", strings.TrimPrefix(entry.Path, "manifests/")))
		if string(files[entry.Path]) != original {
			t.Fatal("changed manifest bytes")
		}
	}
	for name, content := range files {
		if strings.HasPrefix(name, "inputs/") || strings.Contains(name, "release.yaml") {
			t.Fatalf("private entry: %s", name)
		}
		for _, forbidden := range []string{"synthetic-private-password", "private-value-hash-marker", "private-snapshot-marker", `"snapshot":`, `"read_path":`} {
			if bytes.Contains(content, []byte(forbidden)) {
				t.Fatalf("private data leaked in %s", name)
			}
		}
	}
}

func TestPackageReviewRestartDownloadAndOutputChanges(t *testing.T) {
	s, opts := fixture(t)
	run := startFixtureBuild(t, s)
	review := preparePackage(t, s, run.ID)
	again := preparePackage(t, s, run.ID)
	if again.SHA256 != review.SHA256 {
		t.Fatal("review changes archive content")
	}
	first := packageRequest(s, "GET", "/api/builds/"+run.ID+"/packages/"+review.ID+"/archive")
	if first.Code != 200 || packageHash(first.Body.Bytes()) != review.SHA256 || first.Header().Get("X-Checksum-Sha256") != review.SHA256 {
		t.Fatal("archive download checksum")
	}
	// Confirmation/download use the reviewed snapshot, not mutable renderer output files.
	if err := os.WriteFile(filepath.Join(opts.WorkDir, run.ID, "output", run.Files[0]), []byte("changed after review"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	second := packageRequest(restarted, "GET", "/api/builds/"+run.ID+"/packages/"+review.ID+"/archive")
	if second.Code != 200 || !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) {
		t.Fatal("restart did not preserve reviewed package")
	}
	invalid := packageRequest(restarted, "POST", "/api/builds/"+run.ID+"/package-review")
	if invalid.Code != 409 || !strings.Contains(invalid.Body.String(), "differs from recorded") {
		t.Fatal("modified output accepted")
	}
	archive := filepath.Join(opts.WorkDir, run.ID, "packages", review.ID, "deployment.zip")
	if err := os.WriteFile(archive, []byte("tampered package"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := packageRequest(restarted, "GET", "/api/builds/"+run.ID+"/packages/"+review.ID+"/archive"); w.Code != 409 {
		t.Fatal("modified package downloaded")
	}
}

func TestPackageDirectoryExportReviewRetryAndConflict(t *testing.T) {
	_, opts := fixture(t)
	target := filepath.Join(t.TempDir(), "exports")
	opts.PackageOutput = &PackageOutput{Storage: "directory", Directory: target}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	run := startFixtureBuild(t, s)
	review := preparePackage(t, s, run.ID)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("review wrote to export destination")
	}
	expected := "packages/dev/SWD-42/" + run.ID + ".zip"
	resolvedTarget, err := resolvedDirectory(target)
	if err != nil {
		t.Fatal(err)
	}
	if review.Directory != resolvedTarget || review.Path != expected {
		t.Fatalf("destination: %+v", review)
	}
	restarted, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	address := "/api/builds/" + run.ID + "/packages/" + review.ID + "/publish"
	for i := 0; i < 2; i++ {
		if w := packageRequest(restarted, "POST", address); w.Code != 200 {
			t.Fatalf("save/retry: %d %s", w.Code, w.Body.String())
		}
	}
	saved, err := os.ReadFile(filepath.Join(target, expected))
	if err != nil || packageHash(saved) != review.SHA256 {
		t.Fatal("wrong exported bytes")
	}
	// A configuration change after review must not silently redirect publication.
	drift := opts
	copy := *opts.PackageOutput
	copy.Directory = filepath.Join(t.TempDir(), "other")
	drift.PackageOutput = &copy
	other, err := New(drift, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	if w := packageRequest(other, "POST", address); w.Code != 409 {
		t.Fatal("changed destination accepted without new review")
	}
	if _, err := os.Stat(copy.Directory); !os.IsNotExist(err) {
		t.Fatal("wrote to changed destination")
	}
	if err := os.WriteFile(filepath.Join(target, expected), []byte("existing different publication"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := packageRequest(restarted, "POST", address); w.Code != 409 {
		t.Fatal("existing destination overwritten")
	}
	if got := mustRead(t, filepath.Join(target, expected)); got != "existing different publication" {
		t.Fatal("conflict destroyed existing file")
	}
	// Browser navigation cannot use cross-site POST to create or publish packages.
	for _, url := range []string{"/api/builds/" + run.ID + "/package-review", address} {
		r := httptest.NewRequest("POST", url, nil)
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		restarted.Handler().ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("cross-site package action accepted")
		}
	}
}

func TestPackageDirectoryCanBeGitWithoutAutomaticCommit(t *testing.T) {
	_, opts := fixture(t)
	repo, head := sourceGit(t, []byte("existing tracked content"))
	opts.PackageOutput = &PackageOutput{Storage: "directory", Directory: repo, Path: "deliveries/{environment}/{release_id}-{build_id}.zip"}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	run := startFixtureBuild(t, s)
	review := preparePackage(t, s, run.ID)
	w := packageRequest(s, "POST", "/api/builds/"+run.ID+"/packages/"+review.ID+"/publish")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	actual, err := gitCommand(repo, "", nil, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(actual) != head {
		t.Fatal("export changed Git HEAD")
	}
	staged, err := gitCommand(repo, "", nil, "diff", "--cached", "--name-only")
	if err != nil || staged != "" {
		t.Fatal("export staged files")
	}
	status, err := gitCommand(repo, "", nil, "status", "--porcelain")
	if err != nil || !strings.Contains(status, "?? deliveries/") {
		t.Fatal("package not left for owner to commit")
	}
}

func TestPackageRejectsSecretsMalformedYAMLAndUnsafeInventory(t *testing.T) {
	for _, raw := range []string{
		"apiVersion: v1\nkind: Secret\nstringData:\n  password: synthetic-secret-material\n",
		"kind: List\nitems:\n  - kind: Secret\n    stringData: {password: synthetic-secret-material}\n",
		"kind: SecretList\nitems: []\n",
		"kind: ConfigMap\n---\nkind: Secret\n",
		"&loop {kind: List, items: [*loop]}",
		"kind: [unterminated",
	} {
		if err := checkPackageResource([]byte(raw)); err == nil || strings.Contains(err.Error(), "synthetic-secret-material") {
			t.Fatal("secret/invalid YAML accepted or reflected")
		}
	}
	s, opts := fixture(t)
	run := startFixtureBuild(t, s)
	raw := []byte("kind: Secret\nstringData:\n  password: synthetic-secret-material\n")
	name := run.Files[0]
	if err := os.WriteFile(filepath.Join(opts.WorkDir, run.ID, "output", name), raw, 0600); err != nil {
		t.Fatal(err)
	}
	for i := range run.Evidence.Outputs {
		if run.Evidence.Outputs[i].Path == name {
			run.Evidence.Outputs[i].SHA256 = "sha256:" + packageHash(raw)
			run.Evidence.Outputs[i].Bytes = len(raw)
		}
	}
	if _, _, err := s.buildPackage(run); err == nil || !strings.Contains(err.Error(), "Secret resources") {
		t.Fatalf("package containing Secret: %v", err)
	}
	run.Files[0] = "../outside.yaml"
	if _, _, err := s.buildPackage(run); err == nil {
		t.Fatal("unsafe output path accepted")
	}
}

func TestPackageOutputRootAndSymlinks(t *testing.T) {
	_, opts := fixture(t)
	for _, directory := range []string{opts.Root, filepath.Join(opts.WorkDir, "public"), opts.ReleasesRoot} {
		opts.PackageOutput = &PackageOutput{Storage: "directory", Directory: directory}
		if _, err := New(opts, appinfo.For(appinfo.BuildAppName)); err == nil {
			t.Fatal("package output overlaps input/private directories")
		}
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := exportPackage(root, "escape/build.zip", []byte("synthetic package")); err == nil {
		t.Fatal("directory symlink escaped output root")
	}
	if _, err := os.Stat(filepath.Join(outside, "build.zip")); !os.IsNotExist(err) {
		t.Fatal("wrote outside target")
	}
	if err := os.WriteFile(filepath.Join(root, "original.zip"), []byte("synthetic package"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("original.zip", filepath.Join(root, "alias.zip")); err != nil {
		t.Fatal(err)
	}
	if err := exportPackage(root, "alias.zip", []byte("synthetic package")); err == nil {
		t.Fatal("symlink accepted as immutable existing publication")
	}
}

func TestPackageResourceInspectionAllowsCustomResourceBusinessItems(t *testing.T) {
	raw := []byte("apiVersion: custom.example.invalid/v1\nkind: CustomBundle\nitems: [business, data]\n")
	if err := checkPackageResource(raw); err != nil {
		t.Fatalf("custom resource rejected: %v", err)
	}
}

func TestPackageAtomicExportDoesNotOverwriteConcurrentContent(t *testing.T) {
	directory := t.TempDir()
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, raw := range [][]byte{[]byte("first synthetic package"), []byte("second synthetic package")} {
		go func(raw []byte) { <-start; results <- exportPackage(directory, "release/deployment.zip", raw) }(raw)
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent conflicting exports had %d successes", successes)
	}
	content := mustRead(t, filepath.Join(directory, "release/deployment.zip"))
	if content != "first synthetic package" && content != "second synthetic package" {
		t.Fatal("partial content published")
	}
	partial, err := filepath.Glob(filepath.Join(directory, "release", "*.part-*"))
	if err != nil || len(partial) != 0 {
		t.Fatal("temporary file not cleaned up")
	}
}

func TestPackagePublicationRejectsOutputRootRetargetedAfterReview(t *testing.T) {
	_, opts := fixture(t)
	target := filepath.Join(t.TempDir(), "export")
	opts.PackageOutput = &PackageOutput{Storage: "directory", Directory: target}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	run := startFixtureBuild(t, s)
	review := preparePackage(t, s, run.ID)
	if err := os.Symlink(opts.Root, target); err != nil {
		t.Fatal(err)
	}
	w := packageRequest(s, "POST", "/api/builds/"+run.ID+"/packages/"+review.ID+"/publish")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "changed on disk") {
		t.Fatal("retargeted root accepted")
	}
	if _, err := os.Stat(filepath.Join(opts.Root, review.Path)); !os.IsNotExist(err) {
		t.Fatal("package written into input root")
	}
}

func TestPackageRejectsBuildWithSecuredVariableDecryptionEnabled(t *testing.T) {
	s, _ := fixture(t)
	run := startFixtureBuild(t, s)
	run.Context.DecryptSecured = true
	if _, _, err := s.buildPackage(run); err == nil || !strings.Contains(err.Error(), "decryption enabled") {
		t.Fatal("decrypted secured variables could be exported")
	}
}

func TestPackageArtifactoryPublishesReviewedZIP(t *testing.T) {
	var mu sync.Mutex
	var uploaded []byte
	var puts int
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer fixture-token" || !strings.HasPrefix(r.URL.Path, "/artifactory/generic/packages/dev/SWD-42/") {
			http.Error(w, "invalid request", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if uploaded == nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(uploaded)
		case http.MethodPut:
			puts++
			if uploaded != nil || r.Header.Get("If-None-Match") != "*" {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			raw, err := io.ReadAll(r.Body)
			if err != nil || packageHash(raw) != r.Header.Get("X-Checksum-Sha256") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			uploaded = raw
			// The upload can succeed even when its response is lost.
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer remote.Close()

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: remote.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte("fixture-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, opts := fixture(t)
	opts.PackageOutput = &PackageOutput{Storage: "artifactory", BaseURL: remote.URL + "/artifactory/generic", TokenFile: tokenFile, CAFile: caFile, OverwritePolicy: "server-deny"}
	s, err := New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	run := startFixtureBuild(t, s)
	review := preparePackage(t, s, run.ID)
	mu.Lock()
	count := puts
	mu.Unlock()
	if count != 0 || review.URL != opts.PackageOutput.BaseURL+"/"+review.Path || review.Directory != "" {
		t.Fatalf("invalid review: %+v, writes: %d", review, count)
	}
	endpoint := "/api/builds/" + run.ID + "/packages/" + review.ID + "/publish"
	for i := 0; i < 2; i++ {
		if w := packageRequest(s, "POST", endpoint); w.Code != 200 || strings.Contains(w.Body.String(), "fixture-token") {
			t.Fatalf("publish/retry: %d %s", w.Code, w.Body.String())
		}
	}
	archive := packageRequest(s, "GET", "/api/builds/"+run.ID+"/packages/"+review.ID+"/archive")
	mu.Lock()
	count, same := puts, bytes.Equal(uploaded, archive.Body.Bytes())
	uploaded = []byte("another ZIP")
	mu.Unlock()
	if count != 1 || !same || archive.Code != 200 {
		t.Fatalf("expected one exact upload, got %d", count)
	}
	if w := packageRequest(s, "POST", endpoint); w.Code != 409 {
		t.Fatalf("existing different ZIP accepted: %d %s", w.Code, w.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if puts != 1 || string(uploaded) != "another ZIP" {
		t.Fatal("existing remote package was modified")
	}
}
