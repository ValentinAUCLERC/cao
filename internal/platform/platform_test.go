package platform

import (
	"errors"
	"testing"
)

func TestDetectReturnsWindowsForWindowsGOOS(t *testing.T) {
	t.Parallel()

	got := detect("windows", func(string) string { return "" }, func(string) ([]byte, error) {
		return nil, errors.New("not used")
	})
	if got != Windows {
		t.Fatalf("expected %q, got %q", Windows, got)
	}
}

func TestDetectReturnsWSLWhenLinuxReportsMicrosoftKernel(t *testing.T) {
	t.Parallel()

	got := detect("linux", func(string) string { return "" }, func(string) ([]byte, error) {
		return []byte("Linux version 5.15.167.4-microsoft-standard-WSL2"), nil
	})
	if got != WSL {
		t.Fatalf("expected %q, got %q", WSL, got)
	}
}

func TestDetectReturnsGoosNameForUnknownPlatforms(t *testing.T) {
	t.Parallel()

	got := detect("freebsd", func(string) string { return "" }, func(string) ([]byte, error) {
		return nil, errors.New("not used")
	})
	if got != Name("freebsd") {
		t.Fatalf("expected freebsd, got %q", got)
	}
}
