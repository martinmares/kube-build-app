package buildweb

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A review identifies an immutable Git tree. Publication never rebuilds it from
// mutable output files, and never uses the checkout's index for staging.
type publicationReview struct {
	ID          string `json:"id"`
	RunID       string `json:"run_id"`
	Repository  string `json:"repository"`
	Branch      string `json:"branch"`
	Base        string `json:"base"`
	Tree        string `json:"tree"`
	Tag         string `json:"tag"`
	Destination string `json:"destination"`
	Summary     string `json:"summary"`
	Diff        string `json:"diff"`
}

func gitCommand(repo, index string, input []byte, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-C", repo, "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	// Do not inherit a caller's index, worktree or object-directory overrides.
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			command.Env = append(command.Env, entry)
		}
	}
	if index != "" {
		command.Env = append(command.Env, "GIT_INDEX_FILE="+index)
	}
	command.Env = append(command.Env, "GIT_TERMINAL_PROMPT=0")
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(output), nil
}

func (s *Server) configurePublication() error {
	if s.opts.DeploymentsRoot == "" {
		return nil
	}
	root, err := filepath.Abs(s.opts.DeploymentsRoot)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	inputs := []string{s.opts.Root, s.opts.ReleasesRoot, s.opts.WorkDir}
	if s.opts.ArtifactSource != nil && s.opts.ArtifactSource.Storage == "git" && s.opts.ArtifactSource.Remote == nil {
		inputs = append(inputs, s.opts.ArtifactSource.Repository)
	}
	for _, other := range inputs {
		if within(root, other) || within(other, root) {
			return fmt.Errorf("deployment checkout must be separate from input and work directories")
		}
	}
	top, err := gitCommand(root, "", nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if strings.TrimSpace(top) != root {
		return fmt.Errorf("deployments-root must be the Git checkout root")
	}
	s.opts.DeploymentsRoot = root
	return nil
}

func checkoutState(repo string) (branch, head string, err error) {
	status, err := gitCommand(repo, "", nil, "status", "--porcelain", "--untracked-files=all", "--ignored")
	if err != nil {
		return "", "", err
	}
	if status != "" {
		return "", "", fmt.Errorf("deployment checkout must be clean, including untracked and ignored files")
	}
	branch, err = gitCommand(repo, "", nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", "", fmt.Errorf("deployment checkout needs a named branch: %w", err)
	}
	head, err = gitCommand(repo, "", nil, "rev-parse", "HEAD")
	return strings.TrimSpace(branch), strings.TrimSpace(head), err
}

func (s *Server) reviewPublication(w http.ResponseWriter, r *http.Request) {
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	run, ok := s.snapshot(r.PathValue("id"))
	if !ok || run.Status != "succeeded" {
		failure(w, 409, fmt.Errorf("choose a completed build"))
		return
	}
	plan, err := s.preparePublication(run)
	if err != nil {
		failure(w, 409, err)
		return
	}
	respond(w, 200, plan)
}

func (s *Server) preparePublication(run Run) (publicationReview, error) {
	plan := publicationReview{RunID: run.ID, Repository: s.opts.DeploymentsRoot}
	if plan.Repository == "" {
		return plan, fmt.Errorf("Git publication is not configured; start with --deployments-root")
	}
	var err error
	plan.Branch, plan.Base, err = checkoutState(plan.Repository)
	if err != nil {
		return plan, err
	}
	if !filepath.IsLocal(run.ReleaseID) || strings.Contains(run.ReleaseID, "\\") {
		return plan, fmt.Errorf("release ID cannot form a deployment revision path")
	}
	prefix := run.Selection.Environment + "/" + run.ReleaseID + ".r"
	tags, err := gitCommand(plan.Repository, "", nil, "tag", "--list")
	if err != nil {
		return plan, err
	}
	revision := 1
	for _, tag := range strings.Split(tags, "\n") {
		if !strings.HasPrefix(tag, prefix) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(tag, prefix))
		if err == nil && n >= revision {
			if n == int(^uint(0)>>1) {
				return plan, fmt.Errorf("deployment revision counter exhausted")
			}
			revision = n + 1
		}
	}
	plan.Tag = prefix + strconv.Itoa(revision)
	if _, err := gitCommand(plan.Repository, "", nil, "check-ref-format", "refs/tags/"+plan.Tag); err != nil {
		return plan, err
	}
	plan.Destination = "deploy/" + run.Selection.Environment
	receiptPath := "revisions/" + plan.Tag + ".json"
	existing, err := gitCommand(plan.Repository, "", nil, "ls-tree", "--name-only", plan.Base, "--", receiptPath)
	if err != nil {
		return plan, err
	}
	if existing != "" {
		return plan, fmt.Errorf("revision evidence already exists; refusing to overwrite %s", receiptPath)
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return plan, err
	}
	plan.ID = hex.EncodeToString(id)
	// A private index changes only Git objects, never the deployment worktree,
	// branch, tag or user's staged changes during review.
	index := filepath.Join(s.opts.WorkDir, run.ID, "index-"+plan.ID)
	defer os.Remove(index)
	defer os.Remove(index + ".lock")
	if _, err := gitCommand(plan.Repository, index, nil, "read-tree", plan.Base); err != nil {
		return plan, err
	}
	tracked, err := gitCommand(plan.Repository, index, nil, "ls-files", "-z", "--", plan.Destination+"/")
	if err != nil {
		return plan, err
	}
	var entries bytes.Buffer
	for _, path := range strings.Split(tracked, "\x00") {
		if path != "" {
			fmt.Fprintf(&entries, "0 %s\t%s\x00", strings.Repeat("0", len(plan.Base)), path)
		}
	}
	put := func(path string, raw []byte) error {
		blob, err := gitCommand(plan.Repository, "", raw, "hash-object", "-w", "--stdin")
		if err != nil {
			return err
		}
		fmt.Fprintf(&entries, "100644 %s\t%s\x00", strings.TrimSpace(blob), path)
		return nil
	}
	output, err := os.OpenRoot(filepath.Join(s.opts.WorkDir, run.ID, "output"))
	if err != nil {
		return plan, err
	}
	defer output.Close()
	expectedOutputs := map[string]string{}
	if run.Evidence != nil {
		for _, file := range run.Evidence.Outputs {
			expectedOutputs[file.Path] = file.SHA256
		}
		if len(expectedOutputs) != len(run.Files) {
			return plan, fmt.Errorf("build output inventory differs from its evidence; rebuild")
		}
	}
	for _, path := range run.Files {
		if !filepath.IsLocal(path) || strings.ContainsAny(path, "\x00\t\r\n\\") {
			return plan, fmt.Errorf("invalid output path")
		}
		raw, err := readBounded(output, path, 8<<20)
		if err != nil {
			return plan, err
		}
		if run.Evidence != nil && expectedOutputs[path] != fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) {
			return plan, fmt.Errorf("output %s differs from the recorded build; rebuild before reviewing", path)
		}
		if err := put(plan.Destination+"/"+path, raw); err != nil {
			return plan, err
		}
	}
	manifest, err := os.ReadFile(filepath.Join(s.opts.WorkDir, run.ID, "release.yaml"))
	if err != nil {
		return plan, err
	}
	if fmt.Sprintf("sha256:%x", sha256.Sum256(manifest)) != run.Selection.ManifestSHA256 {
		return plan, fmt.Errorf("saved release manifest differs from the build selection")
	}
	receipt, err := json.MarshalIndent(struct {
		ReviewID        string `json:"review_id"`
		Tag             string `json:"tag"`
		Build           Run    `json:"build"`
		ReleaseManifest string `json:"release_manifest"`
	}{plan.ID, plan.Tag, run, string(manifest)}, "", "  ")
	if err != nil {
		return plan, err
	}
	if err := put(receiptPath, append(receipt, '\n')); err != nil {
		return plan, err
	}
	if _, err := gitCommand(plan.Repository, index, entries.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return plan, err
	}
	tree, err := gitCommand(plan.Repository, index, nil, "write-tree")
	if err != nil {
		return plan, err
	}
	plan.Tree = strings.TrimSpace(tree)
	plan.Summary, err = gitCommand(plan.Repository, "", nil, "diff", "--stat", plan.Base, plan.Tree, "--")
	if err != nil {
		return plan, err
	}
	plan.Diff, err = gitCommand(plan.Repository, "", nil, "diff", "--no-ext-diff", "--no-textconv", "--no-color", plan.Base, plan.Tree, "--")
	if err != nil {
		return plan, err
	}
	if len(plan.Diff) > 4<<20 {
		return plan, fmt.Errorf("publication diff exceeds 4 MiB; review outside this UI")
	}
	// Recheck the base after preparing the frozen tree.
	branch, head, err := checkoutState(plan.Repository)
	if err != nil {
		return plan, err
	}
	if branch != plan.Branch || head != plan.Base {
		return plan, fmt.Errorf("deployment checkout changed during review; retry")
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return plan, err
	}
	err = os.WriteFile(filepath.Join(s.opts.WorkDir, run.ID, "publication-"+plan.ID+".json"), raw, 0600)
	return plan, err
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	if !validID(r.PathValue("id")) || !validID(r.PathValue("review")) {
		failure(w, 400, fmt.Errorf("invalid review"))
		return
	}
	raw, err := os.ReadFile(filepath.Join(s.opts.WorkDir, r.PathValue("id"), "publication-"+r.PathValue("review")+".json"))
	if err != nil {
		failure(w, 404, fmt.Errorf("publication review not found"))
		return
	}
	var plan publicationReview
	if err := json.Unmarshal(raw, &plan); err != nil {
		failure(w, 500, err)
		return
	}
	if plan.ID != r.PathValue("review") || plan.RunID != r.PathValue("id") || plan.Repository != s.opts.DeploymentsRoot || plan.Repository == "" {
		failure(w, 409, fmt.Errorf("publication review belongs to another configuration"))
		return
	}
	commit, err := publishReview(plan)
	if err != nil {
		failure(w, 409, err)
		return
	}
	respond(w, 200, map[string]string{"tag": plan.Tag, "commit": commit, "repository": plan.Repository})
}

