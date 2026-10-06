package buildapp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// BuildContext describes configured build inputs without exposing fetched
// environment values or HTTP header contents.
type BuildContext struct {
	VariableSources       []string `json:"variable_sources"`
	EnvFile               string   `json:"env_file,omitempty"`
	EnvURLConfigured      bool     `json:"env_url_configured"`
	EnvURLHeaderCount     int      `json:"env_url_header_count"`
	EnvURLInsecure        bool     `json:"env_url_insecure"`
	DecryptSecured        bool     `json:"decrypt_secured"`
	NamespaceOverride     string   `json:"namespace_override,omitempty"`
	ReleaseManifest       string   `json:"release_manifest,omitempty"`
	ReleaseManifests      []string `json:"release_manifests,omitempty"`
	ReleaseID             string   `json:"release_id,omitempty"`
	ImagePolicy           string   `json:"image_policy"`
	ImageReference        string   `json:"image_reference"`
	ImageOverrideCount    int      `json:"image_override_count"`
	ForceTagConfigured    bool     `json:"force_image_tag_configured"`
	ForcePrefixConfigured bool     `json:"force_image_prefix_configured"`
	ResourcePolicyRoot    string   `json:"resource_policy_root,omitempty"`
	ReplicaProfile        string   `json:"replica_profile,omitempty"`
	ReplicaProfilesFile   string   `json:"replica_profiles_file,omitempty"`
	SyncMetadataProfile   string   `json:"sync_metadata_profile,omitempty"`
	SyncMetadataPrefix    string   `json:"sync_metadata_prefix,omitempty"`
	SyncSet               string   `json:"sync_set,omitempty"`
	ScaleDownCount        int      `json:"scale_down_count"`
	LegacyApplyEnv        bool     `json:"legacy_apply_env"`
	HelmEscapeAssets      bool     `json:"helm_escape_assets"`
	YAMLIndent            int      `json:"yaml_indent"`
}

type Inspection struct {
	Environment    string           `json:"environment"`
	Namespace      string           `json:"namespace,omitempty"`
	Context        BuildContext     `json:"context"`
	TemplateRefs   []TemplateRef    `json:"template_references,omitempty"`
	EffectiveError string           `json:"effective_error,omitempty"`
	Defaults       *SourceDocument  `json:"defaults,omitempty"`
	SharedAssets   *SourceDocument  `json:"shared_assets,omitempty"`
	Usage          []ReferenceUsage `json:"usage,omitempty"`
	Apps           []InspectedApp   `json:"apps"`
}

// TemplateRef is safe editor metadata. Build-variable values are never exposed.
type TemplateRef struct {
	Namespace            string   `json:"namespace"`
	Name                 string   `json:"name"`
	Sources              []string `json:"sources"`
	DependsOnApplication bool     `json:"depends_on_application,omitempty"`
}

type templateRefEntry struct {
	sources map[string]struct{}
	app     bool
}

type ReferenceUsage struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	App       string `json:"app"`
	AppFile   string `json:"app_file"`
	Container string `json:"container,omitempty"`
	Document  string `json:"document"`
	YAMLPath  string `json:"yaml_path"`
}

type InspectedApp struct {
	FileName       string          `json:"file_name"`
	Source         SourceDocument  `json:"source"`
	Effective      *EffectiveApp   `json:"effective,omitempty"`
	EffectiveError string          `json:"effective_error,omitempty"`
	Origins        []InspectOrigin `json:"origins"`
}

type SourceDocument struct {
	Path        string                 `json:"path"`
	ContentHash string                 `json:"content_hash"`
	RawContent  string                 `json:"raw_content"`
	Model       map[string]any         `json:"model,omitempty"`
	Fields      map[string]SourceField `json:"fields,omitempty"`
	ParseError  string                 `json:"parse_error,omitempty"`
}

type SourceField struct {
	Present  bool `json:"present"`
	Line     int  `json:"line,omitempty"`
	Column   int  `json:"column,omitempty"`
	RawValue any  `json:"raw_value,omitempty"`
}

type EffectiveApp struct {
	Name                   string               `json:"name"`
	Kind                   string               `json:"kind"`
	Replicas               int                  `json:"replicas"`
	Ignore                 bool                 `json:"ignore"`
	DisableSharedAssets    bool                 `json:"disable_shared_assets,omitempty"`
	DisableCreateService   bool                 `json:"disable_create_service,omitempty"`
	Strategy               string               `json:"strategy,omitempty"`
	SubdomainName          string               `json:"subdomain_name,omitempty"`
	MinAvailable           any                  `json:"min_available,omitempty"`
	MaxUnavailable         any                  `json:"max_unavailable,omitempty"`
	Labels                 map[string]any       `json:"labels,omitempty"`
	SelectorLabels         map[string]any       `json:"selector_labels,omitempty"`
	Annotations            map[string]any       `json:"annotations,omitempty"`
	PodAnnotations         map[string]any       `json:"pod_annotations,omitempty"`
	SecurityContext        map[string]any       `json:"security_context,omitempty"`
	TerminationGracePeriod *int                 `json:"termination_grace_period,omitempty"`
	ServiceAccount         string               `json:"service_account,omitempty"`
	WorkloadIdentity       map[string]any       `json:"workload_identity,omitempty"`
	PodInfo                map[string]any       `json:"pod_info,omitempty"`
	DownwardAPI            map[string]any       `json:"downward_api,omitempty"`
	Pod                    map[string]any       `json:"pod,omitempty"`
	DeploymentRaw          map[string]any       `json:"deployment_raw,omitempty"`
	PodRaw                 map[string]any       `json:"pod_raw,omitempty"`
	Autoscaling            map[string]any       `json:"autoscaling,omitempty"`
	RolloutOn              map[string]any       `json:"rollout_on,omitempty"`
	RuntimeAssets          []map[string]any     `json:"runtime_assets,omitempty"`
	Tools                  []map[string]any     `json:"tools,omitempty"`
	Registry               []map[string]any     `json:"registry,omitempty"`
	DNS                    []map[string]any     `json:"dns,omitempty"`
	Arch                   string               `json:"arch,omitempty"`
	NodeSelector           map[string]any       `json:"node_selector,omitempty"`
	Tolerations            []any                `json:"tolerations,omitempty"`
	Scheduling             map[string]any       `json:"scheduling,omitempty"`
	Containers             []EffectiveContainer `json:"containers"`
	Sidecars               []EffectiveContainer `json:"sidecars"`
	InitContainers         []map[string]any     `json:"init_containers,omitempty"`
}

