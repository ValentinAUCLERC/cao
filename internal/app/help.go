package app

import (
	"fmt"
	"io"
	"strings"
)

type optionHelp struct {
	Flag        string
	Description string
}

type commandHelp struct {
	Name        string
	Summary     string
	Usage       string
	Description string
	Options     []optionHelp
	Examples    []string
}

var topLevelOrder = []string{
	"doctor",
	"fetch",
	"init",
	"workspace",
	"plan",
	"diff",
	"apply",
	"prune",
	"secrets",
	"help",
}

var helpCatalog = map[string]commandHelp{
	"doctor": {
		Name:    "doctor",
		Summary: "Inspect the local environment, dependencies, and the cao base directories.",
		Usage:   "cao doctor",
		Description: "Checks whether the current machine looks ready for cao. " +
			"It reports the detected platform, the cao workspace directory, the state path, " +
			"whether git, sops, and age are available, and short fix hints when something is missing.",
		Examples: []string{"cao doctor"},
	},
	"fetch": {
		Name:    "fetch",
		Summary: "Clone or update one workspace repository under cao's base directory.",
		Usage:   "cao fetch <repo> [workspace-name]",
		Description: "Clones a repository into `~/.local/share/cao/workspaces/<name>` by default. " +
			"If the workspace already exists and points to the same origin, cao fetches and pulls it. " +
			"The fetched workspace becomes active immediately because all local workspaces are part of the desired state.",
		Examples: []string{
			"cao fetch git@github.com:me/cao-work.git work",
			"cao fetch ~/src/cao-perso perso",
		},
	},
	"init": {
		Name:    "init",
		Summary: "Create a new local workspace skeleton under cao's base directory.",
		Usage:   "cao init <workspace-name>",
		Description: "Creates `workspace.yaml`, `.gitignore`, and the starter directories " +
			"`resources/`, `secrets/`, `files/`, and `bin/` inside the new workspace.",
		Examples: []string{
			"cao init work",
			"cao init perso",
		},
	},
	"workspace": {
		Name:    "workspace",
		Summary: "Inspect workspaces or add simple resources to one workspace.",
		Usage:   "cao workspace <list|path|show|<name> ...>",
		Description: "Use `workspace list` to discover available workspaces, `workspace show` to inspect one, " +
			"and `workspace <name> ... add` commands to create simple managed resources without hand-writing manifests.",
		Examples: []string{
			"cao workspace list",
			"cao workspace path work",
			"cao workspace rename perso personal",
			"cao workspace show perso",
			"cao workspace work secrets add --input ~/Downloads/work-kubeconfig.yaml --format yaml --age age1...",
			"cao workspace work secrets add --input .env --name mysql-root-password --no-target --age age1...",
			"cao workspace work secrets get mysql-root-password > /tmp/mysql-root-password",
			"cao workspace perso files add --input ~/.config/myapp/config.json --target ~/.config/myapp/config.json",
			"cao workspace work command add --name kubectl-work --exec kubectl --env 'KUBECONFIG=${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/work/work-kubeconfig'",
			"cao workspace perso publish add --input ./scripts/devbox --name devbox",
		},
	},
	"workspace rename": {
		Name:    "workspace rename",
		Summary: "Rename a workspace and update the workspace-first references cao can track safely.",
		Usage:   "cao workspace rename <old-name> <new-name>",
		Description: "Renames the workspace directory, updates `workspace.yaml`, rewrites simple secret targets that still follow cao's default layout, " +
			"updates tracked state entries to the new workspace name, and rewrites generated command wrappers when they reference the old default generated path.",
		Examples: []string{
			"cao workspace rename perso personal",
		},
	},
	"workspace secrets add": {
		Name:    "workspace <name> secrets add",
		Summary: "Encrypt a plaintext file and register it as a managed secret resource.",
		Usage:   "cao workspace <name> secrets add --input <path> [--name <name>] [--target <path> | --no-target] [--format <auto|yaml|json|dotenv|binary>] [--age <recipient[,recipient...]>]",
		Description: "Encrypts the input with SOPS into the workspace `secrets/` directory, " +
			"then creates a `resources/secret-<name>.yaml` file. Use `--target` for a secret that `cao apply` should materialize locally, " +
			"or `--no-target` to keep it stored-only and retrieve it later with `cao workspace <name> secrets get`.",
		Options: []optionHelp{
			{Flag: "--input <path>", Description: "Plaintext file to encrypt and register."},
			{Flag: "--name <name>", Description: "Resource name. If omitted, cao derives it from the input filename."},
			{Flag: "--target <path>", Description: "Final local path where the decrypted file should be materialized."},
			{Flag: "--no-target", Description: "Store the secret in the workspace without materializing it during `cao apply`."},
			{Flag: "--format <auto|yaml|json|dotenv|binary>", Description: "Secret format. `auto` infers from the input filename; use `yaml` for kubeconfig files when you want to force structured encryption."},
			{Flag: "--age <recipient[,recipient...]>", Description: "Comma-separated age recipients. Optional when the workspace repo already has valid `.sops.yaml` rules."},
		},
		Examples: []string{
			"cao workspace work secrets add --input ~/Downloads/work-kubeconfig.yaml --format yaml --age age1...",
			"cao workspace perso secrets add --input .env --target ~/.config/my-app/.env --age age1...",
			"cao workspace work secrets add --input .env --name mysql-root-password --no-target --age age1...",
		},
	},
	"workspace secrets get": {
		Name:    "workspace <name> secrets get",
		Summary: "Decrypt one workspace secret on demand.",
		Usage:   "cao workspace <name> secrets get <secret-name> [--output <path> | --stdout]",
		Description: "Decrypts a managed workspace secret and writes the cleartext either to stdout or to a file. " +
			"When stdout is an interactive terminal, cao refuses by default so you do not leak the secret into your scrollback; use `--stdout` to force it.",
		Options: []optionHelp{
			{Flag: "<secret-name>", Description: "Name of the managed secret resource to decrypt."},
			{Flag: "--output <path>", Description: "Write the decrypted secret to a file with strict permissions instead of stdout."},
			{Flag: "--stdout", Description: "Force writing the decrypted secret to stdout even when stdout is a terminal."},
		},
		Examples: []string{
			"cao workspace work secrets get kubeconfig --output ~/.kube/work-config",
			"cao workspace work secrets get mysql-root-password > /tmp/mysql-root-password",
			"cao workspace work secrets get mysql-root-password --stdout",
		},
	},
	"workspace files add": {
		Name:    "workspace <name> files add",
		Summary: "Copy a regular file into a workspace and register it as a managed file resource.",
		Usage:   "cao workspace <name> files add --input <path> --target <path> [--name <name>]",
		Description: "Copies the input file into the workspace `files/` directory and creates " +
			"a matching `resources/file-<name>.yaml` file that `cao apply` will materialize to the target path.",
		Options: []optionHelp{
			{Flag: "--input <path>", Description: "Source file to copy into the workspace."},
			{Flag: "--target <path>", Description: "Final local path where the file should be materialized."},
			{Flag: "--name <name>", Description: "Resource name. If omitted, cao derives it from the input filename."},
		},
		Examples: []string{
			"cao workspace perso files add --input ~/.config/htop/htoprc --target ~/.config/htop/htoprc",
		},
	},
	"workspace command add": {
		Name:    "workspace <name> command add",
		Summary: "Generate a small published wrapper command without writing a script by hand.",
		Usage:   "cao workspace <name> command add --name <command> [--exec <binary> [--arg <value>]...] [--shell <snippet>] [--env KEY=VALUE]... [--target-dir <path>]",
		Description: "Creates a small bash wrapper in the workspace `bin/` directory and registers it as a publish resource. " +
			"Use `--exec` for simple wrappers around an existing binary, optionally with repeated `--arg` and `--env`. " +
			"Use `--shell` when you want to provide the wrapper body directly.",
		Options: []optionHelp{
			{Flag: "--name <command>", Description: "Published command name."},
			{Flag: "--exec <binary>", Description: "Binary to execute from the generated wrapper."},
			{Flag: "--arg <value>", Description: "Argument to insert before the final passthrough `\"$@\"`. Repeat the flag to add several arguments."},
			{Flag: "--shell <snippet>", Description: "Raw shell snippet to place in the wrapper instead of using `--exec`."},
			{Flag: "--env KEY=VALUE", Description: "Environment variable assignment added before the command. Repeat the flag to add several variables."},
			{Flag: "--target-dir <path>", Description: "Override the publish directory. Defaults to `~/.local/bin`."},
		},
		Examples: []string{
			"cao workspace work command add --name kubectl-work --exec kubectl --env 'KUBECONFIG=${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/work/work-kubeconfig'",
			"cao workspace work command add --name kctx-work --exec kubectl --arg config --arg use-context --arg work-prod",
			"cao workspace perso command add --name k-all --shell 'export KUBECONFIG=\"${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/work/work-kubeconfig:${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/perso/personal-kubeconfig\"; exec kubectl \"$@\"'",
		},
	},
	"workspace publish add": {
		Name:    "workspace <name> publish add",
		Summary: "Copy an executable into a workspace and publish it into the user's bin directory.",
		Usage:   "cao workspace <name> publish add --input <path> [--name <name>] [--target-dir <path>]",
		Description: "Copies the input executable into the workspace `bin/` directory and creates " +
			"a `resources/publish-<name>.yaml` manifest so `cao apply` can install it into `~/.local/bin` or another target directory.",
		Options: []optionHelp{
			{Flag: "--input <path>", Description: "Executable or script to publish."},
			{Flag: "--name <name>", Description: "Published command name. If omitted, cao derives it from the input filename."},
			{Flag: "--target-dir <path>", Description: "Override the publish directory. Defaults to `~/.local/bin`."},
		},
		Examples: []string{
			"cao workspace perso publish add --input ./scripts/devbox --name devbox",
		},
	},
	"plan": {
		Name:    "plan",
		Summary: "Build the desired state from all active workspaces without writing files.",
		Usage:   "cao plan [--workspace <name>]...",
		Description: "Scans the workspace directory, loads all active workspaces, resolves the declared resources, " +
			"detects collisions, decrypts the secrets needed for planning, and prints the operations cao wants to perform.",
		Options: []optionHelp{
			{Flag: "--workspace <name>", Description: "Limit the operation to one workspace. Repeat the flag to target several workspaces."},
		},
		Examples: []string{
			"cao plan",
			"cao plan --workspace work",
			"cao plan --workspace work --workspace perso",
		},
	},
	"diff": {
		Name:    "diff",
		Summary: "Compare the current machine state with the desired state from active workspaces.",
		Usage:   "cao diff [--workspace <name>]...",
		Description: "Builds the desired plan and compares it to both the filesystem and cao's `state.json`. " +
			"It highlights creates, updates, adoptions, no-ops, and prune candidates.",
		Options: []optionHelp{
			{Flag: "--workspace <name>", Description: "Limit the operation to one workspace. Repeat the flag to target several workspaces."},
		},
		Examples: []string{
			"cao diff",
			"cao diff --workspace work",
		},
	},
	"apply": {
		Name:    "apply",
		Summary: "Materialize the desired state locally, prune stale managed files, and update cao's state file.",
		Usage:   "cao apply [--workspace <name>]... [--no-prune]",
		Description: "Applies the resolved plan atomically: files are written, secrets are materialized with strict permissions, " +
			"published executables are installed, and stale managed files are pruned by default.",
		Options: []optionHelp{
			{Flag: "--workspace <name>", Description: "Limit the operation to one workspace. Repeat the flag to target several workspaces."},
			{Flag: "--no-prune", Description: "Keep stale managed files on disk during this apply run."},
		},
		Examples: []string{
			"cao apply",
			"cao apply --workspace work",
			"cao apply --workspace work --no-prune",
		},
	},
	"prune": {
		Name:    "prune",
		Summary: "Explicitly remove managed files that are no longer desired by the active workspaces.",
		Usage:   "cao prune [--workspace <name>]...",
		Description: "Loads the desired state and removes stale managed files recorded in `state.json`. " +
			"`cao apply` already prunes by default, so this command is mainly useful when you want a standalone cleanup pass or when you previously used `--no-prune`. " +
			"When you filter on one or more workspaces, cao only prunes stale entries belonging to those workspaces.",
		Options: []optionHelp{
			{Flag: "--workspace <name>", Description: "Limit the operation to one workspace. Repeat the flag to target several workspaces."},
		},
		Examples: []string{
			"cao prune",
			"cao prune --workspace perso",
		},
	},
	"secrets": {
		Name:    "secrets",
		Summary: "Low-level SOPS helpers that are still useful outside a workspace add flow.",
		Usage:   "cao secrets <subcommand> [options]",
		Description: "The main high-level UX for secrets is `cao workspace <name> secrets add`, " +
			"but `cao secrets encrypt` remains available when you want a direct wrapper around SOPS.",
		Examples: []string{
			"cao secrets encrypt --input plain.yaml --output secret.enc.yaml --age age1...",
		},
	},
	"secrets encrypt": {
		Name:    "secrets encrypt",
		Summary: "Encrypt one plaintext file with SOPS.",
		Usage:   "cao secrets encrypt --input <path> [--output <path>] [--age <recipient[,recipient...]>] [--format <auto|yaml|json|dotenv|binary>] [--in-place]",
		Description: "Encrypts a plaintext file with SOPS. If you pass `--age`, cao forwards those recipients directly. " +
			"If you omit `--age`, SOPS can still use `.sops.yaml` creation rules from the current repository.",
		Options: []optionHelp{
			{Flag: "--input <path>", Description: "Plaintext file to encrypt."},
			{Flag: "--output <path>", Description: "Encrypted output path. If omitted, cao derives a path next to the input file."},
			{Flag: "--age <recipient[,recipient...]>", Description: "Comma-separated age recipients."},
			{Flag: "--format <auto|yaml|json|dotenv|binary>", Description: "Force the SOPS input/output type. `auto` infers from the filename."},
			{Flag: "--in-place", Description: "Replace the input file instead of writing a separate output file."},
		},
		Examples: []string{
			"cao secrets encrypt --input ~/Downloads/work-kubeconfig.yaml --output secret.enc.yaml --age age1...",
		},
	},
	"help": {
		Name:    "help",
		Summary: "Show global help or detailed help for a command.",
		Usage:   "cao help [command...]",
		Description: "Without arguments, prints the full command catalog. With one or more command tokens, " +
			"prints detailed help for that command path such as `workspace secrets add` or `secrets encrypt`.",
		Examples: []string{
			"cao help",
			"cao help workspace",
			"cao help workspace secrets add",
			"cao help secrets encrypt",
		},
	},
}

