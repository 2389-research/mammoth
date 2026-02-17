# Setup Command Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `mammoth setup` subcommand that interactively collects API keys, writes a `.env` file, and prints getting-started instructions.

**Architecture:** New subcommand following the same pattern as `serve` — early detection in `main()`, own flag set, self-contained execution. All setup logic lives in `cmd/mammoth/setup.go` with an `io.Reader`/`io.Writer` interface for testability.

**Tech Stack:** Go stdlib only (`bufio`, `flag`, `fmt`, `os`, `strings`)

---

### Task 1: Scaffold setup.go with subcommand wiring

**Files:**
- Create: `cmd/mammoth/setup.go`
- Modify: `cmd/mammoth/main.go:56-60`

**Step 1: Write the failing test**

Create `cmd/mammoth/setup_test.go`:

```go
// ABOUTME: Tests for the mammoth setup subcommand.
// ABOUTME: Validates argument parsing, key collection, .env writing, and summary output.
package main

import (
	"testing"
)

func TestParseSetupArgs_DetectsSetup(t *testing.T) {
	cfg, ok := parseSetupArgs([]string{"setup"})
	if !ok {
		t.Fatal("expected setup subcommand to be detected")
	}
	if cfg.skipKeys {
		t.Error("expected skipKeys to default to false")
	}
	if cfg.envFile != ".env" {
		t.Errorf("expected envFile=.env, got %q", cfg.envFile)
	}
}

func TestParseSetupArgs_NotSetup(t *testing.T) {
	_, ok := parseSetupArgs([]string{"serve"})
	if ok {
		t.Fatal("expected serve not to match setup")
	}
}

func TestParseSetupArgs_WithFlags(t *testing.T) {
	cfg, ok := parseSetupArgs([]string{"setup", "--skip-keys", "--env-file", "/tmp/test.env"})
	if !ok {
		t.Fatal("expected setup subcommand to be detected")
	}
	if !cfg.skipKeys {
		t.Error("expected skipKeys to be true")
	}
	if cfg.envFile != "/tmp/test.env" {
		t.Errorf("expected envFile=/tmp/test.env, got %q", cfg.envFile)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth/ -run TestParseSetupArgs -v`
Expected: FAIL — `parseSetupArgs` undefined

**Step 3: Write minimal implementation**

Create `cmd/mammoth/setup.go`:

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth/ -run TestParseSetupArgs -v`
Expected: PASS

**Step 5: Wire into main.go**

Add the setup subcommand check alongside the serve check in `main.go`. After the serve check (line ~59), add:

```go
if scfg, ok := parseSetupArgs(os.Args[1:]); ok {
    os.Exit(runSetup(scfg))
}
```

Add a stub `runSetup` to `setup.go`:

```go
// runSetup executes the interactive setup wizard.
func runSetup(cfg setupConfig) int {
	return 0
}
```

**Step 6: Commit**

```bash
git add cmd/mammoth/setup.go cmd/mammoth/setup_test.go cmd/mammoth/main.go
git commit -m "feat(setup): scaffold setup subcommand with arg parsing"
```

---

### Task 2: Welcome banner and provider status detection

**Files:**
- Modify: `cmd/mammoth/setup.go`
- Modify: `cmd/mammoth/setup_test.go`

**Step 1: Write the failing test**

Add to `setup_test.go`:

```go
func TestDetectProviders_AllSet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test123")
	t.Setenv("OPENAI_API_KEY", "sk-test123")
	t.Setenv("GEMINI_API_KEY", "AIzaTest123")

	providers := detectProviders()
	for _, p := range providers {
		if !p.isSet {
			t.Errorf("expected %s to be set", p.envVar)
		}
	}
}

func TestDetectProviders_NoneSet(t *testing.T) {
	// t.Setenv with empty string clears the var for this test
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	providers := detectProviders()
	for _, p := range providers {
		if p.isSet {
			t.Errorf("expected %s to not be set", p.envVar)
		}
	}
}

