package engine

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ValentinAUCLERC/cao/internal/command"
	"github.com/ValentinAUCLERC/cao/internal/config"
	"github.com/ValentinAUCLERC/cao/internal/deps"
	"github.com/ValentinAUCLERC/cao/internal/fsutil"
	"github.com/ValentinAUCLERC/cao/internal/platform"
	caoruntime "github.com/ValentinAUCLERC/cao/internal/runtime"
	"github.com/ValentinAUCLERC/cao/internal/secrets"
	"github.com/ValentinAUCLERC/cao/internal/state"
	caoworkspace "github.com/ValentinAUCLERC/cao/internal/workspace"
)

type Engine struct {
	Paths    caoruntime.Paths
	Platform platform.Name
	Runner   command.Runner
}

type Workspace struct {
	Name      string
	Root      string
	Manifest  *config.WorkspaceManifest
	Resources []Resource
}

type Resource struct {
	Workspace string
	Path      string
	Manifest  *config.ResourceManifest
}

type Operation struct {
	Kind      string
	Target    string
	Workspace string
	Owner     string
	Inputs    []string
	Content   []byte
	Hash      string
	Mode      fs.FileMode
	Sensitive bool
}

type Plan struct {
	WorkspaceFilter []string
	Platform        platform.Name
	Workspaces      []Workspace
	Operations      []Operation
	Warnings        []string
}

type DiffItem struct {
	Target    string
	Kind      string
	Workspace string
	Owner     string
	Status    string
}

type ApplyOptions struct {
	Prune bool
}

func New(paths caoruntime.Paths, runner command.Runner) *Engine {
	return &Engine{
		Paths:    paths,
		Platform: platform.Detect(),
		Runner:   runner,
	}
}

func (e *Engine) LoadPlan(ctx context.Context, workspaceFilter []string) (*Plan, error) {
	infos, err := caoworkspace.List(e.Paths, workspaceFilter)
	if err != nil {
		return nil, err
	}

	plan := &Plan{
		WorkspaceFilter: dedupeStrings(workspaceFilter),
		Platform:        e.Platform,
	}

	var active []Workspace
	for _, info := range infos {
		if info.Problem != "" {
			return nil, fmt.Errorf("workspace %q is invalid: %s", info.Name, info.Problem)
		}
		if info.Manifest == nil {
			return nil, fmt.Errorf("workspace %q has no manifest", info.Name)
		}
		if !platform.Matches(info.Manifest.Platforms, e.Platform) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("skip workspace %s on platform %s", info.Name, e.Platform))
			continue
		}

		workspace := Workspace{
			Name:     info.Name,
			Root:     info.Root,
			Manifest: info.Manifest,
		}
		for _, resourceInfo := range info.Resources {
			workspace.Resources = append(workspace.Resources, Resource{
				Workspace: info.Name,
				Path:      resourceInfo.Path,
				Manifest:  resourceInfo.Manifest,
			})
		}
		active = append(active, workspace)
	}

	operations, err := e.buildOperations(ctx, active)
	if err != nil {
		return nil, err
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Target < operations[j].Target })
	plan.Workspaces = active
	plan.Operations = operations
	return plan, nil
}

func (e *Engine) Diff(ctx context.Context, workspaceFilter []string) (*Plan, []DiffItem, *state.State, error) {
	plan, err := e.LoadPlan(ctx, workspaceFilter)
	if err != nil {
		return nil, nil, nil, err
	}
	current, err := state.Load(e.Paths.StateFile)
	if err != nil {
		return nil, nil, nil, err
	}
	return plan, diffPlan(plan, current), current, nil
}

func (e *Engine) Apply(ctx context.Context, workspaceFilter []string) (*Plan, []DiffItem, error) {
	return e.ApplyWithOptions(ctx, workspaceFilter, ApplyOptions{Prune: true})
}

