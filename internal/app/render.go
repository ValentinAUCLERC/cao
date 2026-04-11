package app

import (
	"fmt"
	"strings"

	"github.com/ValentinAUCLERC/cao/internal/deps"
	"github.com/ValentinAUCLERC/cao/internal/engine"
	"github.com/ValentinAUCLERC/cao/internal/platform"
	caoruntime "github.com/ValentinAUCLERC/cao/internal/runtime"
	caoworkspace "github.com/ValentinAUCLERC/cao/internal/workspace"
)

func formatPlan(style outputStyle, plan *engine.Plan, diffItems []engine.DiffItem) string {
	if !style.enabled {
		return engine.FormatPlan(plan, diffItems)
	}

	filter := "all"
	if len(plan.WorkspaceFilter) > 0 {
		filter = strings.Join(plan.WorkspaceFilter, ", ")
	}

	active := fmt.Sprintf("%d", len(plan.Workspaces))
	if len(plan.Workspaces) > 0 {
		active = fmt.Sprintf("%d (%s)", len(plan.Workspaces), strings.Join(plan.ActiveWorkspaces(), ", "))
	}

	var lines []string
	lines = append(lines, style.heading("Plan"))
	lines = append(lines, formatDetailLine(style, "platform", string(plan.Platform), 18))
	lines = append(lines, formatDetailLine(style, "workspace filter", filter, 18))
	lines = append(lines, formatDetailLine(style, "active workspaces", active, 18))

	lines = append(lines, "")
	lines = append(lines, style.heading("Operations"))
	if len(diffItems) == 0 {
		lines = append(lines, "  "+style.muted("No changes."))
	} else {
		for _, item := range diffItems {
			lines = append(lines, fmt.Sprintf(
				"  %s %s %s %s",
				stylePlanStatus(style, padRight(item.Status, 6)),
				style.code(padRight(item.Kind, 8)),
				item.Target,
				style.muted(item.Owner),
			))
		}
	}

	if len(plan.Warnings) > 0 {
		lines = append(lines, "")
		lines = append(lines, style.heading("Warnings"))
		for _, warning := range plan.Warnings {
			lines = append(lines, "  "+style.warning(warning))
		}
	}

	return strings.Join(lines, "\n")
}

func formatDiffSummary(style outputStyle, summary map[string]int) string {
	if !style.enabled {
		var lines []string
		for _, key := range sortedKeys(summary) {
			lines = append(lines, fmt.Sprintf("%s: %d", key, summary[key]))
		}
		return strings.Join(lines, "\n")
	}

	if len(summary) == 0 {
		return style.muted("No changes.")
	}

	var lines []string
	lines = append(lines, style.heading("Summary"))
	for _, key := range sortedKeys(summary) {
		lines = append(lines, fmt.Sprintf("  %s %d", stylePlanStatus(style, padRight(key, 6)), summary[key]))
	}
	return strings.Join(lines, "\n")
}

func formatDoctor(style outputStyle, statuses []deps.Status, paths caoruntime.Paths) []string {
	if !style.enabled {
		return deps.FormatDoctor(statuses, paths)
	}

	var lines []string
	lines = append(lines, style.heading("Dependencies"))
	for _, status := range statuses {
		lines = append(lines, fmt.Sprintf(
			"  %s %s",
			style.label(padRight(string(status.Requirement), 10)),
			styleDoctorStatus(style, doctorSummary(status), status),
		))
		if !status.Present {
			for _, fix := range status.Fixes {
				lines = append(lines, "    "+style.muted("fix: ")+fix)
			}
		}
	}

	lines = append(lines, "")
	lines = append(lines, style.heading("Paths"))
	lines = append(lines, formatDetailLine(style, "platform", string(platform.Detect()), 12))
	lines = append(lines, formatDetailLine(style, "workspaces", paths.WorkspacesDir, 12))
	lines = append(lines, formatDetailLine(style, "state", paths.StateFile, 12))
	return lines
}

func formatPreflight(style outputStyle, commandName string, statuses []deps.Status) string {
	if !style.enabled {
		return deps.FormatPreflight(commandName, statuses)
	}

	problems := deps.BlockingProblems(statuses)
	if len(problems) == 0 {
		return ""
	}

	var lines []string
	lines = append(lines, style.danger("Missing prerequisites")+" for "+style.code("cao "+commandName)+":")
	for _, status := range problems {
		lines = append(lines, "  - "+style.danger(problemSummary(status)))
	}
	lines = append(lines, "")
	lines = append(lines, style.heading("How To Fix"))
	for _, status := range problems {
		lines = append(lines, "  "+style.label(string(status.Requirement)+":"))
		for _, fix := range status.Fixes {
			lines = append(lines, "    "+fix)
		}
	}
	lines = append(lines, "")
	lines = append(lines, "Run "+style.code("cao doctor")+" to inspect the full local setup.")
	return strings.Join(lines, "\n")
}