func (a *App) printGlobalHelp(w io.Writer) {
	style := detectOutputStyle(w)
	fmt.Fprintln(w, style.command("cao")+" composes dotfiles, configs, secrets, and user scripts from the workspaces stored in its own base directory.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, style.heading("Usage"))
	fmt.Fprintln(w, "  "+style.code("cao <command> [options]"))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, style.heading("Top-Level Commands"))
	for _, key := range topLevelOrder {
		command := helpCatalog[key]
		fmt.Fprintf(w, "  %s %s\n", style.command(padRight(command.Name, 10)), command.Summary)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, style.heading("Useful Nested Commands"))
	for _, key := range []string{"workspace rename", "workspace secrets add", "workspace files add", "workspace command add", "workspace publish add", "secrets encrypt"} {
		command := helpCatalog[key]
		fmt.Fprintf(w, "  %s %s\n", style.command(padRight(command.Name, 24)), command.Summary)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, style.heading("Help"))
	fmt.Fprintln(w, "  "+style.code("cao help"))
	fmt.Fprintln(w, "  "+style.code("cao help workspace"))
	fmt.Fprintln(w, "  "+style.code("cao help workspace rename"))
	fmt.Fprintln(w, "  "+style.code("cao help workspace secrets add"))
	fmt.Fprintln(w, "  "+style.code("cao help secrets encrypt"))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, style.heading("Command Details"))
	for index, key := range topLevelOrder {
		if index > 0 {
			fmt.Fprintln(w, "")
		}
		a.printCommandHelp(w, helpCatalog[key])
	}
}

