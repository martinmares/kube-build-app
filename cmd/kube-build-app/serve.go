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
	command := &cobra.Command{Use: "serve", Short: "Serve the standalone manifest build interface", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if opts.target != "" || opts.evidenceDir != "" || len(opts.releaseManifests) > 0 || opts.releaseID != "" {
				return fmt.Errorf("serve selects release manifests in the browser and keeps outputs in --work-dir; do not pass --output-dir, --evidence-dir, --release-manifest or --release-id")
			}
			source, err := configuredArtifactSource(command, sourceConfig, sourceOverrides)
			if err != nil {
				return err
			}
			ui, err := buildweb.New(buildweb.Options{Root: opts.root, ReleasesRoot: releasesRoot, WorkDir: workDir, DeploymentsRoot: deploymentsRoot, ReleaseCAFile: releaseCAFile, ArtifactSource: source, Build: toBuildOptions(opts)}, info)
			if err != nil {
				return err
			}
			server := &http.Server{Addr: listen, Handler: ui.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
			ctx, stop := signal.NotifyContext(command.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
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
	command.Flags().StringVar(&releasesRoot, "releases-root", "", "local image-release directory containing releases/ and distributions/; Git is optional")
	command.Flags().StringVar(&workDir, "work-dir", ".tmp/build-web", "private build history and generated output directory")
	command.Flags().StringVar(&deploymentsRoot, "deployments-root", "", "optional dedicated Git checkout for reviewed local commit/tag publication")
	command.Flags().StringVar(&releaseCAFile, "release-ca-file", "", "additional trusted CA PEM for one-off HTTPS release downloads")
	command.Flags().StringVar(&sourceConfig, "release-source-config", "", "YAML file containing artifact_source for configured HTTPS reads")
	command.Flags().StringVar(&sourceOverrides.BaseURL, "release-base-url", "", "configured Artifactory HTTPS base URL (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.Path, "release-path", "", "relative manifest path with {release_id}, {environment}, {revision} or {distribution} (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.SHA256, "release-sha256", "", "fixed expected manifest SHA256; empty requires checksum in the browser (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.TokenFile, "release-token-file", "", "bearer token file for configured source only (overrides YAML)")
	command.Flags().StringVar(&sourceOverrides.CAFile, "release-source-ca-file", "", "additional CA PEM for configured source only (overrides YAML)")
	_ = command.MarkFlagRequired("releases-root")
	return command
}

func configuredArtifactSource(command *cobra.Command, filename string, overrides buildweb.ArtifactSource) (*buildweb.ArtifactSource, error) {
	var source *buildweb.ArtifactSource
	if filename != "" {
		var err error
		source, err = buildweb.LoadArtifactSource(filename)
		if err != nil {
			return nil, err
		}
	}
	if source == nil {
		source = &buildweb.ArtifactSource{Storage: "artifactory"}
	}
	configured := filename != ""
	for _, field := range []struct {
		flag   string
		value  string
		target *string
	}{
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
	if !configured {
		return nil, nil
	}
	if err := source.Validate(); err != nil {
		return nil, err
	}
	return source, nil
}
