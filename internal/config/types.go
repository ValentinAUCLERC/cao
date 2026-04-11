package config

import "fmt"

type WorkspaceManifest struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Platforms   []string `yaml:"platforms,omitempty"`
}

type ResourceManifest struct {
	Kind        string `yaml:"kind"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Source      string `yaml:"source"`
	Target      string `yaml:"target,omitempty"`
	TargetDir   string `yaml:"targetDir,omitempty"`
	Mode        string `yaml:"mode,omitempty"`
	Format      string `yaml:"format,omitempty"`
}

func (w WorkspaceManifest) Validate() error {
	if w.Name == "" {
		return fmt.Errorf("workspace name is required")
	}
	return nil
}

func (r ResourceManifest) Validate() error {
	if r.Kind == "" {
		return fmt.Errorf("resource kind is required")
	}
	if r.Name == "" {
		return fmt.Errorf("resource name is required")
	}
	if r.Source == "" {
		return fmt.Errorf("resource source is required")
	}
	switch r.Kind {
	case "secret":
	case "file":
		if r.Target == "" {
			return fmt.Errorf("file resources require a target")
		}
	case "publish":
	default:
		return fmt.Errorf("unsupported resource kind %q", r.Kind)
	}
	switch r.Format {
	case "", "auto", "yaml", "json", "dotenv", "binary", "kubeconfig":
	default:
		return fmt.Errorf("unsupported resource format %q", r.Format)
	}
	return nil
}