func publishReview(plan publicationReview) (string, error) {
	branch, head, err := checkoutState(plan.Repository)
	if err != nil {
		return "", err
	}
	if branch != plan.Branch {
		return "", fmt.Errorf("deployment branch changed; review again")
	}
	// A matching immutable tag also allows retry after a failed fast-forward.
	ref := "refs/tags/" + plan.Tag
	exists, err := gitCommand(plan.Repository, "", nil, "tag", "--list", plan.Tag)
	if err != nil {
		return "", err
	}
	var commit string
	if strings.TrimSpace(exists) != "" {
		tree, err := gitCommand(plan.Repository, "", nil, "rev-parse", ref+"^{tree}")
		if err != nil || strings.TrimSpace(tree) != plan.Tree {
			return "", fmt.Errorf("tag already exists for another review; review again")
		}
		commit, err = gitCommand(plan.Repository, "", nil, "rev-parse", ref+"^{commit}")
		if err != nil {
			return "", err
		}
		commit = strings.TrimSpace(commit)
		parent, err := gitCommand(plan.Repository, "", nil, "rev-parse", commit+"^")
		if err != nil || strings.TrimSpace(parent) != plan.Base {
			return "", fmt.Errorf("tag parent differs from reviewed base")
		}
		_, ancestorErr := gitCommand(plan.Repository, "", nil, "merge-base", "--is-ancestor", commit, head)
		if ancestorErr == nil {
			return commit, nil
		}
	}
	if head != plan.Base {
		return "", fmt.Errorf("deployment HEAD changed; review again")
	}
	if commit == "" {
		// commit-tree deliberately commits the reviewed tree without staging the
		// user's checkout or running hooks that could replace reviewed content.
		commit, err = gitCommand(plan.Repository, "", []byte("Publish "+plan.Tag+"\n"), "commit-tree", plan.Tree, "-p", plan.Base)
		if err != nil {
			return "", err
		}
		commit = strings.TrimSpace(commit)
		if _, err := gitCommand(plan.Repository, "", nil, "tag", "-a", plan.Tag, commit, "-m", "Publish "+plan.Tag); err != nil {
			return "", err
		}
	}
	branch, head, err = checkoutState(plan.Repository)
	if err != nil {
		return "", err
	}
	if branch != plan.Branch || head != plan.Base {
		return "", fmt.Errorf("checkout changed; tag retained, retry this review after restoring the reviewed base")
	}
	if _, err := gitCommand(plan.Repository, "", nil, "merge", "--ff-only", "--no-edit", commit); err != nil {
		return "", fmt.Errorf("tag retained; retry this review to finish the local fast-forward: %w", err)
	}
	return commit, nil
}
