package buildweb

import (
	"archive/zip"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"kube-env/internal/appinfo"
	"kube-env/internal/buildapp"
)

//go:embed static/*
var assets embed.FS

type Options struct {
	Root, ReleasesRoot, WorkDir string
	DeploymentsRoot             string
	ReleaseCAFile               string
	ArtifactSource              *ArtifactSource
	Build                       buildapp.Options
}

type Release struct {
	Path   string `json:"path"`
	ID     string `json:"id"`
	Images int    `json:"images"`
	SHA256 string `json:"sha256"`
}

type selection struct {
	RemoteRelease  string `json:"remote_release,omitempty"`
	Environment    string `json:"environment"`
	Manifest       string `json:"manifest"`
	ManifestSHA256 string `json:"manifest_sha256"`
}

type Run struct {
	ReleaseSource   *buildapp.URLOrigin           `json:"release_source,omitempty"`
	Builder         appinfo.Info                  `json:"builder"`
	Context         buildapp.BuildContext         `json:"context"`
	EnvironmentRoot string                        `json:"environment_root"`
	ReleasesRoot    string                        `json:"releases_root"`
	ID              string                        `json:"id"`
	Status          string                        `json:"status"`
	Selection       selection                     `json:"selection"`
	ReleaseID       string                        `json:"release_id"`
	CreatedAt       time.Time                     `json:"created_at"`
	FinishedAt      time.Time                     `json:"finished_at,omitempty"`
	Error           string                        `json:"error,omitempty"`
	Events          []buildapp.BuildEvent         `json:"events"`
	Summary         *buildapp.ResourceSummaryData `json:"summary,omitempty"`
	Evidence        *buildapp.BuildEvidence       `json:"evidence,omitempty"`
	Files           []string                      `json:"files"`
}

type Server struct {
	opts          Options
	info          appinfo.Info
	mu            sync.Mutex
	runs          map[string]*Run
	running       bool
	publicationMu sync.Mutex
}

func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func New(opts Options, info appinfo.Info) (*Server, error) {
	if opts.ArtifactSource != nil {
		copy := *opts.ArtifactSource
		if err := copy.Validate(); err != nil {
			return nil, err
		}
		opts.ArtifactSource = &copy
	}
	if strings.TrimSpace(opts.Root) == "" || strings.TrimSpace(opts.ReleasesRoot) == "" || strings.TrimSpace(opts.WorkDir) == "" {
		return nil, fmt.Errorf("environment, release and work directories are required")
	}
	if _, err := buildapp.DescribeBuildContext(opts.Build); err != nil {
		return nil, err
	}
	for _, path := range []*string{&opts.Root, &opts.ReleasesRoot} {
		absolute, err := filepath.Abs(*path)
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		stat, err := os.Stat(resolved)
		if err != nil {
			return nil, err
		}
		if !stat.IsDir() {
			return nil, fmt.Errorf("input root must be a directory: %s", resolved)
		}
		*path = resolved
	}
	absolute, err := filepath.Abs(opts.WorkDir)
	if err != nil {
		return nil, err
	}
	if within(opts.Root, absolute) || within(opts.ReleasesRoot, absolute) {
		return nil, fmt.Errorf("work directory must be outside environment and release repositories")
	}
	if err := os.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	opts.WorkDir, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	if within(opts.Root, opts.WorkDir) || within(opts.ReleasesRoot, opts.WorkDir) {
		return nil, fmt.Errorf("work directory must be outside environment and release repositories")
	}
	s := &Server{opts: opts, info: info, runs: map[string]*Run{}}
	if err := s.configurePublication(); err != nil {
		return nil, err
	}
	stateRoot, err := os.OpenRoot(opts.WorkDir)
	if err != nil {
		return nil, err
	}
	defer stateRoot.Close()
	entries, err := os.ReadDir(opts.WorkDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validID(entry.Name()) {
			continue
		}
		raw, err := readBounded(stateRoot, filepath.ToSlash(filepath.Join(entry.Name(), "build.json")), 8<<20)
		if err != nil {
			return nil, err
		}
		var run Run
		if err := json.Unmarshal(raw, &run); err != nil {
			return nil, err
		}
		if run.ID != entry.Name() {
			return nil, fmt.Errorf("build directory identity mismatch")
		}
		if run.EnvironmentRoot != opts.Root {
			return nil, fmt.Errorf("work directory contains builds for another environment repository")
		}
		if run.Status == "running" {
			run.Status = "interrupted"
			run.Error = "Server stopped during the build. Start a new build to retry."
			if err := s.save(&run); err != nil {
				return nil, err
			}
		}
		s.runs[run.ID] = &run
	}
	return s, nil
}