type EffectiveContainer struct {
	Name                 string                  `json:"name"`
	Image                string                  `json:"image,omitempty"`
	Startup              map[string]any          `json:"startup,omitempty"`
	Envs                 []map[string]any        `json:"envs,omitempty"`
	Resources            map[string]any          `json:"resources,omitempty"`
	Ports                []map[string]any        `json:"ports,omitempty"`
	Probes               map[string]any          `json:"probes,omitempty"`
	Runtime              map[string]any          `json:"runtime,omitempty"`
	RuntimeAssetRefNames []string                `json:"runtime_asset_ref_names,omitempty"`
	Assets               []map[string]any        `json:"assets,omitempty"`
	Mounts               []map[string]any        `json:"mounts,omitempty"`
	EnvFrom              []map[string]any        `json:"env_from,omitempty"`
	SecurityContext      map[string]any          `json:"security_context,omitempty"`
	Raw                  map[string]any          `json:"raw,omitempty"`
	Origins              []InspectOrigin         `json:"origins,omitempty"`
	Fields               map[string]InspectField `json:"fields,omitempty"`
	EnvEntries           []InspectEnvEntry       `json:"env_entries,omitempty"`
}

type InspectEnvEntry struct {
	Name         string              `json:"name"`
	Effective    map[string]any      `json:"effective"`
	Origins      []InspectOrigin     `json:"origins,omitempty"`
	WriteTarget  *InspectWriteTarget `json:"write_target,omitempty"`
	Capabilities InspectCapabilities `json:"capabilities"`
}

type InspectOrigin struct {
	Kind           string `json:"kind"`
	Document       string `json:"document"`
	YAMLPath       string `json:"yaml_path"`
	DefinitionName string `json:"definition_name,omitempty"`
	Target         string `json:"target"`
}

type InspectField struct {
	Effective    any                 `json:"effective,omitempty"`
	Origins      []InspectOrigin     `json:"origins,omitempty"`
	WriteTarget  *InspectWriteTarget `json:"write_target,omitempty"`
	Capabilities InspectCapabilities `json:"capabilities"`
}

type InspectWriteTarget struct {
	Document               string `json:"document"`
	YAMLPath               string `json:"yaml_path"`
	Scope                  string `json:"scope"`
	SourceIndex            int    `json:"source_index"`
	NameAssertion          string `json:"name_assertion,omitempty"`
	ExpectedHash           string `json:"expected_hash"`
	ExpectedDependencyHash string `json:"expected_dependency_hash,omitempty"`
}

type InspectCapabilities struct {
	CanOverride   bool   `json:"can_override"`
	CanReset      bool   `json:"can_reset"`
	CanEditSource bool   `json:"can_edit_source"`
	Reason        string `json:"reason,omitempty"`
}

// DescribeBuildContext validates option combinations and returns a safe view.
func DescribeBuildContext(opts Options) (BuildContext, error) {
	sources, envFile, envURL, headers, insecure, decrypt, err := effectiveVarsSources(opts)
	if err != nil {
		return BuildContext{}, err
	}
	if normalizeImagePolicy(opts.ImagePolicy) != "fallback" && normalizeImagePolicy(opts.ImagePolicy) != "strict" {
		return BuildContext{}, fmt.Errorf("invalid image policy %q: expected fallback or strict", opts.ImagePolicy)
	}
	if _, err := releaseImageSelectionFromOptions(opts); err != nil {
		return BuildContext{}, err
	}
	indent, err := normalizeYAMLIndent(opts.YAMLIndent)
	if err != nil {
		return BuildContext{}, err
	}
	return BuildContext{
		VariableSources:       sources,
		EnvFile:               envFile,
		EnvURLConfigured:      envURL != "",
		EnvURLHeaderCount:     len(headers),
		EnvURLInsecure:        insecure,
		DecryptSecured:        decrypt,
		NamespaceOverride:     strings.TrimSpace(opts.Namespace),
		ReleaseManifest:       inspectionReleaseManifest(opts),
		ReleaseManifests:      releaseManifestPaths(opts),
		ReleaseID:             strings.TrimSpace(opts.ReleaseID),
		ImagePolicy:           normalizeImagePolicy(opts.ImagePolicy),
		ImageReference:        normalizeImageReference(opts.ImageReference),
		ImageOverrideCount:    len(opts.ImageOverrides),
		ForceTagConfigured:    strings.TrimSpace(opts.ForceImageTag) != "",
		ForcePrefixConfigured: strings.TrimSpace(opts.ForceImagePrefix) != "",
		ResourcePolicyRoot:    strings.TrimSpace(opts.ResourcePolicyRoot),
		ReplicaProfile:        strings.TrimSpace(opts.Profile),
		ReplicaProfilesFile:   strings.TrimSpace(opts.ProfilesFile),
		SyncMetadataProfile:   strings.TrimSpace(opts.SyncProfile),
		SyncMetadataPrefix:    strings.TrimSpace(opts.SyncPrefix),
		SyncSet:               strings.TrimSpace(opts.SyncSet),
		ScaleDownCount:        len(opts.Down),
		LegacyApplyEnv:        opts.LegacyApplyEnv,
		HelmEscapeAssets:      opts.HelmEscapeAssets,
		YAMLIndent:            indent,
	}, nil
}

// ResolveNamespace applies the same variable precedence as Build.
func ResolveNamespace(opts Options) (string, error) {
	if strings.TrimSpace(opts.Root) == "" {
		opts.Root = "environments"
	}
	vars, err := loadBuildVars(filepath.Join(opts.Root, opts.Environment), opts)
	if err != nil {
		return "", err
	}
	namespace := strings.TrimSpace(vars["NAMESPACE"])
	if namespace == "" {
		return "", fmt.Errorf("environment %q does not define NAMESPACE in the configured build variables", opts.Environment)
	}
	return namespace, nil
}

