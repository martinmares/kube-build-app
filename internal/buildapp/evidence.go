package buildapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"kube-env/internal/appinfo"
)

// BuildEvidence is safe metadata. Input contents and resolved variable values
// stay in private snapshots; they are not part of the JSON publication record.
type BuildEvidence struct {
	Builder          appinfo.Info         `json:"builder"`
	ExecutableSHA256 string               `json:"executable_sha256"`
	Version          int                  `json:"version"`
	Context          BuildContext         `json:"context"`
	Settings         RenderSettings       `json:"settings"`
	Repositories     []RepositoryEvidence `json:"repositories"`
	Files            []InputEvidence      `json:"files"`
	Variables        []PrivateInput       `json:"variables"`
	Outputs          []OutputEvidence     `json:"outputs"`
}

type RenderSettings struct {
	ImageOverrides                 []string `json:"image_overrides,omitempty"`
	ForceImageTag                  string   `json:"force_image_tag,omitempty"`
	ForceImagePrefix               string   `json:"force_image_prefix,omitempty"`
	Down                           []string `json:"down,omitempty"`
	ReleaseContextName             string   `json:"release_context_name,omitempty"`
	ReleaseIDEnvName               string   `json:"release_id_env_name,omitempty"`
	DisableReleaseContextConfigMap bool     `json:"disable_release_context_configmap"`
	DisableReleaseContextEnv       bool     `json:"disable_release_context_env"`
}

type RepositoryEvidence struct {
	Root               string `json:"root"`
	Commit             string `json:"commit"`
	CommitAfter        string `json:"commit_after"`
	ChangedDuringBuild bool   `json:"changed_during_build"`
}

type InputEvidence struct {
	URLOrigin    *URLOrigin `json:"url_origin,omitempty"`
	Path         string     `json:"path"`
	ReadPath     string     `json:"read_path,omitempty"`
	ResolvedPath string     `json:"resolved_path,omitempty"`
	SHA256       string     `json:"sha256,omitempty"`
	Bytes        int        `json:"bytes"`
	Missing      bool       `json:"missing"`
	Snapshot     string     `json:"snapshot,omitempty"`
	GitRoot      string     `json:"git_root,omitempty"`
	GitPath      string     `json:"git_path,omitempty"`
	LocalState   string     `json:"local_state,omitempty"`
}

type PrivateInput struct {
	Kind     string `json:"kind"`
	SHA256   string `json:"sha256"`
	Bytes    int    `json:"bytes"`
	Snapshot string `json:"snapshot"`
}

type OutputEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type capturedInput struct {
	evidence InputEvidence
	raw      []byte
	err      error
}

type inputRecorder struct {
	directory        string
	files            map[string]capturedInput
	repositories     map[string]*RepositoryEvidence
	directories      map[string]string
	private          []PrivateInput
	origins          map[string]string
	urlOrigins       map[string]URLOrigin
	executableSHA256 string
}

// BuildWithObserver reports render events. When EvidenceDir is set, repeated
// reads use the first captured bytes, so evidence and rendering cannot diverge.
func BuildWithObserver(opts Options, observe func(BuildEvent)) (Result, error) {
	if opts.EvidenceDir == "" {
		return build(opts, observe)
	}
	if opts.Root == "" {
		opts.Root = "environments"
	}
	if opts.Target == "" {
		opts.Target = filepath.Join(opts.Root, opts.Environment, "target")
	}
	recorder, err := newInputRecorder(opts)
	if err != nil {
		return Result{}, err
	}
	opts.inputs = recorder
	result, buildErr := build(opts, observe)
	evidence, evidenceErr := recorder.finish(opts, result, buildErr == nil)
	result.Evidence = &evidence
	return result, errors.Join(buildErr, evidenceErr)
}

func newInputRecorder(opts Options) (*inputRecorder, error) {
	directory, err := canonicalInputPath(opts.EvidenceDir)
	if err != nil {
		return nil, err
	}
	for _, root := range []string{opts.Root, opts.ResourcePolicyRoot, opts.Target} {
		if root == "" {
			continue
		}
		root, err := canonicalInputPath(root)
		if err != nil {
			return nil, err
		}
		inside, err := filepath.Rel(root, directory)
		outside, reverseErr := filepath.Rel(directory, root)
		if (err == nil && (inside == "." || filepath.IsLocal(inside))) || (reverseErr == nil && (outside == "." || filepath.IsLocal(outside))) {
			return nil, fmt.Errorf("evidence directory must be outside input and output directories")
		}
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, fmt.Errorf("create new evidence directory: %w", err)
	}
	if err := os.Mkdir(filepath.Join(directory, "files"), 0700); err != nil {
		return nil, err
	}
	origins := map[string]string{}
	urlOrigins := map[string]URLOrigin{}
	for staged, origin := range opts.InputURLOrigins {
		staged, err := inputPath(staged)
		if err != nil {
			return nil, err
		}
		if err := origin.Validate(); err != nil {
			return nil, err
		}
		urlOrigins[staged] = origin
	}
	for staged, source := range opts.InputOrigins {
		staged, err := inputPath(staged)
		if err != nil {
			return nil, err
		}
		if _, exists := urlOrigins[staged]; exists {
			return nil, fmt.Errorf("input cannot have both local and URL origins")
		}
		source, err := inputPath(source)
		if err != nil {
			return nil, err
		}
		origins[staged] = source
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(executable)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return nil, err
	}
	return &inputRecorder{directory: directory, files: map[string]capturedInput{}, repositories: map[string]*RepositoryEvidence{}, directories: map[string]string{}, origins: origins, urlOrigins: urlOrigins, executableSHA256: fmt.Sprintf("sha256:%x", hash.Sum(nil))}, nil
}

func inputPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(absolute)); err == nil {
		absolute = filepath.Join(parent, filepath.Base(absolute))
	}
	return absolute, nil
}

// Resolve existing ancestors as well, so a not-yet-created destination cannot
// bypass input/output separation through a symlink in its parent path.
func canonicalInputPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(absolute)
		if parent == absolute {
			return "", err
		}
		tail = append(tail, filepath.Base(absolute))
		absolute = parent
	}
}

func inputHash(raw []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) }

func (r *inputRecorder) snapshot(raw []byte) (string, error) {
	path := "files/" + strings.TrimPrefix(inputHash(raw), "sha256:")
	return path, os.WriteFile(filepath.Join(r.directory, path), raw, 0600)
}

func evidenceGit(directory string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", directory}, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	return command.Output()
}

func (r *inputRecorder) repository(path string) (string, error) {
	directory := filepath.Dir(path)
	if root, known := r.directories[directory]; known {
		return root, nil
	}
	raw, err := evidenceGit(directory, "rev-parse", "--show-toplevel")
	if err != nil {
		// Files outside Git are valid inputs, identified by their captured bytes.
		r.directories[directory] = ""
		return "", nil
	}
	root := strings.TrimSpace(string(raw))
	if r.repositories[root] == nil {
		head, _ := evidenceGit(root, "rev-parse", "--verify", "--quiet", "HEAD")
		r.repositories[root] = &RepositoryEvidence{Root: root, Commit: strings.TrimSpace(string(head))}
	}
	r.directories[directory] = root
	return root, nil
}

func (r *inputRecorder) read(path string) ([]byte, error) {
	absolute, err := inputPath(path)
	if err != nil {
		return nil, err
	}
	if cached, found := r.files[absolute]; found {
		return bytes.Clone(cached.raw), cached.err
	}
	entry := capturedInput{evidence: InputEvidence{Path: absolute}}
	if source := r.origins[absolute]; source != "" && source != absolute {
		entry.evidence.Path = source
		entry.evidence.ReadPath = absolute
	}
	if origin, exists := r.urlOrigins[absolute]; exists {
		entry.evidence.URLOrigin = &origin
		entry.evidence.LocalState = "downloaded snapshot"
	} else {
		entry.evidence.GitRoot, err = r.repository(entry.evidence.Path)
		if err != nil {
			return nil, err
		}
	}
	entry.raw, entry.err = os.ReadFile(absolute)
	entry.evidence.Missing = os.IsNotExist(entry.err)
	if entry.err != nil && !entry.evidence.Missing {
		return nil, entry.err
	}
	if entry.err == nil {
		if entry.evidence.URLOrigin != nil {
			if err := entry.evidence.URLOrigin.Verify(entry.raw); err != nil {
				return nil, err
			}
		}
		entry.evidence.SHA256 = inputHash(entry.raw)
		entry.evidence.Bytes = len(entry.raw)
		entry.evidence.Snapshot, err = r.snapshot(entry.raw)
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, err
		}
		if resolved != absolute {
			entry.evidence.ResolvedPath = resolved
		}
	}
	if root := entry.evidence.GitRoot; root != "" {
		relative, err := filepath.Rel(root, entry.evidence.Path)
		if err != nil {
			return nil, err
		}
		entry.evidence.GitPath = filepath.ToSlash(relative)
		commit := r.repositories[root].Commit
		var tracked []byte
		if commit != "" {
			tracked, err = evidenceGit(root, "ls-tree", "--name-only", commit, "--", entry.evidence.GitPath)
			if err != nil {
				return nil, err
			}
		}
		switch {
		case len(tracked) == 0 && entry.evidence.Missing:
			entry.evidence.LocalState = "absent"
		case len(tracked) == 0:
			entry.evidence.LocalState = "untracked"
		case entry.evidence.Missing:
			entry.evidence.LocalState = "deleted"
		default:
			baseline, err := evidenceGit(root, "show", commit+":"+entry.evidence.GitPath)
			if err != nil {
				return nil, err
			}
			entry.evidence.LocalState = "matches-commit"
			if !bytes.Equal(baseline, entry.raw) {
				entry.evidence.LocalState = "modified"
			}
		}
	}
	r.files[absolute] = entry
	return bytes.Clone(entry.raw), entry.err
}

