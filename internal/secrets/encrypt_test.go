package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valentin/cao/internal/command"
)

type fakeRunner struct {
	name string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ command.RunOptions) ([]byte, error) {
	f.name = name
	f.args = append([]string(nil), args...)
	return []byte("ok"), nil
}

func TestDefaultEncryptedPath(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/tmp/config.yaml": "/tmp/config.enc.yaml",
		"/tmp/config.yml":  "/tmp/config.enc.yml",
		"/tmp/app.json":    "/tmp/app.enc.json",
		"/tmp/.env":        "/tmp/.env.enc",
		"/tmp/plain.txt":   "/tmp/plain.txt.enc",
	}
	for input, want := range cases {
		if got := DefaultEncryptedPath(input); got != want {
			t.Fatalf("input %s: got %s want %s", input, got, want)
		}
	}
}

func TestEncryptBuildsSopsCommand(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	input := filepath.Join(root, "work.kubeconfig.yaml")
	if err := os.WriteFile(input, []byte("apiVersion: v1\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	runner := &fakeRunner{}
	output, err := Encrypt(context.Background(), runner, EncryptOptions{
		InputPath:     input,
		AgeRecipients: []string{"age1abc", "age1def"},
	})
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if output != filepath.Join(root, "work.kubeconfig.enc.yaml") {
		t.Fatalf("unexpected output path %s", output)
	}
	commandLine := runner.name + " " + strings.Join(runner.args, " ")
	if !strings.Contains(commandLine, "--age age1abc,age1def") {
		t.Fatalf("expected recipients in command: %s", commandLine)
	}
	if !strings.Contains(commandLine, "--input-type yaml --output-type yaml") {
		t.Fatalf("expected yaml format in command: %s", commandLine)
	}
	if !strings.Contains(commandLine, "--output "+output) {
		t.Fatalf("expected output in command: %s", commandLine)
	}
}

func TestEncryptTreatsKubeconfigAsYAMLForSops(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	input := filepath.Join(root, "cluster.yaml")
	if err := os.WriteFile(input, []byte("apiVersion: v1\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	runner := &fakeRunner{}
	if _, err := Encrypt(context.Background(), runner, EncryptOptions{
		InputPath: input,
		Format:    "kubeconfig",
	}); err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	commandLine := runner.name + " " + strings.Join(runner.args, " ")
	if !strings.Contains(commandLine, "--input-type yaml --output-type yaml") {
		t.Fatalf("expected yaml format in command: %s", commandLine)
	}
	if strings.Contains(commandLine, "--input-type kubeconfig") {
		t.Fatalf("did not expect unsupported kubeconfig format in command: %s", commandLine)
	}
}

func TestEncryptRejectsConflictingOutputOptions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	input := filepath.Join(root, "plain.txt")
	if err := os.WriteFile(input, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	_, err := Encrypt(context.Background(), &fakeRunner{}, EncryptOptions{
		InputPath:  input,
		OutputPath: filepath.Join(root, "plain.enc"),
		InPlace:    true,
	})
	if err == nil {
		t.Fatalf("expected conflict error")
	}
}
