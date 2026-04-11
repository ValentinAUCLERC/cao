package platform

import (
	"os"
	"runtime"
	"strings"
)

type Name string

const (
	Darwin  Name = "darwin"
	Linux   Name = "linux"
	Windows Name = "windows"
	WSL     Name = "wsl"
)

func Detect() Name {
	return detect(runtime.GOOS, os.Getenv, os.ReadFile)
}

func Matches(allowed []string, current Name) bool {
	if len(allowed) == 0 {
		return true
	}
	currentValue := string(current)
	for _, item := range allowed {
		if item == currentValue {
			return true
		}
		if current == WSL && item == string(Linux) {
			return true
		}
	}
	return false
}

func detect(goos string, getenv func(string) string, readFile func(string) ([]byte, error)) Name {
	switch goos {
	case "darwin":
		return Darwin
	case "windows":
		return Windows
	case "linux":
		if isWSL(getenv, readFile) {
			return WSL
		}
		return Linux
	default:
		return Name(goos)
	}
}

func isWSL(getenv func(string) string, readFile func(string) ([]byte, error)) bool {
	if getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	data, err := readFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}