func (e *Engine) ApplyWithOptions(ctx context.Context, workspaceFilter []string, opts ApplyOptions) (*Plan, []DiffItem, error) {
	plan, diffItems, current, err := e.Diff(ctx, workspaceFilter)
	if err != nil {
		return nil, nil, err
	}
	if err := fsutil.EnsureDir(e.Paths.AppGeneratedDir, 0o700); err != nil {
		return nil, nil, err
	}

	nextState := cloneState(current)
	nextState.AppliedWorkspaces = plan.ActiveWorkspaces()
	for _, operation := range plan.Operations {
		dirMode := fs.FileMode(0o755)
		if operation.Sensitive {
			dirMode = 0o700
		}
		if err := fsutil.WriteFileAtomicWithDirMode(operation.Target, operation.Content, operation.Mode, dirMode); err != nil {
			return nil, nil, fmt.Errorf("apply %s to %s: %w", operation.Kind, operation.Target, err)
		}
		nextState.Entries[operation.Target] = state.Entry{
			Path:      operation.Target,
			Kind:      operation.Kind,
			Workspace: operation.Workspace,
			Owner:     operation.Owner,
			Source:    strings.Join(operation.Inputs, ","),
			Hash:      operation.Hash,
			Mode:      fmt.Sprintf("%#o", operation.Mode),
			Sensitive: operation.Sensitive,
			UpdatedAt: time.Now().UTC(),
		}
	}
	if opts.Prune {
		for _, target := range pruneCandidates(plan, current) {
			if err := fsutil.RemoveFile(target); err != nil {
				return nil, nil, fmt.Errorf("prune %s during apply: %w", target, err)
			}
			delete(nextState.Entries, target)
		}
	}
	if err := state.Save(e.Paths.StateFile, nextState); err != nil {
		return nil, nil, err
	}
	return plan, diffItems, nil
}

func (e *Engine) Prune(ctx context.Context, workspaceFilter []string) ([]string, error) {
	plan, err := e.LoadPlan(ctx, workspaceFilter)
	if err != nil {
		return nil, err
	}
	current, err := state.Load(e.Paths.StateFile)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, target := range pruneCandidates(plan, current) {
		if err := fsutil.RemoveFile(target); err != nil {
			return nil, fmt.Errorf("prune %s: %w", target, err)
		}
		removed = append(removed, target)
		delete(current.Entries, target)
	}
	sort.Strings(removed)
	if err := state.Save(e.Paths.StateFile, current); err != nil {
		return nil, err
	}
	return removed, nil
}

func pruneCandidates(plan *Plan, current *state.State) []string {
	desired := map[string]struct{}{}
	for _, operation := range plan.Operations {
		desired[operation.Target] = struct{}{}
	}
	selected := workspaceSelection(plan.WorkspaceFilter)
	var removed []string
	for target, entry := range current.Entries {
		if _, keep := desired[target]; keep {
			continue
		}
		if len(selected) > 0 && entry.Workspace != "" {
			if _, ok := selected[entry.Workspace]; !ok {
				continue
			}
		} else if len(selected) > 0 && entry.Workspace == "" {
			continue
		}
		removed = append(removed, target)
	}
	sort.Strings(removed)
	return removed
}

func (p *Plan) ActiveWorkspaces() []string {
	names := make([]string, 0, len(p.Workspaces))
	for _, workspace := range p.Workspaces {
		names = append(names, workspace.Name)
	}
	sort.Strings(names)
	return names
}

func (e *Engine) buildOperations(ctx context.Context, workspaces []Workspace) ([]Operation, error) {
	var operations []Operation
	claimedTargets := map[string]string{}

	for _, workspace := range workspaces {
		for _, resource := range workspace.Resources {
			if resource.Manifest != nil && resource.Manifest.Kind == "secret" && !caoworkspace.SecretHasTarget(resource.Manifest) {
				continue
			}
			operation, err := e.buildResourceOperation(ctx, workspace.Name, workspace.Root, resource, claimedTargets)
			if err != nil {
				return nil, err
			}
			operations = append(operations, operation)
		}
	}

	sort.Slice(operations, func(i, j int) bool {
		return operations[i].Target < operations[j].Target
	})
	return operations, nil
}

