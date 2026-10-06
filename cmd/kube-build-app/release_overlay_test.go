package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIRepeatedReleaseManifestAndPublicInjection(t *testing.T) {
	root := writeCLIEnv(t)
	first, second := filepath.Join(root, "a.yml"), filepath.Join(root, "b.yml")
	writeTestFile(t, first, "apiVersion: oci-toolbox/v1\nkind: ImageRelease\nrelease_id: a\nimages:\n  - app_name: api\n    container_name: api\n    image: old.example/api\n    tag: first\n")
	writeTestFile(t, second, "apiVersion: oci-toolbox/v2\nkind: ImageRelease\nrelease_id: b\nimages:\n  - app_name: api\n    container_name: api\n    repository: new.example/api\n    digest: sha256:second\n")
	target := filepath.Join(t.TempDir(), "out")
	runCLI(t, "build", "-e", "test", "-R", root, "-t", target, "--release-manifest", first, "--release-manifest", second, "--release-id", "combined", "--release-context-name", "release-context-test", "--image-policy", "strict")
	deployment, err := os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(deployment), "image: new.example/api@sha256:second") || !strings.Contains(string(deployment), "app.kubernetes.io/release-id: combined") {
		t.Fatal(string(deployment))
	}
	context, err := os.ReadFile(filepath.Join(target, "assets", "shared", "release-context-test.yml"))
	if err != nil || !strings.Contains(string(context), "RELEASE_ID: combined") {
		t.Fatalf("context=%s, err=%v", context, err)
	}
	runCLI(t, "build", "-e", "test", "-R", root, "-t", target, "-r", second, "-r", first, "--release-id", "combined", "--image", "api/api=docker.io/library/nginx:1.27")
	deployment, err = os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil || !strings.Contains(string(deployment), "image: docker.io/library/nginx:1.27") {
		t.Fatalf("deployment=%s, err=%v", deployment, err)
	}
}