func (opts Options) readInput(path string) ([]byte, error) {
	if opts.inputs == nil {
		return os.ReadFile(path)
	}
	return opts.inputs.read(path)
}

func (opts Options) inputFileExists(path string) bool {
	if opts.inputs == nil {
		return isFile(path)
	}
	_, err := opts.inputs.read(path)
	return !os.IsNotExist(err)
}

func (r *inputRecorder) capturePrivate(kind string, raw []byte) error {
	path, err := r.snapshot(raw)
	if err != nil {
		return err
	}
	r.private = append(r.private, PrivateInput{Kind: kind, SHA256: inputHash(raw), Bytes: len(raw), Snapshot: path})
	return nil
}

// Encjson reads a sibling schema, too. Preserve the filename and schema beside
// the captured ciphertext so subprocess reads cannot escape the captured inputs.
func (r *inputRecorder) decryptionInput(path string) (string, func(), error) {
	raw, err := r.read(path)
	if err != nil {
		return "", nil, err
	}
	name := filepath.Base(path)
	schemaName := name + ".schema.json"
	if filepath.Ext(name) == ".json" {
		schemaName = nameWithoutLastExt(name) + ".schema.json"
	}
	schema, schemaErr := r.read(filepath.Join(filepath.Dir(path), schemaName))
	if schemaErr != nil && !os.IsNotExist(schemaErr) {
		return "", nil, schemaErr
	}
	directory, err := os.MkdirTemp(r.directory, "decrypt-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	input := filepath.Join(directory, name)
	if err := os.WriteFile(input, raw, 0600); err != nil {
		cleanup()
		return "", nil, err
	}
	if schemaErr == nil {
		if err := os.WriteFile(filepath.Join(directory, schemaName), schema, 0600); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return input, cleanup, nil
}

func (r *inputRecorder) finish(opts Options, result Result, succeeded bool) (BuildEvidence, error) {
	context, err := DescribeBuildContext(opts)
	if err != nil {
		return BuildEvidence{}, err
	}
	evidence := BuildEvidence{Version: 1, Builder: appinfo.For(appinfo.BuildAppName), Context: context, Variables: r.private, Settings: RenderSettings{
		ImageOverrides: opts.ImageOverrides, ForceImageTag: opts.ForceImageTag, ForceImagePrefix: opts.ForceImagePrefix,
		Down: opts.Down, ReleaseContextName: opts.ReleaseContextName, ReleaseIDEnvName: opts.ReleaseIDEnvName,
		DisableReleaseContextConfigMap: opts.DisableReleaseContextConfigMap, DisableReleaseContextEnv: opts.DisableReleaseContextEnv,
	}}
	evidence.ExecutableSHA256 = r.executableSHA256
	for _, entry := range r.files {
		evidence.Files = append(evidence.Files, entry.evidence)
	}
	sort.Slice(evidence.Files, func(i, j int) bool { return evidence.Files[i].Path < evidence.Files[j].Path })
	for _, repository := range r.repositories {
		head, err := evidenceGit(repository.Root, "rev-parse", "--verify", "--quiet", "HEAD")
		if err != nil && repository.Commit != "" {
			return evidence, err
		}
		repository.CommitAfter = strings.TrimSpace(string(head))
		repository.ChangedDuringBuild = repository.Commit != repository.CommitAfter
		evidence.Repositories = append(evidence.Repositories, *repository)
	}
	sort.Slice(evidence.Repositories, func(i, j int) bool { return evidence.Repositories[i].Root < evidence.Repositories[j].Root })
	if succeeded {
		paths := map[string]bool{}
		for _, group := range [][]string{result.Deployments, result.Services, result.Assets, result.Budgets, result.Autoscaling, result.Externals, result.ServiceAccounts} {
			for _, path := range group {
				paths[path] = true
			}
		}
		for path := range paths {
			raw, err := os.ReadFile(path)
			if err != nil {
				return evidence, err
			}
			rel, err := filepath.Rel(opts.Target, path)
			if err != nil {
				return evidence, err
			}
			evidence.Outputs = append(evidence.Outputs, OutputEvidence{Path: filepath.ToSlash(rel), SHA256: inputHash(raw), Bytes: len(raw)})
		}
		sort.Slice(evidence.Outputs, func(i, j int) bool { return evidence.Outputs[i].Path < evidence.Outputs[j].Path })
	}
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return evidence, err
	}
	return evidence, os.WriteFile(filepath.Join(r.directory, "evidence.json"), append(raw, '\n'), 0600)
}
