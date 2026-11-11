package buildweb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitFixture(t *testing.T, s *Server) string {
	t.Helper()
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if _, err := gitCommand(repo, "", nil, args...); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-b", "main")
	git("config", "user.name", "Lab Builder")
	git("config", "user.email", "builder@example.invalid")
	git("config", "commit.gpgsign", "false")
	git("config", "tag.gpgsign", "false")
	git("config", "core.hooksPath", "/dev/null")
	for _, env := range []string{"dev", "prod"} {
		if err := os.MkdirAll(filepath.Join(repo, "deploy", env), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "deploy", env, "old.yaml"), []byte("old: true\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-m", "Initial synthetic deployment")
	s.opts.DeploymentsRoot = repo
	if err := s.configurePublication(); err != nil {
		t.Fatal(err)
	}
	return repo
}

func publicationRequest(s *Server, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
	return w
}

func TestPublicationReviewAndLocalRevision(t *testing.T) {
	s, opts := fixture(t)
	repo := gitFixture(t, s)
	run := startFixtureBuild(t, s)
	indexBefore := mustRead(t, filepath.Join(repo, ".git/index"))
	_, before, err := checkoutState(repo)
	if err != nil {
		t.Fatal(err)
	}
	w := publicationRequest(s, "/api/builds/"+run.ID+"/publication-review")
	if w.Code != 200 {
		t.Fatalf("review: %d %s", w.Code, w.Body.String())
	}
	var review publicationReview
	if err := json.Unmarshal(w.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.Tag != "dev/SWD-42.r1" || !strings.Contains(review.Diff, "-old: true") || !strings.Contains(review.Diff, "revisions/dev/SWD-42.r1.json") {
		t.Fatalf("incomplete review: tag=%s", review.Tag)
	}
	_, after, err := checkoutState(repo)
	if err != nil || before != after {
		t.Fatalf("review changed checkout: %s %v", after, err)
	}
	if mustRead(t, filepath.Join(repo, ".git/index")) != indexBefore {
		t.Fatal("review changed the checkout index")
	}
	// Confirmation publishes the immutable reviewed tree, not later file edits.
	path := "deployments/api-deployment.yml"
	original := mustRead(t, filepath.Join(opts.WorkDir, run.ID, "output", path))
	if err := os.WriteFile(filepath.Join(opts.WorkDir, run.ID, "output", path), []byte(strings.Replace(original, "cpu: 500m", "cpu: 750m", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	endpoint := "/api/builds/" + run.ID + "/publish/" + review.ID
	w = publicationRequest(s, endpoint)
	if w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	if got := mustRead(t, filepath.Join(repo, "deploy/dev", path)); got != original {
		t.Fatal("did not publish reviewed bytes")
	}
	if _, err := os.Stat(filepath.Join(repo, "deploy/dev/old.yaml")); !os.IsNotExist(err) {
		t.Fatal("old generated file was not deleted")
	}
	if mustRead(t, filepath.Join(repo, "deploy/prod/old.yaml")) != "old: true\n" {
		t.Fatal("another environment changed")
	}
	receipt := mustRead(t, filepath.Join(repo, "revisions/dev/SWD-42.r1.json"))
	if !strings.Contains(receipt, "release_manifest") || !strings.Contains(receipt, "manifest_sha256") {
		t.Fatal("missing build evidence")
	}
	w = publicationRequest(s, endpoint)
	if w.Code != 200 {
		t.Fatalf("idempotent retry: %s", w.Body.String())
	}
	count, err := gitCommand(repo, "", nil, "rev-list", "--count", "HEAD")
	if err != nil || strings.TrimSpace(count) != "2" {
		t.Fatalf("duplicate commit: %s %v", count, err)
	}
	// History and reviews also survive restarting the builder.
	opts.DeploymentsRoot = repo
	restarted, err := New(opts, s.info)
	if err != nil {
		t.Fatal(err)
	}
	if w := publicationRequest(restarted, endpoint); w.Code != 200 {
		t.Fatalf("restart retry: %s", w.Body.String())
	}
	if _, err := restarted.preparePublication(run); err == nil || !strings.Contains(err.Error(), "differs from the recorded build") {
		t.Fatalf("modified output accepted with original evidence: %v", err)
	}
	// A resource revision is a new build from changed inputs, with the same images.
	sourcePath := filepath.Join(opts.Root, "dev/apps/api.yml")
	source := mustRead(t, sourcePath)
	if err := os.WriteFile(sourcePath, []byte(strings.Replace(source, "500m", "750m", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	updatedRun := startFixtureBuild(t, restarted)
	next, err := restarted.preparePublication(updatedRun)
	if err != nil || next.Tag != "dev/SWD-42.r2" {
		t.Fatalf("next revision: %+v %v", next, err)
	}
	if _, err := publishReview(next); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(repo, "deploy/dev", path)); got != strings.Replace(original, "cpu: 500m", "cpu: 750m", 1) {
		t.Fatal("new review did not capture resource revision")
	}
	if w := publicationRequest(restarted, endpoint); w.Code != 200 {
		t.Fatalf("retry after a later revision: %s", w.Body.String())
	}
}

func TestPublicationRejectsDirtyCheckoutStaleReviewAndTagCollision(t *testing.T) {
	s, _ := fixture(t)
	repo := gitFixture(t, s)
	run := startFixtureBuild(t, s)
	first, err := s.preparePublication(run)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.preparePublication(run)
	if err != nil {
		t.Fatal(err)
	}
	dirty := filepath.Join(repo, "user-notes.txt")
	if err := os.WriteFile(dirty, []byte("keep me\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := publishReview(first); err == nil {
		t.Fatal("dirty checkout accepted")
	}
	if _, err := s.preparePublication(run); err == nil {
		t.Fatal("dirty review accepted")
	}
	if mustRead(t, dirty) != "keep me\n" {
		t.Fatal("user file changed")
	}
	if _, err := gitCommand(repo, "", nil, "add", "user-notes.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := publishReview(first); err == nil {
		t.Fatal("staged checkout accepted")
	}
	staged, err := gitCommand(repo, "", nil, "diff", "--cached", "--name-only")
	if err != nil || strings.TrimSpace(staged) != "user-notes.txt" {
		t.Fatalf("staged user changes lost: %s %v", staged, err)
	}
	if _, err := gitCommand(repo, "", nil, "commit", "-m", "Concurrent user commit"); err != nil {
		t.Fatal(err)
	}
	if _, err := publishReview(first); err == nil || !strings.Contains(err.Error(), "HEAD changed") {
		t.Fatalf("stale base accepted: %v", err)
	}
	first, err = s.preparePublication(run)
	if err != nil {
		t.Fatal(err)
	}
	second, err = s.preparePublication(run)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publishReview(first); err != nil {
		t.Fatal(err)
	}
	if _, err := publishReview(second); err == nil || !strings.Contains(err.Error(), "another review") {
		t.Fatalf("tag collision accepted: %v", err)
	}
}

func TestPublicationFailureAndFastForwardRetry(t *testing.T) {
	s, _ := fixture(t)
	repo := gitFixture(t, s)
	run := startFixtureBuild(t, s)
	plan, err := s.preparePublication(run)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a previously created tag with a pending checkout fast-forward.
	commit, err := gitCommand(repo, "", []byte("Publish "+plan.Tag+"\n"), "commit-tree", plan.Tree, "-p", plan.Base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitCommand(repo, "", nil, "tag", "-a", plan.Tag, strings.TrimSpace(commit), "-m", "Pending publication"); err != nil {
		t.Fatal(err)
	}
	got, err := publishReview(plan)
	if err != nil || got != strings.TrimSpace(commit) {
		t.Fatalf("retry: %s %v", got, err)
	}
	// A nonexistent immutable object cannot produce a success or a new tag.
	plan, err = s.preparePublication(run)
	if err != nil {
		t.Fatal(err)
	}
	plan.Tree = strings.Repeat("f", len(plan.Base))
	if _, err := publishReview(plan); err == nil {
		t.Fatal("failed commit reported success")
	}
	tags, err := gitCommand(repo, "", nil, "tag", "--list", plan.Tag)
	if err != nil || tags != "" {
		t.Fatalf("failed commit left tag: %s %v", tags, err)
	}
}
