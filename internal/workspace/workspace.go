package workspace

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/valentin/cao/internal/command"
	"github.com/valentin/cao/internal/config"
	"github.com/valentin/cao/internal/fsutil"
	caoruntime "github.com/valentin/cao/internal/runtime"
	"github.com/valentin/cao/internal/secrets"
	"github.com/valentin/cao/internal/state"
)

type ResourceInfo struct {
	Path     string
	Manifest *config.ResourceManifest
}

type Info struct {
	Name      string
	Root      string
	Manifest  *config.WorkspaceManifest
	Resources []ResourceInfo
	Problem   string
}

type AddSecretOptions struct {
	InputPath     string
	Name          string
	Target        string
	Format        string
	AgeRecipients []string
}

type AddFileOptions struct {
	InputPath string
	Name      string
	Target    string
}

type AddPublishOptions struct {
	InputPath string
	Name      string
	TargetDir string
}

type AddCommandOptions struct {
	Name      string
	Exec      string
	Shell     string
	Env       []string
	Args      []string
	TargetDir string
}

func Root(paths caoruntime.Paths, name string) string {
	return filepath.Join(paths.WorkspacesDir, name)
}

func EnsureBase(paths caoruntime.Paths) error {
	return fsutil.EnsureDir(paths.WorkspacesDir, 0o755)
}

