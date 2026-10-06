package buildapp

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const appLabel = "app.kubernetes.io/name"
const releaseIDLabel = "app.kubernetes.io/release-id"
const customerReleaseIDLabel = "app.kubernetes.io/customer-release-id"
const defaultReleaseIDEnvName = "RELEASE_ID"
const defaultCustomerReleaseIDEnvName = "CUSTOMER_RELEASE_ID"

var releaseLabelValuePattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
var releaseContextNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var releaseContextEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Options struct {
	Environment                    string
	Namespace                      string
	Root                           string
	ResourcePolicyRoot             string
	Target                         string
	Profile                        string
	ProfilesFile                   string
	Inventory                      bool
	DecryptSecured                 bool
	EncjsonPath                    string
	EncjsonLegacyPath              string
	EncjsonKeydir                  string
	EnvFile                        string
	EnvURL                         string
	EnvURLHeaders                  []string
	EnvURLInsecure                 bool
	VarsSources                    []string
	LegacyApplyEnv                 bool
	HelmEscapeAssets               bool
	ReleaseManifest                string
	ReleaseManifests               []string
	ReleaseID                      string
	CustomerReleaseID              string
	ReleaseContextName             string
	ReleaseIDEnvName               string
	CustomerReleaseIDEnvName       string
	DisableReleaseContextConfigMap bool
	DisableReleaseContextEnv       bool
	ImageOverrides                 []string
	ImagePolicy                    string
	ImageReference                 string
	ForceImageTag                  string
	ForceImagePrefix               string
	SyncProfile                    string
	SyncPrefix                     string
	SyncSet                        string
	Down                           []string
	YAMLIndent                     int
}

type Result struct {
	Deployments     []string
	Services        []string
	Assets          []string
	Budgets         []string
	Autoscaling     []string
	Externals       []string
	ServiceAccounts []string
	Events          []BuildEvent
}

type BuildEvent struct {
	Type      string `json:"type"`
	App       string `json:"app,omitempty"`
	Container string `json:"container,omitempty"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Count     int    `json:"count,omitempty"`
	Path      string `json:"path,omitempty"`
}

type ResourceSummaryData struct {
	Environment string                `json:"environment"`
	Items       []ResourceSummaryItem `json:"items"`
	Totals      ResourceSummaryTotals `json:"totals"`
}

type ResourceSummaryItem struct {
	App                   string  `json:"app"`
	Replicas              int     `json:"replicas"`
	Container             string  `json:"container"`
	CPURequest            string  `json:"cpu_request"`
	CPULimit              string  `json:"cpu_limit"`
	MemoryRequest         string  `json:"memory_request"`
	MemoryLimit           string  `json:"memory_limit"`
	JavaXms               string  `json:"java_xms"`
	JavaXmx               string  `json:"java_xmx"`
	CPURequestCores       float64 `json:"cpu_request_cores"`
	CPULimitCores         float64 `json:"cpu_limit_cores"`
	MemoryRequestMiB      float64 `json:"memory_request_mib"`
	MemoryLimitMiB        float64 `json:"memory_limit_mib"`
	TotalCPURequestCores  float64 `json:"total_cpu_request_cores"`
	TotalCPULimitCores    float64 `json:"total_cpu_limit_cores"`
	TotalMemoryRequestMiB float64 `json:"total_memory_request_mib"`
	TotalMemoryLimitMiB   float64 `json:"total_memory_limit_mib"`
}

type ResourceSummaryTotals struct {
	Apps             int     `json:"apps"`
	Containers       int     `json:"containers"`
	Replicas         int     `json:"replicas"`
	CPURequestCores  float64 `json:"cpu_request_cores"`
	CPULimitCores    float64 `json:"cpu_limit_cores"`
	MemoryRequestMiB float64 `json:"memory_request_mib"`
	MemoryLimitMiB   float64 `json:"memory_limit_mib"`
}

type envFile struct {
	Environment map[string]any `json:"environment"`
}

type variable struct {
	Name   string `yaml:"name"`
	Value  string `yaml:"value"`
	Remove bool   `yaml:"remove"`
}

type appModel struct {
	Vars                 []variable           `yaml:"vars"`
	Name                 string               `yaml:"name"`
	Kind                 string               `yaml:"kind"`
	Ignore               bool                 `yaml:"ignore"`
	DisableSharedAssets  bool                 `yaml:"disable_shared_assets"`
	DisableCreateService bool                 `yaml:"disable_create_service"`
	Strategy             string               `yaml:"strategy"`
	SubdomainName        string               `yaml:"subdomain_name"`
	MinAvailable         any                  `yaml:"min_available"`
	MaxUnavailable       any                  `yaml:"max_unavailable"`
	Replicas             int                  `yaml:"replicas"`
	Labels               map[string]any       `yaml:"labels"`
	SelectorLabels       map[string]any       `yaml:"selector_labels"`
	Annotations          map[string]any       `yaml:"annotations"`
	PodAnnotations       map[string]any       `yaml:"pod_annotations"`
	SecurityContext      map[string]any       `yaml:"security_context"`
	TerminationGrace     *int                 `yaml:"termination_grace_period"`
	ServiceAccount       string               `yaml:"service_account"`
	WorkloadIdentity     workloadIdentitySpec `yaml:"workload_identity"`
	PodInfo              podInfoSpec          `yaml:"pod_info"`
	DownwardAPI          downwardAPISpec      `yaml:"downward_api"`
	Pod                  appPodSpec           `yaml:"pod"`
	DeploymentRaw        map[string]any       `yaml:"deployment_raw"`
	PodRaw               map[string]any       `yaml:"pod_raw"`
	Autoscaling          autoscalingSpec      `yaml:"autoscaling"`
	RolloutOn            rolloutOnSpec        `yaml:"rollout_on"`
	SidecarRefs          []string             `yaml:"sidecar_ref_names"`
	RuntimeAssetRefs     []string             `yaml:"runtime_asset_ref_names"`
	RuntimeAssetDefaults runtimeAssetDefaults `yaml:"runtime_asset_defaults"`
	RuntimeAssetDefs     []runtimeAssetSpec   `yaml:"runtime_asset_definitions"`
	LegacyRuntimeAssets  []runtimeAssetSpec   `yaml:"runtime_assets"`
	RuntimeAssets        []runtimeAssetSpec   `yaml:"-"`
	Tools                []toolSpec           `yaml:"tools"`
	InitContainers       []initContainerSpec  `yaml:"init_containers"`
	Registry             []registrySpec       `yaml:"registry"`
	DNS                  []hostAliasSpec      `yaml:"dns"`
	Arch                 string               `yaml:"arch"`
	NodeSelector         map[string]any       `yaml:"node_selector"`
	Tolerations          []any                `yaml:"tolerations"`
	Scheduling           schedulingSpec       `yaml:"scheduling"`
	Containers           []containerSpec      `yaml:"containers"`
	Sidecars             []containerSpec      `yaml:"sidecars"`
}

type rolloutOnSpec struct {
	Checksums map[string]rolloutChecksumSpec `yaml:"checksums"`
}

type rolloutChecksumSpec struct {
	Files []string `yaml:"files"`
}

type registrySpec struct {
	SecretName string `yaml:"secret_name"`
}

type hostAliasSpec struct {
	IP        string   `yaml:"ip"`
	Hostnames []string `yaml:"hostnames"`
}

type workloadIdentitySpec struct {
	ServiceAccount workloadServiceAccountSpec  `yaml:"service_account"`
	Tokens         []workloadIdentityTokenSpec `yaml:"tokens"`
}

type workloadServiceAccountSpec struct {
	Create    bool   `yaml:"create"`
	Name      string `yaml:"name"`
	Automount *bool  `yaml:"automount"`
}

type workloadIdentityTokenSpec struct {
	Name              string `yaml:"name"`
	Audience          string `yaml:"audience"`
	MountPath         string `yaml:"mount_path"`
	Path              string `yaml:"path"`
	ExpirationSeconds int    `yaml:"expiration_seconds"`
}

type runtimeAssetSpec struct {
	Name              string                  `yaml:"name"`
	Source            runtimeAssetSourceSpec  `yaml:"source"`
	Volume            runtimeAssetVolumeSpec  `yaml:"volume"`
	AppRefNames       []string                `yaml:"app_ref_names"`
	ContainerRefNames []string                `yaml:"container_ref_names"`
	LegacyApps        *[]string               `yaml:"apps"`
	LegacyContainers  *[]string               `yaml:"containers"`
	Files             []runtimeAssetFileSpec  `yaml:"files"`
	Fetcher           runtimeAssetFetcherSpec `yaml:"fetcher"`
}

type runtimeAssetDefaults struct {
	Source  runtimeAssetSourceSpec  `yaml:"source"`
	Volume  runtimeAssetVolumeSpec  `yaml:"volume"`
	Fetcher runtimeAssetFetcherSpec `yaml:"fetcher"`
}

type runtimeAssetSourceSpec struct {
	Type                         string `yaml:"type"`
	BaseURL                      string `yaml:"base_url"`
	Tenant                       string `yaml:"tenant"`
	Environment                  string `yaml:"environment"`
	Label                        string `yaml:"label"`
	WorkloadIdentityTokenRefName string `yaml:"workload_identity_token_ref_name"`
	CASharedAssetRefName         string `yaml:"ca_shared_asset_ref_name"`
	CAFile                       string `yaml:"ca_file"`
	LegacyToken                  string `yaml:"token"`
	TimeoutSeconds               int    `yaml:"timeout_seconds"`
	InsecureUpstreamTLS          bool   `yaml:"insecure_upstream_tls"`
}

type runtimeAssetVolumeSpec struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mount_path"`
	Medium    string `yaml:"medium"`
	SizeLimit string `yaml:"size_limit"`
}

type runtimeAssetFileSpec struct {
	Source string `yaml:"source" json:"source"`
	Target string `yaml:"target" json:"target"`
	Mode   string `yaml:"mode" json:"mode"`
	SHA256 string `yaml:"sha256" json:"sha256,omitempty"`
}

type runtimeAssetFetcherSpec struct {
	Image           string                    `yaml:"image"`
	ImagePullPolicy string                    `yaml:"image_pull_policy"`
	Command         string                    `yaml:"command"`
	Resources       map[string]map[string]any `yaml:"resources"`
	SecurityContext map[string]any            `yaml:"security_context"`
}

type podInfoSpec struct {
	Enabled   bool   `yaml:"enabled"`
	MountPath string `yaml:"mount_path"`
}

type downwardAPISpec struct {
	Mounts []downwardAPIMountSpec `yaml:"mounts"`
}

type downwardAPIMountSpec struct {
	Name      string                `yaml:"name"`
	MountPath string                `yaml:"mount_path"`
	Items     []downwardAPIItemSpec `yaml:"items"`
}

type downwardAPIItemSpec struct {
	Path      string `yaml:"path"`
	FieldPath string `yaml:"field_path"`
}

type appPodSpec struct {
	ShareProcessNamespace bool `yaml:"share_process_namespace"`
}

type schedulingSpec struct {
	Arch         string           `yaml:"arch"`
	NodeSelector map[string]any   `yaml:"node_selector"`
	Tolerations  []any            `yaml:"tolerations"`
	Affinity     map[string]any   `yaml:"affinity"`
	Spread       spreadSpec       `yaml:"spread"`
	AntiAffinity antiAffinitySpec `yaml:"anti_affinity"`
}

type autoscalingSpec struct {
	Enabled     bool                  `yaml:"enabled"`
	MinReplicas int                   `yaml:"min_replicas"`
	MaxReplicas int                   `yaml:"max_replicas"`
	CPU         autoscalingMetricSpec `yaml:"cpu"`
	Memory      autoscalingMetricSpec `yaml:"memory"`
	Raw         map[string]any        `yaml:"raw"`
}

type autoscalingMetricSpec struct {
	AverageUtilization int `yaml:"average_utilization"`
}

type spreadSpec struct {
	By                string `yaml:"by"`
	Topology          string `yaml:"topology"`
	MaxSkew           int    `yaml:"max_skew"`
	WhenUnsatisfiable string `yaml:"when_unsatisfiable"`
}

type antiAffinitySpec struct {
	Self     string `yaml:"self"`
	Topology string `yaml:"topology"`
}

type containerSpec struct {
	Name                 string                    `yaml:"name"`
	ProfileRefNames      []string                  `yaml:"profile_ref_names"`
	RuntimeAssetRefs     []string                  `yaml:"runtime_asset_ref_names"`
	Image                string                    `yaml:"image"`
	ImagePullPolicy      string                    `yaml:"image_pull_policy"`
	Assets               []assetSpec               `yaml:"assets"`
	Mounts               []mountSpec               `yaml:"mounts"`
	LegacyEnvVars        []envVar                  `yaml:"env_vars"`
	Envs                 []envVar                  `yaml:"envs"`
	MTLS                 mtlsSpec                  `yaml:"mtls"`
	Ports                []portSpec                `yaml:"ports"`
	Health               healthSpec                `yaml:"health"`
	Probe                probeSpec                 `yaml:"probe"`
	Probes               probesSpec                `yaml:"probes"`
	Lifecycle            lifecycleSpec             `yaml:"lifecycle"`
	SimpleInit           simpleInitSpec            `yaml:"simple_init"`
	Startup              startupSpec               `yaml:"startup"`
	EnableCgroupExporter bool                      `yaml:"enable_cgroup_exporter"`
	Resources            map[string]map[string]any `yaml:"resources"`
	Runtime              runtimeSpec               `yaml:"runtime"`
	SecurityContext      map[string]any            `yaml:"security_context"`
	EnvFrom              []envFromSpec             `yaml:"env_from"`
	Raw                  map[string]any            `yaml:"raw"`
}

type runtimeSpec struct {
	Java javaRuntimeSpec `yaml:"java"`
}

type javaRuntimeSpec struct {
	Xms    string                `yaml:"xms"`
	Xmx    string                `yaml:"xmx"`
	Opts   []string              `yaml:"opts"`
	Export javaRuntimeExportSpec `yaml:"export"`
}

type javaRuntimeExportSpec struct {
	EnvName string `yaml:"env_name"`
}

type initContainerSpec struct {
	Name            string                    `yaml:"name"`
	Image           string                    `yaml:"image"`
	ImagePullPolicy string                    `yaml:"image_pull_policy"`
	Command         []string                  `yaml:"command"`
	Arguments       []string                  `yaml:"arguments"`
	Mounts          []mountSpec               `yaml:"mounts"`
	LegacyEnvVars   []envVar                  `yaml:"env_vars"`
	Envs            []envVar                  `yaml:"envs"`
	EnvFrom         []envFromSpec             `yaml:"env_from"`
	Resources       map[string]map[string]any `yaml:"resources"`
	SecurityContext map[string]any            `yaml:"security_context"`
	Raw             map[string]any            `yaml:"raw"`
}

type simpleInitSpec struct {
	Enabled bool               `yaml:"enabled"`
	Exec    simpleInitExecSpec `yaml:"exec"`
}

type lifecycleSpec struct {
	PreStop lifecycleHandlerSpec `yaml:"pre_stop"`
}

type lifecycleHandlerSpec struct {
	Command []string       `yaml:"command"`
	HTTP    httpHealthSpec `yaml:"http"`
	Raw     map[string]any `yaml:"raw"`
}

type simpleInitExecSpec struct {
	Command []string `yaml:"command"`
}

type envFromSpec struct {
	ConfigMap    string         `yaml:"config_map"`
	ConfigMapRef map[string]any `yaml:"configMapRef"`
	Secret       string         `yaml:"secret"`
	SecretRef    map[string]any `yaml:"secretRef"`
	Prefix       string         `yaml:"prefix"`
	Raw          map[string]any `yaml:"raw"`
}

type healthSpec struct {
	HTTP    httpHealthSpec `yaml:"http"`
	Command []string       `yaml:"command"`
	Delay   *int           `yaml:"delay"`
	Period  *int           `yaml:"period"`
	Timeout *int           `yaml:"timeout"`
	Success *int           `yaml:"success"`
	Failure *int           `yaml:"failure"`
}

type httpHealthSpec struct {
	Path pathValue `yaml:"path"`
	Port intValue  `yaml:"port"`
}

type intValue int

func (v *intValue) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode || strings.TrimSpace(value.Value) == "" {
		return nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value.Value))
	if err != nil {
		return err
	}
	*v = intValue(parsed)
	return nil
}

type pathValue struct {
	Scalar string
	ByType map[string]string
}

func (p *pathValue) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		p.Scalar = value.Value
	case yaml.MappingNode:
		p.ByType = map[string]string{}
		for i := 0; i+1 < len(value.Content); i += 2 {
			p.ByType[value.Content[i].Value] = value.Content[i+1].Value
		}
	}
	return nil
}

func (p pathValue) For(probeType string) string {
	if p.ByType != nil {
		if value := p.ByType[probeType]; value != "" {
			return value
		}
	}
	return p.Scalar
}

func (p pathValue) Empty() bool {
	return p.Scalar == "" && len(p.ByType) == 0
}

type probeSpec struct {
	Live  healthSpec `yaml:"live"`
	Ready healthSpec `yaml:"ready"`
	Start healthSpec `yaml:"start"`
}

type probesSpec struct {
	Preset string         `yaml:"preset"`
	Port   intValue       `yaml:"port"`
	Path   pathValue      `yaml:"path"`
	HTTP   httpHealthSpec `yaml:"http"`
	Live   healthSpec     `yaml:"live"`
	Ready  healthSpec     `yaml:"ready"`
	Start  healthSpec     `yaml:"start"`
}

type mtlsSpec struct {
	Enabled  bool   `yaml:"enabled"`
	MountDir string `yaml:"mount_dir"`
}

type assetSpec struct {
	File       string `yaml:"file"`
	To         string `yaml:"to"`
	Binary     bool   `yaml:"binary"`
	Transform  bool   `yaml:"transform"`
	HelmEscape *bool  `yaml:"helm_escape"`
	Temp       bool   `yaml:"temp"`
	PVC        bool   `yaml:"pvc"`
	Name       string `yaml:"name"`
	NFSServer  string `yaml:"nfs-server"`
	Path       string `yaml:"path"`
	HostPath   string `yaml:"host-path"`
}

type mountSpec struct {
	Type       string         `yaml:"type"`
	File       string         `yaml:"file"`
	MountPath  string         `yaml:"mount_path"`
	Binary     bool           `yaml:"binary"`
	Transform  bool           `yaml:"transform"`
	HelmEscape *bool          `yaml:"helm_escape"`
	Name       string         `yaml:"name"`
	ClaimName  string         `yaml:"claim_name"`
	Server     string         `yaml:"server"`
	Path       string         `yaml:"path"`
	HostPath   string         `yaml:"host_path"`
	Volume     map[string]any `yaml:"volume"`
	Mount      map[string]any `yaml:"mount"`
}

type resolvedAsset struct {
	RefName       string
	VolumeName    string
	ConfigMapKey  string
	ContainerName string
	SourceFile    string
	To            string
	Content       []byte
	Binary        bool
	Kind          string
	PVCName       string
	NFSServer     string
	NFSPath       string
	HostPath      string
	RawVolume     map[string]any
	RawMount      map[string]any
}

type sharedAssetSpec struct {
	Name       string `yaml:"name"`
	File       string `yaml:"file"`
	To         string `yaml:"to"`
	Binary     bool   `yaml:"binary"`
	Transform  bool   `yaml:"transform"`
	HelmEscape *bool  `yaml:"helm_escape"`
}

type sharedAssetsFile struct {
	Assets []sharedAssetSpec `yaml:"assets"`
}

type portSpec struct {
	Name           string       `yaml:"name"`
	Port           int          `yaml:"port"`
	Metrics        bool         `yaml:"metrics"`
	MetricsPathFor *string      `yaml:"metricsPathFor"`
	ExposeAs       []exposeSpec `yaml:"expose_as"`
}

type exposeSpec struct {
	ServiceName string         `yaml:"service_name"`
	Hostname    string         `yaml:"hostname"`
	Port        int            `yaml:"port"`
	Type        string         `yaml:"type"`
	External    []externalSpec `yaml:"external"`
}

type externalSpec struct {
	Name        string             `yaml:"name"`
	AsRoute     bool               `yaml:"as_route"`
	ClassName   string             `yaml:"class_name"`
	HTTP        []externalHostSpec `yaml:"http"`
	HTTPS       []externalHostSpec `yaml:"https"`
	TLS         map[string]any     `yaml:"tls"`
	Annotations map[string]any     `yaml:"annotations"`
	Labels      map[string]any     `yaml:"labels"`
}

type externalHostSpec struct {
	Hostname   string `yaml:"hostname"`
	Path       string `yaml:"path"`
	SecretName string `yaml:"secret_name"`
}

type toolSpec struct {
	Name            string                    `yaml:"name"`
	Image           string                    `yaml:"image"`
	ExposeBin       string                    `yaml:"expose_bin"`
	MountPath       string                    `yaml:"mount_path"`
	LegacyAs        string                    `yaml:"as"`
	ImagePullPolicy string                    `yaml:"image_pull_policy"`
	Resources       map[string]map[string]any `yaml:"resources"`
}

type envVar struct {
	Name                         string `yaml:"name"`
	Value                        string `yaml:"value,omitempty"`
	SecretName                   string `yaml:"secret_name,omitempty"`
	Key                          string `yaml:"key,omitempty"`
	ResourceName                 string `yaml:"resource_name,omitempty"`
	Divisor                      string `yaml:"divisor,omitempty"`
	FieldPath                    string `yaml:"field_path,omitempty"`
	WorkloadIdentityTokenRefName string `yaml:"workload_identity_token_ref_name,omitempty"`
	SharedAssetRefName           string `yaml:"shared_asset_ref_name,omitempty"`
	LegacyWorkloadIdentityToken  string `yaml:"workload_identity_token,omitempty"`
	Remove                       bool   `yaml:"remove,omitempty"`
}

type containerEnvDefault struct {
	ContainerRefName string   `yaml:"container_ref_name"`
	LegacyName       string   `yaml:"name"`
	Envs             []envVar `yaml:"envs"`
}

type containerProfile struct {
	Name     string
	Defaults *yaml.Node
}

type sidecarDefinition struct {
	Name string
	Node *yaml.Node
}

type startupSpec struct {
	Command   []string `yaml:"command"`
	Arguments []string `yaml:"arguments"`
}

var cgroupExporterDefaultVars = []envVar{
	{Name: "CGROUP_EXPORTER_METRICS_PREFIX", Value: "{{TSM_METRICS_PREFIX}}"},
	{Name: "CGROUP_EXPORTER_METRICS_STATIC_LABELS", Value: "cluster_name={{TSM_CLUSTER_NAME}}"},
	{Name: "CGROUP_EXPORTER_LISTEN", Value: "0.0.0.0:9393"},
	{Name: "CGROUP_EXPORTER_CPU_REQUESTS_MCPU", ResourceName: "requests.cpu", Divisor: "1m"},
	{Name: "CGROUP_EXPORTER_CPU_LIMITS_MCPU", ResourceName: "limits.cpu", Divisor: "1m"},
	{Name: "CGROUP_EXPORTER_MEMORY_REQUESTS_MIB", ResourceName: "requests.memory", Divisor: "1Mi"},
	{Name: "CGROUP_EXPORTER_MEMORY_LIMITS_MIB", ResourceName: "limits.memory", Divisor: "1Mi"},
	{Name: "CGROUP_EXPORTER_NODE_NAME", FieldPath: "spec.nodeName"},
}

