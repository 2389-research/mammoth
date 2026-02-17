// ABOUTME: Interactive setup wizard for mammoth — collects API keys, writes .env, prints quickstart.
// ABOUTME: Follows the same subcommand pattern as "mammoth serve" with its own flag set.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

// setupConfig holds configuration for the "mammoth setup" subcommand.
type setupConfig struct {
	skipKeys bool
	envFile  string
}

// parseSetupArgs checks whether args starts with the "setup" subcommand and,
// if so, parses setup-specific flags. Returns the config and true if "setup"
// was detected, or a zero value and false otherwise.
func parseSetupArgs(args []string) (setupConfig, bool) {
	if len(args) == 0 || args[0] != "setup" {
		return setupConfig{}, false
	}

	var cfg setupConfig
	fs := flag.NewFlagSet("mammoth setup", flag.ContinueOnError)
	fs.BoolVar(&cfg.skipKeys, "skip-keys", false, "Skip API key collection")
	fs.StringVar(&cfg.envFile, "env-file", ".env", "Path to write .env file")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: mammoth setup [flags]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Interactive setup wizard — configure API keys and get started.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(2)
	}

	return cfg, true
}

// runSetup executes the interactive setup wizard.
func runSetup(cfg setupConfig) int {
	return 0
}
