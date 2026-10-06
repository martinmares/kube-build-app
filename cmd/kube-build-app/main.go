package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"kube-env/internal/appinfo"
	"kube-env/internal/buildapp"
)

type cliOptions struct {
	envName                   string
	namespace                 string
	root                      string
	resourcePolicyRoot        string
	target                    string
	profile                   string
	profilesFile              string
	inventory                 bool
	decryptSecured            bool
	envFile                   string
	envURL                    string
	envURLHeaders             []string
	envURLInsecure            bool
	varsSources               []string
	legacyApplyEnv            bool
	helmEscapeAssets          bool
	releaseManifests          []string
	releaseID                 string
	customerReleaseID         string
	releaseContextName        string
	releaseIDEnvName          string
	customerReleaseIDEnvName  string
	noReleaseContextConfigMap bool
	noReleaseContextEnv       bool
	imageOverrides            []string
	imagePolicy               string
	imageReference            string
	forceImageTag             string
	forceImagePrefix          string
	syncProfile               string
	syncPrefix                string
	syncSet                   string
	down                      []string
	yamlIndent                int
	list                      bool
	summary                   bool
	summaryFormat             string
	verbose                   bool
	logFormat                 string
	color                     string
	debug                     bool
	showVersion               bool
	skeletonEnv               string
	skeletonNS                string
	skeletonRegistry          string
	skeletonRelease           string
	force                     bool
}

func main() {
	info := appinfo.For(appinfo.BuildAppName)
	opts := &cliOptions{}
	cmd := newRootCommand(info, opts)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", info.Name, err)
		os.Exit(1)
	}
}

