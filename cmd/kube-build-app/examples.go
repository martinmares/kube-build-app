package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type exampleFile struct {
	name    string
	format  string
	content string
}

type cliExample struct {
	title       string
	description string
	shell       string
	files       []exampleFile
}

type exampleSection struct {
	name        string
	description string
	examples    []cliExample
}

func newExamplesCommand(opts *cliOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "examples",
		Aliases: []string{"example"},
		Short:   "Show practical commands and environment YAML examples",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			color, err := examplesUseColor(cmd.OutOrStdout(), opts.color)
			if err != nil {
				return err
			}
			return writeExamples(cmd.OutOrStdout(), color)
		},
	}
}

var examplesCatalog = []exampleSection{
	{
		name:        "Start here",
		description: "Create a small environment, inspect it, then render Kubernetes manifests.",
		examples: []cliExample{
			{
				title:       "Minimal repository",
				description: "Place these files below your working directory. Build-time env and app-local var references are resolved by kube-build-app.",
				files: []exampleFile{
					{name: "environments/dev/env.unsecured.json", format: "JSON", content: `{
  "environment": {
    "NAMESPACE": "demo-dev",
    "TSM_REGISTRY_URL": "registry.example.com/demo",
    "TSM_RELEASE_ID": "1.0.0"
  }
}
`},
					{name: "environments/dev/apps/api.yml", format: "YAML", content: `vars:
  - name: APP_NAME
    value: api
name: "{{var:APP_NAME}}"
replicas: 2
containers:
  - name: api
    image: "{{env:TSM_REGISTRY_URL}}/api:{{env:TSM_RELEASE_ID}}"
    resources:
      cpu: {from: "100m", to: "500m"}
      memory: {from: "128Mi", to: "512Mi"}
`},
				},
				shell: `kube-build-app validate -e dev -R environments
kube-build-app build -e dev -R environments -t deploy/dev`,
			},
			{
				title:       "Build the included smoke fixture",
				description: "From the source checkout, this exercises defaults, assets, services, and routes without external services.",
				shell: `kube-build-app validate -e dev -R fixtures/readme-smoke/environments
kube-build-app build -e dev -R fixtures/readme-smoke/environments -t /tmp/kube-build-app-smoke`,
			},
		},
	},
	{
		name:        "Inspect and validate",
		description: "Read the model before building or changing a deployment.",
		examples: []cliExample{
			{title: "Check model files", description: "Validation does not write manifests.", shell: "kube-build-app validate -e dev -R environments"},
			{title: "List apps", shell: "kube-build-app list -e dev -R environments"},
			{title: "Summarize CPU, memory and replicas", shell: "kube-build-app summary -e dev -R environments"},
			{title: "Export structured inspection data", description: "Summary and inventory are suitable for scripts and other tools.", shell: `kube-build-app summary -e dev -R environments --summary-format json
kube-build-app inventory -e dev -R environments > inventory.json`},
		},
	},
	{
		name:        "Defaults and references",
		description: "Keep common container settings in apps/_defaults.yml; app files select and override them.",
		examples: []cliExample{
			{
				title:       "Reuse a container profile and sidecar",
				description: "The profile supplies startup, resources and probes. The app supplies its own name and image.",
				files: []exampleFile{
					{name: "environments/dev/apps/_defaults.yml", format: "YAML", content: `container_profiles:
  - name: api-service
    defaults:
      startup:
        command: ["/bin/sh"]
        arguments: ["/app/start.sh"]
      resources:
        cpu: {from: "100m", to: "500m"}
        memory: {from: "128Mi", to: "512Mi"}
      probes:
        preset: http
        port: 8080
        path: /health
sidecar_definitions:
  - name: metrics
    image: registry.example.com/demo/metrics:1.0
    resources:
      cpu: {from: "10m", to: "100m"}
      memory: {from: "32Mi", to: "128Mi"}
`},
					{name: "environments/dev/apps/api.yml", format: "YAML", content: `name: api
sidecar_ref_names: [metrics]
containers:
  - name: api
    profile_ref_names: [api-service]
    image: registry.example.com/demo/api:1.0
`},
				},
				shell: "kube-build-app validate -e dev -R environments",
			},
			{
				title:       "Share an asset between apps",
				description: "The file path is relative to the environment. Shared assets are included unless an app disables them.",
				files: []exampleFile{
					{name: "environments/dev/shared.assets.yml", format: "YAML", content: `assets:
  - name: internal-ca
    file: assets/ssl/internal-ca.pem
    to: /var/run/certs/internal-ca.pem
`},
					{name: "environments/dev/assets/ssl/internal-ca.pem", format: "PEM", content: "<your CA certificate>"},
				},
			},
		},
	},
	{
		name:        "App model",
		description: "Declare ports, services, probes and assets in the app YAML.",
		examples: []cliExample{
			{
				title: "Expose a service and add health checks",
				files: []exampleFile{{name: "environments/dev/apps/api.yml", format: "YAML", content: `name: api
containers:
  - name: api
    image: registry.example.com/demo/api:1.0
    ports:
      - name: http
        port: 8080
        expose_as:
          - service_name: api
            port: 80
    probes:
      preset: spring-actuator
      port: 8080
`}},
				shell: "kube-build-app build -e dev -R environments -t deploy/dev",
			},
			{
				title:       "Mount a generated ConfigMap asset",
				description: "With transform: true, build-time placeholders inside the text file are resolved.",
				files: []exampleFile{{name: "environments/dev/apps/api.yml (fragment)", format: "YAML", content: `containers:
  - name: api
    assets:
      - file: assets/config/application.yml.tpl
        to: /app/config/application.yml
        transform: true`}},
			},
			{
				title:       "Add an external HTTP endpoint",
				description: "Add this under an expose_as service entry. The model renders an Ingress or OpenShift Route as appropriate.",
				files: []exampleFile{{name: "environments/dev/apps/api.yml (expose_as fragment)", format: "YAML", content: `expose_as:
  - service_name: api
    port: 80
    external:
      - name: api-public
        http:
          - hostname: "{{env:PUBLIC_API_HOST}}"
            path: /`}},
			},
		},
	},
	{
		name:        "Build inputs",
		description: "Choose where variables and resource limits come from.",
		examples: []cliExample{
			{
				title:       "Use a pre-rendered .env file",
				description: "An explicit file is the sole variable source; do not combine it with --vars-source or --decrypt-secured.",
				shell:       "kube-build-app build -e dev -R environments -E /path/to/release.env -t deploy/dev",
			},
			{
				title:       "Download .env variables",
				description: "Supply any required HTTP authorization header from your environment.",
				shell: `kube-build-app build -e dev -R environments \
  --env-url "https://config.example.com/environments/dev/render" \
  --env-url-header "Authorization: Bearer $CONFIG_TOKEN" \
  -t deploy/dev`,
			},
			{
				title:       "Use a separate resource policy repository",
				description: "With -P, every active primary container and sidecar needs an entry in the mirrored policy file.",
				files: []exampleFile{{name: "resources/dev/apps/api.yml", format: "YAML", content: `containers:
  api:
    cpu: {from: "100m", to: "500m"}
    memory: {from: "128Mi", to: "512Mi"}
`}},
				shell: `kube-build-app validate -e dev -R environments -P resources
kube-build-app build -e dev -R environments -P resources -t deploy/dev`,
			},
		},
	},
	{
		name:        "Release images",
		description: "Pin images from a release manifest or override one container for a local build.",
		examples: []cliExample{
			{
				title:       "Use release manifest images",
				description: "Strict mode requires an image entry for every primary container. The default --image-reference auto uses the digest before the tag. Replace the example digest with your image's actual SHA-256 digest.",
				files: []exampleFile{{name: "release.yml", format: "YAML", content: `apiVersion: oci-toolbox/v2
kind: ImageRelease
release_id: RE_2026.10.03.01
images:
  - app_name: api
    container_name: api
    repository: registry.example.com/demo/api
    tag: RE_2026.10.03.01
    digest: sha256:0123456789abcdef...
`}},
				shell: `kube-build-app build -e dev -R environments -t deploy/dev \
  --release-manifest release.yml --image-policy strict`,
			},
			{
				title:       "Override one image",
				description: "The selector is app/container; a direct override has priority over the release manifest.",
				shell: `kube-build-app build -e dev -R environments -t deploy/dev \
  --image api/api=registry.example.com/demo/api:hotfix`,
			},
		},
	},
	{
		name:        "Deployment output",
		description: "Render manifests for a target namespace and sync metadata profile.",
		examples: []cliExample{
			{
				title:       "Render Argo CD metadata",
				description: "The sync set groups rendered objects. This command only writes local manifests.",
				shell: `kube-build-app build -e prod -R environments -t deploy/prod \
  --sync-metadata-profile argocd --sync-set prod`,
			},
			{
				title: "Render kube-deploy-sync metadata",
				shell: `kube-build-app build -e dev -R environments -t deploy/dev \
  --sync-metadata-profile kube-deploy-sync --sync-set dev`,
			},
			{
				title:       "Change the output namespace",
				description: "--namespace takes priority over NAMESPACE from variable sources.",
				shell:       "kube-build-app build -e dev -R environments -t deploy/dev --namespace demo-preview",
			},
			{
				title: "Scale one app to zero for a build",
				shell: "kube-build-app build -e dev -R environments -t deploy/dev --down worker",
			},
			{
				title:       "Choose a replica profile",
				description: "Profiles override replicas without editing app files.",
				files: []exampleFile{{name: "environments/dev/replica-profiles.yml", format: "YAML", content: `defaults:
  replica_profile_ref_name: normal
profiles:
  normal:
    apps:
      api: 2
  maintenance:
    all: 0
    apps:
      api: 1
`}},
				shell: "kube-build-app build -e dev -R environments -t deploy/dev -p maintenance",
			},
		},
	},
	{
		name:        "Generate or import",
		description: "Bootstrap ordinary editable model files; review generated YAML before using it.",
		examples: []cliExample{
			{
				title: "Create an environment",
				shell: `kube-build-app scaffold env -R environments --env dev \
  --namespace demo-dev --registry-url registry.example.com/demo`,
			},
			{
				title:       "Preview a new app",
				description: "--dry-run writes YAML to stdout. Remove it to create environments/dev/apps/worker.yml.",
				shell: `kube-build-app scaffold app worker -e dev -R environments \
  --runtime binary --replicas 2 --dry-run`,
			},
			{
				title:       "Import an existing Deployment file",
				description: "Add --report import-report.json to inspect conversion notes.",
				shell: `kube-build-app import --file deployment.yml \
  --output environments/dev/apps/api.yml --report import-report.json`,
			},
			{
				title:       "Import from a cluster",
				description: "Uses kubectl and the current context to read a Deployment and matching Services.",
				shell: `kube-build-app import --namespace demo-dev --deployment api \
  --output environments/dev/apps/api.yml`,
			},
			{
				title:       "Enable shell completion",
				description: "Generate the script for your shell and source it using your shell's configuration.",
				shell:       "kube-build-app completion zsh > kube-build-app.zsh",
			},
		},
	},
}

