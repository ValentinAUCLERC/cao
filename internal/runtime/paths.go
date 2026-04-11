package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

type Paths struct {
	Home            string
	ConfigHome      string
	CacheHome       string
	StateHome       string
	DataHome        string
	RuntimeDir      string
	AppConfigDir    string
	AppCacheDir     string
	AppStateDir     string
	AppDataDir      string
	WorkspacesDir   string
	AppGeneratedDir string
	BinDir          string
	StateFile       string
}

func Detect() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("detect home: %w", err)
	}
	return detectFor(goruntime.GOOS, home, os.TempDir(), os.Getenv), nil
}

func detectFor(goos, home, tempDir string, getenv func(string) string) Paths {
	if goos == "windows" {
		configHome := envOrWith(getenv, "APPDATA", filepath.Join(home, "AppData", "Roaming"))
		localHome := envOrWith(getenv, "LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		appConfigDir := filepath.Join(configHome, "cao")
		appLocalDir := filepath.Join(localHome, "cao")
		return Paths{
			Home:            home,
			ConfigHome:      configHome,
			CacheHome:       localHome,
			StateHome:       localHome,
			DataHome:        localHome,
			RuntimeDir:      envOrWith(getenv, "TEMP", filepath.Join(tempDir, "cao-"+sanitizeHome(home))),
			AppConfigDir:    appConfigDir,
			AppCacheDir:     appLocalDir,
			AppStateDir:     appLocalDir,
			AppDataDir:      appLocalDir,
			WorkspacesDir:   filepath.Join(appLocalDir, "workspaces"),
			AppGeneratedDir: filepath.Join(appConfigDir, "generated"),
			BinDir:          filepath.Join(home, ".local", "bin"),
			StateFile:       filepath.Join(appLocalDir, "state.json"),
		}
	}

	configHome := envOrWith(getenv, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cacheHome := envOrWith(getenv, "XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	stateHome := envOrWith(getenv, "XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	dataHome := envOrWith(getenv, "XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	runtimeDir := envOrWith(getenv, "XDG_RUNTIME_DIR", filepath.Join(tempDir, "cao-"+sanitizeHome(home)))
	appConfigDir := filepath.Join(configHome, "cao")
	appCacheDir := filepath.Join(cacheHome, "cao")
	appStateDir := filepath.Join(stateHome, "cao")
	appDataDir := filepath.Join(dataHome, "cao")
	return Paths{
		Home:            home,
		ConfigHome:      configHome,
		CacheHome:       cacheHome,
		StateHome:       stateHome,
		DataHome:        dataHome,
		RuntimeDir:      runtimeDir,
		AppConfigDir:    appConfigDir,
		AppCacheDir:     appCacheDir,
		AppStateDir:     appStateDir,
		AppDataDir:      appDataDir,
		WorkspacesDir:   filepath.Join(appDataDir, "workspaces"),
		AppGeneratedDir: filepath.Join(appConfigDir, "generated"),
		BinDir:          filepath.Join(home, ".local", "bin"),
		StateFile:       filepath.Join(appStateDir, "state.json"),
	}
}

func DefaultGeneratedSecretTarget(workspaceName, resourceName string) string {
	return "${XDG_CONFIG_HOME}/cao/generated/" + workspaceName + "/" + resourceName
}

func (p Paths) Expand(value string) string {
	if value == "" {
		return value
	}
	if strings.HasPrefix(value, "~/") {
		value = filepath.Join(p.Home, strings.TrimPrefix(value, "~/"))
	}
	replacements := map[string]string{
		"${HOME}":            p.Home,
		"${XDG_CONFIG_HOME}": p.ConfigHome,
		"${XDG_CACHE_HOME}":  p.CacheHome,
		"${XDG_STATE_HOME}":  p.StateHome,
		"${XDG_DATA_HOME}":   p.DataHome,
		"${XDG_RUNTIME_DIR}": p.RuntimeDir,
	}
	for from, to := range replacements {
		value = strings.ReplaceAll(value, from, to)
	}
	return filepath.Clean(value)
}

func envOr(key, fallback string) string {
	return envOrWith(os.Getenv, key, fallback)
}

func envOrWith(getenv func(string) string, key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func sanitizeHome(home string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	return strings.Trim(replacer.Replace(home), "_")
}