// Inspect returns source documents even when effective model evaluation fails.
func Inspect(opts Options) (Inspection, error) {
	if strings.TrimSpace(opts.Environment) == "" {
		return Inspection{}, fmt.Errorf("environment is required")
	}
	if strings.TrimSpace(opts.Root) == "" {
		opts.Root = "environments"
	}
	context, err := DescribeBuildContext(opts)
	if err != nil {
		return Inspection{}, err
	}
	envDir := filepath.Join(opts.Root, opts.Environment)
	appsDir := filepath.Join(envDir, "apps")
	appFiles, err := listAppFiles(appsDir)
	if err != nil {
		return Inspection{}, err
	}
	result := Inspection{Environment: opts.Environment, Context: context}
	for _, path := range appFiles {
		source, err := inspectSourceDocument(path, opts.Root)
		if err != nil {
			return Inspection{}, err
		}
		result.Apps = append(result.Apps, InspectedApp{FileName: filepath.Base(path), Source: source})
	}
	defaultsPath := filepath.Join(appsDir, "_defaults.yml")
	if !isFile(defaultsPath) {
		defaultsPath = filepath.Join(appsDir, "_defaults.yaml")
	}
	if isFile(defaultsPath) {
		defaultsSource, sourceErr := inspectSourceDocument(defaultsPath, opts.Root)
		if sourceErr != nil {
			return Inspection{}, sourceErr
		}
		result.Defaults = &defaultsSource
	}
	sharedAssetsPath := filepath.Join(envDir, "shared.assets.yml")
	if isFile(sharedAssetsPath) {
		sharedSource, sourceErr := inspectSourceDocument(sharedAssetsPath, opts.Root)
		if sourceErr != nil {
			return Inspection{}, sourceErr
		}
		result.SharedAssets = &sharedSource
	}
	result.TemplateRefs = sourceTemplateRefs(result)
	result.Usage = inspectReferenceUsage(result.Apps, result.Defaults)

	vars, err := loadBuildVars(envDir, opts)
	if err != nil {
		result.EffectiveError = err.Error()
		return result, nil
	}
	result.TemplateRefs = appendBuildTemplateRefs(result.TemplateRefs, vars, context.VariableSources)
	result.Namespace = strings.TrimSpace(vars["NAMESPACE"])
	sharedAssets, err := loadSharedAssets(envDir, vars, opts)
	if err != nil {
		result.EffectiveError = err.Error()
		return result, nil
	}
	defaultsSource := SourceDocument{}
	if result.Defaults != nil {
		defaultsSource = *result.Defaults
	}
	for index, path := range appFiles {
		app, appErr := loadApp(path, defaultsPath, vars)
		if appErr == nil {
			if app.Kind == "" {
				app.Kind = "Deployment"
			}
			apps := []appModel{app}
			appErr = prepareRuntimeAssetsForApps(apps, opts.Environment)
			if appErr == nil {
				appErr = applyResourcePolicies(apps, []string{path}, opts)
			}
			if appErr == nil {
				appErr = applyImageOverrides(apps, opts)
			}
			imageOverrideOrigins := map[imageKey]InspectOrigin{}
			if appErr == nil {
				imageOverrideOrigins, appErr = inspectImageOverrideOrigins(apps, opts)
			}
			if appErr == nil {
				appErr = applyReplicaProfile(apps, envDir, opts)
			}
			if appErr == nil {
				applyScaleDown(apps, opts.Down)
				appErr = validateApps(apps, sharedAssets)
			}
			if appErr == nil {
				result.Apps[index].Effective = effectiveInspectionApp(apps[0], result.Apps[index].Source, defaultsSource, opts, imageOverrideOrigins)
			}
		}
		if appErr != nil {
			result.Apps[index].EffectiveError = appErr.Error()
		}
		result.Apps[index].Origins = inspectionOrigins(result.Apps[index].Source, defaultsPath, opts)
	}
	normalizeInspectionUsage(result.Usage, result.Apps)
	return result, nil
}

func sourceTemplateRefs(inspection Inspection) []TemplateRef {
	entries := map[string]*templateRefEntry{}
	addDocument := func(document *SourceDocument, application bool) {
		if document == nil {
			return
		}
		items, _ := document.Model["vars"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			name, _ := item["name"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			key := "var:" + name
			current := entries[key]
			if current == nil {
				current = &templateRefEntry{sources: map[string]struct{}{}}
				entries[key] = current
			}
			current.sources[document.Path] = struct{}{}
			current.app = current.app || application
		}
		for _, match := range inspectEnvTemplatePattern.FindAllStringSubmatch(document.RawContent, -1) {
			name := match[1]
			key := "env:" + name
			current := entries[key]
			if current == nil {
				current = &templateRefEntry{sources: map[string]struct{}{}}
				entries[key] = current
			}
			current.sources[document.Path+" (reference)"] = struct{}{}
			current.app = current.app || application
		}
	}
	addDocument(inspection.Defaults, false)
	for index := range inspection.Apps {
		addDocument(&inspection.Apps[index].Source, true)
	}
	return sortedTemplateRefs(entries)
}

func appendBuildTemplateRefs(refs []TemplateRef, vars map[string]string, sources []string) []TemplateRef {
	label := "build variables"
	if len(sources) > 0 {
		label += " (" + strings.Join(sources, " + ") + ")"
	}
	includeAll := true
	for _, source := range sources {
		if source == "env" {
			includeAll = false
			break
		}
	}
	byKey := map[string]int{}
	for index := range refs {
		byKey[refs[index].Namespace+":"+refs[index].Name] = index
	}
	for name := range vars {
		key := "env:" + name
		index, exists := byKey[key]
		if !exists {
			if !includeAll {
				continue
			}
			refs = append(refs, TemplateRef{Namespace: "env", Name: name})
			index = len(refs) - 1
			byKey[key] = index
		}
		refs[index].Sources = appendUniqueString(refs[index].Sources, label)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Namespace != refs[j].Namespace {
			return refs[i].Namespace < refs[j].Namespace
		}
		return refs[i].Name < refs[j].Name
	})
	return refs
}

func sortedTemplateRefs(entries map[string]*templateRefEntry) []TemplateRef {
	refs := make([]TemplateRef, 0, len(entries))
	for key, item := range entries {
		sources := make([]string, 0, len(item.sources))
		for source := range item.sources {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		namespace, name, _ := strings.Cut(key, ":")
		refs = append(refs, TemplateRef{Namespace: namespace, Name: name, Sources: sources, DependsOnApplication: item.app})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Namespace != refs[j].Namespace {
			return refs[i].Namespace < refs[j].Namespace
		}
		return refs[i].Name < refs[j].Name
	})
	return refs
}

