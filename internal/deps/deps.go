package deps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ValentinAUCLERC/cao/internal/command"
	"github.com/ValentinAUCLERC/cao/internal/platform"
	caoruntime "github.com/ValentinAUCLERC/cao/internal/runtime"
	caoworkspace "github.com/ValentinAUCLERC/cao/internal/workspace"
)

type Requirement string

const (
	RequirementGit    Requirement = "git"
	RequirementSops   Requirement = "sops"
	RequirementAge    Requirement = "age"
	RequirementAgeKey Requirement = "age-key"
)

type RequirementSpec struct {
	Requirement Requirement
	Optional    bool
}

type Status struct {
	Requirement Requirement
	Optional    bool
	Present     bool
	Summary     string
	Fixes       []string
}

func Check(ctx context.Context, paths caoruntime.Paths, runner command.Runner, specs []RequirementSpec) ([]Status, error) {
	var statuses []Status
	for _, spec := range normalizeSpecs(specs) {
		var status Status
		switch spec.Requirement {
		case RequirementGit, RequirementSops, RequirementAge:
			status = checkTool(ctx, runner, spec.Requirement, spec.Optional)
		case RequirementAgeKey:
			status = checkAgeKey(paths, spec.Optional)
		default:
			return nil, fmt.Errorf("unknown requirement %q", spec.Requirement)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func HasSecretResources(paths caoruntime.Paths, filters []string) (bool, error) {
	infos, err := caoworkspace.List(paths, filters)
	if err != nil {
		return false, err
	}
	for _, info := range infos {
		if info.Problem != "" {
			continue
		}
		for _, resource := range info.Resources {
			if resource.Manifest != nil && resource.Manifest.Kind == "secret" {
				return true, nil
			}
		}
	}
	return false, nil
}

func BlockingProblems(statuses []Status) []Status {
	var blocking []Status
	for _, status := range statuses {
		if status.Present || status.Optional {
			continue
		}
		blocking = append(blocking, status)
	}
	return blocking
}

func FormatDoctor(statuses []Status, paths caoruntime.Paths) []string {
	var lines []string
	for _, status := range statuses {
		lines = append(lines, fmt.Sprintf("%s: %s", status.Requirement, doctorSummary(status)))
		if !status.Present {
			for _, fix := range status.Fixes {
				lines = append(lines, "  fix: "+fix)
			}
		}
	}
	lines = append(lines, fmt.Sprintf("platform: %s", platform.Detect()))
	lines = append(lines, fmt.Sprintf("workspaces: %s", paths.WorkspacesDir))
	lines = append(lines, fmt.Sprintf("state: %s", paths.StateFile))
	return lines
}

func FormatPreflight(commandName string, statuses []Status) string {
	problems := BlockingProblems(statuses)
	if len(problems) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("missing prerequisites for `cao %s`:", commandName))
	for _, status := range problems {
		lines = append(lines, "  - "+problemSummary(status))
	}
	lines = append(lines, "")
	lines = append(lines, "How to fix:")
	for _, status := range problems {
		lines = append(lines, fmt.Sprintf("  %s:", status.Requirement))
		for _, fix := range status.Fixes {
			lines = append(lines, "    "+fix)
		}
	}
	lines = append(lines, "")
	lines = append(lines, "Run `cao doctor` to inspect the full local setup.")
	return strings.Join(lines, "\n")
}

func checkTool(ctx context.Context, runner command.Runner, requirement Requirement, optional bool) Status {
	status := Status{
		Requirement: requirement,
		Optional:    optional,
		Fixes:       installHints(requirement, platform.Detect()),
	}

	out, err := runner.Run(ctx, string(requirement), []string{"--version"}, command.RunOptions{})
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			status.Summary = "missing"
			return status
		}
		status.Summary = fmt.Sprintf("unavailable (%v)", err)
		return status
	}

	line := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
	if line == "" {
		line = "ok"
	}
	status.Present = true
	status.Summary = line
	return status
}

func checkAgeKey(paths caoruntime.Paths, optional bool) Status {
	status := Status{
		Requirement: RequirementAgeKey,
		Optional:    optional,
		Fixes:       ageKeyFixes(paths, platform.Detect()),
	}

	for _, candidate := range ageKeyCandidates(paths) {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			status.Present = true
			status.Summary = candidate
			return status
		}
	}

	status.Summary = "missing"
	return status
}

func normalizeSpecs(specs []RequirementSpec) []RequirementSpec {
	indexByRequirement := map[Requirement]int{}
	var normalized []RequirementSpec
	for _, spec := range specs {
		if index, ok := indexByRequirement[spec.Requirement]; ok {
			normalized[index].Optional = normalized[index].Optional && spec.Optional
			continue
		}
		indexByRequirement[spec.Requirement] = len(normalized)
		normalized = append(normalized, spec)
	}
	return normalized
}

func ageKeyCandidates(paths caoruntime.Paths) []string {
	return []string{
		os.Getenv("SOPS_AGE_KEY_FILE"),
		filepath.Join(paths.ConfigHome, "sops", "age", "keys.txt"),
		filepath.Join(paths.Home, "Library", "Application Support", "sops", "age", "keys.txt"),
	}
}

func installHints(requirement Requirement, current platform.Name) []string {
	switch requirement {
	case RequirementGit:
		switch current {
		case platform.Darwin:
			return []string{"brew install git"}
		case platform.Windows:
			return []string{"Install Git for Windows from https://git-scm.com/download/win"}
		default:
			return []string{"Install git with your package manager, for example: sudo apt install git"}
		}
	case RequirementSops:
		switch current {
		case platform.Darwin:
			return []string{"brew install sops"}
		case platform.Windows:
			return []string{"Install sops from https://github.com/getsops/sops/releases"}
		default:
			return []string{"Install sops from https://github.com/getsops/sops/releases or your package manager"}
		}
	case RequirementAge:
		switch current {
		case platform.Darwin:
			return []string{"brew install age"}
		case platform.Windows:
			return []string{"Install age from https://github.com/FiloSottile/age/releases"}
		default:
			return []string{"Install age with your package manager, for example: sudo apt install age"}
		}
	default:
		return nil
	}
}

func ageKeyFixes(paths caoruntime.Paths, current platform.Name) []string {
	dir := filepath.Join(paths.ConfigHome, "sops", "age")
	keyFile := filepath.Join(dir, "keys.txt")
	switch current {
	case platform.Windows:
		return []string{
			"New-Item -ItemType Directory -Force -Path '" + dir + "'",
			"age-keygen -o '" + keyFile + "'",
			"Set SOPS_AGE_KEY_FILE if your key lives somewhere else",
		}
	default:
		return []string{
			"mkdir -p " + dir,
			"age-keygen -o " + keyFile,
			"Set SOPS_AGE_KEY_FILE if your key lives somewhere else",
		}
	}
}

func doctorSummary(status Status) string {
	if status.Present {
		return status.Summary
	}
	if status.Summary != "missing" {
		return status.Summary
	}
	switch status.Requirement {
	case RequirementAge:
		return "missing (optional helper CLI for age key management)"
	case RequirementAgeKey:
		return "missing (needed to decrypt age-backed secrets)"
	default:
		return "missing"
	}
}

func problemSummary(status Status) string {
	if status.Requirement == RequirementAgeKey {
		if status.Summary == "missing" {
			return "no age key file was detected"
		}
		return fmt.Sprintf("age key check failed (%s)", status.Summary)
	}
	if status.Summary == "missing" {
		return string(status.Requirement) + " is not installed"
	}
	return fmt.Sprintf("%s is unavailable (%s)", status.Requirement, status.Summary)
}