func newRootCommand(info appinfo.Info, opts *cliOptions) *cobra.Command {
	root := &cobra.Command{
		Use:           "kube-build-app",
		Short:         "Build Kubernetes manifests from kube environment repositories",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.showVersion {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s version %s (commit %s, built %s)\n", info.Name, info.Version, info.Commit, info.Date)
				return err
			}
			if len(os.Args) == 1 {
				return cmd.Help()
			}
			return runLegacyMode(cmd, opts)
		},
	}

	root.PersistentFlags().BoolVar(&opts.showVersion, "version", false, "print version, commit and build timestamp")
	root.PersistentFlags().StringVarP(&opts.envName, "environment", "e", "", "environment name")
	root.PersistentFlags().StringVar(&opts.namespace, "namespace", "", "override target namespace for generated resources")
	root.PersistentFlags().StringVarP(&opts.root, "root", "R", "environments", "environments root directory")
	root.PersistentFlags().StringVarP(&opts.resourcePolicyRoot, "resource-policy-root", "P", "", "external resource policy root directory")
	root.PersistentFlags().StringVarP(&opts.target, "target", "t", "", "target output directory")
	root.PersistentFlags().StringVarP(&opts.profile, "profile", "p", "", "replica profile name")
	root.PersistentFlags().StringVar(&opts.profilesFile, "profiles-file", "", "replica profiles file path")
	root.PersistentFlags().BoolVarP(&opts.decryptSecured, "decrypt-secured", "d", false, "enable env.secured.json variables")
	root.PersistentFlags().StringVarP(&opts.envFile, "env-file", "E", "", "explicit .env file path")
	root.PersistentFlags().StringVar(&opts.envURL, "env-url", "", "HTTP(S) URL returning .env content")
	root.PersistentFlags().StringArrayVar(&opts.envURLHeaders, "env-url-header", nil, "HTTP header for --env-url as 'Name: value'; repeatable")
	root.PersistentFlags().BoolVar(&opts.envURLInsecure, "env-url-insecure", false, "skip TLS certificate verification for --env-url")
	root.PersistentFlags().StringArrayVar(&opts.varsSources, "vars-source", nil, "variable source(s): env, json, dot-env; repeatable or comma-separated")
	root.PersistentFlags().BoolVar(&opts.legacyApplyEnv, "legacy-apply-env", false, "resolve legacy {{VAR}} placeholders in generated deployment and external service manifests")
	root.PersistentFlags().BoolVar(&opts.helmEscapeAssets, "helm-escape-assets", false, "escape remaining {{VAR}} placeholders in text assets")
	root.PersistentFlags().StringArrayVarP(&opts.releaseManifests, "release-manifest", "r", nil, "release manifest YAML path (v1 or v2); repeatable, later files override earlier files")
	root.PersistentFlags().StringVar(&opts.releaseID, "release-id", "", "deployment release ID; required with multiple release manifests, inferred from a single manifest")
	root.PersistentFlags().StringVar(&opts.customerReleaseID, "customer-release-id", "", "customer release ID recorded in deployment metadata and release context")
	root.PersistentFlags().StringVar(&opts.releaseContextName, "release-context-name", "", "stable release context ConfigMap name; defaults to release-context-<sync-set>")
	root.PersistentFlags().StringVar(&opts.releaseIDEnvName, "release-id-env-name", "RELEASE_ID", "container ENV name for release ID")
	root.PersistentFlags().StringVar(&opts.customerReleaseIDEnvName, "customer-release-id-env-name", "CUSTOMER_RELEASE_ID", "container ENV name for customer release ID")
	root.PersistentFlags().BoolVar(&opts.noReleaseContextConfigMap, "no-release-context-configmap", false, "do not generate release context ConfigMap")
	root.PersistentFlags().BoolVar(&opts.noReleaseContextEnv, "no-release-context-env", false, "do not inject release context ENV references")
	root.PersistentFlags().StringArrayVar(&opts.imageOverrides, "image", nil, "inject or override image as app/container=image-reference (including public registries); applied after all release manifests; repeatable or comma-separated")
	root.PersistentFlags().StringVar(&opts.imagePolicy, "image-policy", "fallback", "image override policy: fallback or strict")
	root.PersistentFlags().StringVar(&opts.imageReference, "image-reference", "auto", "release image reference: auto, digest, or tag")
	root.PersistentFlags().StringVar(&opts.forceImageTag, "force-image-tag", "", "force one tag for every image selected from the release manifest")
	root.PersistentFlags().StringVar(&opts.forceImagePrefix, "force-image-prefix", "", "replace release image repository prefixes and preserve only image basenames")
	root.PersistentFlags().StringVar(&opts.syncProfile, "sync-metadata-profile", "", "sync metadata profile: none, kube-deploy-sync, argocd")
	root.PersistentFlags().StringVar(&opts.syncPrefix, "sync-metadata-prefix", "kube-build-app.io", "sync metadata label/annotation prefix")
	root.PersistentFlags().StringVar(&opts.syncSet, "sync-set", "", "sync metadata set name; defaults to environment")
	root.PersistentFlags().StringArrayVarP(&opts.down, "down", "w", nil, "scale app replicas down to 0; repeatable or comma-separated")
	root.PersistentFlags().IntVar(&opts.yamlIndent, "yaml-indent", 2, "manifest YAML indentation: 2 or 4 spaces")
	root.PersistentFlags().BoolVarP(&opts.debug, "debug", "b", false, "debug output compatibility flag")
	root.PersistentFlags().BoolVar(&opts.verbose, "verbose", false, "print build render events to stderr")
	root.PersistentFlags().StringVar(&opts.logFormat, "log-format", "text", "verbose build log format: text or json")
	root.PersistentFlags().StringVar(&opts.color, "color", "auto", "verbose text color: auto, always or never")
	root.Flags().BoolVarP(&opts.inventory, "inventory", "i", false, "print detailed inventory JSON and exit")
	root.Flags().BoolVarP(&opts.list, "list", "l", false, "print app list and exit")
	root.Flags().BoolVarP(&opts.summary, "summary", "s", false, "print resource summary and exit")
	root.Flags().StringVar(&opts.summaryFormat, "summary-format", "text", "summary output format: text or json")

	root.AddCommand(newBuildCommand(opts))
	root.AddCommand(newValidateCommand(opts))
	root.AddCommand(newSummaryCommand(opts))
	root.AddCommand(newInventoryCommand(opts))
	root.AddCommand(newListCommand(opts))
	root.AddCommand(newSkeletonCommand(opts))
	root.AddCommand(newScaffoldCommand(opts))
	root.AddCommand(newAppCommand(opts))
	root.AddCommand(newImportCommand())
	root.AddCommand(newExamplesCommand(opts))
	root.AddCommand(newCompletionCommand(root))
	return root
}

