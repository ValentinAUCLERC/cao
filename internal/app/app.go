package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ValentinAUCLERC/cao/internal/command"
	"github.com/ValentinAUCLERC/cao/internal/deps"
	"github.com/ValentinAUCLERC/cao/internal/engine"
	"github.com/ValentinAUCLERC/cao/internal/fsutil"
	caoruntime "github.com/ValentinAUCLERC/cao/internal/runtime"
	"github.com/ValentinAUCLERC/cao/internal/secrets"
	caoworkspace "github.com/ValentinAUCLERC/cao/internal/workspace"
)

type App struct {
	stdout        io.Writer
	stderr        io.Writer
	detectPaths   func() (caoruntime.Paths, error)
	runnerFactory func() command.Runner
}

type stringListFlag []string

func New(stdout, stderr io.Writer) *App {
	return &App{
		stdout:      stdout,
		stderr:      stderr,
		detectPaths: caoruntime.Detect,
		runnerFactory: func() command.Runner {
			return command.ExecRunner{}
		},
	}
}

func (a *App) Run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.runOverview()
	}
	if isHelpToken(args[0]) {
		return a.runHelp(args[1:])
	}

	paths, err := a.detectPaths()
	if err != nil {
		fmt.Fprintf(a.stderr, "detect runtime paths: %v\n", err)
		return 1
	}
	runner := a.runnerFactory()
	eng := engine.New(paths, runner)

	switch args[0] {
	case "doctor":
		return a.runDoctor(ctx, args[1:], paths, runner)
	case "fetch":
		return a.runFetch(ctx, args[1:], paths, runner)
	case "init":
		return a.runInit(args[1:], paths)
	case "workspace":
		return a.runWorkspace(ctx, args[1:], paths, runner)
	case "plan":
		return a.runScopedCommand(ctx, helpCatalog["plan"], args[1:], paths, runner, func(filters []string) error {
			plan, diffItems, _, err := eng.Diff(ctx, filters)
			if err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, formatPlan(detectOutputStyle(a.stdout), plan, diffItems))
			return nil
		})
	case "diff":
		return a.runScopedCommand(ctx, helpCatalog["diff"], args[1:], paths, runner, func(filters []string) error {
			plan, diffItems, _, err := eng.Diff(ctx, filters)
			if err != nil {
				return err
			}
			style := detectOutputStyle(a.stdout)
			fmt.Fprintln(a.stdout, formatPlan(style, plan, diffItems))
			fmt.Fprintln(a.stdout, "")
			fmt.Fprintln(a.stdout, formatDiffSummary(style, engine.Summary(diffItems)))
			return nil
		})
	case "apply":
		return a.runApply(ctx, args[1:], eng)
	case "prune":
		return a.runScopedCommand(ctx, helpCatalog["prune"], args[1:], paths, runner, func(filters []string) error {
			removed, err := eng.Prune(ctx, filters)
			if err != nil {
				return err
			}
			if len(removed) == 0 {
				style := detectOutputStyle(a.stdout)
				if style.enabled {
					fmt.Fprintln(a.stdout, style.muted("Nothing to prune."))
				} else {
					fmt.Fprintln(a.stdout, "nothing to prune")
				}
				return nil
			}
			for _, item := range removed {
				fmt.Fprintln(a.stdout, item)
			}
			return nil
		})
	case "secrets":
		return a.runSecrets(ctx, args[1:], runner)
	case "list":
		return a.runWorkspace(ctx, []string{"list"}, paths, runner)
	case "help":
		return a.runHelp(args[1:])
	default:
		fmt.Fprintf(a.stderr, "unknown command %q\n\n", args[0])
		a.printGlobalHelp(a.stderr)
		return 1
	}
}

func (a *App) runOverview() int {
	paths, err := a.detectPaths()
	if err != nil {
		fmt.Fprintf(a.stderr, "detect runtime paths: %v\n", err)
		return 1
	}
	infos, err := caoworkspace.List(paths, nil)
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, formatWorkspaceOverview(detectOutputStyle(a.stdout), paths, infos))
	return 0
}

func (a *App) runHelp(args []string) int {
	if len(args) == 0 {
		a.printGlobalHelp(a.stdout)
		return 0
	}
	if command, ok := lookupCommandHelp(args...); ok {
		a.printCommandHelp(a.stdout, command)
		return 0
	}
	for length := len(args); length > 0; length-- {
		if command, ok := lookupCommandHelp(args[:length]...); ok {
			a.printCommandHelp(a.stdout, command)
			return 0
		}
	}
	fmt.Fprintf(a.stderr, "unknown help topic %q\n", strings.Join(args, " "))
	return 1
}

