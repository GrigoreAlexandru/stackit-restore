package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/GrigoreAlexandru/Stackit-Restore/internal/api"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/cli"
	"github.com/GrigoreAlexandru/Stackit-Restore/internal/config"
)

// Compile-time interface guard: ensures *api.Client satisfies cli.API.
var _ cli.API = (*api.Client)(nil)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	opts, err := cli.ParseOptions(os.Args[1:])
	if err != nil {
		return err
	}
	if opts.Help {
		cli.PrintUsage(os.Stdout)
		return nil
	}

	applyFlagOverrides(opts)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := api.CheckPreflightTools(); err != nil {
		return err
	}

	apiClient, err := api.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("initialize STACKIT API client: %w", err)
	}

	return cli.ExecuteWithOptions(ctx, apiClient, opts)
}

// applyFlagOverrides propagates flag overrides into the environment so that
// config.Load() picks them up. This is done here (after ParseOptions) rather
// than inside ParseOptions to keep option parsing free of side-effects.
func applyFlagOverrides(opts cli.Options) {
	if opts.ProjectID != "" {
		os.Setenv("STACKIT_PROJECT_ID", opts.ProjectID)
	}
	if opts.Region != "" {
		os.Setenv("STACKIT_REGION", opts.Region)
	}
	if opts.ServiceAccountKeyPath != "" {
		os.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", opts.ServiceAccountKeyPath)
	}
}