func newSkeletonCommand(opts *cliOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skeleton",
		Short: "Generate starter environment files",
	}
	envCmd := &cobra.Command{
		Use:   "env",
		Short: "Generate an environment skeleton",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkeletonEnv(cmd, opts)
		},
	}
	envCmd.Flags().StringVar(&opts.skeletonEnv, "env", "", "environment name to create")
	envCmd.Flags().StringVar(&opts.skeletonNS, "namespace", "", "Kubernetes namespace value")
	envCmd.Flags().StringVar(&opts.skeletonRegistry, "registry-url", "registry.example.com/project", "default REGISTRY_URL value")
	envCmd.Flags().StringVar(&opts.skeletonRelease, "release-id", "latest", "default RELEASE_ID value")
	envCmd.Flags().BoolVar(&opts.force, "force", false, "overwrite existing generated files")
	cmd.AddCommand(envCmd)
	return cmd
}

func newAppCommand(opts *cliOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Manage app model files",
	}
	scaffoldOpts := defaultScaffoldAppOptions()
	addCmd := &cobra.Command{
		Use:   "add NAME",
		Short: "Generate a starter app model (compatibility alias for scaffold app)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScaffoldApp(cmd, opts, scaffoldOpts, args[0], "app add")
		},
	}
	bindScaffoldAppFlags(addCmd, scaffoldOpts)
	cmd.AddCommand(addCmd)
	return cmd
}

func newBuildCommand(opts *cliOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "build",
		Short: "Build Kubernetes manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBuild(cmd, opts)
		},
	}
}

func newValidateCommand(opts *cliOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate environment app model files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := buildapp.Validate(toBuildOptions(opts)); err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Validation OK")
			return nil
		},
	}
}

func newSummaryCommand(opts *cliOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Print deployment resource summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSummary(cmd, opts)
		},
	}
	cmd.Flags().StringVar(&opts.summaryFormat, "summary-format", "text", "summary output format: text or json")
	return cmd
}

func newInventoryCommand(opts *cliOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "inventory",
		Short: "Print detailed inventory JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInventory(cmd, opts)
		},
	}
}

func newListCommand(opts *cliOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Print app names",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, opts)
		},
	}
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(cmd.OutOrStdout())
			default:
				return fmt.Errorf("unsupported shell %q, expected bash, zsh, fish or powershell", args[0])
			}
		},
	}
	return cmd
}

func runLegacyMode(cmd *cobra.Command, opts *cliOptions) error {
	if opts.summary {
		return runSummary(cmd, opts)
	}
	if opts.inventory {
		return runInventory(cmd, opts)
	}
	if opts.list {
		return runList(cmd, opts)
	}
	return runBuild(cmd, opts)
}