func Build(opts Options) (Result, error) {
	if err := validateReleaseLabelValue("customer release ID", opts.CustomerReleaseID); err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(opts.Environment) == "" {
		return Result{}, errors.New("environment is required")
	}
	yamlIndent, err := normalizeYAMLIndent(opts.YAMLIndent)
	if err != nil {
		return Result{}, err
	}
	opts.YAMLIndent = yamlIndent
	if strings.TrimSpace(opts.Root) == "" {
		opts.Root = "environments"
	}
	if strings.TrimSpace(opts.Target) == "" {
		opts.Target = filepath.Join(opts.Root, opts.Environment, "target")
	}

	envDir := filepath.Join(opts.Root, opts.Environment)
	appsDir := filepath.Join(envDir, "apps")
	if !isDir(appsDir) {
		return Result{}, fmt.Errorf("apps directory not found: %s", appsDir)
	}

	vars, err := loadBuildVars(envDir, opts)
	if err != nil {
		return Result{}, err
	}
	releaseID, err := resolveReleaseID(opts)
	if err != nil {
		return Result{}, err
	}
	if opts.CustomerReleaseID != "" && releaseID == "" {
		return Result{}, errors.New("customer release ID requires --release-id or a release manifest with release_id")
	}
	var releaseContext *releaseContextSpec
	if releaseID != "" {
		if strings.TrimSpace(opts.ReleaseContextName) == "" {
			name, err := releaseContextNameFromDefaults(filepath.Join(appsDir, "_defaults.yml"))
			if err != nil {
				return Result{}, err
			}
			opts.ReleaseContextName = name
		}
		releaseContext, err = newReleaseContextSpec(opts, releaseID)
		if err != nil {
			return Result{}, err
		}
	}

	appFiles, err := listAppFiles(appsDir)
	if err != nil {
		return Result{}, err
	}

	deploymentsDir := filepath.Join(opts.Target, "deployments")
	if err := os.MkdirAll(deploymentsDir, 0o755); err != nil {
		return Result{}, err
	}
	servicesDir := filepath.Join(opts.Target, "services")
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		return Result{}, err
	}
	externalServicesDir := filepath.Join(servicesDir, "external")
	assetsDir := filepath.Join(opts.Target, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		return Result{}, err
	}
	sharedAssetsDir := filepath.Join(assetsDir, "shared")
	sharedAssets, err := loadSharedAssets(envDir, vars, opts)
	if err != nil {
		return Result{}, err
	}
	if len(sharedAssets) > 0 {
		if err := os.MkdirAll(sharedAssetsDir, 0o755); err != nil {
			return Result{}, err
		}
	}

	apps, err := loadApps(appFiles, filepath.Join(appsDir, "_defaults.yml"), vars, opts.Environment)
	if err != nil {
		return Result{}, err
	}
	if err := applyResourcePolicies(apps, appFiles, opts); err != nil {
		return Result{}, err
	}
	if err := applyImageOverrides(apps, opts); err != nil {
		return Result{}, err
	}
	if err := applyReplicaProfile(apps, envDir, opts); err != nil {
		return Result{}, err
	}
	applyScaleDown(apps, opts.Down)
	if err := validateApps(apps, sharedAssets); err != nil {
		return Result{}, err
	}
	sharedAssetsWave := 0
	if appsHaveArgoCDWave(apps) {
		sharedAssetsWave = 999
	}

	result := Result{}
	if strings.TrimSpace(opts.Namespace) != "" {
		result.Events = append(result.Events, BuildEvent{Type: "namespace_override", Name: vars["NAMESPACE"]})
	}
	result.Events = append(result.Events, BuildEvent{Type: "shared_assets", Count: len(sharedAssets)})
	if releaseContext != nil && !opts.DisableReleaseContextConfigMap {
		for _, asset := range sharedAssets {
			if asset.VolumeName == releaseContext.Name {
				return Result{}, fmt.Errorf("release context ConfigMap %q conflicts with an existing shared asset", releaseContext.Name)
			}
		}
		if err := os.MkdirAll(sharedAssetsDir, 0o755); err != nil {
			return Result{}, err
		}
		outPath := filepath.Join(sharedAssetsDir, releaseContext.Name+".yml")
		object := renderReleaseContextConfigMap(*releaseContext, vars["NAMESPACE"], opts.CustomerReleaseID, sharedAssetsWave)
		if err := writeRenderedObject(outPath, object, opts, syncMetadataSpec{ID: syncObjectID(opts, "ConfigMap", vars["NAMESPACE"], releaseContext.Name), Order: 100}); err != nil {
			return Result{}, err
		}
		result.Assets = append(result.Assets, outPath)
		result.Events = append(result.Events, BuildEvent{Type: "asset", Kind: "release-context", Name: releaseContext.Name, Path: outPath})
	}
	for _, asset := range sharedAssets {
		if asset.Kind != "configmap" && asset.Kind != "" {
			continue
		}
		outPath := filepath.Join(sharedAssetsDir, asset.VolumeName+".yml")
		if err := writeRenderedObject(outPath, renderAssetConfigMap(asset, vars["NAMESPACE"], sharedAssetsWave), opts, syncMetadataSpec{ID: syncAssetID(opts, vars["NAMESPACE"], "", asset, true), Order: 100}); err != nil {
			return Result{}, err
		}
		result.Assets = append(result.Assets, outPath)
		result.Events = append(result.Events, BuildEvent{Type: "asset", Kind: "shared", Name: asset.VolumeName, Path: outPath})
	}
	for _, app := range apps {
		if app.Ignore {
			continue
		}
		result.Events = append(result.Events, BuildEvent{Type: "app", App: app.Name, Count: len(app.Containers) + len(app.Sidecars)})
		for _, container := range app.Containers {
			result.Events = append(result.Events, BuildEvent{Type: "container", App: app.Name, Container: container.Name, Count: len(container.Assets)})
		}
		for _, sidecar := range app.Sidecars {
			result.Events = append(result.Events, BuildEvent{Type: "sidecar", App: app.Name, Container: sidecar.Name, Count: len(sidecar.Assets)})
		}
		appSharedAssets := sharedAssets
		if app.DisableSharedAssets {
			appSharedAssets = nil
		}
		resolvedAssets, err := resolveAssets(app, envDir, vars, opts)
		if err != nil {
			return Result{}, err
		}
		rolloutAnnotations, err := rolloutChecksumAnnotations(app, envDir)
		if err != nil {
			return Result{}, err
		}
		if serviceAccount := renderServiceAccount(app, vars["NAMESPACE"]); serviceAccount != nil {
			outPath := filepath.Join(deploymentsDir, app.Name+"-serviceaccount.yml")
			if err := writeRenderedObject(outPath, serviceAccount, opts, syncMetadataSpec{ID: syncObjectID(opts, "ServiceAccount", vars["NAMESPACE"], effectiveServiceAccountName(app)), Order: 180}); err != nil {
				return Result{}, err
			}
			result.ServiceAccounts = append(result.ServiceAccounts, outPath)
			result.Events = append(result.Events, BuildEvent{Type: "serviceaccount", App: app.Name, Name: effectiveServiceAccountName(app), Path: outPath})
		}
		deployment, err := renderDeployment(app, vars["NAMESPACE"], resolvedAssets, appSharedAssets, rolloutAnnotations, releaseID, opts.CustomerReleaseID, releaseContext, !opts.DisableReleaseContextEnv)
		if err != nil {
			return Result{}, err
		}
		outPath := filepath.Join(deploymentsDir, app.Name+"-deployment.yml")
		if err := writeRenderedObject(outPath, deployment, opts, syncMetadataSpec{ID: syncObjectID(opts, app.Kind, vars["NAMESPACE"], app.Name), Order: 200}); err != nil {
			return Result{}, err
		}
		result.Deployments = append(result.Deployments, outPath)
		result.Events = append(result.Events, BuildEvent{Type: "deployment", App: app.Name, Name: app.Name, Path: outPath})

		if !app.DisableCreateService {
			services := renderServices(app, vars["NAMESPACE"], opts.Environment)
			for _, service := range services {
				outPath := filepath.Join(servicesDir, service.Name+"-service.yml")
				if err := writeRenderedObject(outPath, service.Object, opts, syncMetadataSpec{ID: syncObjectID(opts, "Service", vars["NAMESPACE"], service.Name), Order: 300}); err != nil {
					return Result{}, err
				}
				result.Services = append(result.Services, outPath)
				result.Events = append(result.Events, BuildEvent{Type: "service", App: app.Name, Name: service.Name, Path: outPath})

				for _, external := range service.Externals {
					if err := os.MkdirAll(externalServicesDir, 0o755); err != nil {
						return Result{}, err
					}
					outPath := filepath.Join(externalServicesDir, external.Name+"-"+external.Kind+".yml")
					if err := writeRenderedObject(outPath, external.Object, opts, syncMetadataSpec{ID: syncObjectID(opts, externalKindName(external.Kind), vars["NAMESPACE"], external.Name), Order: 400}); err != nil {
						return Result{}, err
					}
					result.Externals = append(result.Externals, outPath)
					result.Events = append(result.Events, BuildEvent{Type: "external", App: app.Name, Name: external.Name, Kind: external.Kind, Path: outPath})
				}
			}
		}

		for _, asset := range configMapResolvedAssets(resolvedAssets) {
			outPath := filepath.Join(assetsDir, asset.VolumeName+".yml")
			if err := writeRenderedObject(outPath, renderAssetConfigMap(asset, vars["NAMESPACE"], appArgoCDWave(app)), opts, syncMetadataSpec{ID: syncAssetID(opts, vars["NAMESPACE"], app.Name, asset, false), Order: 100}); err != nil {
				return Result{}, err
			}
			result.Assets = append(result.Assets, outPath)
			result.Events = append(result.Events, BuildEvent{Type: "asset", App: app.Name, Container: asset.ContainerName, Name: asset.VolumeName, Path: outPath})
		}

		if budget := renderBudget(app, vars["NAMESPACE"]); budget != nil {
			outPath := filepath.Join(deploymentsDir, app.Name+"-budget.yml")
			if err := writeRenderedObject(outPath, budget, opts, syncMetadataSpec{ID: syncObjectID(opts, "PodDisruptionBudget", vars["NAMESPACE"], app.Name), Order: 190}); err != nil {
				return Result{}, err
			}
			result.Budgets = append(result.Budgets, outPath)
			result.Events = append(result.Events, BuildEvent{Type: "budget", App: app.Name, Name: app.Name, Path: outPath})
		}

		if autoscaling := renderAutoscaling(app, vars["NAMESPACE"]); autoscaling != nil {
			outPath := filepath.Join(deploymentsDir, app.Name+"-hpa.yml")
			if err := writeRenderedObject(outPath, autoscaling, opts, syncMetadataSpec{ID: syncObjectID(opts, "HorizontalPodAutoscaler", vars["NAMESPACE"], app.Name), Order: 210}); err != nil {
				return Result{}, err
			}
			result.Autoscaling = append(result.Autoscaling, outPath)
			result.Events = append(result.Events, BuildEvent{Type: "autoscaling", App: app.Name, Name: app.Name, Path: outPath})
		}
	}
	if opts.LegacyApplyEnv {
		if err := applyLegacyEnvironment(result, vars); err != nil {
			return Result{}, err
		}
	}

	return result, nil
}

var legacyEnvironmentPlaceholder = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func applyLegacyEnvironment(result Result, vars map[string]string) error {
	paths := make([]string, 0, len(result.Deployments)+len(result.ServiceAccounts)+len(result.Budgets)+len(result.Autoscaling)+len(result.Externals))
	paths = append(paths, result.Deployments...)
	paths = append(paths, result.ServiceAccounts...)
	paths = append(paths, result.Budgets...)
	paths = append(paths, result.Autoscaling...)
	paths = append(paths, result.Externals...)
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("legacy apply-env read %s: %w", path, err)
		}
		resolved, err := resolveLegacyEnvironmentValue(string(content), vars)
		if err != nil {
			return fmt.Errorf("legacy apply-env %s: %w", path, err)
		}
		var manifest any
		if err := yaml.Unmarshal([]byte(resolved), &manifest); err != nil {
			return fmt.Errorf("legacy apply-env produced invalid YAML in %s: %w", path, err)
		}
		if err := os.WriteFile(path, []byte(resolved), 0o644); err != nil {
			return fmt.Errorf("legacy apply-env write %s: %w", path, err)
		}
	}
	return nil
}

func writeRenderedObject(path string, object map[string]any, opts Options, syncSpec syncMetadataSpec) error {
	if err := applySyncMetadata(object, opts, syncSpec); err != nil {
		return err
	}
	out, err := marshalManifestYAML(object, opts.YAMLIndent)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func normalizeYAMLIndent(indent int) (int, error) {
	switch indent {
	case 0, 2:
		return 2, nil
	case 4:
		return 4, nil
	default:
		return 0, fmt.Errorf("yaml indent must be 2 or 4, got %d", indent)
	}
}

func marshalManifestYAML(object map[string]any, indent int) ([]byte, error) {
	indent, err := normalizeYAMLIndent(indent)
	if err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(indent)
	if err := encoder.Encode(object); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func applySyncMetadata(object map[string]any, opts Options, spec syncMetadataSpec) error {
	profile := strings.TrimSpace(opts.SyncProfile)
	if profile == "" || profile == "none" {
		return nil
	}
	if profile != "kube-deploy-sync" && profile != "argocd" {
		return fmt.Errorf("unsupported sync metadata profile: %s", profile)
	}
	if strings.TrimSpace(spec.ID) == "" {
		return errors.New("sync metadata id is required")
	}
	prefix := syncMetadataPrefix(opts)
	syncSet := syncMetadataSet(opts)
	metadata := ensureMap(object, "metadata")
	labels := ensureMap(metadata, "labels")
	annotations := ensureMap(metadata, "annotations")

	labels["app.kubernetes.io/managed-by"] = profile
	labels[prefix+"/generated-by"] = "kube-build-app"
	labels[prefix+"/sync-set"] = syncSet
	labels[prefix+"/sync-id-hash"] = shortSyncIDHash(spec.ID)
	annotations[prefix+"/sync-id"] = spec.ID
	annotations[prefix+"/sync-order"] = strconv.Itoa(spec.Order)
	if profile == "argocd" {
		if _, ok := annotations["argocd.argoproj.io/sync-wave"]; !ok {
			annotations["argocd.argoproj.io/sync-wave"] = strconv.Itoa(spec.Order)
		}
	}

	hash, err := canonicalObjectHash(object, prefix)
	if err != nil {
		return err
	}
	annotations[prefix+"/sync-hash"] = hash
	return nil
}

func syncMetadataPrefix(opts Options) string {
	if prefix := strings.TrimSpace(opts.SyncPrefix); prefix != "" {
		return strings.TrimSuffix(prefix, "/")
	}
	return "kube-build-app.io"
}

func syncMetadataSet(opts Options) string {
	if syncSet := strings.TrimSpace(opts.SyncSet); syncSet != "" {
		return syncSet
	}
	return strings.TrimSpace(opts.Environment)
}

func syncObjectID(opts Options, kind string, namespace string, name string) string {
	return strings.Join([]string{syncMetadataSet(opts), strings.TrimSpace(kind), strings.TrimSpace(namespace), strings.TrimSpace(name)}, "/")
}

func syncAssetID(opts Options, namespace string, appName string, asset resolvedAsset, shared bool) string {
	source := strings.TrimSpace(asset.SourceFile)
	if source == "" {
		source = strings.TrimSpace(asset.VolumeName)
	}
	if shared {
		return strings.Join([]string{syncMetadataSet(opts), "shared", "asset", strings.TrimSpace(namespace), source + ":" + strings.TrimSpace(asset.To)}, "/")
	}
	return strings.Join([]string{syncMetadataSet(opts), "app", strings.TrimSpace(appName), "container", strings.TrimSpace(asset.ContainerName), "asset", source + ":" + strings.TrimSpace(asset.To)}, "/")
}

func externalKindName(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "ingress":
		return "Ingress"
	case "route":
		return "Route"
	default:
		return strings.TrimSpace(kind)
	}
}

func shortSyncIDHash(syncID string) string {
	sum := sha256.Sum256([]byte(syncID))
	return hex.EncodeToString(sum[:])[:16]
}

func canonicalObjectHash(object map[string]any, prefix string) (string, error) {
	copyObject, err := deepCopyMap(object)
	if err != nil {
		return "", err
	}
	delete(copyObject, "status")
	if metadata, ok := copyObject["metadata"].(map[string]any); ok {
		for _, key := range []string{"creationTimestamp", "resourceVersion", "uid", "generation", "managedFields"} {
			delete(metadata, key)
		}
		if annotations, ok := metadata["annotations"].(map[string]any); ok {
			delete(annotations, strings.TrimSuffix(prefix, "/")+"/sync-hash")
			if len(annotations) == 0 {
				delete(metadata, "annotations")
			}
		}
	}
	canonical, err := json.Marshal(copyObject)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func deepCopyMap(value map[string]any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing
	}
	next := map[string]any{}
	parent[key] = next
	return next
}

func Inventory(opts Options) (map[string]any, error) {
	apps, envDir, _, err := loadPreparedApps(opts, true)
	if err != nil {
		return nil, err
	}

	items := []map[string]any{}
	for _, app := range apps {
		if app.Ignore {
			continue
		}
		rolloutAnnotations, err := rolloutChecksumAnnotations(app, envDir)
		if err != nil {
			return nil, err
		}
		for _, container := range appRuntimeContainers(app) {
			item := map[string]any{
				"env":                    opts.Environment,
				"app":                    app.Name,
				"app_kind":               app.Kind,
				"replicas":               app.Replicas,
				"rollout_checksums":      rolloutAnnotations,
				"container":              container.Name,
				"image":                  container.Image,
				"simple_init_enabled":    container.SimpleInit.Enabled,
				"enable_cgroup_exporter": container.EnableCgroupExporter,
				"mtls_enabled":           container.MTLS.Enabled,
				"resources":              container.Resources,
				"resources_source":       resourceSource(opts),
				"mtls_paths": map[string]any{
					"secured_json": filepath.ToSlash(filepath.Join(opts.Environment, "mtls", app.Name, container.Name+".secured.json")),
					"schema_json":  filepath.ToSlash(filepath.Join(opts.Environment, "mtls", app.Name, container.Name+".secured.schema.json")),
					"mount_target": mtlsMountDir(container),
					"files":        []string{"tls.crt", "tls.key", "ca.crt"},
				},
			}
			if javaRuntimeEnabled(container.Runtime.Java) {
				item["runtime"] = map[string]any{
					"java": map[string]any{
						"xms":             container.Runtime.Java.Xms,
						"xmx":             container.Runtime.Java.Xmx,
						"opts":            container.Runtime.Java.Opts,
						"export_env_name": javaRuntimeEnvName(container.Runtime.Java),
					},
				}
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left := fmt.Sprint(items[i]["app"]) + "\x00" + fmt.Sprint(items[i]["container"])
		right := fmt.Sprint(items[j]["app"]) + "\x00" + fmt.Sprint(items[j]["container"])
		return left < right
	})

	return map[string]any{
		"env":              opts.Environment,
		"apps_count":       len(apps),
		"containers_count": len(items),
		"profiles":         profilesPayload(envDir, opts),
		"items":            items,
	}, nil
}

func Validate(opts Options) error {
	_, _, _, err := loadPreparedApps(opts, true)
	return err
}

func ListApps(opts Options) ([]string, error) {
	apps, _, _, err := loadPreparedApps(opts, true)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(apps))
	for _, app := range apps {
		if !app.Ignore {
			names = append(names, app.Name)
		}
	}
	return names, nil
}

func ResourceSummary(opts Options) (ResourceSummaryData, error) {
	apps, _, _, err := loadPreparedApps(opts, true)
	if err != nil {
		return ResourceSummaryData{}, err
	}
	summary := ResourceSummaryData{Environment: opts.Environment}
	for _, app := range apps {
		if app.Ignore {
			continue
		}
		summary.Totals.Apps++
		summary.Totals.Replicas += app.Replicas
		replicas := app.Replicas
		for _, container := range appRuntimeContainers(app) {
			cpuRequestRaw := resourceRequestValue(container.Resources, "cpu")
			cpuLimitRaw := resourceLimitValue(container.Resources, "cpu")
			memRequestRaw := resourceRequestValue(container.Resources, "memory")
			memLimitRaw := resourceLimitValue(container.Resources, "memory")
			cpuRequest := parseCPU(cpuRequestRaw)
			cpuLimit := parseCPU(cpuLimitRaw)
			memRequest := parseMemoryMiB(memRequestRaw)
			memLimit := parseMemoryMiB(memLimitRaw)
			totalCPURequest := cpuRequest * float64(replicas)
			totalCPULimit := cpuLimit * float64(replicas)
			totalMemRequest := memRequest * float64(replicas)
			totalMemLimit := memLimit * float64(replicas)
			summary.Totals.Containers++
			summary.Totals.CPURequestCores += totalCPURequest
			summary.Totals.CPULimitCores += totalCPULimit
			summary.Totals.MemoryRequestMiB += totalMemRequest
			summary.Totals.MemoryLimitMiB += totalMemLimit
			summary.Items = append(summary.Items, ResourceSummaryItem{
				App:                   app.Name,
				Replicas:              replicas,
				Container:             container.Name,
				CPURequest:            emptyDash(cpuRequestRaw),
				CPULimit:              emptyDash(cpuLimitRaw),
				MemoryRequest:         emptyDash(memRequestRaw),
				MemoryLimit:           emptyDash(memLimitRaw),
				JavaXms:               emptyDash(container.Runtime.Java.Xms),
				JavaXmx:               emptyDash(container.Runtime.Java.Xmx),
				CPURequestCores:       cpuRequest,
				CPULimitCores:         cpuLimit,
				MemoryRequestMiB:      memRequest,
				MemoryLimitMiB:        memLimit,
				TotalCPURequestCores:  totalCPURequest,
				TotalCPULimitCores:    totalCPULimit,
				TotalMemoryRequestMiB: totalMemRequest,
				TotalMemoryLimitMiB:   totalMemLimit,
			})
		}
	}
	return summary, nil
}

func FormatResourceSummaryText(summary ResourceSummaryData) string {
	rows := [][]string{{
		"App",
		"Rep",
		"Container",
		"CPU req",
		"CPU lim",
		"Mem req",
		"Mem lim",
		"JVM Xms",
		"JVM Xmx",
		"CPU req xR",
		"CPU lim xR",
		"Mem req xR",
		"Mem lim xR",
	}}
	for _, item := range summary.Items {
		rows = append(rows, []string{
			item.App,
			strconv.Itoa(item.Replicas),
			item.Container,
			item.CPURequest,
			item.CPULimit,
			item.MemoryRequest,
			item.MemoryLimit,
			item.JavaXms,
			item.JavaXmx,
			formatCPU(item.TotalCPURequestCores),
			formatCPU(item.TotalCPULimitCores),
			formatMiB(item.TotalMemoryRequestMiB),
			formatMiB(item.TotalMemoryLimitMiB),
		})
	}
	table := renderASCIITable(rows, map[int]bool{1: true, 9: true, 10: true, 11: true, 12: true})
	var out strings.Builder
	fmt.Fprintf(&out, "Resource summary for environment: %s\n\n", summary.Environment)
	out.WriteString(table)
	out.WriteString("\n\nTotals\n")
	fmt.Fprintf(&out, "  apps:       %d\n", summary.Totals.Apps)
	fmt.Fprintf(&out, "  containers: %d\n", summary.Totals.Containers)
	fmt.Fprintf(&out, "  replicas:   %d\n", summary.Totals.Replicas)
	fmt.Fprintf(&out, "  cpu req:    %s cores\n", formatCPU(summary.Totals.CPURequestCores))
	fmt.Fprintf(&out, "  cpu lim:    %s cores\n", formatCPU(summary.Totals.CPULimitCores))
	fmt.Fprintf(&out, "  mem req:    %s\n", formatMiB(summary.Totals.MemoryRequestMiB))
	fmt.Fprintf(&out, "  mem lim:    %s\n", formatMiB(summary.Totals.MemoryLimitMiB))
	return out.String()
}

func loadPreparedApps(opts Options, applyProfileAndDown bool) ([]appModel, string, map[string]string, error) {
	if strings.TrimSpace(opts.Environment) == "" {
		return nil, "", nil, errors.New("environment is required")
	}
	if strings.TrimSpace(opts.Root) == "" {
		opts.Root = "environments"
	}
	envDir := filepath.Join(opts.Root, opts.Environment)
	appsDir := filepath.Join(envDir, "apps")
	if !isDir(appsDir) {
		return nil, "", nil, fmt.Errorf("apps directory not found: %s", appsDir)
	}
	vars, err := loadBuildVars(envDir, opts)
	if err != nil {
		return nil, "", nil, err
	}
	appFiles, err := listAppFiles(appsDir)
	if err != nil {
		return nil, "", nil, err
	}
	apps, err := loadApps(appFiles, filepath.Join(appsDir, "_defaults.yml"), vars, opts.Environment)
	if err != nil {
		return nil, "", nil, err
	}
	if err := applyResourcePolicies(apps, appFiles, opts); err != nil {
		return nil, "", nil, err
	}
	sharedAssets, err := loadSharedAssets(envDir, vars, opts)
	if err != nil {
		return nil, "", nil, err
	}
	if err := applyImageOverrides(apps, opts); err != nil {
		return nil, "", nil, err
	}
	if applyProfileAndDown {
		if err := applyReplicaProfile(apps, envDir, opts); err != nil {
			return nil, "", nil, err
		}
		applyScaleDown(apps, opts.Down)
	}
	if err := validateApps(apps, sharedAssets); err != nil {
		return nil, "", nil, err
	}
	return apps, envDir, vars, nil
}

func resourceValue(resources map[string]map[string]any, resource string, key string) string {
	if resources == nil || resources[resource] == nil {
		return ""
	}
	return fmt.Sprint(resources[resource][key])
}

func resourceRequestValue(resources map[string]map[string]any, resource string) string {
	if value := resourceValue(resources, resource, "requests"); value != "" && value != "<nil>" {
		return value
	}
	return resourceValue(resources, resource, "from")
}

func resourceLimitValue(resources map[string]map[string]any, resource string) string {
	if value := resourceValue(resources, resource, "limits"); value != "" && value != "<nil>" {
		return value
	}
	return resourceValue(resources, resource, "to")
}

func emptyDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "<nil>" {
		return "-"
	}
	return value
}

func formatCPU(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

func formatMiB(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64) + "Mi"
}

