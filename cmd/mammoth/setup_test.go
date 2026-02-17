// ABOUTME: Tests for the mammoth setup subcommand.
// ABOUTME: Validates argument parsing, key collection, .env writing, and summary output.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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
	if !strings.Contains(out, "[✓]") {
		t.Errorf("expected checkmark for set provider, got:\n%s", out)
	}
	if !strings.Contains(out, "[ ]") {
		t.Errorf("expected empty box for unset provider, got:\n%s", out)
	}
}

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
	if !strings.Contains(content, "EXISTING_VAR=hello") {
		t.Errorf("existing var was clobbered:\n%s", content)
	}
	if !strings.Contains(content, "ANTHROPIC_API_KEY=sk-ant-new-key") {
		t.Errorf("expected updated ANTHROPIC_API_KEY:\n%s", content)
	}
	if !strings.Contains(content, "OPENAI_API_KEY=sk-new-key") {
		t.Errorf("expected new OPENAI_API_KEY:\n%s", content)
	}
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

	if _, err := os.Stat(path); err == nil {
		t.Error("expected no .env file to be created for empty keys")
	}
}

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

func TestRunSetupEndToEnd(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	input := "sk-ant-test123\n\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	cfg := setupConfig{envFile: envPath}
	code := runSetupWithIO(cfg, r, &w)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected .env to exist: %v", err)
	}
	if !strings.Contains(string(data), "ANTHROPIC_API_KEY=sk-ant-test123") {
		t.Errorf("expected key in .env:\n%s", string(data))
	}

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

	if _, err := os.Stat(cfg.envFile); err == nil {
		t.Error("expected no .env file when keys skipped")
	}
}

func TestRunSetupEndToEnd_ExistingEnvFile(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	if err := os.WriteFile(envPath, []byte("MY_CUSTOM_VAR=keepme\n"), 0644); err != nil {
		t.Fatal(err)
	}

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

	if !strings.Contains(content, "MY_CUSTOM_VAR=keepme") {
		t.Errorf("custom var was clobbered:\n%s", content)
	}
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

	input := "sk-openai-new\n\n"
	r := strings.NewReader(input)
	var w bytes.Buffer

	cfg := setupConfig{envFile: envPath}
	code := runSetupWithIO(cfg, r, &w)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	out := w.String()
	if !strings.Contains(out, "already set") {
		t.Errorf("expected 'already set' for Anthropic:\n%s", out)
	}
	if !strings.Contains(out, "Anthropic") || !strings.Contains(out, "OpenAI") {
		t.Errorf("expected both providers in summary:\n%s", out)
	}
}
