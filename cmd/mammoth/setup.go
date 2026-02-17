// ABOUTME: Interactive setup wizard for mammoth — collects API keys, writes .env, prints quickstart.
// ABOUTME: Follows the same subcommand pattern as "mammoth serve" with its own flag set.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
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

// providerInfo holds the detection state for a single LLM provider.
type providerInfo struct {
	name   string
	envVar string
	prefix string // expected key prefix for format validation
	isSet  bool
}

// detectProviders checks which LLM API keys are set in the environment.
func detectProviders() []providerInfo {
	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", prefix: "sk-ant-"},
		{name: "OpenAI", envVar: "OPENAI_API_KEY", prefix: "sk-"},
		{name: "Gemini", envVar: "GEMINI_API_KEY", prefix: "AIza"},
	}
	for i := range providers {
		providers[i].isSet = os.Getenv(providers[i].envVar) != ""
	}
	return providers
}

// printProviderStatus writes the provider status table to w.
func printProviderStatus(w io.Writer, providers []providerInfo) {
	fmt.Fprintln(w, "LLM Providers:")
	for _, p := range providers {
		check := "[ ]"
		status := "not set"
		if p.isSet {
			check = "[✓]"
			status = "set"
		}
		fmt.Fprintf(w, "  %s %-10s (%s %s)\n", check, p.name, p.envVar, status)
	}
}

// printWelcome writes the setup welcome banner to w.
func printWelcome(w io.Writer) {
	fmt.Fprint(w, mammothASCII)
	fmt.Fprintln(w, "Welcome to mammoth setup!")
	fmt.Fprintln(w)
}

// validateKeyFormat checks whether a key starts with the expected prefix.
func validateKeyFormat(key, prefix string) bool {
	if key == "" {
		return false
	}
	return strings.HasPrefix(key, prefix)
}

// collectKeys interactively prompts for API keys for providers that aren't
// already set. Returns a map of envVar->key for keys the user entered.
func collectKeys(r io.Reader, w io.Writer, providers []providerInfo) map[string]string {
	scanner := bufio.NewScanner(r)
	collected := map[string]string{}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Enter API keys (leave blank to skip):")
	fmt.Fprintln(w)

	for _, p := range providers {
		if p.isSet {
			fmt.Fprintf(w, "  %s: already set ✓\n", p.name)
			continue
		}

		fmt.Fprintf(w, "  %s (%s): ", p.name, p.envVar)
		if !scanner.Scan() {
			break
		}
		key := strings.TrimSpace(scanner.Text())
		if key == "" {
			continue
		}

		if !validateKeyFormat(key, p.prefix) {
			fmt.Fprintf(w, "  Warning: key doesn't match expected format (%s*). Save anyway? [Y/n] ", p.prefix)
			if !scanner.Scan() {
				break
			}
			answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
			if answer == "n" || answer == "no" {
				fmt.Fprintf(w, "  Skipped %s.\n", p.name)
				continue
			}
		}

		collected[p.envVar] = key
	}

	return collected
}

// writeEnvFile writes collected API keys to a .env file. If the file already
// exists, it updates matching keys in place and appends new ones. Existing
// lines that don't match any collected key are preserved as-is. Does nothing
// if keys is empty.
func writeEnvFile(path string, keys map[string]string) error {
	if len(keys) == 0 {
		return nil
	}

	var existingLines []string
	if data, err := os.ReadFile(path); err == nil {
		existingLines = strings.Split(string(data), "\n")
	}

	written := map[string]bool{}
	var outputLines []string

	for _, line := range existingLines {
		trimmed := strings.TrimSpace(line)
		updated := false
		for envVar, value := range keys {
			lineKey := strings.TrimPrefix(trimmed, "export ")
			if k, _, ok := strings.Cut(lineKey, "="); ok && strings.TrimSpace(k) == envVar {
				outputLines = append(outputLines, envVar+"="+value)
				written[envVar] = true
				updated = true
				break
			}
		}
		if !updated {
			outputLines = append(outputLines, line)
		}
	}

	for envVar, value := range keys {
		if !written[envVar] {
			outputLines = append(outputLines, envVar+"="+value)
		}
	}

	for len(outputLines) > 0 && strings.TrimSpace(outputLines[len(outputLines)-1]) == "" {
		outputLines = outputLines[:len(outputLines)-1]
	}

	content := strings.Join(outputLines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0600)
}

// printQuickStart writes the setup summary and getting-started instructions to w.
func printQuickStart(w io.Writer, configured []string) {
	fmt.Fprintln(w)
	if len(configured) > 0 {
		fmt.Fprintf(w, "Setup complete! You configured: %s\n", strings.Join(configured, ", "))
	} else {
		fmt.Fprintln(w, "No API keys configured.")
		fmt.Fprintln(w, "You can set them later in your .env file or environment.")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Quick start:")
	fmt.Fprintln(w, "  mammoth serve               Launch the web UI (recommended)")
	fmt.Fprintln(w, "  mammoth pipeline.dot        Run a pipeline from the CLI")
	fmt.Fprintln(w, "  mammoth -help               See all options")
	fmt.Fprintln(w)
}

// runSetup executes the interactive setup wizard using stdin/stdout.
func runSetup(cfg setupConfig) int {
	return runSetupWithIO(cfg, os.Stdin, os.Stdout)
}

// runSetupWithIO executes the setup wizard with injectable I/O for testing.
func runSetupWithIO(cfg setupConfig, r io.Reader, w io.Writer) int {
	printWelcome(w)

	providers := detectProviders()
	printProviderStatus(w, providers)

	var collected map[string]string
	if !cfg.skipKeys {
		collected = collectKeys(r, w, providers)

		if err := writeEnvFile(cfg.envFile, collected); err != nil {
			fmt.Fprintf(w, "Error writing %s: %v\n", cfg.envFile, err)
			return 1
		}

		if len(collected) > 0 {
			fmt.Fprintf(w, "\nWrote %d key(s) to %s\n", len(collected), cfg.envFile)
		}
	}

	// Build list of configured provider names (existing + newly collected).
	var configured []string
	for _, p := range providers {
		if p.isSet {
			configured = append(configured, p.name)
			continue
		}
		if collected != nil {
			if _, ok := collected[p.envVar]; ok {
				configured = append(configured, p.name)
			}
		}
	}

	printQuickStart(w, configured)
	return 0
}
