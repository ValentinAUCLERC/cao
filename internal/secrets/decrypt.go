package secrets

import (
	"context"
	"fmt"
	"strings"

	"github.com/ValentinAUCLERC/cao/internal/command"
)

func Decrypt(ctx context.Context, runner command.Runner, inputPath string) ([]byte, error) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, fmt.Errorf("input path is required")
	}
	out, err := runner.Run(ctx, "sops", []string{"decrypt", inputPath}, command.RunOptions{})
	if err != nil {
		return nil, err
	}
	return out, nil
}
