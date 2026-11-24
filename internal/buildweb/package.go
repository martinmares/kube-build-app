package buildweb

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"kube-env/internal/appinfo"
	"kube-env/internal/buildapp"
	"kube-env/internal/pathlock"
)

const maxPackageBytes = 64 << 20
const maxPackageArchiveBytes = 80 << 20
const defaultPackagePath = "packages/{environment}/{release_id}/{build_id}.zip"

type PackageOutput struct {
	Storage         string `yaml:"storage"`
	Directory       string `yaml:"directory"`
	Path            string `yaml:"path"`
	BaseURL         string `yaml:"base_url"`
	TokenFile       string `yaml:"token_file"`
	CAFile          string `yaml:"ca_file"`
	OverwritePolicy string `yaml:"overwrite_policy"`
}

func (p PackageOutput) pattern() string {
	if p.Path == "" {
		return defaultPackagePath
	}
	return p.Path
}
func (p PackageOutput) Validate() error {
	switch p.Storage {
	case "directory":
		if strings.TrimSpace(p.Directory) == "" || p.BaseURL != "" || p.TokenFile != "" || p.CAFile != "" || p.OverwritePolicy != "" {
			return fmt.Errorf("directory package_output requires directory and accepts no remote settings")
		}
	case "artifactory":
		if err := buildapp.ValidateDocumentURL(p.BaseURL); err != nil || p.Directory != "" || p.OverwritePolicy != "server-deny" {
			return fmt.Errorf("Artifactory package_output requires an HTTPS base_url and overwrite_policy: server-deny")
		}
	default:
		return fmt.Errorf("package_output storage must be directory or artifactory")
	}
	name, err := expandSourcePath(p.pattern(), map[string]string{"release_id": "example", "environment": "example", "build_id": "example"})
	if err != nil || path.Ext(name) != ".zip" {
		return fmt.Errorf("package path must be a clean relative .zip path using release_id, environment or build_id")
	}
	return nil
}
func (s *Server) configurePackageOutput() error {
	if s.opts.PackageOutput == nil {
		return nil
	}
	target := *s.opts.PackageOutput
	if err := target.Validate(); err != nil {
		return err
	}
	if target.Storage == "artifactory" {
		return nil
	}
	directory, err := resolvedDirectory(target.Directory)
	if err != nil {
		return err
	}
	inputs := []string{s.opts.Root, s.opts.ReleasesRoot, s.opts.WorkDir}
	if source := s.opts.ArtifactSource; source != nil && source.Repository != "" {
		inputs = append(inputs, source.Repository)
	}
	for _, input := range inputs {
		if within(input, directory) || within(directory, input) {
			return fmt.Errorf("package output directory must be separate from inputs and private work directory")
		}
	}
	target.Directory = directory
	s.opts.PackageOutput = &target
	return nil
}

type PackageFile struct {
	Path   string `json:"path" yaml:"path"`
	SHA256 string `json:"sha256" yaml:"sha256"`
	Bytes  int    `json:"bytes" yaml:"bytes"`
}
type PackageManifest struct {
	APIVersion            string        `yaml:"apiVersion"`
	Kind                  string        `yaml:"kind"`
	ReleaseID             string        `yaml:"release_id"`
	Environment           string        `yaml:"environment"`
	BuildID               string        `yaml:"build_id"`
	FinishedAt            time.Time     `yaml:"finished_at"`
	Builder               appinfo.Info  `yaml:"builder"`
	ReleaseSHA256         string        `yaml:"release_manifest_sha256"`
	Evidence              PackageFile   `yaml:"evidence"`
	Files                 []PackageFile `yaml:"files"`
	PrivateVariableInputs int           `yaml:"private_variable_inputs"`
}
type packageReview struct {
	ID          string        `json:"id"`
	RunID       string        `json:"run_id"`
	ReleaseID   string        `json:"release_id"`
	Environment string        `json:"environment"`
	SHA256      string        `json:"sha256"`
	Bytes       int           `json:"bytes"`
	Files       []PackageFile `json:"files"`
	Directory   string        `json:"directory,omitempty"`
	Path        string        `json:"path,omitempty"`
	URL         string        `json:"url,omitempty"`
}

