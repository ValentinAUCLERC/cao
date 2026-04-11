package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/valentin/cao/internal/fsutil"
	"gopkg.in/yaml.v3"
)

func LoadWorkspace(path string) (*WorkspaceManifest, error) {
	var workspace WorkspaceManifest
	if err := loadStrictYAML(path, &workspace); err != nil {
		return nil, err
	}
	if err := workspace.Validate(); err != nil {
		return nil, fmt.Errorf("validate workspace %s: %w", path, err)
	}
	return &workspace, nil
}

func LoadResource(path string) (*ResourceManifest, error) {
	var resource ResourceManifest
	if err := loadStrictYAML(path, &resource); err != nil {
		return nil, err
	}
	if err := resource.Validate(); err != nil {
		return nil, fmt.Errorf("validate resource %s: %w", path, err)
	}
	return &resource, nil
}

func WriteYAML(path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode yaml %s: %w", path, err)
	}
	return fsutil.WriteFileAtomic(path, data, 0o644)
}

func loadStrictYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read yaml %s: %w", path, err)
	}
	if err := ValidateYAMLSubset(data); err != nil {
		return fmt.Errorf("validate yaml subset %s: %w", path, err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decode yaml %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("decode yaml %s: multiple YAML documents are not allowed", path)
	}
	return nil
}

func ValidateYAMLSubset(data []byte) error {
	var root yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&root); err != nil {
		return err
	}
	if len(root.Content) == 0 {
		return fmt.Errorf("empty YAML document")
	}
	return validateNode(root.Content[0], false)
}

func validateNode(node *yaml.Node, isMapKey bool) error {
	if node.Anchor != "" {
		return fmt.Errorf("anchors are not allowed")
	}
	if node.Alias != nil {
		return fmt.Errorf("aliases are not allowed")
	}
	if node.Tag != "" && !strings.HasPrefix(node.Tag, "!!") {
		return fmt.Errorf("custom YAML tags are not allowed: %s", node.Tag)
	}
	if isMapKey && node.Kind == yaml.ScalarNode && node.Value == "<<" {
		return fmt.Errorf("merge keys are not allowed")
	}
	if node.Kind == yaml.MappingNode {
		for index := 0; index < len(node.Content); index += 2 {
			if err := validateNode(node.Content[index], true); err != nil {
				return err
			}
			if index+1 < len(node.Content) {
				if err := validateNode(node.Content[index+1], false); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, child := range node.Content {
		if err := validateNode(child, false); err != nil {
			return err
		}
	}
	return nil
}