func (a *App) runDoctor(ctx context.Context, args []string, paths caoruntime.Paths, runner command.Runner) int {
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["doctor"])
		return 0
	}
	if len(args) > 0 {
		fmt.Fprintln(a.stderr, "doctor does not take extra arguments")
		fmt.Fprintln(a.stderr, "")
		a.printCommandHelp(a.stderr, helpCatalog["doctor"])
		return 1
	}
	statuses, err := deps.Check(ctx, paths, runner, []deps.RequirementSpec{
		{Requirement: deps.RequirementGit},
		{Requirement: deps.RequirementSops},
		{Requirement: deps.RequirementAge, Optional: true},
		{Requirement: deps.RequirementAgeKey, Optional: true},
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "doctor: %v\n", err)
		return 1
	}
	for _, line := range formatDoctor(detectOutputStyle(a.stdout), statuses, paths) {
		fmt.Fprintln(a.stdout, line)
	}
	return 0
}

func (a *App) runFetch(ctx context.Context, args []string, paths caoruntime.Paths, runner command.Runner) int {
	help := helpCatalog["fetch"]
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, help)
		return 0
	}
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(a.stderr, "usage: cao fetch <repo> [workspace-name]")
		fmt.Fprintln(a.stderr, "")
		a.printCommandHelp(a.stderr, help)
		return 1
	}
	repo := args[0]
	name := ""
	if len(args) == 2 {
		name = args[1]
	}
	if err := a.requireDependencies(ctx, help.Name, paths, runner, []deps.RequirementSpec{
		{Requirement: deps.RequirementGit},
	}); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	root, err := caoworkspace.Fetch(ctx, paths, runner, repo, name)
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, root)
	return 0
}

func (a *App) runInit(args []string, paths caoruntime.Paths) int {
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["init"])
		return 0
	}
	if len(args) != 1 {
		fmt.Fprintln(a.stderr, "usage: cao init <workspace-name>")
		fmt.Fprintln(a.stderr, "")
		a.printCommandHelp(a.stderr, helpCatalog["init"])
		return 1
	}
	root, err := caoworkspace.Init(paths, args[0])
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, root)
	return 0
}

func (a *App) runWorkspace(ctx context.Context, args []string, paths caoruntime.Paths, runner command.Runner) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["workspace"])
		return 0
	}

	switch args[0] {
	case "list":
		return a.runWorkspaceList(paths, args[1:])
	case "path":
		return a.runWorkspacePath(paths, args[1:])
	case "rename":
		return a.runWorkspaceRename(paths, args[1:])
	case "show":
		return a.runWorkspaceShow(paths, args[1:])
	}

	workspaceName := args[0]
	if len(args) < 3 {
		a.printCommandHelp(a.stderr, helpCatalog["workspace"])
		return 1
	}
	category := args[1]
	action := args[2]
	switch category {
	case "secrets":
		if action == "add" {
			return a.runWorkspaceSecretsAdd(ctx, workspaceName, args[3:], paths, runner)
		}
		if action == "get" {
			return a.runWorkspaceSecretsGet(ctx, workspaceName, args[3:], paths, runner)
		}
	case "files":
		if action == "add" {
			return a.runWorkspaceFilesAdd(workspaceName, args[3:], paths)
		}
	case "command":
		if action == "add" {
			return a.runWorkspaceCommandAdd(workspaceName, args[3:], paths)
		}
	case "publish":
		if action == "add" {
			return a.runWorkspacePublishAdd(workspaceName, args[3:], paths)
		}
	}
	a.printCommandHelp(a.stderr, helpCatalog["workspace"])
	return 1
}

func (a *App) runWorkspaceList(paths caoruntime.Paths, args []string) int {
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["workspace"])
		return 0
	}
	if len(args) > 0 {
		a.printCommandHelp(a.stderr, helpCatalog["workspace"])
		return 1
	}
	infos, err := caoworkspace.List(paths, nil)
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	if len(infos) == 0 {
		style := detectOutputStyle(a.stdout)
		if style.enabled {
			fmt.Fprintln(a.stdout, style.warning("No workspaces."))
		} else {
			fmt.Fprintln(a.stdout, "no workspaces")
		}
		return 0
	}
	style := detectOutputStyle(a.stdout)
	for _, info := range infos {
		fmt.Fprintln(a.stdout, formatWorkspaceListEntry(style, info))
	}
	return 0
}