func renderASCIITable(rows [][]string, rightAlign map[int]bool) string {
	if len(rows) == 0 {
		return ""
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	border := func() string {
		var out strings.Builder
		out.WriteByte('+')
		for _, width := range widths {
			out.WriteString(strings.Repeat("-", width+2))
			out.WriteByte('+')
		}
		return out.String()
	}
	line := border()
	var out strings.Builder
	out.WriteString(line)
	out.WriteByte('\n')
	for rowIndex, row := range rows {
		out.WriteByte('|')
		for i, width := range widths {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			padding := width - len(cell)
			if rightAlign[i] {
				out.WriteByte(' ')
				out.WriteString(strings.Repeat(" ", padding))
				out.WriteString(cell)
				out.WriteByte(' ')
			} else {
				out.WriteByte(' ')
				out.WriteString(cell)
				out.WriteString(strings.Repeat(" ", padding))
				out.WriteByte(' ')
			}
			out.WriteByte('|')
		}
		out.WriteByte('\n')
		if rowIndex == 0 {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	out.WriteString(line)
	return out.String()
}

func parseCPU(value string) float64 {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "<nil>" {
		return 0
	}
	multiplier := 1.0
	if strings.HasSuffix(value, "m") {
		multiplier = 0.001
		value = strings.TrimSuffix(value, "m")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed * multiplier
}

func parseMemoryMiB(value string) float64 {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "<nil>" {
		return 0
	}
	multiplier := 1.0
	switch {
	case strings.HasSuffix(value, "ki"):
		multiplier = 1.0 / 1024.0
		value = strings.TrimSuffix(value, "ki")
	case strings.HasSuffix(value, "mi"):
		multiplier = 1.0
		value = strings.TrimSuffix(value, "mi")
	case strings.HasSuffix(value, "gi"):
		multiplier = 1024.0
		value = strings.TrimSuffix(value, "gi")
	case strings.HasSuffix(value, "k"):
		multiplier = 1000.0 / 1024.0 / 1024.0
		value = strings.TrimSuffix(value, "k")
	case strings.HasSuffix(value, "m"):
		multiplier = 1000.0 * 1000.0 / 1024.0 / 1024.0
		value = strings.TrimSuffix(value, "m")
	case strings.HasSuffix(value, "g"):
		multiplier = 1000.0 * 1000.0 * 1000.0 / 1024.0 / 1024.0
		value = strings.TrimSuffix(value, "g")
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed * multiplier
}

func loadVars(envDir string, opts Options) (map[string]string, error) {
	vars := map[string]string{}
	sources, envFile, envURL, envURLHeaders, envURLInsecure, decryptSecured, err := effectiveVarsSources(opts)
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		switch source {
		case "json":
			if decryptSecured {
				if err := loadEnvJSONFile(vars, filepath.Join(envDir, "env.secured.json"), true, opts); err != nil {
					return nil, err
				}
			}
			if err := loadEnvJSONFile(vars, filepath.Join(envDir, "env.unsecured.json"), false, opts); err != nil {
				return nil, err
			}
		case "env":
			for _, item := range os.Environ() {
				key, value, ok := strings.Cut(item, "=")
				if ok {
					vars[key] = value
				}
			}
		case "dot-env":
			path := envFile
			if path == "" {
				path = filepath.Join(envDir, ".env")
			}
			if !isFile(path) {
				return nil, fmt.Errorf("vars-source 'dot-env' selected but file not found: %s", path)
			}
			if err := loadDotEnvFile(vars, path); err != nil {
				return nil, err
			}
		case "env-url":
			if err := loadDotEnvURL(vars, envURL, envURLHeaders, envURLInsecure); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("invalid vars source %q", source)
		}
	}
	return vars, nil
}

func loadBuildVars(envDir string, opts Options) (map[string]string, error) {
	vars, err := loadVars(envDir, opts)
	if err != nil {
		return nil, err
	}
	releaseID, err := resolveReleaseID(opts)
	if err != nil {
		return nil, err
	}
	if releaseID != "" {
		if current := strings.TrimSpace(vars["RELEASE_ID"]); current != "" && current != releaseID && strings.TrimSpace(opts.ReleaseID) == "" {
			return nil, fmt.Errorf("release manifest release_id %q conflicts with build variable RELEASE_ID %q", releaseID, current)
		}
		vars["RELEASE_ID"] = releaseID
	}
	if namespace := strings.TrimSpace(opts.Namespace); namespace != "" {
		vars["NAMESPACE"] = namespace
	}
	return vars, nil
}

func effectiveVarsSources(opts Options) ([]string, string, string, []string, bool, bool, error) {
	if strings.TrimSpace(opts.EnvFile) != "" {
		if opts.DecryptSecured {
			return nil, "", "", nil, false, false, errors.New("-E/--env-file cannot be combined with -d/--decrypt-secured")
		}
		if strings.TrimSpace(opts.EnvURL) != "" {
			return nil, "", "", nil, false, false, errors.New("-E/--env-file cannot be combined with --env-url")
		}
		if len(opts.VarsSources) > 0 {
			return nil, "", "", nil, false, false, errors.New("-E/--env-file cannot be combined with --vars-source")
		}
		if len(opts.EnvURLHeaders) > 0 {
			return nil, "", "", nil, false, false, errors.New("--env-url-header requires --env-url")
		}
		if opts.EnvURLInsecure {
			return nil, "", "", nil, false, false, errors.New("--env-url-insecure requires --env-url")
		}
		return []string{"dot-env"}, opts.EnvFile, "", nil, false, false, nil
	}
	if strings.TrimSpace(opts.EnvURL) != "" {
		if opts.DecryptSecured {
			return nil, "", "", nil, false, false, errors.New("--env-url cannot be combined with -d/--decrypt-secured")
		}
		if len(opts.VarsSources) > 0 {
			return nil, "", "", nil, false, false, errors.New("--env-url cannot be combined with --vars-source")
		}
		if err := validateEnvURL(opts.EnvURL); err != nil {
			return nil, "", "", nil, false, false, err
		}
		if err := validateEnvURLHeaders(opts.EnvURLHeaders); err != nil {
			return nil, "", "", nil, false, false, err
		}
		return []string{"env-url"}, "", opts.EnvURL, opts.EnvURLHeaders, opts.EnvURLInsecure, false, nil
	}
	if len(opts.EnvURLHeaders) > 0 {
		return nil, "", "", nil, false, false, errors.New("--env-url-header requires --env-url")
	}
	if opts.EnvURLInsecure {
		return nil, "", "", nil, false, false, errors.New("--env-url-insecure requires --env-url")
	}

	sources := []string{}
	for _, value := range opts.VarsSources {
		for _, part := range strings.Split(value, ",") {
			source := strings.TrimSpace(part)
			if source == "" {
				continue
			}
			switch source {
			case "env", "json", "dot-env":
				sources = append(sources, source)
			default:
				return nil, "", "", nil, false, false, fmt.Errorf("invalid --vars-source %q, expected one of: env, json, dot-env", source)
			}
		}
	}
	if len(sources) == 0 {
		sources = []string{"json", "env"}
	}
	return sources, "", "", nil, false, opts.DecryptSecured, nil
}

func loadEnvJSONFile(vars map[string]string, path string, secured bool, opts Options) error {
	if !isFile(path) {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if secured {
		content, err = decryptEnvJSONFile(path, opts)
		if err != nil {
			return err
		}
	}
	var parsed envFile
	if err := json.Unmarshal(content, &parsed); err != nil {
		return err
	}
	if secured {
		var raw map[string]any
		if err := json.Unmarshal(content, &raw); err != nil {
			return err
		}
		if value, ok := raw["_public_key"]; ok {
			vars["_public_key"] = fmt.Sprint(value)
		}
	}
	for key, value := range parsed.Environment {
		vars[key] = fmt.Sprint(value)
	}
	return nil
}

func decryptEnvJSONFile(path string, opts Options) ([]byte, error) {
	bin := encjsonBinForFile(path, opts)
	args := []string{"decrypt"}
	keydir := strings.TrimSpace(opts.EncjsonKeydir)
	if keydir == "" {
		keydir = strings.TrimSpace(os.Getenv("ENCJSON_KEYDIR"))
	}
	if keydir != "" {
		args = append(args, "-k", keydir)
	}
	args = append(args, "-f", path)
	cmd := exec.Command(bin, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return nil, fmt.Errorf("decrypt %s failed: %w: %s", path, err, stderr)
			}
		}
		return nil, fmt.Errorf("decrypt %s failed: %w", path, err)
	}
	return out, nil
}

func encjsonBinForFile(path string, opts Options) string {
	switch detectEncjsonAPI(path) {
	case "1.0":
		if value := strings.TrimSpace(opts.EncjsonLegacyPath); value != "" {
			return value
		}
		return legacyEncjsonBin()
	case "2.0":
		if value := strings.TrimSpace(opts.EncjsonPath); value != "" {
			return value
		}
		return rustEncjsonBin()
	default:
		if value := strings.TrimSpace(os.Getenv("ENCJSON_BIN")); value != "" {
			return value
		}
		return legacyEncjsonBin()
	}
}

func legacyEncjsonBin() string {
	for _, key := range []string{"ENCJSON_LEGACY_PATH", "ENCJSON_LEGACY_BIN", "ENCJSON_BIN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "encjson"
}

func rustEncjsonBin() string {
	for _, key := range []string{"ENCJSON_PATH", "ENCJSON_RS_BIN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "encjson-rs"
}

func detectEncjsonAPI(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	probe := string(content)
	if len(probe) > 8192 {
		probe = probe[:8192]
	}
	switch {
	case strings.Contains(probe, "EncJson[@api=2.0"):
		return "2.0"
	case strings.Contains(probe, "EncJson[@api=1.0"):
		return "1.0"
	default:
		return ""
	}
}

func loadDotEnvFile(vars map[string]string, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	loadDotEnvContent(vars, string(content))
	return nil
}

func loadDotEnvURL(vars map[string]string, rawURL string, headers []string, insecure bool) error {
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("env url request: %w", err)
	}
	for _, header := range headers {
		name, value, _ := strings.Cut(header, ":")
		request.Header.Add(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	client := http.Client{Timeout: 15 * time.Second}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // Explicit CLI escape hatch for self-signed internal endpoints.
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("env url fetch %s: %w", rawURL, err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return fmt.Errorf("env url read %s: %w", rawURL, err)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		body := strings.TrimSpace(string(content))
		if len(body) > 500 {
			body = body[:500] + "..."
		}
		if body != "" {
			return fmt.Errorf("env url fetch %s failed: HTTP %d: %s", rawURL, response.StatusCode, body)
		}
		return fmt.Errorf("env url fetch %s failed: HTTP %d", rawURL, response.StatusCode)
	}
	loadDotEnvContent(vars, string(content))
	return nil
}

func validateEnvURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid --env-url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("invalid --env-url %q: expected http or https URL", rawURL)
	}
	if parsed.Host == "" {
		return fmt.Errorf("invalid --env-url %q: missing host", rawURL)
	}
	return nil
}

func validateEnvURLHeaders(headers []string) error {
	for _, header := range headers {
		name, _, ok := strings.Cut(header, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("invalid --env-url-header %q: expected 'Name: value'", header)
		}
	}
	return nil
}

func loadDotEnvContent(vars map[string]string, content string) {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
				(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
				value = value[1 : len(value)-1]
			}
		}
		vars[key] = value
	}
}

func loadVarsLegacy(envDir string) (map[string]string, error) {
	vars := map[string]string{}
	unsecuredPath := filepath.Join(envDir, "env.unsecured.json")
	if isFile(unsecuredPath) {
		content, err := os.ReadFile(unsecuredPath)
		if err != nil {
			return nil, err
		}
		var parsed envFile
		if err := json.Unmarshal(content, &parsed); err != nil {
			return nil, err
		}
		for key, value := range parsed.Environment {
			vars[key] = fmt.Sprint(value)
		}
	}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			vars[key] = value
		}
	}
	return vars, nil
}

func loadApps(appFiles []string, defaultsPath string, vars map[string]string, environment string) ([]appModel, error) {
	apps := make([]appModel, 0, len(appFiles))
	for _, appFile := range appFiles {
		app, err := loadApp(appFile, defaultsPath, vars)
		if err != nil {
			return nil, err
		}
		if app.Kind == "" {
			app.Kind = "Deployment"
		}
		apps = append(apps, app)
	}
	if err := prepareRuntimeAssetsForApps(apps, environment); err != nil {
		return nil, err
	}
	return apps, nil
}

func prepareRuntimeAssetsForApps(apps []appModel, environment string) error {
	for appIndex := range apps {
		app := &apps[appIndex]
		if len(app.LegacyRuntimeAssets) > 0 {
			return fmt.Errorf("%s: runtime_assets was removed; use runtime_asset_definitions and runtime_asset_ref_names", app.Name)
		}
		for _, item := range app.RuntimeAssetDefs {
			if item.LegacyApps != nil {
				return fmt.Errorf(
					"%s: runtime_asset_definitions %q uses removed key apps; select assets from apps with runtime_asset_ref_names",
					app.Name,
					item.Name,
				)
			}
			if item.LegacyContainers != nil {
				return fmt.Errorf(
					"%s: runtime_asset_definitions %q uses removed key containers; select assets from containers with runtime_asset_ref_names",
					app.Name,
					item.Name,
				)
			}
			if len(item.AppRefNames) > 0 {
				return fmt.Errorf("%s: runtime_asset_definitions %q uses removed key app_ref_names; select assets from apps with runtime_asset_ref_names", app.Name, item.Name)
			}
			if len(item.ContainerRefNames) > 0 {
				return fmt.Errorf("%s: runtime_asset_definitions %q uses removed key container_ref_names; select assets from containers with runtime_asset_ref_names", app.Name, item.Name)
			}
		}
		resolved, err := resolveRuntimeAssetRefs(*app)
		if err != nil {
			return err
		}
		app.RuntimeAssets = resolved
		normalizeRuntimeAssets(app, environment)
	}
	return nil
}

func resolveRuntimeAssetRefs(app appModel) ([]runtimeAssetSpec, error) {
	definitions := map[string]runtimeAssetSpec{}
	order := make([]string, 0, len(app.RuntimeAssetDefs))
	for _, item := range app.RuntimeAssetDefs {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return nil, fmt.Errorf("%s: runtime_asset_definitions[].name is required", app.Name)
		}
		if _, exists := definitions[name]; exists {
			return nil, fmt.Errorf("%s: duplicate runtime_asset_definitions name %q", app.Name, name)
		}
		item.Name = name
		item = applyRuntimeAssetDefaults(item, app.RuntimeAssetDefaults)
		definitions[name] = item
		order = append(order, name)
	}

	selected := map[string]runtimeAssetSpec{}
	selectedOrder := []string{}
	addRef := func(refName, containerName string) error {
		refName = strings.TrimSpace(refName)
		if refName == "" {
			return nil
		}
		definition, ok := definitions[refName]
		if !ok {
			return fmt.Errorf("%s: runtime_asset_ref_names references unknown runtime asset %q", app.Name, refName)
		}
		item, exists := selected[refName]
		if !exists {
			item = definition
			item.ContainerRefNames = nil
			selectedOrder = append(selectedOrder, refName)
		}
		item.ContainerRefNames = appendUniqueString(item.ContainerRefNames, containerName)
		selected[refName] = item
		return nil
	}

	for _, refName := range app.RuntimeAssetRefs {
		if err := addRef(refName, "*"); err != nil {
			return nil, err
		}
	}
	for _, container := range app.Containers {
		for _, refName := range container.RuntimeAssetRefs {
			if err := addRef(refName, container.Name); err != nil {
				return nil, err
			}
		}
	}
	for _, sidecar := range app.Sidecars {
		for _, refName := range sidecar.RuntimeAssetRefs {
			if err := addRef(refName, sidecar.Name); err != nil {
				return nil, err
			}
		}
	}

	definitionPosition := map[string]int{}
	for index, name := range order {
		definitionPosition[name] = index
	}
	sort.SliceStable(selectedOrder, func(i, j int) bool {
		return definitionPosition[selectedOrder[i]] < definitionPosition[selectedOrder[j]]
	})
	resolved := make([]runtimeAssetSpec, 0, len(selectedOrder))
	for _, name := range selectedOrder {
		resolved = append(resolved, selected[name])
	}
	return resolved, nil
}

func applyRuntimeAssetDefaults(item runtimeAssetSpec, defaults runtimeAssetDefaults) runtimeAssetSpec {
	if !runtimeAssetSourceConfigured(item.Source) {
		item.Source = defaults.Source
	}
	if !runtimeAssetVolumeConfigured(item.Volume) {
		item.Volume = defaults.Volume
	}
	if !runtimeAssetFetcherConfigured(item.Fetcher) {
		item.Fetcher = defaults.Fetcher
	}
	return item
}

func runtimeAssetSourceConfigured(source runtimeAssetSourceSpec) bool {
	return source.Type != "" ||
		source.BaseURL != "" ||
		source.Tenant != "" ||
		source.Environment != "" ||
		source.Label != "" ||
		source.WorkloadIdentityTokenRefName != "" ||
		source.CASharedAssetRefName != "" ||
		source.CAFile != "" ||
		source.LegacyToken != "" ||
		source.TimeoutSeconds != 0 ||
		source.InsecureUpstreamTLS
}

func runtimeAssetVolumeConfigured(volume runtimeAssetVolumeSpec) bool {
	return volume.Name != "" || volume.MountPath != "" || volume.Medium != "" || volume.SizeLimit != ""
}

func runtimeAssetFetcherConfigured(fetcher runtimeAssetFetcherSpec) bool {
	return fetcher.Image != "" ||
		fetcher.ImagePullPolicy != "" ||
		fetcher.Command != "" ||
		len(fetcher.Resources) > 0 ||
		len(fetcher.SecurityContext) > 0
}

