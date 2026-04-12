package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestHelpSecretsEncryptShowsDetailedHelp(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := New(&stdout, &stderr).Run(context.Background(), []string{"help", "secrets", "encrypt"})
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "cao secrets encrypt --input <path>") {
		t.Fatalf("expected encrypt usage, got %q", output)
	}
	if !strings.Contains(output, "--format <auto|yaml|json|dotenv|binary>") {
		t.Fatalf("expected format option, got %q", output)
	}
}

func TestHelpWorkspaceSecretsGetShowsDetailedHelp(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := New(&stdout, &stderr).Run(context.Background(), []string{"help", "workspace", "secrets", "get"})
	if code != 0 {
		t.Fatalf("expected success, got %d", code)
	}
	output := stdout.String()
	if !strings.Contains(output, "cao workspace <name> secrets get <secret-name>") {
		t.Fatalf("expected get usage, got %q", output)
	}
	if !strings.Contains(output, "--output <path>") || !strings.Contains(output, "--stdout") {
		t.Fatalf("expected get options, got %q", output)
	}
}