func (a *App) runWorkspacePath(paths caoruntime.Paths, args []string) int {
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["workspace"])
		return 0
	}
	if len(args) != 1 {
		a.printCommandHelp(a.stderr, helpCatalog["workspace"])
		return 1
	}
	info, err := caoworkspace.Load(paths, args[0])
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, info.Root)
	return 0
}

func (a *App) runWorkspaceRename(paths caoruntime.Paths, args []string) int {
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["workspace rename"])
		return 0
	}
	if len(args) != 2 {
		a.printCommandHelp(a.stderr, helpCatalog["workspace rename"])
		return 1
	}
	root, err := caoworkspace.Rename(paths, args[0], args[1])
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, root)
	return 0
}

func (a *App) runWorkspaceShow(paths caoruntime.Paths, args []string) int {
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["workspace"])
		return 0
	}
	if len(args) != 1 {
		a.printCommandHelp(a.stderr, helpCatalog["workspace"])
		return 1
	}
	info, err := caoworkspace.Load(paths, args[0])
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, formatWorkspaceInfo(detectOutputStyle(a.stdout), info))
	return 0
}

func (a *App) runWorkspaceSecretsAdd(ctx context.Context, workspaceName string, args []string, paths caoruntime.Paths, runner command.Runner) int {
	help := helpCatalog["workspace secrets add"]
	flags := flag.NewFlagSet("cao workspace secrets add", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.Usage = func() { a.printCommandHelp(a.stderr, help) }
	input := flags.String("input", "", "plaintext file to encrypt")
	name := flags.String("name", "", "resource name")
	target := flags.String("target", "", "final materialized path")
	noTarget := flags.Bool("no-target", false, "store without a materialized target")
	format := flags.String("format", "auto", "auto|yaml|json|dotenv|binary")
	recipients := flags.String("age", "", "comma-separated age recipients")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *input == "" {
		fmt.Fprintln(a.stderr, "missing input path")
		fmt.Fprintln(a.stderr, "")
		a.printCommandHelp(a.stderr, help)
		return 1
	}
	if err := a.requireDependencies(ctx, "workspace secrets add", paths, runner, []deps.RequirementSpec{
		{Requirement: deps.RequirementSops},
	}); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	secretPath, resourcePath, err := caoworkspace.AddSecret(ctx, paths, runner, workspaceName, caoworkspace.AddSecretOptions{
		InputPath:     *input,
		Name:          *name,
		Target:        *target,
		NoTarget:      *noTarget,
		Format:        *format,
		AgeRecipients: splitCSV(*recipients),
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	style := detectOutputStyle(a.stdout)
	fmt.Fprintln(a.stdout, formatPathResult(style, "secret", secretPath))
	fmt.Fprintln(a.stdout, formatPathResult(style, "resource", resourcePath))
	return 0
}

func (a *App) runWorkspaceSecretsGet(ctx context.Context, workspaceName string, args []string, paths caoruntime.Paths, runner command.Runner) int {
	help := helpCatalog["workspace secrets get"]
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, help)
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(a.stderr, "missing secret name")
		fmt.Fprintln(a.stderr, "")
		a.printCommandHelp(a.stderr, help)
		return 1
	}

	secretName := strings.TrimSpace(args[0])
	flags := flag.NewFlagSet("cao workspace secrets get", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.Usage = func() { a.printCommandHelp(a.stderr, help) }
	outputPath := flags.String("output", "", "write decrypted secret to a file")
	forceStdout := flags.Bool("stdout", false, "allow printing the secret to stdout when stdout is a terminal")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if secretName == "" || flags.NArg() != 0 {
		a.printCommandHelp(a.stderr, help)
		return 1
	}
	if *outputPath != "" && *forceStdout {
		fmt.Fprintln(a.stderr, "--output and --stdout are mutually exclusive")
		return 1
	}
	if err := a.requireDependencies(ctx, help.Name, paths, runner, []deps.RequirementSpec{
		{Requirement: deps.RequirementSops},
		{Requirement: deps.RequirementAgeKey},
	}); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	info, resource, err := caoworkspace.FindSecret(paths, workspaceName, secretName)
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	sourcePath := filepath.Join(info.Root, filepath.FromSlash(resource.Manifest.Source))
	cleartext, err := secrets.Decrypt(ctx, runner, sourcePath)
	if err != nil {
		fmt.Fprintf(a.stderr, "decrypt secret %s: %v\n", resource.Manifest.Name, err)
		return 1
	}

	if *outputPath != "" {
		targetPath := paths.Expand(*outputPath)
		if err := fsutil.WriteFileAtomicWithDirMode(targetPath, cleartext, 0o600, 0o700); err != nil {
			fmt.Fprintf(a.stderr, "write decrypted secret to %s: %v\n", targetPath, err)
			return 1
		}
		fmt.Fprintln(a.stdout, formatPathResult(detectOutputStyle(a.stdout), "secret", targetPath))
		return 0
	}

	if !*forceStdout && writerIsTerminal(a.stdout) {
		fmt.Fprintln(a.stderr, "refusing to print a secret to an interactive terminal; use --stdout to force or --output <path> to write a file")
		return 1
	}
	if _, err := a.stdout.Write(cleartext); err != nil {
		fmt.Fprintf(a.stderr, "write secret output: %v\n", err)
		return 1
	}
	return 0
}

func (a *App) runWorkspaceFilesAdd(workspaceName string, args []string, paths caoruntime.Paths) int {
	help := helpCatalog["workspace files add"]
	flags := flag.NewFlagSet("cao workspace files add", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.Usage = func() { a.printCommandHelp(a.stderr, help) }
	input := flags.String("input", "", "file to copy")
	name := flags.String("name", "", "resource name")
	target := flags.String("target", "", "final materialized path")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *input == "" || *target == "" {
		fmt.Fprintln(a.stderr, "missing input or target")
		fmt.Fprintln(a.stderr, "")
		a.printCommandHelp(a.stderr, help)
		return 1
	}
	filePath, resourcePath, err := caoworkspace.AddFile(paths, workspaceName, caoworkspace.AddFileOptions{
		InputPath: *input,
		Name:      *name,
		Target:    *target,
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	style := detectOutputStyle(a.stdout)
	fmt.Fprintln(a.stdout, formatPathResult(style, "file", filePath))
	fmt.Fprintln(a.stdout, formatPathResult(style, "resource", resourcePath))
	return 0
}

func (a *App) runWorkspacePublishAdd(workspaceName string, args []string, paths caoruntime.Paths) int {
	help := helpCatalog["workspace publish add"]
	flags := flag.NewFlagSet("cao workspace publish add", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.Usage = func() { a.printCommandHelp(a.stderr, help) }
	input := flags.String("input", "", "script or executable to publish")
	name := flags.String("name", "", "published command name")
	targetDir := flags.String("target-dir", "", "publish directory override")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *input == "" {
		fmt.Fprintln(a.stderr, "missing input path")
		fmt.Fprintln(a.stderr, "")
		a.printCommandHelp(a.stderr, help)
		return 1
	}
	binPath, resourcePath, err := caoworkspace.AddPublish(paths, workspaceName, caoworkspace.AddPublishOptions{
		InputPath: *input,
		Name:      *name,
		TargetDir: *targetDir,
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	style := detectOutputStyle(a.stdout)
	fmt.Fprintln(a.stdout, formatPathResult(style, "publish", binPath))
	fmt.Fprintln(a.stdout, formatPathResult(style, "resource", resourcePath))
	return 0
}

func (a *App) runWorkspaceCommandAdd(workspaceName string, args []string, paths caoruntime.Paths) int {
	help := helpCatalog["workspace command add"]
	flags := flag.NewFlagSet("cao workspace command add", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.Usage = func() { a.printCommandHelp(a.stderr, help) }
	name := flags.String("name", "", "published command name")
	execName := flags.String("exec", "", "binary to execute from the wrapper")
	shellSnippet := flags.String("shell", "", "shell snippet to use as the wrapper body")
	targetDir := flags.String("target-dir", "", "publish directory override")
	var envVars stringListFlag
	var commandArgs stringListFlag
	flags.Var(&envVars, "env", "environment variable assignment")
	flags.Var(&commandArgs, "arg", "command argument")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		a.printCommandHelp(a.stderr, help)
		return 1
	}
	commandPath, resourcePath, err := caoworkspace.AddCommand(paths, workspaceName, caoworkspace.AddCommandOptions{
		Name:      *name,
		Exec:      *execName,
		Shell:     *shellSnippet,
		Env:       append([]string(nil), envVars...),
		Args:      append([]string(nil), commandArgs...),
		TargetDir: *targetDir,
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	style := detectOutputStyle(a.stdout)
	fmt.Fprintln(a.stdout, formatPathResult(style, "command", commandPath))
	fmt.Fprintln(a.stdout, formatPathResult(style, "resource", resourcePath))
	return 0
}

func (a *App) runApply(ctx context.Context, args []string, eng *engine.Engine) int {
	help := helpCatalog["apply"]
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, help)
		return 0
	}
	flags := flag.NewFlagSet(help.Name, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.Usage = func() { a.printCommandHelp(a.stderr, help) }
	var filters stringListFlag
	noPrune := flags.Bool("no-prune", false, "skip removal of stale managed files")
	flags.Var(&filters, "workspace", "workspace filter")
	flags.Var(&filters, "w", "workspace filter")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		a.printCommandHelp(a.stderr, help)
		return 1
	}
	if err := a.requireSecretReadDependencies(ctx, help.Name, filters, eng.Paths, eng.Runner); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	plan, diffItems, err := eng.ApplyWithOptions(ctx, filters, engine.ApplyOptions{Prune: !*noPrune})
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, formatPlan(detectOutputStyle(a.stdout), plan, diffItems))
	return 0
}

func (a *App) runScopedCommand(ctx context.Context, help commandHelp, args []string, paths caoruntime.Paths, runner command.Runner, fn func([]string) error) int {
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, help)
		return 0
	}
	flags := flag.NewFlagSet(help.Name, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.Usage = func() { a.printCommandHelp(a.stderr, help) }
	var filters stringListFlag
	flags.Var(&filters, "workspace", "workspace filter")
	flags.Var(&filters, "w", "workspace filter")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 {
		a.printCommandHelp(a.stderr, help)
		return 1
	}
	if err := a.requireSecretReadDependencies(ctx, help.Name, filters, paths, runner); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if err := fn(filters); err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			fmt.Fprintf(a.stderr, "%s\n", pathErr)
		} else {
			fmt.Fprintf(a.stderr, "%v\n", err)
		}
		return 1
	}
	return 0
}

func (a *App) runSecrets(ctx context.Context, args []string, runner command.Runner) int {
	if len(args) == 0 || isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["secrets"])
		return 0
	}
	switch args[0] {
	case "encrypt":
		return a.runSecretsEncrypt(ctx, args[1:], runner)
	default:
		fmt.Fprintf(a.stderr, "unknown secrets subcommand %q\n\n", args[0])
		a.printCommandHelp(a.stderr, helpCatalog["secrets"])
		return 1
	}
}

func (a *App) runSecretsEncrypt(ctx context.Context, args []string, runner command.Runner) int {
	if len(args) > 0 && isHelpToken(args[0]) {
		a.printCommandHelp(a.stdout, helpCatalog["secrets encrypt"])
		return 0
	}
	flags := flag.NewFlagSet("cao secrets encrypt", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	flags.Usage = func() { a.printCommandHelp(a.stderr, helpCatalog["secrets encrypt"]) }

	input := flags.String("input", "", "plaintext file to encrypt")
	output := flags.String("output", "", "encrypted output path")
	recipients := flags.String("age", "", "comma-separated age recipients")
	format := flags.String("format", "auto", "auto|yaml|json|dotenv|binary")
	inPlace := flags.Bool("in-place", false, "replace the input file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if *input == "" {
		fmt.Fprintln(a.stderr, "missing input path")
		fmt.Fprintln(a.stderr, "")
		a.printCommandHelp(a.stderr, helpCatalog["secrets encrypt"])
		return 1
	}
	if err := a.requireDependencies(ctx, helpCatalog["secrets encrypt"].Name, caoruntime.Paths{}, runner, []deps.RequirementSpec{
		{Requirement: deps.RequirementSops},
	}); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	outputPath, err := secrets.Encrypt(ctx, runner, secrets.EncryptOptions{
		InputPath:     *input,
		OutputPath:    *output,
		InPlace:       *inPlace,
		AgeRecipients: splitCSV(*recipients),
		Format:        *format,
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintln(a.stdout, formatWrittenFile(detectOutputStyle(a.stdout), outputPath))
	return 0
}

func (a *App) requireDependencies(ctx context.Context, commandName string, paths caoruntime.Paths, runner command.Runner, specs []deps.RequirementSpec) error {
	statuses, err := deps.Check(ctx, paths, runner, specs)
	if err != nil {
		return err
	}
	if len(deps.BlockingProblems(statuses)) == 0 {
		return nil
	}
	return errors.New(formatPreflight(detectOutputStyle(a.stderr), commandName, statuses))
}

func (a *App) requireSecretReadDependencies(ctx context.Context, commandName string, filters []string, paths caoruntime.Paths, runner command.Runner) error {
	hasSecrets, err := deps.HasSecretResources(paths, filters)
	if err != nil {
		return err
	}
	if !hasSecrets {
		return nil
	}
	return a.requireDependencies(ctx, commandName, paths, runner, []deps.RequirementSpec{
		{Requirement: deps.RequirementSops},
		{Requirement: deps.RequirementAgeKey},
	})
}

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("workspace name cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func writerIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