func formatWorkspaceListEntry(style outputStyle, info caoworkspace.Info) string {
	if !style.enabled {
		if info.Problem != "" {
			return fmt.Sprintf("%s (invalid: %s)", info.Name, info.Problem)
		}
		return info.Name
	}
	if info.Problem != "" {
		return fmt.Sprintf("%s %s", style.command(info.Name), style.danger("(invalid: "+info.Problem+")"))
	}
	return style.command(info.Name)
}

func formatWorkspaceInfo(style outputStyle, info caoworkspace.Info) string {
	if !style.enabled {
		var lines []string
		lines = append(lines, fmt.Sprintf("workspace: %s", info.Name))
		lines = append(lines, fmt.Sprintf("path: %s", info.Root))
		if info.Problem != "" {
			lines = append(lines, fmt.Sprintf("status: invalid (%s)", info.Problem))
			return strings.Join(lines, "\n")
		}
		lines = append(lines, "status: valid")
		if info.Manifest.Description != "" {
			lines = append(lines, fmt.Sprintf("description: %s", info.Manifest.Description))
		}
		if len(info.Manifest.Platforms) > 0 {
			lines = append(lines, fmt.Sprintf("platforms: %s", strings.Join(info.Manifest.Platforms, ", ")))
		}
		lines = append(lines, fmt.Sprintf("resources: %d", len(info.Resources)))
		for _, resource := range info.Resources {
			target := resource.Manifest.Target
			if target == "" {
				target = "(default)"
			}
			lines = append(lines, fmt.Sprintf("  - %s %s -> %s", resource.Manifest.Kind, resource.Manifest.Name, target))
		}
		return strings.Join(lines, "\n")
	}

	var lines []string
	lines = append(lines, style.heading("Workspace"))
	lines = append(lines, formatDetailLine(style, "workspace", info.Name, 12))
	lines = append(lines, formatDetailLine(style, "path", info.Root, 12))
	if info.Problem != "" {
		lines = append(lines, formatDetailLine(style, "status", style.danger("invalid ("+info.Problem+")"), 12))
		return strings.Join(lines, "\n")
	}

	lines = append(lines, formatDetailLine(style, "status", style.success("valid"), 12))
	if info.Manifest.Description != "" {
		lines = append(lines, formatDetailLine(style, "description", info.Manifest.Description, 12))
	}
	if len(info.Manifest.Platforms) > 0 {
		lines = append(lines, formatDetailLine(style, "platforms", strings.Join(info.Manifest.Platforms, ", "), 12))
	}

	lines = append(lines, "")
	lines = append(lines, style.heading(fmt.Sprintf("Resources (%d)", len(info.Resources))))
	if len(info.Resources) == 0 {
		lines = append(lines, "  "+style.muted("No resources."))
		return strings.Join(lines, "\n")
	}

	for _, resource := range info.Resources {
		target := resource.Manifest.Target
		if target == "" {
			target = "(default)"
		}
		lines = append(lines, fmt.Sprintf(
			"  %s %s %s",
			style.code(padRight(resource.Manifest.Kind, 8)),
			style.label(resource.Manifest.Name),
			style.muted("-> "+target),
		))
	}
	return strings.Join(lines, "\n")
}

func formatPathResult(style outputStyle, label, value string) string {
	if !style.enabled {
		return fmt.Sprintf("%s: %s", label, value)
	}
	return formatDetailLine(style, label, value, 10)
}

func formatWrittenFile(style outputStyle, path string) string {
	if !style.enabled {
		return fmt.Sprintf("encrypted file written to %s", path)
	}
	return style.success("Encrypted file written") + " to " + style.code(path)
}

func formatDetailLine(style outputStyle, label, value string, width int) string {
	left := padRight(label+":", width)
	if !style.enabled {
		return left + " " + value
	}
	return style.label(left) + " " + value
}

func stylePlanStatus(style outputStyle, status string) string {
	switch strings.TrimSpace(status) {
	case "create":
		return style.success(status)
	case "update":
		return style.warning(status)
	case "prune":
		return style.danger(status)
	default:
		return style.info(status)
	}
}

func styleDoctorStatus(style outputStyle, summary string, status deps.Status) string {
	if status.Present {
		return style.success(summary)
	}
	if status.Optional {
		return style.warning(summary)
	}
	return style.danger(summary)
}

func doctorSummary(status deps.Status) string {
	if status.Present {
		return status.Summary
	}
	if status.Summary != "missing" {
		return status.Summary
	}
	switch status.Requirement {
	case deps.RequirementAge:
		return "missing (optional helper CLI for age key management)"
	case deps.RequirementAgeKey:
		return "missing (needed to decrypt age-backed secrets)"
	default:
		return "missing"
	}
}

func problemSummary(status deps.Status) string {
	if status.Requirement == deps.RequirementAgeKey {
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
