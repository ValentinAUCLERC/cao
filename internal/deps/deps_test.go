package deps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ValentinAUCLERC/cao/internal/command"
	"github.com/ValentinAUCLERC/cao/internal/platform"
	"github.com/ValentinAUCLERC/cao/internal/runtime"
)

type fakeRunner struct {
	missing map[string]error
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ command.RunOptions) ([]byte, error) {
	if err, ok := f.missing[name]; ok {
		return nil, err
	}
	return []byte(name + " version 1.0.0\n"), nil
}

func TestCheckReportsMissingToolsAndAgeKey(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testPaths(root)
	statuses, err := Check(context.Background(), paths, &fakeRunner{
		missing: map[string]error{
			"sops": exec.ErrNotFound,
		},
	}, []RequirementSpec{
		{Requirement: RequirementSops},
		{Requirement: RequirementAgeKey},
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	if statuses[0].Requirement != RequirementSops || statuses[0].Present {
		t.Fatalf("expected missing sops status, got %#v", statuses[0])
	}
	if statuses[1].Requirement != RequirementAgeKey || statuses[1].Present {
		t.Fatalf("expected missing age-key status, got %#v", statuses[1])
	}
	if len(statuses[1].Fixes) == 0 {
		t.Fatalf("expected age-key fix hints")
	}
}

func TestAgeKeyFixesUsePowerShellHintsOnWindows(t *testing.T) {
	t.Parallel()

	paths := runtime.Paths{ConfigHome: filepath.Join("Users", "valen", "AppData", "Roaming")}
	fixes := ageKeyFixes(paths, platform.Windows)
	if len(fixes) != 3 {
		t.Fatalf("expected 3 fixes, got %d", len(fixes))
	}
	if fixes[0][:8] != "New-Item" {
		t.Fatalf("expected PowerShell directory hint, got %q", fixes[0])
	}
	if fixes[1][:13] != "age-keygen -o" {
		t.Fatalf("expected age-keygen hint, got %q", fixes[1])
	}
}

func TestHasSecretResourcesRespectsWorkspaceFilter(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := testPaths(root)

	writeFile(t, filepath.Join(paths.WorkspacesDir, "work", "workspace.yaml"), "name: work\n")
	writeFile(t, filepath.Join(paths.WorkspacesDir, "work", "resources", "secret.yaml"), "kind: secret\nname: token\nsource: secrets/token.enc\n")
	writeFile(t, filepath.Join(paths.WorkspacesDir, "work", "secrets", "token.enc"), "encrypted\n")

	writeFile(t, filepath.Join(paths.WorkspacesDir, "perso", "workspace.yaml"), "name: perso\n")
	writeFile(t, filepath.Join(paths.WorkspacesDir, "perso", "resources", "file.yaml"), "kind: file\nname: profile\nsource: files/profile\ntarget: ~/.profile\n")
	writeFile(t, filepath.Join(paths.WorkspacesDir, "perso", "files", "profile"), "profile\n")

	hasSecrets, err := HasSecretResources(paths, []string{"work"})
	if err != nil {
		t.Fatalf("has secrets for work: %v", err)
	}
	if !hasSecrets {
		t.Fatalf("expected work filter to report secret resources")
	}

	hasSecrets, err = HasSecretResources(paths, []string{"perso"})
	if err != nil {
		t.Fatalf("has secrets for perso: %v", err)
	}
	if hasSecrets {
		t.Fatalf("expected perso filter to skip secret resources")
	}
}

func testPaths(root string) runtime.Paths {
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
