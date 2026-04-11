package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ValentinAUCLERC/cao/internal/command"
	"github.com/ValentinAUCLERC/cao/internal/platform"
	"github.com/ValentinAUCLERC/cao/internal/runtime"
	"github.com/ValentinAUCLERC/cao/internal/state"
)

type fakeRunner struct {
	commands []string
	errs     map[string]error
	outputs  map[string][]byte
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ command.RunOptions) ([]byte, error) {
	commandLine := name + " " + strings.Join(args, " ")
	f.commands = append(f.commands, commandLine)
	if err, ok := f.errs[name]; ok {
		return nil, err
	}
	if out, ok := f.outputs[name]; ok {
		return out, nil
	}
	if name == "sops" && len(args) == 2 && args[0] == "decrypt" {
		return []byte("secret=value\n"), nil
	}
	return []byte("ok"), nil
}

func TestLoadPlanRejectsCollisionBetweenResources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "work", "workspace.yaml", "name: work\n")
	writeWorkspaceFile(t, root, "work", "resources/one.yaml", `
kind: file
name: one
source: files/one.txt
target: ~/.gitconfig
`)
	writeWorkspaceFile(t, root, "work", "files/one.txt", "one\n")
	writeWorkspaceFile(t, root, "work", "resources/two.yaml", `
kind: file
name: two
source: files/two.txt
target: ~/.gitconfig
`)
	writeWorkspaceFile(t, root, "work", "files/two.txt", "two\n")

	engine := testEngine(t, root, &fakeRunner{})
	if _, err := engine.LoadPlan(context.Background(), nil); err == nil {
		t.Fatalf("expected target collision")
	}
}

func TestLoadPlanSkipsWorkspacePlatformMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "work", "workspace.yaml", "name: work\n")
	writeWorkspaceFile(t, root, "perso", "workspace.yaml", `
name: perso
platforms:
  - darwin
`)

	engine := testEngine(t, root, &fakeRunner{})
	engine.Platform = platform.Linux

	plan, err := engine.LoadPlan(context.Background(), nil)
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	if len(plan.Workspaces) != 1 {
		t.Fatalf("expected one active workspace, got %d", len(plan.Workspaces))
	}
	if len(plan.Warnings) == 0 {
		t.Fatalf("expected warning for skipped workspace")
	}
}

func TestLoadPlanRespectsWorkspaceFilter(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "work", "workspace.yaml", "name: work\n")
	writeWorkspaceFile(t, root, "work", "resources/work-file.yaml", `
kind: file
name: work-file
source: files/work.txt
target: ~/.config/work.txt
`)
	writeWorkspaceFile(t, root, "work", "files/work.txt", "work\n")
	writeWorkspaceFile(t, root, "perso", "workspace.yaml", "name: perso\n")
	writeWorkspaceFile(t, root, "perso", "resources/perso-file.yaml", `
kind: file
name: perso-file
source: files/perso.txt
target: ~/.config/perso.txt
`)
	writeWorkspaceFile(t, root, "perso", "files/perso.txt", "perso\n")

	engine := testEngine(t, root, &fakeRunner{})
	plan, err := engine.LoadPlan(context.Background(), []string{"work"})
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	if len(plan.Workspaces) != 1 || plan.Workspaces[0].Name != "work" {
		t.Fatalf("expected only work workspace, got %#v", plan.ActiveWorkspaces())
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("expected one operation, got %d", len(plan.Operations))
	}
	if plan.Operations[0].Workspace != "work" {
		t.Fatalf("expected work operation, got %q", plan.Operations[0].Workspace)
	}
}

func TestPruneRemovesStaleEntriesForSelectedWorkspace(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "work", "workspace.yaml", "name: work\n")
	writeWorkspaceFile(t, root, "perso", "workspace.yaml", "name: perso\n")

	engine := testEngine(t, root, &fakeRunner{})
	workTarget := filepath.Join(root, "home", ".config", "work-stale.txt")
	persoTarget := filepath.Join(root, "home", ".config", "perso-stale.txt")
	writeFile(t, workTarget, "stale-work")
	writeFile(t, persoTarget, "stale-perso")

	current := state.New()
	current.Entries[workTarget] = state.Entry{Path: workTarget, Kind: "file", Workspace: "work", Owner: "work/stale"}
	current.Entries[persoTarget] = state.Entry{Path: persoTarget, Kind: "file", Workspace: "perso", Owner: "perso/stale"}
	if err := state.Save(engine.Paths.StateFile, current); err != nil {
		t.Fatalf("save state: %v", err)
	}

	removed, err := engine.Prune(context.Background(), []string{"work"})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(removed) != 1 || removed[0] != workTarget {
		t.Fatalf("unexpected prune result: %#v", removed)
	}
	if _, err := os.Stat(workTarget); !os.IsNotExist(err) {
		t.Fatalf("expected work target to be removed, err=%v", err)
	}
	if _, err := os.Stat(persoTarget); err != nil {
		t.Fatalf("expected perso target to remain, err=%v", err)
	}
}

