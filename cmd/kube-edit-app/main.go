package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"kube-env/internal/appinfo"
	"kube-env/internal/buildapp"
	"kube-env/internal/repository"
	"kube-env/internal/webapp"
)

type cliOptions struct {
	listen            string
	root              string
	basePath          string
	readOnly          bool
	allowWrite        bool
	encjsonPath       string
	encjsonLegacyPath string
	encjsonKeydir     string
	clusterStatus     bool
	kubeconfig        string
	kubeContext       string
	trustedProxy      bool
	authHeaderUser    string
	authHeaderEmail   string
	authHeaderGroups  string
	authGroupPrefix   string
	buildConfig       string
	build             buildapp.Options
	showVersion       bool
}

func main() {
	info := appinfo.For(appinfo.EditAppName)
	opts := &cliOptions{}
	cmd := newRootCommand(info, opts)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", info.Name, err)
		os.Exit(1)
	}
}

func newRootCommand(info appinfo.Info, opts *cliOptions) *cobra.Command {
	root := &cobra.Command{
		Use:           "kube-edit-app",
		Short:         "Web editor for kube environment repositories",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(os.Args) == 1 {
				return cmd.Help()
			}
			if opts.showVersion {
				return printJSON(cmd, info)
			}
			return cmd.Help()
		},
	}

	root.PersistentFlags().BoolVar(&opts.showVersion, "version", false, "print version information as JSON")
	root.AddCommand(newServeCommand(info, opts))
	return root
}

