package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"kube-env/internal/buildapp"
)

type editorBuildConfig struct {
	Environments map[string]editorBuildContext `yaml:"environments"`
}

type editorBuildContext struct {
	Namespace          string   `yaml:"namespace"`
	EnvFile            string   `yaml:"env_file"`
	EnvURL             string   `yaml:"env_url"`
	EnvURLHeaders      []string `yaml:"env_url_headers"`
	EnvURLInsecure     bool     `yaml:"env_url_insecure"`
	VarsSources        []string `yaml:"vars_sources"`
	DecryptSecured     bool     `yaml:"decrypt_secured"`
	ReleaseManifest    string   `yaml:"release_manifest"`
	ReleaseManifests   []string `yaml:"release_manifests"`
	ReleaseID          string   `yaml:"release_id"`
	ImageOverrides     []string `yaml:"images"`
	ImagePolicy        string   `yaml:"image_policy"`
	ImageReference     string   `yaml:"image_reference"`
	ForceImageTag      string   `yaml:"force_image_tag"`
	ForceImagePrefix   string   `yaml:"force_image_prefix"`
	ResourcePolicyRoot string   `yaml:"resource_policy_root"`
	ReplicaProfile     string   `yaml:"replica_profile"`
	ReplicaProfiles    string   `yaml:"replica_profiles_file"`
	Down               []string `yaml:"down"`
	SyncProfile        string   `yaml:"sync_metadata_profile"`
	SyncPrefix         string   `yaml:"sync_metadata_prefix"`
	SyncSet            string   `yaml:"sync_set"`
	YAMLIndent         int      `yaml:"yaml_indent"`
	LegacyApplyEnv     bool     `yaml:"legacy_apply_env"`
	HelmEscapeAssets   bool     `yaml:"helm_escape_assets"`
}

func loadEditorBuildConfig(path string, base buildapp.Options) (map[string]buildapp.Options, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var config editorBuildConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple YAML documents are not allowed")
		}
		return nil, err
	}
	if len(config.Environments) == 0 {
		return nil, errors.New("environments must define at least one build context")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	configDir := filepath.Dir(absPath)
	result := make(map[string]buildapp.Options, len(config.Environments))
	for rawName, configured := range config.Environments {
		name := strings.TrimSpace(rawName)
		if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
			return nil, fmt.Errorf("invalid environment name %q", rawName)
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate environment name %q after trimming whitespace", name)
		}
		opts := configured.options(base, configDir)
		opts.Environment = name
		result[name] = opts
	}
	return result, nil
}

func (configured editorBuildContext) options(base buildapp.Options, configDir string) buildapp.Options {
	opts := base
	opts.Namespace = configured.Namespace
	opts.EnvFile = resolveBuildConfigPath(configDir, configured.EnvFile)
	opts.EnvURL = configured.EnvURL
	opts.EnvURLHeaders = append([]string(nil), configured.EnvURLHeaders...)
	opts.EnvURLInsecure = configured.EnvURLInsecure
	opts.VarsSources = append([]string(nil), configured.VarsSources...)
	opts.DecryptSecured = configured.DecryptSecured
	opts.ReleaseManifest = resolveBuildConfigPath(configDir, configured.ReleaseManifest)
	opts.ReleaseManifests = nil
	for _, path := range configured.ReleaseManifests {
		opts.ReleaseManifests = append(opts.ReleaseManifests, resolveBuildConfigPath(configDir, path))
	}
	opts.ReleaseID = configured.ReleaseID
	opts.ImageOverrides = append([]string(nil), configured.ImageOverrides...)
	if configured.ImagePolicy != "" {
		opts.ImagePolicy = configured.ImagePolicy
	}
	if configured.ImageReference != "" {
		opts.ImageReference = configured.ImageReference
	}
	opts.ForceImageTag = configured.ForceImageTag
	opts.ForceImagePrefix = configured.ForceImagePrefix
	opts.ResourcePolicyRoot = resolveBuildConfigPath(configDir, configured.ResourcePolicyRoot)
	opts.Profile = configured.ReplicaProfile
	opts.ProfilesFile = resolveBuildConfigPath(configDir, configured.ReplicaProfiles)
	opts.Down = append([]string(nil), configured.Down...)
	if configured.SyncProfile != "" {
		opts.SyncProfile = configured.SyncProfile
	}
	if configured.SyncPrefix != "" {
		opts.SyncPrefix = configured.SyncPrefix
	}
	opts.SyncSet = configured.SyncSet
	if configured.YAMLIndent != 0 {
		opts.YAMLIndent = configured.YAMLIndent
	}
	opts.LegacyApplyEnv = configured.LegacyApplyEnv
	opts.HelmEscapeAssets = configured.HelmEscapeAssets
	return opts
}

func resolveBuildConfigPath(configDir, value string) string {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return value
	}
	return filepath.Clean(filepath.Join(configDir, value))
}

func validateBuildConfigEnvironments(configured map[string]buildapp.Options, discovered []string) error {
	missing := []string{}
	known := make(map[string]bool, len(discovered))
	for _, name := range discovered {
		known[name] = true
		if _, ok := configured[name]; !ok {
			missing = append(missing, name)
		}
	}
	unknown := []string{}
	for name := range configured {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unknown)
	if len(missing) > 0 {
		return fmt.Errorf("build config is missing environments: %s", strings.Join(missing, ", "))
	}
	if len(unknown) > 0 {
		return fmt.Errorf("build config contains unknown environments: %s", strings.Join(unknown, ", "))
	}
	return nil
}
