package platform

import (
	"os"
	"runtime"
	"strings"
)

type Name string

const (
	Darwin Name = "darwin"
	Linux  Name = "linux"
	WSL    Name = "wsl"
)

func Detect() Name {
	if runtime.GOOS == "darwin" {
		return Darwin
	}
	if runtime.GOOS == "linux" && isWSL() {
		return WSL
	}
	return Linux
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

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "microsoft")
}
