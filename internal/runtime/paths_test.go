package runtime

import (
	"path/filepath"
	"testing"
)

func TestDetectForWindowsUsesAppDataLayout(t *testing.T) {
	t.Parallel()

	home := filepath.Join("users", "valen")
	tempDir := filepath.Join("tmp")
	paths := detectFor("windows", home, tempDir, func(string) string { return "" })

	configHome := filepath.Join(home, "AppData", "Roaming")
	localHome := filepath.Join(home, "AppData", "Local")
	if paths.ConfigHome != configHome {
		t.Fatalf("expected config home %q, got %q", configHome, paths.ConfigHome)
	}
	if paths.WorkspacesDir != filepath.Join(localHome, "cao", "workspaces") {
		t.Fatalf("unexpected workspaces dir %q", paths.WorkspacesDir)
	}
	if paths.StateFile != filepath.Join(localHome, "cao", "state.json") {
		t.Fatalf("unexpected state file %q", paths.StateFile)
	}
	if paths.AppGeneratedDir != filepath.Join(configHome, "cao", "generated") {
		t.Fatalf("unexpected generated dir %q", paths.AppGeneratedDir)
	}
}

func TestDetectForWindowsRespectsAppDataEnv(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"APPDATA":      filepath.Join("custom", "roaming"),
		"LOCALAPPDATA": filepath.Join("custom", "local"),
		"TEMP":         filepath.Join("custom", "temp"),
	}
	paths := detectFor("windows", filepath.Join("users", "valen"), filepath.Join("tmp"), func(key string) string {
		return values[key]
	})

	if paths.ConfigHome != values["APPDATA"] {
		t.Fatalf("expected config home %q, got %q", values["APPDATA"], paths.ConfigHome)
	}
	if paths.DataHome != values["LOCALAPPDATA"] {
		t.Fatalf("expected data home %q, got %q", values["LOCALAPPDATA"], paths.DataHome)
	}
	if paths.RuntimeDir != values["TEMP"] {
		t.Fatalf("expected runtime dir %q, got %q", values["TEMP"], paths.RuntimeDir)
	}
}