func appendUniqueString(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func normalizeRuntimeAssets(app *appModel, environment string) {
	for index := range app.RuntimeAssets {
		item := &app.RuntimeAssets[index]
		if strings.TrimSpace(item.Source.Type) == "" {
			item.Source.Type = "simple_config"
		}
		if strings.TrimSpace(item.Source.Tenant) == "" {
			item.Source.Tenant = "default"
		}
		if strings.TrimSpace(item.Source.Environment) == "" {
			item.Source.Environment = environment
		}
		if item.Source.TimeoutSeconds == 0 {
			item.Source.TimeoutSeconds = 30
		}
		if strings.TrimSpace(item.Volume.Name) == "" {
			item.Volume.Name = item.Name
		}
		if len(item.ContainerRefNames) == 0 {
			item.ContainerRefNames = []string{"*"}
		}
		if strings.TrimSpace(item.Fetcher.Command) == "" {
			item.Fetcher.Command = "simple-idm-token-proxy"
		}
		for fileIndex := range item.Files {
			if strings.TrimSpace(item.Files[fileIndex].Mode) == "" {
				item.Files[fileIndex].Mode = "0440"
			}
		}
	}
}

func validateApps(apps []appModel, sharedAssets []resolvedAsset) error {
	sharedAssetNames := make(map[string]bool, len(sharedAssets))
	sharedAssetMountPaths := make(map[string]bool, len(sharedAssets))
	for _, asset := range sharedAssets {
		if asset.RefName != "" {
			sharedAssetNames[asset.RefName] = true
		}
		sharedAssetMountPaths[asset.To] = true
	}
	for _, app := range apps {
		if err := validateTools(app); err != nil {
			return err
		}
		tokenNames := map[string]bool{}
		for _, token := range app.WorkloadIdentity.Tokens {
			if strings.TrimSpace(token.Name) == "" {
				return fmt.Errorf("%s: workload_identity.tokens[].name is required", app.Name)
			}
			if strings.TrimSpace(token.Audience) == "" {
				return fmt.Errorf("%s: workload_identity token %q requires audience", app.Name, token.Name)
			}
			if tokenNames[token.Name] {
				return fmt.Errorf("%s: duplicate workload_identity token %q", app.Name, token.Name)
			}
			tokenNames[token.Name] = true
		}
		if err := validateRuntimeAssets(app, tokenNames, sharedAssetNames, sharedAssetMountPaths); err != nil {
			return err
		}
		for _, mount := range app.DownwardAPI.Mounts {
			if strings.TrimSpace(mount.Name) == "" {
				return fmt.Errorf("%s: downward_api.mounts[].name is required", app.Name)
			}
			if strings.TrimSpace(mount.MountPath) == "" {
				return fmt.Errorf("%s: downward_api mount %q requires mount_path", app.Name, mount.Name)
			}
			if len(mount.Items) == 0 {
				return fmt.Errorf("%s: downward_api mount %q requires items", app.Name, mount.Name)
			}
			for _, item := range mount.Items {
				if strings.TrimSpace(item.Path) == "" || strings.TrimSpace(item.FieldPath) == "" {
					return fmt.Errorf("%s: downward_api mount %q requires item path and field_path", app.Name, mount.Name)
				}
			}
		}
		if app.Autoscaling.Enabled {
			if app.Autoscaling.MinReplicas < 1 {
				return fmt.Errorf("%s: autoscaling.min_replicas must be greater than 0", app.Name)
			}
			if app.Autoscaling.MaxReplicas < app.Autoscaling.MinReplicas {
				return fmt.Errorf("%s: autoscaling.max_replicas must be greater than or equal to min_replicas", app.Name)
			}
			if app.Autoscaling.CPU.AverageUtilization == 0 && app.Autoscaling.Memory.AverageUtilization == 0 && !autoscalingHasRawMetrics(app.Autoscaling) {
				return fmt.Errorf("%s: autoscaling requires at least one metric: cpu.average_utilization, memory.average_utilization or raw.spec.metrics", app.Name)
			}
			if !validAutoscalingUtilization(app.Autoscaling.CPU.AverageUtilization) {
				return fmt.Errorf("%s: autoscaling.cpu.average_utilization must be between 1 and 100", app.Name)
			}
			if !validAutoscalingUtilization(app.Autoscaling.Memory.AverageUtilization) {
				return fmt.Errorf("%s: autoscaling.memory.average_utilization must be between 1 and 100", app.Name)
			}
		}
		for _, container := range appRuntimeContainers(app) {
			if _, err := imagePullPolicy(container.ImagePullPolicy); err != nil {
				return fmt.Errorf("%s: container %q: %w", app.Name, container.Name, err)
			}
			if err := validateEnvReferences(app, container.Name, effectiveContainerEnvs(container), tokenNames, sharedAssetNames); err != nil {
				return err
			}
			for _, port := range container.Ports {
				for _, expose := range port.ExposeAs {
					if expose.ServiceName != "" && expose.Hostname != "" && expose.ServiceName != expose.Hostname {
						return fmt.Errorf("%s: container %q port %q expose_as has conflicting service_name and legacy hostname", app.Name, container.Name, port.Name)
					}
				}
			}
			if javaRuntimeEnabled(container.Runtime.Java) {
				envName := javaRuntimeEnvName(container.Runtime.Java)
				if containerHasEnvVar(container.Envs, envName) {
					return fmt.Errorf("%s: container %q defines runtime.java export env %q and envs with the same name", app.Name, container.Name, envName)
				}
			}
			if !container.SimpleInit.Enabled {
				continue
			}
			if len(container.Startup.Command) > 0 || len(container.Startup.Arguments) > 0 {
				return fmt.Errorf("%s: container %q has both startup and simple_init.enabled=true (XOR violation)", app.Name, container.Name)
			}
			if len(container.SimpleInit.Exec.Command) == 0 {
				return fmt.Errorf("%s: container %q requires simple_init.exec.command as non-empty array", app.Name, container.Name)
			}
		}
		for _, container := range app.InitContainers {
			if _, err := imagePullPolicy(container.ImagePullPolicy); err != nil {
				return fmt.Errorf("%s: init container %q: %w", app.Name, container.Name, err)
			}
			if err := validateEnvReferences(app, container.Name, effectiveInitContainerEnvs(container), tokenNames, sharedAssetNames); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateEnvReferences(app appModel, containerName string, items []envVar, tokenNames, sharedAssetNames map[string]bool) error {
	for _, item := range items {
		if legacy := strings.TrimSpace(item.LegacyWorkloadIdentityToken); legacy != "" {
			return fmt.Errorf(
				"%s: container %q env %q uses removed key workload_identity_token; use workload_identity_token_ref_name",
				app.Name,
				containerName,
				item.Name,
			)
		}
		tokenRefName := strings.TrimSpace(item.WorkloadIdentityTokenRefName)
		sharedAssetRefName := strings.TrimSpace(item.SharedAssetRefName)
		if tokenRefName == "" && sharedAssetRefName == "" {
			continue
		}
		if item.Value != "" ||
			item.SecretName != "" ||
			item.Key != "" ||
			item.ResourceName != "" ||
			item.Divisor != "" ||
			item.FieldPath != "" ||
			(tokenRefName != "" && sharedAssetRefName != "") ||
			item.Remove {
			return fmt.Errorf(
				"%s: container %q env %q combines a reference with another value source",
				app.Name,
				containerName,
				item.Name,
			)
		}
		if tokenRefName != "" && !tokenNames[tokenRefName] {
			return fmt.Errorf(
				"%s: container %q env %q references unknown workload_identity token %q",
				app.Name,
				containerName,
				item.Name,
				tokenRefName,
			)
		}
		if sharedAssetRefName == "" {
			continue
		}
		if app.DisableSharedAssets {
			return fmt.Errorf(
				"%s: container %q env %q uses shared_asset_ref_name while disable_shared_assets is true",
				app.Name,
				containerName,
				item.Name,
			)
		}
		if !sharedAssetNames[sharedAssetRefName] {
			return fmt.Errorf(
				"%s: container %q env %q references unknown shared asset %q",
				app.Name,
				containerName,
				item.Name,
				sharedAssetRefName,
			)
		}
	}
	return nil
}

func validateRuntimeAssets(app appModel, tokenNames, sharedAssetNames, sharedAssetMountPaths map[string]bool) error {
	names := map[string]bool{}
	reservedVolumes := reservedVolumeNames(app)
	runtimeVolumes := map[string]runtimeAssetVolumeSpec{}
	mountPaths := map[string]string{}
	volumeTargets := map[string]map[string]string{}
	for _, item := range app.RuntimeAssets {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			return fmt.Errorf("%s: runtime_assets[].name is required", app.Name)
		}
		if !isDNSLabel(name) {
			return fmt.Errorf("%s: runtime_assets name %q must be a Kubernetes DNS label", app.Name, name)
		}
		if len("runtime-assets-"+name) > 63 {
			return fmt.Errorf("%s: runtime_assets name %q is too long for generated init container name", app.Name, name)
		}
		if names[name] {
			return fmt.Errorf("%s: duplicate runtime_assets name %q", app.Name, name)
		}
		names[name] = true

		if item.Source.Type != "simple_config" {
			return fmt.Errorf("%s: runtime_assets %q source.type must be simple_config", app.Name, name)
		}
		if err := validateHTTPBaseURL(item.Source.BaseURL); err != nil {
			return fmt.Errorf("%s: runtime_assets %q: %w", app.Name, name, err)
		}
		if strings.TrimSpace(item.Source.Tenant) == "" {
			return fmt.Errorf("%s: runtime_assets %q source.tenant is required", app.Name, name)
		}
		if strings.TrimSpace(item.Source.Environment) == "" {
			return fmt.Errorf("%s: runtime_assets %q source.environment is required", app.Name, name)
		}
		if legacy := strings.TrimSpace(item.Source.LegacyToken); legacy != "" {
			return fmt.Errorf(
				"%s: runtime_assets %q uses removed key source.token; use source.workload_identity_token_ref_name",
				app.Name,
				name,
			)
		}
		tokenRefName := strings.TrimSpace(item.Source.WorkloadIdentityTokenRefName)
		if tokenRefName == "" {
			return fmt.Errorf("%s: runtime_assets %q source.workload_identity_token_ref_name is required", app.Name, name)
		}
		if !tokenNames[tokenRefName] {
			return fmt.Errorf("%s: runtime_assets %q references unknown workload_identity token %q", app.Name, name, tokenRefName)
		}
		if item.Source.TimeoutSeconds < 1 {
			return fmt.Errorf("%s: runtime_assets %q source.timeout_seconds must be greater than 0", app.Name, name)
		}
		caFile := strings.TrimSpace(item.Source.CAFile)
		caSharedAssetRefName := strings.TrimSpace(item.Source.CASharedAssetRefName)
		if caFile != "" && caSharedAssetRefName != "" {
			return fmt.Errorf(
				"%s: runtime_assets %q source.ca_file cannot be combined with source.ca_shared_asset_ref_name",
				app.Name,
				name,
			)
		}
		if caFile != "" {
			if err := validateAbsoluteMountPath(caFile); err != nil {
				return fmt.Errorf("%s: runtime_assets %q source.ca_file: %w", app.Name, name, err)
			}
			if app.DisableSharedAssets {
				return fmt.Errorf(
					"%s: runtime_assets %q source.ca_file cannot be used while disable_shared_assets is true",
					app.Name,
					name,
				)
			}
			if !sharedAssetMountPaths[caFile] {
				return fmt.Errorf(
					"%s: runtime_assets %q source.ca_file %q does not match any shared asset target",
					app.Name,
					name,
					caFile,
				)
			}
			if item.Source.InsecureUpstreamTLS {
				return fmt.Errorf(
					"%s: runtime_assets %q source.ca_file cannot be combined with source.insecure_upstream_tls",
					app.Name,
					name,
				)
			}
		}
		if caSharedAssetRefName != "" {
			if app.DisableSharedAssets {
				return fmt.Errorf(
					"%s: runtime_assets %q source.ca_shared_asset_ref_name cannot be used while disable_shared_assets is true",
					app.Name,
					name,
				)
			}
			if !sharedAssetNames[caSharedAssetRefName] {
				return fmt.Errorf(
					"%s: runtime_assets %q references unknown shared asset %q",
					app.Name,
					name,
					caSharedAssetRefName,
				)
			}
			if item.Source.InsecureUpstreamTLS {
				return fmt.Errorf(
					"%s: runtime_assets %q source.ca_shared_asset_ref_name cannot be combined with source.insecure_upstream_tls",
					app.Name,
					name,
				)
			}
		}

		volumeName := strings.TrimSpace(item.Volume.Name)
		if !isDNSLabel(volumeName) {
			return fmt.Errorf("%s: runtime_assets %q volume.name %q must be a Kubernetes DNS label", app.Name, name, volumeName)
		}
		if existing, ok := runtimeVolumes[volumeName]; ok {
			if existing != item.Volume {
				return fmt.Errorf(
					"%s: runtime_assets %q volume.name %q conflicts with a differently configured runtime volume",
					app.Name,
					name,
					volumeName,
				)
			}
		} else if reservedVolumes[volumeName] {
			return fmt.Errorf("%s: runtime_assets %q volume.name %q conflicts with another generated volume", app.Name, name, volumeName)
		} else {
			runtimeVolumes[volumeName] = item.Volume
		}
		if err := validateAbsoluteMountPath(item.Volume.MountPath); err != nil {
			return fmt.Errorf("%s: runtime_assets %q volume.mount_path: %w", app.Name, name, err)
		}
		if existingVolume, ok := mountPaths[item.Volume.MountPath]; ok && existingVolume != volumeName {
			return fmt.Errorf("%s: runtime_assets %q duplicates volume.mount_path %q", app.Name, name, item.Volume.MountPath)
		}
		mountPaths[item.Volume.MountPath] = volumeName
		if item.Volume.Medium != "" && item.Volume.Medium != "Memory" {
			return fmt.Errorf("%s: runtime_assets %q volume.medium must be Memory or empty", app.Name, name)
		}

		if strings.TrimSpace(item.Fetcher.Image) == "" {
			return fmt.Errorf("%s: runtime_assets %q fetcher.image is required", app.Name, name)
		}
		if _, err := imagePullPolicy(item.Fetcher.ImagePullPolicy); err != nil {
			return fmt.Errorf("%s: runtime_assets %q fetcher: %w", app.Name, name, err)
		}
		if strings.TrimSpace(item.Fetcher.Command) == "" {
			return fmt.Errorf("%s: runtime_assets %q fetcher.command is required", app.Name, name)
		}

		if len(item.Files) == 0 {
			return fmt.Errorf("%s: runtime_assets %q requires at least one file", app.Name, name)
		}
		targets := volumeTargets[volumeName]
		if targets == nil {
			targets = map[string]string{}
			volumeTargets[volumeName] = targets
		}
		for _, file := range item.Files {
			if err := validateRuntimeAssetSourcePath(file.Source); err != nil {
				return fmt.Errorf("%s: runtime_assets %q file source: %w", app.Name, name, err)
			}
			target, err := normalizeRuntimeAssetTarget(file.Target)
			if err != nil {
				return fmt.Errorf("%s: runtime_assets %q file target: %w", app.Name, name, err)
			}
			if existingAsset, exists := targets[target]; exists {
				return fmt.Errorf(
					"%s: runtime_assets %q file target %q conflicts with runtime asset %q in shared volume %q",
					app.Name,
					name,
					target,
					existingAsset,
					volumeName,
				)
			}
			targets[target] = name
			if !regexp.MustCompile(`^0[0-7]{3}$`).MatchString(file.Mode) {
				return fmt.Errorf("%s: runtime_assets %q file %q mode must use four octal digits such as 0440", app.Name, name, file.Target)
			}
			if err := validateRuntimeAssetSHA256(file.SHA256); err != nil {
				return fmt.Errorf("%s: runtime_assets %q file %q: %w", app.Name, name, file.Target, err)
			}
		}

		for _, selector := range item.ContainerRefNames {
			if selector == "*" {
				continue
			}
			if !appHasRuntimeContainer(app, selector) {
				return fmt.Errorf("%s: runtime_assets %q container_ref_names references unknown container %q", app.Name, name, selector)
			}
		}
	}
	return nil
}

func reservedVolumeNames(app appModel) map[string]bool {
	out := map[string]bool{}
	for _, token := range app.WorkloadIdentity.Tokens {
		if name := strings.TrimSpace(token.Name); name != "" {
			out[name+"-token"] = true
		}
	}
	for _, mount := range app.DownwardAPI.Mounts {
		if name := strings.TrimSpace(mount.Name); name != "" {
			out[name] = true
		}
	}
	if app.PodInfo.Enabled {
		out["podinfo"] = true
	}
	if len(app.Tools) > 0 {
		out["app-tools"] = true
	}
	for _, container := range appRuntimeContainers(app) {
		for _, mount := range container.Mounts {
			if name := effectiveMountName(mount); name != "" {
				out[name] = true
			}
		}
	}
	for _, container := range app.InitContainers {
		for _, mount := range container.Mounts {
			if name := effectiveMountName(mount); name != "" {
				out[name] = true
			}
		}
	}
	return out
}

func effectiveMountName(mount mountSpec) string {
	if name, _ := mount.Volume["name"].(string); strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(mount.Name)
}

func isDNSLabel(value string) bool {
	return regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(value) && len(value) <= 63
}

func validateHTTPBaseURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed == nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("source.base_url must be an absolute HTTP(S) URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("source.base_url must not contain a query or fragment")
	}
	return nil
}

func validateAbsoluteMountPath(value string) error {
	value = strings.TrimSpace(value)
	if !filepath.IsAbs(value) || value == "/" || filepath.Clean(value) != value {
		return fmt.Errorf("must be a normalized absolute path other than /")
	}
	return nil
}

func validateRuntimeAssetSourcePath(value string) error {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, `\`) {
		return fmt.Errorf("must be a non-empty relative path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("must not contain empty, . or .. path segments")
		}
	}
	return nil
}

func normalizeRuntimeAssetTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, `\`) {
		return "", fmt.Errorf("must be a non-empty relative path")
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("must not contain empty, . or .. path segments")
		}
	}
	return value, nil
}

func validateRuntimeAssetSHA256(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) != 64 || !regexp.MustCompile(`^[0-9a-fA-F]{64}$`).MatchString(value) {
		return fmt.Errorf("sha256 must contain 64 hexadecimal digits")
	}
	return nil
}

func appHasRuntimeContainer(app appModel, name string) bool {
	for _, container := range appRuntimeContainers(app) {
		if container.Name == name {
			return true
		}
	}
	return false
}

func validateTools(app appModel) error {
	seenPaths := map[string]bool{}
	for _, tool := range app.Tools {
		if strings.TrimSpace(tool.LegacyAs) != "" {
			return fmt.Errorf("%s: tool %q uses removed key as; use mount_path", app.Name, tool.Name)
		}
		if strings.TrimSpace(tool.ExposeBin) == "" {
			return fmt.Errorf("%s: tool %q requires expose_bin", app.Name, tool.Name)
		}
		if strings.TrimSpace(tool.Image) == "" {
			return fmt.Errorf("%s: tool %q requires image", app.Name, tool.Name)
		}
		mountPath, err := normalizedToolMountPath(tool)
		if err != nil {
			return fmt.Errorf("%s: tool %q: %w", app.Name, tool.Name, err)
		}
		if seenPaths[mountPath] {
			return fmt.Errorf("%s: duplicate tool mount_path %q", app.Name, mountPath)
		}
		seenPaths[mountPath] = true
		if _, err := imagePullPolicy(tool.ImagePullPolicy); err != nil {
			return fmt.Errorf("%s: tool %q: %w", app.Name, tool.Name, err)
		}
	}
	return nil
}

func validAutoscalingUtilization(value int) bool {
	return value == 0 || (value >= 1 && value <= 100)
}

func appRuntimeContainers(app appModel) []containerSpec {
	out := make([]containerSpec, 0, len(app.Containers)+len(app.Sidecars))
	out = append(out, app.Containers...)
	out = append(out, app.Sidecars...)
	return out
}

func autoscalingHasRawMetrics(autoscaling autoscalingSpec) bool {
	spec, ok := autoscaling.Raw["spec"].(map[string]any)
	if !ok {
		return false
	}
	metrics, ok := spec["metrics"].([]any)
	return ok && len(metrics) > 0
}

func containerHasEnvVar(items []envVar, name string) bool {
	for _, item := range items {
		if item.Name == name {
			return true
		}
	}
	return false
}

func applyReplicaProfile(apps []appModel, envDir string, opts Options) error {
	path := strings.TrimSpace(opts.ProfilesFile)
	if path == "" {
		path = filepath.Join(envDir, "replica-profiles.yml")
	}
	if !isFile(path) {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var parsed replicaProfilesFile
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(parsed.Defaults.LegacyProfile) != "" {
		return fmt.Errorf("%s: defaults.profile was removed; use defaults.replica_profile_ref_name", path)
	}

	profileName := strings.TrimSpace(opts.Profile)
	if profileName == "" {
		profileName = strings.TrimSpace(os.Getenv("REPLICA_PROFILE"))
	}
	if profileName == "" {
		profileName = strings.TrimSpace(parsed.Defaults.ProfileRefName)
	}
	if profileName == "" {
		return nil
	}

	profile, ok := parsed.Profiles[profileName]
	if !ok {
		return nil
	}
	if profile.All != nil {
		for i := range apps {
			apps[i].Replicas = *profile.All
		}
	}

	overrides := profile.Apps
	if overrides == nil {
		overrides = map[string]int{}
		for name, replicas := range profile.LegacyApps {
			overrides[name] = replicas
		}
	}
	for appName, replicas := range overrides {
		for i := range apps {
			if apps[i].Name == appName {
				apps[i].Replicas = replicas
				break
			}
		}
	}
	return nil
}

func applyScaleDown(apps []appModel, down []string) {
	if len(down) == 0 {
		return
	}
	names := map[string]bool{}
	for _, item := range down {
		for _, part := range strings.Split(item, ",") {
			name := strings.TrimSpace(part)
			if name != "" {
				names[name] = true
			}
		}
	}
	for i := range apps {
		if names[apps[i].Name] {
			apps[i].Replicas = 0
		}
	}
}

func applyImageOverrides(apps []appModel, opts Options) error {
	policy := normalizeImagePolicy(opts.ImagePolicy)
	if policy != "fallback" && policy != "strict" {
		return fmt.Errorf("invalid image policy %q: expected fallback or strict", opts.ImagePolicy)
	}
	selection, err := releaseImageSelectionFromOptions(opts)
	if err != nil {
		return err
	}
	images := map[imageKey]string{}
	for _, path := range releaseManifestPaths(opts) {
		manifest, err := loadReleaseManifest(path)
		if err != nil {
			return err
		}
		for _, app := range apps {
			for _, container := range app.Containers {
				image, err := manifest.imageFor(app.Name, container.Name, selection)
				if err != nil {
					return err
				}
				if image != "" {
					images[imageKey{App: app.Name, Container: container.Name}] = image
				}
			}
			for _, sidecar := range app.Sidecars {
				image, err := manifest.sidecarImageFor(app.Name, sidecar.Name, selection)
				if err != nil {
					return err
				}
				if image != "" {
					images[imageKey{App: app.Name, Container: sidecar.Name}] = image
				}
			}
		}
	}
	cliImages, err := parseImageOverrides(opts.ImageOverrides)
	if err != nil {
		return err
	}
	for key, image := range cliImages {
		images[key] = image
	}
	for i := range apps {
		for j := range apps[i].Containers {
			key := imageKey{App: apps[i].Name, Container: apps[i].Containers[j].Name}
			if image := images[key]; image != "" {
				apps[i].Containers[j].Image = image
			}
		}
		for j := range apps[i].Sidecars {
			key := imageKey{App: apps[i].Name, Container: apps[i].Sidecars[j].Name}
			if image := images[key]; image != "" {
				apps[i].Sidecars[j].Image = image
			}
		}
	}
	if policy == "strict" {
		for _, app := range apps {
			for _, container := range app.Containers {
				key := imageKey{App: app.Name, Container: container.Name}
				if images[key] == "" {
					return fmt.Errorf("image override missing for %s/%s in strict image policy", app.Name, container.Name)
				}
			}
		}
	}
	return nil
}

type imageKey struct {
	App       string
	Container string
}

func parseImageOverrides(items []string) (map[imageKey]string, error) {
	overrides := map[imageKey]string{}
	for _, item := range items {
		for _, part := range splitCommaSeparated(item) {
			keyRaw, image, ok := strings.Cut(part, "=")
			if !ok {
				return nil, fmt.Errorf("invalid image override %q: expected app/container=image", part)
			}
			app, container, ok := strings.Cut(strings.TrimSpace(keyRaw), "/")
			if !ok || strings.TrimSpace(app) == "" || strings.TrimSpace(container) == "" {
				return nil, fmt.Errorf("invalid image override %q: expected app/container=image", part)
			}
			image = strings.TrimSpace(image)
			if image == "" {
				return nil, fmt.Errorf("invalid image override %q: image must not be empty", part)
			}
			overrides[imageKey{App: strings.TrimSpace(app), Container: strings.TrimSpace(container)}] = image
		}
	}
	return overrides, nil
}

func splitCommaSeparated(items string) []string {
	var out []string
	for _, part := range strings.Split(items, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeImagePolicy(policy string) string {
	policy = strings.TrimSpace(strings.ToLower(policy))
	if policy == "" {
		return "fallback"
	}
	return policy
}

func normalizeImageReference(reference string) string {
	reference = strings.TrimSpace(strings.ToLower(reference))
	if reference == "" {
		return "auto"
	}
	return reference
}

type releaseImageSelection struct {
	ReferenceMode string
	ForceTag      string
	ForcePrefix   string
}

func releaseImageSelectionFromOptions(opts Options) (releaseImageSelection, error) {
	selection := releaseImageSelection{
		ReferenceMode: normalizeImageReference(opts.ImageReference),
		ForceTag:      strings.TrimSpace(opts.ForceImageTag),
		ForcePrefix:   strings.Trim(strings.TrimSpace(opts.ForceImagePrefix), "/"),
	}
	if selection.ReferenceMode != "auto" && selection.ReferenceMode != "digest" && selection.ReferenceMode != "tag" {
		return releaseImageSelection{}, fmt.Errorf("invalid image reference %q: expected auto, digest, or tag", opts.ImageReference)
	}
	if opts.ForceImageTag != "" && selection.ForceTag == "" {
		return releaseImageSelection{}, errors.New("--force-image-tag must not be empty")
	}
	if selection.ForceTag != "" && !validImageTag(selection.ForceTag) {
		return releaseImageSelection{}, fmt.Errorf("invalid --force-image-tag %q", opts.ForceImageTag)
	}
	if opts.ForceImagePrefix != "" && selection.ForcePrefix == "" {
		return releaseImageSelection{}, errors.New("--force-image-prefix must not be empty")
	}
	if selection.ForcePrefix != "" {
		if err := validateImagePrefix(selection.ForcePrefix); err != nil {
			return releaseImageSelection{}, err
		}
	}
	if selection.ForceTag != "" && selection.ReferenceMode == "digest" {
		return releaseImageSelection{}, errors.New("--force-image-tag cannot be combined with --image-reference digest")
	}
	if (selection.ForceTag != "" || selection.ForcePrefix != "") && len(releaseManifestPaths(opts)) == 0 {
		return releaseImageSelection{}, errors.New("--force-image-tag and --force-image-prefix require --release-manifest")
	}
	return selection, nil
}

func validImageTag(tag string) bool {
	return len(tag) <= 128 && imageTagPattern.MatchString(tag)
}

var imageTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func validateImagePrefix(prefix string) error {
	if strings.Contains(prefix, "://") {
		return fmt.Errorf("invalid --force-image-prefix %q: use an OCI registry/path without a URL scheme", prefix)
	}
	if strings.ContainsAny(prefix, "@ \t\r\n") || strings.Contains(prefix, "//") {
		return fmt.Errorf("invalid --force-image-prefix %q", prefix)
	}
	if slash := strings.LastIndex(prefix, "/"); slash >= 0 && strings.Contains(prefix[slash+1:], ":") {
		return fmt.Errorf("invalid --force-image-prefix %q: prefix must not contain an image tag", prefix)
	}
	return nil
}

type releaseManifest struct {
	APIVersion   string         `yaml:"apiVersion"`
	Kind         string         `yaml:"kind"`
	ReleaseID    string         `yaml:"release_id"`
	CreatedAt    string         `yaml:"created_at"`
	Bundle       *releaseBundle `yaml:"bundle"`
	RegistryBase string         `yaml:"repository_prefix"`
	Platform     string         `yaml:"platform"`
	Images       []releaseImage `yaml:"images"`
	ExtraTags    []string       `yaml:"extra_tags"`
}

type releaseBundle struct {
	Name     string `yaml:"name"`
	Revision string `yaml:"revision"`
}

type releaseImage struct {
	ID            string         `yaml:"id"`
	AppName       string         `yaml:"app_name"`
	ContainerName string         `yaml:"container_name"`
	Source        *releaseSource `yaml:"source"`
	Image         string         `yaml:"repository"`
	Tag           string         `yaml:"tag"`
	Digest        string         `yaml:"digest"`
	ExtraTags     []string       `yaml:"extra_tags"`
	Platform      string         `yaml:"platform"`
}

type releaseSource struct {
	Image  string `yaml:"repository"`
	Tag    string `yaml:"tag"`
	Digest string `yaml:"digest"`
}

func loadReleaseManifest(path string) (releaseManifest, error) {
	if !isFile(path) {
		return releaseManifest{}, fmt.Errorf("release manifest not found: %s", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return releaseManifest{}, err
	}
	content, err = normalizeReleaseDocument(content)
	if err != nil {
		return releaseManifest{}, fmt.Errorf("%s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var manifest releaseManifest
	if err := decoder.Decode(&manifest); err != nil {
		return releaseManifest{}, fmt.Errorf("%s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return releaseManifest{}, fmt.Errorf("%s: multiple YAML documents are not allowed", path)
		}
		return releaseManifest{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := manifest.normalizeAndValidate(); err != nil {
		return releaseManifest{}, fmt.Errorf("%s: %w", path, err)
	}
	return manifest, nil
}

func (m *releaseManifest) normalizeAndValidate() error {
	if (m.APIVersion != "" || m.Kind != "") && ((m.APIVersion != "oci-toolbox/v1" && m.APIVersion != "oci-toolbox/v2") || m.Kind != "ImageRelease") {
		return fmt.Errorf("unsupported release format %q / %q", m.APIVersion, m.Kind)
	}
	m.ReleaseID = strings.TrimSpace(m.ReleaseID)
	m.CreatedAt = strings.TrimSpace(m.CreatedAt)
	m.RegistryBase = strings.TrimSpace(m.RegistryBase)
	m.Platform = strings.TrimSpace(m.Platform)
	if m.Bundle != nil {
		m.Bundle.Name = strings.TrimSpace(m.Bundle.Name)
		m.Bundle.Revision = strings.TrimSpace(m.Bundle.Revision)
		if m.Bundle.Name == "" {
			return errors.New("bundle.name is required when bundle metadata is present")
		}
	}

	ids := map[string]struct{}{}
	selectors := map[imageKey]struct{}{}
	for i := range m.Images {
		item := &m.Images[i]
		item.ID = strings.TrimSpace(item.ID)
		item.AppName = strings.TrimSpace(item.AppName)
		item.ContainerName = strings.TrimSpace(item.ContainerName)
		item.Image = strings.TrimSpace(item.Image)
		item.Tag = strings.TrimSpace(item.Tag)
		item.Digest = strings.TrimSpace(item.Digest)
		item.Platform = strings.TrimSpace(item.Platform)

		if item.AppName == "" {
			return fmt.Errorf("images[%d].app_name is required", i)
		}
		if item.AppName == "*" && item.ContainerName == "" {
			return fmt.Errorf("images[%d].container_name is required for a wildcard app_name", i)
		}
		if item.Image == "" {
			return fmt.Errorf("images[%d].image is required", i)
		}
		if item.ID != "" {
			if _, exists := ids[item.ID]; exists {
				return fmt.Errorf("duplicate image id %q", item.ID)
			}
			ids[item.ID] = struct{}{}
		}
		selector := imageKey{App: item.AppName, Container: item.ContainerName}
		if _, exists := selectors[selector]; exists {
			return fmt.Errorf("duplicate image selector %q/%q", item.AppName, item.ContainerName)
		}
		selectors[selector] = struct{}{}

		if item.Platform != "" && m.Platform != "" && item.Platform != m.Platform {
			return fmt.Errorf(
				"images[%d].platform %q conflicts with manifest platform %q",
				i,
				item.Platform,
				m.Platform,
			)
		}
		if item.Source != nil {
			item.Source.Image = strings.TrimSpace(item.Source.Image)
			item.Source.Tag = strings.TrimSpace(item.Source.Tag)
			item.Source.Digest = strings.TrimSpace(item.Source.Digest)
			if item.Source.Image == "" {
				return fmt.Errorf("images[%d].source.image is required when source metadata is present", i)
			}
			if item.Source.Digest == "" {
				return fmt.Errorf("images[%d].source.digest is required when source metadata is present", i)
			}
		}
	}
	return nil
}

func (m releaseManifest) imageFor(appName string, containerName string, selection releaseImageSelection) (string, error) {
	if image, found, err := m.exactImageFor(appName, containerName, selection); found || err != nil {
		return image, err
	}
	if image, found, err := m.wildcardImageFor(containerName, selection); found || err != nil {
		return image, err
	}
	image, _, err := m.defaultImageFor(appName, selection)
	return image, err
}

func (m releaseManifest) sidecarImageFor(appName string, containerName string, selection releaseImageSelection) (string, error) {
	if image, found, err := m.exactImageFor(appName, containerName, selection); found || err != nil {
		return image, err
	}
	image, _, err := m.wildcardImageFor(containerName, selection)
	return image, err
}

func (m releaseManifest) exactImageFor(appName string, containerName string, selection releaseImageSelection) (string, bool, error) {
	var match *releaseImage
	for i := range m.Images {
		item := &m.Images[i]
		if item.AppName == appName && item.ContainerName == containerName {
			match = item
			break
		}
	}
	return releaseImageRef(match, selection)
}

func (m releaseManifest) wildcardImageFor(containerName string, selection releaseImageSelection) (string, bool, error) {
	var match *releaseImage
	for i := range m.Images {
		item := &m.Images[i]
		if item.AppName == "*" && item.ContainerName == containerName {
			match = item
			break
		}
	}
	return releaseImageRef(match, selection)
}

func (m releaseManifest) defaultImageFor(appName string, selection releaseImageSelection) (string, bool, error) {
	var match *releaseImage
	for i := range m.Images {
		item := &m.Images[i]
		if item.AppName == appName && strings.TrimSpace(item.ContainerName) == "" {
			match = item
			break
		}
	}
	return releaseImageRef(match, selection)
}

func releaseImageRef(match *releaseImage, selection releaseImageSelection) (string, bool, error) {
	if match == nil || strings.TrimSpace(match.Image) == "" {
		return "", false, nil
	}
	repository := strings.TrimSpace(match.Image)
	if selection.ForcePrefix != "" {
		basename, err := imageRepositoryBasename(repository)
		if err != nil {
			return "", true, err
		}
		repository = selection.ForcePrefix + "/" + basename
	}
	if selection.ForceTag != "" {
		if selection.ForcePrefix == "" {
			if _, err := imageRepositoryBasename(repository); err != nil {
				return "", true, err
			}
		}
		return repository + ":" + selection.ForceTag, true, nil
	}
	digest := strings.TrimSpace(match.Digest)
	tag := strings.TrimSpace(match.Tag)
	switch selection.ReferenceMode {
	case "digest":
		if digest == "" {
			return "", true, fmt.Errorf("release image %s has no digest required by --image-reference digest", match.Image)
		}
		return repository + "@" + digest, true, nil
	case "tag":
		if tag == "" {
			return "", true, fmt.Errorf("release image %s has no tag required by --image-reference tag", match.Image)
		}
		return repository + ":" + tag, true, nil
	}
	if digest != "" {
		return repository + "@" + digest, true, nil
	}
	if tag != "" {
		return repository + ":" + tag, true, nil
	}
	return repository, true, nil
}

func imageRepositoryBasename(repository string) (string, error) {
	repository = strings.TrimSpace(repository)
	original := repository
	if strings.Contains(repository, "://") {
		return "", fmt.Errorf("release image %q must not contain a URL scheme", repository)
	}
	if slash := strings.LastIndex(repository, "/"); slash >= 0 {
		repository = repository[slash+1:]
	}
	if repository == "" || strings.ContainsAny(repository, "@:") {
		return "", fmt.Errorf("release image %q must be an untagged repository name for --force-image-prefix", original)
	}
	return repository, nil
}

type replicaProfilesFile struct {
	Defaults replicaProfileDefaults        `yaml:"defaults"`
	Profiles map[string]replicaProfileSpec `yaml:"profiles"`
}

type replicaProfileDefaults struct {
	ProfileRefName string `yaml:"replica_profile_ref_name"`
	LegacyProfile  string `yaml:"profile"`
}

type replicaProfileSpec struct {
	All        *int
	Apps       map[string]int
	LegacyApps map[string]int
}

func (p *replicaProfileSpec) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return nil
	}
	p.LegacyApps = map[string]int{}
	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i].Value
		node := value.Content[i+1]
		switch key {
		case "all":
			replicas, ok := yamlNodeToInt(node)
			if ok {
				p.All = &replicas
			}
		case "apps":
			apps := map[string]int{}
			if node.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(node.Content); j += 2 {
					replicas, ok := yamlNodeToInt(node.Content[j+1])
					if ok {
						apps[node.Content[j].Value] = replicas
					}
				}
			}
			p.Apps = apps
		default:
			replicas, ok := yamlNodeToInt(node)
			if ok {
				p.LegacyApps[key] = replicas
			}
		}
	}
	return nil
}

func yamlNodeToInt(node *yaml.Node) (int, bool) {
	var value int
	if err := node.Decode(&value); err == nil {
		return value, true
	}
	var text string
	if err := node.Decode(&text); err != nil {
		return 0, false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func listAppFiles(appsDir string) ([]string, error) {
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		files = append(files, filepath.Join(appsDir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func loadApp(path string, defaultsPath string, vars map[string]string) (appModel, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return appModel{}, err
	}
	defaultsRaw, err := readOptionalFile(defaultsPath)
	if err != nil {
		return appModel{}, err
	}

	content := applyEnvPlaceholders(string(raw), vars)
	defaultsContent := applyEnvPlaceholders(defaultsRaw, vars)
	defaultVars, err := extractVariableList(defaultsContent)
	if err != nil {
		return appModel{}, fmt.Errorf("%s: %w", defaultsPath, err)
	}
	appVarList, err := extractVariableList(content)
	if err != nil {
		return appModel{}, fmt.Errorf("%s: %w", path, err)
	}
	mergedVarMap := variablesToMap(mergeVariables(defaultVars, appVarList))
	defaultsContent = applyAppVarPlaceholders(defaultsContent, mergedVarMap)
	content = applyAppVarPlaceholders(content, mergedVarMap)

	content, err = mergeDefaults(defaultsContent, content)
	if err != nil {
		return appModel{}, fmt.Errorf("%s: %w", path, err)
	}
	appVars, err := extractVars(content)
	if err != nil {
		return appModel{}, fmt.Errorf("%s: %w", path, err)
	}
	content = applyAppVarPlaceholders(content, appVars)

	var app appModel
	if err := yaml.Unmarshal([]byte(content), &app); err != nil {
		return appModel{}, fmt.Errorf("%s: %w", path, appYAMLUnmarshalError(content, err))
	}
	if app.Name == "" {
		return appModel{}, fmt.Errorf("%s: missing app name", path)
	}
	return app, nil
}

func appYAMLUnmarshalError(content string, err error) error {
	hint := barePlaceholderHint(content)
	if hint == "" {
		return err
	}
	return fmt.Errorf("%w\n\n%s", err, hint)
}

func barePlaceholderHint(content string) string {
	pattern := regexp.MustCompile(`\{\{\s*[A-Za-z_][A-Za-z0-9_]*\s*\}\}`)
	type occurrence struct {
		line int
		text string
	}
	var matches []occurrence
	for index, line := range strings.Split(content, "\n") {
		for _, value := range pattern.FindAllString(line, -1) {
			matches = append(matches, occurrence{line: index + 1, text: value})
			if len(matches) >= 5 {
				break
			}
		}
		if len(matches) >= 5 {
			break
		}
	}
	if len(matches) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("hint: bare {{NAME}} placeholders are passthrough legacy placeholders and are not resolved before app model parsing.\n")
	b.WriteString("hint: use {{env:NAME}} for .env/process variables or {{var:NAME}} for YAML vars in typed app fields such as port, replicas or probe port.\n")
	b.WriteString("hint: unresolved bare placeholder occurrence(s):")
	for _, match := range matches {
		fmt.Fprintf(&b, "\n  line %d: %s", match.line, match.text)
	}
	return b.String()
}

func extractVars(content string) (map[string]string, error) {
	vars, err := extractVariableList(content)
	if err != nil {
		return nil, err
	}
	return variablesToMap(vars), nil
}

func extractVariableList(content string) ([]variable, error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	type varsOnly struct {
		Vars []variable `yaml:"vars"`
	}
	var parsed varsOnly
	if err := yaml.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, err
	}
	return parsed.Vars, nil
}

func variablesToMap(items []variable) map[string]string {
	vars := map[string]string{}
	for _, item := range items {
		if item.Name != "" && !item.Remove {
			vars[item.Name] = item.Value
		}
	}
	return vars
}

func readOptionalFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" || !isFile(path) {
		return "", nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func mergeDefaults(defaultsContent, appContent string) (string, error) {
	if strings.TrimSpace(defaultsContent) == "" {
		return appContent, nil
	}

	defaultsNode, err := parseYAMLMapping(defaultsContent)
	if err != nil {
		return "", fmt.Errorf("_defaults.yml: %w", err)
	}
	appNode, err := parseYAMLMapping(appContent)
	if err != nil {
		return "", err
	}

	containerEnvDefaults, err := parseContainerEnvDefaults(mappingValue(defaultsNode, "container_envs"))
	if err != nil {
		return "", err
	}
	if mappingValue(defaultsNode, "container_defaults") != nil {
		return "", errors.New("container_defaults was removed; use container_profiles and containers[].profile_ref_names")
	}
	containerProfiles, err := parseContainerProfiles(mappingValue(defaultsNode, "container_profiles"))
	if err != nil {
		return "", err
	}
	if mappingValue(defaultsNode, "sidecars") != nil {
		return "", errors.New("_defaults.yml sidecars was removed; use sidecar_definitions and app sidecar_ref_names")
	}
	sidecarDefinitions, err := parseSidecarDefinitions(mappingValue(defaultsNode, "sidecar_definitions"))
	if err != nil {
		return "", err
	}

	removeMappingValue(defaultsNode, "container_profiles")
	removeMappingValue(defaultsNode, "sidecar_definitions")
	removeMappingValue(defaultsNode, "release_context_name")
	merged := mergeMappingNodes(defaultsNode, appNode)
	mergedVars, err := mergeVarsFromNodes(defaultsNode, appNode)
	if err != nil {
		return "", err
	}
	setMappingValue(merged, "vars", mergedVars)
	removeMappingValue(merged, "container_envs")
	if err := applySidecarRefs(merged, sidecarDefinitions); err != nil {
		return "", err
	}
	if err := applyContainerProfiles(merged, containerProfiles); err != nil {
		return "", err
	}
	if err := applyContainerEnvDefaults(merged, containerEnvDefaults); err != nil {
		return "", err
	}

	out, err := yaml.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseContainerEnvDefaults(node *yaml.Node) ([]containerEnvDefault, error) {
	if node == nil {
		return nil, nil
	}
	var items []containerEnvDefault
	if err := node.Decode(&items); err != nil {
		return nil, err
	}
	for _, item := range items {
		if strings.TrimSpace(item.LegacyName) != "" {
			return nil, errors.New("container_envs[].name was removed; use container_ref_name")
		}
		if strings.TrimSpace(item.ContainerRefName) == "" {
			return nil, errors.New("container_envs[].container_ref_name is required")
		}
	}
	return items, nil
}

func parseContainerProfiles(node *yaml.Node) (map[string]containerProfile, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, errors.New("container_profiles must be a sequence")
	}
	result := map[string]containerProfile{}
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			return nil, errors.New("container_profiles items must be mappings")
		}
		name := strings.TrimSpace(scalarMappingValue(item, "name"))
		if name == "" {
			return nil, errors.New("container_profiles[].name is required")
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate container_profiles name %q", name)
		}
		defaultsNode := mappingValue(item, "defaults")
		if defaultsNode == nil {
			return nil, fmt.Errorf("container_profiles %q defaults is required", name)
		}
		if defaultsNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("container_profiles %q defaults must be a mapping", name)
		}
		if mappingValue(defaultsNode, "name") != nil {
			return nil, fmt.Errorf("container_profiles %q defaults.name is not allowed; set container names in app files", name)
		}
		result[name] = containerProfile{Name: name, Defaults: cloneNode(defaultsNode)}
	}
	return result, nil
}

func parseSidecarDefinitions(node *yaml.Node) (map[string]sidecarDefinition, error) {
	if node == nil {
		return nil, nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, errors.New("sidecar_definitions must be a sequence")
	}
	result := map[string]sidecarDefinition{}
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			return nil, errors.New("sidecar_definitions items must be mappings")
		}
		name := strings.TrimSpace(scalarMappingValue(item, "name"))
		if name == "" {
			return nil, errors.New("sidecar_definitions[].name is required")
		}
		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate sidecar_definitions name %q", name)
		}
		result[name] = sidecarDefinition{Name: name, Node: cloneNode(item)}
	}
	return result, nil
}

func applySidecarRefs(root *yaml.Node, definitions map[string]sidecarDefinition) error {
	appName := scalarMappingValue(root, "name")
	refNames, err := stringSequenceMappingValue(root, "sidecar_ref_names")
	if err != nil {
		return fmt.Errorf("%s: sidecar_ref_names: %w", appName, err)
	}
	removeMappingValue(root, "sidecar_ref_names")
	resolved := &yaml.Node{Kind: yaml.SequenceNode}
	resolvedByName := map[string]int{}
	for _, refName := range refNames {
		definition, ok := definitions[refName]
		if !ok {
			return fmt.Errorf("%s: sidecar_ref_names references unknown sidecar %q", appName, refName)
		}
		if _, exists := resolvedByName[refName]; exists {
			continue
		}
		resolvedByName[refName] = len(resolved.Content)
		resolved.Content = append(resolved.Content, cloneNode(definition.Node))
	}

	localSidecars := mappingValue(root, "sidecars")
	if localSidecars != nil {
		if localSidecars.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s: sidecars must be a sequence", appName)
		}
		for _, localSidecar := range localSidecars.Content {
			if localSidecar.Kind != yaml.MappingNode {
				return fmt.Errorf("%s: sidecars items must be mappings", appName)
			}
			name := strings.TrimSpace(scalarMappingValue(localSidecar, "name"))
			if name == "" {
				return fmt.Errorf("%s: sidecars[].name is required", appName)
			}
			if index, exists := resolvedByName[name]; exists {
				merged, err := mergeContainerDefaultNodes(resolved.Content[index], localSidecar)
				if err != nil {
					return err
				}
				resolved.Content[index] = merged
				continue
			}
			if _, exists := definitions[name]; exists {
				return fmt.Errorf("%s: sidecars %q matches sidecar_definitions but is not selected; add it to sidecar_ref_names before patching it", appName, name)
			}
			resolvedByName[name] = len(resolved.Content)
			resolved.Content = append(resolved.Content, cloneNode(localSidecar))
		}
	}
	if len(resolved.Content) == 0 {
		return nil
	}
	setMappingValue(root, "sidecars", resolved)
	return nil
}

func applyContainerProfiles(root *yaml.Node, profiles map[string]containerProfile) error {
	if len(profiles) == 0 {
		return nil
	}
	if err := applyContainerProfilesToSequence(scalarMappingValue(root, "name"), "containers", mappingValue(root, "containers"), profiles); err != nil {
		return err
	}
	return applyContainerProfilesToSequence(scalarMappingValue(root, "name"), "sidecars", mappingValue(root, "sidecars"), profiles)
}

func applyContainerProfilesToSequence(appName, fieldName string, containersNode *yaml.Node, profiles map[string]containerProfile) error {
	if containersNode == nil || containersNode.Kind != yaml.SequenceNode {
		return nil
	}
	for _, containerNode := range containersNode.Content {
		if containerNode.Kind != yaml.MappingNode {
			continue
		}
		profileRefNames, err := stringSequenceMappingValue(containerNode, "profile_ref_names")
		if err != nil {
			return fmt.Errorf("%s: %s[].profile_ref_names: %w", appName, fieldName, err)
		}
		if len(profileRefNames) == 0 {
			continue
		}
		containerName := scalarMappingValue(containerNode, "name")
		effective := &yaml.Node{Kind: yaml.MappingNode}
		for _, profileRefName := range profileRefNames {
			profile, ok := profiles[profileRefName]
			if !ok {
				return fmt.Errorf("%s: %s %q profile_ref_names references unknown profile %q", appName, fieldName, containerName, profileRefName)
			}
			effective, err = mergeContainerDefaultNodes(effective, profile.Defaults)
			if err != nil {
				return err
			}
		}
		merged, err := mergeContainerDefaultNodes(effective, containerNode)
		if err != nil {
			return err
		}
		removeMappingValue(merged, "profile_ref_names")
		*containerNode = *merged
	}
	return nil
}

func mergeContainerDefaultNodes(defaultsNode, appNode *yaml.Node) (*yaml.Node, error) {
	merged := mergeMappingNodes(defaultsNode, appNode)
	defaultEnvs := mappingValue(defaultsNode, "envs")
	appEnvs := mappingValue(appNode, "envs")
	if defaultEnvs == nil && appEnvs == nil {
		return merged, nil
	}
	defaultVars, err := parseVarsNode(defaultEnvs, true)
	if err != nil {
		return nil, err
	}
	appVars, err := parseVarsNode(appEnvs, true)
	if err != nil {
		return nil, err
	}
	mergedVars := mergeVars(defaultVars, appVars)
	if len(mergedVars) == 0 {
		removeMappingValue(merged, "envs")
	} else {
		setMappingValue(merged, "envs", varsToNode(mergedVars))
	}
	return merged, nil
}

func stringSequenceMappingValue(node *yaml.Node, key string) ([]string, error) {
	value := mappingValue(node, key)
	if value == nil {
		return nil, nil
	}
	if value.Kind != yaml.SequenceNode {
		return nil, errors.New("must be a sequence")
	}
	var result []string
	for _, item := range value.Content {
		if item.Kind != yaml.ScalarNode {
			return nil, errors.New("must contain only scalar strings")
		}
		text := strings.TrimSpace(item.Value)
		if text != "" {
			result = append(result, text)
		}
	}
	return result, nil
}

func applyContainerEnvDefaults(root *yaml.Node, defaults []containerEnvDefault) error {
	if len(defaults) == 0 {
		return nil
	}
	containersNode := mappingValue(root, "containers")
	if containersNode == nil || containersNode.Kind != yaml.SequenceNode {
		return nil
	}

	for _, containerNode := range containersNode.Content {
		if containerNode.Kind != yaml.MappingNode {
			continue
		}
		containerName := scalarMappingValue(containerNode, "name")
		effective := []envVar{}
		for _, item := range defaults {
			if item.ContainerRefName == "*" {
				effective = mergeVars(effective, item.Envs)
			}
		}
		for _, item := range defaults {
			if item.ContainerRefName != "*" && item.ContainerRefName == containerName {
				effective = mergeVars(effective, item.Envs)
			}
		}

		local, err := parseVarsNode(mappingValue(containerNode, "envs"), true)
		if err != nil {
			return err
		}
		effective = mergeVars(effective, local)
		if len(effective) == 0 {
			removeMappingValue(containerNode, "envs")
		} else {
			setMappingValue(containerNode, "envs", varsToNode(effective))
		}
	}
	return nil
}

func parseVarsNode(node *yaml.Node, allowRemove bool) ([]envVar, error) {
	if node == nil {
		return nil, nil
	}
	var items []envVar
	if err := node.Decode(&items); err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Name == "" {
			return nil, errors.New("vars item is missing name")
		}
		if item.Remove && !allowRemove {
			return nil, errors.New("remove: true is not allowed here")
		}
	}
	return items, nil
}

func mergeVars(defaults []envVar, overrides []envVar) []envVar {
	result := make([]envVar, 0, len(defaults)+len(overrides))
	positions := map[string]int{}
	for _, item := range defaults {
		if item.Name == "" {
			continue
		}
		positions[item.Name] = len(result)
		result = append(result, item)
	}
	for _, item := range overrides {
		if item.Name == "" {
			continue
		}
		if item.Remove {
			index, ok := positions[item.Name]
			if !ok {
				continue
			}
			result = append(result[:index], result[index+1:]...)
			positions = rebuildEnvVarPositions(result)
			continue
		}
		if index, ok := positions[item.Name]; ok {
			result[index] = item
		} else {
			positions[item.Name] = len(result)
			result = append(result, item)
		}
	}
	return result
}

func rebuildEnvVarPositions(items []envVar) map[string]int {
	positions := map[string]int{}
	for index, item := range items {
		positions[item.Name] = index
	}
	return positions
}

func varsToNode(items []envVar) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode}
	for _, item := range items {
		itemNode := &yaml.Node{Kind: yaml.MappingNode}
		itemNode.Content = append(itemNode.Content, scalarNode("name"), scalarNode(item.Name))
		switch {
		case item.SecretName != "":
			itemNode.Content = append(itemNode.Content, scalarNode("secret_name"), scalarNode(item.SecretName))
			itemNode.Content = append(itemNode.Content, scalarNode("key"), scalarNode(item.Key))
		case item.ResourceName != "":
			itemNode.Content = append(itemNode.Content, scalarNode("resource_name"), scalarNode(item.ResourceName))
			itemNode.Content = append(itemNode.Content, scalarNode("divisor"), scalarNode(item.Divisor))
		case item.FieldPath != "":
			itemNode.Content = append(itemNode.Content, scalarNode("field_path"), scalarNode(item.FieldPath))
		case item.LegacyWorkloadIdentityToken != "":
			itemNode.Content = append(
				itemNode.Content,
				scalarNode("workload_identity_token"),
				scalarNode(item.LegacyWorkloadIdentityToken),
			)
		case item.WorkloadIdentityTokenRefName != "":
			itemNode.Content = append(
				itemNode.Content,
				scalarNode("workload_identity_token_ref_name"),
				scalarNode(item.WorkloadIdentityTokenRefName),
			)
		case item.SharedAssetRefName != "":
			itemNode.Content = append(
				itemNode.Content,
				scalarNode("shared_asset_ref_name"),
				scalarNode(item.SharedAssetRefName),
			)
		default:
			itemNode.Content = append(itemNode.Content, scalarNode("value"), scalarNode(item.Value))
		}
		node.Content = append(node.Content, itemNode)
	}
	return node
}

func parseYAMLMapping(content string) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode}, nil
	}
	node := doc.Content[0]
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("YAML root must be a mapping")
	}
	return node, nil
}

func mergeMappingNodes(defaultsNode, appNode *yaml.Node) *yaml.Node {
	result := cloneNode(defaultsNode)
	for i := 0; i+1 < len(appNode.Content); i += 2 {
		key := appNode.Content[i]
		appValue := appNode.Content[i+1]
		existingIndex := mappingKeyIndex(result, key.Value)
		if existingIndex < 0 {
			result.Content = append(result.Content, cloneNode(key), cloneNode(appValue))
			continue
		}
		defaultValue := result.Content[existingIndex+1]
		if defaultValue.Kind == yaml.MappingNode && appValue.Kind == yaml.MappingNode {
			result.Content[existingIndex+1] = mergeMappingNodes(defaultValue, appValue)
		} else {
			result.Content[existingIndex+1] = cloneNode(appValue)
		}
	}
	return result
}

func mergeVarsFromNodes(defaultsNode, appNode *yaml.Node) (*yaml.Node, error) {
	defaultVars, err := parseVariableNode(mappingValue(defaultsNode, "vars"), false)
	if err != nil {
		return nil, err
	}
	appVars, err := parseVariableNode(mappingValue(appNode, "vars"), true)
	if err != nil {
		return nil, err
	}
	merged := mergeVariables(defaultVars, appVars)
	return variablesToNode(merged), nil
}

func parseVariableNode(node *yaml.Node, allowRemove bool) ([]variable, error) {
	if node == nil {
		return nil, nil
	}
	var vars []variable
	if err := node.Decode(&vars); err != nil {
		return nil, err
	}
	for _, item := range vars {
		if item.Name == "" {
			return nil, errors.New("vars item is missing name")
		}
		if item.Remove && !allowRemove {
			return nil, errors.New("remove: true is not allowed in defaults vars")
		}
	}
	return vars, nil
}

func mergeVariables(defaults []variable, overrides []variable) []variable {
	result := make([]variable, 0, len(defaults)+len(overrides))
	positions := map[string]int{}
	for _, item := range defaults {
		positions[item.Name] = len(result)
		result = append(result, item)
	}
	for _, item := range overrides {
		if item.Remove {
			index, ok := positions[item.Name]
			if !ok {
				continue
			}
			result = append(result[:index], result[index+1:]...)
			positions = rebuildVariablePositions(result)
			continue
		}
		if index, ok := positions[item.Name]; ok {
			result[index] = item
		} else {
			positions[item.Name] = len(result)
			result = append(result, item)
		}
	}
	return result
}

func rebuildVariablePositions(vars []variable) map[string]int {
	positions := map[string]int{}
	for index, item := range vars {
		positions[item.Name] = index
	}
	return positions
}

func variablesToNode(vars []variable) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode}
	for _, item := range vars {
		itemNode := &yaml.Node{Kind: yaml.MappingNode}
		itemNode.Content = append(itemNode.Content, scalarNode("name"), scalarNode(item.Name))
		itemNode.Content = append(itemNode.Content, scalarNode("value"), scalarNode(item.Value))
		node.Content = append(node.Content, itemNode)
	}
	return node
}

func mappingKeyIndex(node *yaml.Node, key string) int {
	if node == nil || node.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	index := mappingKeyIndex(node, key)
	if index < 0 {
		return nil
	}
	return node.Content[index+1]
}

func scalarMappingValue(node *yaml.Node, key string) string {
	value := mappingValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func setMappingValue(node *yaml.Node, key string, value *yaml.Node) {
	index := mappingKeyIndex(node, key)
	if index < 0 {
		node.Content = append(node.Content, scalarNode(key), value)
		return
	}
	node.Content[index+1] = value
}

func removeMappingValue(node *yaml.Node, key string) {
	index := mappingKeyIndex(node, key)
	if index < 0 {
		return
	}
	node.Content = append(node.Content[:index], node.Content[index+2:]...)
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func cloneNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	out := *node
	if len(node.Content) > 0 {
		out.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			out.Content[i] = cloneNode(child)
		}
	}
	return &out
}

func renderServiceAccount(app appModel, namespace string) map[string]any {
	if !app.WorkloadIdentity.ServiceAccount.Create {
		return nil
	}
	name := effectiveServiceAccountName(app)
	if name == "" {
		return nil
	}
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ServiceAccount",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
	}
}

func effectiveServiceAccountName(app appModel) string {
	if name := strings.TrimSpace(app.WorkloadIdentity.ServiceAccount.Name); name != "" {
		return name
	}
	if name := strings.TrimSpace(app.ServiceAccount); name != "" {
		return name
	}
	if hasWorkloadIdentity(app) {
		return app.Name
	}
	return ""
}

func effectiveServiceAccountAutomount(app appModel) (bool, bool) {
	if app.WorkloadIdentity.ServiceAccount.Automount != nil {
		return *app.WorkloadIdentity.ServiceAccount.Automount, true
	}
	if len(app.WorkloadIdentity.Tokens) > 0 {
		return false, true
	}
	return false, false
}

func hasWorkloadIdentity(app appModel) bool {
	return app.WorkloadIdentity.ServiceAccount.Create ||
		strings.TrimSpace(app.WorkloadIdentity.ServiceAccount.Name) != "" ||
		app.WorkloadIdentity.ServiceAccount.Automount != nil ||
		len(app.WorkloadIdentity.Tokens) > 0
}

func workloadIdentityAssets(app appModel) []resolvedAsset {
	out := make([]resolvedAsset, 0, len(app.WorkloadIdentity.Tokens))
	for _, token := range app.WorkloadIdentity.Tokens {
		name := strings.TrimSpace(token.Name)
		if name == "" {
			continue
		}
		path := effectiveWorkloadTokenPath(token)
		mountPath := effectiveWorkloadTokenMountPath(token)
		expirationSeconds := token.ExpirationSeconds
		if expirationSeconds == 0 {
			expirationSeconds = 3600
		}
		serviceAccountToken := map[string]any{
			"path":              path,
			"audience":          token.Audience,
			"expirationSeconds": expirationSeconds,
		}
		out = append(out, resolvedAsset{
			VolumeName: name + "-token",
			Kind:       "raw",
			RawVolume: map[string]any{
				"name": name + "-token",
				"projected": map[string]any{
					"sources": []any{
						map[string]any{"serviceAccountToken": serviceAccountToken},
					},
				},
			},
			RawMount: map[string]any{
				"name":      name + "-token",
				"mountPath": mountPath,
				"readOnly":  true,
			},
		})
	}
	return out
}

func effectiveWorkloadTokenPath(token workloadIdentityTokenSpec) string {
	if path := strings.TrimSpace(token.Path); path != "" {
		return path
	}
	return "token"
}

func effectiveWorkloadTokenMountPath(token workloadIdentityTokenSpec) string {
	if mountPath := strings.TrimSpace(token.MountPath); mountPath != "" {
		return mountPath
	}
	return "/var/run/secrets/workload-identity/" + strings.TrimSpace(token.Name)
}

func findWorkloadIdentityToken(app appModel, name string) (workloadIdentityTokenSpec, bool) {
	for _, token := range app.WorkloadIdentity.Tokens {
		if token.Name == name {
			return token, true
		}
	}
	return workloadIdentityTokenSpec{}, false
}

func downwardAPIAssets(app appModel) []resolvedAsset {
	mounts := append([]downwardAPIMountSpec{}, app.DownwardAPI.Mounts...)
	if app.PodInfo.Enabled {
		mountPath := strings.TrimSpace(app.PodInfo.MountPath)
		if mountPath == "" {
			mountPath = "/etc/podinfo"
		}
		mounts = append(mounts, downwardAPIMountSpec{
			Name:      "podinfo",
			MountPath: mountPath,
			Items: []downwardAPIItemSpec{
				{Path: "namespace", FieldPath: "metadata.namespace"},
				{Path: "pod_name", FieldPath: "metadata.name"},
			},
		})
	}
	out := make([]resolvedAsset, 0, len(mounts))
	for _, mount := range mounts {
		name := strings.TrimSpace(mount.Name)
		if name == "" {
			continue
		}
		mountPath := strings.TrimSpace(mount.MountPath)
		if mountPath == "" {
			continue
		}
		items := make([]any, 0, len(mount.Items))
		for _, item := range mount.Items {
			if strings.TrimSpace(item.Path) == "" || strings.TrimSpace(item.FieldPath) == "" {
				continue
			}
			items = append(items, map[string]any{
				"path": strings.TrimSpace(item.Path),
				"fieldRef": map[string]any{
					"fieldPath": strings.TrimSpace(item.FieldPath),
				},
			})
		}
		if len(items) == 0 {
			continue
		}
		out = append(out, resolvedAsset{
			VolumeName: name,
			Kind:       "raw",
			RawVolume: map[string]any{
				"name":        name,
				"downwardAPI": map[string]any{"items": items},
			},
			RawMount: map[string]any{
				"name":      name,
				"mountPath": mountPath,
				"readOnly":  true,
			},
		})
	}
	return out
}

func cloneAssetsMap(items map[string][]resolvedAsset) map[string][]resolvedAsset {
	out := make(map[string][]resolvedAsset, len(items))
	for key, value := range items {
		out[key] = append([]resolvedAsset{}, value...)
	}
	return out
}

func validateReleaseLabelValue(name, value string) error {
	if value == "" {
		return nil
	}
	if len(value) > 63 || !releaseLabelValuePattern.MatchString(value) {
		return fmt.Errorf("%s %q must be a Kubernetes label value of at most 63 characters (letters, digits, dot, underscore or hyphen; start and end with a letter or digit)", name, value)
	}
	return nil
}

func setReleaseLabel(labels map[string]any, appName, key, value string) error {
	if value == "" {
		return nil
	}
	if existing, ok := labels[key]; ok && fmt.Sprint(existing) != value {
		return fmt.Errorf("%s: label %q conflicts with computed release ID", appName, key)
	}
	labels[key] = value
	return nil
}

type releaseContextSpec struct {
	Name                     string
	ReleaseID                string
	ReleaseIDEnvName         string
	CustomerReleaseIDEnvName string
}

func newReleaseContextSpec(opts Options, releaseID string) (*releaseContextSpec, error) {
	name := strings.TrimSpace(opts.ReleaseContextName)
	if name == "" {
		name = "release-context"
	}
	if len(name) > 63 || !releaseContextNamePattern.MatchString(name) {
		return nil, fmt.Errorf("release context ConfigMap name %q must be a DNS label of at most 63 characters; use --release-context-name to override", name)
	}
	releaseEnv := strings.TrimSpace(opts.ReleaseIDEnvName)
	if releaseEnv == "" {
		releaseEnv = defaultReleaseIDEnvName
	}
	customerEnv := strings.TrimSpace(opts.CustomerReleaseIDEnvName)
	if customerEnv == "" {
		customerEnv = defaultCustomerReleaseIDEnvName
	}
	if !releaseContextEnvNamePattern.MatchString(releaseEnv) || !releaseContextEnvNamePattern.MatchString(customerEnv) || releaseEnv == customerEnv {
		return nil, fmt.Errorf("release context ENV names %q and %q must be distinct valid environment variable names", releaseEnv, customerEnv)
	}
	return &releaseContextSpec{Name: name, ReleaseID: releaseID, ReleaseIDEnvName: releaseEnv, CustomerReleaseIDEnvName: customerEnv}, nil
}

func releaseContextNameFromDefaults(path string) (string, error) {
	content, err := readOptionalFile(path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		return "", nil
	}
	node, err := parseYAMLMapping(content)
	if err != nil {
		return "", fmt.Errorf("_defaults.yml: %w", err)
	}
	value := mappingValue(node, "release_context_name")
	if value == nil {
		return "", nil
	}
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.TrimSpace(value.Value) == "" {
		return "", errors.New("_defaults.yml release_context_name must be a nonempty string")
	}
	return strings.TrimSpace(value.Value), nil
}

func renderReleaseContextConfigMap(ctx releaseContextSpec, namespace, customerReleaseID string, argoCDWave int) map[string]any {
	data := map[string]any{defaultReleaseIDEnvName: ctx.ReleaseID}
	if customerReleaseID != "" {
		data[defaultCustomerReleaseIDEnvName] = customerReleaseID
	}
	metadata := map[string]any{"name": ctx.Name, "namespace": namespace}
	if argoCDWave != 0 {
		metadata["annotations"] = map[string]any{"argocd.argoproj.io/sync-wave": fmt.Sprint(argoCDWave * -1)}
	}
	return map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "metadata": metadata, "data": data}
}

func addReleaseContextEnvs(container map[string]any, appName string, ctx releaseContextSpec) error {
	var envs []map[string]any
	if current, ok := container["env"]; ok {
		switch values := current.(type) {
		case []map[string]any:
			envs = values
		case []any:
			for _, value := range values {
				entry, valid := value.(map[string]any)
				if !valid {
					return fmt.Errorf("%s/%v: cannot inject release context into unsupported raw env list", appName, container["name"])
				}
				envs = append(envs, entry)
			}
		default:
			return fmt.Errorf("%s/%v: cannot inject release context into unsupported raw env list", appName, container["name"])
		}
	}
	for _, item := range []struct {
		name, key string
		optional  bool
	}{
		{ctx.ReleaseIDEnvName, defaultReleaseIDEnvName, false},
		{ctx.CustomerReleaseIDEnvName, defaultCustomerReleaseIDEnvName, true},
	} {
		ref := map[string]any{"name": ctx.Name, "key": item.key}
		if item.optional {
			ref["optional"] = true
		}
		computed := map[string]any{"name": item.name, "valueFrom": map[string]any{"configMapKeyRef": ref}}
		found := false
		for _, existing := range envs {
			if existing["name"] != item.name {
				continue
			}
			if !reflect.DeepEqual(existing, computed) {
				return fmt.Errorf("%s/%v: ENV %q conflicts with generated release context reference", appName, container["name"], item.name)
			}
			found = true
			break
		}
		if !found {
			envs = append(envs, computed)
		}
	}
	container["env"] = envs
	return nil
}

func renderDeployment(app appModel, namespace string, assets map[string][]resolvedAsset, sharedAssets []resolvedAsset, rolloutAnnotations map[string]string, releaseID, customerReleaseID string, releaseContext *releaseContextSpec, injectReleaseContextEnv bool) (map[string]any, error) {
	labels := map[string]any{appLabel: app.Name}
	for key, value := range app.Labels {
		labels[key] = value
	}
	for _, identity := range []struct{ key, value string }{{releaseIDLabel, releaseID}, {customerReleaseIDLabel, customerReleaseID}} {
		if err := setReleaseLabel(labels, app.Name, identity.key, identity.value); err != nil {
			return nil, err
		}
	}
	workloadAssets := workloadIdentityAssets(app)
	workloadAssets = append(workloadAssets, downwardAPIAssets(app)...)
	runtimeVolumes := runtimeAssetVolumeAssets(app)

	containers := make([]map[string]any, 0, len(app.Containers)+len(app.Sidecars))
	for _, container := range app.Containers {
		containerAssets := append([]resolvedAsset{}, assets[container.Name]...)
		containerAssets = append(containerAssets, workloadAssets...)
		containerAssets = append(containerAssets, runtimeAssetMountAssets(app, container.Name, false)...)
		containers = append(containers, renderContainer(app, container, containerAssets, sharedAssets, app.Tools))
	}
	for _, sidecar := range app.Sidecars {
		sidecarAssets := append([]resolvedAsset{}, assets[sidecar.Name]...)
		sidecarAssets = append(sidecarAssets, workloadAssets...)
		sidecarAssets = append(sidecarAssets, runtimeAssetMountAssets(app, sidecar.Name, true)...)
		containers = append(containers, renderContainer(app, sidecar, sidecarAssets, sharedAssets, app.Tools))
	}
	if releaseContext != nil && injectReleaseContextEnv {
		for _, container := range containers {
			if err := addReleaseContextEnvs(container, app.Name, *releaseContext); err != nil {
				return nil, err
			}
		}
	}
	initAssets := assets
	if len(workloadAssets) > 0 {
		initAssets = cloneAssetsMap(assets)
		for _, container := range app.InitContainers {
			initAssets[container.Name] = append(initAssets[container.Name], workloadAssets...)
		}
	}
	initContainers, err := renderInitContainers(app, initAssets, sharedAssets, app.Tools)
	if err != nil {
		return nil, err
	}
	extraVolumes := append([]resolvedAsset{}, workloadAssets...)
	extraVolumes = append(extraVolumes, runtimeVolumes...)

	podSpec := map[string]any{
		"containers":       containers,
		"imagePullSecrets": renderImagePullSecrets(app.Registry),
		"volumes":          renderVolumes(app, assets, sharedAssets, extraVolumes),
	}
	if len(app.SecurityContext) > 0 {
		podSpec["securityContext"] = cloneMap(app.SecurityContext)
	}
	if app.TerminationGrace != nil {
		podSpec["terminationGracePeriodSeconds"] = *app.TerminationGrace
	}
	if name := effectiveServiceAccountName(app); name != "" {
		podSpec["serviceAccountName"] = name
	}
	if automount, ok := effectiveServiceAccountAutomount(app); ok {
		podSpec["automountServiceAccountToken"] = automount
	}
	if app.Pod.ShareProcessNamespace {
		podSpec["shareProcessNamespace"] = true
	}
	if len(app.DNS) > 0 {
		podSpec["hostAliases"] = renderHostAliases(app.DNS)
	}
	applyScheduling(podSpec, app)
	if len(initContainers) > 0 {
		podSpec["initContainers"] = initContainers
	}
	if len(app.Tools) > 0 {
		podSpec["volumes"] = append(podSpec["volumes"].([]any), map[string]any{
			"name":     "app-tools",
			"emptyDir": map[string]any{},
		})
	}
	for key, value := range app.PodRaw {
		podSpec[key] = value
	}

	metadata := map[string]any{
		"labels":    labels,
		"name":      app.Name,
		"namespace": namespace,
	}
	if len(app.Annotations) > 0 {
		metadata["annotations"] = app.Annotations
	}
	templateLabels := map[string]any{appLabel: app.Name}
	for key, value := range effectiveSelectorLabels(app) {
		templateLabels[key] = value
	}
	templateMetadata := map[string]any{"labels": templateLabels}
	podAnnotations := map[string]any{}
	for key, value := range app.PodAnnotations {
		podAnnotations[key] = value
	}
	for key, value := range rolloutAnnotations {
		if existing, ok := podAnnotations[key]; ok && fmt.Sprint(existing) != value {
			return nil, fmt.Errorf("pod annotation %q conflicts with computed rollout checksum", key)
		}
		podAnnotations[key] = value
	}
	if len(podAnnotations) > 0 {
		templateMetadata["annotations"] = podAnnotations
	}

	spec := map[string]any{
		"replicas": app.Replicas,
		"selector": map[string]any{"matchLabels": effectiveSelectorLabels(app)},
		"strategy": renderStrategy(app.Strategy),
		"template": map[string]any{
			"metadata": templateMetadata,
			"spec":     podSpec,
		},
	}
	if app.Kind == "StatefulSet" {
		delete(spec, "strategy")
		spec["updateStrategy"] = map[string]any{"type": "RollingUpdate"}
		serviceName := app.SubdomainName
		if serviceName == "" {
			serviceName = app.Name
		}
		spec["serviceName"] = serviceName
	}

	deployment := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       app.Kind,
		"metadata":   metadata,
		"spec":       spec,
	}
	mergeRawMap(deployment, app.DeploymentRaw)
	return deployment, nil
}

func applyScheduling(podSpec map[string]any, app appModel) {
	nodeSelector := map[string]any{}
	if app.Arch != "" {
		nodeSelector["kubernetes.io/arch"] = app.Arch
	}
	for key, value := range app.NodeSelector {
		nodeSelector[key] = value
	}
	if app.Scheduling.Arch != "" {
		nodeSelector["kubernetes.io/arch"] = app.Scheduling.Arch
	}
	for key, value := range app.Scheduling.NodeSelector {
		nodeSelector[key] = value
	}
	if len(nodeSelector) > 0 {
		podSpec["nodeSelector"] = nodeSelector
	}

	if len(app.Tolerations) > 0 {
		podSpec["tolerations"] = app.Tolerations
	}
	if len(app.Scheduling.Tolerations) > 0 {
		podSpec["tolerations"] = app.Scheduling.Tolerations
	}

	affinity := cloneMap(app.Scheduling.Affinity)
	if selfAntiAffinity := renderSelfAntiAffinity(app); selfAntiAffinity != nil {
		podAntiAffinity, _ := affinity["podAntiAffinity"].(map[string]any)
		if podAntiAffinity == nil {
			podAntiAffinity = map[string]any{}
			affinity["podAntiAffinity"] = podAntiAffinity
		}
		for key, value := range selfAntiAffinity {
			podAntiAffinity[key] = value
		}
	}
	if len(affinity) > 0 {
		podSpec["affinity"] = affinity
	}

	if spread := renderTopologySpread(app); spread != nil {
		podSpec["topologySpreadConstraints"] = []any{spread}
	}
}

func renderSelfAntiAffinity(app appModel) map[string]any {
	mode := strings.TrimSpace(app.Scheduling.AntiAffinity.Self)
	if mode == "" {
		return nil
	}
	topology := strings.TrimSpace(app.Scheduling.AntiAffinity.Topology)
	if topology == "" {
		topology = "kubernetes.io/hostname"
	}
	term := map[string]any{
		"topologyKey": topology,
		"labelSelector": map[string]any{
			"matchLabels": map[string]any{appLabel: app.Name},
		},
	}
	switch mode {
	case "required":
		return map[string]any{"requiredDuringSchedulingIgnoredDuringExecution": []any{term}}
	default:
		return map[string]any{"preferredDuringSchedulingIgnoredDuringExecution": []any{
			map[string]any{"weight": 100, "podAffinityTerm": term},
		}}
	}
}

func renderTopologySpread(app appModel) map[string]any {
	spread := app.Scheduling.Spread
	if spread.By == "" && spread.Topology == "" {
		return nil
	}
	topology := strings.TrimSpace(spread.Topology)
	if topology == "" {
		switch strings.TrimSpace(spread.By) {
		case "zone":
			topology = "topology.kubernetes.io/zone"
		default:
			topology = "kubernetes.io/hostname"
		}
	}
	maxSkew := spread.MaxSkew
	if maxSkew == 0 {
		maxSkew = 1
	}
	whenUnsatisfiable := strings.TrimSpace(spread.WhenUnsatisfiable)
	if whenUnsatisfiable == "" {
		whenUnsatisfiable = "ScheduleAnyway"
	}
	return map[string]any{
		"maxSkew":           maxSkew,
		"topologyKey":       topology,
		"whenUnsatisfiable": whenUnsatisfiable,
		"labelSelector": map[string]any{
			"matchLabels": map[string]any{appLabel: app.Name},
		},
	}
}

func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}

func mergeRawMap(target map[string]any, raw map[string]any) {
	for key, value := range raw {
		if nestedTarget, ok := target[key].(map[string]any); ok {
			if nestedRaw, ok := value.(map[string]any); ok {
				mergeRawMap(nestedTarget, nestedRaw)
				continue
			}
		}
		target[key] = value
	}
}

func rolloutChecksumAnnotations(app appModel, envDir string) (map[string]string, error) {
	if len(app.RolloutOn.Checksums) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for group, config := range app.RolloutOn.Checksums {
		group = strings.TrimSpace(group)
		if group == "" {
			return nil, errors.New("invalid empty rollout checksum group")
		}
		if len(config.Files) == 0 {
			return nil, fmt.Errorf("rollout_on.checksums.%s.files must not be empty", group)
		}
		files := append([]string{}, config.Files...)
		for i, file := range files {
			normalized, err := normalizeRolloutPath(file)
			if err != nil {
				return nil, err
			}
			files[i] = normalized
		}
		sort.Strings(files)
		hash := sha256.New()
		for _, relative := range files {
			fullPath := filepath.Join(envDir, relative)
			content, err := os.ReadFile(fullPath)
			if err != nil {
				return nil, fmt.Errorf("rollout checksum file not found for %q: %s", group, relative)
			}
			hash.Write([]byte(relative))
			hash.Write([]byte{0})
			hash.Write(content)
			hash.Write([]byte{0})
		}
		out["checksum/"+group] = hex.EncodeToString(hash.Sum(nil))
	}
	return out, nil
}

func normalizeRolloutPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "..") || strings.Contains(value, "//") {
		return "", fmt.Errorf("invalid rollout checksum path: %s", value)
	}
	return filepath.ToSlash(value), nil
}

func renderStrategy(value string) map[string]any {
	switch value {
	case "recreate":
		return map[string]any{"type": "Recreate"}
	case "one-by-one":
		return map[string]any{"rollingUpdate": map[string]any{"maxSurge": 0, "maxUnavailable": 1}, "type": "RollingUpdate"}
	default:
		return map[string]any{"rollingUpdate": map[string]any{"maxSurge": "25%", "maxUnavailable": "25%"}, "type": "RollingUpdate"}
	}
}

func effectiveSelectorLabels(app appModel) map[string]any {
	if len(app.SelectorLabels) > 0 {
		return cloneMap(app.SelectorLabels)
	}
	return map[string]any{appLabel: app.Name}
}

func renderBudget(app appModel, namespace string) map[string]any {
	if app.MinAvailable == nil && app.MaxUnavailable == nil {
		return nil
	}
	labels := map[string]any{appLabel: app.Name}
	for key, value := range app.Labels {
		labels[key] = value
	}
	spec := map[string]any{"selector": map[string]any{"matchLabels": effectiveSelectorLabels(app)}}
	if app.MinAvailable != nil {
		spec["minAvailable"] = app.MinAvailable
	} else {
		spec["maxUnavailable"] = app.MaxUnavailable
	}
	return map[string]any{"apiVersion": "policy/v1", "kind": "PodDisruptionBudget", "metadata": map[string]any{"labels": labels, "name": app.Name, "namespace": namespace}, "spec": spec}
}

func renderAutoscaling(app appModel, namespace string) map[string]any {
	if !app.Autoscaling.Enabled {
		return nil
	}
	labels := map[string]any{appLabel: app.Name}
	for key, value := range app.Labels {
		labels[key] = value
	}
	metadata := map[string]any{
		"labels":    labels,
		"name":      app.Name,
		"namespace": namespace,
	}
	if len(app.Annotations) > 0 {
		metadata["annotations"] = cloneMap(app.Annotations)
	}
	hpa := map[string]any{
		"apiVersion": "autoscaling/v2",
		"kind":       "HorizontalPodAutoscaler",
		"metadata":   metadata,
		"spec": map[string]any{
			"scaleTargetRef": map[string]any{
				"apiVersion": "apps/v1",
				"kind":       app.Kind,
				"name":       app.Name,
			},
			"minReplicas": app.Autoscaling.MinReplicas,
			"maxReplicas": app.Autoscaling.MaxReplicas,
			"metrics":     renderAutoscalingMetrics(app.Autoscaling),
		},
	}
	mergeRawMap(hpa, app.Autoscaling.Raw)
	return hpa
}

func renderAutoscalingMetrics(autoscaling autoscalingSpec) []any {
	metrics := []any{}
	if autoscaling.CPU.AverageUtilization > 0 {
		metrics = append(metrics, renderAutoscalingResourceMetric("cpu", autoscaling.CPU.AverageUtilization))
	}
	if autoscaling.Memory.AverageUtilization > 0 {
		metrics = append(metrics, renderAutoscalingResourceMetric("memory", autoscaling.Memory.AverageUtilization))
	}
	return metrics
}

func renderAutoscalingResourceMetric(name string, averageUtilization int) map[string]any {
	return map[string]any{
		"type": "Resource",
		"resource": map[string]any{
			"name": name,
			"target": map[string]any{
				"type":               "Utilization",
				"averageUtilization": averageUtilization,
			},
		},
	}
}

func renderImagePullSecrets(registry []registrySpec) []any {
	out := make([]any, 0, len(registry))
	for _, item := range registry {
		if item.SecretName == "" {
			continue
		}
		out = append(out, map[string]any{"name": item.SecretName})
	}
	return out
}

func renderHostAliases(dns []hostAliasSpec) []any {
	out := make([]any, 0, len(dns))
	for _, item := range dns {
		if item.IP == "" {
			continue
		}
		out = append(out, map[string]any{
			"ip":        item.IP,
			"hostnames": item.Hostnames,
		})
	}
	return out
}

func renderContainer(app appModel, container containerSpec, assets []resolvedAsset, sharedAssets []resolvedAsset, tools []toolSpec) map[string]any {
	mounts := renderVolumeMounts(append(append([]resolvedAsset{}, assets...), sharedAssets...))
	mounts = append(mounts, renderToolVolumeMounts(tools)...)
	out := map[string]any{
		"name":            container.Name,
		"image":           container.Image,
		"imagePullPolicy": mustImagePullPolicy(container.ImagePullPolicy),
		"resources":       renderResources(container.Resources),
		"volumeMounts":    mounts,
	}
	if len(container.Startup.Command) > 0 {
		out["command"] = container.Startup.Command
	}
	if len(container.Startup.Arguments) > 0 {
		out["args"] = container.Startup.Arguments
	}
	if len(container.SecurityContext) > 0 {
		out["securityContext"] = cloneMap(container.SecurityContext)
	}
	vars := effectiveContainerEnvs(container)
	if len(vars) > 0 {
		out["env"] = renderVars(app, vars, sharedAssets)
	}
	if envFrom := renderEnvFrom(container.EnvFrom); len(envFrom) > 0 {
		out["envFrom"] = envFrom
	}
	if len(container.Ports) > 0 {
		out["ports"] = renderContainerPorts(container.Ports)
	}
	if lifecycle := renderLifecycle(container.Lifecycle); lifecycle != nil {
		out["lifecycle"] = lifecycle
	}
	if probe := renderHealth(container.Health, "live"); probe != nil {
		out["livenessProbe"] = probe
	}
	if probe := renderHealth(container.Health, "ready"); probe != nil {
		out["readinessProbe"] = probe
	}
	if probe := renderHealth(container.Probe.Live, "live"); probe != nil {
		out["livenessProbe"] = probe
	}
	if probe := renderHealth(container.Probe.Ready, "live"); probe != nil {
		out["readinessProbe"] = probe
	}
	if probe := renderHealth(container.Probe.Start, "live"); probe != nil {
		out["startupProbe"] = probe
	}
	for key, probe := range renderProbes(container.Probes) {
		out[key] = probe
	}
	for key, value := range container.Raw {
		out[key] = value
	}
	return out
}

func runtimeAssetVolumeAssets(app appModel) []resolvedAsset {
	out := make([]resolvedAsset, 0, len(app.RuntimeAssets))
	seen := map[string]bool{}
	for _, item := range app.RuntimeAssets {
		if seen[item.Volume.Name] {
			continue
		}
		seen[item.Volume.Name] = true
		out = append(out, runtimeAssetVolumeAsset(item))
	}
	return out
}

func runtimeAssetVolumeAsset(item runtimeAssetSpec) resolvedAsset {
	emptyDir := map[string]any{}
	if item.Volume.Medium != "" {
		emptyDir["medium"] = item.Volume.Medium
	}
	if item.Volume.SizeLimit != "" {
		emptyDir["sizeLimit"] = item.Volume.SizeLimit
	}
	return resolvedAsset{
		VolumeName: item.Volume.Name,
		Kind:       "raw",
		RawVolume: map[string]any{
			"name":     item.Volume.Name,
			"emptyDir": emptyDir,
		},
		RawMount: map[string]any{
			"name":      item.Volume.Name,
			"mountPath": item.Volume.MountPath,
			"readOnly":  true,
		},
	}
}

func runtimeAssetMountAssets(app appModel, containerName string, sidecar bool) []resolvedAsset {
	out := make([]resolvedAsset, 0, len(app.RuntimeAssets))
	seen := map[string]bool{}
	for _, item := range app.RuntimeAssets {
		if runtimeAssetAppliesToContainer(item, containerName, sidecar) && !seen[item.Volume.Name] {
			seen[item.Volume.Name] = true
			out = append(out, runtimeAssetVolumeAsset(item))
		}
	}
	return out
}

func runtimeAssetAppliesToContainer(item runtimeAssetSpec, containerName string, sidecar bool) bool {
	for _, selector := range item.ContainerRefNames {
		if selector == "*" && !sidecar {
			return true
		}
		if selector == containerName {
			return true
		}
	}
	return false
}

func renderRuntimeAssetInitContainers(app appModel, sharedAssets []resolvedAsset) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(app.RuntimeAssets))
	for _, item := range app.RuntimeAssets {
		tokenRefName := item.Source.WorkloadIdentityTokenRefName
		token, ok := findWorkloadIdentityToken(app, tokenRefName)
		if !ok {
			return nil, fmt.Errorf("%s: runtime_assets %q references unknown workload_identity token %q", app.Name, item.Name, tokenRefName)
		}
		filesJSON, err := runtimeAssetFilesJSON(item)
		if err != nil {
			return nil, fmt.Errorf("%s: runtime_assets %q: %w", app.Name, item.Name, err)
		}
		tokenMountPath := effectiveWorkloadTokenMountPath(token)
		tokenFile := filepath.ToSlash(filepath.Join(tokenMountPath, effectiveWorkloadTokenPath(token)))
		arguments := []string{
			"fetch",
			"--upstream",
			item.Source.BaseURL,
			"--token-file",
			tokenFile,
			"--timeout-seconds",
			strconv.Itoa(item.Source.TimeoutSeconds),
			"--output-dir",
			item.Volume.MountPath,
			"--files-json",
			filesJSON,
		}
		if item.Source.InsecureUpstreamTLS {
			arguments = append(arguments, "--insecure-upstream-tls")
		}
		volumeMounts := []any{
			map[string]any{
				"name":      item.Volume.Name,
				"mountPath": item.Volume.MountPath,
			},
			map[string]any{
				"name":      token.Name + "-token",
				"mountPath": tokenMountPath,
				"readOnly":  true,
			},
		}
		caFile := strings.TrimSpace(item.Source.CAFile)
		caAssetRefName := strings.TrimSpace(item.Source.CASharedAssetRefName)
		var caAsset resolvedAsset
		var hasCAAsset bool
		if caAssetRefName != "" {
			caAsset, hasCAAsset = sharedAssetForRefName(sharedAssets, caAssetRefName)
			if hasCAAsset {
				caFile = caAsset.To
			}
		} else if caFile != "" {
			caAsset, hasCAAsset = sharedAssetForMountPath(sharedAssets, caFile)
		}
		if caFile != "" {
			if !hasCAAsset {
				if caAssetRefName != "" {
					return nil, fmt.Errorf(
						"%s: runtime_assets %q source.ca_shared_asset_ref_name references unknown shared asset %q",
						app.Name,
						item.Name,
						caAssetRefName,
					)
				}
				return nil, fmt.Errorf("%s: runtime_assets %q source.ca_file %q does not match any shared asset target", app.Name, item.Name, caFile)
			}
			volumeMounts = append(volumeMounts, renderVolumeMounts([]resolvedAsset{caAsset})...)
			arguments = append(arguments, "--ca-file", caFile)
		}
		fetcher := map[string]any{
			"name":            "runtime-assets-" + item.Name,
			"image":           item.Fetcher.Image,
			"imagePullPolicy": mustImagePullPolicy(item.Fetcher.ImagePullPolicy),
			"command":         []string{item.Fetcher.Command},
			"args":            arguments,
			"resources":       renderResources(runtimeAssetFetcherResources(item.Fetcher.Resources)),
			"volumeMounts":    volumeMounts,
		}
		if len(item.Fetcher.SecurityContext) > 0 {
			fetcher["securityContext"] = cloneMap(item.Fetcher.SecurityContext)
		}
		out = append(out, fetcher)
	}
	return out, nil
}

func sharedAssetForMountPath(sharedAssets []resolvedAsset, mountPath string) (resolvedAsset, bool) {
	for _, asset := range sharedAssets {
		if asset.To == mountPath {
			return asset, true
		}
	}
	return resolvedAsset{}, false
}

func sharedAssetForRefName(sharedAssets []resolvedAsset, refName string) (resolvedAsset, bool) {
	for _, asset := range sharedAssets {
		if asset.RefName == refName {
			return asset, true
		}
	}
	return resolvedAsset{}, false
}

func runtimeAssetFilesJSON(item runtimeAssetSpec) (string, error) {
	type fetchFile struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Mode   string `json:"mode"`
		SHA256 string `json:"sha256,omitempty"`
	}
	files := make([]fetchFile, 0, len(item.Files))
	for _, file := range item.Files {
		source := runtimeAssetAPIPath(item.Source, file.Source)
		files = append(files, fetchFile{
			Source: source,
			Target: file.Target,
			Mode:   file.Mode,
			SHA256: file.SHA256,
		})
	}
	content, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func runtimeAssetAPIPath(source runtimeAssetSourceSpec, file string) string {
	segments := []string{
		"api", "v1", "tenants", source.Tenant, "envs", source.Environment, "assets",
	}
	if source.Label != "" {
		segments = append(segments, source.Label)
	}
	segments = append(segments, strings.Split(file, "/")...)
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return "/" + strings.Join(segments, "/")
}

func runtimeAssetFetcherResources(overrides map[string]map[string]any) map[string]map[string]any {
	resources := map[string]map[string]any{
		"cpu": {
			"from": "10m",
			"to":   "100m",
		},
		"memory": {
			"from": "16Mi",
			"to":   "128Mi",
		},
	}
	for resource, values := range overrides {
		if resources[resource] == nil {
			resources[resource] = map[string]any{}
		}
		for key, value := range values {
			resources[resource][key] = value
		}
	}
	return resources
}

func renderInitContainers(app appModel, assets map[string][]resolvedAsset, sharedAssets []resolvedAsset, tools []toolSpec) ([]map[string]any, error) {
	out := renderToolInitContainers(app.Tools)
	runtimeFetchers, err := renderRuntimeAssetInitContainers(app, sharedAssets)
	if err != nil {
		return nil, err
	}
	out = append(out, runtimeFetchers...)
	for _, container := range app.InitContainers {
		out = append(out, renderInitContainer(app, container, assets[container.Name], sharedAssets, tools))
	}
	return out, nil
}

func renderInitContainer(app appModel, container initContainerSpec, assets, sharedAssets []resolvedAsset, tools []toolSpec) map[string]any {
	mounts := renderVolumeMounts(append(append([]resolvedAsset{}, assets...), sharedAssets...))
	mounts = append(mounts, renderToolVolumeMounts(tools)...)
	out := map[string]any{
		"name":            container.Name,
		"image":           container.Image,
		"imagePullPolicy": mustImagePullPolicy(container.ImagePullPolicy),
	}
	if len(container.Resources) > 0 {
		out["resources"] = renderResources(container.Resources)
	}
	if len(mounts) > 0 {
		out["volumeMounts"] = mounts
	}
	if len(container.Command) > 0 {
		out["command"] = container.Command
	}
	if len(container.Arguments) > 0 {
		out["args"] = container.Arguments
	}
	if len(container.SecurityContext) > 0 {
		out["securityContext"] = cloneMap(container.SecurityContext)
	}
	if vars := effectiveInitContainerEnvs(container); len(vars) > 0 {
		out["env"] = renderVars(app, vars, sharedAssets)
	}
	if envFrom := renderEnvFrom(container.EnvFrom); len(envFrom) > 0 {
		out["envFrom"] = envFrom
	}
	for key, value := range container.Raw {
		out[key] = value
	}
	return out
}

func renderEnvFrom(items []envFromSpec) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		if len(item.Raw) > 0 {
			out = append(out, cloneMap(item.Raw))
			continue
		}
		entry := map[string]any{}
		if len(item.ConfigMapRef) > 0 {
			entry["configMapRef"] = cloneMap(item.ConfigMapRef)
		} else if strings.TrimSpace(item.ConfigMap) != "" {
			entry["configMapRef"] = map[string]any{"name": strings.TrimSpace(item.ConfigMap)}
		}
		if len(item.SecretRef) > 0 {
			entry["secretRef"] = cloneMap(item.SecretRef)
		} else if strings.TrimSpace(item.Secret) != "" {
			entry["secretRef"] = map[string]any{"name": strings.TrimSpace(item.Secret)}
		}
		if strings.TrimSpace(item.Prefix) != "" {
			entry["prefix"] = strings.TrimSpace(item.Prefix)
		}
		if len(entry) > 0 {
			out = append(out, entry)
		}
	}
	return out
}

func renderLifecycle(lifecycle lifecycleSpec) map[string]any {
	out := map[string]any{}
	if preStop := renderLifecycleHandler(lifecycle.PreStop); preStop != nil {
		out["preStop"] = preStop
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func renderLifecycleHandler(handler lifecycleHandlerSpec) map[string]any {
	if len(handler.Raw) > 0 {
		return cloneMap(handler.Raw)
	}
	if len(handler.Command) > 0 {
		return map[string]any{"exec": map[string]any{"command": handler.Command}}
	}
	if hasHTTPHealth(handler.HTTP) {
		return map[string]any{"httpGet": map[string]any{
			"path": handler.HTTP.Path.For("live"),
			"port": int(handler.HTTP.Port),
		}}
	}
	return nil
}

func renderProbes(probes probesSpec) map[string]any {
	out := map[string]any{}
	if isEmptyProbes(probes) {
		return out
	}

	for _, item := range []struct {
		key      string
		kind     string
		override healthSpec
	}{
		{key: "livenessProbe", kind: "live", override: probes.Live},
		{key: "readinessProbe", kind: "ready", override: probes.Ready},
		{key: "startupProbe", kind: "start", override: probes.Start},
	} {
		if probe := renderHealth(resolveProbeHealth(probes, item.kind, item.override), item.kind); probe != nil {
			out[item.key] = probe
		}
	}
	return out
}

func resolveProbeHealth(probes probesSpec, kind string, override healthSpec) healthSpec {
	health := defaultProbeHealth(probes, kind)
	baseHTTP := commonProbeHTTP(probes, kind)
	if hasHTTPHealth(baseHTTP) {
		health.HTTP = baseHTTP
	}

	if len(override.Command) > 0 {
		health.Command = override.Command
		health.HTTP = httpHealthSpec{}
	}
	if hasHTTPHealth(override.HTTP) {
		if override.HTTP.Port != 0 {
			health.HTTP.Port = override.HTTP.Port
		}
		if !override.HTTP.Path.Empty() {
			health.HTTP.Path = override.HTTP.Path
		}
		health.Command = nil
	}
	if override.Delay != nil {
		health.Delay = override.Delay
	}
	if override.Period != nil {
		health.Period = override.Period
	}
	if override.Timeout != nil {
		health.Timeout = override.Timeout
	}
	if override.Success != nil {
		health.Success = override.Success
	}
	if override.Failure != nil {
		health.Failure = override.Failure
	}
	return health
}

func defaultProbeHealth(probes probesSpec, kind string) healthSpec {
	preset := strings.TrimSpace(probes.Preset)
	if preset == "" {
		return healthSpec{}
	}

	switch kind {
	case "live":
		return healthSpec{Period: intPtr(10), Timeout: intPtr(2), Success: intPtr(1), Failure: intPtr(5)}
	case "ready":
		return healthSpec{Period: intPtr(2), Timeout: intPtr(2), Success: intPtr(2), Failure: intPtr(2)}
	case "start":
		return healthSpec{Period: intPtr(10), Timeout: intPtr(2), Failure: intPtr(30)}
	default:
		return healthSpec{}
	}
}

func commonProbeHTTP(probes probesSpec, kind string) httpHealthSpec {
	http := probes.HTTP
	if http.Port == 0 {
		http.Port = probes.Port
	}
	if http.Path.Empty() {
		http.Path = probes.Path
	}

	if strings.TrimSpace(probes.Preset) == "spring-actuator" && http.Path.Empty() {
		switch kind {
		case "live":
			http.Path = scalarPath("/actuator/health/liveness")
		case "ready":
			http.Path = scalarPath("/actuator/health/readiness")
		case "start":
			http.Path = scalarPath("/actuator/health")
		}
	}
	return http
}

func isEmptyProbes(probes probesSpec) bool {
	return strings.TrimSpace(probes.Preset) == "" &&
		probes.Port == 0 &&
		probes.Path.Empty() &&
		!hasHTTPHealth(probes.HTTP) &&
		isEmptyHealth(probes.Live) &&
		isEmptyHealth(probes.Ready) &&
		isEmptyHealth(probes.Start)
}

func isEmptyHealth(health healthSpec) bool {
	return !hasHTTPHealth(health.HTTP) &&
		len(health.Command) == 0 &&
		health.Delay == nil &&
		health.Period == nil &&
		health.Timeout == nil &&
		health.Success == nil &&
		health.Failure == nil
}

func hasHTTPHealth(http httpHealthSpec) bool {
	return http.Port != 0 || !http.Path.Empty()
}

func scalarPath(value string) pathValue {
	return pathValue{Scalar: value}
}

func intPtr(value int) *int {
	return &value
}

func renderHealth(health healthSpec, probeType string) map[string]any {
	out := map[string]any{}
	if health.HTTP.Port != 0 || health.HTTP.Path.For(probeType) != "" {
		out["httpGet"] = map[string]any{
			"path": health.HTTP.Path.For(probeType),
			"port": int(health.HTTP.Port),
		}
	} else if len(health.Command) > 0 {
		out["exec"] = map[string]any{
			"command": health.Command,
		}
	}
	if len(out) == 0 {
		return nil
	}
	if health.Delay != nil {
		out["initialDelaySeconds"] = *health.Delay
	}
	if health.Period != nil {
		out["periodSeconds"] = *health.Period
	}
	if health.Timeout != nil {
		out["timeoutSeconds"] = *health.Timeout
	}
	if health.Success != nil {
		out["successThreshold"] = *health.Success
	}
	if health.Failure != nil {
		out["failureThreshold"] = *health.Failure
	}
	return out
}

func resolveAssets(app appModel, envDir string, vars map[string]string, opts Options) (map[string][]resolvedAsset, error) {
	out := map[string][]resolvedAsset{}
	for _, container := range app.Containers {
		assetSpecs := append([]assetSpec{}, container.Assets...)
		assetSpecs = append(assetSpecs, mtlsAssetSpecs(app.Name, container)...)
		for _, item := range assetSpecs {
			asset, err := resolveAsset(app.Name, container.Name, item, envDir, vars, opts)
			if err != nil {
				return nil, err
			}
			out[container.Name] = append(out[container.Name], asset)
		}
		for _, item := range container.Mounts {
			asset, err := resolveMount(app.Name, container.Name, item, envDir, vars, opts)
			if err != nil {
				return nil, err
			}
			out[container.Name] = append(out[container.Name], asset)
		}
	}
	for _, container := range app.Sidecars {
		assetSpecs := append([]assetSpec{}, container.Assets...)
		for _, item := range assetSpecs {
			asset, err := resolveAsset(app.Name, container.Name, item, envDir, vars, opts)
			if err != nil {
				return nil, err
			}
			out[container.Name] = append(out[container.Name], asset)
		}
		for _, item := range container.Mounts {
			asset, err := resolveMount(app.Name, container.Name, item, envDir, vars, opts)
			if err != nil {
				return nil, err
			}
			out[container.Name] = append(out[container.Name], asset)
		}
	}
	for _, container := range app.InitContainers {
		for _, item := range container.Mounts {
			asset, err := resolveMount(app.Name, container.Name, item, envDir, vars, opts)
			if err != nil {
				return nil, err
			}
			out[container.Name] = append(out[container.Name], asset)
		}
	}
	return out, nil
}

func loadSharedAssets(envDir string, vars map[string]string, opts Options) ([]resolvedAsset, error) {
	path := filepath.Join(envDir, "shared.assets.yml")
	if !isFile(path) {
		return nil, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var parsed sharedAssetsFile
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := make([]resolvedAsset, 0, len(parsed.Assets))
	refNames := map[string]bool{}
	for _, item := range parsed.Assets {
		refName := strings.TrimSpace(item.Name)
		if refName != "" {
			if !isDNSLabel(refName) {
				return nil, fmt.Errorf("%s: shared asset name %q must be a Kubernetes DNS label", path, refName)
			}
			if refNames[refName] {
				return nil, fmt.Errorf("%s: duplicate shared asset name %q", path, refName)
			}
			refNames[refName] = true
		}
		asset, err := resolveAsset("shared", "shared", assetSpec{
			File:       item.File,
			To:         item.To,
			Binary:     item.Binary,
			Transform:  item.Transform,
			HelmEscape: item.HelmEscape,
		}, envDir, vars, opts)
		if err != nil {
			return nil, err
		}
		asset.RefName = refName
		out = append(out, asset)
	}
	return out, nil
}

func mtlsAssetSpecs(appName string, container containerSpec) []assetSpec {
	if !container.MTLS.Enabled {
		return nil
	}
	mountDir := mtlsMountDir(container)
	return []assetSpec{
		{
			File:      filepath.ToSlash(filepath.Join("mtls", appName, container.Name+".secured.json")),
			To:        mountDir + "/mtls.secured.json",
			Transform: false,
			Binary:    false,
		},
		{
			File:      filepath.ToSlash(filepath.Join("mtls", appName, container.Name+".secured.schema.json")),
			To:        mountDir + "/mtls.secured.schema.json",
			Transform: false,
			Binary:    false,
		},
	}
}

func mtlsMountDir(container containerSpec) string {
	mountDir := strings.TrimSpace(container.MTLS.MountDir)
	if mountDir == "" {
		return "/app/mtls.enc"
	}
	return mountDir
}

func profilesPayload(envDir string, opts Options) any {
	path := strings.TrimSpace(opts.ProfilesFile)
	if path == "" {
		path = filepath.Join(envDir, "replica-profiles.yml")
	}
	if !isFile(path) {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{"file": path, "error": err.Error()}
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return map[string]any{"file": path, "error": err.Error()}
	}
	defaults, _ := data["defaults"].(map[string]any)
	profiles, _ := data["profiles"].(map[string]any)
	if defaults == nil {
		defaults = map[string]any{}
	}
	if profiles == nil {
		profiles = map[string]any{}
	}
	return map[string]any{
		"file":     path,
		"defaults": defaults,
		"profiles": profiles,
	}
}

func resolveAsset(appName string, containerName string, spec assetSpec, envDir string, vars map[string]string, opts Options) (resolvedAsset, error) {
	if spec.To == "" {
		return resolvedAsset{}, fmt.Errorf("asset %s target path is required", spec.File)
	}
	targetPath := spec.To
	if opts.LegacyApplyEnv {
		var err error
		targetPath, err = resolveLegacyEnvironmentValue(targetPath, vars)
		if err != nil {
			return resolvedAsset{}, fmt.Errorf("asset %s target path: %w", spec.File, err)
		}
	}
	if spec.Temp {
		content := targetPath
		digest := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(spec.File+targetPath+content)))
		return resolvedAsset{VolumeName: appName + "-temp-" + digest, ContainerName: containerName, To: targetPath, Kind: "temp"}, nil
	}
	if spec.PVC {
		content := targetPath
		digest := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(spec.File+targetPath+content)))
		return resolvedAsset{VolumeName: appName + "-pvc-" + digest, ContainerName: containerName, To: targetPath, Kind: "pvc", PVCName: spec.Name}, nil
	}
	if spec.NFSServer != "" {
		content := spec.Path
		digest := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(spec.File+targetPath+content)))
		return resolvedAsset{VolumeName: appName + "-nfs-" + digest, ContainerName: containerName, To: targetPath, Kind: "nfs", NFSServer: spec.NFSServer, NFSPath: spec.Path}, nil
	}
	if spec.HostPath != "" {
		content := spec.HostPath + targetPath
		digest := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(spec.File+targetPath+content)))
		return resolvedAsset{VolumeName: appName + "-host-" + digest, ContainerName: containerName, To: targetPath, Kind: "host", HostPath: spec.HostPath}, nil
	}
	if spec.File == "" {
		return resolvedAsset{}, errors.New("asset file is required")
	}
	sourcePath := filepath.Join(envDir, spec.File)
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return resolvedAsset{}, err
	}
	renderedContent := content
	if spec.Transform && !spec.Binary {
		renderedContent = []byte(applyEnvPlainPlaceholders(string(content), vars))
	}
	if assetHelmEscape(spec, opts) && !spec.Binary {
		renderedContent = []byte(helmEscapePlaceholders(string(renderedContent)))
	}
	digestInput := spec.File + targetPath + string(renderedContent)
	digest := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(digestInput)))
	key := filepath.Base(sourcePath)
	if spec.Transform {
		key = nameWithoutLastExt(key)
	}
	return resolvedAsset{
		VolumeName:    containerName + "-asset-" + digest,
		ConfigMapKey:  key,
		ContainerName: containerName,
		SourceFile:    spec.File,
		To:            targetPath,
		Content:       renderedContent,
		Binary:        spec.Binary,
		Kind:          "configmap",
	}, nil
}

func resolveMount(appName string, containerName string, spec mountSpec, envDir string, vars map[string]string, opts Options) (resolvedAsset, error) {
	switch strings.TrimSpace(spec.Type) {
	case "config":
		return resolveAsset(appName, containerName, assetSpec{
			File:       spec.File,
			To:         spec.MountPath,
			Binary:     spec.Binary,
			Transform:  spec.Transform,
			HelmEscape: spec.HelmEscape,
		}, envDir, vars, opts)
	case "empty_dir":
		asset, err := resolveAsset(appName, containerName, assetSpec{Temp: true, To: spec.MountPath}, envDir, vars, opts)
		return withMountVolumeName(asset, spec.Name), err
	case "pvc":
		claimName := spec.ClaimName
		if claimName == "" {
			claimName = spec.Name
		}
		asset, err := resolveAsset(appName, containerName, assetSpec{PVC: true, Name: claimName, To: spec.MountPath}, envDir, vars, opts)
		return withMountVolumeName(asset, spec.Name), err
	case "nfs":
		asset, err := resolveAsset(appName, containerName, assetSpec{NFSServer: spec.Server, Path: spec.Path, To: spec.MountPath}, envDir, vars, opts)
		return withMountVolumeName(asset, spec.Name), err
	case "host_path":
		asset, err := resolveAsset(appName, containerName, assetSpec{HostPath: spec.Path, To: spec.MountPath}, envDir, vars, opts)
		return withMountVolumeName(asset, spec.Name), err
	case "raw":
		volumeName := rawVolumeName(spec)
		if volumeName == "" {
			return resolvedAsset{}, errors.New("raw mount requires volume.name or mount.name")
		}
		if len(spec.Volume) == 0 {
			return resolvedAsset{}, errors.New("raw mount requires volume")
		}
		if len(spec.Mount) == 0 {
			return resolvedAsset{}, errors.New("raw mount requires mount")
		}
		return resolvedAsset{VolumeName: volumeName, ContainerName: containerName, Kind: "raw", RawVolume: spec.Volume, RawMount: spec.Mount}, nil
	case "":
		return resolvedAsset{}, errors.New("mount type is required")
	default:
		return resolvedAsset{}, fmt.Errorf("unsupported mount type: %s", spec.Type)
	}
}

func withMountVolumeName(asset resolvedAsset, name string) resolvedAsset {
	if strings.TrimSpace(name) != "" {
		asset.VolumeName = strings.TrimSpace(name)
	}
	return asset
}

func rawVolumeName(spec mountSpec) string {
	if name, _ := spec.Volume["name"].(string); name != "" {
		return name
	}
	if name, _ := spec.Mount["name"].(string); name != "" {
		return name
	}
	return spec.Name
}

func renderVolumes(app appModel, assets map[string][]resolvedAsset, sharedAssets []resolvedAsset, extraAssets []resolvedAsset) []any {
	all := orderedResolvedAssets(app, assets)
	all = append(sharedAssets, all...)
	all = append(all, extraAssets...)
	out := make([]any, 0, len(all))
	seenVolumes := map[string]bool{}
	for _, asset := range all {
		if seenVolumes[asset.VolumeName] {
			continue
		}
		seenVolumes[asset.VolumeName] = true
		volume := map[string]any{"name": asset.VolumeName}
		switch asset.Kind {
		case "temp":
			volume["emptyDir"] = map[string]any{}
		case "pvc":
			volume["persistentVolumeClaim"] = map[string]any{"claimName": asset.PVCName}
		case "nfs":
			volume["nfs"] = map[string]any{"server": asset.NFSServer, "path": asset.NFSPath}
		case "host":
			volume["hostPath"] = map[string]any{"path": asset.HostPath, "type": "Directory"}
		case "raw":
			volume = cloneMap(asset.RawVolume)
		default:
			volume["configMap"] = map[string]any{"defaultMode": 420, "name": asset.VolumeName}
		}
		out = append(out, volume)
	}
	return out
}

func renderVolumeMounts(assets []resolvedAsset) []any {
	out := make([]any, 0, len(assets))
	for _, asset := range assets {
		if asset.Kind == "raw" {
			out = append(out, cloneMap(asset.RawMount))
			continue
		}
		mount := map[string]any{
			"mountPath": asset.To,
			"name":      asset.VolumeName,
		}
		if asset.Kind == "configmap" || asset.Kind == "" {
			mount["readOnly"] = true
			mount["subPath"] = asset.ConfigMapKey
		}
		out = append(out, mount)
	}
	return out
}

func renderAssetConfigMap(asset resolvedAsset, namespace string, argocdWave int) map[string]any {
	obj := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":      asset.VolumeName,
			"namespace": namespace,
		},
	}
	if argocdWave != 0 {
		obj["metadata"].(map[string]any)["annotations"] = map[string]any{
			"argocd.argoproj.io/sync-wave": fmt.Sprint(argocdWave * -1),
		}
	}
	if asset.Binary {
		obj["binaryData"] = map[string]any{
			asset.ConfigMapKey: base64.StdEncoding.EncodeToString(asset.Content),
		}
	} else {
		obj["data"] = map[string]any{
			asset.ConfigMapKey: string(asset.Content),
		}
	}
	return obj
}

func appArgoCDWave(app appModel) int {
	for key, value := range app.Annotations {
		if key != "argocd.argoproj.io/sync-wave" {
			continue
		}
		switch typed := value.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		case float64:
			return int(typed)
		case string:
			parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
			return parsed
		default:
			parsed, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
			return parsed
		}
	}
	return 0
}

func appsHaveArgoCDWave(apps []appModel) bool {
	for _, app := range apps {
		if appArgoCDWave(app) != 0 {
			return true
		}
	}
	return false
}

func allResolvedAssets(assets map[string][]resolvedAsset) []resolvedAsset {
	keys := make([]string, 0, len(assets))
	for key := range assets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []resolvedAsset{}
	for _, key := range keys {
		out = append(out, assets[key]...)
	}
	return out
}

func orderedResolvedAssets(app appModel, assets map[string][]resolvedAsset) []resolvedAsset {
	out := []resolvedAsset{}
	seen := map[string]bool{}
	for _, container := range app.Containers {
		out = append(out, assets[container.Name]...)
		seen[container.Name] = true
	}
	for _, container := range app.Sidecars {
		out = append(out, assets[container.Name]...)
		seen[container.Name] = true
	}
	keys := make([]string, 0, len(assets))
	for key := range assets {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		out = append(out, assets[key]...)
	}
	return out
}

func configMapResolvedAssets(assets map[string][]resolvedAsset) []resolvedAsset {
	all := allResolvedAssets(assets)
	out := make([]resolvedAsset, 0, len(all))
	for _, asset := range all {
		if asset.Kind == "configmap" || asset.Kind == "" {
			out = append(out, asset)
		}
	}
	return out
}

func assetHelmEscape(spec assetSpec, opts Options) bool {
	if spec.HelmEscape != nil {
		return *spec.HelmEscape
	}
	return opts.HelmEscapeAssets
}

func helmEscapePlaceholders(content string) string {
	pattern := regexp.MustCompile(`(\{\{\s*)(\w+)(\s*\}\})`)
	indexes := pattern.FindAllStringIndex(content, -1)
	if len(indexes) == 0 {
		return content
	}
	var out strings.Builder
	last := 0
	for _, index := range indexes {
		start, end := index[0], index[1]
		match := content[start:end]
		out.WriteString(content[last:start])
		if start >= 3 && end+3 <= len(content) && content[start-3:start] == "{{`" && content[end:end+3] == "`}}" {
			out.WriteString(match)
		} else {
			out.WriteString("{{`")
			out.WriteString(match)
			out.WriteString("`}}")
		}
		last = end
	}
	out.WriteString(content[last:])
	return out.String()
}

