package buildapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func evidenceFixture(t *testing.T) Options {
	t.Helper()
	base := t.TempDir()
	opts := Options{Root: filepath.Join(base, "environments"), Target: filepath.Join(base, "output"), Environment: "test", EvidenceDir: filepath.Join(base, "evidence"), VarsSources: []string{"json"}}
	writeFile(t, filepath.Join(opts.Root, "test/apps/api.yml"), `name: api
containers:
  - name: api
    image: registry.example.invalid/api@sha256:`+strings.Repeat("a", 64)+`
    assets:
      - file: assets/message.txt
        to: /app/message.txt
        transform: true
rollout_on:
  checksums:
    config:
      files: [assets/message.txt]
`)
	writeJSON(t, filepath.Join(opts.Root, "test/env.unsecured.json"), map[string]any{"environment": map[string]string{"NAMESPACE": "lab", "MESSAGE": "synthetic-message"}})
	writeFile(t, filepath.Join(opts.Root, "test/assets/message.txt"), "{{env:MESSAGE}}\n")
	return opts
}

func fixtureGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory, "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "GIT_") {
			command.Env = append(command.Env, item)
		}
	}
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, raw)
	}
	return strings.TrimSpace(string(raw))
}

func initEvidenceRepository(t *testing.T, directory string) string {
	t.Helper()
	fixtureGit(t, directory, "init", "-b", "main")
	fixtureGit(t, directory, "config", "user.name", "Synthetic Builder")
	fixtureGit(t, directory, "config", "user.email", "builder@example.invalid")
	fixtureGit(t, directory, "config", "commit.gpgsign", "false")
	fixtureGit(t, directory, "add", ".")
	fixtureGit(t, directory, "commit", "-m", "Synthetic inputs")
	return fixtureGit(t, directory, "rev-parse", "HEAD")
}

