package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWithoutArgsShowsWorkspaceOverview(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "perso", "workspace.yaml"), "name: perso\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "perso", "resources", "secret.yaml"), "kind: secret\nname: kubeconfig\nsource: secrets/kubeconfig.enc.yaml\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "perso", "resources", "file.yaml"), "kind: file\nname: gitconfig\nsource: files/gitconfig\ntarget: ~/.gitconfig\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "perso", "resources", "publish.yaml"), "kind: publish\nname: devbox\nsource: bin/devbox\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr, paths, &fakeDependencyRunner{})
	code := app.Run(context.Background(), nil)
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}

	output := stdout.String()
	if !strings.Contains(output, "loaded workspaces: 1") {
		t.Fatalf("expected overview header in stdout, got %q", output)
	}
	if !strings.Contains(output, "workspace: perso") {
		t.Fatalf("expected workspace entry in stdout, got %q", output)
	}
	if !strings.Contains(output, "secret kubeconfig -> (stored only)") {
		t.Fatalf("expected stored-only secret in stdout, got %q", output)
	}
	if !strings.Contains(output, "file gitconfig -> "+filepath.Join(paths.Home, ".gitconfig")) {
		t.Fatalf("expected file target in stdout, got %q", output)
	}
	if !strings.Contains(output, "devbox -> "+filepath.Join(paths.BinDir, "devbox")) {
		t.Fatalf("expected command target in stdout, got %q", output)
	}
	if !strings.Contains(output, "help: cao --help") {
		t.Fatalf("expected help hint in stdout, got %q", output)
	}
	if strings.Contains(output, "cao composes dotfiles") {
		t.Fatalf("expected overview instead of global help, got %q", output)
	}
}

func TestRunWorkspaceSecretsGetWritesStoredOnlySecretToStdout(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "workspace.yaml"), "name: work\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "resources", "secret.yaml"), "kind: secret\nname: token\nsource: secrets/token.enc.yaml\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "secrets", "token.enc.yaml"), "encrypted\n")
	writeTestFile(t, filepath.Join(paths.ConfigHome, "sops", "age", "keys.txt"), "AGE-SECRET-KEY-1...\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := newTestApp(&stdout, &stderr, paths, &fakeDependencyRunner{})
	code := app.Run(context.Background(), []string{"workspace", "work", "secrets", "get", "token"})
	if code != 0 {
		t.Fatalf("expected success, got %d with stderr %q", code, stderr.String())
	}
	if stdout.String() != "secret=value\n" {
		t.Fatalf("expected raw secret on stdout, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestRunWorkspaceSecretsGetAcceptsUnnormalizedSecretName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "workspace.yaml"), "name: work\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "resources", "secret.yaml"), "kind: secret\nname: saas-pprod-mysql-root-pwd\nsource: secrets/saas-pprod-mysql-root-pwd.enc\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "secrets", "saas-pprod-mysql-root-pwd.enc"), "encrypted\n")
	writeTestFile(t, filepath.Join(paths.ConfigHome, "sops", "age", "keys.txt"), "AGE-SECRET-KEY-1...\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := newTestApp(&stdout, &stderr, paths, &fakeDependencyRunner{})
	code := app.Run(context.Background(), []string{"workspace", "work", "secrets", "get", "SAAS_PPROD_MYSQL_ROOT_PWD"})
	if code != 0 {
		t.Fatalf("expected success, got %d with stderr %q", code, stderr.String())
	}
	if stdout.String() != "secret=value\n" {
		t.Fatalf("expected raw secret on stdout, got %q", stdout.String())
	}
}

func TestRunWorkspaceSecretsGetCanWriteToFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "workspace.yaml"), "name: work\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "resources", "secret.yaml"), "kind: secret\nname: token\nsource: secrets/token.enc.yaml\ntarget: ~/.config/token\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "secrets", "token.enc.yaml"), "encrypted\n")
	writeTestFile(t, filepath.Join(paths.ConfigHome, "sops", "age", "keys.txt"), "AGE-SECRET-KEY-1...\n")

	outputPath := filepath.Join(root, "out", "token.txt")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := newTestApp(&stdout, &stderr, paths, &fakeDependencyRunner{})
	code := app.Run(context.Background(), []string{"workspace", "work", "secrets", "get", "token", "--output", outputPath})
	if code != 0 {
		t.Fatalf("expected success, got %d with stderr %q", code, stderr.String())
	}

	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if string(content) != "secret=value\n" {
		t.Fatalf("unexpected output file content %q", string(content))
	}
	if !strings.Contains(stdout.String(), outputPath) {
		t.Fatalf("expected success output to mention %s, got %q", outputPath, stdout.String())
	}
}