func (a *App) printCommandHelp(w io.Writer, command commandHelp) {
	style := detectOutputStyle(w)
	fmt.Fprintf(w, "%s\n", style.command(command.Name))
	fmt.Fprintf(w, "  %s\n", command.Summary)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, style.heading("Usage"))
	fmt.Fprintf(w, "  %s\n", style.code(command.Usage))
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, style.heading("Description"))
	for _, line := range wrapText(command.Description, 78) {
		fmt.Fprintf(w, "  %s\n", line)
	}
	if len(command.Options) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, style.heading("Options"))
		for _, option := range command.Options {
			fmt.Fprintf(w, "  %s\n", style.code(option.Flag))
			for _, line := range wrapText(option.Description, 74) {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
	}
	if len(command.Examples) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, style.heading("Examples"))
		for _, example := range command.Examples {
			fmt.Fprintf(w, "  %s\n", style.code(example))
		}
	}
}

func lookupCommandHelp(parts ...string) (commandHelp, bool) {
	key := strings.Join(parts, " ")
	command, ok := helpCatalog[key]
	return command, ok
}

func isHelpToken(value string) bool {
	return value == "-h" || value == "--help" || value == "help"
}

func wrapText(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current += " " + word
	}
	lines = append(lines, current)
	return lines
}
