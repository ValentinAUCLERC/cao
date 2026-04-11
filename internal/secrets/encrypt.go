package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ValentinAUCLERC/cao/internal/command"
	"github.com/ValentinAUCLERC/cao/internal/fsutil"
)

type EncryptOptions struct {
	InputPath     string
	OutputPath    string
	InPlace       bool
	AgeRecipients []string
	Format        string
	WorkingDir    string
	FileHint      string
}

func Encrypt(ctx context.Context, runner command.Runner, opts EncryptOptions) (string, error) {
	if opts.InputPath == "" {
		return "", fmt.Errorf("input path is required")
	}
	if _, err := os.Stat(opts.InputPath); err != nil {
		return "", fmt.Errorf("inspect input %s: %w", opts.InputPath, err)
	}
	if opts.InPlace && opts.OutputPath != "" {
		return "", fmt.Errorf("cannot use --in-place and --output together")
	}

	outputPath := opts.OutputPath
	if !opts.InPlace && outputPath == "" {
		outputPath = DefaultEncryptedPath(opts.InputPath)
	}
	if outputPath != "" {
		if err := fsutil.EnsureDir(filepath.Dir(outputPath), 0o755); err != nil {
			return "", fmt.Errorf("prepare output directory: %w", err)
		}
	}

	format := normalizeFormat(opts.Format)
	if format == "" {
		format = DetectFormat(opts.InputPath)
	}
	sopsFormat := sopsFormatFor(format)

	args := []string{"encrypt"}
	if opts.InPlace {
		args = append(args, "--in-place")
	} else {
		args = append(args, "--output", outputPath)
	}
	if len(opts.AgeRecipients) > 0 {
		args = append(args, "--age", strings.Join(opts.AgeRecipients, ","))
	}
	if opts.FileHint != "" {
		args = append(args, "--filename-override", opts.FileHint)
	}
	if sopsFormat != "" {
		args = append(args, "--input-type", sopsFormat, "--output-type", sopsFormat)
	}
	args = append(args, opts.InputPath)

	if _, err := runner.Run(ctx, "sops", args, command.RunOptions{Dir: opts.WorkingDir}); err != nil {
		return "", err
	}
	if opts.InPlace {
		return opts.InputPath, nil
	}
	return outputPath, nil
}

func DefaultEncryptedPath(inputPath string) string {
	dir := filepath.Dir(inputPath)
	base := filepath.Base(inputPath)
	switch {
	case strings.HasSuffix(base, ".yaml"):
		return filepath.Join(dir, strings.TrimSuffix(base, ".yaml")+".enc.yaml")
	case strings.HasSuffix(base, ".yml"):
		return filepath.Join(dir, strings.TrimSuffix(base, ".yml")+".enc.yml")
	case strings.HasSuffix(base, ".json"):
		return filepath.Join(dir, strings.TrimSuffix(base, ".json")+".enc.json")
	case base == ".env" || strings.HasPrefix(base, ".env."):
		return filepath.Join(dir, base+".enc")
	default:
		return filepath.Join(dir, base+".enc")
	}
}

func DetectFormat(path string) string {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case strings.HasSuffix(base, ".yaml"), strings.HasSuffix(base, ".yml"):
		return "yaml"
	case strings.HasSuffix(base, ".json"):
		return "json"
	case base == ".env", strings.HasPrefix(base, ".env."):
		return "dotenv"
	default:
		return "binary"
	}
}

func normalizeFormat(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "auto" {
		return ""
	}
	return value
}

func sopsFormatFor(format string) string {
	switch format {
	case "kubeconfig":
		return "yaml"
	default:
		return format
	}
}
