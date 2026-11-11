package buildapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildGeneratesBasicDeployment(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
vars:
  - name: APP_NAME
    value: "api"
name: {{var:APP_NAME}}
replicas: 2
containers:
  - name: "{{var:APP_NAME}}"
    image: "{{TSM_REGISTRY_URL}}/{{var:APP_NAME}}:{{TSM_RELEASE_ID}}"
    startup:
      command:
        - /bin/sh
      arguments:
        - -c
        - echo ok
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
    ports:
      - name: http
        port: 8080
        expose_as:
          - service_name: api
            port: 80
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deployments) != 1 {
		t.Fatalf("len(deployments) = %d, want 1", len(result.Deployments))
	}

	deploymentPath := filepath.Join(target, "deployments", "api-deployment.yml")
	content, err := os.ReadFile(deploymentPath)
	if err != nil {
		t.Fatal(err)
	}
	var deployment map[string]any
	if err := yaml.Unmarshal(content, &deployment); err != nil {
		t.Fatal(err)
	}

	if got := digString(deployment, "metadata", "name"); got != "api" {
		t.Fatalf("metadata.name = %q, want api", got)
	}
	if got := digString(deployment, "metadata", "namespace"); got != "nac-test" {
		t.Fatalf("metadata.namespace = %q, want nac-test", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}" {
		t.Fatalf("container image = %q, want unresolved apply-env placeholders", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "ports", "0", "containerPort"); got != 8080 {
		t.Fatalf("container port = %d, want 8080", got)
	}

	service := loadYAML(t, filepath.Join(target, "services", "api-service.yml"))
	if got := digString(service, "metadata", "name"); got != "api" {
		t.Fatalf("service metadata.name = %q, want api", got)
	}
	if got := digInt(service, "spec", "ports", "0", "port"); got != 80 {
		t.Fatalf("service port = %d, want 80", got)
	}
	if got := digInt(service, "spec", "ports", "0", "targetPort"); got != 8080 {
		t.Fatalf("service targetPort = %d, want 8080", got)
	}
}

func TestBuildUsesExplicitSelectorLabelsForDeploymentAndServices(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "selector-test"},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
selector_labels:
  app: imported-api
containers:
  - name: api
    image: api:1
    ports:
      - name: http
        port: 8080
        expose_as:
          - service_name: api
            port: 80