func (e *Engine) buildResourceOperation(ctx context.Context, workspaceName, workspaceRoot string, resource Resource, claimedTargets map[string]string) (Operation, error) {
	sourcePath := filepath.Join(workspaceRoot, filepath.FromSlash(resource.Manifest.Source))
	owner := workspaceName + "/" + resource.Manifest.Name
	switch resource.Manifest.Kind {
	case "secret":
		cleartext, err := e.decryptSecret(ctx, sourcePath)
		if err != nil {
			return Operation{}, fmt.Errorf("decrypt secret %s: %w", sourcePath, err)
		}
		target := e.Paths.Expand(resource.Manifest.Target)
		if err := ensureTargetAvailable(claimedTargets, target, owner); err != nil {
			return Operation{}, err
		}
		mode, err := fsutil.ParseMode(resource.Manifest.Mode, 0o600)
		if err != nil {
			return Operation{}, err
		}
		return Operation{
			Kind:      "secret",
			Target:    target,
			Workspace: workspaceName,
			Owner:     owner,
			Inputs:    []string{sourcePath},
			Content:   cleartext,
			Hash:      fsutil.HashBytes(cleartext),
			Mode:      mode,
			Sensitive: true,
		}, nil
	case "file":
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return Operation{}, fmt.Errorf("read file resource %s: %w", sourcePath, err)
		}
		target := e.Paths.Expand(resource.Manifest.Target)
		if err := ensureTargetAvailable(claimedTargets, target, owner); err != nil {
			return Operation{}, err
		}
		mode, err := fsutil.ParseMode(resource.Manifest.Mode, 0o644)
		if err != nil {
			return Operation{}, err
		}
		return Operation{
			Kind:      "file",
			Target:    target,
			Workspace: workspaceName,
			Owner:     owner,
			Inputs:    []string{sourcePath},
			Content:   content,
			Hash:      fsutil.HashBytes(content),
			Mode:      mode,
		}, nil
	case "publish":
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return Operation{}, fmt.Errorf("read publish resource %s: %w", sourcePath, err)
		}
		targetDir := resource.Manifest.TargetDir
		if targetDir == "" {
			targetDir = e.Paths.BinDir
		}
		target := filepath.Join(e.Paths.Expand(targetDir), publishName(resource.Manifest.Name, sourcePath))
		if err := ensureTargetAvailable(claimedTargets, target, owner); err != nil {
			return Operation{}, err
		}
		mode, err := fsutil.ParseMode(resource.Manifest.Mode, 0o755)
		if err != nil {
			return Operation{}, err
		}
		return Operation{
			Kind:      "publish",
			Target:    target,
			Workspace: workspaceName,
			Owner:     owner,
			Inputs:    []string{sourcePath},
			Content:   content,
			Hash:      fsutil.HashBytes(content),
			Mode:      mode,
		}, nil
	default:
		return Operation{}, fmt.Errorf("unsupported resource kind %q", resource.Manifest.Kind)
	}
}

func ensureTargetAvailable(claimed map[string]string, targetPath, owner string) error {
	if previous, exists := claimed[targetPath]; exists {
		return fmt.Errorf("target collision on %s between %s and %s", targetPath, previous, owner)
	}
	claimed[targetPath] = owner
	return nil
}

func publishName(explicitName, sourcePath string) string {
	if explicitName != "" {
		return explicitName
	}
	return filepath.Base(sourcePath)
}

func (e *Engine) decryptSecret(ctx context.Context, path string) ([]byte, error) {
	return secrets.Decrypt(ctx, e.Runner, path)
}