func validID(id string) bool {
	value, err := hex.DecodeString(id)
	return err == nil && len(value) == 16
}

func readBounded(root *os.Root, name string, limit int64) ([]byte, error) {
	file, err := root.Open(filepath.FromSlash(name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("file exceeds size limit")
	}
	return raw, nil
}

func (s *Server) catalog() ([]string, []Release, []string, error) {
	root, err := os.OpenRoot(s.opts.Root)
	if err != nil {
		return nil, nil, nil, err
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return nil, nil, nil, err
	}
	var environments []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if stat, err := root.Stat(filepath.Join(entry.Name(), "apps")); err == nil && stat.IsDir() {
			environments = append(environments, entry.Name())
		}
	}
	releasesRoot, err := os.OpenRoot(s.opts.ReleasesRoot)
	if err != nil {
		return nil, nil, nil, err
	}
	defer releasesRoot.Close()
	var releases []Release
	var warnings []string
	for _, pattern := range []string{"releases/*.yaml", "releases/*.yml", "distributions/*/*.yaml", "distributions/*/*.yml"} {
		paths, err := fs.Glob(releasesRoot.FS(), pattern)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, path := range paths {
			raw, err := readBounded(releasesRoot, path, 8<<20)
			if err != nil {
				warnings = append(warnings, path+": "+err.Error())
				continue
			}
			id, count, err := buildapp.ReleaseMetadata(raw)
			if err != nil || id == "" {
				warnings = append(warnings, path+": invalid image release")
				continue
			}
			releases = append(releases, Release{Path: path, ID: id, Images: count, SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(raw))})
		}
	}
	sort.Slice(releases, func(i, j int) bool { return releases[i].Path < releases[j].Path })
	return environments, releases, warnings, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	files, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(files))))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := assets.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(raw)
	})
	mux.HandleFunc("GET /api/catalog", s.handleCatalog)
	mux.Handle("POST /api/releases/configured", http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.readConfiguredRelease)))
	mux.Handle("POST /api/releases/url", http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.readURLRelease)))
	mux.HandleFunc("POST /api/builds", s.start)
	mux.HandleFunc("GET /api/builds/{id}", s.status)
	mux.HandleFunc("GET /api/builds/{id}/file/{path...}", s.file)
	mux.HandleFunc("GET /api/builds/{id}/archive", s.archive)
	mux.HandleFunc("POST /api/builds/{id}/publication-review", s.reviewPublication)
	mux.HandleFunc("POST /api/builds/{id}/publish/{review}", s.publish)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodPost && r.Header.Get("Origin") != "" {
			origin, err := url.Parse(r.Header.Get("Origin"))
			if err != nil || origin.Host != r.Host {
				http.Error(w, "Cross-origin build request rejected", http.StatusForbidden)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func failure(w http.ResponseWriter, status int, err error) {
	respond(w, status, map[string]string{"error": err.Error()})
}

func (s *Server) handleCatalog(w http.ResponseWriter, r *http.Request) {
	environments, releases, warnings, err := s.catalog()
	if err != nil {
		failure(w, 500, err)
		return
	}
	s.mu.Lock()
	history := make([]Run, 0, len(s.runs))
	for _, run := range s.runs {
		history = append(history, Run{ID: run.ID, Status: run.Status, Selection: run.Selection, ReleaseID: run.ReleaseID, CreatedAt: run.CreatedAt})
	}
	s.mu.Unlock()
	sort.Slice(history, func(i, j int) bool { return history[i].CreatedAt.After(history[j].CreatedAt) })
	if len(history) > 20 {
		history = history[:20]
	}
	context, _ := buildapp.DescribeBuildContext(s.opts.Build)
	respond(w, 200, map[string]any{"environments": environments, "releases": releases, "warnings": warnings, "history": history, "version": s.info.Version, "root": s.opts.Root, "resource_policy_root": s.opts.Build.ResourcePolicyRoot, "context": context, "deployments_root": s.opts.DeploymentsRoot, "artifact_source": s.configuredSourceView()})
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var chosen selection
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&chosen); err != nil {
		failure(w, 400, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		failure(w, 400, fmt.Errorf("expected one build selection"))
		return
	}
	environments, releases, _, err := s.catalog()
	if err != nil {
		failure(w, 500, err)
		return
	}
	found := false
	for _, env := range environments {
		if env == chosen.Environment {
			found = true
			break
		}
	}
	if !found {
		failure(w, 400, fmt.Errorf("choose an existing environment"))
		return
	}
	var selected *Release
	var raw []byte
	var source *buildapp.URLOrigin
	if chosen.RemoteRelease != "" {
		if chosen.Manifest != "" {
			failure(w, 400, fmt.Errorf("choose a local manifest or a downloaded release, not both"))
			return
		}
		remote, content, err := s.remoteRelease(chosen.RemoteRelease)
		if err != nil {
			failure(w, 409, err)
			return
		}
		selected, raw, source = &remote.Release, content, &remote.Origin
		if selected.SHA256 != chosen.ManifestSHA256 {
			failure(w, 409, fmt.Errorf("downloaded release selection changed"))
			return
		}
	} else {
		for i := range releases {
			if releases[i].Path == chosen.Manifest {
				selected = &releases[i]
				break
			}
		}
		if selected == nil {
			failure(w, 400, fmt.Errorf("choose an existing release manifest"))
			return
		}
		if selected.SHA256 != chosen.ManifestSHA256 {
			failure(w, 409, fmt.Errorf("release changed; refresh the selection"))
			return
		}
		root, err := os.OpenRoot(s.opts.ReleasesRoot)
		if err != nil {
			failure(w, 500, err)
			return
		}
		raw, err = readBounded(root, chosen.Manifest, 8<<20)
		root.Close()
		if err != nil {
			failure(w, 400, err)
			return
		}
		if fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) != chosen.ManifestSHA256 {
			failure(w, 409, fmt.Errorf("release changed; refresh the selection"))
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		failure(w, 409, fmt.Errorf("a build is already running"))
		return
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		failure(w, 500, err)
		return
	}
	id := hex.EncodeToString(random[:])
	directory := filepath.Join(s.opts.WorkDir, id)
	if err := os.Mkdir(directory, 0700); err != nil {
		failure(w, 500, err)
		return
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = os.RemoveAll(directory)
		}
	}()
	if err := os.WriteFile(filepath.Join(directory, "release.yaml"), raw, 0600); err != nil {
		failure(w, 500, err)
		return
	}
	run := &Run{ReleaseSource: source, Builder: s.info, EnvironmentRoot: s.opts.Root, ReleasesRoot: s.opts.ReleasesRoot, ID: id, Status: "running", Selection: chosen, ReleaseID: selected.ID, CreatedAt: time.Now().UTC()}
	run.Context, err = buildapp.DescribeBuildContext(s.buildOptions(run))
	if err != nil {
		failure(w, 400, err)
		return
	}
	if err := s.save(run); err != nil {
		failure(w, 500, err)
		return
	}
	accepted = true
	s.runs[id] = run
	s.running = true
	go s.build(run)
	respond(w, http.StatusAccepted, run)
}

func (s *Server) save(run *Run) error {
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.opts.WorkDir, run.ID, "build.json")
	if err := os.WriteFile(path+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

func (s *Server) buildOptions(run *Run) buildapp.Options {
	directory := filepath.Join(s.opts.WorkDir, run.ID)
	opts := s.opts.Build
	opts.Root, opts.Environment, opts.Target = s.opts.Root, run.Selection.Environment, filepath.Join(directory, "output")
	opts.ReleaseManifest = ""
	opts.ReleaseManifests = []string{filepath.Join(directory, "release.yaml")}
	opts.InputOrigins = map[string]string{opts.ReleaseManifests[0]: filepath.Join(s.opts.ReleasesRoot, run.Selection.Manifest)}
	if run.ReleaseSource != nil {
		opts.InputOrigins = nil
		opts.InputURLOrigins = map[string]buildapp.URLOrigin{opts.ReleaseManifests[0]: *run.ReleaseSource}
	}
	opts.ReleaseID = run.ReleaseID
	opts.EvidenceDir = filepath.Join(directory, "inputs")
	return opts
}

func (s *Server) build(run *Run) {
	opts := s.buildOptions(run)
	result, err := buildapp.BuildWithObserver(opts, func(event buildapp.BuildEvent) {
		if event.Path != "" {
			event.Path, _ = filepath.Rel(opts.Target, event.Path)
			event.Path = filepath.ToSlash(event.Path)
		}
		s.mu.Lock()
		run.Events = append(run.Events, event)
		s.mu.Unlock()
	})
	var files []string
	if err == nil {
		err = filepath.WalkDir(opts.Target, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type().IsRegular() {
				rel, err := filepath.Rel(opts.Target, path)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	run.FinishedAt = time.Now().UTC()
	run.Evidence = result.Evidence
	if err != nil {
		run.Status = "failed"
		run.Error = err.Error()
	} else {
		run.Status = "succeeded"
		run.Files = files
		run.Summary = &result.Summary
	}
	if err := s.save(run); err != nil {
		run.Status = "failed"
		run.Error = "Cannot save build result: " + err.Error()
	}
}

func (s *Server) snapshot(id string) (Run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return Run{}, false
	}
	copy := *run
	copy.Events = append([]buildapp.BuildEvent(nil), run.Events...)
	copy.Files = append([]string(nil), run.Files...)
	return copy, true
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	run, ok := s.snapshot(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	respond(w, 200, run)
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	run, ok := s.snapshot(r.PathValue("id"))
	if !ok || run.Status != "succeeded" {
		http.NotFound(w, r)
		return
	}
	name := r.PathValue("path")
	allowed := false
	for _, file := range run.Files {
		if file == name {
			allowed = true
			break
		}
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(filepath.Join(s.opts.WorkDir, run.ID, "output"))
	if err != nil {
		failure(w, 500, err)
		return
	}
	defer root.Close()
	raw, err := readBounded(root, name, 1<<20)
	if err != nil {
		failure(w, 400, err)
		return
	}
	if !utf8.Valid(raw) {
		failure(w, 400, fmt.Errorf("binary files are available in the ZIP download"))
		return
	}
	respond(w, 200, map[string]string{"path": name, "content": string(raw)})
}

func (s *Server) archive(w http.ResponseWriter, r *http.Request) {
	run, ok := s.snapshot(r.PathValue("id"))
	if !ok || run.Status != "succeeded" {
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(filepath.Join(s.opts.WorkDir, run.ID, "output"))
	if err != nil {
		failure(w, 500, err)
		return
	}
	defer root.Close()
	temporary, err := os.CreateTemp(filepath.Join(s.opts.WorkDir, run.ID), "download-*.zip")
	if err != nil {
		failure(w, 500, err)
		return
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	writer := zip.NewWriter(temporary)
	for _, name := range run.Files {
		file, err := root.Open(filepath.FromSlash(name))
		if err != nil {
			failure(w, 500, err)
			return
		}
		entry, err := writer.Create(name)
		if err == nil {
			_, err = io.Copy(entry, file)
		}
		file.Close()
		if err != nil {
			failure(w, 500, err)
			return
		}
	}
	if err := writer.Close(); err != nil {
		failure(w, 500, err)
		return
	}
	if _, err := temporary.Seek(0, io.SeekStart); err != nil {
		failure(w, 500, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="build-`+run.ID+`.zip"`)
	http.ServeContent(w, r, "build-"+run.ID+".zip", run.FinishedAt, temporary)
}
