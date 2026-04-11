package main

import (
	"context"
	"os"

	"github.com/valentin/cao/internal/app"
)

func main() {
	ctx := context.Background()
	code := app.New(os.Stdout, os.Stderr).Run(ctx, os.Args[1:])
	os.Exit(code)
}
