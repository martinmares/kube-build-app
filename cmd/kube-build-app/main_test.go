package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"kube-env/internal/appinfo"
)

func TestVersionPrintsBuildInfoWithoutEnvironment(t *testing.T) {
	info := appinfo.Info{Name: appinfo.BuildAppName, Version: "0.20.5", Commit: "409ec9b", Date: "2026-10-06T20:15:49Z"}
	cmd := newRootCommand(info, &cliOptions{})
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version failed: %v", err)
	}
	want := "kube-build-app version 0.20.5 (commit 409ec9b, built 2026-10-06T20:15:49Z)\n"
	if out.String() != want {
		t.Fatalf("version output = %q, want %q", out.String(), want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestCobraLegacySummaryFlag(t *testing.T) {
	root := writeCLIEnv(t)
	out := runCLI(t, "-s", "-e", "test", "-R", root)
	for _, expected := range []string{
		"Resource summary for environment: test",
		"| api |   2 | api",
		"Totals",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("output missing %q:\n%s", expected, out)
		}
	}
}

func TestCobraBuildReportsDefaultOutputDir(t *testing.T) {
	root := writeCLIEnv(t)
	out := runCLI(t, "build", "-e", "test", "-R", root)
	for _, expected := range []string{
		"Generated 1 deployment(s)",
		"Output: " + filepath.Join(root, "test", "target"),
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("build output missing %q:\n%s", expected, out)
		}
	}
}

func TestCobraResourcePolicyRootOverridesInlineResources(t *testing.T) {
	root := writeCLIEnv(t)
	policyRoot := filepath.Join(t.TempDir(), "resources")
	writeTestFile(t, filepath.Join(policyRoot, "test", "apps", "api.yml"), `
containers:
  api:
    cpu: {from: "300m", to: "900m"}
    memory: {from: "256Mi", to: "768Mi"}
`)
	out := runCLI(t, "summary", "-e", "test", "-R", root, "-P", policyRoot)
	if !strings.Contains(out, "| api |   2 | api       | 300m") || !strings.Contains(out, "768Mi") {
		t.Fatalf("summary does not use external resource policy:\n%s", out)
	}
}

func TestCobraBuildImageOverride(t *testing.T) {
	root := writeCLIEnv(t)
	target := filepath.Join(t.TempDir(), "target")
	runCLI(t, "build", "-e", "test", "-R", root, "-t", target, "--image", "api/api=registry.cli/api:2")
	content, err := os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "image: registry.cli/api:2") {
		t.Fatalf("deployment image override missing:\n%s", content)
	}
}

func TestCobraBuildForceImageTagAndPrefix(t *testing.T) {
	root := writeCLIEnv(t)
	target := filepath.Join(t.TempDir(), "target")
	manifest := filepath.Join(t.TempDir(), "release.yml")
	writeTestFile(t, manifest, `
images:
  - app_name: api
    container_name: api
    image: harbor.example.com/old-project/team/api
    digest: sha256:abcdef
    tag: original
`)

	runCLI(
		t,
		"build",
		"-e", "test",
		"-R", root,
		"-t", target,
		"--release-manifest", manifest,
		"--force-image-prefix", "artifactory.example.com/docker-release/",
		"--force-image-tag", "emergency-1",
	)
	content, err := os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "image: artifactory.example.com/docker-release/api:emergency-1") {
		t.Fatalf("forced release image missing:\n%s", content)
	}
}

func TestCobraBuildYAMLIndent(t *testing.T) {
	root := writeCLIEnv(t)
	target := filepath.Join(t.TempDir(), "target")
	runCLI(t, "build", "-e", "test", "-R", root, "-t", target, "--yaml-indent", "4")
	content, err := os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "\n    template:\n") {
		t.Fatalf("deployment does not use four-space YAML indentation:\n%s", content)
	}
}

