package buildapp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// releaseManifestPaths preserves the public single-file Options API while
// allowing ordered overlays. CLI callers use ReleaseManifests exclusively.
func releaseManifestPaths(opts Options) []string {
	paths := make([]string, 0, len(opts.ReleaseManifests)+1)
	if path := strings.TrimSpace(opts.ReleaseManifest); path != "" {
		paths = append(paths, path)
	}
	paths = append(paths, opts.ReleaseManifests...)
	return paths
}

func normalizeReleaseDocument(content []byte) ([]byte, error) {
	var doc yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple YAML documents are not allowed")
		}
		return nil, err
	}
	var header struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
	}
	if err := doc.Decode(&header); err != nil {
		return nil, err
	}
	var root *yaml.Node
	if len(doc.Content) != 0 {
		root = doc.Content[0]
	}
	headerPresent := releaseMappingValue(root, "apiVersion") != nil || releaseMappingValue(root, "kind") != nil
	if headerPresent && ((header.APIVersion != "oci-toolbox/v1" && header.APIVersion != "oci-toolbox/v2") || header.Kind != "ImageRelease") {
		return nil, fmt.Errorf("unsupported release format %q / %q", header.APIVersion, header.Kind)
	}
	if header.APIVersion != "oci-toolbox/v2" {
		if err := normalizeLegacyReleaseFields(&doc); err != nil {
			return nil, err
		}
	}
	return yaml.Marshal(&doc)
}

// normalizeLegacyReleaseFields only renames v1 fields at their schema locations.
// Unknown fields and duplicate keys remain errors in the strict decoder.
func normalizeLegacyReleaseFields(doc *yaml.Node) error {
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if err := renameLegacyField(root, "registry_base", "repository_prefix"); err != nil {
		return err
	}
	images := releaseMappingValue(root, "images")
	if images == nil || images.Kind != yaml.SequenceNode {
		return nil
	}
	for _, image := range images.Content {
		if err := renameLegacyField(image, "image", "repository"); err != nil {
			return err
		}
		if source := releaseMappingValue(image, "source"); source != nil {
			if err := renameLegacyField(source, "image", "repository"); err != nil {
				return err
			}
		}
	}
	return nil
}

func releaseMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func renameLegacyField(node *yaml.Node, old, canonical string) error {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	if releaseMappingValue(node, canonical) != nil {
		return fmt.Errorf("field %q requires apiVersion %s", canonical, "oci-toolbox/v2")
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == old {
			node.Content[i].Value = canonical
		}
	}
	return nil
}

// resolveReleaseID validates every input before writing deployment files.
// Multiple inputs require an explicit deployment identity; no source release
// silently becomes the identity of the combined deployment.
func resolveReleaseID(opts Options) (string, error) {
	paths := releaseManifestPaths(opts)
	id := strings.TrimSpace(opts.ReleaseID)
	if opts.ReleaseID != "" && id == "" {
		return "", errors.New("release ID must not be empty")
	}
	if len(paths) > 1 && id == "" {
		return "", errors.New("multiple --release-manifest files require an explicit --release-id for deployment metadata")
	}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			return "", errors.New("release manifest path must not be empty")
		}
		manifest, err := loadReleaseManifest(path)
		if err != nil {
			return "", err
		}
		if len(paths) == 1 && id == "" {
			id = strings.TrimSpace(manifest.ReleaseID)
		}
	}
	if err := validateReleaseLabelValue("release ID", id); err != nil {
		return "", err
	}
	return id, nil
}