func nameWithoutLastExt(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return strings.TrimSuffix(name, ext)
}

func renderContainerPorts(ports []portSpec) []map[string]any {
	out := make([]map[string]any, 0, len(ports))
	for _, port := range ports {
		out = append(out, map[string]any{
			"name":          port.Name,
			"containerPort": port.Port,
			"protocol":      "TCP",
		})
	}
	return out
}

type renderedService struct {
	Name      string
	Object    map[string]any
	Externals []renderedExternal
}

type renderedExternal struct {
	Name   string
	Kind   string
	Object map[string]any
}

type syncMetadataSpec struct {
	ID    string
	Order int
}

func renderServices(app appModel, namespace string, environment string) []renderedService {
	servicesByHost := map[string][]map[string]any{}
	externalPortsByHost := map[string][]serviceExternalPort{}
	headlessByHost := map[string]bool{}
	metricsPathForByHost := map[string]string{}
	hasMetricsByHost := map[string]bool{}

	for _, container := range app.Containers {
		for _, port := range container.Ports {
			for _, expose := range port.ExposeAs {
				serviceName := expose.ServiceName
				if serviceName == "" {
					serviceName = expose.Hostname
				}
				if serviceName == "" {
					continue
				}
				serviceType := expose.Type
				if serviceType == "" {
					serviceType = "clusterip"
				}
				servicePort := map[string]any{
					"name":       fmt.Sprintf("%s-%d", port.Name, expose.Port),
					"port":       expose.Port,
					"targetPort": port.Port,
				}
				servicesByHost[serviceName] = append(servicesByHost[serviceName], servicePort)
				if len(expose.External) > 0 {
					externalPortsByHost[serviceName] = append(externalPortsByHost[serviceName], serviceExternalPort{
						Name:      fmt.Sprintf("%s-%d", port.Name, expose.Port),
						Port:      expose.Port,
						Externals: expose.External,
					})
				}
				if serviceType == "headless" {
					headlessByHost[serviceName] = true
				}
				if port.Metrics {
					metricsPort := map[string]any{
						"name":       "metrics",
						"port":       9090,
						"targetPort": port.Port,
					}
					servicesByHost[serviceName] = append(servicesByHost[serviceName], metricsPort)
					if len(expose.External) > 0 {
						externalPortsByHost[serviceName] = append(externalPortsByHost[serviceName], serviceExternalPort{
							Name:      "metrics",
							Port:      9090,
							Externals: expose.External,
						})
					}
					hasMetricsByHost[serviceName] = true
					if port.MetricsPathFor != nil {
						metricsPathForByHost[serviceName] = serviceName
					}
				}
			}
		}
	}

	names := make([]string, 0, len(servicesByHost))
	for name := range servicesByHost {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]renderedService, 0, len(names))
	for _, name := range names {
		metadata := map[string]any{
			"name":      name,
			"namespace": namespace,
		}
		if hasMetricsByHost[name] {
			labels := map[string]any{
				appLabel:      app.Name,
				"metrics":     "true",
				"application": app.Name,
				"environment": environment,
			}
			if metricsPathForByHost[name] != "" {
				labels["metricsPathFor"] = metricsPathForByHost[name]
			}
			metadata["labels"] = labels
		}
		spec := map[string]any{
			"selector": effectiveSelectorLabels(app),
			"ports":    servicesByHost[name],
		}
		if headlessByHost[name] {
			spec["clusterIP"] = "None"
		}
		out = append(out, renderedService{
			Name: name,
			Object: map[string]any{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata":   metadata,
				"spec":       spec,
			},
			Externals: renderExternals(name, namespace, externalPortsByHost[name]),
		})
	}
	return out
}

