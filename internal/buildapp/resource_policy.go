package buildapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type resourcePolicyFile struct {
	Containers map[string]resourcePolicyResources `yaml:"containers"`
}

type resourcePolicyResources struct {
	CPU              resourcePolicyRange `yaml:"cpu"`
	Memory           resourcePolicyRange `yaml:"memory"`
	EphemeralStorage resourcePolicyRange `yaml:"ephemeral-storage"`
}

type resourcePolicyRange struct {
	From any `yaml:"from"`
	To   any `yaml:"to"`
}

func applyResourcePolicies(apps []appModel, appFiles []string, opts Options) error {
	policyRoot := strings.TrimSpace(opts.ResourcePolicyRoot)
	if policyRoot == "" {
		return nil
	}
	if len(apps) != len(appFiles) {
		return fmt.Errorf("resource policy: internal app/file count mismatch")
	}

	policyAppsDir := filepath.Join(policyRoot, opts.Environment, "apps")
	for index := range apps {
		app := &apps[index]
		if app.Ignore {
			continue
		}
		policyPath := filepath.Join(policyAppsDir, filepath.Base(appFiles[index]))
		policy, err := loadResourcePolicy(policyPath, opts)
		if err != nil {
			return fmt.Errorf("%s: %w", app.Name, err)
		}
		if err := applyAppResourcePolicy(app, policy, policyPath); err != nil {
			return err
		}
	}
	return nil
}

func loadResourcePolicy(path string, opts Options) (resourcePolicyFile, error) {
	content, err := opts.readInput(path)
	if err != nil {
		if os.IsNotExist(err) {
			return resourcePolicyFile{}, fmt.Errorf("resource policy file not found: %s", path)
		}
		return resourcePolicyFile{}, fmt.Errorf("read resource policy %s: %w", path, err)
	}
	var policy resourcePolicyFile
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&policy); err != nil {
		return resourcePolicyFile{}, fmt.Errorf("parse resource policy %s: %w", path, err)
	}
	if len(policy.Containers) == 0 {
		return resourcePolicyFile{}, fmt.Errorf("resource policy %s must define containers", path)
	}
	return policy, nil
}

func applyAppResourcePolicy(app *appModel, policy resourcePolicyFile, path string) error {
	expected := make(map[string]bool, len(app.Containers)+len(app.Sidecars))
	for index := range app.Containers {
		container := &app.Containers[index]
		if err := applyContainerResourcePolicy(app.Name, container, policy, path); err != nil {
			return err
		}
		expected[container.Name] = true
	}
	for index := range app.Sidecars {
		container := &app.Sidecars[index]
		if err := applyContainerResourcePolicy(app.Name, container, policy, path); err != nil {
			return err
		}
		expected[container.Name] = true
	}

	extra := make([]string, 0)
	for name := range policy.Containers {
		if !expected[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("%s: resource policy %s contains unknown container(s): %s", app.Name, path, strings.Join(extra, ", "))
	}
	return nil
}

func applyContainerResourcePolicy(appName string, container *containerSpec, policy resourcePolicyFile, path string) error {
	name := strings.TrimSpace(container.Name)
	resources, ok := policy.Containers[name]
	if !ok {
		return fmt.Errorf("%s: resource policy %s has no entry for container %q", appName, path, name)
	}
	if err := validateResourcePolicyResources(resources); err != nil {
		return fmt.Errorf("%s: resource policy %s container %q: %w", appName, path, name, err)
	}
	container.Resources = resources.asModelResources()
	return nil
}

func validateResourcePolicyResources(resources resourcePolicyResources) error {
	for name, values := range map[string]resourcePolicyRange{
		"cpu":    resources.CPU,
		"memory": resources.Memory,
	} {
		if resourcePolicyValueMissing(values.From) {
			return fmt.Errorf("%s.from is required", name)
		}
		if resourcePolicyValueMissing(values.To) {
			return fmt.Errorf("%s.to is required", name)
		}
	}
	if resourcePolicyRangeConfigured(resources.EphemeralStorage) {
		if resourcePolicyValueMissing(resources.EphemeralStorage.From) {
			return fmt.Errorf("ephemeral-storage.from is required when ephemeral-storage is configured")
		}
		if resourcePolicyValueMissing(resources.EphemeralStorage.To) {
			return fmt.Errorf("ephemeral-storage.to is required when ephemeral-storage is configured")
		}
	}
	return nil
}

func resourcePolicyValueMissing(value any) bool {
	return value == nil || strings.TrimSpace(fmt.Sprint(value)) == ""
}

func resourcePolicyRangeConfigured(values resourcePolicyRange) bool {
	return values.From != nil || values.To != nil
}

func (resources resourcePolicyResources) asModelResources() map[string]map[string]any {
	out := map[string]map[string]any{
		"cpu": {
			"from": resources.CPU.From,
			"to":   resources.CPU.To,
		},
		"memory": {
			"from": resources.Memory.From,
			"to":   resources.Memory.To,
		},
	}
	if resourcePolicyRangeConfigured(resources.EphemeralStorage) {
		out["ephemeral-storage"] = map[string]any{
			"from": resources.EphemeralStorage.From,
			"to":   resources.EphemeralStorage.To,
		}
	}
	return out
}

func resourceSource(opts Options) string {
	if strings.TrimSpace(opts.ResourcePolicyRoot) != "" {
		return "external-policy"
	}
	return "inline"
}