func TestRunWorkspaceSecretsAddSupportsInlineValue(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "workspace.yaml"), "name: work\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := newTestApp(&stdout, &stderr, paths, &fakeDependencyRunner{})
	code := app.Run(context.Background(), []string{
		"workspace", "work", "secrets", "add",
		"--name", "mysql-root-password",
		"--value", "supersecret",
		"--no-target",
	})
	if code != 0 {
		t.Fatalf("expected success, got %d with stderr %q", code, stderr.String())
	}

	resourcePath := filepath.Join(paths.WorkspacesDir, "work", "resources", "secret-mysql-root-password.yaml")
	content, err := os.ReadFile(resourcePath)
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if strings.Contains(string(content), "target:") {
		t.Fatalf("expected stored-only manifest without target, got %q", string(content))
	}
	if !strings.Contains(stdout.String(), filepath.Join(paths.WorkspacesDir, "work", "secrets", "mysql-root-password.enc")) {
		t.Fatalf("expected created secret path in stdout, got %q", stdout.String())
	}
}

func TestRunWorkspaceSecretsAddSupportsStdin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "workspace.yaml"), "name: work\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := newTestApp(&stdout, &stderr, paths, &fakeDependencyRunner{})
	app.stdin = strings.NewReader("supersecret-from-stdin")
	code := app.Run(context.Background(), []string{
		"workspace", "work", "secrets", "add",
		"--name", "mysql-root-password",
		"--stdin",
		"--no-target",
	})
	if code != 0 {
		t.Fatalf("expected success, got %d with stderr %q", code, stderr.String())
	}

	if _, err := os.Stat(filepath.Join(paths.WorkspacesDir, "work", "secrets", "mysql-root-password.enc")); err != nil {
		t.Fatalf("expected encrypted secret file, err=%v", err)
	}
}

func TestRunWithoutArgsShowsNoWorkspacesHint(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := newTestApp(&stdout, &stderr, paths, &fakeDependencyRunner{})
	code := app.Run(context.Background(), nil)
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}

	output := stdout.String()
	if !strings.Contains(output, "status: no workspaces found") {
		t.Fatalf("expected empty-workspace hint, got %q", output)
	}
	if !strings.Contains(output, "hint: cao init <name> | cao fetch <repo> [workspace-name]") {
		t.Fatalf("expected setup hint, got %q", output)
	}
}

func TestRunHelpFlagStillShowsGlobalHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := New(&stdout, &stderr).Run(context.Background(), []string{"--help"})
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}

	output := stdout.String()
	if !strings.Contains(output, "cao composes dotfiles") {
		t.Fatalf("expected global help in stdout, got %q", output)
	}
	if !strings.Contains(output, "Top-Level Commands") {
		t.Fatalf("expected command catalog in stdout, got %q", output)
	}
}

func TestHelpCommandShowsDetailedCommandHelp(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := New(&stdout, &stderr).Run(context.Background(), []string{"help", "plan"})
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}

	output := stdout.String()
	if !strings.Contains(output, "cao plan [--workspace <name>]...") {
		t.Fatalf("expected workspace-scoped plan usage, got %q", output)
	}
	if !strings.Contains(output, "Build the desired state from all active workspaces") {
		t.Fatalf("expected plan summary, got %q", output)
	}
}

func TestScopedHelpFlagShowsDetailedHelpOnStdout(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := New(&stdout, &stderr).Run(context.Background(), []string{"apply", "--help"})
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}

	output := stdout.String()
	if !strings.Contains(output, "cao apply [--workspace <name>]... [--no-prune]") {
		t.Fatalf("expected apply usage, got %q", output)
	}
	if !strings.Contains(output, "--workspace <name>") {
		t.Fatalf("expected workspace option, got %q", output)
	}
	if !strings.Contains(output, "--no-prune") {
		t.Fatalf("expected no-prune option, got %q", output)
	}
}

func TestHelpCommandShowsWorkspaceCommandAddHelp(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := New(&stdout, &stderr).Run(context.Background(), []string{"help", "workspace", "command", "add"})
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}

	output := stdout.String()
	if !strings.Contains(output, "cao workspace <name> command add --name <command>") {
		t.Fatalf("expected command add usage, got %q", output)
	}
	if !strings.Contains(output, "--exec <binary>") || !strings.Contains(output, "--env KEY=VALUE") {
		t.Fatalf("expected command add options, got %q", output)
	}
}

func TestHelpCommandShowsWorkspaceRenameHelp(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := New(&stdout, &stderr).Run(context.Background(), []string{"help", "workspace", "rename"})
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}

	output := stdout.String()
	if !strings.Contains(output, "cao workspace rename <old-name> <new-name>") {
		t.Fatalf("expected workspace rename usage, got %q", output)
	}
}