func inspectReferenceUsage(apps []InspectedApp, defaults *SourceDocument) []ReferenceUsage {
	usage := []ReferenceUsage{}
	for _, inspected := range apps {
		model := inspected.Source.Model
		appName := strings.TrimSpace(fmt.Sprint(model["name"]))
		if appName == "" || appName == "<nil>" {
			appName = strings.TrimSuffix(inspected.FileName, filepath.Ext(inspected.FileName))
		}
		appendRefs := func(kind string, value any, container, yamlPath string) {
			for _, ref := range stringValues(value) {
				usage = append(usage, ReferenceUsage{Kind: kind, Name: ref, App: appName, AppFile: inspected.FileName, Container: container, Document: inspected.Source.Path, YAMLPath: yamlPath})
			}
		}
		appendRefs("sidecar_definition", model["sidecar_ref_names"], "", "sidecar_ref_names")
		appendRefs("runtime_asset_definition", model["runtime_asset_ref_names"], "*", "runtime_asset_ref_names")
		for index, raw := range inspectAnySlice(model["containers"]) {
			container, _ := raw.(map[string]any)
			name := strings.TrimSpace(fmt.Sprint(container["name"]))
			base := fmt.Sprintf("containers[%d]", index)
			appendRefs("container_profile", container["profile_ref_names"], name, base+".profile_ref_names")
			appendRefs("runtime_asset_definition", container["runtime_asset_ref_names"], name, base+".runtime_asset_ref_names")
			usage = append(usage, inspectEnvReferenceUsage(inspected.Source.Path, inspected.FileName, appName, name, base+".envs", container["envs"])...)
		}
		for index, raw := range inspectAnySlice(model["sidecars"]) {
			container, _ := raw.(map[string]any)
			name := strings.TrimSpace(fmt.Sprint(container["name"]))
			base := fmt.Sprintf("sidecars[%d]", index)
			appendRefs("container_profile", container["profile_ref_names"], name, base+".profile_ref_names")
			appendRefs("runtime_asset_definition", container["runtime_asset_ref_names"], name, base+".runtime_asset_ref_names")
			usage = append(usage, inspectEnvReferenceUsage(inspected.Source.Path, inspected.FileName, appName, name, base+".envs", container["envs"])...)
		}
	}
	if defaults != nil {
		usage = expandReferenceUsage(usage, *defaults)
	}
	sort.Slice(usage, func(i, j int) bool {
		left := usage[i].Kind + "\x00" + usage[i].Name + "\x00" + usage[i].App + "\x00" + usage[i].Container
		right := usage[j].Kind + "\x00" + usage[j].Name + "\x00" + usage[j].App + "\x00" + usage[j].Container
		return left < right
	})
	return usage
}

func expandReferenceUsage(base []ReferenceUsage, defaults SourceDocument) []ReferenceUsage {
	usage := []ReferenceUsage{}
	queue := append([]ReferenceUsage(nil), base...)
	expanded := map[string]bool{}
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		usage = append(usage, item)
		key := item.Kind + "\x00" + item.Name + "\x00" + item.AppFile + "\x00" + item.Container
		if expanded[key] {
			continue
		}
		expanded[key] = true
		children := []ReferenceUsage{}
		switch item.Kind {
		case "runtime_asset_definition":
			definition := namedDefinition(defaults.Model["runtime_asset_definitions"], item.Name)
			source, _ := definition["source"].(map[string]any)
			if len(source) == 0 {
				assetDefaults, _ := defaults.Model["runtime_asset_defaults"].(map[string]any)
				source, _ = assetDefaults["source"].(map[string]any)
			}
			children = appendReferenceFieldUsage(children, item, source, "workload_identity_token_ref_name", "workload_identity_token", "runtime_asset_definitions.source")
			children = appendReferenceFieldUsage(children, item, source, "ca_shared_asset_ref_name", "shared_asset", "runtime_asset_definitions.source")
		case "container_profile":
			definition := namedDefinition(defaults.Model["container_profiles"], item.Name)
			body, _ := definition["defaults"].(map[string]any)
			children = append(children, inspectEnvReferenceUsage(item.Document, item.AppFile, item.App, item.Container, "container_profiles.defaults.envs", body["envs"])...)
		case "sidecar_definition":
			definition := namedDefinition(defaults.Model["sidecar_definitions"], item.Name)
			parent := item
			parent.Container = item.Name
			children = appendReferenceListUsage(children, parent, definition, "profile_ref_names", "container_profile", "sidecar_definitions.profile_ref_names")
			children = appendReferenceListUsage(children, parent, definition, "runtime_asset_ref_names", "runtime_asset_definition", "sidecar_definitions.runtime_asset_ref_names")
			children = append(children, inspectEnvReferenceUsage(item.Document, item.AppFile, item.App, item.Name, "sidecar_definitions.envs", definition["envs"])...)
		}
		queue = append(queue, children...)
	}
	return deduplicateReferenceUsage(usage)
}

func appendReferenceListUsage(usage []ReferenceUsage, parent ReferenceUsage, values map[string]any, field, kind, yamlPath string) []ReferenceUsage {
	for _, ref := range stringValues(values[field]) {
		usage = append(usage, ReferenceUsage{Kind: kind, Name: ref, App: parent.App, AppFile: parent.AppFile, Container: parent.Container, Document: parent.Document, YAMLPath: yamlPath})
	}
	return usage
}

func appendReferenceFieldUsage(usage []ReferenceUsage, parent ReferenceUsage, values map[string]any, field, kind, yamlPath string) []ReferenceUsage {
	ref := strings.TrimSpace(fmt.Sprint(values[field]))
	if ref == "" || ref == "<nil>" {
		return usage
	}
	return append(usage, ReferenceUsage{Kind: kind, Name: ref, App: parent.App, AppFile: parent.AppFile, Container: parent.Container, Document: parent.Document, YAMLPath: yamlPath + "." + field})
}

func namedDefinition(value any, name string) map[string]any {
	for _, raw := range inspectAnySlice(value) {
		definition, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(definition["name"])) == name {
			return definition
		}
	}
	return nil
}