func packageHash(raw []byte) string { return fmt.Sprintf("%x", sha256.Sum256(raw)) }

// Public metadata contains no variable snapshots or resolved variable hashes.
func packageEvidence(evidence buildapp.BuildEvidence) ([]byte, error) {
	evidence.Files = append([]buildapp.InputEvidence(nil), evidence.Files...)
	for i := range evidence.Files {
		file := &evidence.Files[i]
		file.Snapshot = ""
		file.ReadPath = ""
		file.ResolvedPath = ""
		if file.URLOrigin != nil || file.GitOrigin != nil {
			file.Path = ""
		}
	}
	evidence.Variables = nil
	evidence.Context.ReleaseManifest = ""
	evidence.Context.ReleaseManifests = nil
	return json.MarshalIndent(evidence, "", "  ")
}

// Inspect every YAML document, including List items. Never silently drop a Secret.
func checkPackageResource(raw []byte) error {
	var inspect func(map[string]any, int) error
	inspect = func(value map[string]any, depth int) error {
		if depth > 128 {
			return fmt.Errorf("resource list is nested too deeply")
		}
		kind, _ := value["kind"].(string)
		if kind == "Secret" || kind == "SecretList" {
			return fmt.Errorf("contains Kubernetes Secret resources; this build cannot be packaged")
		}
		if kind != "List" && !strings.HasSuffix(kind, "List") {
			return nil
		}
		if value["items"] == nil {
			return nil
		}
		items, ok := value["items"].([]any)
		if !ok {
			return fmt.Errorf("cannot inspect resource list")
		}
		for _, entry := range items {
			item, ok := entry.(map[string]any)
			if !ok {
				return fmt.Errorf("cannot inspect resource list item")
			}
			if err := inspect(item, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	for {
		var value map[string]any
		err := decoder.Decode(&value)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("cannot inspect generated Kubernetes YAML")
		}
		if err := inspect(value, 0); err != nil {
			return err
		}
	}
}

func (s *Server) buildPackage(run Run) ([]byte, []PackageFile, error) {
	if run.Context.DecryptSecured {
		return nil, nil, fmt.Errorf("builds with secured-variable decryption enabled cannot be packaged; use external secret references")
	}
	if run.Status != "succeeded" || run.Evidence == nil || len(run.Files) == 0 || len(run.Files) > 4096 {
		return nil, nil, fmt.Errorf("package requires a completed build with recorded output evidence")
	}
	expected := map[string]buildapp.OutputEvidence{}
	for _, file := range run.Evidence.Outputs {
		if _, exists := expected[file.Path]; exists {
			return nil, nil, fmt.Errorf("duplicate output evidence")
		}
		expected[file.Path] = file
	}
	if len(expected) != len(run.Files) {
		return nil, nil, fmt.Errorf("output inventory differs from recorded build")
	}
	output, err := os.OpenRoot(filepath.Join(s.opts.WorkDir, run.ID, "output"))
	if err != nil {
		return nil, nil, err
	}
	defer output.Close()
	names := append([]string(nil), run.Files...)
	sort.Strings(names)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	add := func(name string, raw []byte) error {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(0600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = entry.Write(raw)
		return err
	}
	var inventory []PackageFile
	total := 0
	for i, name := range names {
		if !filepath.IsLocal(name) || strings.ContainsAny(name, "\x00\t\r\n\\") || path.Clean(name) != name || (i > 0 && names[i-1] == name) {
			return nil, nil, fmt.Errorf("invalid package output path")
		}
		// The package is a set of Kubernetes manifests, not an arbitrary directory backup.
		extension := strings.ToLower(path.Ext(name))
		if extension != ".yaml" && extension != ".yml" && extension != ".json" {
			return nil, nil, fmt.Errorf("output %s is not a Kubernetes manifest", name)
		}
		raw, err := readBounded(output, name, 8<<20)
		if err != nil {
			return nil, nil, err
		}
		total += len(raw)
		if total > maxPackageBytes {
			return nil, nil, fmt.Errorf("package output exceeds 64 MiB")
		}
		recorded, found := expected[name]
		if !found || recorded.SHA256 != "sha256:"+packageHash(raw) || recorded.Bytes != len(raw) {
			return nil, nil, fmt.Errorf("output %s differs from recorded build; rebuild before packaging", name)
		}
		if err := checkPackageResource(raw); err != nil {
			return nil, nil, fmt.Errorf("output %s: %w", name, err)
		}
		destination := "manifests/" + name
		inventory = append(inventory, PackageFile{Path: destination, SHA256: recorded.SHA256, Bytes: len(raw)})
		if err := add(destination, raw); err != nil {
			return nil, nil, err
		}
	}
	evidence, err := packageEvidence(*run.Evidence)
	if err != nil {
		return nil, nil, err
	}
	if len(evidence) > 8<<20 {
		return nil, nil, fmt.Errorf("build evidence exceeds 8 MiB")
	}
	manifest := PackageManifest{APIVersion: "kube-build-app/v1", Kind: "DeploymentPackage", ReleaseID: run.ReleaseID, Environment: run.Selection.Environment, BuildID: run.ID, FinishedAt: run.FinishedAt, Builder: run.Builder, ReleaseSHA256: run.Selection.ManifestSHA256, Files: inventory, PrivateVariableInputs: len(run.Evidence.Variables), Evidence: PackageFile{Path: "build-evidence.json", SHA256: "sha256:" + packageHash(evidence), Bytes: len(evidence)}}
	metadata, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, nil, err
	}
	if err := add("build-evidence.json", evidence); err != nil {
		return nil, nil, err
	}
	if err := add("package.yaml", metadata); err != nil {
		return nil, nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, nil, err
	}
	if archive.Len() > maxPackageArchiveBytes {
		return nil, nil, fmt.Errorf("package archive is too large")
	}
	return archive.Bytes(), inventory, nil
}
func (s *Server) packageTarget(run Run) (directory, relative, address string, err error) {
	if s.opts.PackageOutput == nil {
		return "", "", "", nil
	}
	target := s.opts.PackageOutput
	relative, err = expandSourcePath(target.pattern(), map[string]string{"release_id": run.ReleaseID, "environment": run.Selection.Environment, "build_id": run.ID})
	if err != nil || target.Storage == "directory" {
		return target.Directory, relative, "", err
	}
	base, err := url.Parse(target.BaseURL)
	if err != nil {
		return "", "", "", err
	}
	return "", relative, base.JoinPath(relative).String(), nil
}
func (s *Server) reviewPackage(w http.ResponseWriter, r *http.Request) {
	run, ok := s.snapshot(r.PathValue("id"))
	if !ok || run.Status != "succeeded" {
		failure(w, 409, fmt.Errorf("choose a completed build"))
		return
	}
	raw, files, err := s.buildPackage(run)
	if err != nil {
		failure(w, 409, err)
		return
	}
	directory, relative, address, err := s.packageTarget(run)
	if err != nil {
		failure(w, 409, err)
		return
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		failure(w, 500, err)
		return
	}
	review := packageReview{ID: hex.EncodeToString(id[:]), RunID: run.ID, ReleaseID: run.ReleaseID, Environment: run.Selection.Environment, SHA256: packageHash(raw), Bytes: len(raw), Files: files, Directory: directory, Path: relative, URL: address}
	base := filepath.Join(s.opts.WorkDir, run.ID, "packages")
	if err := os.MkdirAll(base, 0700); err != nil {
		failure(w, 500, err)
		return
	}
	base = filepath.Join(base, review.ID)
	if err := os.Mkdir(base, 0700); err != nil {
		failure(w, 500, err)
		return
	}
	stored := false
	defer func() {
		if !stored {
			_ = os.RemoveAll(base)
		}
	}()
	if err := os.WriteFile(filepath.Join(base, "deployment.zip"), raw, 0600); err != nil {
		failure(w, 500, err)
		return
	}
	metadata, err := json.Marshal(review)
	if err != nil {
		failure(w, 500, err)
		return
	}
	if len(metadata) > 2<<20 {
		failure(w, 409, fmt.Errorf("package inventory exceeds review size limit"))
		return
	}
	if err := os.WriteFile(filepath.Join(base, "review.json"), metadata, 0600); err != nil {
		failure(w, 500, err)
		return
	}
	stored = true
	respond(w, 200, review)
}
func (s *Server) savedPackage(run Run, id string) (packageReview, []byte, error) {
	if !validID(id) {
		return packageReview{}, nil, fmt.Errorf("invalid package review")
	}
	root, err := os.OpenRoot(filepath.Join(s.opts.WorkDir, run.ID, "packages"))
	if err != nil {
		return packageReview{}, nil, err
	}
	defer root.Close()
	metadata, err := readBounded(root, id+"/review.json", 2<<20)
	if err != nil {
		return packageReview{}, nil, err
	}
	var review packageReview
	if json.Unmarshal(metadata, &review) != nil || review.ID != id || review.RunID != run.ID || review.ReleaseID != run.ReleaseID || review.Environment != run.Selection.Environment {
		return review, nil, fmt.Errorf("package review identity mismatch")
	}
	raw, err := readBounded(root, id+"/deployment.zip", maxPackageArchiveBytes)
	if err != nil {
		return review, nil, err
	}
	if len(raw) != review.Bytes || packageHash(raw) != review.SHA256 {
		return review, nil, fmt.Errorf("stored package differs from reviewed content")
	}
	return review, raw, nil
}
func (s *Server) packageArchive(w http.ResponseWriter, r *http.Request) {
	run, ok := s.snapshot(r.PathValue("id"))
	if !ok || run.Status != "succeeded" {
		http.NotFound(w, r)
		return
	}
	review, raw, err := s.savedPackage(run, r.PathValue("review"))
	if err != nil {
		failure(w, 409, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="deployment-`+review.RunID+`.zip"`)
	w.Header().Set("X-Checksum-Sha256", review.SHA256)
	http.ServeContent(w, r, "deployment.zip", run.FinishedAt, bytes.NewReader(raw))
}

var packageExportLocks = pathlock.NewLockManager()

func (s *Server) publishPackage(w http.ResponseWriter, r *http.Request) {
	run, ok := s.snapshot(r.PathValue("id"))
	if !ok || run.Status != "succeeded" {
		failure(w, 409, fmt.Errorf("choose a completed build"))
		return
	}
	review, raw, err := s.savedPackage(run, r.PathValue("review"))
	if err != nil {
		failure(w, 409, err)
		return
	}
	directory, relative, address, err := s.packageTarget(run)
	if err != nil || (directory == "" && address == "") || directory != review.Directory || relative != review.Path || address != review.URL {
		failure(w, 409, fmt.Errorf("package destination changed or is not configured; review again"))
		return
	}
	if address != "" {
		if err := publishPackageToArtifactory(r.Context(), *s.opts.PackageOutput, address, review.SHA256, raw); err != nil {
			failure(w, 409, err)
			return
		}
		respond(w, 200, map[string]string{"status": "published", "url": address, "sha256": review.SHA256})
		return
	}
	resolved, err := resolvedDirectory(directory)
	if err != nil || resolved != directory {
		failure(w, 409, fmt.Errorf("package destination changed on disk; check output configuration"))
		return
	}
	unlock, err := packageExportLocks.TryAcquire(filepath.Join(directory, relative), "package export")
	if err != nil {
		failure(w, 409, fmt.Errorf("package destination is busy"))
		return
	}
	defer unlock()
	if err := exportPackage(directory, relative, raw); err != nil {
		failure(w, 409, err)
		return
	}
	respond(w, 200, map[string]string{"status": "saved", "directory": directory, "path": relative, "sha256": review.SHA256})
}
func exportPackage(directory, relative string, raw []byte) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	verify := func() (bool, error) {
		info, err := root.Lstat(relative)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("package destination must be a regular file")
		}
		content, err := readBounded(root, relative, maxPackageArchiveBytes)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !bytes.Equal(content, raw) {
			return false, fmt.Errorf("destination contains different content; refusing to overwrite")
		}
		return true, nil
	}
	if exists, err := verify(); err != nil || exists {
		return err
	}
	if err := root.MkdirAll(filepath.FromSlash(path.Dir(relative)), 0700); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	temporary := relative + ".part-" + hex.EncodeToString(random[:])
	file, err := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Link installs the complete file without replacing an existing name, also across processes.
	if err := root.Link(temporary, relative); err != nil && !os.IsExist(err) {
		return err
	}
	exists, err := verify()
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("package was not saved")
	}
	return nil
}