func List(paths caoruntime.Paths, filters []string) ([]Info, error) {
	if err := EnsureBase(paths); err != nil {
		return nil, err
	}
	selected := map[string]struct{}{}
	for _, item := range filters {
		selected[item] = struct{}{}
	}
	if len(filters) > 0 {
		var infos []Info
		for _, name := range dedupeStrings(filters) {
			root := Root(paths, name)
			if _, err := os.Stat(root); err != nil {
				return nil, fmt.Errorf("workspace %q not found in %s", name, paths.WorkspacesDir)
			}
			info, err := loadRoot(root)
			if err != nil {
				return nil, err
			}
			infos = append(infos, info)
		}
		sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
		return infos, nil
	}

	entries, err := os.ReadDir(paths.WorkspacesDir)
	if err != nil {
		return nil, fmt.Errorf("read workspaces dir: %w", err)
	}
	var infos []Info
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if len(selected) > 0 {
			if _, ok := selected[entry.Name()]; !ok {
				continue
			}
		}
		info, err := loadRoot(filepath.Join(paths.WorkspacesDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

func Load(paths caoruntime.Paths, name string) (Info, error) {
	root := Root(paths, name)
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return Info{}, fmt.Errorf("workspace %q not found in %s", name, paths.WorkspacesDir)
		}
		return Info{}, fmt.Errorf("inspect workspace %q: %w", name, err)
	}
	return loadRoot(root)
}

func Init(paths caoruntime.Paths, name string) (string, error) {
	if err := EnsureBase(paths); err != nil {
		return "", err
	}
	name = sanitizeName(name)
	if name == "" {
		return "", fmt.Errorf("workspace name is required")
	}
	root := Root(paths, name)
	if _, err := os.Stat(root); err == nil {
		return "", fmt.Errorf("workspace %q already exists", name)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect workspace %q: %w", name, err)
	}
	dirs := []string{
		root,
		filepath.Join(root, "resources"),
		filepath.Join(root, "secrets"),
		filepath.Join(root, "files"),
		filepath.Join(root, "bin"),
	}
	for _, dir := range dirs {
		if err := fsutil.EnsureDir(dir, 0o755); err != nil {
			return "", err
		}
	}
	if err := config.WriteYAML(filepath.Join(root, "workspace.yaml"), config.WorkspaceManifest{Name: name}); err != nil {
		return "", err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(root, ".gitignore"), []byte(workspaceGitignore), 0o644); err != nil {
		return "", err
	}
	for _, keep := range []string{
		filepath.Join(root, "resources", ".gitkeep"),
		filepath.Join(root, "secrets", ".gitkeep"),
		filepath.Join(root, "files", ".gitkeep"),
		filepath.Join(root, "bin", ".gitkeep"),
	} {
		if err := fsutil.WriteFileAtomic(keep, nil, 0o644); err != nil {
			return "", err
		}
	}
	return root, nil
}

func Rename(paths caoruntime.Paths, fromName, toName string) (string, error) {
	if err := EnsureBase(paths); err != nil {
		return "", err
	}
	fromName = sanitizeName(fromName)
	toName = sanitizeName(toName)
	if fromName == "" || toName == "" {
		return "", fmt.Errorf("both source and destination workspace names are required")
	}
	if fromName == toName {
		return "", fmt.Errorf("source and destination workspace names are identical")
	}

	fromInfo, err := Load(paths, fromName)
	if err != nil {
		return "", err
	}
	if fromInfo.Problem != "" {
		return "", fmt.Errorf("workspace %q is invalid: %s", fromName, fromInfo.Problem)
	}
	toRoot := Root(paths, toName)
	if _, err := os.Stat(toRoot); err == nil {
		return "", fmt.Errorf("workspace %q already exists", toName)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect workspace %q: %w", toName, err)
	}

	if err := os.Rename(fromInfo.Root, toRoot); err != nil {
		return "", fmt.Errorf("rename workspace %q to %q: %w", fromName, toName, err)
	}

	manifestPath := filepath.Join(toRoot, "workspace.yaml")
	manifest, err := config.LoadWorkspace(manifestPath)
	if err != nil {
		return "", err
	}
	manifest.Name = toName
	if err := config.WriteYAML(manifestPath, *manifest); err != nil {
		return "", err
	}

	if err := rewriteWorkspaceResources(toRoot, fromName, toName); err != nil {
		return "", err
	}
	if err := rewriteWorkspaceBinScripts(toRoot, fromName, toName); err != nil {
		return "", err
	}
	if err := rewriteStateForWorkspaceRename(paths, fromName, toName); err != nil {
		return "", err
	}

	return toRoot, nil
}

func Fetch(ctx context.Context, paths caoruntime.Paths, runner command.Runner, repo, name string) (string, error) {
	if err := EnsureBase(paths); err != nil {
		return "", err
	}
	if repo == "" {
		return "", fmt.Errorf("repository is required")
	}
	if name == "" {
		name = DeriveName(repo)
	}
	name = sanitizeName(name)
	if name == "" {
		return "", fmt.Errorf("could not derive workspace name from %q", repo)
	}
	root := Root(paths, name)
	gitDir := filepath.Join(root, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		if info, statErr := os.Stat(root); statErr == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("workspace %q exists but is not a directory", name)
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil {
				return "", fmt.Errorf("inspect workspace %q: %w", name, readErr)
			}
			if len(entries) > 0 {
				return "", fmt.Errorf("workspace %q already exists but is not a git clone", name)
			}
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("inspect workspace %q: %w", name, statErr)
		}
		if _, err := runner.Run(ctx, "git", []string{"clone", repo, root}, command.RunOptions{}); err != nil {
			return "", fmt.Errorf("clone workspace %q: %w", name, err)
		}
	} else if err != nil {
		return "", fmt.Errorf("inspect workspace %q: %w", name, err)
	} else {
		out, err := runner.Run(ctx, "git", []string{"-C", root, "remote", "get-url", "origin"}, command.RunOptions{})
		if err != nil {
			return "", fmt.Errorf("inspect workspace %q origin: %w", name, err)
		}
		actual := strings.TrimSpace(string(out))
		if !sameRemote(actual, repo) {
			return "", fmt.Errorf("workspace %q already exists but origin is %q instead of %q", name, actual, repo)
		}
	}
	if _, err := runner.Run(ctx, "git", []string{"-C", root, "fetch", "--all", "--tags", "--prune"}, command.RunOptions{}); err != nil {
		return "", fmt.Errorf("fetch workspace %q: %w", name, err)
	}
	if _, err := runner.Run(ctx, "git", []string{"-C", root, "pull", "--ff-only"}, command.RunOptions{}); err != nil {
		return "", fmt.Errorf("pull workspace %q: %w", name, err)
	}
	return root, nil
}

func AddSecret(ctx context.Context, paths caoruntime.Paths, runner command.Runner, workspaceName string, opts AddSecretOptions) (string, string, error) {
	info, err := Load(paths, workspaceName)
	if err != nil {
		return "", "", err
	}
	if info.Problem != "" {
		return "", "", fmt.Errorf("workspace %q is invalid: %s", workspaceName, info.Problem)
	}
	if opts.InputPath == "" {
		return "", "", fmt.Errorf("input path is required")
	}
	name := opts.Name
	if name == "" {
		name = deriveResourceName(opts.InputPath)
	}
	name = sanitizeName(name)
	if name == "" {
		return "", "", fmt.Errorf("could not derive resource name from %q", opts.InputPath)
	}
	format := strings.TrimSpace(opts.Format)
	if format == "" || format == "auto" {
		format = secrets.DetectFormat(opts.InputPath)
	}
	sourceRel := filepath.ToSlash(filepath.Join("secrets", secretFilename(name, format)))
	outputPath := filepath.Join(info.Root, filepath.FromSlash(sourceRel))
	if _, err := secrets.Encrypt(ctx, runner, secrets.EncryptOptions{
		InputPath:     opts.InputPath,
		OutputPath:    outputPath,
		AgeRecipients: opts.AgeRecipients,
		Format:        format,
		WorkingDir:    info.Root,
		FileHint:      sourceRel,
	}); err != nil {
		return "", "", err
	}
	target := opts.Target
	if target == "" {
		target = defaultSecretTarget(workspaceName, name)
	}
	resource := config.ResourceManifest{
		Kind:   "secret",
		Name:   name,
		Source: sourceRel,
		Target: target,
		Mode:   "0600",
		Format: format,
	}
	resourcePath := filepath.Join(info.Root, "resources", "secret-"+name+".yaml")
	if err := config.WriteYAML(resourcePath, resource); err != nil {
		return "", "", err
	}
	return outputPath, resourcePath, nil
}