func newServeCommand(info appinfo.Info, opts *cliOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start web editor server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd, info, opts)
		},
	}
	cmd.Flags().StringVar(&opts.listen, "listen", "127.0.0.1:8183", "HTTP listen address")
	cmd.Flags().StringVar(&opts.root, "root", os.Getenv("ENVIRONMENTS_ROOT"), "environment repository root")
	cmd.Flags().StringVar(&opts.basePath, "base-path", "/", "reverse proxy base path")
	cmd.Flags().BoolVar(&opts.readOnly, "read-only", false, "disable mutating API endpoints")
	cmd.Flags().BoolVar(&opts.allowWrite, "allow-write", false, "explicitly allow mutating API endpoints")
	cmd.Flags().StringVar(&opts.encjsonPath, "encjson-path", os.Getenv("ENCJSON_PATH"), "modern EncJson binary path")
	cmd.Flags().StringVar(&opts.encjsonLegacyPath, "encjson-legacy-path", os.Getenv("ENCJSON_LEGACY_PATH"), "legacy EncJson binary path")
	cmd.Flags().StringVar(&opts.encjsonKeydir, "encjson-keydir", os.Getenv("ENCJSON_KEYDIR"), "optional EncJson key directory")
	cmd.Flags().BoolVar(&opts.clusterStatus, "cluster-status", false, "enable read-only Kubernetes namespace status panel")
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "optional kubeconfig path for read-only cluster status")
	cmd.Flags().StringVar(&opts.kubeContext, "context", "", "optional kubeconfig context for read-only cluster status")
	cmd.Flags().BoolVar(&opts.trustedProxy, "trusted-proxy-auth", envBool("KUBE_EDIT_TRUSTED_PROXY_AUTH"), "enable trusted proxy X-Auth-* authentication")
	cmd.Flags().StringVar(&opts.authHeaderUser, "auth-header-user", envDefault("KUBE_EDIT_AUTH_HEADER_USER", "X-Auth-User"), "trusted proxy username header")
	cmd.Flags().StringVar(&opts.authHeaderEmail, "auth-header-email", envDefault("KUBE_EDIT_AUTH_HEADER_EMAIL", "X-Auth-Email"), "trusted proxy email header")
	cmd.Flags().StringVar(&opts.authHeaderGroups, "auth-header-groups", envDefault("KUBE_EDIT_AUTH_HEADER_GROUPS", "X-Auth-Groups"), "trusted proxy groups header")
	cmd.Flags().StringVar(&opts.authGroupPrefix, "auth-group-prefix", envDefault("KUBE_EDIT_AUTH_GROUP_PREFIX", "kube-edit-app"), "trusted proxy group prefix")
	cmd.Flags().StringVar(&opts.buildConfig, "build-config", "", "per-environment build context YAML file")
	cmd.Flags().StringVar(&opts.build.Namespace, "namespace", "", "override target namespace for build and cluster inspection")
	cmd.Flags().StringVarP(&opts.build.ResourcePolicyRoot, "resource-policy-root", "P", "", "external resource policy root directory")
	cmd.Flags().StringVarP(&opts.build.Profile, "profile", "p", "", "replica profile name")
	cmd.Flags().StringVar(&opts.build.ProfilesFile, "profiles-file", "", "replica profiles file path")
	cmd.Flags().BoolVarP(&opts.build.DecryptSecured, "decrypt-secured", "d", false, "enable secured JSON variables for build operations")
	cmd.Flags().StringVarP(&opts.build.EnvFile, "env-file", "E", "", "explicit .env file for build operations")
	cmd.Flags().StringVar(&opts.build.EnvURL, "env-url", "", "HTTP(S) URL returning .env content for build operations")
	cmd.Flags().StringArrayVar(&opts.build.EnvURLHeaders, "env-url-header", nil, "HTTP header for --env-url; repeatable")
	cmd.Flags().BoolVar(&opts.build.EnvURLInsecure, "env-url-insecure", false, "skip TLS verification for --env-url")
	cmd.Flags().StringArrayVar(&opts.build.VarsSources, "vars-source", nil, "variable sources: env, json, dot-env; repeatable")
	cmd.Flags().BoolVar(&opts.build.LegacyApplyEnv, "legacy-apply-env", false, "resolve legacy placeholders in generated preview files")
	cmd.Flags().BoolVar(&opts.build.HelmEscapeAssets, "helm-escape-assets", false, "escape remaining placeholders in text assets")
	cmd.Flags().StringArrayVarP(&opts.build.ReleaseManifests, "release-manifest", "r", nil, "release manifest YAML path (v1 or v2); repeatable, later files override earlier files")
	cmd.Flags().StringVar(&opts.build.ReleaseID, "release-id", "", "deployment release ID; required with multiple release manifests")
	cmd.Flags().StringArrayVar(&opts.build.ImageOverrides, "image", nil, "image override app/container=image; repeatable")
	cmd.Flags().StringVar(&opts.build.ImagePolicy, "image-policy", "fallback", "image policy: fallback or strict")
	cmd.Flags().StringVar(&opts.build.ImageReference, "image-reference", "auto", "release image reference: auto, digest, or tag")
	cmd.Flags().StringVar(&opts.build.ForceImageTag, "force-image-tag", "", "force one release image tag")
	cmd.Flags().StringVar(&opts.build.ForceImagePrefix, "force-image-prefix", "", "replace release image repository prefixes")
	cmd.Flags().StringVar(&opts.build.SyncProfile, "sync-metadata-profile", "", "sync metadata profile")
	cmd.Flags().StringVar(&opts.build.SyncPrefix, "sync-metadata-prefix", "kube-build-app.io", "sync metadata prefix")
	cmd.Flags().StringVar(&opts.build.SyncSet, "sync-set", "", "sync metadata set name")
	cmd.Flags().StringArrayVarP(&opts.build.Down, "down", "w", nil, "scale named app to zero; repeatable")
	cmd.Flags().IntVar(&opts.build.YAMLIndent, "yaml-indent", 2, "preview YAML indentation: 2 or 4")
	return cmd
}