func examplesUseColor(w io.Writer, mode string) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
			return false, nil
		}
		file, ok := w.(*os.File)
		if !ok {
			return false, nil
		}
		info, err := file.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0, nil
	default:
		return false, fmt.Errorf("examples: invalid --color %q, expected auto, always or never", mode)
	}
}

func writeExamples(w io.Writer, color bool) error {
	write := func(format string, args ...any) error {
		_, err := fmt.Fprintf(w, format, args...)
		return err
	}
	if err := write("%s\n", examplePaint("KUBE-BUILD-APP EXAMPLES", ansiBold+ansiCyan, color)); err != nil {
		return err
	}
	if err := write("SHELL commands run from the repository root; FILE blocks are inputs to create or edit.\n"); err != nil {
		return err
	}
	if err := write("Use your own environment names, image references, paths and credentials.\n"); err != nil {
		return err
	}
	for _, section := range examplesCatalog {
		if err := write("\n%s\n  %s\n", examplePaint("## "+strings.ToUpper(section.name), ansiBold+ansiCyan, color), section.description); err != nil {
			return err
		}
		for _, example := range section.examples {
			if err := write("\n  %s\n", examplePaint("> "+example.title, ansiBold+ansiYellow, color)); err != nil {
				return err
			}
			if example.description != "" {
				if err := write("    %s\n", example.description); err != nil {
					return err
				}
			}
			for _, file := range example.files {
				label := "FILE " + file.name + " (" + file.format + ")"
				if err := write("\n    %s\n", examplePaint(label, ansiBold+ansiBlue, color)); err != nil {
					return err
				}
				for _, line := range strings.Split(strings.TrimSuffix(file.content, "\n"), "\n") {
					if err := write("      %s\n", examplePaint(line, ansiBlue, color)); err != nil {
						return err
					}
				}
			}
			if example.shell != "" {
				if err := write("\n    %s\n", examplePaint("SHELL", ansiBold+ansiGreen, color)); err != nil {
					return err
				}
				for _, line := range strings.Split(example.shell, "\n") {
					if err := write("      %s\n", examplePaint(line, ansiGreen, color)); err != nil {
						return err
					}
				}
			}
		}
	}
	return write("\nRun 'kube-build-app <command> --help' for all flags. Full model reference: README.md\n")
}

func examplePaint(value, style string, color bool) string {
	if !color {
		return value
	}
	return style + value + ansiReset
}
