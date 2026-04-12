package app

import (
	"strings"
	"testing"

	"github.com/ValentinAUCLERC/cao/internal/config"
	"github.com/ValentinAUCLERC/cao/internal/deps"
	"github.com/ValentinAUCLERC/cao/internal/engine"
	"github.com/ValentinAUCLERC/cao/internal/platform"
	caoruntime "github.com/ValentinAUCLERC/cao/internal/runtime"
	caoworkspace "github.com/ValentinAUCLERC/cao/internal/workspace"
)

func TestFormatPlanStyledAddsSectionsAndANSI(t *testing.T) {
	t.Parallel()

	output := formatPlan(outputStyle{enabled: true}, &engine.Plan{
		Platform:   platform.Linux,
		Workspaces: []engine.Workspace{{Name: "work"}},
		Warnings:   []string{"skip workspace personal on platform linux"},
	}, []engine.DiffItem{{
		Status: "create",
		Kind:   "secret",
		Target: "/tmp/work-kubeconfig",
		Owner:  "work/work-kubeconfig",
	}})

	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI styling in %q", output)
	}
	if !strings.Contains(output, "Plan") || !strings.Contains(output, "Operations") || !strings.Contains(output, "Warnings") {
		t.Fatalf("expected styled sections in %q", output)
	}
}

func TestFormatPreflightStyledHighlightsProblems(t *testing.T) {
	t.Parallel()

	output := formatPreflight(outputStyle{enabled: true}, "apply", []deps.Status{{
		Requirement: deps.RequirementSops,
		Summary:     "missing",
		Fixes:       []string{"Install sops"},
	}})

	if !strings.Contains(output, "Missing prerequisites") {
		t.Fatalf("expected preflight title in %q", output)
	}
	if !strings.Contains(output, "sops is not installed") {
		t.Fatalf("expected problem summary in %q", output)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI styling in %q", output)
	}
}

func TestFormatWorkspaceInfoStyledShowsResourceSection(t *testing.T) {
	t.Parallel()

	output := formatWorkspaceInfo(outputStyle{enabled: true}, caoworkspace.Info{
		Name: "work",
		Root: "/tmp/work",
		Manifest: &config.WorkspaceManifest{
			Name:        "work",
			Description: "Professional environment",
			Platforms:   []string{"linux"},
		},
		Resources: []caoworkspace.ResourceInfo{{
			Manifest: &config.ResourceManifest{
				Kind:   "secret",
				Name:   "kubeconfig",
				Target: "${XDG_CONFIG_HOME}/cao/generated/work/kubeconfig",
			},
		}},
	})

	if !strings.Contains(output, "Workspace") || !strings.Contains(output, "Resources (1)") {
		t.Fatalf("expected workspace sections in %q", output)
	}
	if !strings.Contains(output, "kubeconfig") {
		t.Fatalf("expected resource details in %q", output)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI styling in %q", output)
	}
}

func TestFormatWorkspaceOverviewStyledShowsSectionsAndANSI(t *testing.T) {
	t.Parallel()

	output := formatWorkspaceOverview(outputStyle{enabled: true}, caoruntime.Paths{
		WorkspacesDir:   "/tmp/workspaces",
		ConfigHome:      "/tmp/config",
		BinDir:          "/tmp/bin",
		AppGeneratedDir: "/tmp/config/cao/generated",
	}, []caoworkspace.Info{{
		Name: "work",
		Resources: []caoworkspace.ResourceInfo{
			{
				Manifest: &config.ResourceManifest{
					Kind: "secret",
					Name: "kubeconfig",
				},
			},
			{
				Manifest: &config.ResourceManifest{
					Kind: "publish",
					Name: "kubectl-work",
				},
			},
		},
	}})

	if !strings.Contains(output, "Loaded Workspaces") {
		t.Fatalf("expected overview heading in %q", output)
	}
	if !strings.Contains(output, "Files") || !strings.Contains(output, "Commands") {
		t.Fatalf("expected resource sections in %q", output)
	}
	if !strings.Contains(output, "kubectl-work") || !strings.Contains(output, "kubeconfig") {
		t.Fatalf("expected resource names in %q", output)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI styling in %q", output)
	}
}