func diffPlan(plan *Plan, current *state.State) []DiffItem {
	desired := map[string]Operation{}
	var items []DiffItem
	for _, operation := range plan.Operations {
		desired[operation.Target] = operation
		currentHash, err := fsutil.HashFile(operation.Target)
		status := "create"
		if err == nil {
			status = "update"
			if currentHash == operation.Hash {
				status = "noop"
				if entry, exists := current.Entries[operation.Target]; !exists || entry.Hash != operation.Hash {
					status = "adopt"
				}
			}
		}
		items = append(items, DiffItem{
			Target:    operation.Target,
			Kind:      operation.Kind,
			Workspace: operation.Workspace,
			Owner:     operation.Owner,
			Status:    status,
		})
	}
	selected := workspaceSelection(plan.WorkspaceFilter)
	for target, entry := range current.Entries {
		if _, keep := desired[target]; keep {
			continue
		}
		if len(selected) > 0 && entry.Workspace != "" {
			if _, ok := selected[entry.Workspace]; !ok {
				continue
			}
		} else if len(selected) > 0 && entry.Workspace == "" {
			continue
		}
		items = append(items, DiffItem{
			Target:    target,
			Kind:      entry.Kind,
			Workspace: entry.Workspace,
			Owner:     entry.OwnerLabel(),
			Status:    "prune",
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Target < items[j].Target
	})
	return items
}

func Summary(diffItems []DiffItem) map[string]int {
	summary := map[string]int{}
	for _, item := range diffItems {
		summary[item.Status]++
	}
	return summary
}

func FormatPlan(plan *Plan, diffItems []DiffItem) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("platform: %s", plan.Platform))
	if len(plan.WorkspaceFilter) == 0 {
		lines = append(lines, "workspace filter: all")
	} else {
		lines = append(lines, "workspace filter: "+strings.Join(plan.WorkspaceFilter, ", "))
	}
	lines = append(lines, fmt.Sprintf("workspaces: %d", len(plan.Workspaces)))
	if len(plan.Workspaces) > 0 {
		lines = append(lines, "active workspaces: "+strings.Join(plan.ActiveWorkspaces(), ", "))
	}
	for _, item := range diffItems {
		lines = append(lines, fmt.Sprintf("%-6s %-8s %-40s %s", item.Status, item.Kind, item.Target, item.Owner))
	}
	if len(plan.Warnings) > 0 {
		lines = append(lines, "")
		lines = append(lines, "warnings:")
		for _, warning := range plan.Warnings {
			lines = append(lines, "  - "+warning)
		}
	}
	return strings.Join(lines, "\n")
}

func Doctor(ctx context.Context, paths caoruntime.Paths, runner command.Runner) ([]string, error) {
	statuses, err := deps.Check(ctx, paths, runner, []deps.RequirementSpec{
		{Requirement: deps.RequirementGit},
		{Requirement: deps.RequirementSops},
		{Requirement: deps.RequirementAge, Optional: true},
		{Requirement: deps.RequirementAgeKey, Optional: true},
	})
	if err != nil {
		return nil, err
	}
	return deps.FormatDoctor(statuses, paths), nil
}

func workspaceSelection(filters []string) map[string]struct{} {
	if len(filters) == 0 {
		return nil
	}
	selected := map[string]struct{}{}
	for _, filter := range filters {
		selected[filter] = struct{}{}
	}
	return selected
}

func dedupeStrings(items []string) []string {
	seen := map[string]struct{}{}
	var deduped []string
	for _, item := range items {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		deduped = append(deduped, item)
	}
	return deduped
}

func cloneState(current *state.State) *state.State {
	clone := &state.State{
		Version:           current.Version,
		AppliedWorkspaces: append([]string(nil), current.AppliedWorkspaces...),
		UpdatedAt:         current.UpdatedAt,
		Entries:           map[string]state.Entry{},
	}
	for key, value := range current.Entries {
		clone.Entries[key] = value
	}
	return clone
}
