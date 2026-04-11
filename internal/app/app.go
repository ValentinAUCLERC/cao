package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/valentin/cao/internal/command"
	"github.com/valentin/cao/internal/deps"
	"github.com/valentin/cao/internal/engine"
	caoruntime "github.com/valentin/cao/internal/runtime"
	"github.com/valentin/cao/internal/secrets"
	caoworkspace "github.com/valentin/cao/internal/workspace"
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
		a.printGlobalHelp(a.stdout)
		return 0
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
			fmt.Fprintln(a.stdout, engine.FormatPlan(plan, diffItems))
			return nil
		})
	case "diff":
		return a.runScopedCommand(ctx, helpCatalog["diff"], args[1:], paths, runner, func(filters []string) error {
			plan, diffItems, _, err := eng.Diff(ctx, filters)
			if err != nil {
				return err
			}
			fmt.Fprintln(a.stdout, engine.FormatPlan(plan, diffItems))
			fmt.Fprintln(a.stdout, "")
			for _, key := range sortedKeys(engine.Summary(diffItems)) {
				fmt.Fprintf(a.stdout, "%s: %d\n", key, engine.Summary(diffItems)[key])
			}
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
				fmt.Fprintln(a.stdout, "nothing to prune")
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
	results, err := engine.Doctor(ctx, paths, runner)
	if err != nil {
		fmt.Fprintf(a.stderr, "doctor: %v\n", err)
		return 1
	}
	for _, line := range results {
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
		fmt.Fprintln(a.stdout, "no workspaces")
		return 0
	}
	for _, info := range infos {
		if info.Problem != "" {
			fmt.Fprintf(a.stdout, "%s (invalid: %s)\n", info.Name, info.Problem)
			continue
		}
		fmt.Fprintln(a.stdout, info.Name)
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
	fmt.Fprintf(a.stdout, "workspace: %s\n", info.Name)
	fmt.Fprintf(a.stdout, "path: %s\n", info.Root)
	if info.Problem != "" {
		fmt.Fprintf(a.stdout, "status: invalid (%s)\n", info.Problem)
		return 0
	}
	fmt.Fprintln(a.stdout, "status: valid")
	if info.Manifest.Description != "" {
		fmt.Fprintf(a.stdout, "description: %s\n", info.Manifest.Description)
	}
	if len(info.Manifest.Platforms) > 0 {
		fmt.Fprintf(a.stdout, "platforms: %s\n", strings.Join(info.Manifest.Platforms, ", "))
	}
	fmt.Fprintf(a.stdout, "resources: %d\n", len(info.Resources))
	for _, resource := range info.Resources {
		target := resource.Manifest.Target
		if target == "" {
			target = "(default)"
		}
		fmt.Fprintf(a.stdout, "  - %s %s -> %s\n", resource.Manifest.Kind, resource.Manifest.Name, target)
	}
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
	format := flags.String("format", "auto", "auto|yaml|json|dotenv|binary|kubeconfig")
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
		Format:        *format,
		AgeRecipients: splitCSV(*recipients),
	})
	if err != nil {
		fmt.Fprintf(a.stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintf(a.stdout, "secret: %s\nresource: %s\n", secretPath, resourcePath)
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
	fmt.Fprintf(a.stdout, "file: %s\nresource: %s\n", filePath, resourcePath)
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
	fmt.Fprintf(a.stdout, "publish: %s\nresource: %s\n", binPath, resourcePath)
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
	fmt.Fprintf(a.stdout, "command: %s\nresource: %s\n", commandPath, resourcePath)
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
	fmt.Fprintln(a.stdout, engine.FormatPlan(plan, diffItems))
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
	fmt.Fprintf(a.stdout, "encrypted file written to %s\n", outputPath)
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
	return errors.New(deps.FormatPreflight(commandName, statuses))
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
