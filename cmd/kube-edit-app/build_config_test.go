package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"kube-env/internal/buildapp"
)

func TestLoadEditorBuildConfigMapsContextsAndResolvesPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "editor-build.yml")
	content := `environments:
  dev:
    namespace: dev-namespace
    env_file: inputs/dev.env
    env_url_headers: ['Authorization: Bearer secret']
    vars_sources: [dot-env, json]
    decrypt_secured: true
    release_manifest: releases/dev.yml
    images: [api/api=registry.example.test/api:1]
    image_policy: strict
    image_reference: digest
    force_image_tag: release-1
    force_image_prefix: registry.example.test
    resource_policy_root: policies
    replica_profile: small
    replica_profiles_file: profiles.yml
    down: [worker]
    sync_metadata_profile: kube-deploy-sync
    sync_metadata_prefix: sync.example.test
    sync_set: dev
    yaml_indent: 4
    legacy_apply_env: true
    helm_escape_assets: true
  test:
    env_url: https://config.example.test/render
    env_url_insecure: true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	contexts, err := loadEditorBuildConfig(path, buildapp.Options{
		Root:           "/repository",
		ImagePolicy:    "fallback",
		ImageReference: "auto",
		SyncPrefix:     "kube-build-app.io",
		YAMLIndent:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	dev := contexts["dev"]
	if dev.Environment != "dev" || dev.Namespace != "dev-namespace" || dev.Root != "/repository" {
		t.Fatalf("unexpected dev identity: %+v", dev)
	}
	for field, pair := range map[string][2]string{
		"env file":         {dev.EnvFile, filepath.Join(dir, "inputs", "dev.env")},
		"release manifest": {dev.ReleaseManifest, filepath.Join(dir, "releases", "dev.yml")},
		"policy root":      {dev.ResourcePolicyRoot, filepath.Join(dir, "policies")},
		"profiles file":    {dev.ProfilesFile, filepath.Join(dir, "profiles.yml")},
	} {
		got, want := pair[0], pair[1]
		if got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
	if !reflect.DeepEqual(dev.EnvURLHeaders, []string{"Authorization: Bearer secret"}) ||
		!reflect.DeepEqual(dev.VarsSources, []string{"dot-env", "json"}) ||
		!reflect.DeepEqual(dev.ImageOverrides, []string{"api/api=registry.example.test/api:1"}) ||
		!reflect.DeepEqual(dev.Down, []string{"worker"}) {
		t.Fatalf("unexpected list options: %+v", dev)
	}
	if !dev.DecryptSecured || dev.ImagePolicy != "strict" || dev.ImageReference != "digest" ||
		dev.ForceImageTag != "release-1" || dev.ForceImagePrefix != "registry.example.test" ||
		dev.Profile != "small" || dev.SyncProfile != "kube-deploy-sync" ||
		dev.SyncPrefix != "sync.example.test" || dev.SyncSet != "dev" || dev.YAMLIndent != 4 ||
		!dev.LegacyApplyEnv || !dev.HelmEscapeAssets {
		t.Fatalf("unexpected dev options: %+v", dev)
	}
	testContext := contexts["test"]
	if testContext.Environment != "test" || testContext.EnvURL != "https://config.example.test/render" || !testContext.EnvURLInsecure {
		t.Fatalf("unexpected test context: %+v", testContext)
	}
	if testContext.ImagePolicy != "fallback" || testContext.ImageReference != "auto" || testContext.YAMLIndent != 2 {
		t.Fatalf("test context did not inherit safe defaults: %+v", testContext)
	}
}

func TestLoadEditorBuildConfigRejectsInvalidSchemaAndNames(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"unknown field", "environments:\n  dev:\n    env_urll: https://example.test\n", "field env_urll not found"},
		{"empty", "environments: {}\n", "at least one"},
		{"path name", "environments:\n  ../dev: {}\n", "invalid environment name"},
		{"trim collision", "environments:\n  dev: {}\n  ' dev ': {}\n", "duplicate environment name"},
		{"multiple documents", "environments:\n  dev: {}\n---\nenvironments:\n  test: {}\n", "multiple YAML documents"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "build.yml")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := loadEditorBuildConfig(path, buildapp.Options{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestValidateBuildConfigEnvironmentsRequiresExactSet(t *testing.T) {
	configured := map[string]buildapp.Options{"dev": {}, "obsolete": {}}
	if err := validateBuildConfigEnvironments(configured, []string{"dev", "test"}); err == nil || !strings.Contains(err.Error(), "missing environments: test") {
		t.Fatalf("missing environment error = %v", err)
	}
	delete(configured, "obsolete")
	configured["test"] = buildapp.Options{}
	if err := validateBuildConfigEnvironments(configured, []string{"dev", "test"}); err != nil {
		t.Fatalf("exact environment set rejected: %v", err)
	}
	configured["obsolete"] = buildapp.Options{}
	if err := validateBuildConfigEnvironments(configured, []string{"dev", "test"}); err == nil || !strings.Contains(err.Error(), "unknown environments: obsolete") {
		t.Fatalf("unknown environment error = %v", err)
	}
}

func TestBuildConfigOrderedReleaseManifests(t *testing.T) {
	dir := t.TempDir()
	configured := editorBuildContext{ReleaseManifest: "base.yml", ReleaseManifests: []string{"core.yml", "addons.yml"}, ReleaseID: "combined"}
	opts := configured.options(buildapp.Options{}, dir)
	if opts.ReleaseManifest != filepath.Join(dir, "base.yml") || len(opts.ReleaseManifests) != 2 || opts.ReleaseManifests[0] != filepath.Join(dir, "core.yml") || opts.ReleaseManifests[1] != filepath.Join(dir, "addons.yml") || opts.ReleaseID != "combined" {
		t.Fatalf("options=%#v", opts)
	}
}
