package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kube-env/internal/appinfo"
)

func TestExamplesCommandOutput(t *testing.T) {
	cmd := newRootCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"examples", "--color", "never"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		"KUBE-BUILD-APP EXAMPLES",
		"## START HERE",
		"## DEFAULTS AND REFERENCES",
		"## RELEASE IMAGES",
		"## DEPLOYMENT OUTPUT",
		"FILE environments/dev/apps/_defaults.yml (YAML)",
		"sidecar_ref_names: [metrics]",
		"SHELL",
		"kube-build-app build -e dev -R environments -P resources --output-dir deploy/dev",
		"--sync-metadata-profile argocd --sync-set prod",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("examples output missing %q", want)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Error("plain output contains ANSI escapes")
	}

	output.Reset()
	cmd = newRootCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"examples", "--color", "always"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), ansiBold+ansiCyan+"KUBE-BUILD-APP EXAMPLES"+ansiReset) {
		t.Error("forced color output lacks styled heading")
	}
}

func TestExamplesModelsValidate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	envFile := findExampleFile(t, "Start here", "Minimal repository", "environments/dev/env.unsecured.json")
	defaults := findExampleFile(t, "Defaults and references", "Reuse a container profile and sidecar", "environments/dev/apps/_defaults.yml")
	app := findExampleFile(t, "Defaults and references", "Reuse a container profile and sidecar", "environments/dev/apps/api.yml")
	for name, content := range map[string]string{
		"env.unsecured.json": envFile,
		"apps/_defaults.yml": defaults,
		"apps/api.yml":       app,
	} {
		path := filepath.Join(root, "dev", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.TrimSpace(runCLI(t, "validate", "-e", "dev", "-R", root)); got != "Validation OK" {
		t.Fatalf("documented defaults and app model do not validate: %s", got)
	}
}

func TestExamplesBuildInputs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	writeExampleTestFile(t, filepath.Join(root, "dev", "env.unsecured.json"),
		findExampleFile(t, "Start here", "Minimal repository", "environments/dev/env.unsecured.json"))
	writeExampleTestFile(t, filepath.Join(root, "dev", "apps", "api.yml"),
		findExampleFile(t, "Start here", "Minimal repository", "environments/dev/apps/api.yml"))

	if got := strings.TrimSpace(runCLI(t, "validate", "-e", "dev", "-R", root)); got != "Validation OK" {
		t.Fatalf("minimal example does not validate: %s", got)
	}
	release := filepath.Join(t.TempDir(), "release.yml")
	writeExampleTestFile(t, release,
		findExampleFile(t, "Release images", "Use release manifest images", "release.yml"))
	target := filepath.Join(t.TempDir(), "deploy")
	runCLI(t, "build", "-e", "dev", "-R", root, "--output-dir", target,
		"--release-manifest", release, "--image-policy", "strict")
	deployment, err := os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(deployment), "image: registry.example.com/demo/api@sha256:0123456789abcdef...") {
		t.Fatalf("release example did not render a digest image reference:\n%s", deployment)
	}

	policyRoot := filepath.Join(t.TempDir(), "resources")
	writeExampleTestFile(t, filepath.Join(policyRoot, "dev", "apps", "api.yml"),
		findExampleFile(t, "Build inputs", "Use a separate resource policy repository", "resources/dev/apps/api.yml"))
	if got := strings.TrimSpace(runCLI(t, "validate", "-e", "dev", "-R", root, "-P", policyRoot)); got != "Validation OK" {
		t.Fatalf("resource policy example does not validate: %s", got)
	}

	writeExampleTestFile(t, filepath.Join(root, "dev", "apps", "api.yml"),
		findExampleFile(t, "App model", "Expose a service and add health checks", "environments/dev/apps/api.yml"))
	if got := strings.TrimSpace(runCLI(t, "validate", "-e", "dev", "-R", root)); got != "Validation OK" {
		t.Fatalf("service and probes example does not validate: %s", got)
	}
}

func writeExampleTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findExampleFile(t *testing.T, sectionName, title, name string) string {
	t.Helper()
	for _, section := range examplesCatalog {
		if section.name != sectionName {
			continue
		}
		for _, example := range section.examples {
			if example.title != title {
				continue
			}
			for _, file := range example.files {
				if file.name == name {
					return file.content
				}
			}
		}
	}
	t.Fatalf("example file not found: %s / %s / %s", sectionName, title, name)
	return ""
}