func AddFile(paths caoruntime.Paths, workspaceName string, opts AddFileOptions) (string, string, error) {
	info, err := Load(paths, workspaceName)
	if err != nil {
		return "", "", err
	}
	if info.Problem != "" {
		return "", "", fmt.Errorf("workspace %q is invalid: %s", workspaceName, info.Problem)
	}
	if opts.InputPath == "" {
		return "", "", fmt.Errorf("input path is required")
	}
	if opts.Target == "" {
		return "", "", fmt.Errorf("target is required")
	}
	name := opts.Name
	if name == "" {
		name = deriveResourceName(opts.InputPath)
	}
	name = sanitizeName(name)
	if name == "" {
		return "", "", fmt.Errorf("could not derive resource name from %q", opts.InputPath)
	}
	sourceRel := filepath.ToSlash(filepath.Join("files", storedFilename(name, opts.InputPath)))
	outputPath := filepath.Join(info.Root, filepath.FromSlash(sourceRel))
	if err := copyFile(opts.InputPath, outputPath, 0o644); err != nil {
		return "", "", err
	}
	resource := config.ResourceManifest{
		Kind:   "file",
		Name:   name,
		Source: sourceRel,
		Target: opts.Target,
		Mode:   "0644",
	}
	resourcePath := filepath.Join(info.Root, "resources", "file-"+name+".yaml")
	if err := config.WriteYAML(resourcePath, resource); err != nil {
		return "", "", err
	}
	return outputPath, resourcePath, nil
}

func AddPublish(paths caoruntime.Paths, workspaceName string, opts AddPublishOptions) (string, string, error) {
	info, err := Load(paths, workspaceName)
	if err != nil {
		return "", "", err
	}
	if info.Problem != "" {
		return "", "", fmt.Errorf("workspace %q is invalid: %s", workspaceName, info.Problem)
	}
	if opts.InputPath == "" {
		return "", "", fmt.Errorf("input path is required")
	}
	name := opts.Name
	if name == "" {
		name = derivePublishName(opts.InputPath)
	}
	name = sanitizeName(name)
	if name == "" {
		return "", "", fmt.Errorf("could not derive publish name from %q", opts.InputPath)
	}
	sourceRel := filepath.ToSlash(filepath.Join("bin", name))
	outputPath := filepath.Join(info.Root, filepath.FromSlash(sourceRel))
	if err := copyFile(opts.InputPath, outputPath, 0o755); err != nil {
		return "", "", err
	}
	resource := config.ResourceManifest{
		Kind:      "publish",
		Name:      name,
		Source:    sourceRel,
		TargetDir: opts.TargetDir,
		Mode:      "0755",
	}
	resourcePath := filepath.Join(info.Root, "resources", "publish-"+name+".yaml")
	if err := config.WriteYAML(resourcePath, resource); err != nil {
		return "", "", err
	}
	return outputPath, resourcePath, nil
}

func AddCommand(paths caoruntime.Paths, workspaceName string, opts AddCommandOptions) (string, string, error) {
	info, err := Load(paths, workspaceName)
	if err != nil {
		return "", "", err
	}
	if info.Problem != "" {
		return "", "", fmt.Errorf("workspace %q is invalid: %s", workspaceName, info.Problem)
	}
	name := sanitizeName(opts.Name)
	if name == "" {
		return "", "", fmt.Errorf("command name is required")
	}
	if strings.TrimSpace(opts.Exec) == "" && strings.TrimSpace(opts.Shell) == "" {
		return "", "", fmt.Errorf("either exec or shell is required")
	}
	if strings.TrimSpace(opts.Exec) != "" && strings.TrimSpace(opts.Shell) != "" {
		return "", "", fmt.Errorf("exec and shell are mutually exclusive")
	}
	for _, item := range opts.Env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return "", "", fmt.Errorf("invalid env assignment %q, expected KEY=VALUE", item)
		}
	}

	content, err := buildCommandScript(opts)
	if err != nil {
		return "", "", err
	}
	sourceRel := filepath.ToSlash(filepath.Join("bin", name))
	outputPath := filepath.Join(info.Root, filepath.FromSlash(sourceRel))
	if err := fsutil.WriteFileAtomic(outputPath, content, 0o755); err != nil {
		return "", "", err
	}
	resource := config.ResourceManifest{
		Kind:      "publish",
		Name:      name,
		Source:    sourceRel,
		TargetDir: opts.TargetDir,
		Mode:      "0755",
	}
	resourcePath := filepath.Join(info.Root, "resources", "publish-"+name+".yaml")
	if err := config.WriteYAML(resourcePath, resource); err != nil {
		return "", "", err
	}
	return outputPath, resourcePath, nil
}