func runServe(cmd *cobra.Command, info appinfo.Info, opts *cliOptions) error {
	if opts.readOnly && opts.allowWrite {
		return errors.New("--read-only and --allow-write cannot be used together")
	}
	if opts.root == "" {
		return errors.New("--root is required or ENVIRONMENTS_ROOT must be set")
	}
	if opts.buildConfig == "" && opts.build.ResourcePolicyRoot != "" && samePath(opts.root, opts.build.ResourcePolicyRoot) {
		return errors.New("--resource-policy-root resolves to the same directory as --root; omit it unless resource policies are stored in a separate directory")
	}
	opts.build.Root = opts.root
	opts.build.EncjsonPath = opts.encjsonPath
	opts.build.EncjsonLegacyPath = opts.encjsonLegacyPath
	opts.build.EncjsonKeydir = opts.encjsonKeydir
	repo, err := repository.New(opts.root)
	if err != nil {
		return fmt.Errorf("invalid repository root: %w", err)
	}
	buildOptionsByEnv := map[string]buildapp.Options(nil)
	if opts.buildConfig == "" {
		if _, err := buildapp.DescribeBuildContext(opts.build); err != nil {
			return fmt.Errorf("invalid build context: %w", err)
		}
	} else {
		if changed := changedBuildContextFlags(cmd); len(changed) > 0 {
			return fmt.Errorf("--build-config cannot be combined with per-build CLI flags: --%s", strings.Join(changed, ", --"))
		}
		buildOptionsByEnv, err = loadEditorBuildConfig(opts.buildConfig, opts.build)
		if err != nil {
			return fmt.Errorf("invalid --build-config: %w", err)
		}
		environments, err := repo.Environments()
		if err != nil {
			return fmt.Errorf("discover build-config environments: %w", err)
		}
		discovered := make([]string, 0, len(environments))
		for _, environment := range environments {
			discovered = append(discovered, environment.Name)
		}
		if err := validateBuildConfigEnvironments(buildOptionsByEnv, discovered); err != nil {
			return fmt.Errorf("invalid --build-config: %w", err)
		}
		for env, configured := range buildOptionsByEnv {
			configured.Root = opts.root
			configured.EncjsonPath = opts.encjsonPath
			configured.EncjsonLegacyPath = opts.encjsonLegacyPath
			configured.EncjsonKeydir = opts.encjsonKeydir
			if configured.ResourcePolicyRoot != "" && samePath(opts.root, configured.ResourcePolicyRoot) {
				return fmt.Errorf("invalid --build-config environment %q: resource_policy_root resolves to the same directory as --root", env)
			}
			if _, err := buildapp.DescribeBuildContext(configured); err != nil {
				return fmt.Errorf("invalid --build-config environment %q: %w", env, err)
			}
			buildOptionsByEnv[env] = configured
		}
	}

	serverOpts := webapp.Options{
		BasePath:          opts.basePath,
		ReadOnly:          opts.readOnly || !opts.allowWrite,
		EncjsonPath:       opts.encjsonPath,
		EncjsonLegacyPath: opts.encjsonLegacyPath,
		EncjsonKeydir:     opts.encjsonKeydir,
		ClusterStatus:     opts.clusterStatus,
		Kubeconfig:        opts.kubeconfig,
		KubeContext:       opts.kubeContext,
		BuildOptions:      opts.build,
		BuildOptionsByEnv: buildOptionsByEnv,
		TrustedProxyAuth: webapp.TrustedProxyAuthOptions{
			Enabled:      opts.trustedProxy,
			HeaderUser:   opts.authHeaderUser,
			HeaderEmail:  opts.authHeaderEmail,
			HeaderGroups: opts.authHeaderGroups,
			GroupPrefix:  opts.authGroupPrefix,
		},
	}
	server := webapp.NewServer(info, repo, serverOpts)
	if err := server.ListenAndServe(opts.listen); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("web editor failed", "error", err)
		return err
	}
	return nil
}

func changedBuildContextFlags(cmd *cobra.Command) []string {
	names := []string{
		"namespace", "resource-policy-root", "profile", "profiles-file",
		"decrypt-secured", "env-file", "env-url", "env-url-header", "env-url-insecure", "vars-source",
		"legacy-apply-env", "helm-escape-assets", "release-manifest", "release-id", "image", "image-policy", "image-reference",
		"force-image-tag", "force-image-prefix", "sync-metadata-profile", "sync-metadata-prefix", "sync-set", "down", "yaml-indent",
	}
	changed := []string{}
	for _, name := range names {
		if cmd.Flags().Changed(name) {
			changed = append(changed, name)
		}
	}
	return changed
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	leftInfo, leftStatErr := os.Stat(leftAbs)
	rightInfo, rightStatErr := os.Stat(rightAbs)
	if leftStatErr == nil && rightStatErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func envDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func printJSON(cmd *cobra.Command, payload any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		return errors.Join(errors.New("failed to write JSON"), err)
	}
	return nil
}