func TestPrintProviderStatus(t *testing.T) {
	var buf bytes.Buffer
	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", isSet: true},
		{name: "OpenAI", envVar: "OPENAI_API_KEY", isSet: false},
	}
	printProviderStatus(&buf, providers)
	out := buf.String()
	if !strings.Contains(out, "[✓] Anthropic") {
		t.Errorf("expected checkmark for Anthropic, got:\n%s", out)
	}
	if !strings.Contains(out, "[ ] OpenAI") {
		t.Errorf("expected empty box for OpenAI, got:\n%s", out)
	}
}
```

Add `"bytes"` and `"strings"` to the test imports.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth/ -run "TestDetectProviders|TestPrintProviderStatus" -v`
Expected: FAIL — `detectProviders`, `providerInfo`, `printProviderStatus` undefined

**Step 3: Write minimal implementation**

Add to `setup.go`:

```go
import (
	"fmt"
	"io"
	"os"
)

// providerInfo holds the detection state for a single LLM provider.
type providerInfo struct {
	name    string
	envVar  string
	prefix  string // expected key prefix for format validation
	isSet   bool
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth/ -run "TestDetectProviders|TestPrintProviderStatus" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth/setup.go cmd/mammoth/setup_test.go
git commit -m "feat(setup): add provider detection and status display"
```

---

### Task 3: Interactive API key collection with format validation

**Files:**
- Modify: `cmd/mammoth/setup.go`
- Modify: `cmd/mammoth/setup_test.go`

**Step 1: Write the failing test**

Add to `setup_test.go`:

```go
func TestValidateKeyFormat(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		key    string
		valid  bool
	}{
		{"anthropic valid", "sk-ant-", "sk-ant-abc123", true},
		{"anthropic invalid", "sk-ant-", "wrong-key", false},
		{"openai valid", "sk-", "sk-abc123", true},
		{"gemini valid", "AIza", "AIzaSyAbc123", true},
		{"empty key", "sk-ant-", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateKeyFormat(tt.key, tt.prefix)
			if got != tt.valid {
				t.Errorf("validateKeyFormat(%q, %q) = %v, want %v", tt.key, tt.prefix, got, tt.valid)
			}
		})
	}
}

func TestCollectKeys_SkipsSetProviders(t *testing.T) {
	// Simulate user entering nothing (just hitting enter) for unset providers
	input := "\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", prefix: "sk-ant-", isSet: true},
		{name: "OpenAI", envVar: "OPENAI_API_KEY", prefix: "sk-", isSet: false},
		{name: "Gemini", envVar: "GEMINI_API_KEY", prefix: "AIza", isSet: false},
	}

	collected := collectKeys(r, &w, providers)
	if len(collected) != 0 {
		t.Errorf("expected no keys collected when user enters nothing, got %d", len(collected))
	}
	if !strings.Contains(w.String(), "Anthropic") {
		t.Error("expected Anthropic to appear as already set")
	}
}

func TestCollectKeys_AcceptsValidKey(t *testing.T) {
	input := "sk-ant-abc123\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", prefix: "sk-ant-", isSet: false},
	}

	collected := collectKeys(r, &w, providers)
	if len(collected) != 1 {
		t.Fatalf("expected 1 key collected, got %d", len(collected))
	}
	if collected["ANTHROPIC_API_KEY"] != "sk-ant-abc123" {
		t.Errorf("unexpected key value: %q", collected["ANTHROPIC_API_KEY"])
	}
}

func TestCollectKeys_WarnsOnBadFormat(t *testing.T) {
	// User enters a key that doesn't match the prefix, then confirms with Y
	input := "wrong-key\ny\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", prefix: "sk-ant-", isSet: false},
	}

	collected := collectKeys(r, &w, providers)
	if len(collected) != 1 {
		t.Fatalf("expected 1 key collected (user confirmed), got %d", len(collected))
	}
	if !strings.Contains(w.String(), "Warning") {
		t.Error("expected format warning in output")
	}
}

func TestCollectKeys_RejectsOnBadFormatWhenUserDeclines(t *testing.T) {
	// User enters a key that doesn't match the prefix, declines to save
	input := "wrong-key\nn\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", prefix: "sk-ant-", isSet: false},
	}

	collected := collectKeys(r, &w, providers)
	if len(collected) != 0 {
		t.Errorf("expected 0 keys when user declines, got %d", len(collected))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth/ -run "TestValidateKeyFormat|TestCollectKeys" -v`