type serviceExternalPort struct {
	Name      string
	Port      int
	Externals []externalSpec
}

func renderExternals(serviceName string, namespace string, ports []serviceExternalPort) []renderedExternal {
	out := []renderedExternal{}
	for _, port := range ports {
		for _, external := range port.Externals {
			if external.Name == "" {
				continue
			}
			if external.AsRoute {
				out = append(out, renderedExternal{
					Name:   external.Name,
					Kind:   "route",
					Object: renderRoute(serviceName, namespace, port, external),
				})
			} else {
				out = append(out, renderedExternal{
					Name:   external.Name,
					Kind:   "ingress",
					Object: renderIngress(serviceName, namespace, port, external),
				})
			}
		}
	}
	return out
}

func renderIngress(serviceName string, namespace string, port serviceExternalPort, external externalSpec) map[string]any {
	metadata := map[string]any{"name": external.Name, "namespace": namespace}
	if len(external.Annotations) > 0 {
		metadata["annotations"] = external.Annotations
	}
	if len(external.Labels) > 0 {
		metadata["labels"] = external.Labels
	}
	spec := map[string]any{}
	if external.ClassName != "" {
		spec["ingressClassName"] = external.ClassName
	}
	rules := []any{}
	for _, host := range external.HTTP {
		rules = append(rules, map[string]any{
			"host": host.Hostname,
			"http": map[string]any{
				"paths": []any{
					map[string]any{
						"path": host.Path,
						"backend": map[string]any{
							"service": map[string]any{
								"name": serviceName,
								"port": map[string]any{"number": port.Port},
							},
						},
						"pathType": "ImplementationSpecific",
					},
				},
			},
		})
	}
	if len(rules) > 0 {
		spec["rules"] = rules
	}
	tls := []any{}
	for _, host := range external.HTTPS {
		entry := map[string]any{"hosts": []any{host.Hostname}}
		if host.SecretName != "" {
			entry["secretName"] = host.SecretName
		}
		tls = append(tls, entry)
	}
	if len(tls) > 0 {
		spec["tls"] = tls
	}
	return map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "Ingress",
		"metadata":   metadata,
		"spec":       spec,
	}
}