`)

	if _, err := Build(Options{Environment: "test", Root: root, Target: target}); err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "selector", "matchLabels", "app"); got != "imported-api" {
		t.Fatalf("deployment selector app = %q, want imported-api", got)
	}
	if got := digString(deployment, "spec", "selector", "matchLabels", appLabel); got != "" {
		t.Fatalf("deployment selector unexpectedly contains generated label %q", got)
	}
	if got := digString(deployment, "spec", "template", "metadata", "labels", appLabel); got != "api" {
		t.Fatalf("pod template generated label = %q, want api", got)
	}
	service := loadYAML(t, filepath.Join(target, "services", "api-service.yml"))
	if got := digString(service, "spec", "selector", "app"); got != "imported-api" {
		t.Fatalf("service selector app = %q, want imported-api", got)
	}
	if got := digString(service, "spec", "selector", appLabel); got != "" {
		t.Fatalf("service selector unexpectedly contains generated label %q", got)
	}
}

func TestWriteRenderedObjectUsesConfiguredYAMLIndent(t *testing.T) {
	object := map[string]any{
		"spec": map[string]any{
			"replicas": 1,
		},
	}

	defaultPath := filepath.Join(t.TempDir(), "default.yml")
	if err := writeRenderedObject(defaultPath, object, Options{}, syncMetadataSpec{}); err != nil {
		t.Fatal(err)
	}
	defaultContent, err := os.ReadFile(defaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(defaultContent), "\n  replicas: 1\n") {
		t.Fatalf("default YAML must use two-space indentation:\n%s", defaultContent)
	}

	fourSpacePath := filepath.Join(t.TempDir(), "four-space.yml")
	if err := writeRenderedObject(fourSpacePath, object, Options{YAMLIndent: 4}, syncMetadataSpec{}); err != nil {
		t.Fatal(err)
	}
	fourSpaceContent, err := os.ReadFile(fourSpacePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fourSpaceContent), "\n    replicas: 1\n") {
		t.Fatalf("configured YAML must use four-space indentation:\n%s", fourSpaceContent)
	}
}

func TestBuildRejectsUnsupportedYAMLIndent(t *testing.T) {
	_, err := Build(Options{Environment: "test", YAMLIndent: 3})
	if err == nil || !strings.Contains(err.Error(), "yaml indent must be 2 or 4") {
		t.Fatalf("Build error = %v, want invalid YAML indentation", err)
	}
}

func TestBuildSupportsEnvFileAndVarsSourcePrecedence(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "json-ns",
			"TSM_REGISTRY_URL": "json-registry",
			"TSM_RELEASE_ID":   "json-release",
			"SOURCE_TEST":      "json-value",
		},
	})
	writeFile(t, filepath.Join(envDir, ".env"), "SOURCE_TEST=dot-value\nTSM_RELEASE_ID=dot-release\n")
	writeFile(t, filepath.Join(envDir, "explicit.env"), "SOURCE_TEST=explicit-value\nNAMESPACE=explicit-ns\nTSM_REGISTRY_URL=explicit-registry\nTSM_RELEASE_ID=explicit-release\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{env:TSM_REGISTRY_URL}}/{{env:SOURCE_TEST}}:{{env:TSM_RELEASE_ID}}"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	t.Setenv("SOURCE_TEST", "env-value")
	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "json-registry/env-value:json-release" {
		t.Fatalf("default image = %q", got)
	}

	target2 := filepath.Join(t.TempDir(), "target")
	_, err = Build(Options{Environment: "test", Root: root, Target: target2, VarsSources: []string{"json,dot-env"}})
	if err != nil {
		t.Fatal(err)
	}
	deployment = loadYAML(t, filepath.Join(target2, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "json-registry/dot-value:dot-release" {
		t.Fatalf("dot-env image = %q", got)
	}

	target3 := filepath.Join(t.TempDir(), "target")
	_, err = Build(Options{Environment: "test", Root: root, Target: target3, EnvFile: filepath.Join(envDir, "explicit.env")})
	if err != nil {
		t.Fatal(err)
	}
	deployment = loadYAML(t, filepath.Join(target3, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "namespace"); got != "explicit-ns" {
		t.Fatalf("explicit namespace = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "explicit-registry/explicit-value:explicit-release" {
		t.Fatalf("explicit image = %q", got)
	}
}

func TestBuildLegacyApplyEnvResolvesBareDeploymentPlaceholders(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeFile(t, filepath.Join(envDir, "explicit.env"), "NAMESPACE=legacy-ns\nTSM_REGISTRY_URL=legacy-registry\nTSM_RELEASE_ID=9.1.0\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
labels:
  app.kubernetes.io/version: "{{TSM_RELEASE_ID}}"
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{
		Environment:    "test",
		Root:           root,
		Target:         target,
		EnvFile:        filepath.Join(envDir, "explicit.env"),
		LegacyApplyEnv: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "labels", "app.kubernetes.io/version"); got != "9.1.0" {
		t.Fatalf("version label = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "legacy-registry/api:9.1.0" {
		t.Fatalf("image = %q", got)
	}
}

func TestBuildLegacyApplyEnvResolvesAssetTargetBeforeSyncMetadata(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeFile(t, filepath.Join(envDir, "explicit.env"), "NAMESPACE=legacy-ns\nTSM_REGISTRY_URL=legacy-registry\nTSM_RELEASE_ID=9.1.0\nJAVA_SSL_KEYSTORE_LOCATION=/app/resources/client.jks\n")
	writeFile(t, filepath.Join(envDir, "assets", "client.conf"), "runtime={{RUNTIME_SECRET}}\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    assets:
      - file: assets/client.conf
        to: "{{JAVA_SSL_KEYSTORE_LOCATION}}"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	result, err := Build(Options{
		Environment:    "test",
		Root:           root,
		Target:         target,
		EnvFile:        filepath.Join(envDir, "explicit.env"),
		LegacyApplyEnv: true,
		SyncProfile:    "kube-deploy-sync",
		SyncSet:        "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 1 {
		t.Fatalf("len(assets) = %d, want 1", len(result.Assets))
	}
	asset := loadYAML(t, result.Assets[0])
	wantSyncID := "test/app/api/container/api/asset/assets/client.conf:/app/resources/client.jks"
	assertSyncMetadata(t, asset, wantSyncID, "100", "kube-deploy-sync")
	if got := digString(asset, "data", "client.conf"); got != "runtime={{RUNTIME_SECRET}}\n" {
		t.Fatalf("asset content = %q, runtime placeholder must remain untouched", got)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "mountPath"); got != "/app/resources/client.jks" {
		t.Fatalf("asset mount path = %q", got)
	}
}

func TestLegacyApplyEnvRejectsUnknownVariableAndSkipsOrdinaryServices(t *testing.T) {
	dir := t.TempDir()
	deployment := filepath.Join(dir, "deployment.yml")
	service := filepath.Join(dir, "service.yml")
	writeFile(t, deployment, "value: '{{MISSING}}'\n")
	writeFile(t, service, "value: '{{UNCHANGED}}'\n")

	err := applyLegacyEnvironment(Result{Deployments: []string{deployment}, Services: []string{service}}, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "unresolved variable(s): MISSING") {
		t.Fatalf("error = %v", err)
	}
	content, readErr := os.ReadFile(service)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "value: '{{UNCHANGED}}'\n" {
		t.Fatalf("ordinary service was unexpectedly changed: %s", content)
	}
}

func TestBuildSupportsEnvURL(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{env:TSM_REGISTRY_URL}}/api:{{env:TSM_RELEASE_ID}}"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization header = %q", got)
		}
		fmt.Fprintln(w, "NAMESPACE=url-ns")
		fmt.Fprintln(w, "TSM_REGISTRY_URL=url-registry")
		fmt.Fprintln(w, "TSM_RELEASE_ID=url-release")
	}))
	defer server.Close()

	_, err := Build(Options{
		Environment:   "test",
		Root:          root,
		Target:        target,
		EnvURL:        server.URL,
		EnvURLHeaders: []string{"Authorization: Bearer test-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "namespace"); got != "url-ns" {
		t.Fatalf("namespace = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "url-registry/api:url-release" {
		t.Fatalf("image = %q", got)
	}
}

func TestBuildSupportsInsecureEnvURLTLS(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{env:TSM_REGISTRY_URL}}/api:{{env:TSM_RELEASE_ID}}"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "NAMESPACE=tls-ns")
		fmt.Fprintln(w, "TSM_REGISTRY_URL=tls-registry")
		fmt.Fprintln(w, "TSM_RELEASE_ID=tls-release")
	}))
	defer server.Close()

	_, err := Build(Options{
		Environment:    "test",
		Root:           root,
		Target:         target,
		EnvURL:         server.URL,
		EnvURLInsecure: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "namespace"); got != "tls-ns" {
		t.Fatalf("namespace = %q", got)
	}
}

func TestBuildRejectsEnvURLCombinations(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "env file",
			opts: Options{EnvFile: "local.env", EnvURL: "https://example.test/env"},
			want: "-E/--env-file cannot be combined with --env-url",
		},
		{
			name: "vars source",
			opts: Options{EnvURL: "https://example.test/env", VarsSources: []string{"json"}},
			want: "--env-url cannot be combined with --vars-source",
		},
		{
			name: "decrypt secured",
			opts: Options{EnvURL: "https://example.test/env", DecryptSecured: true},
			want: "--env-url cannot be combined with -d/--decrypt-secured",
		},
		{
			name: "header without url",
			opts: Options{EnvURLHeaders: []string{"Authorization: Bearer token"}},
			want: "--env-url-header requires --env-url",
		},
		{
			name: "insecure without url",
			opts: Options{EnvURLInsecure: true},
			want: "--env-url-insecure requires --env-url",
		},
		{
			name: "invalid header",
			opts: Options{EnvURL: "https://example.test/env", EnvURLHeaders: []string{"Authorization"}},
			want: "invalid --env-url-header",
		},
		{
			name: "invalid url scheme",
			opts: Options{EnvURL: "file:///tmp/env"},
			want: "expected http or https URL",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadVars(t.TempDir(), tc.opts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadVars error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBuildHintsBarePlaceholderInTypedAppField(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeFile(t, filepath.Join(envDir, ".env"), "NAMESPACE=nac-test\nDEFAULT_EXPOSE_PORT=8080\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
containers:
  - name: api
    image: registry.local/api:1
    ports:
      - name: http
        port: {{DEFAULT_EXPOSE_PORT}}
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, EnvFile: filepath.Join(envDir, ".env")})
	if err == nil {
		t.Fatal("expected bare placeholder YAML unmarshal error")
	}
	message := err.Error()
	for _, expected := range []string{
		"cannot unmarshal !!map into int",
		"bare {{NAME}} placeholders are passthrough legacy placeholders",
		"use {{env:NAME}} for .env/process variables or {{var:NAME}} for YAML vars",
		"{{DEFAULT_EXPOSE_PORT}}",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("error message missing %q:\n%s", expected, message)
		}
	}
}

func TestLoadVarsDecryptSecuredUsesEncjsonAPISelection(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE": "json-ns",
		},
	})
	writeFile(t, filepath.Join(envDir, "env.secured.json"), `{
  "_public_key": "dummy",
  "environment": {
    "SECRET": "EncJson[@api=2.0:@box=<encrypted>]"
  }
}`)
	rustBin, rustArgs := writeFakeEncjson(t, "rust", `{"_public_key":"decrypted-key","environment":{"SECRET":"decrypted-secret"}}`)
	legacyBin, _ := writeFakeEncjson(t, "legacy", `{"_public_key":"legacy-key","environment":{"SECRET":"legacy-secret"}}`)
	keydir := filepath.Join(t.TempDir(), "keys")
	t.Setenv("ENCJSON_PATH", rustBin)
	t.Setenv("ENCJSON_LEGACY_PATH", legacyBin)
	t.Setenv("ENCJSON_KEYDIR", keydir)

	vars, err := loadVars(envDir, Options{DecryptSecured: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := vars["SECRET"]; got != "decrypted-secret" {
		t.Fatalf("SECRET = %q, want decrypted-secret", got)
	}
	if got := vars["_public_key"]; got != "decrypted-key" {
		t.Fatalf("_public_key = %q, want decrypted-key", got)
	}
	argsContent, err := os.ReadFile(rustArgs)
	if err != nil {
		t.Fatal(err)
	}
	args := string(argsContent)
	for _, expected := range []string{"decrypt", "-k", keydir, "-f", filepath.Join(envDir, "env.secured.json")} {
		if !strings.Contains(args, expected) {
			t.Fatalf("fake encjson args missing %q: %s", expected, args)
		}
	}
}

func TestLoadVarsDecryptSecuredUsesLegacyEncjsonForAPI1(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE": "json-ns",
		},
	})
	writeFile(t, filepath.Join(envDir, "env.secured.json"), `{
  "_public_key": "dummy",
  "environment": {
    "SECRET": "EncJson[@api=1.0:@box=<encrypted>]"
  }
}`)
	rustBin, _ := writeFakeEncjson(t, "rust", `{"_public_key":"rust-key","environment":{"SECRET":"rust-secret"}}`)
	legacyBin, legacyArgs := writeFakeEncjson(t, "legacy", `{"_public_key":"legacy-key","environment":{"SECRET":"legacy-secret"}}`)
	t.Setenv("ENCJSON_PATH", rustBin)
	t.Setenv("ENCJSON_LEGACY_PATH", legacyBin)
	t.Setenv("ENCJSON_KEYDIR", filepath.Join(t.TempDir(), "keys"))

	vars, err := loadVars(envDir, Options{DecryptSecured: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := vars["SECRET"]; got != "legacy-secret" {
		t.Fatalf("SECRET = %q, want legacy-secret", got)
	}
	if _, err := os.Stat(legacyArgs); err != nil {
		t.Fatalf("legacy encjson was not executed: %v", err)
	}
}

func TestLoadVarsDecryptSecuredOmitsKeydirWhenUnset(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE": "json-ns",
		},
	})
	writeFile(t, filepath.Join(envDir, "env.secured.json"), `{
  "_public_key": "dummy",
  "environment": {
    "SECRET": "EncJson[@api=2.0:@box=<encrypted>]"
  }
}`)
	rustBin, rustArgs := writeFakeEncjson(t, "rust", `{"_public_key":"decrypted-key","environment":{"SECRET":"decrypted-secret"}}`)
	t.Setenv("ENCJSON_PATH", rustBin)
	t.Setenv("ENCJSON_KEYDIR", "")

	vars, err := loadVars(envDir, Options{DecryptSecured: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := vars["SECRET"]; got != "decrypted-secret" {
		t.Fatalf("SECRET = %q, want decrypted-secret", got)
	}
	argsContent, err := os.ReadFile(rustArgs)
	if err != nil {
		t.Fatal(err)
	}
	args := string(argsContent)
	if strings.Contains(args, "-k") {
		t.Fatalf("fake encjson args unexpectedly contain -k: %s", args)
	}
	for _, expected := range []string{"decrypt", "-f", filepath.Join(envDir, "env.secured.json")} {
		if !strings.Contains(args, expected) {
			t.Fatalf("fake encjson args missing %q: %s", expected, args)
		}
	}
}

func TestBuildGeneratesServiceWithMetricsAndHeadless(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    ports:
      - name: http
        port: 8080
        metrics: true
        metricsPathFor: api
        expose_as:
          - hostname: api-headless
            port: 80
            type: headless
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Services) != 1 {
		t.Fatalf("len(services) = %d, want 1", len(result.Services))
	}

	service := loadYAML(t, filepath.Join(target, "services", "api-headless-service.yml"))
	if got := digString(service, "spec", "clusterIP"); got != "None" {
		t.Fatalf("clusterIP = %q, want None", got)
	}
	if got := digString(service, "metadata", "labels", "metrics"); got != "true" {
		t.Fatalf("metrics label = %q, want true", got)
	}
	if got := digString(service, "metadata", "labels", "metricsPathFor"); got != "api-headless" {
		t.Fatalf("metricsPathFor label = %q, want api-headless", got)
	}
	if got := digInt(service, "spec", "ports", "1", "port"); got != 9090 {
		t.Fatalf("metrics port = %d, want 9090", got)
	}
	if got := digInt(service, "spec", "ports", "1", "targetPort"); got != 8080 {
		t.Fatalf("metrics targetPort = %d, want 8080", got)
	}
}

func TestBuildGeneratesExternalIngressAndRoute(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    ports:
      - name: http
        port: 8080
        expose_as:
          - hostname: api
            port: 80
            external:
              - name: api-public
                http:
                  - hostname: api.example.test
                    path: /
                https:
                  - hostname: api.example.test
                    secret_name: api-tls
              - name: api-route
                as_route: true
                http:
                  - hostname: api-route.example.test
                tls:
                  termination: edge
                  insecureEdgeTerminationPolicy: Redirect
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target, SyncProfile: "kube-deploy-sync"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Externals) != 2 {
		t.Fatalf("len(externals) = %d, want 2", len(result.Externals))
	}

	ingress := loadYAML(t, filepath.Join(target, "services", "external", "api-public-ingress.yml"))
	assertSyncMetadata(t, ingress, "test/Ingress/nac-test/api-public", "400", "kube-deploy-sync")
	if got := digString(ingress, "kind"); got != "Ingress" {
		t.Fatalf("ingress kind = %q", got)
	}
	if got := digString(ingress, "spec", "rules", "0", "host"); got != "api.example.test" {
		t.Fatalf("ingress host = %q", got)
	}
	if got := digInt(ingress, "spec", "rules", "0", "http", "paths", "0", "backend", "service", "port", "number"); got != 80 {
		t.Fatalf("ingress backend port = %d, want 80", got)
	}

	route := loadYAML(t, filepath.Join(target, "services", "external", "api-route-route.yml"))
	assertSyncMetadata(t, route, "test/Route/nac-test/api-route", "400", "kube-deploy-sync")
	if got := digString(route, "kind"); got != "Route" {
		t.Fatalf("route kind = %q", got)
	}
	if got := digString(route, "spec", "port", "targetPort"); got != "http-80" {
		t.Fatalf("route targetPort = %q, want http-80", got)
	}
	if got := digString(route, "spec", "tls", "termination"); got != "edge" {
		t.Fatalf("route tls termination = %q, want edge", got)
	}
}

func TestBuildGeneratesLegacyHealthProbes(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    health:
      http:
        path:
          live: /live
          ready: /ready
        port: 8080
      delay: 10
      period: 5
      timeout: 3
      success: 1
      failure: 2
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "livenessProbe", "httpGet", "path"); got != "/live" {
		t.Fatalf("liveness path = %q, want /live", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "readinessProbe", "httpGet", "path"); got != "/ready" {
		t.Fatalf("readiness path = %q, want /ready", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "livenessProbe", "initialDelaySeconds"); got != 10 {
		t.Fatalf("initialDelaySeconds = %d, want 10", got)
	}
}

func TestBuildGeneratesExplicitProbeBlocks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    probe:
      live:
        command: ["/bin/check-live"]
        delay: 11
      ready:
        http:
          path: /ready
          port: 8080
        period: 7
      start:
        http:
          path: /start
          port: 8080
        failure: 30
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "livenessProbe", "exec", "command", "0"); got != "/bin/check-live" {
		t.Fatalf("liveness command = %q, want /bin/check-live", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "livenessProbe", "initialDelaySeconds"); got != 11 {
		t.Fatalf("liveness delay = %d, want 11", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "readinessProbe", "httpGet", "path"); got != "/ready" {
		t.Fatalf("readiness path = %q, want /ready", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "startupProbe", "httpGet", "path"); got != "/start" {
		t.Fatalf("startup path = %q, want /start", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "startupProbe", "failureThreshold"); got != 30 {
		t.Fatalf("startup failureThreshold = %d, want 30", got)
	}
}

func TestBuildGeneratesModernProbesPreset(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
vars:
  - name: EXPOSE_PORT
    value: 8080
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    probes:
      preset: spring-actuator
      port: "{{var:EXPOSE_PORT}}"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "livenessProbe", "httpGet", "path"); got != "/actuator/health/liveness" {
		t.Fatalf("liveness path = %q, want /actuator/health/liveness", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "readinessProbe", "httpGet", "path"); got != "/actuator/health/readiness" {
		t.Fatalf("readiness path = %q, want /actuator/health/readiness", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "startupProbe", "httpGet", "path"); got != "/actuator/health" {
		t.Fatalf("startup path = %q, want /actuator/health", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "livenessProbe", "httpGet", "port"); got != 8080 {
		t.Fatalf("liveness port = %d, want 8080", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "startupProbe", "failureThreshold"); got != 30 {
		t.Fatalf("startup failureThreshold = %d, want 30", got)
	}
}

func TestBuildGeneratesModernProbesOverrides(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    probes:
      http:
        path: /healthz
        port: 8080
      ready:
        period: 3
        success: 1
      start:
        command: ["/bin/check-start"]
        failure: 40
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "livenessProbe", "httpGet", "path"); got != "/healthz" {
		t.Fatalf("liveness path = %q, want /healthz", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "readinessProbe", "periodSeconds"); got != 3 {
		t.Fatalf("readiness period = %d, want 3", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "startupProbe", "exec", "command", "0"); got != "/bin/check-start" {
		t.Fatalf("startup command = %q, want /bin/check-start", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "startupProbe", "failureThreshold"); got != 40 {
		t.Fatalf("startup failureThreshold = %d, want 40", got)
	}
}

func TestBuildGeneratesSchedulingFieldsAndRawContainerPassthrough(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
arch: amd64
node_selector:
  workload: tsm
tolerations:
  - key: dedicated
    operator: Equal
    value: tsm
    effect: NoSchedule
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    raw:
      securityContext:
        allowPrivilegeEscalation: false
      stdin: true
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "nodeSelector", "kubernetes.io/arch"); got != "amd64" {
		t.Fatalf("arch nodeSelector = %q, want amd64", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "nodeSelector", "workload"); got != "tsm" {
		t.Fatalf("workload nodeSelector = %q, want tsm", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "tolerations", "0", "key"); got != "dedicated" {
		t.Fatalf("toleration key = %q, want dedicated", got)
	}
	if got := digBool(deployment, "spec", "template", "spec", "containers", "0", "securityContext", "allowPrivilegeEscalation"); got {
		t.Fatalf("allowPrivilegeEscalation = true, want false")
	}
	if got := digBool(deployment, "spec", "template", "spec", "containers", "0", "stdin"); !got {
		t.Fatalf("stdin = false, want true")
	}
}

func TestBuildGeneratesPodAndContainerSecurityContext(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
security_context:
  runAsNonRoot: true
  fsGroup: 2000
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    security_context:
      allowPrivilegeEscalation: false
      runAsUser: 1000
      capabilities:
        drop: ["ALL"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digBool(deployment, "spec", "template", "spec", "securityContext", "runAsNonRoot"); !got {
		t.Fatalf("pod runAsNonRoot = false, want true")
	}
	if got := digInt(deployment, "spec", "template", "spec", "securityContext", "fsGroup"); got != 2000 {
		t.Fatalf("pod fsGroup = %d, want 2000", got)
	}
	if got := digBool(deployment, "spec", "template", "spec", "containers", "0", "securityContext", "allowPrivilegeEscalation"); got {
		t.Fatalf("container allowPrivilegeEscalation = true, want false")
	}
	if got := digInt(deployment, "spec", "template", "spec", "containers", "0", "securityContext", "runAsUser"); got != 1000 {
		t.Fatalf("container runAsUser = %d, want 1000", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "securityContext", "capabilities", "drop", "0"); got != "ALL" {
		t.Fatalf("container dropped capability = %q, want ALL", got)
	}
}

func TestBuildGeneratesLifecyclePreStopAndTerminationGrace(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
termination_grace_period: 45
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    lifecycle:
      pre_stop:
        command: ["/bin/sh", "-c", "sleep 10"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digInt(deployment, "spec", "template", "spec", "terminationGracePeriodSeconds"); got != 45 {
		t.Fatalf("terminationGracePeriodSeconds = %d, want 45", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "lifecycle", "preStop", "exec", "command", "2"); got != "sleep 10" {
		t.Fatalf("preStop command[2] = %q, want sleep 10", got)
	}
}

func TestBuildGeneratesServiceAccountAndEnvFrom(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
service_account: tsm-api
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    env_from:
      - config_map: api-config
      - secret: api-secret
        prefix: SECRET_
      - secretRef:
          name: optional-secret
          optional: true
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "serviceAccountName"); got != "tsm-api" {
		t.Fatalf("serviceAccountName = %q, want tsm-api", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "envFrom", "0", "configMapRef", "name"); got != "api-config" {
		t.Fatalf("envFrom[0].configMapRef.name = %q, want api-config", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "envFrom", "1", "secretRef", "name"); got != "api-secret" {
		t.Fatalf("envFrom[1].secretRef.name = %q, want api-secret", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "envFrom", "1", "prefix"); got != "SECRET_" {
		t.Fatalf("envFrom[1].prefix = %q, want SECRET_", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "envFrom", "2", "secretRef", "name"); got != "optional-secret" {
		t.Fatalf("envFrom[2].secretRef.name = %q, want optional-secret", got)
	}
	if got := digBool(deployment, "spec", "template", "spec", "containers", "0", "envFrom", "2", "secretRef", "optional"); !got {
		t.Fatalf("envFrom[2].secretRef.optional = false, want true")
	}
}

func TestBuildGeneratesWorkloadIdentityServiceAccountAndToken(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
workload_identity:
  service_account:
    create: true
  tokens:
    - name: simple-config
      audience: simple-config-server
pod_info:
  enabled: true
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ServiceAccounts) != 1 {
		t.Fatalf("len(serviceAccounts) = %d, want 1", len(result.ServiceAccounts))
	}

	serviceAccount := loadYAML(t, filepath.Join(target, "deployments", "api-serviceaccount.yml"))
	if got := digString(serviceAccount, "kind"); got != "ServiceAccount" {
		t.Fatalf("service account kind = %q, want ServiceAccount", got)
	}
	if got := digString(serviceAccount, "metadata", "name"); got != "api" {
		t.Fatalf("service account metadata.name = %q, want api", got)
	}
	if got := digString(serviceAccount, "metadata", "namespace"); got != "nac-test" {
		t.Fatalf("service account metadata.namespace = %q, want nac-test", got)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "serviceAccountName"); got != "api" {
		t.Fatalf("serviceAccountName = %q, want api", got)
	}
	if got := digBool(deployment, "spec", "template", "spec", "automountServiceAccountToken"); got {
		t.Fatalf("automountServiceAccountToken = true, want false")
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "0", "projected", "sources", "0", "serviceAccountToken", "audience"); got != "simple-config-server" {
		t.Fatalf("token audience = %q, want simple-config-server", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "0", "projected", "sources", "0", "serviceAccountToken", "path"); got != "token" {
		t.Fatalf("token path = %q, want token", got)
	}
	if got := digInt(deployment, "spec", "template", "spec", "volumes", "0", "projected", "sources", "0", "serviceAccountToken", "expirationSeconds"); got != 3600 {
		t.Fatalf("token expirationSeconds = %d, want 3600", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "mountPath"); got != "/var/run/secrets/workload-identity/simple-config" {
		t.Fatalf("token mountPath = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "1", "downwardAPI", "items", "0", "fieldRef", "fieldPath"); got != "metadata.namespace" {
		t.Fatalf("podinfo namespace fieldPath = %q, want metadata.namespace", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "1", "mountPath"); got != "/etc/podinfo" {
		t.Fatalf("podinfo mountPath = %q, want /etc/podinfo", got)
	}
}

func TestBuildGeneratesDownwardAPIMount(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
downward_api:
  mounts:
    - name: runtime-info
      mount_path: /etc/runtime-info
      items:
        - path: namespace
          field_path: metadata.namespace
        - path: pod_name
          field_path: metadata.name
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "volumes", "0", "name"); got != "runtime-info" {
		t.Fatalf("downwardAPI volume name = %q, want runtime-info", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "0", "downwardAPI", "items", "1", "fieldRef", "fieldPath"); got != "metadata.name" {
		t.Fatalf("downwardAPI pod_name fieldPath = %q, want metadata.name", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "mountPath"); got != "/etc/runtime-info" {
		t.Fatalf("downwardAPI mountPath = %q, want /etc/runtime-info", got)
	}
	if got := digBool(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "readOnly"); !got {
		t.Fatalf("downwardAPI mount readOnly = false, want true")
	}
}

func TestBuildGeneratesSidecarsAndSharedProcessNamespace(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
pod:
  share_process_namespace: true
workload_identity:
  service_account:
    create: true
  tokens:
    - name: simple-config
      audience: simple-config-server
sidecars:
  - name: simple-config-token-proxy
    image: "{{TSM_REGISTRY_URL}}/simple-config-token-proxy:{{TSM_RELEASE_ID}}"
    startup:
      command:
        - simple-config-token-proxy
      arguments:
        - --listen
        - 127.0.0.1:9999
        - --upstream
        - https://config.example.test/simple-config-server
        - --token-file
        - /var/run/secrets/workload-identity/simple-config/token
    resources:
      cpu:
        from: "10m"
        to: "100m"
      memory:
        from: "32Mi"
        to: "128Mi"
  - name: cgroup-runtime-exporter
    image: "{{TSM_REGISTRY_URL}}/cgroup-runtime-exporter:{{TSM_RELEASE_ID}}"
    envs:
      - name: TARGET_PID
        value: "1"
    resources:
      cpu:
        from: "5m"
        to: "50m"
      memory:
        from: "16Mi"
        to: "64Mi"
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    envs:
      - name: SPRING_CLOUD_CONFIG_URI
        value: http://127.0.0.1:9999
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digBool(deployment, "spec", "template", "spec", "shareProcessNamespace"); !got {
		t.Fatalf("shareProcessNamespace = false, want true")
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "1", "name"); got != "simple-config-token-proxy" {
		t.Fatalf("sidecar[0].name = %q, want simple-config-token-proxy", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "1", "command", "0"); got != "simple-config-token-proxy" {
		t.Fatalf("sidecar command[0] = %q, want simple-config-token-proxy", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "1", "args", "1"); got != "127.0.0.1:9999" {
		t.Fatalf("sidecar args[1] = %q, want 127.0.0.1:9999", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "1", "volumeMounts", "0", "mountPath"); got != "/var/run/secrets/workload-identity/simple-config" {
		t.Fatalf("sidecar token mountPath = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "2", "name"); got != "cgroup-runtime-exporter" {
		t.Fatalf("sidecar[1].name = %q, want cgroup-runtime-exporter", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "2", "env", "0", "name"); got != "TARGET_PID" {
		t.Fatalf("cgroup env[0].name = %q, want TARGET_PID", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "2", "env", "0", "value"); got != "1" {
		t.Fatalf("cgroup TARGET_PID = %q, want 1", got)
	}
}

func TestBuildReleaseManifestDefaultDoesNotOverrideSidecarImage(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	manifestPath := filepath.Join(root, "release.yml")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, manifestPath, `
images:
  - app_name: api
    image: registry.release/api
    tag: "2.0.0"
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
sidecars:
  - name: helper
    image: registry.local/helper:1.0.0
    resources:
      cpu: {from: "10m", to: "50m"}
      memory: {from: "16Mi", to: "64Mi"}
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, ReleaseManifest: manifestPath})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "registry.release/api:2.0.0" {
		t.Fatalf("app image = %q, want registry.release/api:2.0.0", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "1", "image"); got != "registry.local/helper:1.0.0" {
		t.Fatalf("sidecar image = %q, want registry.local/helper:1.0.0", got)
	}
}

func TestBuildReleaseManifestWildcardOverridesSharedSidecar(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	manifestPath := filepath.Join(root, "release.yml")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "nac-test"},
	})
	writeFile(t, manifestPath, `
images:
  - app_name: api
    container_name: api
    image: registry.release/api
    tag: "2.4"
  - app_name: worker
    container_name: worker
    image: registry.release/worker
    tag: "2.4"
  - app_name: "*"
    container_name: cgroup-runtime-exporter
    image: registry.release/cgroup-runtime-exporter
    tag: "2.4"
  - app_name: worker
    container_name: cgroup-runtime-exporter
    image: registry.release/worker-cgroup-runtime-exporter
    tag: "2.4.1"
`)
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
sidecar_definitions:
  - name: cgroup-runtime-exporter
    image: registry.local/cgroup-runtime-exporter:latest
    resources:
      cpu: {from: "10m", to: "50m"}
      memory: {from: "16Mi", to: "64Mi"}
`)
	for _, appName := range []string{"api", "worker"} {
		writeFile(t, filepath.Join(envDir, "apps", appName+".yml"), fmt.Sprintf(`
name: %s
sidecar_ref_names:
  - cgroup-runtime-exporter
containers:
  - name: %s
    image: registry.local/%s:latest
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`, appName, appName, appName))
	}

	_, err := Build(Options{
		Environment:     "test",
		Root:            root,
		Target:          target,
		ReleaseManifest: manifestPath,
		ImagePolicy:     "strict",
	})
	if err != nil {
		t.Fatal(err)
	}

	api := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(api, "spec", "template", "spec", "containers", "1", "image"); got != "registry.release/cgroup-runtime-exporter:2.4" {
		t.Fatalf("api sidecar image = %q, want wildcard image", got)
	}
	worker := loadYAML(t, filepath.Join(target, "deployments", "worker-deployment.yml"))
	if got := digString(worker, "spec", "template", "spec", "containers", "1", "image"); got != "registry.release/worker-cgroup-runtime-exporter:2.4.1" {
		t.Fatalf("worker sidecar image = %q, want exact override", got)
	}
}

func TestBuildReleaseManifestProvidesReleaseIDBuildVariable(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	manifestPath := filepath.Join(root, "release.yml")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "nac-test"},
	})
	writeFile(t, manifestPath, `
