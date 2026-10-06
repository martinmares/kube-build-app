package buildapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseManifestOverlaysAreOrderedAndCLIImagesWin(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "v1.yml"), filepath.Join(dir, "v2.yml")
	writeFile(t, first, `apiVersion: oci-toolbox/v1
kind: ImageRelease
release_id: product-a
images:
  - id: api
    app_name: api
    container_name: main
    image: old.example/api
    tag: first
  - app_name: api
    container_name: metrics
    image: old.example/metrics
    tag: first
`)
	writeFile(t, second, `apiVersion: oci-toolbox/v2
kind: ImageRelease
release_id: product-b
images:
  - id: api
    app_name: api
    repository: new.example/api
    digest: sha256:second
  - app_name: '*'
    container_name: metrics
    repository: new.example/metrics
    tag: second
`)
	models := func() []appModel {
		return []appModel{{Name: "api", Containers: []containerSpec{{Name: "main", Image: "original"}, {Name: "helper", Image: "original"}}, Sidecars: []containerSpec{{Name: "metrics", Image: "original"}, {Name: "untouched", Image: "original"}}}}
	}
	opts := Options{ReleaseManifests: []string{first, second}, ReleaseID: "combined", ImageOverrides: []string{"api/helper=docker.io/library/nginx:1.27"}, ImagePolicy: "strict"}
	apps := models()
	if err := applyImageOverrides(apps, opts); err != nil {
		t.Fatal(err)
	}
	if apps[0].Containers[0].Image != "new.example/api@sha256:second" || apps[0].Containers[1].Image != "docker.io/library/nginx:1.27" || apps[0].Sidecars[0].Image != "new.example/metrics:second" || apps[0].Sidecars[1].Image != "original" {
		t.Fatalf("images = %#v", apps[0])
	}
	origins, err := inspectImageOverrideOrigins(models(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if origins[imageKey{"api", "main"}].Document != second || origins[imageKey{"api", "metrics"}].Document != second || origins[imageKey{"api", "helper"}].Kind != "cli_image_override" {
		t.Fatalf("origins = %#v", origins)
	}
	opts.ReleaseManifests = []string{second, first}
	apps = models()
	if err := applyImageOverrides(apps, opts); err != nil {
		t.Fatal(err)
	}
	if apps[0].Containers[0].Image != "old.example/api:first" || apps[0].Sidecars[0].Image != "old.example/metrics:first" {
		t.Fatalf("reverse order = %#v", apps[0])
	}
	opts.ImageOverrides = append(opts.ImageOverrides, "api/main=docker.io/library/alpine:3.21")
	apps = models()
	if err := applyImageOverrides(apps, opts); err != nil {
		t.Fatal(err)
	}
	if apps[0].Containers[0].Image != "docker.io/library/alpine:3.21" {
		t.Fatal(apps[0].Containers[0].Image)
	}
}

func TestMultipleReleasesRequireDeploymentIDBeforeWriting(t *testing.T) {
	root := t.TempDir()
	env := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(env, "env.unsecured.json"), map[string]any{"environment": map[string]any{"NAMESPACE": "test"}})
	writeMinimalApp(t, env, "api.yml", "name: api\n")
	first, second := filepath.Join(root, "a.yml"), filepath.Join(root, "b.yml")
	writeFile(t, first, "release_id: a\n")
	writeFile(t, second, "release_id: b\n")
	opts := Options{Environment: "test", Root: root, Target: filepath.Join(root, "out"), ReleaseManifests: []string{first, second}}
	if _, err := Build(opts); err == nil || !strings.Contains(err.Error(), "--release-id") {
		t.Fatalf("missing ID error = %v", err)
	}
	if _, err := os.Stat(opts.Target); !os.IsNotExist(err) {
		t.Fatalf("output created before validation: %v", err)
	}
	opts.ReleaseID = "combined"
	opts.ReleaseManifests = append(opts.ReleaseManifests, filepath.Join(root, "missing.yml"))
	if _, err := Build(opts); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing overlay error = %v", err)
	}
	if _, err := os.Stat(opts.Target); !os.IsNotExist(err) {
		t.Fatalf("output created before validation: %v", err)
	}
}

func TestReleaseV2HeaderAndSourceCompatibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release.yml")
	valid := `apiVersion: oci-toolbox/v2
kind: ImageRelease
release_id: example
repository_prefix: registry.example/team
images:
  - app_name: api
    container_name: main
    repository: registry.example/team/api
    digest: sha256:target
    source: {repository: source.example/api, tag: old, digest: 'sha256:target'}
`
	writeFile(t, path, valid)
	m, err := loadReleaseManifest(path)
	if err != nil || m.RegistryBase != "registry.example/team" || m.Images[0].Source.Image != "source.example/api" {
		t.Fatalf("v2 = %#v, %v", m, err)
	}
	for _, bad := range []string{strings.Replace(valid, "repository: registry.example/team/api", "image: registry.example/team/api", 1), strings.Replace(valid, "repository_prefix:", "registry_base:", 1), strings.Replace(valid, "repository: source.example/api", "image: source.example/api", 1), valid + "---\n{}\n", valid + "unknown: true\n"} {
		writeFile(t, path, bad)
		if _, err := loadReleaseManifest(path); err == nil {
			t.Fatalf("accepted invalid v2: %s", bad)
		}
	}
}

func TestExplicitReleaseIDOverridesExternalVariableAndSingleInferenceRemains(t *testing.T) {
	root := t.TempDir()
	env := filepath.Join(root, "test")
	writeJSON(t, filepath.Join(env, "env.unsecured.json"), map[string]any{"environment": map[string]any{"RELEASE_ID": "previous"}})
	file := filepath.Join(root, "release.yml")
	writeFile(t, file, "release_id: product-release\n")
	opts := Options{ReleaseManifests: []string{file}, ReleaseID: "deployment-release"}
	vars, err := loadBuildVars(env, opts)
	if err != nil || vars["RELEASE_ID"] != "deployment-release" {
		t.Fatalf("vars=%#v,err=%v", vars, err)
	}
	opts.ReleaseID = ""
	if _, err := loadBuildVars(env, opts); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("automatic conflict=%v", err)
	}
	writeJSON(t, filepath.Join(env, "env.unsecured.json"), map[string]any{"environment": map[string]any{}})
	vars, err = loadBuildVars(env, opts)
	if err != nil || vars["RELEASE_ID"] != "product-release" {
		t.Fatalf("automatic vars=%#v,err=%v", vars, err)
	}
}
