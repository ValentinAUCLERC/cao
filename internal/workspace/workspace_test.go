package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ValentinAUCLERC/cao/internal/command"
	"github.com/ValentinAUCLERC/cao/internal/config"
	"github.com/ValentinAUCLERC/cao/internal/runtime"
	"github.com/ValentinAUCLERC/cao/internal/state"
)

type fakeRunner struct {
	name string
	args []string
	dir  string
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, opts command.RunOptions) ([]byte, error) {
	f.name = name
	f.args = append([]string(nil), args...)
	f.dir = opts.Dir
	if name == "sops" && len(args) > 0 && args[0] == "encrypt" {
		for index := 0; index < len(args)-1; index++ {
			if args[index] == "--output" {
				outputPath := args[index+1]
				if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
					return nil, err
				}
				if err := os.WriteFile(outputPath, []byte("encrypted"), 0o600); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	return []byte("ok"), nil
}

func TestInitCreatesWorkspaceSkeleton(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)

	root, err := Init(paths, "Demo Workspace")
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	if filepath.Base(root) != "demo-workspace" {
		t.Fatalf("unexpected workspace root %s", root)
	}
	for _, item := range []string{
		filepath.Join(root, "workspace.yaml"),
		filepath.Join(root, ".gitignore"),
		filepath.Join(root, "resources", ".gitkeep"),
		filepath.Join(root, "secrets", ".gitkeep"),
		filepath.Join(root, "files", ".gitkeep"),
		filepath.Join(root, "bin", ".gitkeep"),
	} {
		if _, err := os.Stat(item); err != nil {
			t.Fatalf("expected %s to exist: %v", item, err)
		}
	}
}

func TestAddSecretWritesEncryptedFileAndResourceManifest(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)
	root, err := Init(paths, "work")
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	input := filepath.Join(t.TempDir(), "work.kubeconfig.yaml")
	if err := os.WriteFile(input, []byte("apiVersion: v1\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	runner := &fakeRunner{}
	secretPath, resourcePath, err := AddSecret(context.Background(), paths, runner, "work", AddSecretOptions{
		InputPath:     input,
		Format:        "yaml",
		AgeRecipients: []string{"age1example"},
	})
	if err != nil {
		t.Fatalf("add secret: %v", err)
	}

	if secretPath != filepath.Join(root, "secrets", "work-kubeconfig.enc.yaml") {
		t.Fatalf("unexpected secret path %s", secretPath)
	}
	if _, err := os.Stat(secretPath); err != nil {
		t.Fatalf("expected encrypted secret to exist: %v", err)
	}
	resource, err := config.LoadResource(resourcePath)
	if err != nil {
		t.Fatalf("load resource: %v", err)
	}
	if resource.Kind != "secret" || resource.Format != "yaml" {
		t.Fatalf("unexpected resource manifest %#v", resource)
	}
	if resource.Target != "${XDG_CONFIG_HOME}/cao/generated/work/work-kubeconfig" {
		t.Fatalf("unexpected secret target %q", resource.Target)
	}
	commandLine := runner.name + " " + strings.Join(runner.args, " ")
	if !strings.Contains(commandLine, "--input-type yaml --output-type yaml") {
		t.Fatalf("expected secret to be encrypted as yaml, got %s", commandLine)
	}
	if !strings.Contains(commandLine, "--filename-override secrets/work-kubeconfig.enc.yaml") {
		t.Fatalf("expected filename override for workspace secret path, got %s", commandLine)
	}
	if runner.dir != root {
		t.Fatalf("expected sops to run from workspace root %s, got %s", root, runner.dir)
	}
}

func TestAddCommandWritesWrapperAndPublishResource(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)
	root, err := Init(paths, "work")
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	commandPath, resourcePath, err := AddCommand(paths, "work", AddCommandOptions{
		Name: "kubectl-work",
		Exec: "kubectl",
		Env: []string{
			"KUBECONFIG=${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/work/work-kubeconfig",
		},
		Args: []string{"--context", "work-prod"},
	})
	if err != nil {
		t.Fatalf("add command: %v", err)
	}

	if commandPath != filepath.Join(root, "bin", "kubectl-work") {
		t.Fatalf("unexpected command path %s", commandPath)
	}
	content, err := os.ReadFile(commandPath)
	if err != nil {
		t.Fatalf("read command wrapper: %v", err)
	}
	script := string(content)
	if !strings.Contains(script, "export KUBECONFIG=${XDG_CONFIG_HOME:-$HOME/.config}'/cao/generated/work/work-kubeconfig'") {
		t.Fatalf("expected KUBECONFIG export, got %q", script)
	}
	if !strings.Contains(script, "exec 'kubectl' '--context' 'work-prod' \"$@\"") {
		t.Fatalf("expected exec wrapper, got %q", script)
	}
	resource, err := config.LoadResource(resourcePath)
	if err != nil {
		t.Fatalf("load resource: %v", err)
	}
	if resource.Kind != "publish" || resource.Source != "bin/kubectl-work" {
		t.Fatalf("unexpected resource manifest %#v", resource)
	}
}

func TestRenameWorkspaceUpdatesManifestResourcesStateAndWrappers(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)
	root, err := Init(paths, "perso")
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	resourcePath := filepath.Join(root, "resources", "secret-main.yaml")
	if err := config.WriteYAML(resourcePath, config.ResourceManifest{
		Kind:   "secret",
		Name:   "main",
		Source: "secrets/main.enc",
		Target: "${XDG_CONFIG_HOME}/cao/generated/perso/main",
		Mode:   "0600",
	}); err != nil {
		t.Fatalf("write resource: %v", err)
	}

	commandPath, _, err := AddCommand(paths, "perso", AddCommandOptions{
		Name: "k-perso",
		Exec: "kubectl",
		Env: []string{
			"KUBECONFIG=${XDG_CONFIG_HOME:-$HOME/.config}/cao/generated/perso/main",
		},
	})
	if err != nil {
		t.Fatalf("add command: %v", err)
	}

	current := state.New()
	oldTarget := filepath.Join(paths.ConfigHome, "cao", "generated", "perso", "main")
	current.Entries[oldTarget] = state.Entry{
		Path:      oldTarget,
		Kind:      "secret",
		Workspace: "perso",
		Owner:     "perso/main",
	}
	current.AppliedWorkspaces = []string{"perso"}
	if err := state.Save(paths.StateFile, current); err != nil {
		t.Fatalf("save state: %v", err)
	}

	newRoot, err := Rename(paths, "perso", "personal")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if newRoot != filepath.Join(paths.WorkspacesDir, "personal") {
		t.Fatalf("unexpected new root %s", newRoot)
	}
	if _, err := os.Stat(filepath.Join(paths.WorkspacesDir, "perso")); !os.IsNotExist(err) {
		t.Fatalf("expected old workspace dir to be gone, err=%v", err)
	}
	manifest, err := config.LoadWorkspace(filepath.Join(newRoot, "workspace.yaml"))
	if err != nil {
		t.Fatalf("load renamed manifest: %v", err)
	}
	if manifest.Name != "personal" {
		t.Fatalf("expected manifest name personal, got %q", manifest.Name)
	}
	resource, err := config.LoadResource(filepath.Join(newRoot, "resources", "secret-main.yaml"))
	if err != nil {
		t.Fatalf("load renamed resource: %v", err)
	}
	if resource.Target != "${XDG_CONFIG_HOME}/cao/generated/personal/main" {
		t.Fatalf("unexpected renamed target %q", resource.Target)
	}
	content, err := os.ReadFile(filepath.Join(newRoot, "bin", filepath.Base(commandPath)))
	if err != nil {
		t.Fatalf("read renamed wrapper: %v", err)
	}
	if !strings.Contains(string(content), "/cao/generated/personal/main") {
		t.Fatalf("expected wrapper to reference renamed workspace, got %q", string(content))
	}
	updatedState, err := state.Load(paths.StateFile)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	entry, ok := updatedState.Entries[oldTarget]
	if !ok {
		t.Fatalf("expected old target entry to remain tracked for future prune")
	}
	if entry.Workspace != "personal" || entry.Owner != "personal/main" {
		t.Fatalf("unexpected renamed state entry %#v", entry)
	}
	if len(updatedState.AppliedWorkspaces) != 1 || updatedState.AppliedWorkspaces[0] != "personal" {
		t.Fatalf("unexpected applied workspaces %#v", updatedState.AppliedWorkspaces)
	}
}

