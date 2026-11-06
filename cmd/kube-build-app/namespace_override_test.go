package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCobraBuildNamespaceOverride(t *testing.T) {
	root := writeCLIEnv(t)
	target := filepath.Join(t.TempDir(), "target")
	output := runCLI(t,
		"build",
		"-e", "test",
		"-R", root,
		"--output-dir", target,
		"--namespace", "cli-override",
		"--verbose",
		"--color", "never",
	)
	content, err := os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "namespace: cli-override") {
		t.Fatalf("deployment does not contain CLI namespace override:\n%s", content)
	}
	if !strings.Contains(output, "Generated 1 deployment(s)") {
		t.Fatalf("unexpected command output:\n%s", output)
	}
	if !strings.Contains(output, "Build namespace: cli-override (CLI override)") {
		t.Fatalf("verbose namespace override event missing:\n%s", output)
	}
}