func deduplicateReferenceUsage(usage []ReferenceUsage) []ReferenceUsage {
	seen := map[string]bool{}
	result := make([]ReferenceUsage, 0, len(usage))
	for _, item := range usage {
		key := item.Kind + "\x00" + item.Name + "\x00" + item.AppFile + "\x00" + item.Container + "\x00" + item.YAMLPath
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	return result
}

func normalizeInspectionUsage(usage []ReferenceUsage, apps []InspectedApp) {
	for index := range usage {
		for _, app := range apps {
			if app.FileName != usage[index].AppFile || app.Effective == nil {
				continue
			}
			rawAppName := strings.TrimSpace(fmt.Sprint(app.Source.Model["name"]))
			usage[index].App = app.Effective.Name
			if usage[index].Container == rawAppName && len(app.Effective.Containers) > 0 {
				usage[index].Container = app.Effective.Containers[0].Name
			}
			break
		}
	}
}

func inspectEnvReferenceUsage(document, appFile, app, container, base string, value any) []ReferenceUsage {
	usage := []ReferenceUsage{}
	for index, raw := range inspectAnySlice(value) {
		env, _ := raw.(map[string]any)
		for field, kind := range map[string]string{
			"workload_identity_token_ref_name": "workload_identity_token",
			"shared_asset_ref_name":            "shared_asset",
		} {
			if ref := strings.TrimSpace(fmt.Sprint(env[field])); ref != "" && ref != "<nil>" {
				usage = append(usage, ReferenceUsage{Kind: kind, Name: ref, App: app, AppFile: appFile, Container: container, Document: document, YAMLPath: fmt.Sprintf("%s[%d].%s", base, index, field)})
			}
		}
	}
	return usage
}

func inspectSourceDocument(path, root string) (SourceDocument, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return SourceDocument{}, err
	}
	digest := sha256.Sum256(content)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	doc := SourceDocument{Path: filepath.ToSlash(rel), ContentHash: hex.EncodeToString(digest[:]), RawContent: string(content)}
	model, fields, err := parseTemplateYAMLDocument(content)
	if err != nil {
		doc.ParseError = err.Error()
	} else {
		doc.Model = model
		doc.Fields = fields
	}
	return doc, nil
}