func TestAddCommandKeepsVariableExpansionButLiteralizesCommandSubstitution(t *testing.T) {
	t.Parallel()
	paths := testPaths(t)
	_, err := Init(paths, "work")
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	commandPath, _, err := AddCommand(paths, "work", AddCommandOptions{
		Name: "safe-env",
		Exec: "env",
		Env: []string{
			`SAFE=${HOME}/ok/$(touch pwned)/still-literal`,
		},
	})
	if err != nil {
		t.Fatalf("add command: %v", err)
	}

	content, err := os.ReadFile(commandPath)
	if err != nil {
		t.Fatalf("read command wrapper: %v", err)
	}
	script := string(content)
	if !strings.Contains(script, "export SAFE=${HOME}'/ok/$(touch pwned)/still-literal'") {
		t.Fatalf("expected SAFE export to preserve ${HOME} and literalize $(...), got %q", script)
	}
	if strings.Contains(script, "export SAFE=\"") {
		t.Fatalf("did not expect a double-quoted shell assignment, got %q", script)
	}
}

func testPaths(t *testing.T) runtime.Paths {
	t.Helper()
	root := t.TempDir()
	return runtime.Paths{
		Home:            filepath.Join(root, "home"),
		ConfigHome:      filepath.Join(root, "home", ".config"),
		CacheHome:       filepath.Join(root, "cache"),
		StateHome:       filepath.Join(root, "state"),
		DataHome:        filepath.Join(root, "data"),
		RuntimeDir:      filepath.Join(root, "runtime"),
		AppConfigDir:    filepath.Join(root, "home", ".config", "cao"),
		AppCacheDir:     filepath.Join(root, "cache", "cao"),
		AppStateDir:     filepath.Join(root, "state", "cao"),
		AppDataDir:      filepath.Join(root, "data", "cao"),
		WorkspacesDir:   filepath.Join(root, "data", "cao", "workspaces"),
		AppGeneratedDir: filepath.Join(root, "home", ".config", "cao", "generated"),
		BinDir:          filepath.Join(root, "home", ".local", "bin"),
		StateFile:       filepath.Join(root, "state", "cao", "state.json"),
	}
}