func loadRoot(root string) (Info, error) {
	name := filepath.Base(root)
	info := Info{
		Name: name,
		Root: root,
	}
	manifestPath := filepath.Join(root, "workspace.yaml")
	manifest, err := config.LoadWorkspace(manifestPath)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file or directory") {
			info.Problem = "missing workspace.yaml"
			return info, nil
		}
		info.Problem = err.Error()
		return info, nil
	}
	info.Manifest = manifest
	if info.Manifest.Name == "" {
		info.Manifest.Name = name
	}
	resourceDir := filepath.Join(root, "resources")
	resourceEntries, err := os.ReadDir(resourceDir)
	if err != nil && !os.IsNotExist(err) {
		return Info{}, fmt.Errorf("read resources for workspace %q: %w", name, err)
	}
	for _, entry := range resourceEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		resourcePath := filepath.Join(resourceDir, entry.Name())
		resource, err := config.LoadResource(resourcePath)
		if err != nil {
			info.Problem = err.Error()
			return info, nil
		}
		info.Resources = append(info.Resources, ResourceInfo{
			Path:     resourcePath,
			Manifest: resource,
		})
	}
	sort.Slice(info.Resources, func(i, j int) bool {
		return info.Resources[i].Manifest.Name < info.Resources[j].Manifest.Name
	})
	return info, nil
}

func defaultSecretTarget(workspaceName, resourceName string) string {
	return caoruntime.DefaultGeneratedSecretTarget(workspaceName, resourceName)
}

func secretFilename(name, format string) string {
	switch format {
	case "yaml", "kubeconfig":
		return name + ".enc.yaml"
	case "json":
		return name + ".enc.json"
	default:
		return name + ".enc"
	}
}

func storedFilename(name, input string) string {
	ext := filepath.Ext(input)
	if ext == "" || ext == "." {
		return name
	}
	if strings.HasPrefix(filepath.Base(input), ".") && strings.Count(filepath.Base(input), ".") == 1 {
		return "." + name
	}
	return name + ext
}

func deriveResourceName(input string) string {
	base := filepath.Base(input)
	switch base {
	case ".env":
		return "env"
	}
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func derivePublishName(input string) string {
	base := filepath.Base(input)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

func DeriveName(repo string) string {
	repo = strings.TrimSpace(repo)
	repo = strings.TrimRight(repo, "/")
	repo = strings.TrimSuffix(repo, ".git")
	index := strings.LastIndexAny(repo, "/:")
	if index >= 0 {
		repo = repo[index+1:]
	}
	return sanitizeName(repo)
}

func sanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var out []rune
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			lastDash = false
		case r == '-', r == '_', r == '.', r == ' ':
			if !lastDash {
				out = append(out, '-')
				lastDash = true
			}
		}
	}
	return strings.Trim(string(out), "-")
}

func copyFile(inputPath, outputPath string, mode fs.FileMode) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read input %s: %w", inputPath, err)
	}
	return fsutil.WriteFileAtomic(outputPath, data, mode)
}