var inspectTemplatePattern = regexp.MustCompile(`\{\{[^{}\r\n]+\}\}`)
var inspectEnvTemplatePattern = regexp.MustCompile(`\{\{\s*env\s*:\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func parseTemplateYAML(content []byte) (map[string]any, error) {
	model, _, err := parseTemplateYAMLDocument(content)
	return model, err
}

func parseTemplateYAMLDocument(content []byte) (map[string]any, map[string]SourceField, error) {
	matches := inspectTemplatePattern.FindAll(content, -1)
	replaced := append([]byte(nil), content...)
	values := map[string]string{}
	for index, match := range matches {
		token := fmt.Sprintf("__KUBE_EDIT_TEMPLATE_%06d__", index)
		replaced = []byte(strings.Replace(string(replaced), string(match), token, 1))
		values[token] = string(match)
	}
	var node yaml.Node
	if err := yaml.Unmarshal(replaced, &node); err != nil {
		return nil, nil, err
	}
	var model map[string]any
	if err := node.Decode(&model); err != nil {
		return nil, nil, err
	}
	model = restoreTemplateValues(model, values).(map[string]any)
	fields := map[string]SourceField{}
	if len(node.Content) > 0 {
		collectSourceFields(node.Content[0], "", values, fields)
	}
	return model, fields, nil
}

func collectSourceFields(node *yaml.Node, path string, templates map[string]string, fields map[string]SourceField) {
	switch node.Kind {
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			keyNode, valueNode := node.Content[index], node.Content[index+1]
			key := restoreTemplateValues(keyNode.Value, templates).(string)
			next := key
			if path != "" {
				next = path + "." + key
			}
			fields[next] = SourceField{Present: true, Line: valueNode.Line, Column: valueNode.Column, RawValue: sourceNodeValue(valueNode, templates)}
			collectSourceFields(valueNode, next, templates, fields)
		}
	case yaml.SequenceNode:
		for index, item := range node.Content {
			next := fmt.Sprintf("%s[%d]", path, index)
			fields[next] = SourceField{Present: true, Line: item.Line, Column: item.Column, RawValue: sourceNodeValue(item, templates)}
			collectSourceFields(item, next, templates, fields)
		}
	}
}

func sourceNodeValue(node *yaml.Node, templates map[string]string) any {
	var value any
	if err := node.Decode(&value); err != nil {
		return nil
	}
	return restoreTemplateValues(value, templates)
}

func restoreTemplateValues(value any, templates map[string]string) any {
	switch typed := value.(type) {
	case string:
		for token, original := range templates {
			typed = strings.ReplaceAll(typed, token, original)
		}
		return typed
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[restoreTemplateValues(key, templates).(string)] = restoreTemplateValues(item, templates)
		}
		return result
	case []any:
		for index := range typed {
			typed[index] = restoreTemplateValues(typed[index], templates)
		}
	}
	return value
}

func inspectImageOverrideOrigins(apps []appModel, opts Options) (map[imageKey]InspectOrigin, error) {
	origins := map[imageKey]InspectOrigin{}
	selection, err := releaseImageSelectionFromOptions(opts)
	if err != nil {
		return nil, err
	}
	for _, path := range releaseManifestPaths(opts) {
		manifest, err := loadReleaseManifest(path)
		if err != nil {
			return nil, err
		}
		origin := InspectOrigin{Kind: "release_manifest", Document: filepath.ToSlash(path), YAMLPath: "images"}
		for _, app := range apps {
			for _, container := range app.Containers {
				image, err := manifest.imageFor(app.Name, container.Name, selection)
				if err != nil {
					return nil, err
				}
				if image != "" {
					origins[imageKey{App: app.Name, Container: container.Name}] = origin
				}
			}
			for _, sidecar := range app.Sidecars {
				image, err := manifest.sidecarImageFor(app.Name, sidecar.Name, selection)
				if err != nil {
					return nil, err
				}
				if image != "" {
					origins[imageKey{App: app.Name, Container: sidecar.Name}] = origin
				}
			}
		}
	}
	cliImages, err := parseImageOverrides(opts.ImageOverrides)
	if err != nil {
		return nil, err
	}
	for key := range cliImages {
		origins[key] = InspectOrigin{Kind: "cli_image_override", Document: "command line", YAMLPath: "--image"}
	}
	return origins, nil
}

func effectiveInspectionApp(app appModel, source, defaults SourceDocument, opts Options, imageOverrideOrigins map[imageKey]InspectOrigin) *EffectiveApp {
	result := &EffectiveApp{
		Name: app.Name, Kind: app.Kind, Replicas: app.Replicas, Ignore: app.Ignore,
		DisableSharedAssets: app.DisableSharedAssets, DisableCreateService: app.DisableCreateService,
		Strategy: app.Strategy, SubdomainName: app.SubdomainName,
		MinAvailable: app.MinAvailable, MaxUnavailable: app.MaxUnavailable,
		Labels: app.Labels, SelectorLabels: app.SelectorLabels,
		Annotations: app.Annotations, PodAnnotations: app.PodAnnotations,
		SecurityContext: app.SecurityContext, TerminationGracePeriod: app.TerminationGrace,
		ServiceAccount: app.ServiceAccount, WorkloadIdentity: genericYAMLMap(app.WorkloadIdentity),
		PodInfo: genericYAMLMap(app.PodInfo), DownwardAPI: genericYAMLMap(app.DownwardAPI),
		Pod: genericYAMLMap(app.Pod), DeploymentRaw: app.DeploymentRaw, PodRaw: app.PodRaw,
		Autoscaling: genericYAMLMap(app.Autoscaling), RolloutOn: genericYAMLMap(app.RolloutOn),
		RuntimeAssets: genericYAMLList(app.RuntimeAssets), Tools: genericYAMLList(app.Tools),
		Registry: genericYAMLList(app.Registry), DNS: genericYAMLList(app.DNS),
		Arch: app.Arch, NodeSelector: app.NodeSelector, Tolerations: app.Tolerations,
		Scheduling: genericYAMLMap(app.Scheduling), InitContainers: genericYAMLList(app.InitContainers),
	}
	for index, container := range app.Containers {
		effective := effectiveInspectionContainer(container)
		effective.Origins = inspectContainerOrigins(source, defaults, "containers", index, effective.Name)
		effective.Fields = inspectContainerFields(effective, source, defaults, opts, "containers", index, imageOverrideOrigins[imageKey{App: app.Name, Container: effective.Name}])
		effective.EnvEntries = inspectEnvEntries(effective, source, defaults, "containers", index)
		result.Containers = append(result.Containers, effective)
	}
	for index, sidecar := range app.Sidecars {
		effective := effectiveInspectionContainer(sidecar)
		effective.Origins = inspectContainerOrigins(source, defaults, "sidecars", index, effective.Name)
		effective.Fields = inspectContainerFields(effective, source, defaults, opts, "sidecars", index, imageOverrideOrigins[imageKey{App: app.Name, Container: effective.Name}])
		effective.EnvEntries = inspectEnvEntries(effective, source, defaults, "sidecars", index)
		result.Sidecars = append(result.Sidecars, effective)
	}
	return result
}

func inspectContainerOrigins(source, defaults SourceDocument, scope string, index int, effectiveName string) []InspectOrigin {
	target := fmt.Sprintf("%s[%d]", scope, index)
	origins := []InspectOrigin{}
	if scope == "containers" {
		container := sourceContainer(source, scope, index)
		for _, ref := range stringValues(container["profile_ref_names"]) {
			origins = append(origins, InspectOrigin{Kind: "container_profile", Document: defaults.Path, YAMLPath: "container_profiles", DefinitionName: ref, Target: target})
		}
		if container != nil {
			origins = append(origins, InspectOrigin{Kind: "local", Document: source.Path, YAMLPath: target, Target: target})
		}
		return origins
	}
	for _, ref := range stringValues(source.Model["sidecar_ref_names"]) {
		if ref == effectiveName {
			origins = append(origins, InspectOrigin{Kind: "sidecar_definition", Document: defaults.Path, YAMLPath: "sidecar_definitions", DefinitionName: ref, Target: target})
		}
	}
	for sourceIndex, raw := range inspectAnySlice(source.Model["sidecars"]) {
		container, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(container["name"])) == effectiveName {
			origins = append(origins, InspectOrigin{Kind: "local", Document: source.Path, YAMLPath: fmt.Sprintf("sidecars[%d]", sourceIndex), DefinitionName: effectiveName, Target: target})
		}
	}
	return origins
}

func inspectEnvEntries(container EffectiveContainer, source, defaults SourceDocument, scope string, index int) []InspectEnvEntry {
	result := make([]InspectEnvEntry, 0, len(container.Envs))
	for _, env := range container.Envs {
		name := strings.TrimSpace(fmt.Sprint(env["name"]))
		origins, localIndex := inspectEnvOrigins(name, container.Name, source, defaults, scope, index)
		sourceIndex := index
		if scope == "sidecars" {
			sourceIndex = sourceContainerIndexByName(source, scope, container.Name)
		}
		selectedSharedSidecar := scope == "sidecars" && stringListContains(stringValues(source.Model["sidecar_ref_names"]), container.Name)
		canEdit := scope == "containers" || sourceIndex >= 0 || selectedSharedSidecar
		reason := ""
		if !canEdit {
			reason = "sidecar has no safe local name selector"
		}
		path := fmt.Sprintf("%s[%d].envs[name=%s]", scope, index, name)
		var writeTarget *InspectWriteTarget
		if canEdit {
			writeTarget = &InspectWriteTarget{
				Document: source.Path, YAMLPath: path, Scope: "app_" + strings.TrimSuffix(scope, "s") + "_env",
				SourceIndex: sourceIndex, NameAssertion: container.Name, ExpectedHash: source.ContentHash,
			}
			if scope == "sidecars" {
				writeTarget.ExpectedDependencyHash = defaults.ContentHash
			}
		}
		result = append(result, InspectEnvEntry{
			Name: name, Effective: env, Origins: origins,
			WriteTarget:  writeTarget,
			Capabilities: InspectCapabilities{CanOverride: canEdit, CanReset: canEdit && localIndex >= 0, CanEditSource: canEdit, Reason: reason},
		})
	}
	return result
}

func stringListContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func inspectEnvOrigins(name, effectiveContainerName string, source, defaults SourceDocument, scope string, index int) ([]InspectOrigin, int) {
	target := fmt.Sprintf("%s[%d].envs[name=%s]", scope, index, name)
	origins := []InspectOrigin{}
	sourceIndex := index
	if scope == "sidecars" {
		sourceIndex = sourceContainerIndexByName(source, scope, effectiveContainerName)
	}
	container := sourceContainer(source, scope, sourceIndex)
	if scope == "containers" {
		for _, selector := range []string{"*", effectiveContainerName} {
			for _, raw := range inspectAnySlice(defaults.Model["container_envs"]) {
				group, _ := raw.(map[string]any)
				groupSelector := strings.TrimSpace(fmt.Sprint(group["container_ref_name"]))
				if groupSelector == selector && namedItemIndex(group["envs"], name) >= 0 {
					origins = append(origins, InspectOrigin{Kind: "container_env_defaults", Document: defaults.Path, YAMLPath: "container_envs", DefinitionName: groupSelector, Target: target})
				}
			}
		}
		for _, ref := range stringValues(container["profile_ref_names"]) {
			if definitionHasNamedItem(defaults, "container_profiles", ref, "envs", name) {
				origins = append(origins, InspectOrigin{Kind: "container_profile", Document: defaults.Path, YAMLPath: "container_profiles", DefinitionName: ref, Target: target})
			}
		}
	} else {
		for _, ref := range stringValues(source.Model["sidecar_ref_names"]) {
			if ref == effectiveContainerName && definitionHasNamedItem(defaults, "sidecar_definitions", ref, "envs", name) {
				origins = append(origins, InspectOrigin{Kind: "sidecar_definition", Document: defaults.Path, YAMLPath: "sidecar_definitions", DefinitionName: ref, Target: target})
			}
		}
	}
	localIndex := namedItemIndex(container["envs"], name)
	if localIndex >= 0 {
		origins = append(origins, InspectOrigin{Kind: "local", Document: source.Path, YAMLPath: fmt.Sprintf("%s[%d].envs[%d]", scope, sourceIndex, localIndex), DefinitionName: name, Target: target})
	}
	return origins, localIndex
}

func effectiveInspectionContainer(container containerSpec) EffectiveContainer {
	return EffectiveContainer{
		Name: container.Name, Image: container.Image,
		Startup: genericYAMLMap(container.Startup), Envs: genericYAMLList(effectiveContainerEnvs(container)),
		Resources: genericNestedMap(container.Resources), Ports: genericYAMLList(container.Ports),
		Probes: genericYAMLMap(container.Probes), Runtime: genericYAMLMap(container.Runtime),
		RuntimeAssetRefNames: container.RuntimeAssetRefs, Assets: genericYAMLList(container.Assets),
		Mounts: genericYAMLList(container.Mounts), EnvFrom: genericYAMLList(container.EnvFrom),
		SecurityContext: container.SecurityContext, Raw: container.Raw,
	}
}

func inspectContainerFields(container EffectiveContainer, source, defaults SourceDocument, opts Options, scope string, index int, imageOverrideOrigin InspectOrigin) map[string]InspectField {
	values := map[string]any{
		"image": container.Image, "startup": container.Startup, "envs": container.Envs,
		"resources": container.Resources, "ports": container.Ports, "probes": container.Probes,
		"runtime": container.Runtime, "runtime_asset_ref_names": container.RuntimeAssetRefNames,
		"assets": container.Assets, "mounts": container.Mounts, "env_from": container.EnvFrom,
		"security_context": container.SecurityContext, "raw": container.Raw,
	}
	result := make(map[string]InspectField, len(values))
	for field, value := range values {
		path := fmt.Sprintf("%s[%d].%s", scope, index, field)
		_, local := source.Fields[path]
		sourceIndex := index
		if scope == "sidecars" {
			sourceIndex = sourceContainerIndexByName(source, scope, container.Name)
			localSidecar := sourceContainer(source, scope, sourceIndex)
			_, local = localSidecar[field]
			path = fmt.Sprintf("sidecars[name=%s].%s", container.Name, field)
		}
		origins := inspectFieldOrigins(source, defaults, opts, scope, index, sourceIndex, field, local, container.Name)
		if field == "image" && imageOverrideOrigin.Kind != "" {
			imageOverrideOrigin.Target = path
			origins = append(origins, imageOverrideOrigin)
		}
		reason := ""
		canEdit := scope == "containers"
		if scope == "sidecars" && (field == "resources" || field == "startup") {
			selectedShared := stringListContains(stringValues(source.Model["sidecar_ref_names"]), container.Name)
			canEdit = sourceIndex >= 0 || selectedShared
			if strings.TrimSpace(opts.ResourcePolicyRoot) != "" {
				canEdit = false
				reason = "external resource policy is authoritative"
			}
		}
		if !canEdit && reason == "" {
			reason = "field has no safe app-local writer"
		}
		var writeTarget *InspectWriteTarget
		if canEdit {
			writeTarget = &InspectWriteTarget{
				Document: source.Path, YAMLPath: path, Scope: "app_" + strings.TrimSuffix(scope, "s"),
				SourceIndex: sourceIndex, NameAssertion: container.Name, ExpectedHash: source.ContentHash,
			}
			if scope == "sidecars" {
				writeTarget.ExpectedDependencyHash = defaults.ContentHash
			}
		}
		result[field] = InspectField{
			Effective:    value,
			Origins:      origins,
			WriteTarget:  writeTarget,
			Capabilities: InspectCapabilities{CanOverride: canEdit, CanReset: canEdit && local, CanEditSource: canEdit, Reason: reason},
		}
	}
	return result
}

func inspectFieldOrigins(source, defaults SourceDocument, opts Options, scope string, index, sourceIndex int, field string, local bool, effectiveContainerName string) []InspectOrigin {
	target := fmt.Sprintf("%s[%d].%s", scope, index, field)
	origins := []InspectOrigin{}
	container := sourceContainer(source, scope, sourceIndex)
	if scope == "containers" {
		for _, ref := range stringValues(container["profile_ref_names"]) {
			if definitionHasField(defaults, "container_profiles", ref, field) {
				origins = append(origins, InspectOrigin{Kind: "container_profile", Document: defaults.Path, YAMLPath: "container_profiles", DefinitionName: ref, Target: target})
			}
		}
		if field == "envs" && len(inspectAnySlice(defaults.Model["container_envs"])) > 0 {
			origins = append(origins, InspectOrigin{Kind: "container_env_defaults", Document: defaults.Path, YAMLPath: "container_envs", Target: target})
		}
	} else {
		definition := namedDefinition(defaults.Model["sidecar_definitions"], effectiveContainerName)
		if definition != nil {
			definitionName := strings.TrimSpace(fmt.Sprint(definition["name"]))
			for _, ref := range stringValues(definition["profile_ref_names"]) {
				if definitionHasField(defaults, "container_profiles", ref, field) {
					origins = append(origins, InspectOrigin{Kind: "container_profile", Document: defaults.Path, YAMLPath: "container_profiles", DefinitionName: ref, Target: target})
				}
			}
			if _, present := definition[field]; present {
				origins = append(origins, InspectOrigin{Kind: "sidecar_definition", Document: defaults.Path, YAMLPath: "sidecar_definitions", DefinitionName: definitionName, Target: target})
			}
		}
	}
	if local {
		localPath := fmt.Sprintf("%s[%d].%s", scope, sourceIndex, field)
		origins = append(origins, InspectOrigin{Kind: "local", Document: source.Path, YAMLPath: localPath, Target: target})
	}
	if field == "resources" && strings.TrimSpace(opts.ResourcePolicyRoot) != "" {
		origins = append(origins, InspectOrigin{Kind: "external_resource_policy", Document: filepath.ToSlash(filepath.Join(opts.ResourcePolicyRoot, opts.Environment, "apps", filepath.Base(source.Path))), YAMLPath: scope, Target: target})
	}
	return origins
}

func sourceContainer(source SourceDocument, scope string, index int) map[string]any {
	items := inspectAnySlice(source.Model[scope])
	if index < 0 || index >= len(items) {
		return nil
	}
	result, _ := items[index].(map[string]any)
	return result
}

func sourceContainerName(source SourceDocument, scope string, index int) string {
	container := sourceContainer(source, scope, index)
	if container == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(container["name"]))
}

func sourceContainerIndexByName(source SourceDocument, scope, name string) int {
	for index, raw := range inspectAnySlice(source.Model[scope]) {
		container, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(container["name"])) == name {
			return index
		}
	}
	return -1
}

func definitionHasField(defaults SourceDocument, catalog, name, field string) bool {
	for _, raw := range inspectAnySlice(defaults.Model[catalog]) {
		definition, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(definition["name"])) != name {
			continue
		}
		body, _ := definition["defaults"].(map[string]any)
		if catalog == "sidecar_definitions" {
			body = definition
		}
		_, ok := body[field]
		return ok
	}
	return false
}

func definitionHasNamedItem(defaults SourceDocument, catalog, definitionName, field, itemName string) bool {
	for _, raw := range inspectAnySlice(defaults.Model[catalog]) {
		definition, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(definition["name"])) != definitionName {
			continue
		}
		body, _ := definition["defaults"].(map[string]any)
		if catalog == "sidecar_definitions" {
			body = definition
		}
		return namedItemIndex(body[field], itemName) >= 0
	}
	return false
}

func namedItemIndex(value any, name string) int {
	for index, raw := range inspectAnySlice(value) {
		item, _ := raw.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(item["name"])) == name {
			return index
		}
	}
	return -1
}

func genericYAMLMap(value any) map[string]any {
	data, _ := yaml.Marshal(value)
	var result map[string]any
	_ = yaml.Unmarshal(data, &result)
	return compactMap(result)
}

func genericNestedMap(value any) map[string]any { return genericYAMLMap(value) }

func genericYAMLList(value any) []map[string]any {
	data, _ := yaml.Marshal(value)
	var result []map[string]any
	_ = yaml.Unmarshal(data, &result)
	for index := range result {
		result[index] = compactMap(result[index])
	}
	return result
}

func compactMap(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		switch typed := item.(type) {
		case map[string]any:
			if nested := compactMap(typed); len(nested) > 0 {
				result[key] = nested
			}
		case []any:
			if len(typed) > 0 {
				result[key] = typed
			}
		case string:
			if typed != "" {
				result[key] = typed
			}
		case nil:
		default:
			result[key] = item
		}
	}
	return result
}

func inspectionOrigins(source SourceDocument, defaultsPath string, opts Options) []InspectOrigin {
	if source.Model == nil {
		return nil
	}
	origins := []InspectOrigin{}
	doc := source.Path
	for index, raw := range inspectAnySlice(source.Model["containers"]) {
		container, _ := raw.(map[string]any)
		target := fmt.Sprintf("containers[%d]", index)
		for _, ref := range stringValues(container["profile_ref_names"]) {
			origins = append(origins, InspectOrigin{Kind: "container_profile", Document: filepath.ToSlash(defaultsPath), YAMLPath: "container_profiles", DefinitionName: ref, Target: target})
		}
		origins = append(origins, InspectOrigin{Kind: "local", Document: doc, YAMLPath: target, Target: target})
	}
	for _, ref := range stringValues(source.Model["sidecar_ref_names"]) {
		origins = append(origins, InspectOrigin{Kind: "sidecar_definition", Document: filepath.ToSlash(defaultsPath), YAMLPath: "sidecar_definitions", DefinitionName: ref, Target: "sidecars"})
	}
	for index := range inspectAnySlice(source.Model["sidecars"]) {
		target := fmt.Sprintf("sidecars[%d]", index)
		origins = append(origins, InspectOrigin{Kind: "local", Document: doc, YAMLPath: target, Target: target})
	}
	for _, ref := range stringValues(source.Model["runtime_asset_ref_names"]) {
		origins = append(origins, InspectOrigin{Kind: "runtime_asset_definition", Document: filepath.ToSlash(defaultsPath), YAMLPath: "runtime_asset_definitions", DefinitionName: ref, Target: "containers"})
	}
	if strings.TrimSpace(opts.ResourcePolicyRoot) != "" {
		origins = append(origins, InspectOrigin{Kind: "external_resource_policy", Document: filepath.ToSlash(filepath.Join(opts.ResourcePolicyRoot, opts.Environment, "apps", filepath.Base(source.Path))), YAMLPath: "containers", Target: "containers+sidecars"})
	}
	for _, path := range releaseManifestPaths(opts) {
		origins = append(origins, InspectOrigin{Kind: "release_manifest", Document: filepath.ToSlash(path), YAMLPath: "images", Target: "images"})
	}
	return origins
}

func stringValues(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func inspectAnySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func inspectionReleaseManifest(opts Options) string {
	paths := releaseManifestPaths(opts)
	if len(paths) == 1 {
		return paths[0]
	}
	return ""
}
