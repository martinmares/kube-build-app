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
	source, err := configuredArtifactSource(command, filename, buildweb.ArtifactSource{BaseURL: "https://new.example.invalid", Path: "releases/{release_id}.yaml", TokenFile: "local/token"}, buildweb.GitRemote{})
	if err != nil {
		t.Fatal(err)
	}
	if source.BaseURL != "https://new.example.invalid" || source.Path != "releases/{release_id}.yaml" || source.SHA256 != "" || source.TokenFile != "local/token" {
		t.Fatalf("precedence: %+v", source)
	}
	command = newServeCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	source, err = configuredArtifactSource(command, filename, buildweb.ArtifactSource{}, buildweb.GitRemote{})
	if err != nil || source.TokenFile != filepath.Join(base, "credentials/token") {
		t.Fatalf("YAML relative path: %+v %v", source, err)
	}
	source, err = configuredArtifactSource(command, "", buildweb.ArtifactSource{}, buildweb.GitRemote{})
	if err != nil || source != nil {
		t.Fatalf("unconfigured: %+v %v", source, err)
	}
	if err := command.Flags().Set("release-base-url", "https://only.example.invalid"); err != nil {
		t.Fatal(err)
	}
	if _, err := configuredArtifactSource(command, "", buildweb.ArtifactSource{BaseURL: "https://only.example.invalid"}, buildweb.GitRemote{}); err == nil {
		t.Fatal("incomplete source accepted")
	}
}

func TestConfiguredGitSourceCLI(t *testing.T) {
	command := newServeCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	values := map[string]string{"release-storage": "git", "release-repository": "./source-repository", "release-tag": "release/{release_id}", "release-path": "releases/{release_id}.yaml"}
	for flag, value := range values {
		if err := command.Flags().Set(flag, value); err != nil {
			t.Fatal(err)
		}
	}
	source, err := configuredArtifactSource(command, "", buildweb.ArtifactSource{Storage: "git", Repository: "./source-repository", Tag: "release/{release_id}", Path: "releases/{release_id}.yaml"}, buildweb.GitRemote{})
	if err != nil || source.Storage != "git" || source.Repository != "./source-repository" {
		t.Fatalf("Git CLI: %+v %v", source, err)
	}
}

func TestRemoteGitCLIOverrides(t *testing.T) {
	command := newServeCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	values := map[string]string{"release-storage": "git", "release-git-url": "https://git.example.invalid/project.git", "release-git-username": "reader", "release-git-password-file": "private/password", "release-git-refresh-interval": "5m", "release-tag": "release/{release_id}", "release-path": "releases/{release_id}.yaml"}
	for flag, value := range values {
		if err := command.Flags().Set(flag, value); err != nil {
			t.Fatal(err)
		}
	}
	source, err := configuredArtifactSource(command, "", buildweb.ArtifactSource{Storage: "git", Tag: "release/{release_id}", Path: "releases/{release_id}.yaml"}, buildweb.GitRemote{URL: "https://git.example.invalid/project.git", Username: "reader", PasswordFile: "private/password", RefreshInterval: "5m"})
	if err != nil || source.Remote == nil || source.Remote.PasswordFile != "private/password" || source.Remote.RefreshInterval != "5m" {
		t.Fatalf("remote CLI: %+v %v", source, err)
	}
	if err := command.Flags().Set("release-repository", "attached"); err != nil {
		t.Fatal(err)
	}
	if _, err := configuredArtifactSource(command, "", buildweb.ArtifactSource{Storage: "git", Repository: "attached", Tag: "release/{release_id}", Path: "releases/{release_id}.yaml"}, *source.Remote); err == nil {
		t.Fatal("CLI accepted attached and remote source")
	}
}

func TestRemoteGitConfigCanDisableScheduleOrSwitchToAttachedCheckout(t *testing.T) {
	base := t.TempDir()
	filename := filepath.Join(base, "source.yaml")
	if err := os.WriteFile(filename, []byte("artifact_source:\n  storage: git\n  tag: release/{release_id}\n  path: releases/{release_id}.yaml\n  remote:\n    url: https://git.example.invalid/project.git\n    refresh_interval: 5m\n"), 0600); err != nil {
		t.Fatal(err)
	}
	command := newServeCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	if err := command.Flags().Set("release-git-refresh-interval", ""); err != nil {
		t.Fatal(err)
	}
	source, err := configuredArtifactSource(command, filename, buildweb.ArtifactSource{}, buildweb.GitRemote{})
	if err != nil || source.Remote == nil || source.Remote.RefreshInterval != "" {
		t.Fatalf("clear schedule: %+v %v", source, err)
	}
	command = newServeCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	if err := command.Flags().Set("release-git-url", ""); err != nil {
		t.Fatal(err)
	}
	if err := command.Flags().Set("release-repository", "./attached"); err != nil {
		t.Fatal(err)
	}
	source, err = configuredArtifactSource(command, filename, buildweb.ArtifactSource{Repository: "./attached"}, buildweb.GitRemote{})
	if err != nil || source.Remote != nil || source.Repository != "./attached" {
		t.Fatalf("switch source: %+v %v", source, err)
	}
}

func TestPackageOnlyConfigAndCLIOverrides(t *testing.T) {
	base := t.TempDir()
	filename := filepath.Join(base, "builder.yaml")
	raw := "package_output:\n  storage: directory\n  directory: shared\n  path: 'packages/{release_id}/{environment}/{build_id}.zip'\n"
	if err := os.WriteFile(filename, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	command := newServeCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	source, err := configuredArtifactSource(command, filename, buildweb.ArtifactSource{}, buildweb.GitRemote{})
	if err != nil || source != nil {
		t.Fatalf("package-only config: %+v %v", source, err)
	}
	target, err := configuredPackageOutput(command, filename, buildweb.PackageOutput{})
	if err != nil || target.Directory != filepath.Join(base, "shared") {
		t.Fatalf("relative output: %+v %v", target, err)
	}
	if err := command.Flags().Set("package-output-dir", "cli-output"); err != nil {
		t.Fatal(err)
	}
	if err := command.Flags().Set("package-path", "handoff/{build_id}.zip"); err != nil {
		t.Fatal(err)
	}
	target, err = configuredPackageOutput(command, filename, buildweb.PackageOutput{Directory: "cli-output", Path: "handoff/{build_id}.zip"})
	if err != nil || target.Directory != "cli-output" || target.Path != "handoff/{build_id}.zip" {
		t.Fatalf("CLI precedence: %+v %v", target, err)
	}
	if err := command.Flags().Set("package-output-dir", ""); err != nil {
		t.Fatal(err)
	}
	target, err = configuredPackageOutput(command, filename, buildweb.PackageOutput{})
	if err != nil || target != nil {
		t.Fatal("cannot disable package output")
	}
}
