# Enhanced Setup Command Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enhance `mammoth setup` to collect API keys + base URLs and store in XDG config directory.

**Architecture:** Add `defaultConfigDir()` for XDG_CONFIG_HOME resolution, extend `providerInfo` with base URL env var, update setup wizard to prompt for base URLs, change default write target to XDG config, update `loadDotEnvAuto()` to include XDG config in load chain.

**Tech Stack:** Go 1.25+, standard library only

---

### Task 1: Add `defaultConfigDir()` to datadir.go

**Files:**
- Modify: `cmd/mammoth/datadir.go`
- Modify: `cmd/mammoth/datadir_test.go`

**Step 1: Write failing tests**

Add to `cmd/mammoth/datadir_test.go`:

```go
func TestDefaultConfigDirUsesXDGConfigHome(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", customDir)

	got, err := defaultConfigDir()
	if err != nil {
		t.Fatalf("defaultConfigDir failed: %v", err)
	}

	want := filepath.Join(customDir, "mammoth")
	if got != want {
		t.Errorf("defaultConfigDir() = %q, want %q", got, want)
	}
}

func TestDefaultConfigDirFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := defaultConfigDir()
	if err != nil {
		t.Fatalf("defaultConfigDir failed: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	want := filepath.Join(home, ".config", "mammoth")
	if got != want {
		t.Errorf("defaultConfigDir() = %q, want %q", got, want)
	}
}

func TestDefaultConfigDirReturnsAbsolutePath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")

	got, err := defaultConfigDir()
	if err != nil {
		t.Fatalf("defaultConfigDir failed: %v", err)
	}

	if !filepath.IsAbs(got) {
		t.Errorf("defaultConfigDir() returned relative path: %q", got)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/mammoth/ -run TestDefaultConfigDir -v`
Expected: FAIL — `defaultConfigDir` undefined

**Step 3: Implement `defaultConfigDir()`**

Add to `cmd/mammoth/datadir.go`:

```go
// defaultConfigDir returns the default config directory for mammoth settings.
// It checks XDG_CONFIG_HOME first, then falls back to ~/.config/mammoth.
func defaultConfigDir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mammoth"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".config", "mammoth"), nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./cmd/mammoth/ -run TestDefaultConfigDir -v`
Expected: PASS (3 tests)

**Step 5: Commit**

```bash
git add cmd/mammoth/datadir.go cmd/mammoth/datadir_test.go
git commit -m "feat(setup): add defaultConfigDir for XDG config resolution"
```

---

### Task 2: Add base URL collection to setup wizard

**Files:**
- Modify: `cmd/mammoth/setup.go`
- Modify: `cmd/mammoth/setup_test.go`

**Step 1: Write failing tests**

Add to `cmd/mammoth/setup_test.go`:

```go
func TestCollectBaseURLs_PromptsForUnsetProviders(t *testing.T) {
	input := "https://custom.anthropic.example.com\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", baseURLVar: "ANTHROPIC_BASE_URL", prefix: "sk-ant-", isSet: true},
		{name: "OpenAI", envVar: "OPENAI_API_KEY", baseURLVar: "OPENAI_BASE_URL", prefix: "sk-", isSet: true},
	}

	collected := collectBaseURLs(r, &w, providers)
	if collected["ANTHROPIC_BASE_URL"] != "https://custom.anthropic.example.com" {
		t.Errorf("expected Anthropic base URL, got %q", collected["ANTHROPIC_BASE_URL"])
	}
	if _, ok := collected["OPENAI_BASE_URL"]; ok {
		t.Error("expected no OpenAI base URL when blank entered")
	}
}

func TestCollectBaseURLs_SkipsProvidersWithoutKeys(t *testing.T) {
	r := strings.NewReader("")
	var w bytes.Buffer

	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", baseURLVar: "ANTHROPIC_BASE_URL", prefix: "sk-ant-", isSet: false},
	}

	collected := collectBaseURLs(r, &w, providers)
	if len(collected) != 0 {
		t.Errorf("expected no base URLs for providers without keys, got %d", len(collected))
	}
}

func TestCollectBaseURLs_SkipsAlreadySetBaseURLs(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "https://existing.example.com")

	r := strings.NewReader("")
	var w bytes.Buffer

	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", baseURLVar: "ANTHROPIC_BASE_URL", prefix: "sk-ant-", isSet: true},
	}

	collected := collectBaseURLs(r, &w, providers)
	if len(collected) != 0 {
		t.Errorf("expected no base URLs when already set, got %d", len(collected))
	}
	if !strings.Contains(w.String(), "already set") {
		t.Error("expected 'already set' message for existing base URL")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/mammoth/ -run TestCollectBaseURLs -v`