func TestCobraBuildRejectsSummaryFlag(t *testing.T) {
	root := writeCLIEnv(t)
	out, err := runCLIError("build", "-e", "test", "-R", root, "-s")
	if err == nil {
		t.Fatalf("build -s succeeded, want error:\n%s", out)
	}
	if !strings.Contains(err.Error(), "unknown shorthand flag: 's'") {
		t.Fatalf("unexpected error: %v\n%s", err, out)
	}
}

func TestCobraNoArgsPrintsHelp(t *testing.T) {
	var out bytes.Buffer
	cmd := newRootCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	cmd.SetArgs(nil)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	oldArgs := os.Args
	os.Args = []string{"kube-build-app"}
	defer func() { os.Args = oldArgs }()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute without args failed: %v\n%s", err, out.String())
	}
	for _, expected := range []string{
		"Build Kubernetes manifests from kube environment repositories",
		"Available Commands:",
		"completion",
	} {
		if !strings.Contains(out.String(), expected) {
			t.Fatalf("help output missing %q:\n%s", expected, out.String())
		}
	}
}

func TestCobraSubcommands(t *testing.T) {
	root := writeCLIEnv(t)
	if got := strings.TrimSpace(runCLI(t, "list", "-e", "test", "-R", root)); got != "api" {
		t.Fatalf("list output = %q, want api", got)
	}
	if got := strings.TrimSpace(runCLI(t, "validate", "-e", "test", "-R", root)); got != "Validation OK" {
		t.Fatalf("validate output = %q, want Validation OK", got)
	}
	jsonOut := runCLI(t, "summary", "--summary-format", "json", "-e", "test", "-R", root)
	if !strings.Contains(jsonOut, `"environment": "test"`) || !strings.Contains(jsonOut, `"total_cpu_request_cores": 0.2`) {
		t.Fatalf("unexpected summary json:\n%s", jsonOut)
	}
}

func TestCobraCompletionCommand(t *testing.T) {
	out := runCLI(t, "completion", "bash")
	if !strings.Contains(out, "bash completion for kube-build-app") {
		t.Fatalf("unexpected completion output:\n%s", out)
	}
}

