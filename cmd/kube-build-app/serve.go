package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"kube-env/internal/appinfo"
	"kube-env/internal/buildweb"
)

func newServeCommand(info appinfo.Info, opts *cliOptions) *cobra.Command {
	var listen, releasesRoot, workDir, deploymentsRoot, releaseCAFile, sourceConfig string
	var sourceOverrides buildweb.ArtifactSource
	var remoteOverrides buildweb.GitRemote
	var packageOverrides buildweb.PackageOutput
	command := &cobra.Command{Use: "serve", Short: "Serve the standalone manifest build interface", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if opts.target != "" || opts.evidenceDir != "" || len(opts.releaseManifests) > 0 || opts.releaseID != "" {
				return fmt.Errorf("serve selects release manifests in the browser and keeps outputs in --work-dir; do not pass --output-dir, --evidence-dir, --release-manifest or --release-id")
			}
			source, err := configuredArtifactSource(command, sourceConfig, sourceOverrides, remoteOverrides)
			if err != nil {
				return err
			}
			packageOutput, err := configuredPackageOutput(command, sourceConfig, packageOverrides)
			if err != nil {
				return err
			}
			ui, err := buildweb.New(buildweb.Options{Root: opts.root, ReleasesRoot: releasesRoot, WorkDir: workDir, DeploymentsRoot: deploymentsRoot, ReleaseCAFile: releaseCAFile, ArtifactSource: source, PackageOutput: packageOutput, Build: toBuildOptions(opts)}, info)
			if err != nil {
				return err
			}
			server := &http.Server{Addr: listen, Handler: ui.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
			ctx, stop := signal.NotifyContext(command.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			ui.StartSourceRefresh(ctx, func(err error) {
				if err != nil {
					fmt.Fprintln(command.ErrOrStderr(), err)
				}
			})
			go func() {
				<-ctx.Done()
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdown)
			}()
			fmt.Fprintf(command.OutOrStdout(), "%s UI listening on http://%s\n", info.Name, listen)
			err = server.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}}
	command.Flags().StringVar(&listen, "listen", "127.0.0.1:8787", "HTTP listen address")
	command.Flags().StringVar(&releasesRoot, "releases-root", "", "enable local release selection from a directory containing releases/ and distributions/")
	command.Flags().StringVar(&workDir, "work-dir", ".tmp/build-web", "private build history and generated output directory")
	command.Flags().StringVar(&deploymentsRoot, "deployments-root", "", "optional dedicated Git checkout for reviewed local commit/tag publication")
	command.Flags().StringVar(&releaseCAFile, "release-ca-file", "", "additional trusted CA PEM for one-off HTTPS release downloads")
	command.Flags().StringVar(&sourceConfig, "release-source-config", "", "server YAML with artifact_source and/or package_output")
	command.Flags().StringVar(&sourceOverrides.Storage, "release-storage", "", "configured source storage: git or artifactory (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.Repository, "release-repository", "", "existing source Git checkout root (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.Tag, "release-tag", "", "source tag template, for example release/{release_id} (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.BaseURL, "release-base-url", "", "configured Artifactory HTTPS base URL (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.Path, "release-path", "", "relative manifest path with {release_id}, {environment}, {revision} or {distribution} (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.SHA256, "release-sha256", "", "fixed expected manifest SHA256; empty requires checksum in the browser (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.TokenFile, "release-token-file", "", "bearer token file for configured source only (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.CAFile, "release-source-ca-file", "", "additional CA PEM for configured source only (overrides YAML)")
	command.Flags().StringVar(&remoteOverrides.URL, "release-git-url", "", "remote Git source URL, instead of attached repository (overrides YAML)")
	command.Flags().StringVar(&remoteOverrides.Username, "release-git-username", "", "HTTPS Git username (overrides YAML)")
	command.Flags().StringVar(&remoteOverrides.PasswordFile, "release-git-password-file", "", "HTTPS Git password/token file (overrides YAML)")
	command.Flags().StringVar(&remoteOverrides.CAFile, "release-git-ca-file", "", "additional trusted CA for remote Git (overrides YAML)")
	command.Flags().StringVar(&remoteOverrides.RefreshInterval, "release-git-refresh-interval", "", "optional managed Git refresh interval, e.g. 5m (overrides YAML)")
	command.Flags().StringVar(&sourceConfig, "config", "", "server YAML with artifact_source and/or package_output")
	command.MarkFlagsMutuallyExclusive("config", "release-source-config")
	command.Flags().StringVar(&packageOverrides.Directory, "package-output-dir", "", "optional directory for confirmed package export (overrides YAML)")
	command.Flags().StringVar(&packageOverrides.Path, "package-path", "", "relative ZIP path with {release_id}, {environment}, {build_id} (overrides YAML)")
	return command
}

func configuredArtifactSource(command *cobra.Command, filename string, overrides buildweb.ArtifactSource, remoteOverrides buildweb.GitRemote) (*buildweb.ArtifactSource, error) {
	var source *buildweb.ArtifactSource
	if filename != "" {
		config, err := buildweb.LoadServeConfig(filename)
		if err != nil {
			return nil, err
		}
		source = config.Source
	}
	configured := source != nil
	if source == nil {
		source = &buildweb.ArtifactSource{Storage: "artifactory"}
	}
	for _, field := range []struct {
		flag   string
		value  string
		target *string
	}{
		{"release-storage", overrides.Storage, &source.Storage},
		{"release-repository", overrides.Repository, &source.Repository},
		{"release-tag", overrides.Tag, &source.Tag},
		{"release-base-url", overrides.BaseURL, &source.BaseURL},
		{"release-path", overrides.Path, &source.Path},
		{"release-sha256", overrides.SHA256, &source.SHA256},
		{"release-token-file", overrides.TokenFile, &source.TokenFile},
		{"release-source-ca-file", overrides.CAFile, &source.CAFile},
	} {
		if command.Flags().Changed(field.flag) {
			*field.target = field.value
			configured = true
		}
	}
	remote := source.Remote
	if remote == nil {
		remote = &buildweb.GitRemote{}
	}
	for _, field := range []struct {
		flag   string
		value  string
		target *string
	}{
		{"release-git-url", remoteOverrides.URL, &remote.URL},
		{"release-git-username", remoteOverrides.Username, &remote.Username},
		{"release-git-password-file", remoteOverrides.PasswordFile, &remote.PasswordFile},
		{"release-git-ca-file", remoteOverrides.CAFile, &remote.CAFile},
		{"release-git-refresh-interval", remoteOverrides.RefreshInterval, &remote.RefreshInterval},
	} {
		if command.Flags().Changed(field.flag) {
			*field.target = field.value
			source.Remote = remote
			configured = true
		}
	}
	if command.Flags().Changed("release-git-url") && remote.URL == "" {
		source.Remote = nil
	}
	if !configured {
		return nil, nil
	}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	return source, nil
}

func configuredPackageOutput(command *cobra.Command, filename string, overrides buildweb.PackageOutput) (*buildweb.PackageOutput, error) {
	var target *buildweb.PackageOutput
	if filename != "" {
		config, err := buildweb.LoadServeConfig(filename)
		if err != nil {
			return nil, err
		}
		target = config.PackageOutput
	}
	configured := target != nil
	if target == nil {
		target = &buildweb.PackageOutput{Storage: "directory"}
	}
	if command.Flags().Changed("package-output-dir") {
		*target = buildweb.PackageOutput{Storage: "directory", Directory: overrides.Directory, Path: target.Path}
		configured = true
	}
	if command.Flags().Changed("package-path") {
		target.Path = overrides.Path
		configured = true
	}
	if !configured || (command.Flags().Changed("package-output-dir") && target.Directory == "") {
		return nil, nil
	}
	if err := target.Validate(); err != nil {
		return nil, err
	}
	return target, nil
}