func renderRoute(serviceName string, namespace string, port serviceExternalPort, external externalSpec) map[string]any {
	host := externalHostSpec{}
	if len(external.HTTP) > 0 {
		host = external.HTTP[0]
	}
	spec := map[string]any{
		"host": host.Hostname,
		"port": map[string]any{
			"targetPort": port.Name,
		},
		"to": map[string]any{
			"kind": "Service",
			"name": serviceName,
		},
		"wildcardPolicy": "None",
	}
	if len(external.TLS) > 0 {
		tls := map[string]any{}
		if value, ok := external.TLS["termination"]; ok {
			tls["termination"] = value
		}
		if value, ok := external.TLS["insecureEdgeTerminationPolicy"]; ok {
			tls["insecureEdgeTerminationPolicy"] = value
		}
		if len(tls) > 0 {
			spec["tls"] = tls
		}
	}
	return map[string]any{
		"apiVersion": "route.openshift.io/v1",
		"kind":       "Route",
		"metadata":   map[string]any{"name": external.Name, "namespace": namespace},
		"spec":       spec,
	}
}

func renderVars(app appModel, items []envVar, sharedAssets []resolvedAsset) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		env := map[string]any{"name": item.Name}
		switch {
		case item.SecretName != "":
			env["valueFrom"] = map[string]any{
				"secretKeyRef": map[string]any{
					"key":  item.Key,
					"name": item.SecretName,
				},
			}
		case item.ResourceName != "":
			env["valueFrom"] = map[string]any{
				"resourceFieldRef": map[string]any{
					"resource": item.ResourceName,
					"divisor":  item.Divisor,
				},
			}
		case item.FieldPath != "":
			env["valueFrom"] = map[string]any{
				"fieldRef": map[string]any{
					"fieldPath": item.FieldPath,
				},
			}
		case item.WorkloadIdentityTokenRefName != "":
			token, ok := findWorkloadIdentityToken(app, strings.TrimSpace(item.WorkloadIdentityTokenRefName))
			if ok {
				env["value"] = effectiveWorkloadTokenFile(token)
			}
		case item.SharedAssetRefName != "":
			asset, ok := sharedAssetForRefName(sharedAssets, strings.TrimSpace(item.SharedAssetRefName))
			if ok {
				env["value"] = asset.To
			}
		default:
			env["value"] = item.Value
		}
		out = append(out, env)
	}
	return out
}