func TestCobraImportDeploymentWithExplicitReport(t *testing.T) {
	tempDir := t.TempDir()
	source := filepath.Join(tempDir, "deployment.yml")
	output := filepath.Join(tempDir, "environments", "test", "apps", "api.yml")
	report := filepath.Join(tempDir, "api.import-report.json")
	deployment := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: source
spec:
  replicas: 2
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: registry.local/api:1
`
	if err := os.WriteFile(source, []byte(deployment), 0o644); err != nil {
		t.Fatal(err)
	}

	commandOutput := runCLI(t, "import", "--file", source, "--output", output, "--report", report)
	if !strings.Contains(commandOutput, "Imported Deployment api -> "+output) ||
		!strings.Contains(commandOutput, "Report: "+report) {
		t.Fatalf("unexpected import output:\n%s", commandOutput)
	}
	appContent, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"name: api", "replicas: 2", "image: registry.local/api:1"} {
		if !strings.Contains(string(appContent), expected) {
			t.Fatalf("app output missing %q:\n%s", expected, appContent)
		}
	}
	reportContent, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"source": "` + source + `"`,
		`"related_objects_read": false`,
		`"secret_values_read": false`,
	} {
		if !strings.Contains(string(reportContent), expected) {
			t.Fatalf("report missing %q:\n%s", expected, reportContent)
		}
	}

	root := filepath.Join(tempDir, "environments")
	envFile := filepath.Join(root, "test", "env.unsecured.json")
	if err := os.WriteFile(envFile, []byte(`{"environment":{"NAMESPACE":"import-test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runCLI(t, "validate", "-e", "test", "-R", root)); got != "Validation OK" {
		t.Fatalf("imported model does not validate: %s", got)
	}
	target := filepath.Join(tempDir, "deploy")
	runCLI(t, "build", "-e", "test", "-R", root, "-t", target)
	rendered, err := os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "image: registry.local/api:1") {
		t.Fatalf("imported model did not render expected image:\n%s", rendered)
	}
}

func TestCobraImportDoesNotCreateImplicitReportAndDoesNotOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	source := filepath.Join(tempDir, "deployment.yml")
	output := filepath.Join(tempDir, "api.yml")
	deployment := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: api:1
`
	if err := os.WriteFile(source, []byte(deployment), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, "import", "-f", source, "-o", output)
	if _, err := os.Stat(filepath.Join(tempDir, "api.import-report.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("implicit report exists or stat failed: %v", err)
	}
	_, err := runCLIError("import", "-f", source, "-o", output)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected overwrite result: %v", err)
	}
}

func TestCobraClusterImportReadsMatchingServices(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake kubectl shell script is Unix-specific")
	}

	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fakeKubectl := filepath.Join(binDir, "kubectl")
	script := `#!/bin/sh
case "$*" in
  *"get deployment tsm-gateway -o yaml"*)
    cat <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: tsm-gateway
  namespace: sandbox
spec:
  selector:
    matchLabels:
      app: tsm-gateway
  template:
    metadata:
      labels:
        app: tsm-gateway
    spec:
      containers:
        - name: tsm-gateway
          image: registry.local/tsm-gateway:2.4
          ports:
            - containerPort: 8080
          resources:
            requests:
              cpu: 100m
              memory: 250Mi
            limits:
              cpu: "4"
              memory: 1100Mi
YAML
    ;;
  *"get services -o yaml"*)
    cat <<'YAML'
apiVersion: v1
kind: ServiceList
items:
  - apiVersion: v1
    kind: Service
    metadata:
      name: tsm-gateway
    spec:
      selector:
        app: tsm-gateway
      ports:
        - name: http
          port: 80
          targetPort: 8080
YAML
    ;;
  *)
    echo "unexpected kubectl arguments: $*" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(fakeKubectl, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KUBECONFIG", "")

	output := filepath.Join(tempDir, "tsm-gateway.yml")
	report := filepath.Join(tempDir, "tsm-gateway.import-report.json")
	runCLI(t,
		"import",
		"--namespace", "sandbox",
		"--deployment", "tsm-gateway",
		"--output", output,
		"--report", report,
	)

	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	model := string(content)
	for _, expected := range []string{
		"from: 100m",
		`to: "4"`,
		"- name: http",
		"service_name: tsm-gateway",
		"port: 80",
	} {
		if !strings.Contains(model, expected) {
			t.Fatalf("cluster import missing %q:\n%s", expected, model)
		}
	}
	if strings.Contains(model, "requests:") || strings.Contains(model, "limits:") {
		t.Fatalf("cluster import emitted compatibility resource aliases:\n%s", model)
	}

	reportContent, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"services": 1`, `"related_objects_read": true`} {
		if !strings.Contains(string(reportContent), expected) {
			t.Fatalf("cluster import report missing %q:\n%s", expected, reportContent)
		}
	}
}

func TestCobraBuildVerboseTextAndJSON(t *testing.T) {
	root := writeCLIEnv(t)
	text := runCLI(t, "build", "-e", "test", "-R", root, "--verbose")
	for _, expected := range []string{
		"App api, with 1 container/s",
		"container api has 0 asset/s",
		"deployment api ->",
		"Generated 1 deployment(s)",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("verbose text missing %q:\n%s", expected, text)
		}
	}
	jsonOut := runCLI(t, "build", "-e", "test", "-R", root, "--verbose", "--log-format", "json")
	for _, expected := range []string{
		`"type": "app"`,
		`"app": "api"`,
		`"type": "deployment"`,
		"Generated 1 deployment(s)",
	} {
		if !strings.Contains(jsonOut, expected) {
			t.Fatalf("verbose json missing %q:\n%s", expected, jsonOut)
		}
	}
}

