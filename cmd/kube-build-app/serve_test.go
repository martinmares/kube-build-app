package main

import (
	"bytes"
	"kube-env/internal/buildweb"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kube-env/internal/appinfo"
)

func TestServeHelpDescribesStandaloneBuilder(t *testing.T) {
	command := newRootCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"serve", "--help"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"--releases-root", "--work-dir", "--listen"} {
		if !strings.Contains(output.String(), option) {
			t.Fatalf("missing %s", option)
		}
	}
}

func TestServeRequiresBrowserOwnedOutputSelection(t *testing.T) {
	command := newRootCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	command.SetArgs([]string{"serve", "--releases-root", "missing", "--output-dir", "existing-deployments"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "keeps outputs in --work-dir") {
		t.Fatalf("unexpected result: %v", err)
	}
}

func TestConfiguredSourceCLIOverrides(t *testing.T) {
	base := t.TempDir()
	filename := filepath.Join(base, "source.yaml")
	if err := os.WriteFile(filename, []byte("artifact_source:\n  storage: artifactory\n  base_url: https://old.example.invalid\n  path: '{release_id}.yaml'\n  token_file: credentials/token\n  sha256: '"+strings.Repeat("a", 64)+"'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command := newServeCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	for name, value := range map[string]string{"release-base-url": "https://new.example.invalid", "release-path": "releases/{release_id}.yaml", "release-sha256": "", "release-token-file": "local/token"} {
		if err := command.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	source, err := configuredArtifactSource(command, filename, buildweb.ArtifactSource{BaseURL: "https://new.example.invalid", Path: "releases/{release_id}.yaml", TokenFile: "local/token"})
	if err != nil {
		t.Fatal(err)
	}
	if source.BaseURL != "https://new.example.invalid" || source.Path != "releases/{release_id}.yaml" || source.SHA256 != "" || source.TokenFile != "local/token" {
		t.Fatalf("precedence: %+v", source)
	}
	command = newServeCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	source, err = configuredArtifactSource(command, filename, buildweb.ArtifactSource{})
	if err != nil || source.TokenFile != filepath.Join(base, "credentials/token") {
		t.Fatalf("YAML relative path: %+v %v", source, err)
	}
	source, err = configuredArtifactSource(command, "", buildweb.ArtifactSource{})
	if err != nil || source != nil {
		t.Fatalf("unconfigured: %+v %v", source, err)
	}
	if err := command.Flags().Set("release-base-url", "https://only.example.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := configuredArtifactSource(command, "", buildweb.ArtifactSource{BaseURL: "https://only.example.invalid"}); err == nil {
		t.Fatal("incomplete source accepted")
	}
}