Expected: FAIL — `baseURLVar` field and `collectBaseURLs` undefined

**Step 3: Implement changes**

3a. Add `baseURLVar` field to `providerInfo` struct in `setup.go`:

```go
type providerInfo struct {
	name       string
	envVar     string
	baseURLVar string // env var for the provider's base URL
	prefix     string
	isSet      bool
}
```

3b. Update `detectProviders()` to include baseURLVar:

```go
func detectProviders() []providerInfo {
	providers := []providerInfo{
		{name: "Anthropic", envVar: "ANTHROPIC_API_KEY", baseURLVar: "ANTHROPIC_BASE_URL", prefix: "sk-ant-"},
		{name: "OpenAI", envVar: "OPENAI_API_KEY", baseURLVar: "OPENAI_BASE_URL", prefix: "sk-"},
		{name: "Gemini", envVar: "GEMINI_API_KEY", baseURLVar: "GEMINI_BASE_URL", prefix: "AIza"},
	}
	for i := range providers {
		providers[i].isSet = os.Getenv(providers[i].envVar) != ""
	}
	return providers
}
```

3c. Add `collectBaseURLs()` function:

```go
// collectBaseURLs prompts for base URLs for providers that have API keys configured.
// Only prompts for providers where isSet is true (key exists) and the base URL env
// var is not already set. Returns a map of baseURLVar->url for URLs the user entered.
func collectBaseURLs(r io.Reader, w io.Writer, providers []providerInfo) map[string]string {
	scanner := bufio.NewScanner(r)
	collected := map[string]string{}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "Base URLs (leave blank for provider defaults):")
	fmt.Fprintln(w)

	for _, p := range providers {
		if !p.isSet {
			continue
		}

		if existing := os.Getenv(p.baseURLVar); existing != "" {
			fmt.Fprintf(w, "  %s base URL: already set ✓\n", p.name)
			continue
		}

		fmt.Fprintf(w, "  %s base URL (%s): ", p.name, p.baseURLVar)
		if !scanner.Scan() {
			break
		}
		url := strings.TrimSpace(scanner.Text())
		if url == "" {
			continue
		}

		collected[p.baseURLVar] = url
	}

	return collected
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./cmd/mammoth/ -run TestCollectBaseURLs -v`
Expected: PASS (3 tests)

Also run existing tests to verify no regressions:
Run: `go test ./cmd/mammoth/ -v`
Expected: All pass

**Step 5: Commit**

```bash
git add cmd/mammoth/setup.go cmd/mammoth/setup_test.go
git commit -m "feat(setup): add base URL collection for LLM providers"
```

---

### Task 3: Change default write target to XDG config and update loadDotEnvAuto

**Files:**
- Modify: `cmd/mammoth/setup.go`
- Modify: `cmd/mammoth/dotenv.go`
- Modify: `cmd/mammoth/setup_test.go`
- Modify: `cmd/mammoth/dotenv_test.go`

**Step 1: Write failing tests**

Add to `cmd/mammoth/dotenv_test.go`:

```go
func TestLoadDotEnvAutoLoadsXDGConfig(t *testing.T) {
	// Create a temp XDG config dir with a config.env
	configDir := t.TempDir()
	mammothDir := filepath.Join(configDir, "mammoth")
	os.MkdirAll(mammothDir, 0755)
	configPath := filepath.Join(mammothDir, "config.env")
	os.WriteFile(configPath, []byte("TEST_XDG_LOAD=from_xdg\n"), 0644)

	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("TEST_XDG_LOAD", "")
	os.Unsetenv("TEST_XDG_LOAD")

	loadDotEnvAuto()

	if got := os.Getenv("TEST_XDG_LOAD"); got != "from_xdg" {
		t.Errorf("expected TEST_XDG_LOAD=from_xdg, got %q", got)
	}
}
```

Add to `cmd/mammoth/setup_test.go`:

```go
func TestRunSetupWritesToXDGConfig(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Input: anthropic key, blank base URL, blank openai, blank gemini
	input := "sk-ant-xdg-test\n\n\n\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	cfg := setupConfig{} // no envFile override — should use XDG default
	code := runSetupWithIO(cfg, r, &w)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\noutput:\n%s", code, w.String())
	}

	expectedPath := filepath.Join(configDir, "mammoth", "config.env")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("expected config.env at %s: %v", expectedPath, err)
	}
	if !strings.Contains(string(data), "ANTHROPIC_API_KEY=sk-ant-xdg-test") {
		t.Errorf("expected key in config.env:\n%s", string(data))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./cmd/mammoth/ -run "TestLoadDotEnvAutoLoadsXDGConfig|TestRunSetupWritesToXDGConfig" -v`
Expected: FAIL

**Step 3: Implement changes**

3a. Update `loadDotEnvAuto()` in `dotenv.go` to include XDG config:

After the exe-dir block, add:

```go
// XDG config directory (lowest priority — loaded last so local .env wins).
if cfgDir, err := defaultConfigDir(); err == nil {
	addPath(filepath.Join(cfgDir, "config.env"))
}
```

3b. Update `setupConfig` to default to empty string (meaning "use XDG"):

Change the field tag and default:
```go
type setupConfig struct {
	skipKeys bool
	envFile  string // empty string means use XDG config dir
}
```

3c. Update `runSetupWithIO()` to resolve envFile default:

Add near the top of the function:
```go
// Resolve default config path: XDG config dir.
envFile := cfg.envFile
if envFile == "" {
	cfgDir, err := defaultConfigDir()
	if err != nil {
		fmt.Fprintf(w, "Error resolving config directory: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		fmt.Fprintf(w, "Error creating config directory: %v\n", err)
		return 1
	}
	envFile = filepath.Join(cfgDir, "config.env")
}
```

Replace `cfg.envFile` references in the function with `envFile`.

3d. Integrate base URL collection into `runSetupWithIO()`:

After key collection, add base URL collection. Merge base URL map into collected map before writing.

3e. Update `parseSetupArgs` default for envFile from `.env` to `""`:

```go
fs.StringVar(&cfg.envFile, "env-file", "", "Path to write config file (default: XDG config dir)")
```

**Step 4: Run tests to verify they pass**

Run: `go test ./cmd/mammoth/ -v`
Expected: All pass (existing + new)

**Step 5: Commit**

```bash
git add cmd/mammoth/setup.go cmd/mammoth/setup_test.go cmd/mammoth/dotenv.go cmd/mammoth/dotenv_test.go
git commit -m "feat(setup): store config in XDG config dir, load from XDG in dotenv"
```

---

### Task 4: Update help text and verify E2E

**Files:**
- Modify: `cmd/mammoth/help.go`

**Step 1: Update help text**

Update the setup section in `help.go` to mention XDG config storage and base URL configuration.

**Step 2: Run full test suite**

Run: `go test -count=1 ./...`
Expected: All pass

**Step 3: Build and verify E2E**

```bash
go build -o /tmp/mammoth-test ./cmd/mammoth
XDG_CONFIG_HOME=/tmp/test-xdg /tmp/mammoth-test setup --help
```

**Step 4: Commit**

```bash
git add cmd/mammoth/help.go
git commit -m "docs(setup): update help text for XDG config and base URLs"
```
