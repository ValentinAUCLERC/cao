package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/valentin/cao/internal/command"
	"github.com/valentin/cao/internal/runtime"
)

type fakeDependencyRunner struct {
	missing map[string]error
}

func (f *fakeDependencyRunner) Run(_ context.Context, name string, args []string, _ command.RunOptions) ([]byte, error) {
	if err, ok := f.missing[name]; ok {
		return nil, err
	}
	if name == "sops" && len(args) == 2 && args[0] == "decrypt" {
		return []byte("secret=value\n"), nil
	}
	return []byte(name + " version 1.0.0\n"), nil
}

func TestRunFetchReportsMissingGitDependency(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := &fakeDependencyRunner{
		missing: map[string]error{
			"git": exec.ErrNotFound,
		},
	}

	app := newTestApp(&stdout, &stderr, paths, runner)
	code := app.Run(context.Background(), []string{"fetch", "git@github.com:me/workspace.git", "work"})
	if code != 1 {
		t.Fatalf("expected failure, got %d", code)
	}

	output := stderr.String()
	if !strings.Contains(output, "git is not installed") {
		t.Fatalf("expected git dependency message, got %q", output)
	}
	if !strings.Contains(output, "cao doctor") {
		t.Fatalf("expected doctor hint, got %q", output)
	}
}

func TestRunApplySkipsSecretPreflightWhenNoSecretsExist(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "workspace.yaml"), "name: work\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "resources", "file.yaml"), "kind: file\nname: profile\nsource: files/profile\ntarget: ~/.profile\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "files", "profile"), "profile\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := &fakeDependencyRunner{
		missing: map[string]error{
			"sops": exec.ErrNotFound,
		},
	}

	app := newTestApp(&stdout, &stderr, paths, runner)
	code := app.Run(context.Background(), []string{"apply"})
	if code != 0 {
		t.Fatalf("expected success, got %d with stderr %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "create") {
		t.Fatalf("expected apply plan output, got %q", stdout.String())
	}
}

func TestRunApplyReportsMissingAgeKeyForSecretWorkspaces(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testRuntimePaths(root)
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "workspace.yaml"), "name: work\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "resources", "secret.yaml"), "kind: secret\nname: token\nsource: secrets/token.enc.yaml\n")
	writeTestFile(t, filepath.Join(paths.WorkspacesDir, "work", "secrets", "token.enc.yaml"), "encrypted\n")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := newTestApp(&stdout, &stderr, paths, &fakeDependencyRunner{})
	code := app.Run(context.Background(), []string{"apply"})
	if code != 1 {
		t.Fatalf("expected failure, got %d", code)
	}

	output := stderr.String()
	if !strings.Contains(output, "no age key file was detected") {
		t.Fatalf("expected missing age key message, got %q", output)
	}
	if !strings.Contains(output, "age-keygen -o") {
		t.Fatalf("expected age key fix hint, got %q", output)
	}
}

func newTestApp(stdout, stderr *bytes.Buffer, paths runtime.Paths, runner command.Runner) *App {
	app := New(stdout, stderr)
	app.detectPaths = func() (runtime.Paths, error) { return paths, nil }
	app.runnerFactory = func() command.Runner { return runner }
	return app
}

func testRuntimePaths(root string) runtime.Paths {
	home := filepath.Join(root, "home")
	configHome := filepath.Join(home, ".config")
	cacheHome := filepath.Join(home, ".cache")
	stateHome := filepath.Join(home, ".local", "state")
	dataHome := filepath.Join(home, ".local", "share")
	return runtime.Paths{
		Home:            home,
		ConfigHome:      configHome,
		CacheHome:       cacheHome,
		StateHome:       stateHome,
		DataHome:        dataHome,
		RuntimeDir:      filepath.Join(root, "run"),
		AppConfigDir:    filepath.Join(configHome, "cao"),
		AppCacheDir:     filepath.Join(cacheHome, "cao"),
		AppStateDir:     filepath.Join(stateHome, "cao"),
		AppDataDir:      filepath.Join(dataHome, "cao"),
		WorkspacesDir:   filepath.Join(dataHome, "cao", "workspaces"),
		AppGeneratedDir: filepath.Join(configHome, "cao", "generated"),
		BinDir:          filepath.Join(home, ".local", "bin"),
		StateFile:       filepath.Join(stateHome, "cao", "state.json"),
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