func TestCobraBuildVerboseColorModes(t *testing.T) {
	root := writeCLIEnv(t)
	colored := runCLI(t, "build", "-e", "test", "-R", root, "--verbose", "--color", "always")
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("expected ANSI color output:\n%s", colored)
	}
	plain := runCLI(t, "build", "-e", "test", "-R", root, "--verbose", "--color", "never")
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("expected plain output, got ANSI color:\n%s", plain)
	}
	jsonOut := runCLI(t, "build", "-e", "test", "-R", root, "--verbose", "--log-format", "json", "--color", "always")
	if strings.Contains(jsonOut, "\x1b[") {
		t.Fatalf("json log must not be colorized:\n%s", jsonOut)
	}
}

func TestSkeletonEnvAndAppAdd(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	out := runCLI(t, "skeleton", "env", "--root", root, "--env", "dev", "--namespace", "app-dev", "--registry-url", "registry.local/app", "--release-id", "1.0.0")
	if !strings.Contains(out, "Created environment skeleton:") {
		t.Fatalf("unexpected skeleton output:\n%s", out)
	}
	envPath := filepath.Join(root, "dev", "env.unsecured.json")
	envContent, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"NAMESPACE": "app-dev"`, `"REGISTRY_URL": "registry.local/app"`, `"RELEASE_ID": "1.0.0"`} {
		if !strings.Contains(string(envContent), expected) {
			t.Fatalf("env skeleton missing %q:\n%s", expected, envContent)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "dev", "apps", "_defaults.yml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dev", "apps", "_scaffold.yml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "_scaffold.yml")); err != nil {
		t.Fatal(err)
	}

	out = runCLI(t, "app", "add", "api", "-e", "dev", "--root", root)
	if !strings.Contains(out, "Created app model:") {
		t.Fatalf("unexpected app add output:\n%s", out)
	}
	appContent, err := os.ReadFile(filepath.Join(root, "dev", "apps", "api.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"name: api",
		"{{REGISTRY_URL}}/api:{{RELEASE_ID}}",
		"from: 100m",
		"to: 512Mi",
	} {
		if !strings.Contains(string(appContent), expected) {
			t.Fatalf("app skeleton missing %q:\n%s", expected, appContent)
		}
	}
}

func TestAppAddImageOverrideAndNoClobber(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	runCLI(t, "skeleton", "env", "--root", root, "--env", "dev")
	runCLI(t, "app", "add", "worker", "-e", "dev", "--root", root, "--image", "custom/worker:1")
	content, err := os.ReadFile(filepath.Join(root, "dev", "apps", "worker.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "image: custom/worker:1") {
		t.Fatalf("app image override missing:\n%s", content)
	}
	out, err := runCLIError("app", "add", "worker", "-e", "dev", "--root", root)
	if err == nil {
		t.Fatalf("app add overwrote existing file, output:\n%s", out)
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected no-clobber error: %v\n%s", err, out)
	}
}

func TestScaffoldAppMergesGlobalAndEnvironmentProfiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	runCLI(t, "scaffold", "env", "--root", root, "--env", "dev")
	envDir := filepath.Join(root, "dev")
	writeTestFile(t, filepath.Join(envDir, "assets", "utils", "start-java.sh"), "#!/bin/sh\nexec java \"$@\"\n")
	writeTestFile(t, filepath.Join(envDir, "assets", "config", "global.yml"), "source: global\n")
	writeTestFile(t, filepath.Join(envDir, "assets", "config", "local.yml"), "source: local\n")
	writeTestFile(t, filepath.Join(envDir, "shared.assets.yml"), `
assets:
  - file: assets/utils/start-java.sh
    to: /app/start-java.sh
`)
	writeTestFile(t, filepath.Join(root, "_scaffold.yml"), `
version: 1
defaults:
  replicas: 3
  resources:
    cpu: {from: 50m, to: 500m}
    memory: {from: 128Mi, to: 512Mi}
profiles:
  java-jib:
    runtime: java-jib
    app:
      registry:
        - secret_name: registry-secret
      labels:
        app.kubernetes.io/component: ${APP_NAME}
    container_defaults:
      envs:
        - name: PROFILE_SOURCE
          value: global
      ports:
        - name: http
          port: 8080
          expose_as:
            - hostname: ${APP_NAME}
              port: 80
      probes:
        preset: spring-actuator
        port: 8080
    wrapper:
      source: shared
      path: /app/start-java.sh
      arguments:
        - /app/jib-classpath-file
    assets:
      - file: config/global.yml
        to: /app/config.yml
    sidecars:
      - name: metrics
        image: registry.local/metrics:global
        startup:
          command: [/bin/metrics]
        resources:
          cpu: {from: 5m, to: 25m}
          memory: {from: 8Mi, to: 32Mi}
`)
	writeTestFile(t, filepath.Join(envDir, "apps", "_scaffold.yml"), `
version: 1
profiles:
  java-jib:
    resources:
      cpu:
        to: 900m
    assets:
      - file: config/local.yml
        to: /app/config.yml
    sidecars:
      - name: metrics
        image: registry.local/metrics:local
`)

	runCLI(t,
		"scaffold", "app", "api",
		"-e", "dev",
		"--root", root,
		"--scaffold-profile", "java-jib",
	)
	content, err := os.ReadFile(filepath.Join(envDir, "apps", "api.yml"))
	if err != nil {
		t.Fatal(err)
	}
	model := string(content)
	for _, expected := range []string{
		"replicas: 3",
		"secret_name: registry-secret",
		"app.kubernetes.io/component: api",
		"name: PROFILE_SOURCE",
		"value: global",
		"hostname: api",
		"preset: spring-actuator",
		"from: 50m",
		"to: 900m",
		"from: 128Mi",
		"to: 512Mi",
		"file: assets/config/local.yml",
		"image: registry.local/metrics:local",
		"command:",
		"- /bin/metrics",
		"from: 5m",
		"- /app/start-java.sh",
	} {
		if !strings.Contains(model, expected) {
			t.Fatalf("merged scaffold model missing %q:\n%s", expected, model)
		}
	}
	if strings.Contains(model, "assets/config/global.yml") || strings.Contains(model, "metrics:global") {
		t.Fatalf("environment override did not replace global values:\n%s", model)
	}
	if got := strings.TrimSpace(runCLI(t, "validate", "-e", "dev", "-R", root)); got != "Validation OK" {
		t.Fatalf("model with scaffold fragments does not validate: %s", got)
	}
}

func TestScaffoldEnvDoesNotOverwriteGlobalConfig(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	runCLI(t, "scaffold", "env", "--root", root, "--env", "dev")
	globalPath := filepath.Join(root, "_scaffold.yml")
	custom := "version: 1\nprofiles:\n  custom: {runtime: custom}\n"
	writeTestFile(t, globalPath, custom)

	runCLI(t, "scaffold", "env", "--root", root, "--env", "prod")
	content, err := os.ReadFile(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != custom {
		t.Fatalf("creating another environment overwrote global scaffold:\n%s", content)
	}
	localContent, err := os.ReadFile(filepath.Join(root, "prod", "apps", "_scaffold.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(localContent), "Optional overrides") {
		t.Fatalf("environment scaffold is not an override template:\n%s", localContent)
	}
}

func TestScaffoldAppUsesRepositoryProfileAndBuilds(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	runCLI(t, "scaffold", "env", "--root", root, "--env", "dev", "--namespace", "app-dev")
	envDir := filepath.Join(root, "dev")
	writeTestFile(t, filepath.Join(envDir, "assets", "utils", "start-java.sh"), "#!/bin/sh\nexec java \"$@\"\n")
	writeTestFile(t, filepath.Join(envDir, "assets", "config", "api.json.tpl"), "{}\n")
	writeTestFile(t, filepath.Join(envDir, "shared.assets.yml"), `
assets:
  - file: assets/utils/start-java.sh
    to: /app/start-java.sh
`)
	writeTestFile(t, filepath.Join(envDir, "apps", "_scaffold.yml"), `
version: 1
defaults:
  replicas: 2
  resources:
    cpu: {from: 50m, to: 300m}
    memory: {from: 96Mi, to: 384Mi}
profiles:
  java-jib:
    runtime: java-jib
    image: "{{REGISTRY_URL}}/${APP_NAME}:{{RELEASE_ID}}"
    wrapper:
      source: shared
      path: /app/start-java.sh
      arguments:
        - /app/jib-classpath-file
        - /app/jib-main-class-file
    assets:
      - file: config/${APP_NAME}.json.tpl
        to: /app/${APP_NAME}.json.tpl
        transform: true
    sidecars:
      - name: ${APP_NAME}-metrics
        image: "{{REGISTRY_URL}}/metrics:{{RELEASE_ID}}"
        envs:
          - name: TARGET_CONTAINER
            value: ${CONTAINER_NAME}
        resources:
          cpu: {from: 10m, to: 50m}
          memory: {from: 16Mi, to: 64Mi}
`)

	output := runCLI(t,
		"scaffold", "app", "api",
		"-e", "dev",
		"--root", root,
		"--scaffold-profile", "java-jib",
	)
	if !strings.Contains(output, "Created app model:") {
		t.Fatalf("unexpected scaffold output:\n%s", output)
	}
	appPath := filepath.Join(envDir, "apps", "api.yml")
	content, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatal(err)
	}
	model := string(content)
	for _, expected := range []string{
		"name: api",
		"replicas: 2",
		"image: '{{REGISTRY_URL}}/api:{{RELEASE_ID}}'",
		"- /app/start-java.sh",
		"- /app/jib-classpath-file",
		"file: assets/config/api.json.tpl",
		"to: /app/api.json.tpl",
		"name: api-metrics",
		"value: api",
		"from: 50m",
		"to: 384Mi",
	} {
		if !strings.Contains(model, expected) {
			t.Fatalf("scaffold model missing %q:\n%s", expected, model)
		}
	}
	if strings.Contains(model, "file: assets/utils/start-java.sh") {
		t.Fatalf("shared wrapper was duplicated as an app asset:\n%s", model)
	}
	if got := strings.TrimSpace(runCLI(t, "validate", "-e", "dev", "-R", root)); got != "Validation OK" {
		t.Fatalf("scaffolded model does not validate: %s", got)
	}
	target := filepath.Join(t.TempDir(), "deploy")
	runCLI(t, "build", "-e", "dev", "-R", root, "-t", target)
	deployment, err := os.ReadFile(filepath.Join(target, "deployments", "api-deployment.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"name: api-metrics", "mountPath: /app/start-java.sh"} {
		if !strings.Contains(string(deployment), expected) {
			t.Fatalf("built deployment missing %q:\n%s", expected, deployment)
		}
	}
}

func TestScaffoldAppAssetWrapperAndCLIAsset(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	runCLI(t, "scaffold", "env", "--root", root, "--env", "dev")
	envDir := filepath.Join(root, "dev")
	writeTestFile(t, filepath.Join(envDir, "assets", "utils", "start-worker.sh"), "#!/bin/sh\nexec worker\n")
	writeTestFile(t, filepath.Join(envDir, "assets", "config", "worker.yml"), "listen: 8080\n")

	runCLI(t,
		"scaffold", "app", "worker",
		"-e", "dev",
		"--root", root,
		"--runtime", "binary",
		"--wrapper-source", "asset",
		"--wrapper-file", "utils/start-worker.sh",
		"--wrapper-path", "/app/start-worker.sh",
		"--asset", "config/worker.yml=/app/config.yml",
		"--sidecar", "audit=registry.local/audit:1",
	)
	content, err := os.ReadFile(filepath.Join(envDir, "apps", "worker.yml"))
	if err != nil {
		t.Fatal(err)
	}
	model := string(content)
	for _, expected := range []string{
		"- /app/start-worker.sh",
		"file: assets/utils/start-worker.sh",
		"file: assets/config/worker.yml",
		"name: audit",
		"image: registry.local/audit:1",
		"from: 10m",
	} {
		if !strings.Contains(model, expected) {
			t.Fatalf("scaffold model missing %q:\n%s", expected, model)
		}
	}
}

func TestScaffoldAppDryRunAndSafetyChecks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	runCLI(t, "scaffold", "env", "--root", root, "--env", "dev")
	envDir := filepath.Join(root, "dev")
	writeTestFile(t, filepath.Join(envDir, "assets", "config.yml"), "enabled: true\n")

	output := runCLI(t,
		"scaffold", "app", "preview",
		"-e", "dev",
		"--root", root,
		"--dry-run",
		"--asset", "config.yml=/app/config.yml",
	)
	if !strings.Contains(output, "name: preview") {
		t.Fatalf("dry-run did not print model:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(envDir, "apps", "preview.yml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote app file or stat failed: %v", err)
	}

	_, err := runCLIError(
		"scaffold", "app", "unsafe",
		"-e", "dev",
		"--root", root,
		"--asset", "../secret=/app/secret",
	)
	if err == nil || !strings.Contains(err.Error(), "escapes <environment>/assets") {
		t.Fatalf("unexpected traversal result: %v", err)
	}

	if runtime.GOOS != "windows" {
		outside := filepath.Join(t.TempDir(), "outside-secret")
		writeTestFile(t, outside, "secret\n")
		if err := os.Symlink(outside, filepath.Join(envDir, "assets", "linked-secret")); err != nil {
			t.Fatal(err)
		}
		_, err = runCLIError(
			"scaffold", "app", "unsafe-link",
			"-e", "dev",
			"--root", root,
			"--asset", "linked-secret=/app/secret",
		)
		if err == nil || !strings.Contains(err.Error(), "resolves outside <environment>/assets") {
			t.Fatalf("unexpected symlink escape result: %v", err)
		}
	}

	_, err = runCLIError(
		"scaffold", "app", "missing-shared",
		"-e", "dev",
		"--root", root,
		"--wrapper-source", "shared",
		"--wrapper-path", "/app/missing.sh",
	)
	if err == nil || !strings.Contains(err.Error(), "is not provided by shared.assets.yml") {
		t.Fatalf("unexpected shared wrapper result: %v", err)
	}
}

func TestScaffoldConfigRejectsUnknownFields(t *testing.T) {
	root := filepath.Join(t.TempDir(), "environments")
	runCLI(t, "scaffold", "env", "--root", root, "--env", "dev")
	writeTestFile(t, filepath.Join(root, "dev", "apps", "_scaffold.yml"), `
version: 1
profiles:
  broken:
    runtime: custom
    wraper:
      source: image
      path: /app/start.sh
`)

	_, err := runCLIError(
		"scaffold", "app", "api",
		"-e", "dev",
		"--root", root,
		"--scaffold-profile", "broken",
	)
	if err == nil || !strings.Contains(err.Error(), `field wraper not found`) {
		t.Fatalf("unexpected unknown field result: %v", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runCLIError(args...)
	if err != nil {
		t.Fatalf("execute %v failed: %v\n%s", args, err, out)
	}
	return out
}

func runCLIError(args ...string) (string, error) {
	var out bytes.Buffer
	cmd := newRootCommand(appinfo.For(appinfo.BuildAppName), &cliOptions{})
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()
	return out.String(), err
}

func writeCLIEnv(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "environments")
	envDir := filepath.Join(root, "test")
	if err := os.MkdirAll(filepath.Join(envDir, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "env.unsecured.json"), []byte(`{"environment":{"NAMESPACE":"cli-test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	app := `
name: api
replicas: 2
containers:
  - name: api
    image: api:latest
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "64Mi", to: "128Mi"}
`
	if err := os.WriteFile(filepath.Join(envDir, "apps", "api.yml"), []byte(app), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
