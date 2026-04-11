package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunWithoutArgsShowsGlobalHelp(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := New(&stdout, &stderr).Run(context.Background(), nil)
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}

	output := stdout.String()
	if !strings.Contains(output, "cao composes dotfiles") {
		t.Fatalf("expected global help in stdout, got %q", output)
	}
	if !strings.Contains(output, "workspace") || !strings.Contains(output, "fetch") {
		t.Fatalf("expected workspace-first command catalog, got %q", output)
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