Expected: FAIL — `validateKeyFormat`, `collectKeys` undefined

**Step 3: Write minimal implementation**

Add to `setup.go`:

```go
import (
	"bufio"
	"io"
	"strings"
)

// validateKeyFormat checks whether a key starts with the expected prefix.
func validateKeyFormat(key, prefix string) bool {
	if key == "" {
		return false
	}
	return strings.HasPrefix(key, prefix)
}

// collectKeys interactively prompts for API keys for providers that aren't
// already set. Returns a map of envVar→key for keys the user entered.
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth/ -run "TestValidateKeyFormat|TestCollectKeys" -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth/setup.go cmd/mammoth/setup_test.go
git commit -m "feat(setup): add interactive API key collection with format validation"
```

---

### Task 4: Write .env file (append, no clobber)

**Files:**
- Modify: `cmd/mammoth/setup.go`
- Modify: `cmd/mammoth/setup_test.go`

**Step 1: Write the failing test**

Add to `setup_test.go`:

```go
func TestWriteEnvFile_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	keys := map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-test123",
		"OPENAI_API_KEY":    "sk-test456",
	}

	err := writeEnvFile(path, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read .env: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "ANTHROPIC_API_KEY=sk-ant-test123") {
		t.Errorf("missing ANTHROPIC_API_KEY in .env:\n%s", content)
	}
	if !strings.Contains(content, "OPENAI_API_KEY=sk-test456") {
		t.Errorf("missing OPENAI_API_KEY in .env:\n%s", content)
	}
}

func TestWriteEnvFile_AppendsWithoutClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	// Write an initial .env
	if err := os.WriteFile(path, []byte("EXISTING_VAR=hello\nANTHROPIC_API_KEY=old-key\n"), 0644); err != nil {
		t.Fatal(err)
	}

	keys := map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-new-key",
		"OPENAI_API_KEY":    "sk-new-key",
	}

	err := writeEnvFile(path, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	// Existing var must be preserved
	if !strings.Contains(content, "EXISTING_VAR=hello") {
		t.Errorf("existing var was clobbered:\n%s", content)
	}
	// Existing key should be updated
	if !strings.Contains(content, "ANTHROPIC_API_KEY=sk-ant-new-key") {
		t.Errorf("expected updated ANTHROPIC_API_KEY:\n%s", content)
	}
	// New key should be appended
	if !strings.Contains(content, "OPENAI_API_KEY=sk-new-key") {
		t.Errorf("expected new OPENAI_API_KEY:\n%s", content)
	}
	// Old value should not appear
	if strings.Contains(content, "old-key") {
		t.Errorf("old key value should be replaced:\n%s", content)
	}
}

func TestWriteEnvFile_EmptyKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	err := writeEnvFile(path, map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File should not be created for empty keys
	if _, err := os.Stat(path); err == nil {
		t.Error("expected no .env file to be created for empty keys")
	}
}
```

