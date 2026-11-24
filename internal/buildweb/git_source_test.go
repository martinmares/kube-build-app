package buildweb

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kube-env/internal/appinfo"
)

func sourceGit(t *testing.T, raw []byte) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "releases"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "releases/SWD-42.yaml"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Fixture"}, {"config", "user.email", "fixture@example.invalid"}, {"add", "."}, {"commit", "-m", "Release fixture"}, {"tag", "release/SWD-42"}} {
		if _, err := gitCommand(root, "", nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	head, err := gitCommand(root, "", nil, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return root, strings.TrimSpace(head)
}

func TestConfiguredGitCapturesTagAndBuildsWithoutRepository(t *testing.T) {
	s, opts := fixture(t)
	raw := []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, "releases/SWD-42.yaml")))
	opts.ReleasesRoot = ""
	repo, head := sourceGit(t, raw)
	opts.ArtifactSource = &ArtifactSource{Storage: "git", Repository: repo, Tag: "release/{release_id}", Path: "releases/{release_id}.yaml"}
	var err error
	s, err = New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "releases/SWD-42.yaml")
	if err := os.WriteFile(file, []byte("uncommitted working file"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommand(repo, "", nil, "add", "."); err != nil {
		t.Fatal(err)
	}
	before, err := gitCommand(repo, "", nil, "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	w := postConfigured(s, `{"release_id":"SWD-42","environment":"dev"}`)
	if w.Code != 200 {
		t.Fatalf("read: %d %s", w.Code, w.Body.String())
	}
	var captured remoteRelease
	if err := json.Unmarshal(w.Body.Bytes(), &captured); err != nil {
		t.Fatal(err)
	}
	if captured.GitOrigin == nil || captured.GitOrigin.Commit != head || captured.GitOrigin.Verify(raw) != nil || captured.GitOrigin.Tag != "release/SWD-42" {
		t.Fatalf("origin: %+v", captured)
	}
	after, err := gitCommand(repo, "", nil, "status", "--porcelain")
	if err != nil || after != before {
		t.Fatal("source working tree changed")
	}
	// A new read observes a moved tag, but the earlier reviewed selection stays pinned.
	updated := strings.ReplaceAll(string(raw), strings.Repeat("a", 64), strings.Repeat("c", 64))
	if err := os.WriteFile(file, []byte(updated), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "Second release fixture"}, {"tag", "-f", "release/SWD-42"}} {
		if _, err := gitCommand(repo, "", nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	newer := postConfigured(s, `{"release_id":"SWD-42"}`)
	if newer.Code != 200 {
		t.Fatal(newer.Body.String())
	}
	var second remoteRelease
	if err := json.Unmarshal(newer.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if second.GitOrigin.Commit == head || second.Release.SHA256 == captured.Release.SHA256 {
		t.Fatal("moved tag not observed by fresh read")
	}
	if err := os.Rename(repo, repo+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(repo+"-unavailable", repo) })
	s, err = New(opts, appinfo.For(appinfo.BuildAppName))
	if err != nil {
		t.Fatal(err)
	}
	w = postBuild(t, s, selection{Environment: "dev", RemoteRelease: captured.Token, ManifestSHA256: captured.Release.SHA256})
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	var accepted Run
	if err := json.Unmarshal(w.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	run := waitBuild(t, s, accepted.ID)
	if run.Status != "succeeded" || run.ReleaseSource != nil || run.ReleaseGitSource == nil || run.ReleaseGitSource.Commit != head {
		t.Fatalf("build: %+v", run)
	}
	found := false
	for _, input := range run.Evidence.Files {
		if input.GitOrigin != nil {
			found = true
			if input.GitOrigin.Verify(raw) != nil || input.GitRoot != "" || input.LocalState != "captured Git snapshot" {
				t.Fatalf("evidence: %+v", input)
			}
		}
	}
	if !found {
		t.Fatal("missing Git input evidence")
	}
	// Changes to the captured file must be rejected without reading the original repository.
	staged := filepath.Join(opts.WorkDir, ".url-releases", captured.Token, "release.yaml")
	if err := os.WriteFile(staged, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	if w := postBuild(t, s, selection{Environment: "dev", RemoteRelease: captured.Token, ManifestSHA256: captured.Release.SHA256}); w.Code != 409 {
		t.Fatal("modified snapshot accepted")
	}
}

func TestGitSourceTagsChecksumsAndFileTypes(t *testing.T) {
	raw := []byte("synthetic document\n")
	repo, head := sourceGit(t, raw)
	checksum := fmt.Sprintf("%x", sha256.Sum256(raw))
	ctx := context.Background()
	_, origin, err := readGitRelease(ctx, repo, "release/SWD-42", "releases/SWD-42.yaml", checksum)
	if err != nil || origin.Commit != head || origin.Verify(raw) != nil {
		t.Fatalf("lightweight tag: %+v %v", origin, err)
	}
	if _, err := gitCommand(repo, "", nil, "tag", "-a", "annotated/SWD-42", "-m", "Annotated fixture"); err != nil {
		t.Fatal(err)
	}
	_, origin, err = readGitRelease(ctx, repo, "annotated/SWD-42", "releases/SWD-42.yaml", "")
	if err != nil || origin.Commit != head {
		t.Fatalf("annotated tag: %+v %v", origin, err)
	}
	for _, tc := range []struct{ tag, path, hash string }{
		{"main", "releases/SWD-42.yaml", ""},
		{"release/SWD-42", "releases/missing.yaml", ""},
		{"release/SWD-42", "releases/SWD-42.yaml", strings.Repeat("0", 64)},
		{"release/SWD-42", "releases", ""},
		{"release/SWD-42", "releases/SWD-42.yaml", "invalid"},
	} {
		if _, _, err := readGitRelease(ctx, repo, tc.tag, tc.path, tc.hash); err == nil {
			t.Errorf("accepted invalid Git input: %+v", tc)
		}
	}
	if err := os.Symlink("SWD-42.yaml", filepath.Join(repo, "releases/link.yaml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "releases/large.yaml"), []byte(strings.Repeat("x", (8<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "File type fixtures"}, {"tag", "types"}} {
		if _, err := gitCommand(repo, "", nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, document := range []string{"releases/link.yaml", "releases/large.yaml"} {
		if _, _, err := readGitRelease(ctx, repo, "types", document, ""); err == nil {
			t.Errorf("accepted %s", document)
		}
	}
	if _, err := gitCommand(repo, "", nil, "tag", "-d", "release/SWD-42"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readGitRelease(ctx, repo, "release/SWD-42", "releases/SWD-42.yaml", ""); err == nil {
		t.Fatal("deleted tag accepted")
	}
}

func TestGitSourceCannotBeUsedAsWorkOrPublicationDirectory(t *testing.T) {
	_, opts := fixture(t)
	raw := []byte(mustRead(t, filepath.Join(opts.ReleasesRoot, "releases/SWD-42.yaml")))
	repo, _ := sourceGit(t, raw)
	opts.ArtifactSource = &ArtifactSource{Storage: "git", Repository: repo, Tag: "release/{release_id}", Path: "releases/{release_id}.yaml"}
	opts.WorkDir = filepath.Join(repo, "new-work")
	if _, err := New(opts, appinfo.For(appinfo.BuildAppName)); err == nil {
		t.Fatal("work inside source accepted")
	}
	if _, err := os.Stat(opts.WorkDir); !os.IsNotExist(err) {
		t.Fatal("created a work directory in the source")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}
	opts.WorkDir = filepath.Join(alias, "nested", "new-work")
	if _, err := New(opts, appinfo.For(appinfo.BuildAppName)); err == nil {
		t.Fatal("work inside symlinked source accepted")
	}
	if _, err := os.Stat(filepath.Join(repo, "nested")); !os.IsNotExist(err) {
		t.Fatal("created directories through source alias")
	}
	opts.WorkDir = filepath.Join(t.TempDir(), "work")
	opts.DeploymentsRoot = repo
	if _, err := New(opts, appinfo.For(appinfo.BuildAppName)); err == nil {
		t.Fatal("publishing to source accepted")
	}
}