func TestEvidenceCapturesReadBytesGitChangesAndExactOutputs(t *testing.T) {
	opts := evidenceFixture(t)
	initial := initEvidenceRepository(t, opts.Root)
	opts.ResourcePolicyRoot = filepath.Join(filepath.Dir(opts.Root), "resources")
	policy := filepath.Join(opts.ResourcePolicyRoot, "test/apps/api.yml")
	writeFile(t, policy, "containers:\n  api:\n    cpu: {from: 100m, to: 500m}\n    memory: {from: 128Mi, to: 512Mi}\n")
	policyCommit := initEvidenceRepository(t, opts.ResourcePolicyRoot)
	changedPolicy := "containers:\n  api:\n    cpu: {from: 100m, to: 750m}\n    memory: {from: 128Mi, to: 512Mi}\n"
	writeFile(t, policy, changedPolicy)
	asset := filepath.Join(opts.Root, "test/assets/message.txt")
	original, _ := os.ReadFile(asset)
	var changed bool
	result, err := BuildWithObserver(opts, func(event BuildEvent) {
		if event.Type == "deployment" && !changed {
			changed = true
			writeFile(t, asset, "changed after consumption\n")
			fixtureGit(t, opts.Root, "add", ".")
			fixtureGit(t, opts.Root, "commit", "-m", "Concurrent synthetic input change")
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	evidence := result.Evidence
	if evidence == nil || len(evidence.Repositories) != 2 || evidence.ExecutableSHA256 == "" {
		t.Fatal("missing repository/builder evidence")
	}
	for _, repo := range evidence.Repositories {
		if filepath.Base(repo.Root) == "environments" && (repo.Commit != initial || !repo.ChangedDuringBuild || repo.CommitAfter == initial) {
			t.Fatalf("lost original Git context: %+v", repo)
		}
		if filepath.Base(repo.Root) == "resources" && (repo.Commit != policyCommit || repo.ChangedDuringBuild) {
			t.Fatalf("wrong policy context: %+v", repo)
		}
	}
	var sawAsset, sawPolicy, sawMissing bool
	for _, file := range evidence.Files {
		if file.Missing {
			if file.Snapshot != "" {
				t.Fatal("missing file has contents")
			}
			sawMissing = true
			continue
		}
		raw, err := os.ReadFile(filepath.Join(opts.EvidenceDir, file.Snapshot))
		if err != nil || inputHash(raw) != file.SHA256 {
			t.Fatalf("snapshot mismatch: %s %v", file.Path, err)
		}
		stat, _ := os.Stat(filepath.Join(opts.EvidenceDir, file.Snapshot))
		if stat.Mode().Perm() != 0600 {
			t.Fatal("snapshot is not private")
		}
		if file.GitPath == "test/assets/message.txt" {
			sawAsset = true
			if !bytes.Equal(raw, original) || file.LocalState != "matches-commit" {
				t.Fatal("evidence re-read changed source instead of consumed bytes")
			}
		}
		if filepath.Base(file.GitRoot) == "resources" {
			sawPolicy = true
			if string(raw) != changedPolicy || file.LocalState != "modified" {
				t.Fatal("lost local resource changes")
			}
		}
	}
	if !sawAsset || !sawPolicy || !sawMissing {
		t.Fatal("missing input classes")
	}
	deployment, _ := os.ReadFile(result.Deployments[0])
	if !bytes.Contains(deployment, []byte("750m")) || !bytes.Contains(deployment, []byte("@sha256:")) {
		t.Fatal("recorded policy was not rendered")
	}
	for _, file := range evidence.Outputs {
		raw, err := os.ReadFile(filepath.Join(opts.Target, file.Path))
		if err != nil || inputHash(raw) != file.SHA256 {
			t.Fatalf("output not bound to evidence: %s %v", file.Path, err)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(opts.EvidenceDir, "evidence.json"))
	if err != nil || !json.Valid(metadata) {
		t.Fatal("missing evidence file")
	}
}

func TestEvidenceCachesPresentAndAbsentInputs(t *testing.T) {
	opts := evidenceFixture(t)
	recorder, err := newInputRecorder(opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.inputs = recorder
	path := filepath.Join(opts.Root, "test/assets/message.txt")
	first, err := opts.readInput(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "changed\n")
	again, err := opts.readInput(path)
	if err != nil || !bytes.Equal(first, again) {
		t.Fatal("a repeated read changed its input")
	}
	absent := filepath.Join(opts.Root, "test/replica-profiles.yml")
	if opts.inputFileExists(absent) {
		t.Fatal("missing file exists")
	}
	writeFile(t, absent, "new file\n")
	if opts.inputFileExists(absent) {
		t.Fatal("an initially absent input appeared halfway through a build")
	}
}

func TestEvidenceSupportsAnUncommittedInputRepository(t *testing.T) {
	opts := evidenceFixture(t)
	fixtureGit(t, opts.Root, "init", "-b", "main")
	result, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Evidence.Repositories) != 1 || result.Evidence.Repositories[0].Commit != "" {
		t.Fatal("invented a source commit")
	}
	for _, file := range result.Evidence.Files {
		if file.GitRoot != "" && !file.Missing && file.LocalState != "untracked" {
			t.Fatalf("incorrect uncommitted source: %+v", file)
		}
	}
}

func TestEvidenceCapturesRemoteVariablesOnceWithoutPublishingValues(t *testing.T) {
	opts := evidenceFixture(t)
	const secret = "synthetic-private-value"
	const header = "synthetic-auth-header"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != header {
			t.Error("missing request header")
		}
		w.Write([]byte("NAMESPACE=lab\nMESSAGE=" + secret + "\n"))
	}))
	defer server.Close()
	opts.VarsSources = nil
	opts.EnvURL = server.URL + "/?private-query=synthetic-query"
	opts.EnvURLHeaders = []string{"Authorization: " + header}
	result, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || len(result.Evidence.Variables) != 2 {
		t.Fatal("variables fetched twice or not captured")
	}
	public, _ := json.Marshal(result.Evidence)
	for _, value := range []string{secret, header, "synthetic-query", server.URL} {
		if bytes.Contains(public, []byte(value)) {
			t.Fatal("private variable/transport data appeared in metadata")
		}
	}
	for _, input := range result.Evidence.Variables {
		raw, err := os.ReadFile(filepath.Join(opts.EvidenceDir, input.Snapshot))
		if err != nil || !bytes.Contains(raw, []byte(secret)) || inputHash(raw) != input.SHA256 {
			t.Fatal("private variable snapshot differs from consumed data")
		}
	}
}

func TestEvidenceRejectsOverlapAndPreservesFailureEvidence(t *testing.T) {
	opts := evidenceFixture(t)
	alias := filepath.Join(filepath.Dir(opts.Root), "alias")
	if err := os.Symlink(opts.Root, alias); err != nil {
		t.Fatal(err)
	}
	original := opts.EvidenceDir
	for _, directory := range []string{filepath.Join(alias, "new-evidence"), filepath.Join(opts.Target, "inputs"), filepath.Dir(opts.Target)} {
		opts.EvidenceDir = directory
		if _, err := Build(opts); err == nil {
			t.Fatal("overlapping evidence directory accepted")
		}
	}
	opts.EvidenceDir = original
	writeFile(t, filepath.Join(opts.Root, "test/apps/api.yml"), "invalid: [yaml\n")
	result, err := Build(opts)
	if err == nil || result.Evidence == nil || len(result.Evidence.Files) == 0 || len(result.Evidence.Outputs) != 0 {
		t.Fatal("failed build lost evidence or reported completed outputs")
	}
	if _, err := os.Stat(filepath.Join(opts.EvidenceDir, "evidence.json")); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceDecryptsCapturedCiphertextWithItsSiblingSchema(t *testing.T) {
	opts := evidenceFixture(t)
	opts.DecryptSecured = true
	opts.EncjsonPath = filepath.Join(filepath.Dir(opts.Root), "synthetic-encjson")
	writeFile(t, filepath.Join(opts.Root, "test/env.secured.json"), `{"environment":{"MESSAGE":"EncJson[@api=2.0:@box=synthetic]"}}`)
	writeFile(t, filepath.Join(opts.Root, "test/env.secured.schema.json"), `{"synthetic_schema":true}`)
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-f" ]; then shift; input="$1"; fi
  shift
done
test -f "$input" || exit 10
test -f "${input%.json}.schema.json" || exit 11
printf '%s' '{"environment":{"DECRYPTED":"synthetic-decrypted"}}'
`
	writeFile(t, opts.EncjsonPath, script)
	if err := os.Chmod(opts.EncjsonPath, 0700); err != nil {
		t.Fatal(err)
	}
	result, err := Build(opts)
	if err != nil {
		t.Fatal(err)
	}
	var foundSchema bool
	for _, file := range result.Evidence.Files {
		if filepath.Base(file.Path) == "env.secured.schema.json" && !file.Missing {
			foundSchema = true
		}
	}
	if !foundSchema {
		t.Fatal("sidecar schema was not recorded")
	}
	public, _ := json.Marshal(result.Evidence)
	if bytes.Contains(public, []byte("synthetic-decrypted")) {
		t.Fatal("decrypted values published")
	}
	for _, input := range result.Evidence.Variables {
		if input.Kind == "resolved-variables" {
			raw, err := os.ReadFile(filepath.Join(opts.EvidenceDir, input.Snapshot))
			if err != nil || !bytes.Contains(raw, []byte("synthetic-decrypted")) {
				t.Fatal("decryption result not retained privately")
			}
		}
	}
	entries, err := os.ReadDir(opts.EvidenceDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "decrypt-") {
			t.Fatal("temporary decrypt directory retained")
		}
	}
}