release_id: "2.4"
images:
  - app_name: api
    container_name: api
    image: registry.release/api
    tag: "2.4"
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
labels:
  app.kubernetes.io/version: "{{env:RELEASE_ID}}"
containers:
  - name: api
    image: registry.local/api:latest
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, ReleaseManifest: manifestPath, ReleaseID: "TSM-Core_RE61_SP_04.01"})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "labels", "app.kubernetes.io/version"); got != "TSM-Core_RE61_SP_04.01" {
		t.Fatalf("version label = %q, want ReleaseID", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "registry.release/api:2.4" {
		t.Fatalf("image = %q, want registry.release/api:2.4", got)
	}
	if got := digString(deployment, "metadata", "labels", releaseIDLabel); got != "TSM-Core_RE61_SP_04.01" {
		t.Fatalf("release ID label = %q", got)
	}
	if got := digString(deployment, "spec", "template", "metadata", "labels", releaseIDLabel); got != "" {
		t.Fatalf("Pod template release ID = %q, want absent", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "env", "0", "valueFrom", "configMapKeyRef", "name"); got != "release-context" {
		t.Fatalf("release context ref = %q", got)
	}
	context := loadYAML(t, filepath.Join(target, "assets", "shared", "release-context.yml"))
	if got := digString(context, "data", "RELEASE_ID"); got != "TSM-Core_RE61_SP_04.01" {
		t.Fatalf("context release ID = %q", got)
	}
	if got := digString(deployment, "spec", "selector", "matchLabels", releaseIDLabel); got != "" {
		t.Fatalf("release ID must not be added to selector: %q", got)
	}
	_, err = Build(Options{Environment: "test", Root: root, Target: target, ReleaseManifest: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	deployment = loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "labels", releaseIDLabel); got != "2.4" {
		t.Fatalf("dev release ID label = %q", got)
	}
	context = loadYAML(t, filepath.Join(target, "assets", "shared", "release-context.yml"))
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
labels:
  app.kubernetes.io/release-id: different-release
containers:
  - name: api
    image: registry.local/api:latest
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
	_, err = Build(Options{Environment: "test", Root: root, Target: target, ReleaseManifest: manifestPath, ReleaseID: "TSM-Core_RE61_SP_04.01"})
	if err == nil || !strings.Contains(err.Error(), "conflicts with computed release ID") {
		t.Fatalf("conflicting release label should fail, got %v", err)
	}
}

func TestReleaseContextPatchChangesOnlyAffectedPodTemplate(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{"environment": map[string]any{"NAMESPACE": "nac-test"}})
	for _, app := range []string{"api", "worker"} {
		writeFile(t, filepath.Join(envDir, "apps", app+".yml"), "name: "+app+"\ncontainers:\n  - name: "+app+"\n    image: registry.local/"+app+":latest\n    resources:\n      cpu: {from: \"100m\", to: \"200m\"}\n      memory: {from: \"128Mi\", to: \"256Mi\"}\n")
	}
	first := filepath.Join(root, "first.yml")
	second := filepath.Join(root, "second.yml")
	writeFile(t, first, "release_id: 2026.09.11.01\nimages:\n  - app_name: api\n    container_name: api\n    image: registry.release/api\n    tag: v1\n  - app_name: worker\n    container_name: worker\n    image: registry.release/worker\n    tag: v1\n")
	writeFile(t, second, "release_id: 2026.09.11.02\nimages:\n  - app_name: api\n    container_name: api\n    image: registry.release/api\n    tag: v2\n  - app_name: worker\n    container_name: worker\n    image: registry.release/worker\n    tag: v1\n")
	oldTarget, newTarget := filepath.Join(root, "old"), filepath.Join(root, "new")
	for _, build := range []struct{ manifest, customer, target string }{{first, "RE_2026.09.11.01", oldTarget}, {second, "RE_2026.09.11.01.patch1", newTarget}} {
		if _, err := Build(Options{Environment: "test", Root: root, Target: build.target, ReleaseManifest: build.manifest, ReleaseID: build.customer, SyncProfile: "kube-deploy-sync"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, app := range []string{"api", "worker"} {
		oldDeployment := loadYAML(t, filepath.Join(oldTarget, "deployments", app+"-deployment.yml"))
		newDeployment := loadYAML(t, filepath.Join(newTarget, "deployments", app+"-deployment.yml"))
		oldTemplate := oldDeployment["spec"].(map[string]any)["template"]
		newTemplate := newDeployment["spec"].(map[string]any)["template"]
		if equal := reflect.DeepEqual(oldTemplate, newTemplate); equal != (app == "worker") {
			t.Fatalf("%s Pod template equal = %v", app, equal)
		}
	}
	oldContext := loadYAML(t, filepath.Join(oldTarget, "assets", "shared", "release-context.yml"))
	newContext := loadYAML(t, filepath.Join(newTarget, "assets", "shared", "release-context.yml"))
	if reflect.DeepEqual(oldContext["data"], newContext["data"]) {
		t.Fatal("release context data did not change")
	}
	if got := digString(newContext, "metadata", "annotations", "kube-build-app.io/sync-order"); got != "100" {
		t.Fatalf("release context sync order = %q", got)
	}
	devTarget := filepath.Join(root, "dev")
	if _, err := Build(Options{Environment: "test", Root: root, Target: devTarget, ReleaseManifest: first, SyncProfile: "kube-deploy-sync"}); err != nil {
		t.Fatal(err)
	}
	devDeployment := loadYAML(t, filepath.Join(devTarget, "deployments", "worker-deployment.yml"))
	oldDeployment := loadYAML(t, filepath.Join(oldTarget, "deployments", "worker-deployment.yml"))
	if !reflect.DeepEqual(devDeployment["spec"].(map[string]any)["template"], oldDeployment["spec"].(map[string]any)["template"]) {
		t.Fatal("first customer release ID changed worker Pod template")
	}
	contextPath := filepath.Join(devTarget, "assets", "shared", "release-context.yml")
	before, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Build(Options{Environment: "test", Root: root, Target: devTarget, ReleaseManifest: first, SyncProfile: "kube-deploy-sync"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("repeated build changed release context ConfigMap")
	}
}

func TestReleaseContextNameFromDefaultsAndCLIOverride(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{"environment": map[string]any{"NAMESPACE": "nac-test"}})
	writeFile(t, filepath.Join(root, "release.yml"), "release_id: 2.4\n")
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), "release_context_name: release-context-test\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `name: api
containers:
  - name: api
    image: registry.local/api:latest
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
	opts := Options{Environment: "test", Root: root, Target: filepath.Join(root, "from-defaults"), ReleaseManifest: filepath.Join(root, "release.yml")}
	if _, err := Build(opts); err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(opts.Target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "env", "0", "valueFrom", "configMapKeyRef", "name"); got != "release-context-test" {
		t.Fatalf("defaults release context ref = %q", got)
	}
	loadYAML(t, filepath.Join(opts.Target, "assets", "shared", "release-context-test.yml"))

	opts.Target = filepath.Join(root, "from-cli")
	opts.ReleaseContextName = "release-context-cli"
	if _, err := Build(opts); err != nil {
		t.Fatal(err)
	}
	loadYAML(t, filepath.Join(opts.Target, "assets", "shared", "release-context-cli.yml"))
	if _, err := os.Stat(filepath.Join(opts.Target, "assets", "shared", "release-context-test.yml")); !os.IsNotExist(err) {
		t.Fatalf("defaults ConfigMap generated despite CLI override: %v", err)
	}
}

func TestReleaseContextOptionsAndManualEnvConflict(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	target := filepath.Join(root, "out")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{"environment": map[string]any{"NAMESPACE": "nac-test"}})
	writeFile(t, filepath.Join(root, "release.yml"), "release_id: 2.4\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `name: api
containers:
  - name: api
    image: registry.local/api:latest
    envs:
      - name: RELEASE_ID
        field_path: metadata.labels['app.kubernetes.io/release-id']
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
	opts := Options{Environment: "test", Root: root, Target: target, ReleaseManifest: filepath.Join(root, "release.yml")}
	if _, err := Build(opts); err == nil || !strings.Contains(err.Error(), `ENV "RELEASE_ID" conflicts`) {
		t.Fatalf("ENV conflict = %v", err)
	}
	opts.ReleaseContextName = "custom-context"
	opts.ReleaseIDEnvName = "APP_RELEASE_ID"
	opts.DisableReleaseContextConfigMap = true
	if _, err := Build(opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "assets", "shared", "custom-context.yml")); !os.IsNotExist(err) {
		t.Fatalf("unexpected generated ConfigMap: %v", err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "env", "1", "name"); got != "APP_RELEASE_ID" {
		t.Fatalf("custom ENV = %q", got)
	}
	opts.DisableReleaseContextEnv = true
	opts.DisableReleaseContextConfigMap = false
	otherTarget := filepath.Join(root, "other")
	opts.Target = otherTarget
	if _, err := Build(opts); err != nil {
		t.Fatal(err)
	}
	deployment = loadYAML(t, filepath.Join(otherTarget, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "env", "1", "name"); got != "" {
		t.Fatalf("unexpected injected ENV = %q", got)
	}
}

func TestReleaseIDLabelsRejectInvalidValues(t *testing.T) {
	for _, value := range []string{"a/b", "a_", strings.Repeat("a", 64)} {
		if err := validateReleaseLabelValue("release manifest ID", value); err == nil {
			t.Fatalf("accepted invalid release ID %q", value)
		}
	}
	for _, value := range []string{"2026.09.10.02", "TSM-Core_RE42_SP02"} {
		if err := validateReleaseLabelValue("release manifest ID", value); err != nil {
			t.Fatalf("rejected valid release ID %q: %v", value, err)
		}
	}
}

func TestBuildReleaseManifestAcceptsMatchingExternalReleaseID(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	manifestPath := filepath.Join(root, "release.yml")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":  "nac-test",
			"RELEASE_ID": "2.4",
		},
	})
	writeFile(t, manifestPath, `release_id: "2.4"`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
labels:
  app.kubernetes.io/version: "{{env:RELEASE_ID}}"
containers:
  - name: api
    image: registry.local/api:latest
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	if _, err := Build(Options{Environment: "test", Root: root, Target: target, ReleaseManifest: manifestPath}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildReleaseManifestRejectsConflictingExternalReleaseID(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	manifestPath := filepath.Join(root, "release.yml")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":  "nac-test",
			"RELEASE_ID": "2.3",
		},
	})
	writeFile(t, manifestPath, `release_id: "2.4"`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
containers:
  - name: api
    image: registry.local/api:latest
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, ReleaseManifest: manifestPath})
	if err == nil || !strings.Contains(err.Error(), `release manifest release_id "2.4" conflicts with build variable RELEASE_ID "2.3"`) {
		t.Fatalf("err = %v, want RELEASE_ID conflict", err)
	}
}

func TestLoadReleaseManifestAcceptsOCIToolboxMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.yml")
	writeFile(t, path, `
release_id: RE_2026.07.28.01
created_at: 2026-07-28T18:00:00Z
bundle:
  name: stable
  revision: abc123
registry_base: registry.example.com/release
platform: linux/amd64
images:
  - id: api
    app_name: api
    container_name: application
    source:
      image: registry-source.example.com/team/api
      tag: build-1
      digest: sha256:source
    image: registry.example.com/release/api
    tag: RE_2026.07.28.01
    digest: sha256:target
    extra_tags: [stable]
    platform: linux/amd64
extra_tags: [stable]
`)

	manifest, err := loadReleaseManifest(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Bundle == nil || manifest.Bundle.Name != "stable" || manifest.Bundle.Revision != "abc123" {
		t.Fatalf("bundle = %#v", manifest.Bundle)
	}
	if manifest.Platform != "linux/amd64" || manifest.Images[0].Platform != "linux/amd64" {
		t.Fatalf("platform = %q, image platform = %q", manifest.Platform, manifest.Images[0].Platform)
	}
	if manifest.Images[0].Source == nil || manifest.Images[0].Source.Digest != "sha256:source" {
		t.Fatalf("source = %#v", manifest.Images[0].Source)
	}
	got, err := manifest.imageFor("api", "application", releaseImageSelection{ReferenceMode: "auto"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "registry.example.com/release/api@sha256:target" {
		t.Fatalf("image = %q", got)
	}
	got, err = manifest.imageFor("api", "application", releaseImageSelection{ReferenceMode: "tag"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "registry.example.com/release/api:RE_2026.07.28.01" {
		t.Fatalf("tag image = %q", got)
	}
}

func TestReleaseImageReferenceModes(t *testing.T) {
	image := &releaseImage{
		Image:  "registry.example.com/release/api",
		Tag:    "release-1",
		Digest: "sha256:target",
	}
	for _, test := range []struct {
		mode string
		want string
	}{
		{mode: "auto", want: "registry.example.com/release/api@sha256:target"},
		{mode: "digest", want: "registry.example.com/release/api@sha256:target"},
		{mode: "tag", want: "registry.example.com/release/api:release-1"},
	} {
		got, found, err := releaseImageRef(image, releaseImageSelection{ReferenceMode: test.mode})
		if err != nil {
			t.Fatalf("mode %s: %v", test.mode, err)
		}
		if !found || got != test.want {
			t.Fatalf("mode %s = %q, %v; want %q, true", test.mode, got, found, test.want)
		}
	}

	withoutTag := *image
	withoutTag.Tag = ""
	if _, _, err := releaseImageRef(&withoutTag, releaseImageSelection{ReferenceMode: "tag"}); err == nil || !strings.Contains(err.Error(), "has no tag") {
		t.Fatalf("missing tag error = %v", err)
	}
	withoutDigest := *image
	withoutDigest.Digest = ""
	if _, _, err := releaseImageRef(&withoutDigest, releaseImageSelection{ReferenceMode: "digest"}); err == nil || !strings.Contains(err.Error(), "has no digest") {
		t.Fatalf("missing digest error = %v", err)
	}
}

func TestApplyImageOverridesRejectsInvalidImageReference(t *testing.T) {
	err := applyImageOverrides(nil, Options{ImageReference: "sha"})
	if err == nil || !strings.Contains(err.Error(), "expected auto, digest, or tag") {
		t.Fatalf("error = %v", err)
	}
}

func TestReleaseImageForceTagAndPrefix(t *testing.T) {
	image := &releaseImage{
		Image:  "harbor.example.com/old-project/team/api",
		Tag:    "original",
		Digest: "sha256:target",
	}
	got, found, err := releaseImageRef(image, releaseImageSelection{
		ReferenceMode: "auto",
		ForceTag:      "emergency-1",
		ForcePrefix:   "artifactory.example.com/docker-release",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "artifactory.example.com/docker-release/api:emergency-1" {
		t.Fatalf("image = %q, %v", got, found)
	}

	got, found, err = releaseImageRef(image, releaseImageSelection{
		ReferenceMode: "digest",
		ForcePrefix:   "artifactory.example.com:5000/docker-release",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "artifactory.example.com:5000/docker-release/api@sha256:target" {
		t.Fatalf("digest image = %q, %v", got, found)
	}
}

func TestReleaseImageForceOptionsValidation(t *testing.T) {
	for _, test := range []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "requires release manifest",
			opts: Options{ForceImageTag: "emergency-1"},
			want: "require --release-manifest",
		},
		{
			name: "tag conflicts with digest mode",
			opts: Options{ReleaseManifest: "release.yml", ImageReference: "digest", ForceImageTag: "emergency-1"},
			want: "cannot be combined",
		},
		{
			name: "invalid tag",
			opts: Options{ReleaseManifest: "release.yml", ForceImageTag: "bad/tag"},
			want: "invalid --force-image-tag",
		},
		{
			name: "prefix rejects URL scheme",
			opts: Options{ReleaseManifest: "release.yml", ForceImagePrefix: "https://registry.example.com/release"},
			want: "without a URL scheme",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := releaseImageSelectionFromOptions(test.opts)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadReleaseManifestRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.yml")
	writeFile(t, path, `
release_id: release-1
unexpected: true
`)

	_, err := loadReleaseManifest(path, Options{})
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadReleaseManifestRejectsDuplicateSelector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.yml")
	writeFile(t, path, `
images:
  - id: api
    app_name: api
    container_name: application
    image: registry.example.com/release/api
    digest: sha256:first
  - id: api-copy
    app_name: api
    container_name: application
    image: registry.example.com/release/api-copy
    digest: sha256:second
`)

	_, err := loadReleaseManifest(path, Options{})
	if err == nil || !strings.Contains(err.Error(), `duplicate image selector "api"/"application"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadReleaseManifestRejectsConflictingPlatform(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release.yml")
	writeFile(t, path, `
platform: linux/amd64
images:
  - app_name: api
    container_name: api
    image: registry.example.com/release/api
    digest: sha256:target
    platform: linux/arm64
`)

	_, err := loadReleaseManifest(path, Options{})
	if err == nil || !strings.Contains(err.Error(), `platform "linux/arm64" conflicts with manifest platform "linux/amd64"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildGeneratesGenericInitContainers(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
init_containers:
  - name: migrate
    image: "{{TSM_REGISTRY_URL}}/api-migrate:{{TSM_RELEASE_ID}}"
    command: ["/bin/sh", "-c"]
    arguments: ["./migrate.sh"]
    envs:
      - name: LOG_LEVEL
        value: INFO
    env_from:
      - config_map: api-config
    mounts:
      - type: empty_dir
        name: work
        mount_path: /work
    security_context:
      runAsNonRoot: true
    resources:
      cpu:
        from: "50m"
        to: "100m"
      memory:
        from: "64Mi"
        to: "128Mi"
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    mounts:
      - type: empty_dir
        name: work
        mount_path: /work
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "name"); got != "migrate" {
		t.Fatalf("init container name = %q, want migrate", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "image"); got != "{{TSM_REGISTRY_URL}}/api-migrate:{{TSM_RELEASE_ID}}" {
		t.Fatalf("init container image = %q, want runtime placeholder image", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "args", "0"); got != "./migrate.sh" {
		t.Fatalf("init container args[0] = %q, want ./migrate.sh", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "env", "0", "name"); got != "LOG_LEVEL" {
		t.Fatalf("init container env[0].name = %q, want LOG_LEVEL", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "envFrom", "0", "configMapRef", "name"); got != "api-config" {
		t.Fatalf("init container envFrom[0].configMapRef.name = %q, want api-config", got)
	}
	if got := digBool(deployment, "spec", "template", "spec", "initContainers", "0", "securityContext", "runAsNonRoot"); !got {
		t.Fatalf("init container runAsNonRoot = false, want true")
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "volumeMounts", "0", "mountPath"); got != "/work" {
		t.Fatalf("init container volumeMount mountPath = %q, want /work", got)
	}
	if emptyDir := digAny(deployment, "spec", "template", "spec", "volumes", "0", "emptyDir"); emptyDir == nil {
		t.Fatal("expected emptyDir volume for init/container shared mount")
	}
}

func TestBuildAcceptsRequestsLimitsResourceAliases(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 2
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu:
        requests: "250m"
        limits: "750m"
      memory:
        requests: "384Mi"
        limits: "768Mi"
      ephemeral-storage:
        requests: "1Gi"
        limits: "2Gi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "resources", "requests", "cpu"); got != "250m" {
		t.Fatalf("cpu request = %q, want 250m", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "resources", "limits", "memory"); got != "768Mi" {
		t.Fatalf("memory limit = %q, want 768Mi", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "resources", "requests", "ephemeral-storage"); got != "1Gi" {
		t.Fatalf("ephemeral-storage request = %q, want 1Gi", got)
	}

	summary, err := ResourceSummary(Options{Environment: "test", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Totals.CPURequestCores != 0.5 {
		t.Fatalf("cpu request total = %v, want 0.5", summary.Totals.CPURequestCores)
	}
	if summary.Totals.MemoryLimitMiB != 1536 {
		t.Fatalf("memory limit total = %v, want 1536", summary.Totals.MemoryLimitMiB)
	}
}

func TestBuildGeneratesJavaRuntimeEnv(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 2
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    runtime:
      java:
        xms: "512m"
        xmx: "1536m"
        opts:
          - "-XX:+UseG1GC"
        export:
          env_name: APP_JAVA_OPTS
    resources:
      cpu: {requests: "250m", limits: "1"}
      memory: {requests: "512Mi", limits: "2048Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	env := digSlice(deployment, "spec", "template", "spec", "containers", "0", "env")
	if got := envValue(env, "APP_JAVA_OPTS"); got != "-Xms512m -Xmx1536m -XX:+UseG1GC" {
		t.Fatalf("APP_JAVA_OPTS = %q, want composed JVM opts", got)
	}

	summary, err := ResourceSummary(Options{Environment: "test", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := summary.Items[0].JavaXms; got != "512m" {
		t.Fatalf("summary java_xms = %q, want 512m", got)
	}
	if got := summary.Items[0].JavaXmx; got != "1536m" {
		t.Fatalf("summary java_xmx = %q, want 1536m", got)
	}
}

func TestBuildAppliesDeploymentAndPodRaw(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
deployment_raw:
  spec:
    revisionHistoryLimit: 2
pod_raw:
  dnsPolicy: ClusterFirst
  enableServiceLinks: false
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digInt(deployment, "spec", "revisionHistoryLimit"); got != 2 {
		t.Fatalf("revisionHistoryLimit = %d, want 2", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "dnsPolicy"); got != "ClusterFirst" {
		t.Fatalf("dnsPolicy = %q, want ClusterFirst", got)
	}
	if got := digBool(deployment, "spec", "template", "spec", "enableServiceLinks"); got {
		t.Fatal("enableServiceLinks = true, want false")
	}
	if got := digInt(deployment, "spec", "replicas"); got != 1 {
		t.Fatalf("replicas = %d, want 1", got)
	}
}

func TestBuildGeneratesModernSchedulingFields(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 2
scheduling:
  arch: amd64
  node_selector:
    workload: tsm
  tolerations:
    - key: dedicated
      operator: Equal
      value: tsm
      effect: NoSchedule
  spread:
    by: hostname
    max_skew: 1
    when_unsatisfiable: ScheduleAnyway
  anti_affinity:
    self: preferred
    topology: kubernetes.io/hostname
  affinity:
    nodeAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 10
          preference:
            matchExpressions:
              - key: disk
                operator: In
                values: [ssd]
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "nodeSelector", "kubernetes.io/arch"); got != "amd64" {
		t.Fatalf("arch nodeSelector = %q, want amd64", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "nodeSelector", "workload"); got != "tsm" {
		t.Fatalf("workload nodeSelector = %q, want tsm", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "topologySpreadConstraints", "0", "topologyKey"); got != "kubernetes.io/hostname" {
		t.Fatalf("topologyKey = %q, want kubernetes.io/hostname", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "affinity", "podAntiAffinity", "preferredDuringSchedulingIgnoredDuringExecution", "0", "podAffinityTerm", "topologyKey"); got != "kubernetes.io/hostname" {
		t.Fatalf("podAntiAffinity topologyKey = %q, want kubernetes.io/hostname", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "affinity", "nodeAffinity", "preferredDuringSchedulingIgnoredDuringExecution", "0", "preference", "matchExpressions", "0", "key"); got != "disk" {
		t.Fatalf("nodeAffinity match key = %q, want disk", got)
	}
}

func TestBuildGeneratesRegistryDNSAndAnnotations(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
annotations:
  argocd.argoproj.io/sync-wave: "2"
pod_annotations:
  checksum/config: abc123
registry:
  - secret_name: regcred
dns:
  - ip: 10.0.0.10
    hostnames:
      - legacy.local
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "annotations", "argocd.argoproj.io/sync-wave"); got != "2" {
		t.Fatalf("sync-wave annotation = %q, want 2", got)
	}
	if got := digString(deployment, "spec", "template", "metadata", "annotations", "checksum/config"); got != "abc123" {
		t.Fatalf("pod checksum annotation = %q, want abc123", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "imagePullSecrets", "0", "name"); got != "regcred" {
		t.Fatalf("imagePullSecret = %q, want regcred", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "hostAliases", "0", "ip"); got != "10.0.0.10" {
		t.Fatalf("hostAlias ip = %q, want 10.0.0.10", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "hostAliases", "0", "hostnames", "0"); got != "legacy.local" {
		t.Fatalf("hostAlias hostname = %q, want legacy.local", got)
	}
}

func TestBuildGeneratesToolsInitContainersAndCgroupDefaults(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
tools:
  - name: util-encjson-rs
    image: toolbox:1
    expose_bin: /usr/bin/encjson-rs
    mount_path: /usr/local/bin/encjson
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    enable_cgroup_exporter: true
    envs:
      - name: CGROUP_EXPORTER_LISTEN
        value: "127.0.0.1:9393"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "name"); got != "util-encjson-rs" {
		t.Fatalf("tool initContainer name = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "mountPath"); got != "/usr/local/bin/encjson" {
		t.Fatalf("tools mountPath = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "subPath"); got != "usr/local/bin/encjson" {
		t.Fatalf("tools subPath = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "imagePullPolicy"); got != "Always" {
		t.Fatalf("tool imagePullPolicy = %q, want Always", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "resources", "requests", "cpu"); got != "10m" {
		t.Fatalf("tool CPU request = %q, want 10m", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "resources", "limits", "cpu"); got != "100m" {
		t.Fatalf("tool CPU limit = %q, want 100m", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "resources", "requests", "memory"); got != "16Mi" {
		t.Fatalf("tool memory request = %q, want 16Mi", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "initContainers", "0", "resources", "limits", "memory"); got != "128Mi" {
		t.Fatalf("tool memory limit = %q, want 128Mi", got)
	}
	env := digSlice(deployment, "spec", "template", "spec", "containers", "0", "env")
	if got := envValue(env, "CGROUP_EXPORTER_LISTEN"); got != "127.0.0.1:9393" {
		t.Fatalf("CGROUP_EXPORTER_LISTEN = %q", got)
	}
	if got := envFieldPath(env, "CGROUP_EXPORTER_NODE_NAME"); got != "spec.nodeName" {
		t.Fatalf("CGROUP_EXPORTER_NODE_NAME fieldPath = %q", got)
	}
}

func TestToolResourcesMergeDefaultsAndOverrides(t *testing.T) {
	resources := toolResources(map[string]map[string]any{
		"cpu": {
			"limits": "250m",
		},
		"memory": {
			"requests": "32Mi",
		},
	})
	if got := fmt.Sprint(resources["cpu"]["requests"]); got != "10m" {
		t.Fatalf("CPU request = %q, want default 10m", got)
	}
	if got := fmt.Sprint(resources["cpu"]["limits"]); got != "250m" {
		t.Fatalf("CPU limit = %q, want override 250m", got)
	}
	if got := fmt.Sprint(resources["memory"]["requests"]); got != "32Mi" {
		t.Fatalf("memory request = %q, want override 32Mi", got)
	}
	if got := fmt.Sprint(resources["memory"]["limits"]); got != "128Mi" {
		t.Fatalf("memory limit = %q, want default 128Mi", got)
	}
}

func TestValidateRejectsStartupAndSimpleInitTogether(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "invalid.yml"), `
name: invalid
replicas: 1
containers:
  - name: invalid
    image: "{{TSM_REGISTRY_URL}}/invalid:{{TSM_RELEASE_ID}}"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    simple_init:
      enabled: true
      exec:
        command: ["/app/start.sh"]
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	err := Validate(Options{Environment: "test", Root: root})
	if err == nil {
		t.Fatal("expected simple_init/startup validation error")
	}
	if !strings.Contains(err.Error(), "XOR violation") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestBuildGeneratesAutoscalingHPA(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 2
labels:
  tier: backend
annotations:
  argocd.argoproj.io/sync-wave: "20"
autoscaling:
  enabled: true
  min_replicas: 2
  max_replicas: 6
  cpu:
    average_utilization: 75
  memory:
    average_utilization: 80
  raw:
    spec:
      behavior:
        scaleDown:
          stabilizationWindowSeconds: 300
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu: {requests: "100m", limits: "500m"}
      memory: {requests: "128Mi", limits: "512Mi"}
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target, SyncProfile: "kube-deploy-sync"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Autoscaling) != 1 {
		t.Fatalf("len(autoscaling) = %d, want 1", len(result.Autoscaling))
	}

	hpa := loadYAML(t, filepath.Join(target, "deployments", "api-hpa.yml"))
	assertSyncMetadata(t, hpa, "test/HorizontalPodAutoscaler/nac-test/api", "210", "kube-deploy-sync")
	if got := digString(hpa, "apiVersion"); got != "autoscaling/v2" {
		t.Fatalf("apiVersion = %q, want autoscaling/v2", got)
	}
	if got := digString(hpa, "kind"); got != "HorizontalPodAutoscaler" {
		t.Fatalf("kind = %q, want HorizontalPodAutoscaler", got)
	}
	if got := digString(hpa, "metadata", "name"); got != "api" {
		t.Fatalf("metadata.name = %q, want api", got)
	}
	if got := digString(hpa, "metadata", "namespace"); got != "nac-test" {
		t.Fatalf("metadata.namespace = %q, want nac-test", got)
	}
	if got := digString(hpa, "metadata", "labels", "tier"); got != "backend" {
		t.Fatalf("metadata.labels.tier = %q, want backend", got)
	}
	if got := digString(hpa, "metadata", "annotations", "argocd.argoproj.io/sync-wave"); got != "20" {
		t.Fatalf("metadata annotation = %q, want 20", got)
	}
	if got := digString(hpa, "spec", "scaleTargetRef", "kind"); got != "Deployment" {
		t.Fatalf("scaleTargetRef.kind = %q, want Deployment", got)
	}
	if got := digString(hpa, "spec", "scaleTargetRef", "name"); got != "api" {
		t.Fatalf("scaleTargetRef.name = %q, want api", got)
	}
	if got := digInt(hpa, "spec", "minReplicas"); got != 2 {
		t.Fatalf("minReplicas = %d, want 2", got)
	}
	if got := digInt(hpa, "spec", "maxReplicas"); got != 6 {
		t.Fatalf("maxReplicas = %d, want 6", got)
	}
	if got := digString(hpa, "spec", "metrics", "0", "resource", "name"); got != "cpu" {
		t.Fatalf("first metric resource name = %q, want cpu", got)
	}
	if got := digInt(hpa, "spec", "metrics", "0", "resource", "target", "averageUtilization"); got != 75 {
		t.Fatalf("cpu averageUtilization = %d, want 75", got)
	}
	if got := digString(hpa, "spec", "metrics", "1", "resource", "name"); got != "memory" {
		t.Fatalf("second metric resource name = %q, want memory", got)
	}
	if got := digInt(hpa, "spec", "metrics", "1", "resource", "target", "averageUtilization"); got != 80 {
		t.Fatalf("memory averageUtilization = %d, want 80", got)
	}
	if got := digInt(hpa, "spec", "behavior", "scaleDown", "stabilizationWindowSeconds"); got != 300 {
		t.Fatalf("raw behavior scaleDown stabilizationWindowSeconds = %d, want 300", got)
	}
}

func TestValidateRejectsInvalidAutoscaling(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "invalid.yml"), `
name: invalid
replicas: 1
autoscaling:
  enabled: true
  min_replicas: 4
  max_replicas: 2
  cpu:
    average_utilization: 75
containers:
  - name: invalid
    image: "{{TSM_REGISTRY_URL}}/invalid:{{TSM_RELEASE_ID}}"
    resources:
      cpu: {requests: "100m", limits: "500m"}
      memory: {requests: "128Mi", limits: "512Mi"}
`)

	err := Validate(Options{Environment: "test", Root: root})
	if err == nil {
		t.Fatal("expected autoscaling validation error")
	}
	if !strings.Contains(err.Error(), "autoscaling.max_replicas") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestValidateRejectsJavaRuntimeEnvConflict(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "invalid.yml"), `
name: invalid
replicas: 1
containers:
  - name: invalid
    image: "{{TSM_REGISTRY_URL}}/invalid:{{TSM_RELEASE_ID}}"
    runtime:
      java:
        xms: "512m"
        xmx: "1024m"
    envs:
      - name: JAVA_OPTS
        value: "-Xmx256m"
    resources:
      cpu: {requests: "100m", limits: "500m"}
      memory: {requests: "128Mi", limits: "512Mi"}
`)

	err := Validate(Options{Environment: "test", Root: root})
	if err == nil {
		t.Fatal("expected java runtime env conflict validation error")
	}
	if !strings.Contains(err.Error(), `env "JAVA_OPTS"`) {
		t.Fatalf("validation error = %v", err)
	}
}

func TestBuildGeneratesRolloutChecksumAnnotations(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{"environment": map[string]any{"NAMESPACE": "nac-test", "TSM_REGISTRY_URL": "registry.local/tsm", "TSM_RELEASE_ID": "1.0.0"}})
	writeFile(t, filepath.Join(envDir, "env.secured.json"), `{"_public_key":"dummy","environment":{"SECRET":"abc"}}`)
	writeFile(t, filepath.Join(envDir, "assets", "config.tpl"), "url={{EPAP_URL}}\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
rollout_on:
  checksums:
    config:
      files:
        - env.unsecured.json
        - env.secured.json
    mtls:
      files:
        - assets/config.tpl
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "metadata", "annotations", "checksum/config"); got != checksumForFiles(t, envDir, "env.secured.json", "env.unsecured.json") {
		t.Fatalf("checksum/config = %q", got)
	}
	if got := digString(deployment, "spec", "template", "metadata", "annotations", "checksum/mtls"); got != checksumForFiles(t, envDir, "assets/config.tpl") {
		t.Fatalf("checksum/mtls = %q", got)
	}
}

func TestBuildRolloutChecksumMissingFileFails(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{"environment": map[string]any{"NAMESPACE": "nac-test", "TSM_REGISTRY_URL": "registry.local/tsm", "TSM_RELEASE_ID": "1.0.0"}})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
rollout_on:
  checksums:
    config:
      files: [missing.file]
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    startup: {command: ["/bin/sh"], arguments: ["-c", "echo ok"]}
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err == nil {
		t.Fatal("expected missing rollout checksum file error")
	}
}

func TestBuildRolloutChecksumConflictingPodAnnotationFails(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{"environment": map[string]any{"NAMESPACE": "nac-test", "TSM_REGISTRY_URL": "registry.local/tsm", "TSM_RELEASE_ID": "1.0.0"}})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
pod_annotations:
  checksum/config: manual
rollout_on:
  checksums:
    config:
      files: [env.unsecured.json]
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    startup: {command: ["/bin/sh"], arguments: ["-c", "echo ok"]}
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err == nil {
		t.Fatal("expected conflicting pod annotation error")
	}
}

func TestBuildSkipsIgnoredAppsAndCanDisableServiceGeneration(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "ignored.yml"), `
name: ignored
ignore: true
replicas: 1
containers:
  - name: ignored
    image: "{{TSM_REGISTRY_URL}}/ignored:{{TSM_RELEASE_ID}}"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
disable_create_service: true
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    ports:
      - name: http
        port: 8080
        expose_as:
          - hostname: api
            port: 80
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deployments) != 1 {
		t.Fatalf("len(deployments) = %d, want 1", len(result.Deployments))
	}
	if _, err := os.Stat(filepath.Join(target, "deployments", "ignored-deployment.yml")); !os.IsNotExist(err) {
		t.Fatalf("ignored deployment exists or stat error differs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "services", "api-service.yml")); !os.IsNotExist(err) {
		t.Fatalf("disabled service exists or stat error differs: %v", err)
	}
}

func TestBuildGeneratesStrategiesStatefulSetAndBudget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeMinimalApp(t, envDir, "recreate.yml", `
name: recreate
strategy: recreate
min_available: 1
`)
	writeMinimalApp(t, envDir, "one-by-one.yml", `
name: one-by-one
strategy: one-by-one
max_unavailable: 1
`)
	writeMinimalApp(t, envDir, "stateful.yml", `
name: stateful
kind: StatefulSet
subdomain_name: stateful-headless
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target, SyncProfile: "kube-deploy-sync"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Budgets) != 2 {
		t.Fatalf("len(budgets) = %d, want 2", len(result.Budgets))
	}

	recreate := loadYAML(t, filepath.Join(target, "deployments", "recreate-deployment.yml"))
	if got := digString(recreate, "spec", "strategy", "type"); got != "Recreate" {
		t.Fatalf("recreate strategy type = %q, want Recreate", got)
	}
	recreateBudget := loadYAML(t, filepath.Join(target, "deployments", "recreate-budget.yml"))
	assertSyncMetadata(t, recreateBudget, "test/PodDisruptionBudget/nac-test/recreate", "190", "kube-deploy-sync")
	if got := digInt(recreateBudget, "spec", "minAvailable"); got != 1 {
		t.Fatalf("minAvailable = %d, want 1", got)
	}

	oneByOne := loadYAML(t, filepath.Join(target, "deployments", "one-by-one-deployment.yml"))
	if got := digInt(oneByOne, "spec", "strategy", "rollingUpdate", "maxSurge"); got != 0 {
		t.Fatalf("one-by-one maxSurge = %d, want 0", got)
	}
	if got := digInt(oneByOne, "spec", "strategy", "rollingUpdate", "maxUnavailable"); got != 1 {
		t.Fatalf("one-by-one maxUnavailable = %d, want 1", got)
	}
	oneByOneBudget := loadYAML(t, filepath.Join(target, "deployments", "one-by-one-budget.yml"))
	assertSyncMetadata(t, oneByOneBudget, "test/PodDisruptionBudget/nac-test/one-by-one", "190", "kube-deploy-sync")
	if got := digInt(oneByOneBudget, "spec", "maxUnavailable"); got != 1 {
		t.Fatalf("budget maxUnavailable = %d, want 1", got)
	}

	stateful := loadYAML(t, filepath.Join(target, "deployments", "stateful-deployment.yml"))
	assertSyncMetadata(t, stateful, "test/StatefulSet/nac-test/stateful", "200", "kube-deploy-sync")
	if got := digString(stateful, "kind"); got != "StatefulSet" {
		t.Fatalf("kind = %q, want StatefulSet", got)
	}
	if got := digString(stateful, "spec", "serviceName"); got != "stateful-headless" {
		t.Fatalf("serviceName = %q, want stateful-headless", got)
	}
	if got := digString(stateful, "spec", "updateStrategy", "type"); got != "RollingUpdate" {
		t.Fatalf("updateStrategy.type = %q, want RollingUpdate", got)
	}
	if strategy := digAny(stateful, "spec", "strategy"); strategy != nil {
		t.Fatalf("stateful strategy exists: %#v", strategy)
	}
}

func TestBuildAppliesReplicaProfile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "replica-profiles.yml"), `
defaults:
  replica_profile_ref_name: maintenance
profiles:
  maintenance:
    all: 1
    apps:
      api: 2
  legacy:
    all: 3
    worker: 4
`)
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)
	writeMinimalApp(t, envDir, "worker.yml", `
name: worker
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	api := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digInt(api, "spec", "replicas"); got != 2 {
		t.Fatalf("api replicas = %d, want 2", got)
	}
	worker := loadYAML(t, filepath.Join(target, "deployments", "worker-deployment.yml"))
	if got := digInt(worker, "spec", "replicas"); got != 1 {
		t.Fatalf("worker replicas = %d, want 1", got)
	}
}

func TestBuildAppliesLegacyReplicaProfileFromOption(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "replica-profiles.yml"), `
profiles:
  legacy:
    all: 3
    worker: 4
`)
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)
	writeMinimalApp(t, envDir, "worker.yml", `
name: worker
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, Profile: "legacy"})
	if err != nil {
		t.Fatal(err)
	}

	api := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digInt(api, "spec", "replicas"); got != 3 {
		t.Fatalf("api replicas = %d, want 3", got)
	}
	worker := loadYAML(t, filepath.Join(target, "deployments", "worker-deployment.yml"))
	if got := digInt(worker, "spec", "replicas"); got != 4 {
		t.Fatalf("worker replicas = %d, want 4", got)
	}
}

func TestBuildAppliesReleaseManifestAndScaleDown(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	manifestPath := filepath.Join(root, "release.yml")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, manifestPath, `
images:
  - app_name: api
    image: registry.release/api
    tag: "2.0.0"
  - app_name: worker
    container_name: worker
    image: registry.release/worker
    digest: sha256:abcdef
`)
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)
	writeFile(t, filepath.Join(envDir, "apps", "worker.yml"), `
name: worker
replicas: 3
containers:
  - name: worker
    image: "{{TSM_REGISTRY_URL}}/worker:{{TSM_RELEASE_ID}}"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, ReleaseManifest: manifestPath, Down: []string{"worker"}})
	if err != nil {
		t.Fatal(err)
	}

	api := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(api, "spec", "template", "spec", "containers", "0", "image"); got != "registry.release/api:2.0.0" {
		t.Fatalf("api image = %q", got)
	}
	worker := loadYAML(t, filepath.Join(target, "deployments", "worker-deployment.yml"))
	if got := digString(worker, "spec", "template", "spec", "containers", "0", "image"); got != "registry.release/worker@sha256:abcdef" {
		t.Fatalf("worker image = %q", got)
	}
	if got := digInt(worker, "spec", "replicas"); got != 0 {
		t.Fatalf("worker replicas = %d, want 0", got)
	}
}

func TestBuildAppliesImageOverrideOverReleaseManifest(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	manifestPath := filepath.Join(root, "release.yml")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, manifestPath, `
images:
  - app_name: api
    container_name: api
    image: registry.release/api
    tag: "2.0.0"
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{
		Environment:      "test",
		Root:             root,
		Target:           target,
		ReleaseManifest:  manifestPath,
		ImageOverrides:   []string{"api/api=registry.cli/api@sha256:1234"},
		ForceImageTag:    "emergency-1",
		ForceImagePrefix: "artifactory.example.com/docker-release",
	})
	if err != nil {
		t.Fatal(err)
	}

	api := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(api, "spec", "template", "spec", "containers", "0", "image"); got != "registry.cli/api@sha256:1234" {
		t.Fatalf("api image = %q", got)
	}
}

func TestBuildImagePolicyStrictRequiresOverride(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, ImagePolicy: "strict"})
	if err == nil || !strings.Contains(err.Error(), "image override missing for api/app") {
		t.Fatalf("err = %v, want missing strict image override", err)
	}
}

func TestBuildImagePolicyStrictDoesNotRequireSidecarOverride(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	manifestPath := filepath.Join(root, "release.yml")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "nac-test"},
	})
	writeFile(t, manifestPath, `
images:
  - app_name: api
    container_name: api
    image: registry.release/api
    tag: "1.0.0"
`)
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
sidecar_definitions:
  - name: cgroup-runtime-exporter
    image: registry.local/cgroup-runtime-exporter:stable
    resources:
      cpu: {from: "10m", to: "50m"}
      memory: {from: "16Mi", to: "64Mi"}
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
sidecar_ref_names:
  - cgroup-runtime-exporter
containers:
  - name: api
    image: "<from release manifest>"
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{
		Environment:     "test",
		Root:            root,
		Target:          target,
		ReleaseManifest: manifestPath,
		ImagePolicy:     "strict",
	})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "registry.release/api:1.0.0" {
		t.Fatalf("primary image = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "1", "image"); got != "registry.local/cgroup-runtime-exporter:stable" {
		t.Fatalf("sidecar image = %q", got)
	}
}

func TestBuildSelectsSidecarDefinitionsAndAppliesLocalPatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "nac-test"},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
sidecar_definitions:
  - name: cgroup-runtime-exporter
    image: registry.local/cgroup-runtime-exporter:stable
    envs:
      - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
        value: '(^|/)java(\s|$)'
      - name: KEEP_ME
        value: "default"
    resources:
      cpu: {from: "10m", to: "50m"}
      memory: {from: "16Mi", to: "64Mi"}
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
sidecar_ref_names:
  - cgroup-runtime-exporter
sidecars:
  - name: cgroup-runtime-exporter
    envs:
      - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
        value: '(^|/)nginx(\s|$)'
containers:
  - name: api
    image: registry.local/api:1
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	containers := digSlice(deployment, "spec", "template", "spec", "containers")
	sidecar := namedObject(containers, "cgroup-runtime-exporter")
	if sidecar == nil {
		t.Fatalf("sidecar missing: %#v", containers)
	}
	env := digSlice(sidecar, "env")
	if got := envValue(env, "CGROUP_EXPORTER_TARGET_PID_REGEXP"); got != `(^|/)nginx(\s|$)` {
		t.Fatalf("CGROUP_EXPORTER_TARGET_PID_REGEXP = %q", got)
	}
	if got := envValue(env, "KEEP_ME"); got != "default" {
		t.Fatalf("KEEP_ME = %q", got)
	}
}

func TestBuildRejectsUnselectedSidecarDefinitionPatch(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "nac-test"},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
sidecar_definitions:
  - name: cgroup-runtime-exporter
    image: registry.local/cgroup-runtime-exporter:stable
    startup:
      command: ["/usr/local/bin/cgroup-runtime-exporter"]
    envs:
      - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
        value: '(^|/)java(\s|$)'
    resources:
      cpu: {from: "2m", to: "10m"}
      memory: {from: "8Mi", to: "32Mi"}
`)
	writeFile(t, filepath.Join(envDir, "apps", "ui.yml"), `
name: ui
sidecars:
  - name: cgroup-runtime-exporter
    envs:
      - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
        value: '(^|/)nginx(\s|$)'
containers:
  - name: ui
    image: registry.local/ui:1
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err == nil || !strings.Contains(err.Error(), `ui: sidecars "cgroup-runtime-exporter" matches sidecar_definitions but is not selected; add it to sidecar_ref_names before patching it`) {
		t.Fatalf("Build error = %v", err)
	}
}

func TestBuildRejectsRemovedDefaultSidecars(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "nac-test"},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
sidecars:
  - name: cgroup-runtime-exporter
    image: registry.local/cgroup-runtime-exporter:stable
`)
	writeMinimalApp(t, envDir, "api.yml", "name: api\n")

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err == nil || !strings.Contains(err.Error(), "_defaults.yml sidecars was removed; use sidecar_definitions and app sidecar_ref_names") {
		t.Fatalf("Build error = %v", err)
	}
}

func TestBuildRejectsUnknownSidecarRefName(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "nac-test"},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
sidecar_definitions:
  - name: cgroup-runtime-exporter
    image: registry.local/cgroup-runtime-exporter:stable
`)
	writeMinimalApp(t, envDir, "api.yml", "name: api\nsidecar_ref_names:\n  - missing\n")

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err == nil || !strings.Contains(err.Error(), `api: sidecar_ref_names references unknown sidecar "missing"`) {
		t.Fatalf("Build error = %v", err)
	}
}

func TestBuildReleaseManifestMustExistWhenSpecified(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, ReleaseManifest: filepath.Join(root, "missing.yml")})
	if err == nil || !strings.Contains(err.Error(), "release manifest not found") {
		t.Fatalf("err = %v, want missing release manifest", err)
	}
}

func TestInventoryContainsProfilesAndMTLSPaths(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "assets", "inventory.cfg"), "value=1\n")
	writeFile(t, filepath.Join(envDir, "replica-profiles.yml"), `
defaults:
  replica_profile_ref_name: normal
profiles:
  normal:
    apps:
      tsm-deco: 1
`)
	writeFile(t, filepath.Join(envDir, "apps", "tsm-deco.yml"), `
name: tsm-deco
rollout_on:
  checksums:
    config:
      files:
        - env.unsecured.json
        - assets/inventory.cfg
replicas: 2
containers:
  - name: tsm-deco
    image: "{{TSM_REGISTRY_URL}}/tsm-deco:{{TSM_RELEASE_ID}}"
    mtls:
      enabled: true
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	payload, err := Inventory(Options{Environment: "test", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := digString(payload, "profiles", "defaults", "replica_profile_ref_name"); got != "normal" {
		t.Fatalf("profile default = %q, want normal", got)
	}
	items := payload["items"].([]map[string]any)
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if got := item["replicas"]; got != 1 {
		t.Fatalf("inventory replicas = %#v, want profile-adjusted 1", got)
	}
	mtlsPaths := item["mtls_paths"].(map[string]any)
	if got := mtlsPaths["secured_json"]; got != "test/mtls/tsm-deco/tsm-deco.secured.json" {
		t.Fatalf("secured_json = %#v", got)
	}
	checksums := item["rollout_checksums"].(map[string]string)
	if got := checksums["checksum/config"]; got != checksumForFiles(t, envDir, "assets/inventory.cfg", "env.unsecured.json") {
		t.Fatalf("checksum/config = %q", got)
	}
}

func TestResourceSummarySupportsTextAndStructuredData(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE": "summary-test",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 3
containers:
  - name: api
    image: api:latest
    resources:
      cpu:
        from: "250m"
        to: "1"
      memory:
        from: "128Mi"
        to: "512Mi"
`)

	summary, err := ResourceSummary(Options{Environment: "test", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Environment != "test" {
		t.Fatalf("environment = %q, want test", summary.Environment)
	}
	if summary.Totals.Apps != 1 || summary.Totals.Containers != 1 || summary.Totals.Replicas != 3 {
		t.Fatalf("unexpected totals: %#v", summary.Totals)
	}
	if summary.Totals.CPURequestCores != 0.75 {
		t.Fatalf("cpu request total = %v, want 0.75", summary.Totals.CPURequestCores)
	}
	if summary.Totals.CPULimitCores != 3 {
		t.Fatalf("cpu limit total = %v, want 3", summary.Totals.CPULimitCores)
	}
	if summary.Totals.MemoryRequestMiB != 384 {
		t.Fatalf("memory request total = %v, want 384", summary.Totals.MemoryRequestMiB)
	}
	if summary.Totals.MemoryLimitMiB != 1536 {
		t.Fatalf("memory limit total = %v, want 1536", summary.Totals.MemoryLimitMiB)
	}
	text := FormatResourceSummaryText(summary)
	for _, expected := range []string{
		"Resource summary for environment: test",
		"+-----+-----+-----------+---------+---------+---------+---------+---------+---------+------------+------------+------------+------------+",
		"| api |   3 | api       | 250m    | 1       | 128Mi   | 512Mi   | -       | -       |      0.750 |      3.000 |    384.0Mi |   1536.0Mi |",
		"cpu req:    0.750 cores",
		"mem lim:    1536.0Mi",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("summary text missing %q:\n%s", expected, text)
		}
	}
}

func TestBuildGeneratesAssetConfigMapVolumeAndMount(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
			"DYNAMIC_VALUE":    "resolved",
		},
	})
	rawContent := "value={{DYNAMIC_VALUE}}\n"
	writeFile(t, filepath.Join(envDir, "assets", "app.conf"), rawContent)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    assets:
      - file: assets/app.conf
        to: /app/app.conf
        transform: false
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 1 {
		t.Fatalf("len(assets) = %d, want 1", len(result.Assets))
	}

	expectedDigest := assetCRC("assets/app.conf", "/app/app.conf", rawContent)
	expectedName := "api-asset-" + expectedDigest

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "volumes", "0", "name"); got != expectedName {
		t.Fatalf("volume name = %q, want %s", got, expectedName)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "mountPath"); got != "/app/app.conf" {
		t.Fatalf("mountPath = %q, want /app/app.conf", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "subPath"); got != "app.conf" {
		t.Fatalf("subPath = %q, want app.conf", got)
	}

	configMap := loadYAML(t, filepath.Join(target, "assets", expectedName+".yml"))
	if got := digString(configMap, "metadata", "name"); got != expectedName {
		t.Fatalf("configMap name = %q, want %s", got, expectedName)
	}
	if got := digString(configMap, "data", "app.conf"); got != rawContent {
		t.Fatalf("asset content = %q, want raw content", got)
	}
}

func TestBuildDoesNotAddSyncMetadataByDefault(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "annotations", "kube-build-app.io/sync-id"); got != "" {
		t.Fatalf("default build emitted sync-id = %q", got)
	}
	if got := digString(deployment, "metadata", "labels", "kube-build-app.io/sync-set"); got != "" {
		t.Fatalf("default build emitted sync-set = %q", got)
	}
}

func TestBuildAddsSyncMetadataProfile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "assets", "app.conf"), "value=one\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    assets:
      - file: assets/app.conf
        to: /app/app.conf
    ports:
      - name: http
        port: 8080
        expose_as:
          - service_name: api
            port: 80
    resources:
      cpu: {requests: "100m", limits: "500m"}
      memory: {requests: "128Mi", limits: "512Mi"}
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target, SyncProfile: "kube-deploy-sync"})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	assertSyncMetadata(t, deployment, "test/Deployment/nac-test/api", "200", "kube-deploy-sync")
	if got := digString(deployment, "metadata", "labels", "kube-build-app.io/sync-set"); got != "test" {
		t.Fatalf("deployment sync-set = %q, want test", got)
	}

	service := loadYAML(t, filepath.Join(target, "services", "api-service.yml"))
	assertSyncMetadata(t, service, "test/Service/nac-test/api", "300", "kube-deploy-sync")

	if len(result.Assets) != 1 {
		t.Fatalf("len(assets) = %d, want 1", len(result.Assets))
	}
	configMap := loadYAML(t, result.Assets[0])
	assertSyncMetadata(t, configMap, "test/app/api/container/api/asset/assets/app.conf:/app/app.conf", "100", "kube-deploy-sync")
}

func TestBuildSyncMetadataSupportsCustomPrefixAndSyncSet(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, SyncProfile: "kube-deploy-sync", SyncPrefix: "sync.example.test", SyncSet: "release-2026.20"})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "labels", "app.kubernetes.io/managed-by"); got != "kube-deploy-sync" {
		t.Fatalf("managed-by label = %q, want kube-deploy-sync", got)
	}
	if got := digString(deployment, "metadata", "labels", "sync.example.test/generated-by"); got != "kube-build-app" {
		t.Fatalf("custom generated-by = %q, want kube-build-app", got)
	}
	if got := digString(deployment, "metadata", "labels", "sync.example.test/sync-set"); got != "release-2026.20" {
		t.Fatalf("custom sync-set = %q, want release-2026.20", got)
	}
	if got := digString(deployment, "metadata", "annotations", "sync.example.test/sync-id"); got != "release-2026.20/Deployment/nac-test/api" {
		t.Fatalf("custom sync-id = %q", got)
	}
	if got := digString(deployment, "metadata", "annotations", "sync.example.test/sync-hash"); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("custom sync-hash = %q, want sha256 prefix", got)
	}
}

func TestBuildRejectsUnsupportedSyncMetadataProfile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, SyncProfile: "unknown-syncer"})
	if err == nil {
		t.Fatal("expected unsupported sync metadata profile error")
	}
	if !strings.Contains(err.Error(), "unsupported sync metadata profile") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildSyncMetadataKeepsAssetSyncIDStableAcrossContentHashName(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    assets:
      - file: assets/app.conf
        to: /app/app.conf
    resources:
      cpu: {requests: "100m", limits: "500m"}
      memory: {requests: "128Mi", limits: "512Mi"}
`)

	writeFile(t, filepath.Join(envDir, "assets", "app.conf"), "value=one\n")
	targetOne := filepath.Join(t.TempDir(), "target")
	resultOne, err := Build(Options{Environment: "test", Root: root, Target: targetOne, SyncProfile: "kube-deploy-sync"})
	if err != nil {
		t.Fatal(err)
	}
	first := loadYAML(t, resultOne.Assets[0])

	writeFile(t, filepath.Join(envDir, "assets", "app.conf"), "value=two\n")
	targetTwo := filepath.Join(t.TempDir(), "target")
	resultTwo, err := Build(Options{Environment: "test", Root: root, Target: targetTwo, SyncProfile: "kube-deploy-sync"})
	if err != nil {
		t.Fatal(err)
	}
	second := loadYAML(t, resultTwo.Assets[0])

	if filepath.Base(resultOne.Assets[0]) == filepath.Base(resultTwo.Assets[0]) {
		t.Fatalf("asset object name did not change: %s", resultOne.Assets[0])
	}
	if got, want := digString(second, "metadata", "annotations", "kube-build-app.io/sync-id"), digString(first, "metadata", "annotations", "kube-build-app.io/sync-id"); got != want {
		t.Fatalf("sync-id changed: %q != %q", got, want)
	}
	if got, want := digString(second, "metadata", "annotations", "kube-build-app.io/sync-hash"), digString(first, "metadata", "annotations", "kube-build-app.io/sync-hash"); got == want {
		t.Fatalf("sync-hash did not change: %q", got)
	}
}

func TestBuildArgocdSyncMetadataProfileAddsSyncWave(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    resources:
      cpu: {requests: "100m", limits: "500m"}
      memory: {requests: "128Mi", limits: "512Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, SyncProfile: "argocd"})
	if err != nil {
		t.Fatal(err)
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	assertSyncMetadata(t, deployment, "test/Deployment/nac-test/api", "200", "argocd")
	if got := digString(deployment, "metadata", "annotations", "argocd.argoproj.io/sync-wave"); got != "200" {
		t.Fatalf("argocd sync-wave = %q, want 200", got)
	}
}

func TestCanonicalObjectHashIgnoresRuntimeFieldsAndSyncHash(t *testing.T) {
	object := map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]any{
			"name":              "api",
			"namespace":         "nac-test",
			"resourceVersion":   "123",
			"creationTimestamp": "now",
			"annotations": map[string]any{
				"kube-build-app.io/sync-id":   "test/ConfigMap/nac-test/api",
				"kube-build-app.io/sync-hash": "sha256:old",
			},
		},
		"data":   map[string]any{"app.conf": "value=one\n"},
		"status": map[string]any{"ignored": true},
	}
	first, err := canonicalObjectHash(object, "kube-build-app.io")
	if err != nil {
		t.Fatal(err)
	}
	metadata := object["metadata"].(map[string]any)
	metadata["resourceVersion"] = "456"
	object["status"] = map[string]any{"ignored": false}
	metadata["annotations"].(map[string]any)["kube-build-app.io/sync-hash"] = "sha256:new"
	second, err := canonicalObjectHash(object, "kube-build-app.io")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("hash changed after volatile field update: %s != %s", first, second)
	}
	object["data"].(map[string]any)["app.conf"] = "value=two\n"
	third, err := canonicalObjectHash(object, "kube-build-app.io")
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatalf("hash did not change after desired content update: %s", third)
	}
}

func TestBuildGeneratesSharedAssetsAndCanDisableThemPerApp(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
			"SHARED_VALUE":     "resolved",
		},
	})
	writeFile(t, filepath.Join(envDir, "assets", "shared.conf.tpl"), "shared={{SHARED_VALUE}}\n")
	writeFile(t, filepath.Join(envDir, "shared.assets.yml"), `
assets:
  - file: assets/shared.conf.tpl
    to: /app/shared.conf
    transform: true
`)
	writeMinimalApp(t, envDir, "api.yml", `
name: api
`)
	writeMinimalApp(t, envDir, "isolated.yml", `
name: isolated
disable_shared_assets: true
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 1 {
		t.Fatalf("len(assets) = %d, want 1", len(result.Assets))
	}

	content := "shared=resolved\n"
	expectedName := "shared-asset-" + assetCRC("assets/shared.conf.tpl", "/app/shared.conf", content)
	shared := loadYAML(t, filepath.Join(target, "assets", "shared", expectedName+".yml"))
	if got := digString(shared, "data", "shared.conf"); got != content {
		t.Fatalf("shared asset content = %q, want %q", got, content)
	}

	api := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(api, "spec", "template", "spec", "volumes", "0", "name"); got != expectedName {
		t.Fatalf("shared volume name = %q, want %s", got, expectedName)
	}
	if got := digString(api, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "mountPath"); got != "/app/shared.conf" {
		t.Fatalf("shared mountPath = %q, want /app/shared.conf", got)
	}

	isolated := loadYAML(t, filepath.Join(target, "deployments", "isolated-deployment.yml"))
	if mounts := digSlice(isolated, "spec", "template", "spec", "containers", "0", "volumeMounts"); len(mounts) != 0 {
		t.Fatalf("isolated app has shared mounts: %#v", mounts)
	}
	if volumes := digSlice(isolated, "spec", "template", "spec", "volumes"); len(volumes) != 0 {
		t.Fatalf("isolated app has shared volumes: %#v", volumes)
	}
}

func TestBuildGeneratesTransformedAndBinaryAssets(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
			"DYNAMIC_VALUE":    "resolved",
		},
	})
	writeFile(t, filepath.Join(envDir, "assets", "rendered.conf.tpl"), "value={{DYNAMIC_VALUE}}\n")
	writeFile(t, filepath.Join(envDir, "assets", "cert.bin"), "\x00\x01abc")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    assets:
      - file: assets/rendered.conf.tpl
        to: /app/rendered.conf
        transform: true
      - file: assets/cert.bin
        to: /app/cert.bin
        binary: true
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	renderedContent := "value=resolved\n"
	renderedName := "api-asset-" + assetCRC("assets/rendered.conf.tpl", "/app/rendered.conf", renderedContent)
	renderedConfigMap := loadYAML(t, filepath.Join(target, "assets", renderedName+".yml"))
	if got := digString(renderedConfigMap, "data", "rendered.conf"); got != renderedContent {
		t.Fatalf("rendered asset content = %q, want %q", got, renderedContent)
	}

	binaryContent := "\x00\x01abc"
	binaryName := "api-asset-" + assetCRC("assets/cert.bin", "/app/cert.bin", binaryContent)
	binaryConfigMap := loadYAML(t, filepath.Join(target, "assets", binaryName+".yml"))
	if got := digString(binaryConfigMap, "binaryData", "cert.bin"); got != base64.StdEncoding.EncodeToString([]byte(binaryContent)) {
		t.Fatalf("binary asset content = %q", got)
	}
}

func TestBuildGeneratesVolumeOnlyAssetTypes(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    assets:
      - temp: true
        to: /tmp/cache
      - pvc: true
        name: data-claim
        to: /data
      - nfs-server: nfs.local
        path: /export/data
        to: /nfs
      - host-path: /var/lib/host-data
        to: /host
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 0 {
		t.Fatalf("len(configmap assets) = %d, want 0", len(result.Assets))
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	volumes := digSlice(deployment, "spec", "template", "spec", "volumes")
	if len(volumes) != 4 {
		t.Fatalf("len(volumes) = %d, want 4: %#v", len(volumes), volumes)
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "1", "persistentVolumeClaim", "claimName"); got != "data-claim" {
		t.Fatalf("pvc claimName = %q", got)
	}
	if emptyDir := digAny(deployment, "spec", "template", "spec", "volumes", "0", "emptyDir"); emptyDir == nil {
		t.Fatalf("temp volume missing emptyDir: %#v", volumes[0])
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "2", "nfs", "server"); got != "nfs.local" {
		t.Fatalf("nfs server = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "3", "hostPath", "path"); got != "/var/lib/host-data" {
		t.Fatalf("hostPath path = %q", got)
	}
	if subPath := digAny(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "subPath"); subPath != nil {
		t.Fatalf("temp mount has subPath: %#v", subPath)
	}
}

func TestBuildGeneratesModernMounts(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "assets", "app.conf"), "value=1\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    mounts:
      - type: config
        file: assets/app.conf
        mount_path: /app/app.conf
      - type: empty_dir
        name: cache
        mount_path: /tmp/cache
      - type: pvc
        name: data
        claim_name: data-claim
        mount_path: /data
      - type: nfs
        name: nfs-data
        server: nfs.local
        path: /export/data
        mount_path: /nfs
      - type: host_path
        name: host-data
        path: /var/lib/host-data
        mount_path: /host
      - type: raw
        volume:
          name: projected-config
          projected:
            sources: []
        mount:
          name: projected-config
          mountPath: /app/projected
          readOnly: true
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 1 {
		t.Fatalf("len(configmap assets) = %d, want 1", len(result.Assets))
	}
	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "0", "mountPath"); got != "/app/app.conf" {
		t.Fatalf("config mountPath = %q", got)
	}
	if emptyDir := digAny(deployment, "spec", "template", "spec", "volumes", "1", "emptyDir"); emptyDir == nil {
		t.Fatalf("empty_dir volume missing emptyDir")
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "2", "persistentVolumeClaim", "claimName"); got != "data-claim" {
		t.Fatalf("pvc claimName = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "3", "nfs", "server"); got != "nfs.local" {
		t.Fatalf("nfs server = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "4", "hostPath", "path"); got != "/var/lib/host-data" {
		t.Fatalf("hostPath path = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "volumes", "5", "name"); got != "projected-config" {
		t.Fatalf("raw volume name = %q", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "5", "mountPath"); got != "/app/projected" {
		t.Fatalf("raw mountPath = %q", got)
	}
	if got := digBool(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts", "5", "readOnly"); !got {
		t.Fatalf("raw mount readOnly = false, want true")
	}
}

func TestBuildHelmEscapesAssets(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "assets", "values.yaml"), "plain={{ HELM_VALUE }} wrapped={{`{{ ALREADY }}`}}\n")
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    assets:
      - file: assets/values.yaml
        to: /app/values.yaml
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target, HelmEscapeAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	expected := "plain={{`{{ HELM_VALUE }}`}} wrapped={{`{{ ALREADY }}`}}\n"
	name := "api-asset-" + assetCRC("assets/values.yaml", "/app/values.yaml", expected)
	configMap := loadYAML(t, filepath.Join(target, "assets", name+".yml"))
	if got := digString(configMap, "data", "values.yaml"); got != expected {
		t.Fatalf("helm escaped content = %q, want %q", got, expected)
	}
}

func TestBuildAddsMTLSAssets(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeJSON(t, filepath.Join(envDir, "mtls", "api", "api.secured.json"), map[string]any{
		"_public_key": "dummy",
		"environment": map[string]any{
			"tls.crt": "dummy",
		},
	})
	writeJSON(t, filepath.Join(envDir, "mtls", "api", "api.secured.schema.json"), map[string]any{
		"*": map[string]any{
			"encoding": "base64",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    mtls:
      enabled: true
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	result, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assets) != 2 {
		t.Fatalf("len(assets) = %d, want 2", len(result.Assets))
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	mounts := digSlice(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts")
	mountPaths := map[string]bool{}
	for _, item := range mounts {
		mount, _ := item.(map[string]any)
		path, _ := mount["mountPath"].(string)
		mountPaths[path] = true
	}
	if !mountPaths["/app/mtls.enc/mtls.secured.json"] {
		t.Fatalf("missing mtls.secured.json mount: %#v", mountPaths)
	}
	if !mountPaths["/app/mtls.enc/mtls.secured.schema.json"] {
		t.Fatalf("missing mtls.secured.schema.json mount: %#v", mountPaths)
	}
}

func TestBuildAddsMTLSAssetsWithCustomMountDir(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeJSON(t, filepath.Join(envDir, "mtls", "api", "api.secured.json"), map[string]any{"environment": map[string]any{}})
	writeJSON(t, filepath.Join(envDir, "mtls", "api", "api.secured.schema.json"), map[string]any{})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    mtls:
      enabled: true
      mount_dir: /custom/mtls
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	mounts := digSlice(deployment, "spec", "template", "spec", "containers", "0", "volumeMounts")
	mountPaths := map[string]bool{}
	for _, item := range mounts {
		mount, _ := item.(map[string]any)
		path, _ := mount["mountPath"].(string)
		mountPaths[path] = true
	}
	if !mountPaths["/custom/mtls/mtls.secured.json"] {
		t.Fatalf("missing custom mtls.secured.json mount: %#v", mountPaths)
	}
	if !mountPaths["/custom/mtls/mtls.secured.schema.json"] {
		t.Fatalf("missing custom mtls.secured.schema.json mount: %#v", mountPaths)
	}
}

func TestBuildMergesDefaultsVarsWithOverrideAndRemove(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
replicas: 3
vars:
  - name: APP_NAME
    value: "from-defaults"
  - name: REMOVED
    value: "should-not-appear"
labels:
  inherited: "true"
containers:
  - name: ignored-default-container
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
vars:
  - name: APP_NAME
    value: "api"
  - name: REMOVED
    remove: true
name: {{var:APP_NAME}}
containers:
  - name: "{{var:APP_NAME}}"
    image: "{{TSM_REGISTRY_URL}}/{{var:APP_NAME}}:{{TSM_RELEASE_ID}}"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	if got := digString(deployment, "metadata", "name"); got != "api" {
		t.Fatalf("metadata.name = %q, want api", got)
	}
	if got := digString(deployment, "metadata", "labels", "inherited"); got != "true" {
		t.Fatalf("metadata.labels.inherited = %q, want true", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "image"); got != "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}" {
		t.Fatalf("container image = %q, want unresolved apply-env placeholders", got)
	}
	if got := digString(deployment, "spec", "template", "spec", "containers", "0", "args", "1"); got != "echo ok" {
		t.Fatalf("container arg = %q, want echo ok", got)
	}
}

func TestBuildAppliesContainerEnvDefaultsOverrideAndRemove(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
container_envs:
  - container_ref_name: "*"
    envs:
      - name: GLOBAL_FLAG
        value: "true"
      - name: SHARED_SECRET
        secret_name: shared-secret
        key: shared-key
  - container_ref_name: "api"
    envs:
      - name: SERVICE_ONLY
        value: "service-default"
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    envs:
      - name: GLOBAL_FLAG
        value: "false"
      - name: SHARED_SECRET
        remove: true
      - name: LOCAL_ONLY
        field_path: spec.nodeName
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	env := digSlice(deployment, "spec", "template", "spec", "containers", "0", "env")
	if len(env) != 3 {
		t.Fatalf("len(env) = %d, want 3: %#v", len(env), env)
	}
	if got := envValue(env, "GLOBAL_FLAG"); got != "false" {
		t.Fatalf("GLOBAL_FLAG = %q, want false", got)
	}
	if got := envValue(env, "SERVICE_ONLY"); got != "service-default" {
		t.Fatalf("SERVICE_ONLY = %q, want service-default", got)
	}
	if got := envFieldPath(env, "LOCAL_ONLY"); got != "spec.nodeName" {
		t.Fatalf("LOCAL_ONLY fieldPath = %q, want spec.nodeName", got)
	}
	if got := envValue(env, "SHARED_SECRET"); got != "" {
		t.Fatalf("SHARED_SECRET unexpectedly exists: %q", got)
	}
}

func TestBuildAppliesContainerDefaults(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":           "nac-test",
			"TSM_REGISTRY_URL":    "registry.local/tsm",
			"TSM_RELEASE_ID":      "1.0.0",
			"DEFAULT_EXPOSE_PORT": 8080,
			"DEFAULT_HTTP_PORT":   80,
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
container_profiles:
  - name: java-service
    defaults:
      image: "{{env:TSM_REGISTRY_URL}}/{{var:APP_NAME}}:{{env:TSM_RELEASE_ID}}"
      startup:
        command: ["/bin/sh"]
        arguments:
          - /app/start-java.sh
          - /app/jib-classpath-file
          - /app/jib-main-class-file
      resources:
        cpu: {from: "100m", to: "300m"}
        memory: {from: "512Mi", to: "1024Mi"}
      envs:
        - name: SPRING_CONFIG_IMPORT
          value: "configserver:http://127.0.0.1:9999"
        - name: REMOVE_ME
          value: "default"
      ports:
        - name: http
          port: {{env:DEFAULT_EXPOSE_PORT}}
          expose_as:
            - hostname: "{{var:APP_NAME}}"
              port: {{env:DEFAULT_HTTP_PORT}}
      probes:
        http:
          path: /actuator/health
          port: {{env:DEFAULT_EXPOSE_PORT}}
        live: {failure: 5, period: 10, timeout: 2}
        ready: {failure: 2, period: 2, success: 2, timeout: 2}
        start: {failure: 30, period: 10, timeout: 2}
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
vars:
  - name: APP_NAME
    value: api
name: "{{var:APP_NAME}}"
replicas: 1
containers:
  - name: "{{var:APP_NAME}}"
    profile_ref_names:
      - java-service
    resources:
      cpu: {from: "150m", to: "500m"}
    envs:
      - name: JAVA_ARGS
        value: -Xms300m -Xmx700m
          -Dbuild.module={{var:APP_NAME}}
      - name: REMOVE_ME
        remove: true
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	containerPath := []string{"spec", "template", "spec", "containers", "0"}
	if got := digString(deployment, append(containerPath, "image")...); got != "registry.local/tsm/api:1.0.0" {
		t.Fatalf("image = %q", got)
	}
	if got := digString(deployment, append(containerPath, "command", "0")...); got != "/bin/sh" {
		t.Fatalf("command[0] = %q", got)
	}
	if got := digString(deployment, append(containerPath, "args", "2")...); got != "/app/jib-main-class-file" {
		t.Fatalf("args[2] = %q", got)
	}
	if got := digString(deployment, append(containerPath, "resources", "requests", "cpu")...); got != "150m" {
		t.Fatalf("cpu request = %q", got)
	}
	if got := digString(deployment, append(containerPath, "resources", "requests", "memory")...); got != "512Mi" {
		t.Fatalf("memory request = %q", got)
	}
	env := digSlice(deployment, append(containerPath, "env")...)
	if got := envValue(env, "SPRING_CONFIG_IMPORT"); got != "configserver:http://127.0.0.1:9999" {
		t.Fatalf("SPRING_CONFIG_IMPORT = %q", got)
	}
	if got := envValue(env, "JAVA_ARGS"); got != "-Xms300m -Xmx700m -Dbuild.module=api" {
		t.Fatalf("JAVA_ARGS = %q", got)
	}
	if got := envValue(env, "REMOVE_ME"); got != "" {
		t.Fatalf("REMOVE_ME unexpectedly exists: %q", got)
	}
	if got := digString(deployment, append(containerPath, "readinessProbe", "httpGet", "path")...); got != "/actuator/health" {
		t.Fatalf("readiness probe path = %q", got)
	}

	service := loadYAML(t, filepath.Join(target, "services", "api-service.yml"))
	if got := digInt(service, "spec", "ports", "0", "targetPort"); got != 8080 {
		t.Fatalf("service targetPort = %d", got)
	}
}

func TestBuildRendersLegacyContainerEnvVars(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE":        "nac-test",
			"TSM_REGISTRY_URL": "registry.local/tsm",
			"TSM_RELEASE_ID":   "1.0.0",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
replicas: 1
containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    env_vars:
      - name: JAVA_ARGS
        value: -Xms500m -Xmx500m
          -Dbuild.module=api
      - name: SECRET_PUBLIC_KEY
        key: public-key
        secret_name: tsm-secrets
    resources:
      cpu:
        from: "100m"
        to: "200m"
      memory:
        from: "128Mi"
        to: "256Mi"
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	env := digSlice(deployment, "spec", "template", "spec", "containers", "0", "env")
	if len(env) != 2 {
		t.Fatalf("len(env) = %d, want 2: %#v", len(env), env)
	}
	if got := envValue(env, "JAVA_ARGS"); got != "-Xms500m -Xmx500m -Dbuild.module=api" {
		t.Fatalf("JAVA_ARGS = %q", got)
	}
	if got := envSecretName(env, "SECRET_PUBLIC_KEY"); got != "tsm-secrets" {
		t.Fatalf("SECRET_PUBLIC_KEY secret name = %q, want tsm-secrets", got)
	}
}

func TestBuildResolvesWorkloadIdentityTokenEnvReferences(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE": "tsm-test",
		},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
workload_identity:
  service_account:
    create: true
  tokens:
    - name: simple-config
      audience: simple-config-server
    - name: custom-token
      audience: custom-service
      mount_path: /var/run/custom-identity
      path: credential.jwt

container_envs:
  - container_ref_name: "*"
    envs:
      - name: DEFAULT_TOKEN_FILE
        workload_identity_token_ref_name: simple-config
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
init_containers:
  - name: prepare
    image: registry.example.test/prepare:1
    envs:
      - name: INIT_TOKEN_FILE
        workload_identity_token_ref_name: simple-config
sidecars:
  - name: token-proxy
    image: registry.example.test/token-proxy:1
    envs:
      - name: PROXY_TOKEN_FILE
        workload_identity_token_ref_name: custom-token
containers:
  - name: api
    image: registry.example.test/api:1
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	podSpec := digAny(deployment, "spec", "template", "spec").(map[string]any)
	containers := podSpec["containers"].([]any)
	api := namedObject(containers, "api")
	if got := envValue(digSlice(api, "env"), "DEFAULT_TOKEN_FILE"); got != "/var/run/secrets/workload-identity/simple-config/token" {
		t.Fatalf("DEFAULT_TOKEN_FILE = %q", got)
	}
	proxy := namedObject(containers, "token-proxy")
	if got := envValue(digSlice(proxy, "env"), "PROXY_TOKEN_FILE"); got != "/var/run/custom-identity/credential.jwt" {
		t.Fatalf("PROXY_TOKEN_FILE = %q", got)
	}
	initContainers := podSpec["initContainers"].([]any)
	prepare := namedObject(initContainers, "prepare")
	if got := envValue(digSlice(prepare, "env"), "INIT_TOKEN_FILE"); got != "/var/run/secrets/workload-identity/simple-config/token" {
		t.Fatalf("INIT_TOKEN_FILE = %q", got)
	}
}

func TestValidateRejectsUnknownWorkloadIdentityTokenEnvReference(t *testing.T) {
	app := appModel{
		Name: "api",
		Containers: []containerSpec{
			{
				Name: "api",
				Envs: []envVar{
					{Name: "TOKEN_FILE", WorkloadIdentityTokenRefName: "missing"},
				},
			},
		},
	}

	err := validateApps([]appModel{app}, nil)
	if err == nil || !strings.Contains(err.Error(), `env "TOKEN_FILE" references unknown workload_identity token "missing"`) {
		t.Fatalf("validateApps error = %v", err)
	}
}

func TestValidateRejectsConflictingWorkloadIdentityTokenEnvSource(t *testing.T) {
	app := appModel{
		Name: "api",
		WorkloadIdentity: workloadIdentitySpec{
			Tokens: []workloadIdentityTokenSpec{
				{Name: "simple-config", Audience: "simple-config-server"},
			},
		},
		Sidecars: []containerSpec{
			{
				Name: "token-proxy",
				Envs: []envVar{
					{
						Name:                         "TOKEN_FILE",
						Value:                        "/tmp/token",
						WorkloadIdentityTokenRefName: "simple-config",
					},
				},
			},
		},
	}

	err := validateApps([]appModel{app}, nil)
	if err == nil || !strings.Contains(err.Error(), `env "TOKEN_FILE" combines a reference with another value source`) {
		t.Fatalf("validateApps error = %v", err)
	}
}

func TestBuildGeneratesAuthenticatedRuntimeAssetsFetcher(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{
			"NAMESPACE": "tsm-test",
		},
	})
	writeFile(t, filepath.Join(envDir, "assets", "ssl", "internal-ca.pem"), `
-----BEGIN CERTIFICATE-----
test
-----END CERTIFICATE-----
`)
	writeFile(t, filepath.Join(envDir, "shared.assets.yml"), `
assets:
  - name: internal-ca
    file: assets/ssl/internal-ca.pem
    to: /var/run/certs/internal-ca.pem
`)
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
workload_identity:
  service_account:
    create: true
  tokens:
    - name: simple-config
      audience: simple-config-server

runtime_asset_definitions:
  - name: java-runtime-config
    source:
      base_url: https://config.example.test/simple-config-server
      label: release-1
      workload_identity_token_ref_name: simple-config
      ca_shared_asset_ref_name: internal-ca
    volume:
      name: runtime-config
      mount_path: /app/runtime-config
      medium: Memory
      size_limit: 16Mi
    fetcher:
      image: registry.example.test/simple-idm-token-proxy:1.0.0
    files:
      - source: files/ssl/client-keystore.jks
        target: client-keystore.jks
        mode: "0440"
        sha256: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      - source: files/ssl/client-truststore.jks
        target: ssl/client-truststore.jks

container_envs:
  - container_ref_name: "*"
    envs:
      - name: SSL_KEYSTORE
        value: /app/runtime-config/client-keystore.jks
      - name: SSL_CERT_FILE
        shared_asset_ref_name: internal-ca
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
runtime_asset_ref_names:
  - java-runtime-config
sidecars:
  - name: metrics
    image: registry.example.test/metrics:1
    resources:
      cpu: {from: "10m", to: "50m"}
      memory: {from: "16Mi", to: "64Mi"}
containers:
  - name: api
    image: registry.example.test/api:1
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	podSpec := digAny(deployment, "spec", "template", "spec").(map[string]any)
	volumes := podSpec["volumes"].([]any)
	runtimeVolume := namedObject(volumes, "runtime-config")
	if runtimeVolume == nil {
		t.Fatalf("runtime-config volume missing: %#v", volumes)
	}
	if got := digString(runtimeVolume, "emptyDir", "medium"); got != "Memory" {
		t.Fatalf("runtime volume medium = %q, want Memory", got)
	}
	if got := digString(runtimeVolume, "emptyDir", "sizeLimit"); got != "16Mi" {
		t.Fatalf("runtime volume sizeLimit = %q, want 16Mi", got)
	}

	initContainers := podSpec["initContainers"].([]any)
	fetcher := namedObject(initContainers, "runtime-assets-java-runtime-config")
	if fetcher == nil {
		t.Fatalf("runtime fetcher missing: %#v", initContainers)
	}
	if got := digString(fetcher, "image"); got != "registry.example.test/simple-idm-token-proxy:1.0.0" {
		t.Fatalf("fetcher image = %q", got)
	}
	if got := digString(fetcher, "command", "0"); got != "simple-idm-token-proxy" {
		t.Fatalf("fetcher command = %q", got)
	}
	if got := digString(fetcher, "resources", "requests", "cpu"); got != "10m" {
		t.Fatalf("fetcher CPU request = %q", got)
	}
	if got := digString(fetcher, "resources", "limits", "memory"); got != "128Mi" {
		t.Fatalf("fetcher memory limit = %q", got)
	}
	if got := namedObject(digSlice(fetcher, "volumeMounts"), "runtime-config"); got == nil || got["mountPath"] != "/app/runtime-config" {
		t.Fatalf("fetcher runtime mount missing: %#v", digSlice(fetcher, "volumeMounts"))
	}
	if got := namedObject(digSlice(fetcher, "volumeMounts"), "simple-config-token"); got == nil || got["mountPath"] != "/var/run/secrets/workload-identity/simple-config" {
		t.Fatalf("fetcher token mount missing: %#v", digSlice(fetcher, "volumeMounts"))
	}
	if got := mountAtPath(digSlice(fetcher, "volumeMounts"), "/var/run/certs/internal-ca.pem"); got == nil || got["readOnly"] != true || got["subPath"] != "internal-ca.pem" {
		t.Fatalf("fetcher CA mount missing: %#v", digSlice(fetcher, "volumeMounts"))
	}

	args := digSlice(fetcher, "args")
	if got := argumentValue(args, "--ca-file"); got != "/var/run/certs/internal-ca.pem" {
		t.Fatalf("fetcher --ca-file = %q", got)
	}
	filesJSON := argumentValue(args, "--files-json")
	var files []map[string]any
	if err := json.Unmarshal([]byte(filesJSON), &files); err != nil {
		t.Fatalf("invalid fetcher files JSON %q: %v", filesJSON, err)
	}
	if got := files[0]["source"]; got != "/api/v1/tenants/default/envs/test/assets/release-1/files/ssl/client-keystore.jks" {
		t.Fatalf("first source = %q", got)
	}
	if got := files[1]["mode"]; got != "0440" {
		t.Fatalf("default file mode = %q, want 0440", got)
	}

	containers := podSpec["containers"].([]any)
	api := namedObject(containers, "api")
	if got := namedObject(digSlice(api, "volumeMounts"), "runtime-config"); got == nil || got["readOnly"] != true {
		t.Fatalf("main container runtime mount missing or writable: %#v", digSlice(api, "volumeMounts"))
	}
	if got := envValue(digSlice(api, "env"), "SSL_KEYSTORE"); got != "/app/runtime-config/client-keystore.jks" {
		t.Fatalf("SSL_KEYSTORE = %q", got)
	}
	if got := envValue(digSlice(api, "env"), "SSL_CERT_FILE"); got != "/var/run/certs/internal-ca.pem" {
		t.Fatalf("SSL_CERT_FILE = %q", got)
	}
	metrics := namedObject(containers, "metrics")
	if got := namedObject(digSlice(metrics, "volumeMounts"), "runtime-config"); got != nil {
		t.Fatalf("wildcard runtime asset unexpectedly mounted into sidecar: %#v", got)
	}
	if got := namedObject(digSlice(metrics, "volumeMounts"), "simple-config-token"); got == nil {
		t.Fatalf("workload identity token not mounted into sidecar")
	}
}

func TestBuildComposesRuntimeAssetDefaultsIntoSharedVolume(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "tsm-test"},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
workload_identity:
  tokens:
    - name: simple-config
      audience: simple-config-server

runtime_asset_defaults:
  source:
    base_url: https://config.example.test/simple-config-server
    workload_identity_token_ref_name: simple-config
  volume:
    name: runtime-config
    mount_path: /app/runtime-config
    medium: Memory
    size_limit: 16Mi
  fetcher:
    image: registry.example.test/simple-idm-token-proxy:1.0.0

runtime_asset_definitions:
  - name: java-runtime-config
    files:
      - source: files/ssl/client-truststore.jks
        target: client-truststore.jks
  - name: connector-runtime-config
    files:
      - source: files/connector/mtls.yml
        target: mtls.yml
`)
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
runtime_asset_ref_names:
  - java-runtime-config
  - connector-runtime-config
sidecars:
  - name: mtls-gateway
    image: registry.example.test/mtls-gateway:1
    runtime_asset_ref_names:
      - connector-runtime-config
    resources:
      cpu: {from: "10m", to: "50m"}
      memory: {from: "16Mi", to: "64Mi"}
containers:
  - name: api
    image: registry.example.test/api:1
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)

	if _, err := Build(Options{Environment: "test", Root: root, Target: target}); err != nil {
		t.Fatal(err)
	}

	deployment := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	podSpec := digAny(deployment, "spec", "template", "spec").(map[string]any)
	if got := countNamedObjects(podSpec["volumes"].([]any), "runtime-config"); got != 1 {
		t.Fatalf("runtime-config volume count = %d, want 1", got)
	}

	initContainers := podSpec["initContainers"].([]any)
	if namedObject(initContainers, "runtime-assets-java-runtime-config") == nil {
		t.Fatalf("java runtime fetcher missing: %#v", initContainers)
	}
	if namedObject(initContainers, "runtime-assets-connector-runtime-config") == nil {
		t.Fatalf("connector runtime fetcher missing: %#v", initContainers)
	}

	containers := podSpec["containers"].([]any)
	api := namedObject(containers, "api")
	if got := countNamedObjects(digSlice(api, "volumeMounts"), "runtime-config"); got != 1 {
		t.Fatalf("api runtime-config mount count = %d, want 1", got)
	}
	mtlsGateway := namedObject(containers, "mtls-gateway")
	if got := countNamedObjects(digSlice(mtlsGateway, "volumeMounts"), "runtime-config"); got != 1 {
		t.Fatalf("sidecar runtime-config mount count = %d, want 1", got)
	}
}

func TestValidateRejectsDifferentlyConfiguredSharedRuntimeVolume(t *testing.T) {
	first := validRuntimeAssetForTest("java-config", "truststore.jks")
	second := validRuntimeAssetForTest("connector-config", "mtls.yml")
	second.Volume.SizeLimit = "32Mi"
	app := appModel{
		Name:          "api",
		RuntimeAssets: []runtimeAssetSpec{first, second},
		Containers:    []containerSpec{{Name: "api"}},
	}

	err := validateRuntimeAssets(
		app,
		map[string]bool{"simple-config": true},
		map[string]bool{},
		map[string]bool{},
	)
	if err == nil || !strings.Contains(err.Error(), `volume.name "runtime-config" conflicts with a differently configured runtime volume`) {
		t.Fatalf("validateRuntimeAssets error = %v", err)
	}
}

func TestValidateRejectsDuplicateTargetInSharedRuntimeVolume(t *testing.T) {
	first := validRuntimeAssetForTest("java-config", "config.yml")
	second := validRuntimeAssetForTest("connector-config", "config.yml")
	app := appModel{
		Name:          "api",
		RuntimeAssets: []runtimeAssetSpec{first, second},
		Containers:    []containerSpec{{Name: "api"}},
	}

	err := validateRuntimeAssets(
		app,
		map[string]bool{"simple-config": true},
		map[string]bool{},
		map[string]bool{},
	)
	if err == nil || !strings.Contains(err.Error(), `file target "config.yml" conflicts with runtime asset "java-config"`) {
		t.Fatalf("validateRuntimeAssets error = %v", err)
	}
}

func validRuntimeAssetForTest(name, target string) runtimeAssetSpec {
	return runtimeAssetSpec{
		Name: name,
		Source: runtimeAssetSourceSpec{
			Type:                         "simple_config",
			BaseURL:                      "https://config.example.test",
			Tenant:                       "default",
			Environment:                  "test",
			WorkloadIdentityTokenRefName: "simple-config",
			TimeoutSeconds:               30,
		},
		Volume: runtimeAssetVolumeSpec{
			Name:      "runtime-config",
			MountPath: "/app/runtime-config",
			Medium:    "Memory",
			SizeLimit: "16Mi",
		},
		Files: []runtimeAssetFileSpec{{
			Source: "files/" + target,
			Target: target,
			Mode:   "0440",
		}},
		Fetcher: runtimeAssetFetcherSpec{
			Image:   "registry.example.test/fetcher:1",
			Command: "simple-idm-token-proxy",
		},
		ContainerRefNames: []string{"*"},
	}
}

func TestBuildRejectsRuntimeAssetCAFileWithoutMatchingSharedAsset(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "tsm-test"},
	})
	writeFile(t, filepath.Join(envDir, "apps", "api.yml"), `
name: api
runtime_asset_ref_names:
  - runtime-config
workload_identity:
  tokens:
    - name: simple-config
      audience: simple-config-server
runtime_asset_definitions:
  - name: runtime-config
    source:
      base_url: https://config.example.test
      workload_identity_token_ref_name: simple-config
      ca_file: /var/run/certs/missing.pem
    volume:
      name: runtime-config
      mount_path: /app/runtime-config
    fetcher:
      image: registry.example.test/fetcher:1
    files:
      - source: files/config.yml
        target: config.yml
containers:
  - name: api
    image: registry.example.test/api:1
`)

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err == nil || !strings.Contains(err.Error(), `source.ca_file "/var/run/certs/missing.pem" does not match any shared asset target`) {
		t.Fatalf("Build error = %v", err)
	}
}

func TestBuildRejectsDuplicateSharedAssetRefName(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "tsm-test"},
	})
	writeFile(t, filepath.Join(envDir, "assets", "first.pem"), "first")
	writeFile(t, filepath.Join(envDir, "assets", "second.pem"), "second")
	writeFile(t, filepath.Join(envDir, "shared.assets.yml"), `
assets:
  - name: internal-ca
    file: assets/first.pem
    to: /var/run/certs/first.pem
  - name: internal-ca
    file: assets/second.pem
    to: /var/run/certs/second.pem
`)
	writeMinimalApp(t, envDir, "api.yml", "name: api\n")

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err == nil || !strings.Contains(err.Error(), `duplicate shared asset name "internal-ca"`) {
		t.Fatalf("Build error = %v", err)
	}
}

func TestValidateRejectsRuntimeAssetsWithUnknownToken(t *testing.T) {
	app := appModel{
		Name: "api",
		RuntimeAssets: []runtimeAssetSpec{
			{
				Name: "runtime-config",
				Source: runtimeAssetSourceSpec{
					Type:                         "simple_config",
					BaseURL:                      "https://config.example.test",
					Tenant:                       "default",
					Environment:                  "test",
					WorkloadIdentityTokenRefName: "missing",
					TimeoutSeconds:               30,
				},
				Volume: runtimeAssetVolumeSpec{
					Name:      "runtime-config",
					MountPath: "/app/runtime-config",
				},
				ContainerRefNames: []string{"*"},
				Files: []runtimeAssetFileSpec{
					{Source: "files/config.yml", Target: "config.yml", Mode: "0440"},
				},
				Fetcher: runtimeAssetFetcherSpec{Image: "registry.example.test/fetcher:1", Command: "simple-idm-token-proxy"},
			},
		},
	}
	err := validateApps([]appModel{app}, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown workload_identity token "missing"`) {
		t.Fatalf("validateApps error = %v", err)
	}
}

func TestValidateRejectsRuntimeAssetWithBothCASelectors(t *testing.T) {
	app := appModel{
		Name: "api",
		RuntimeAssets: []runtimeAssetSpec{
			{
				Name: "runtime-config",
				Source: runtimeAssetSourceSpec{
					Type:                         "simple_config",
					BaseURL:                      "https://config.example.test",
					Tenant:                       "default",
					Environment:                  "test",
					WorkloadIdentityTokenRefName: "simple-config",
					CAFile:                       "/var/run/certs/internal-ca.pem",
					CASharedAssetRefName:         "internal-ca",
					TimeoutSeconds:               30,
				},
				Volume:            runtimeAssetVolumeSpec{Name: "runtime-config", MountPath: "/app/runtime-config"},
				ContainerRefNames: []string{"*"},
				Files:             []runtimeAssetFileSpec{{Source: "files/config.yml", Target: "config.yml", Mode: "0440"}},
				Fetcher:           runtimeAssetFetcherSpec{Image: "registry.example.test/fetcher:1", Command: "simple-idm-token-proxy"},
			},
		},
	}

	err := validateRuntimeAssets(
		app,
		map[string]bool{"simple-config": true},
		map[string]bool{"internal-ca": true},
		map[string]bool{"/var/run/certs/internal-ca.pem": true},
	)
	if err == nil || !strings.Contains(err.Error(), "source.ca_file cannot be combined with source.ca_shared_asset_ref_name") {
		t.Fatalf("validateRuntimeAssets error = %v", err)
	}
}

func TestValidateRejectsRuntimeAssetCAFileWithInsecureTLS(t *testing.T) {
	app := appModel{
		Name: "api",
		RuntimeAssets: []runtimeAssetSpec{
			{
				Name: "runtime-config",
				Source: runtimeAssetSourceSpec{
					Type:                         "simple_config",
					BaseURL:                      "https://config.example.test",
					Tenant:                       "default",
					Environment:                  "test",
					WorkloadIdentityTokenRefName: "simple-config",
					CAFile:                       "/var/run/certs/internal-ca.pem",
					TimeoutSeconds:               30,
					InsecureUpstreamTLS:          true,
				},
				Volume: runtimeAssetVolumeSpec{
					Name:      "runtime-config",
					MountPath: "/app/runtime-config",
				},
				ContainerRefNames: []string{"*"},
				Files: []runtimeAssetFileSpec{
					{Source: "files/config.yml", Target: "config.yml", Mode: "0440"},
				},
				Fetcher: runtimeAssetFetcherSpec{
					Image:   "registry.example.test/fetcher:1",
					Command: "simple-idm-token-proxy",
				},
			},
		},
	}

	err := validateRuntimeAssets(
		app,
		map[string]bool{"simple-config": true},
		nil,
		map[string]bool{"/var/run/certs/internal-ca.pem": true},
	)
	if err == nil || !strings.Contains(err.Error(), "source.ca_file cannot be combined with source.insecure_upstream_tls") {
		t.Fatalf("validateRuntimeAssets error = %v", err)
	}
}

func TestBuildSelectsDefaultRuntimeAssetsByAppRefs(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "tsm-test"},
	})
	writeFile(t, filepath.Join(envDir, "apps", "_defaults.yml"), `
workload_identity:
  tokens:
    - name: simple-config
      audience: simple-config-server
runtime_asset_definitions:
  - name: runtime-config
    source:
      base_url: https://config.example.test
      workload_identity_token_ref_name: simple-config
    volume:
      mount_path: /app/runtime-config
    fetcher:
      image: registry.example.test/fetcher:1
    files:
      - source: files/config.yml
        target: config.yml
`)
	writeMinimalApp(t, envDir, "api.yml", "name: api\nruntime_asset_ref_names:\n  - runtime-config\n")
	writeMinimalApp(t, envDir, "worker.yml", "name: worker\n")

	if _, err := Build(Options{Environment: "test", Root: root, Target: target}); err != nil {
		t.Fatal(err)
	}

	api := loadYAML(t, filepath.Join(target, "deployments", "api-deployment.yml"))
	apiInit := digSlice(api, "spec", "template", "spec", "initContainers")
	if namedObject(apiInit, "runtime-assets-runtime-config") == nil {
		t.Fatalf("api runtime asset fetcher missing: %#v", apiInit)
	}
	worker := loadYAML(t, filepath.Join(target, "deployments", "worker-deployment.yml"))
	if got := digAny(worker, "spec", "template", "spec", "initContainers"); got != nil {
		t.Fatalf("worker unexpectedly inherited runtime asset fetcher: %#v", got)
	}
}

func TestRuntimeAssetReferenceMigrationErrors(t *testing.T) {
	legacyApps := []string{"api"}
	legacyContainers := []string{"api"}
	tests := []struct {
		name string
		app  appModel
		want string
	}{
		{
			name: "runtime assets",
			app:  appModel{Name: "api", LegacyRuntimeAssets: []runtimeAssetSpec{{Name: "runtime-config"}}},
			want: "runtime_assets was removed; use runtime_asset_definitions and runtime_asset_ref_names",
		},
		{
			name: "apps",
			app:  appModel{Name: "api", RuntimeAssetDefs: []runtimeAssetSpec{{Name: "runtime-config", LegacyApps: &legacyApps}}},
			want: "uses removed key apps",
		},
		{
			name: "containers",
			app:  appModel{Name: "api", RuntimeAssetDefs: []runtimeAssetSpec{{Name: "runtime-config", LegacyContainers: &legacyContainers}}},
			want: "uses removed key containers",
		},
		{
			name: "app ref names",
			app:  appModel{Name: "api", RuntimeAssetDefs: []runtimeAssetSpec{{Name: "runtime-config", AppRefNames: []string{"api"}}}},
			want: "uses removed key app_ref_names",
		},
		{
			name: "container ref names",
			app:  appModel{Name: "api", RuntimeAssetDefs: []runtimeAssetSpec{{Name: "runtime-config", ContainerRefNames: []string{"api"}}}},
			want: "uses removed key container_ref_names",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps := []appModel{tt.app}
			err := prepareRuntimeAssetsForApps(apps, "test")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("prepareRuntimeAssetsForApps error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRuntimeAssetRejectsUnknownRefName(t *testing.T) {
	apps := []appModel{{
		Name:             "api",
		RuntimeAssetRefs: []string{"missing"},
		RuntimeAssetDefs: []runtimeAssetSpec{{Name: "runtime-config"}},
	}}
	err := prepareRuntimeAssetsForApps(apps, "test")
	if err == nil || !strings.Contains(err.Error(), `runtime_asset_ref_names references unknown runtime asset "missing"`) {
		t.Fatalf("prepareRuntimeAssetsForApps error = %v", err)
	}
}

func TestContainerEnvDefaultsRejectRemovedNameSelector(t *testing.T) {
	root, err := parseYAMLMapping(`
container_envs:
  - name: "*"
    envs:
      - name: FLAG
        value: "true"
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseContainerEnvDefaults(mappingValue(root, "container_envs"))
	if err == nil || !strings.Contains(err.Error(), "container_envs[].name was removed; use container_ref_name") {
		t.Fatalf("parseContainerEnvDefaults error = %v", err)
	}
}

func TestContainerProfilesRejectNameInDefaults(t *testing.T) {
	root, err := parseYAMLMapping(`
container_profiles:
  - name: java-service
    defaults:
      name: api
`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseContainerProfiles(mappingValue(root, "container_profiles"))
	if err == nil || !strings.Contains(err.Error(), `container_profiles "java-service" defaults.name is not allowed`) {
		t.Fatalf("parseContainerProfiles error = %v", err)
	}
}

func TestReplicaProfilesRejectRemovedDefaultProfileKey(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target")
	envDir := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(envDir, "env.unsecured.json"), map[string]any{
		"environment": map[string]any{"NAMESPACE": "tsm-test"},
	})
	writeFile(t, filepath.Join(envDir, "replica-profiles.yml"), `
defaults:
  profile: normal
profiles:
  normal:
    all: 1
`)
	writeMinimalApp(t, envDir, "api.yml", "name: api\n")

	_, err := Build(Options{Environment: "test", Root: root, Target: target})
	if err == nil || !strings.Contains(err.Error(), "defaults.profile was removed; use defaults.replica_profile_ref_name") {
		t.Fatalf("Build error = %v", err)
	}
}

func TestValidateRejectsRemovedReferenceKeys(t *testing.T) {
	app := appModel{
		Name: "api",
		WorkloadIdentity: workloadIdentitySpec{
			Tokens: []workloadIdentityTokenSpec{{Name: "simple-config", Audience: "simple-config-server"}},
		},
		Containers: []containerSpec{{
			Name: "api",
			Envs: []envVar{{
				Name:                        "TOKEN_FILE",
				LegacyWorkloadIdentityToken: "simple-config",
			}},
		}},
	}
	err := validateApps([]appModel{app}, nil)
	if err == nil || !strings.Contains(err.Error(), "uses removed key workload_identity_token; use workload_identity_token_ref_name") {
		t.Fatalf("validateApps error = %v", err)
	}

	app.Containers[0].Envs = nil
	app.RuntimeAssets = []runtimeAssetSpec{{
		Name: "runtime-config",
		Source: runtimeAssetSourceSpec{
			Type:           "simple_config",
			BaseURL:        "https://config.example.test",
			Tenant:         "default",
			Environment:    "test",
			LegacyToken:    "simple-config",
			TimeoutSeconds: 30,
		},
		Volume: runtimeAssetVolumeSpec{Name: "runtime-config", MountPath: "/app/runtime-config"},
		Files:  []runtimeAssetFileSpec{{Source: "files/config.yml", Target: "config.yml", Mode: "0440"}},
		Fetcher: runtimeAssetFetcherSpec{
			Image:   "registry.example.test/fetcher:1",
			Command: "simple-idm-token-proxy",
		},
		ContainerRefNames: []string{"*"},
	}}
	err = validateApps([]appModel{app}, nil)
	if err == nil || !strings.Contains(err.Error(), "uses removed key source.token; use source.workload_identity_token_ref_name") {
		t.Fatalf("validateApps error = %v", err)
	}
}

func namedObject(items []any, name string) map[string]any {
	for _, item := range items {
		object, ok := item.(map[string]any)
		if ok && object["name"] == name {
			return object
		}
	}
	return nil
}

func countNamedObjects(items []any, name string) int {
	count := 0
	for _, item := range items {
		object, ok := item.(map[string]any)
		if ok && object["name"] == name {
			count++
		}
	}
	return count
}

func mountAtPath(items []any, mountPath string) map[string]any {
	for _, item := range items {
		object, ok := item.(map[string]any)
		if ok && object["mountPath"] == mountPath {
			return object
		}
	}
	return nil
}

func argumentValue(arguments []any, name string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == name {
			value, _ := arguments[index+1].(string)
			return value
		}
	}
	return ""
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(content))
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMinimalApp(t *testing.T, envDir string, fileName string, header string) {
	t.Helper()
	writeFile(t, filepath.Join(envDir, "apps", fileName), header+`
replicas: 1
containers:
  - name: app
    image: "{{TSM_REGISTRY_URL}}/app:{{TSM_RELEASE_ID}}"
    startup:
      command: ["/bin/sh"]
      arguments: ["-c", "echo ok"]
    resources:
      cpu: {from: "100m", to: "200m"}
      memory: {from: "128Mi", to: "256Mi"}
`)
}

func loadYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := yaml.Unmarshal(content, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func digString(value any, path ...string) string {
	current := value
	for _, part := range path {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil {
				return ""
			}
			if len(typed) <= index {
				return ""
			}
			current = typed[index]
		default:
			return ""
		}
	}
	if out, ok := current.(string); ok {
		return out
	}
	return ""
}

func assertSyncMetadata(t *testing.T, object map[string]any, syncID string, order string, manager string) {
	t.Helper()
	if got := digString(object, "metadata", "labels", "app.kubernetes.io/managed-by"); got != manager {
		t.Fatalf("managed-by label = %q, want %q", got, manager)
	}
	if got := digString(object, "metadata", "labels", "kube-build-app.io/generated-by"); got != "kube-build-app" {
		t.Fatalf("generated-by label = %q, want kube-build-app", got)
	}
	if got := digString(object, "metadata", "labels", "kube-build-app.io/sync-id-hash"); len(got) != 16 {
		t.Fatalf("sync-id-hash = %q, want 16 hex chars", got)
	}
	if got := digString(object, "metadata", "annotations", "kube-build-app.io/sync-id"); got != syncID {
		t.Fatalf("sync-id = %q, want %q", got, syncID)
	}
	if got := digString(object, "metadata", "annotations", "kube-build-app.io/sync-order"); got != order {
		t.Fatalf("sync-order = %q, want %q", got, order)
	}
	if got := digString(object, "metadata", "annotations", "kube-build-app.io/sync-hash"); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("sync-hash = %q, want sha256 prefix", got)
	}
}

func digInt(value any, path ...string) int {
	current := digAny(value, path...)
	switch typed := current.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func digBool(value any, path ...string) bool {
	current := digAny(value, path...)
	out, _ := current.(bool)
	return out
}

func digSlice(value any, path ...string) []any {
	current := digAny(value, path...)
	if out, ok := current.([]any); ok {
		return out
	}
	return nil
}

func digAny(value any, path ...string) any {
	current := value
	for _, part := range path {
		switch typed := current.(type) {
		case map[string]any:
			current = typed[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil {
				return nil
			}
			if index < 0 || len(typed) <= index {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
	}
	return current
}

func envValue(items []any, name string) string {
	item := envItem(items, name)
	if item == nil {
		return ""
	}
	value, _ := item["value"].(string)
	return value
}

func envFieldPath(items []any, name string) string {
	item := envItem(items, name)
	if item == nil {
		return ""
	}
	valueFrom, _ := item["valueFrom"].(map[string]any)
	fieldRef, _ := valueFrom["fieldRef"].(map[string]any)
	fieldPath, _ := fieldRef["fieldPath"].(string)
	return fieldPath
}

func envSecretName(items []any, name string) string {
	item := envItem(items, name)
	if item == nil {
		return ""
	}
	valueFrom, _ := item["valueFrom"].(map[string]any)
	secretKeyRef, _ := valueFrom["secretKeyRef"].(map[string]any)
	secretName, _ := secretKeyRef["name"].(string)
	return secretName
}

func envItem(items []any, name string) map[string]any {
	for _, item := range items {
		env, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if env["name"] == name {
			return env
		}
	}
	return nil
}

func assetCRC(relativeFile string, targetPath string, content string) string {
	return fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(relativeFile+targetPath+content)))
}

func checksumForFiles(t *testing.T, envDir string, relativePaths ...string) string {
	t.Helper()
	sort.Strings(relativePaths)
	hash := sha256.New()
	for _, relative := range relativePaths {
		content, err := os.ReadFile(filepath.Join(envDir, relative))
		if err != nil {
			t.Fatal(err)
		}
		hash.Write([]byte(relative))
		hash.Write([]byte{0})
		hash.Write(content)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func writeFakeEncjson(t *testing.T, name string, output string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	argsPath := filepath.Join(dir, name+".args")
	binPath := filepath.Join(dir, name)
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" > %q
cat <<'JSON'
%s
JSON
`, argsPath, output)
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binPath, argsPath
}