Add `"path/filepath"` to the test imports.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth/ -run TestWriteEnvFile -v`
Expected: FAIL — `writeEnvFile` undefined

**Step 3: Write minimal implementation**

Add to `setup.go`:

```go
// writeEnvFile writes collected API keys to a .env file. If the file already
// exists, it updates matching keys in place and appends new ones. Existing
// lines that don't match any collected key are preserved as-is. Does nothing
// if keys is empty.
func writeEnvFile(path string, keys map[string]string) error {
	if len(keys) == 0 {
		return nil
	}

	// Read existing content if the file exists.
	var existingLines []string
	if data, err := os.ReadFile(path); err == nil {
		existingLines = strings.Split(string(data), "\n")
	}

	// Track which keys we've already written (via update-in-place).
	written := map[string]bool{}
	var outputLines []string

	for _, line := range existingLines {
		trimmed := strings.TrimSpace(line)
		// Check if this line sets a key we're updating.
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

	// Append any keys that weren't already in the file.
	for envVar, value := range keys {
		if !written[envVar] {
			outputLines = append(outputLines, envVar+"="+value)
		}
	}

	// Clean up trailing empty lines, then ensure final newline.
	for len(outputLines) > 0 && strings.TrimSpace(outputLines[len(outputLines)-1]) == "" {
		outputLines = outputLines[:len(outputLines)-1]
	}

	content := strings.Join(outputLines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0600)
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth/ -run TestWriteEnvFile -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth/setup.go cmd/mammoth/setup_test.go
git commit -m "feat(setup): add .env file writing with append and no-clobber"
```

---

### Task 5: Summary output and printQuickStart

**Files:**
- Modify: `cmd/mammoth/setup.go`
- Modify: `cmd/mammoth/setup_test.go`

**Step 1: Write the failing test**

Add to `setup_test.go`:

```go
func TestPrintQuickStart_WithProviders(t *testing.T) {
	var buf bytes.Buffer
	configured := []string{"Anthropic", "OpenAI"}
	printQuickStart(&buf, configured)

	out := buf.String()
	if !strings.Contains(out, "Anthropic, OpenAI") {
		t.Errorf("expected configured providers listed:\n%s", out)
	}
	if !strings.Contains(out, "mammoth serve") {
		t.Errorf("expected mammoth serve in quickstart:\n%s", out)
	}
}

func TestPrintQuickStart_NoProviders(t *testing.T) {
	var buf bytes.Buffer
	printQuickStart(&buf, nil)

	out := buf.String()
	if !strings.Contains(out, "No API keys configured") {
		t.Errorf("expected no-keys message:\n%s", out)
	}
	if !strings.Contains(out, "mammoth serve") {
		t.Errorf("expected mammoth serve even with no keys:\n%s", out)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth/ -run TestPrintQuickStart -v`
Expected: FAIL — `printQuickStart` undefined

**Step 3: Write minimal implementation**

Add to `setup.go`:

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth/ -run TestPrintQuickStart -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth/setup.go cmd/mammoth/setup_test.go
git commit -m "feat(setup): add quickstart summary output"
```

---

### Task 6: Wire runSetup end-to-end

**Files:**
- Modify: `cmd/mammoth/setup.go`
- Modify: `cmd/mammoth/setup_test.go`

**Step 1: Write the failing test**

Add to `setup_test.go`:

```go
func TestRunSetupEndToEnd(t *testing.T) {
	// Clear all API keys for a clean test
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// Simulate user entering an Anthropic key and skipping others
	input := "sk-ant-test123\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	cfg := setupConfig{envFile: envPath}
	code := runSetupWithIO(cfg, r, &w)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// Verify .env was written
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected .env to exist: %v", err)
	}
	if !strings.Contains(string(data), "ANTHROPIC_API_KEY=sk-ant-test123") {
		t.Errorf("expected key in .env:\n%s", string(data))
	}

	// Verify summary was printed
	out := w.String()
	if !strings.Contains(out, "Anthropic") {
		t.Errorf("expected Anthropic in summary:\n%s", out)
	}
	if !strings.Contains(out, "mammoth serve") {
		t.Errorf("expected quickstart in output:\n%s", out)
	}
}

func TestRunSetupSkipKeys(t *testing.T) {
	var w bytes.Buffer
	r := strings.NewReader("")

	dir := t.TempDir()
	cfg := setupConfig{skipKeys: true, envFile: filepath.Join(dir, ".env")}
	code := runSetupWithIO(cfg, r, &w)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// .env should not be created when skipping keys and none collected
	if _, err := os.Stat(cfg.envFile); err == nil {
		t.Error("expected no .env file when keys skipped")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth/ -run "TestRunSetupEndToEnd|TestRunSetupSkipKeys" -v`
Expected: FAIL — `runSetupWithIO` undefined

**Step 3: Write minimal implementation**

Update `setup.go` — replace the stub `runSetup` and add `runSetupWithIO`:

```go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth/ -run "TestRunSetupEndToEnd|TestRunSetupSkipKeys" -v`
Expected: PASS

**Step 5: Run all setup tests**

Run: `go test ./cmd/mammoth/ -run "TestParseSetupArgs|TestDetectProviders|TestPrintProviderStatus|TestValidateKeyFormat|TestCollectKeys|TestWriteEnvFile|TestPrintQuickStart|TestRunSetup" -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add cmd/mammoth/setup.go cmd/mammoth/setup_test.go
git commit -m "feat(setup): wire end-to-end setup wizard with runSetupWithIO"
```

---

### Task 7: Update help text to mention setup

**Files:**
- Modify: `cmd/mammoth/help.go`
- Modify: `cmd/mammoth/help_test.go`

**Step 1: Write the failing test**

Add to `help_test.go`:

```go
func TestPrintHelp_IncludesSetup(t *testing.T) {
	var buf bytes.Buffer
	printHelp(&buf, "test")
	if !strings.Contains(buf.String(), "mammoth setup") {
		t.Error("expected help to mention mammoth setup")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/mammoth/ -run TestPrintHelp_IncludesSetup -v`
Expected: FAIL — "mammoth setup" not in help output

**Step 3: Update help.go**

In the Usage section of `printHelp`, add after the serve line:

```go
fmt.Fprintln(w, "  mammoth setup                       Interactive setup wizard")
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/mammoth/ -run TestPrintHelp_IncludesSetup -v`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./cmd/mammoth/ -v`
Expected: All PASS

**Step 6: Commit**

```bash
git add cmd/mammoth/help.go cmd/mammoth/help_test.go
git commit -m "feat(setup): add setup to help output"
```

---

### Task 8: Full integration test and final polish

**Files:**
- Modify: `cmd/mammoth/setup_test.go`

**Step 1: Write the integration test**

Add to `setup_test.go`:

```go
func TestRunSetupEndToEnd_ExistingEnvFile(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// Pre-existing .env with a custom var
	if err := os.WriteFile(envPath, []byte("MY_CUSTOM_VAR=keepme\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// User enters an Anthropic key
	input := "sk-ant-integration\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	cfg := setupConfig{envFile: envPath}
	code := runSetupWithIO(cfg, r, &w)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	// Custom var preserved
	if !strings.Contains(content, "MY_CUSTOM_VAR=keepme") {
		t.Errorf("custom var was clobbered:\n%s", content)
	}
	// New key added
	if !strings.Contains(content, "ANTHROPIC_API_KEY=sk-ant-integration") {
		t.Errorf("new key not written:\n%s", content)
	}
}

func TestRunSetupEndToEnd_PreExistingKeys(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-already-set")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	// User enters an OpenAI key, skips Gemini
	input := "sk-openai-new\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	cfg := setupConfig{envFile: envPath}
	code := runSetupWithIO(cfg, r, &w)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	out := w.String()
	// Should show Anthropic as already set
	if !strings.Contains(out, "already set") {
		t.Errorf("expected 'already set' for Anthropic:\n%s", out)
	}
	// Summary should list both configured providers
	if !strings.Contains(out, "Anthropic") || !strings.Contains(out, "OpenAI") {
		t.Errorf("expected both providers in summary:\n%s", out)
	}
}
```

**Step 2: Run all tests**

Run: `go test ./cmd/mammoth/ -v`
Expected: All PASS

**Step 3: Run the full project test suite**

Run: `go test ./...`
Expected: All PASS

**Step 4: Commit**

```bash
git add cmd/mammoth/setup_test.go
git commit -m "test(setup): add integration tests for existing .env and pre-existing keys"
```