func TestApplyPrunesStaleEntriesByDefault(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "work", "workspace.yaml", "name: work\n")
	writeWorkspaceFile(t, root, "work", "resources/current.yaml", `
kind: file
name: current
source: files/current.txt
target: ~/.config/current.txt
`)
	writeWorkspaceFile(t, root, "work", "files/current.txt", "current\n")

	engine := testEngine(t, root, &fakeRunner{})
	staleTarget := filepath.Join(root, "home", ".config", "stale.txt")
	writeFile(t, staleTarget, "stale")
	current := state.New()
	current.Entries[staleTarget] = state.Entry{Path: staleTarget, Kind: "file", Workspace: "work", Owner: "work/stale"}
	if err := state.Save(engine.Paths.StateFile, current); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if _, _, err := engine.Apply(context.Background(), []string{"work"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(staleTarget); !os.IsNotExist(err) {
		t.Fatalf("expected stale target to be pruned during apply, err=%v", err)
	}
}

func TestApplyCanSkipPrune(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "work", "workspace.yaml", "name: work\n")
	writeWorkspaceFile(t, root, "work", "resources/current.yaml", `
kind: file
name: current
source: files/current.txt
target: ~/.config/current.txt
`)
	writeWorkspaceFile(t, root, "work", "files/current.txt", "current\n")

	engine := testEngine(t, root, &fakeRunner{})
	staleTarget := filepath.Join(root, "home", ".config", "stale.txt")
	writeFile(t, staleTarget, "stale")
	current := state.New()
	current.Entries[staleTarget] = state.Entry{Path: staleTarget, Kind: "file", Workspace: "work", Owner: "work/stale"}
	if err := state.Save(engine.Paths.StateFile, current); err != nil {
		t.Fatalf("save state: %v", err)
	}

	if _, _, err := engine.ApplyWithOptions(context.Background(), []string{"work"}, ApplyOptions{Prune: false}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(staleTarget); err != nil {
		t.Fatalf("expected stale target to remain with no-prune, err=%v", err)
	}
}

func TestSecretResourceUsesDecryptedContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "work", "workspace.yaml", "name: work\n")
	writeWorkspaceFile(t, root, "work", "resources/app-secret.yaml", `
kind: secret
name: app-secret
source: secrets/app.env.enc
target: ~/.config/secrets/app.env
`)
	writeWorkspaceFile(t, root, "work", "secrets/app.env.enc", "encrypted")

	runner := &fakeRunner{}
	engine := testEngine(t, root, runner)
	plan, err := engine.LoadPlan(context.Background(), nil)
	if err != nil {
		t.Fatalf("load plan: %v", err)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("expected one operation, got %d", len(plan.Operations))
	}
	if string(plan.Operations[0].Content) != "secret=value\n" {
		t.Fatalf("unexpected secret content %q", string(plan.Operations[0].Content))
	}
}

func TestApplySecuresGeneratedSecretDirectories(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeWorkspaceFile(t, root, "work", "workspace.yaml", "name: work\n")
	writeWorkspaceFile(t, root, "work", "resources/app-secret.yaml", `
kind: secret
name: app-secret
source: secrets/app.env.enc
`)
	writeWorkspaceFile(t, root, "work", "secrets/app.env.enc", "encrypted")

	engine := testEngine(t, root, &fakeRunner{})
	if _, _, err := engine.Apply(context.Background(), nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	generatedDir := filepath.Join(root, "home", ".config", "cao", "generated")
	workspaceDir := filepath.Join(generatedDir, "work")
	for _, dir := range []string{generatedDir, workspaceDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("expected %s to be 0700, got %#o", dir, info.Mode().Perm())
		}
	}
}

func TestDoctorIncludesFixHintsForMissingDependencies(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	engine := testEngine(t, root, &fakeRunner{
		errs: map[string]error{
			"git":  exec.ErrNotFound,
			"sops": exec.ErrNotFound,
			"age":  exec.ErrNotFound,
		},
	})

	lines, err := Doctor(context.Background(), engine.Paths, engine.Runner)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	output := strings.Join(lines, "\n")
	if !strings.Contains(output, "git: missing") {
		t.Fatalf("expected missing git in doctor output, got %q", output)
	}
	if !strings.Contains(output, "fix:") {
		t.Fatalf("expected doctor fix hints, got %q", output)
	}
	if !strings.Contains(output, "age-key: missing") {
		t.Fatalf("expected age-key status in doctor output, got %q", output)
	}
}

func testEngine(t *testing.T, root string, runner command.Runner) *Engine {
	t.Helper()
	paths := runtime.Paths{
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
	for _, dir := range []string{paths.BinDir, paths.AppStateDir, paths.WorkspacesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return &Engine{
		Paths:    paths,
		Platform: platform.Linux,
		Runner:   runner,
	}
}

func writeWorkspaceFile(t *testing.T, root, workspaceName, relativePath, content string) {
	t.Helper()
	path := filepath.Join(root, "data", "cao", "workspaces", workspaceName, filepath.FromSlash(relativePath))
	writeFile(t, path, content)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