func runBuild(cmd *cobra.Command, opts *cliOptions) error {
	_ = opts.debug
	if opts.logFormat != "text" && opts.logFormat != "json" {
		return fmt.Errorf("build failed: invalid --log-format %q, expected text or json", opts.logFormat)
	}
	if opts.color != "auto" && opts.color != "always" && opts.color != "never" {
		return fmt.Errorf("build failed: invalid --color %q, expected auto, always or never", opts.color)
	}
	result, err := buildapp.Build(toBuildOptions(opts))
	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	if opts.verbose {
		if err := printBuildEvents(cmd.ErrOrStderr(), result.Events, opts.logFormat, shouldColor(opts.color)); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Generated %d deployment(s)\n", len(result.Deployments))
	fmt.Fprintf(cmd.OutOrStdout(), "Output: %s\n", effectiveTargetDir(opts))
	return nil
}

func runSummary(cmd *cobra.Command, opts *cliOptions) error {
	if opts.summaryFormat != "text" && opts.summaryFormat != "json" {
		return fmt.Errorf("summary failed: invalid --summary-format %q, expected text or json", opts.summaryFormat)
	}
	payload, err := buildapp.ResourceSummary(toBuildOptions(opts))
	if err != nil {
		return fmt.Errorf("summary failed: %w", err)
	}
	if opts.summaryFormat == "text" {
		fmt.Fprintln(cmd.OutOrStdout(), buildapp.FormatResourceSummaryText(payload))
		return nil
	}
	return printJSON(cmd, payload)
}

func runInventory(cmd *cobra.Command, opts *cliOptions) error {
	payload, err := buildapp.Inventory(toBuildOptions(opts))
	if err != nil {
		return fmt.Errorf("inventory failed: %w", err)
	}
	return printJSON(cmd, payload)
}

func runList(cmd *cobra.Command, opts *cliOptions) error {
	names, err := buildapp.ListApps(toBuildOptions(opts))
	if err != nil {
		return fmt.Errorf("list failed: %w", err)
	}
	for i, name := range names {
		if i > 0 {
			fmt.Fprint(cmd.OutOrStdout(), " ")
		}
		fmt.Fprint(cmd.OutOrStdout(), name)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

func runSkeletonEnv(cmd *cobra.Command, opts *cliOptions) error {
	envName := strings.TrimSpace(opts.skeletonEnv)
	if envName == "" {
		envName = strings.TrimSpace(opts.envName)
	}
	if envName == "" {
		return errors.New("skeleton env failed: --env or -e is required")
	}
	namespace := strings.TrimSpace(opts.skeletonNS)
	if namespace == "" {
		namespace = envName
	}
	envDir := filepath.Join(opts.root, envName)
	appsDir := filepath.Join(envDir, "apps")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return fmt.Errorf("skeleton env failed: %w", err)
	}
	envPath := filepath.Join(envDir, "env.unsecured.json")
	envPayload := map[string]any{
		"environment": map[string]any{
			"NAMESPACE":    namespace,
			"REGISTRY_URL": strings.TrimSpace(opts.skeletonRegistry),
			"RELEASE_ID":   strings.TrimSpace(opts.skeletonRelease),
		},
	}
	envContent, err := json.MarshalIndent(envPayload, "", "  ")
	if err != nil {
		return fmt.Errorf("skeleton env failed: %w", err)
	}
	envContent = append(envContent, '\n')
	if err := writeFileNoClobber(envPath, envContent, opts.force); err != nil {
		return fmt.Errorf("skeleton env failed: %w", err)
	}
	defaultsPath := filepath.Join(appsDir, "_defaults.yml")
	defaults := []byte(`# Shared defaults for apps in this environment.
vars: []
container_envs: []
`)
	if err := writeFileNoClobber(defaultsPath, defaults, opts.force); err != nil {
		return fmt.Errorf("skeleton env failed: %w", err)
	}
	globalScaffoldPath := filepath.Join(opts.root, "_scaffold.yml")
	if err := writeFileIfMissing(globalScaffoldPath, defaultGlobalScaffoldConfigContent()); err != nil {
		return fmt.Errorf("skeleton env failed: %w", err)
	}
	scaffoldPath := filepath.Join(appsDir, "_scaffold.yml")
	if err := writeFileNoClobber(scaffoldPath, defaultEnvironmentScaffoldConfigContent(), opts.force); err != nil {
		return fmt.Errorf("skeleton env failed: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Created environment skeleton: %s\n", envDir)
	return nil
}

func writeFileIfMissing(path string, content []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func writeFileNoClobber(path string, content []byte, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; use --force to overwrite", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.WriteFile(path, content, 0o644)
}

func toBuildOptions(opts *cliOptions) buildapp.Options {
	return buildapp.Options{
		Environment:                    opts.envName,
		Namespace:                      opts.namespace,
		Root:                           opts.root,
		ResourcePolicyRoot:             opts.resourcePolicyRoot,
		Target:                         opts.target,
		Profile:                        opts.profile,
		ProfilesFile:                   opts.profilesFile,
		Inventory:                      opts.inventory,
		DecryptSecured:                 opts.decryptSecured,
		EnvFile:                        opts.envFile,
		EnvURL:                         opts.envURL,
		EnvURLHeaders:                  opts.envURLHeaders,
		EnvURLInsecure:                 opts.envURLInsecure,
		VarsSources:                    opts.varsSources,
		LegacyApplyEnv:                 opts.legacyApplyEnv,
		HelmEscapeAssets:               opts.helmEscapeAssets,
		ReleaseManifests:               append([]string(nil), opts.releaseManifests...),
		ReleaseID:                      opts.releaseID,
		CustomerReleaseID:              opts.customerReleaseID,
		ReleaseContextName:             opts.releaseContextName,
		ReleaseIDEnvName:               opts.releaseIDEnvName,
		CustomerReleaseIDEnvName:       opts.customerReleaseIDEnvName,
		DisableReleaseContextConfigMap: opts.noReleaseContextConfigMap,
		DisableReleaseContextEnv:       opts.noReleaseContextEnv,
		ImageOverrides:                 opts.imageOverrides,
		ImagePolicy:                    opts.imagePolicy,
		ImageReference:                 opts.imageReference,
		ForceImageTag:                  opts.forceImageTag,
		ForceImagePrefix:               opts.forceImagePrefix,
		SyncProfile:                    opts.syncProfile,
		SyncPrefix:                     opts.syncPrefix,
		SyncSet:                        opts.syncSet,
		Down:                           opts.down,
		YAMLIndent:                     opts.yamlIndent,
	}
}

func effectiveTargetDir(opts *cliOptions) string {
	if opts.target != "" {
		return opts.target
	}
	return filepath.Join(opts.root, opts.envName, "target")
}

func printBuildEvents(out io.Writer, events []buildapp.BuildEvent, format string, color bool) error {
	if format == "json" {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(events); err != nil {
			return errors.Join(errors.New("failed to write build events"), err)
		}
		return nil
	}
	for _, event := range events {
		switch event.Type {
		case "namespace_override":
			fmt.Fprintf(out, "%s: %s %s\n", paint("Build namespace", ansiBold, color), event.Name, paint("(CLI override)", ansiDim, color))
		case "shared_assets":
			fmt.Fprintf(out, "%s: %d\n", paint("Build shared assets", ansiBold, color), event.Count)
		case "app":
			fmt.Fprintf(out, "%s %s, with %d container/s\n", paint("App", ansiBlue, color), paint(event.App, ansiBold, color), event.Count)
		case "container":
			fmt.Fprintf(out, "  %s %s has %d asset/s\n", paint("container", ansiCyan, color), paint(event.Container, ansiBold, color), event.Count)
		case "deployment":
			fmt.Fprintf(out, "  %s %s -> %s\n", paint("deployment", ansiGreen, color), event.Name, paint(event.Path, ansiDim, color))
		case "service":
			fmt.Fprintf(out, "  %s %s -> %s\n", paint("service", ansiGreen, color), event.Name, paint(event.Path, ansiDim, color))
		case "external":
			fmt.Fprintf(out, "  %s %s %s -> %s\n", paint("external", ansiMagenta, color), event.Kind, event.Name, paint(event.Path, ansiDim, color))
		case "asset":
			if event.Kind == "shared" {
				fmt.Fprintf(out, "  %s %s -> %s\n", paint("shared asset", ansiYellow, color), event.Name, paint(event.Path, ansiDim, color))
			} else {
				fmt.Fprintf(out, "    %s %s -> %s\n", paint("asset", ansiYellow, color), event.Name, paint(event.Path, ansiDim, color))
			}
		case "budget":
			fmt.Fprintf(out, "  %s %s -> %s\n", paint("budget", ansiMagenta, color), event.Name, paint(event.Path, ansiDim, color))
		case "autoscaling":
			fmt.Fprintf(out, "  %s %s -> %s\n", paint("autoscaling", ansiMagenta, color), event.Name, paint(event.Path, ansiDim, color))
		}
	}
	return nil
}

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiBlue    = "\x1b[34m"
	ansiCyan    = "\x1b[36m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiMagenta = "\x1b[35m"
)

func paint(value string, code string, enabled bool) string {
	if !enabled {
		return value
	}
	return code + value + ansiReset
}

func shouldColor(mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		return stderrIsTerminal()
	}
}

func stderrIsTerminal() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func printJSON(cmd *cobra.Command, payload any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		return errors.Join(errors.New("failed to write JSON"), err)
	}
	return nil
}