func effectiveWorkloadTokenFile(token workloadIdentityTokenSpec) string {
	return filepath.ToSlash(filepath.Join(
		effectiveWorkloadTokenMountPath(token),
		effectiveWorkloadTokenPath(token),
	))
}

func effectiveContainerEnvs(container containerSpec) []envVar {
	items := []envVar{}
	if container.EnableCgroupExporter {
		items = append(items, cgroupExporterDefaultVars...)
	}
	if item, ok := renderJavaRuntimeEnvVar(container.Runtime.Java); ok {
		items = append(items, item)
	}
	items = append(items, container.LegacyEnvVars...)
	items = append(items, container.Envs...)
	if len(items) == 0 {
		return nil
	}
	positions := map[string]int{}
	out := []envVar{}
	for _, item := range items {
		if item.Name == "" {
			continue
		}
		if index, ok := positions[item.Name]; ok {
			out[index] = item
			continue
		}
		positions[item.Name] = len(out)
		out = append(out, item)
	}
	return out
}

func effectiveInitContainerEnvs(container initContainerSpec) []envVar {
	items := append([]envVar{}, container.LegacyEnvVars...)
	items = append(items, container.Envs...)
	if len(items) == 0 {
		return nil
	}
	positions := map[string]int{}
	out := []envVar{}
	for _, item := range items {
		if item.Name == "" {
			continue
		}
		if index, ok := positions[item.Name]; ok {
			out[index] = item
			continue
		}
		positions[item.Name] = len(out)
		out = append(out, item)
	}
	return out
}

func renderJavaRuntimeEnvVar(java javaRuntimeSpec) (envVar, bool) {
	if !javaRuntimeEnabled(java) {
		return envVar{}, false
	}
	parts := []string{}
	if strings.TrimSpace(java.Xms) != "" {
		parts = append(parts, "-Xms"+strings.TrimSpace(java.Xms))
	}
	if strings.TrimSpace(java.Xmx) != "" {
		parts = append(parts, "-Xmx"+strings.TrimSpace(java.Xmx))
	}
	for _, opt := range java.Opts {
		if strings.TrimSpace(opt) != "" {
			parts = append(parts, strings.TrimSpace(opt))
		}
	}
	return envVar{Name: javaRuntimeEnvName(java), Value: strings.Join(parts, " ")}, true
}

func javaRuntimeEnabled(java javaRuntimeSpec) bool {
	if strings.TrimSpace(java.Xms) != "" || strings.TrimSpace(java.Xmx) != "" {
		return true
	}
	for _, opt := range java.Opts {
		if strings.TrimSpace(opt) != "" {
			return true
		}
	}
	return false
}

func javaRuntimeEnvName(java javaRuntimeSpec) string {
	if strings.TrimSpace(java.Export.EnvName) != "" {
		return strings.TrimSpace(java.Export.EnvName)
	}
	return "JAVA_OPTS"
}

func renderToolInitContainers(tools []toolSpec) []map[string]any {
	out := []map[string]any{}
	for _, tool := range tools {
		if strings.TrimSpace(tool.ExposeBin) == "" {
			continue
		}
		image := strings.TrimSpace(tool.Image)
		if image == "" {
			image = strings.TrimSpace(tool.Name)
		}
		if image == "" {
			continue
		}
		relTarget := toolTargetRelative(tool)
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			name = "tool-" + filepath.Base(relTarget)
		}
		script := strings.Join([]string{
			"set -eu",
			"mkdir -p /work/tools/" + shellQuote(filepath.Dir(relTarget)),
			"cp " + shellQuote(tool.ExposeBin) + " /work/tools/" + shellQuote(relTarget),
			"chmod +x /work/tools/" + shellQuote(relTarget),
		}, "; ")
		out = append(out, map[string]any{
			"name":            name,
			"image":           image,
			"imagePullPolicy": mustImagePullPolicy(tool.ImagePullPolicy),
			"command":         []string{"/bin/sh", "-ec", script},
			"resources":       renderResources(toolResources(tool.Resources)),
			"volumeMounts": []any{
				map[string]any{
					"name":      "app-tools",
					"mountPath": "/work/tools",
				},
			},
		})
	}
	return out
}

func toolResources(overrides map[string]map[string]any) map[string]map[string]any {
	// A ResourceQuota applies to initContainers too. Keep tools small by default,
	// while allowing a tool image with higher requirements to override either value.
	resources := map[string]map[string]any{
		"cpu": {
			"requests": "10m",
			"limits":   "100m",
		},
		"memory": {
			"requests": "16Mi",
			"limits":   "128Mi",
		},
	}
	for resource, values := range overrides {
		if resources[resource] == nil {
			resources[resource] = map[string]any{}
		}
		for key, value := range values {
			resources[resource][key] = value
		}
	}
	return resources
}

func toolTargetRelative(tool toolSpec) string {
	target, err := normalizedToolMountPath(tool)
	if err != nil {
		panic(err)
	}
	return strings.TrimPrefix(target, "/")
}

func renderToolVolumeMounts(tools []toolSpec) []any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		mountPath, err := normalizedToolMountPath(tool)
		if err != nil {
			panic(err)
		}
		out = append(out, map[string]any{
			"name":      "app-tools",
			"mountPath": mountPath,
			"subPath":   toolTargetRelative(tool),
			"readOnly":  true,
		})
	}
	return out
}

func normalizedToolMountPath(tool toolSpec) (string, error) {
	target := strings.TrimSpace(tool.MountPath)
	if target == "" {
		return "", errors.New("mount_path is required")
	}
	if !filepath.IsAbs(target) || target == "/" {
		return "", fmt.Errorf("invalid mount_path %q", target)
	}
	if filepath.Clean(target) != target {
		return "", fmt.Errorf("invalid mount_path %q", target)
	}
	return target, nil
}

func imagePullPolicy(value string) (string, error) {
	policy := strings.TrimSpace(value)
	if policy == "" {
		return "Always", nil
	}
	switch policy {
	case "Always", "IfNotPresent", "Never":
		return policy, nil
	default:
		return "", fmt.Errorf("invalid image_pull_policy %q: expected Always, IfNotPresent or Never", policy)
	}
}

func mustImagePullPolicy(value string) string {
	policy, err := imagePullPolicy(value)
	if err != nil {
		panic(err)
	}
	return policy
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func renderResources(resources map[string]map[string]any) map[string]any {
	out := map[string]any{}
	for _, name := range []string{"cpu", "memory", "ephemeral-storage"} {
		values, ok := resources[name]
		if !ok {
			continue
		}
		if value, ok := firstResourceValue(values, "requests", "from"); ok {
			requests, _ := out["requests"].(map[string]any)
			if requests == nil {
				requests = map[string]any{}
				out["requests"] = requests
			}
			requests[name] = value
		}
		if value, ok := firstResourceValue(values, "limits", "to"); ok {
			limits, _ := out["limits"].(map[string]any)
			if limits == nil {
				limits = map[string]any{}
				out["limits"] = limits
			}
			limits[name] = value
		}
	}
	return out
}

func firstResourceValue(values map[string]any, preferred string, legacy string) (any, bool) {
	if value, ok := values[preferred]; ok {
		return value, true
	}
	value, ok := values[legacy]
	return value, ok
}

func applyEnvPlaceholders(content string, vars map[string]string) string {
	for key, value := range vars {
		pattern := regexp.MustCompile(`\{\{\s*env\s*:\s*` + regexp.QuoteMeta(key) + `\s*\}\}`)
		content = pattern.ReplaceAllStringFunc(content, func(string) string { return value })
	}
	return content
}

func applyEnvPlainPlaceholders(content string, vars map[string]string) string {
	for key, value := range vars {
		pattern := regexp.MustCompile(`\{\{\s*` + regexp.QuoteMeta(key) + `\s*\}\}`)
		content = pattern.ReplaceAllStringFunc(content, func(string) string { return value })
	}
	return content
}

func resolveLegacyEnvironmentValue(value string, vars map[string]string) (string, error) {
	missing := map[string]bool{}
	resolved := legacyEnvironmentPlaceholder.ReplaceAllStringFunc(value, func(placeholder string) string {
		matches := legacyEnvironmentPlaceholder.FindStringSubmatch(placeholder)
		resolvedValue, found := vars[matches[1]]
		if !found {
			missing[matches[1]] = true
			return placeholder
		}
		return resolvedValue
	})
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, name)
		}
		sort.Strings(names)
		return "", fmt.Errorf("unresolved variable(s): %s", strings.Join(names, ", "))
	}
	return resolved, nil
}

func applyAppVarPlaceholders(content string, vars map[string]string) string {
	for key, value := range vars {
		pattern := regexp.MustCompile(`\{\{\s*var\s*:\s*` + regexp.QuoteMeta(key) + `\s*\}\}`)
		content = pattern.ReplaceAllStringFunc(content, func(string) string { return value })
	}
	return content
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
