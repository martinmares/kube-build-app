package buildapp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReleaseManifestHeaderCompatibility(t *testing.T) {
	const body = "release_id: example\nimages:\n  - app_name: api\n    container_name: main\n    image: registry.example.com/api\n    digest: sha256:target\n"
	for _, tc := range []struct {
		name, header string
		valid        bool
	}{
		{"legacy", "", true},
		{"v1", "apiVersion: oci-toolbox/v1\nkind: ImageRelease\n", true},
		{"unknown version", "apiVersion: oci-toolbox/v99\nkind: ImageRelease\n", false},
		{"unknown kind", "apiVersion: oci-toolbox/v1\nkind: Bundle\n", false},
		{"incomplete", "kind: ImageRelease\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "release.yml")
			writeFile(t, path, tc.header+body)
			manifest, err := loadReleaseManifest(path)
			if !tc.valid {
				if err == nil || !strings.Contains(err.Error(), "unsupported release format") {
					t.Fatalf("expected unsupported header: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			image, err := manifest.imageFor("api", "main", releaseImageSelection{ReferenceMode: "auto"})
			if err != nil || image != "registry.example.com/api@sha256:target" {
				t.Fatalf("image = %q, error = %v", image, err)
			}
		})
	}
}