func buildCommandScript(opts AddCommandOptions) ([]byte, error) {
	lines := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
	}
	for _, item := range opts.Env {
		parts := strings.SplitN(item, "=", 2)
		rendered, err := shellQuoteWithVariables(parts[1])
		if err != nil {
			return nil, fmt.Errorf("render env %s: %w", parts[0], err)
		}
		lines = append(lines, "export "+parts[0]+"="+rendered)
	}
	lines = append(lines, "")
	if strings.TrimSpace(opts.Exec) != "" {
		command := []string{"exec", shellQuoteLiteral(strings.TrimSpace(opts.Exec))}
		for _, arg := range opts.Args {
			command = append(command, shellQuoteLiteral(arg))
		}
		command = append(command, "\"$@\"")
		lines = append(lines, strings.Join(command, " "))
	} else {
		lines = append(lines, strings.TrimSpace(opts.Shell))
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func shellQuoteLiteral(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\"'\"'`) + "'"
}

func shellQuoteWithVariables(value string) (string, error) {
	if value == "" {
		return "''", nil
	}

	var parts []string
	var literal strings.Builder
	flushLiteral := func() {
		if literal.Len() == 0 {
			return
		}
		parts = append(parts, shellQuoteLiteral(literal.String()))
		literal.Reset()
	}

	for index := 0; index < len(value); {
		if value[index] != '$' {
			literal.WriteByte(value[index])
			index++
			continue
		}
		if index+1 >= len(value) {
			literal.WriteByte(value[index])
			index++
			continue
		}
		switch next := value[index+1]; {
		case next == '{':
			end, err := shellVariableEnd(value, index)
			if err != nil {
				return "", err
			}
			flushLiteral()
			parts = append(parts, value[index:end])
			index = end
		case (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') || next == '_':
			end := index + 2
			for end < len(value) {
				char := value[end]
				if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' {
					end++
					continue
				}
				break
			}
			flushLiteral()
			parts = append(parts, value[index:end])
			index = end
		default:
			literal.WriteByte(value[index])
			index++
		}
	}
	flushLiteral()
	return strings.Join(parts, ""), nil
}

func shellVariableEnd(value string, start int) (int, error) {
	depth := 0
	for index := start; index < len(value); index++ {
		switch value[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return index + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated parameter expansion in %q", value)
}

func rewriteWorkspaceResources(root, fromName, toName string) error {
	resourceDir := filepath.Join(root, "resources")
	entries, err := os.ReadDir(resourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read resources for workspace rename: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		resourcePath := filepath.Join(resourceDir, entry.Name())
		resource, err := config.LoadResource(resourcePath)
		if err != nil {
			return err
		}
		if resource.Kind == "secret" && resource.Target == defaultSecretTarget(fromName, resource.Name) {
			resource.Target = defaultSecretTarget(toName, resource.Name)
		}
		if err := config.WriteYAML(resourcePath, *resource); err != nil {
			return err
		}
	}
	return nil
}

func rewriteWorkspaceBinScripts(root, fromName, toName string) error {
	binDir := filepath.Join(root, "bin")
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read bin dir for workspace rename: %w", err)
	}
	replacements := generatedPathReplacements(fromName, toName)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(binDir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read bin file %s: %w", path, err)
		}
		updated := string(content)
		for _, replacement := range replacements {
			updated = strings.ReplaceAll(updated, replacement[0], replacement[1])
		}
		if updated == string(content) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat bin file %s: %w", path, err)
		}
		if err := fsutil.WriteFileAtomic(path, []byte(updated), info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func rewriteStateForWorkspaceRename(paths caoruntime.Paths, fromName, toName string) error {
	current, err := state.Load(paths.StateFile)
	if err != nil {
		return err
	}
	for target, entry := range current.Entries {
		if entry.Workspace == fromName {
			entry.Workspace = toName
			entry.Owner = renameOwnerPrefix(entry.Owner, fromName, toName)
			current.Entries[target] = entry
		}
	}
	for index, item := range current.AppliedWorkspaces {
		if item == fromName {
			current.AppliedWorkspaces[index] = toName
		}
	}
	return state.Save(paths.StateFile, current)
}

func renameOwnerPrefix(value, fromName, toName string) string {
	prefix := fromName + "/"
	if strings.HasPrefix(value, prefix) {
		return toName + "/" + strings.TrimPrefix(value, prefix)
	}
	return value
}

func generatedPathReplacements(fromName, toName string) [][2]string {
	patterns := []string{
		"${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/%s/",
		"${XDG_CONFIG_HOME:-$HOME/.config}'/cao/generated/%s/",
		"${XDG_CONFIG_HOME}/cao/generated/%s/",
		"${XDG_CONFIG_HOME}'/cao/generated/%s/",
	}
	replacements := make([][2]string, 0, len(patterns))
	for _, pattern := range patterns {
		replacements = append(replacements, [2]string{
			fmt.Sprintf(pattern, fromName),
			fmt.Sprintf(pattern, toName),
		})
	}
	return replacements
}

func sameRemote(actual, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if actual == expected {
		return true
	}
	if actualPath, err := filepath.Abs(actual); err == nil {
		if expectedPath, err := filepath.Abs(expected); err == nil && actualPath == expectedPath {
			return true
		}
	}
	return false
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

const workspaceGitignore = `*.tmp
*.bak
*.dec
*.plain
*.decrypted
`
