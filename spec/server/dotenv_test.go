// ABOUTME: Tests for the .env file loader.
// ABOUTME: Verifies parsing of KEY=VALUE pairs, comments, quotes, and no-override behavior.
package server

import (
	"os"
	"path/filepath"
	"testing"
)

// unsetForTest unsets an env var and registers cleanup to unset it again after the test.
func unsetForTest(t *testing.T, key string) {
	t.Helper()
	_ = os.Unsetenv(key)
	t.Cleanup(func() { _ = os.Unsetenv(key) })
}

func TestLoadDotEnv_BasicKeyValue(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	_ = os.WriteFile(envFile, []byte("TEST_DOTENV_BASIC=hello\n"), 0o644)

	unsetForTest(t, "TEST_DOTENV_BASIC")

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	got := os.Getenv("TEST_DOTENV_BASIC")
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestLoadDotEnv_DoesNotOverrideExisting(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	_ = os.WriteFile(envFile, []byte("TEST_DOTENV_EXISTING=fromfile\n"), 0o644)

	t.Setenv("TEST_DOTENV_EXISTING", "fromenv")

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	got := os.Getenv("TEST_DOTENV_EXISTING")
	if got != "fromenv" {
		t.Errorf("got %q, want %q (should not override)", got, "fromenv")
	}
}

func TestLoadDotEnv_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := `TEST_DOTENV_DOUBLE="double quoted"
TEST_DOTENV_SINGLE='single quoted'
`
	_ = os.WriteFile(envFile, []byte(content), 0o644)

	unsetForTest(t, "TEST_DOTENV_DOUBLE")
	unsetForTest(t, "TEST_DOTENV_SINGLE")

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	if got := os.Getenv("TEST_DOTENV_DOUBLE"); got != "double quoted" {
		t.Errorf("double: got %q, want %q", got, "double quoted")
	}
	if got := os.Getenv("TEST_DOTENV_SINGLE"); got != "single quoted" {
		t.Errorf("single: got %q, want %q", got, "single quoted")
	}
}

func TestLoadDotEnv_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := `# This is a comment
TEST_DOTENV_COMMENT=works

# Another comment

TEST_DOTENV_AFTER_BLANK=also_works
`
	_ = os.WriteFile(envFile, []byte(content), 0o644)

	unsetForTest(t, "TEST_DOTENV_COMMENT")
	unsetForTest(t, "TEST_DOTENV_AFTER_BLANK")

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	if got := os.Getenv("TEST_DOTENV_COMMENT"); got != "works" {
		t.Errorf("comment: got %q, want %q", got, "works")
	}
	if got := os.Getenv("TEST_DOTENV_AFTER_BLANK"); got != "also_works" {
		t.Errorf("after_blank: got %q, want %q", got, "also_works")
	}
}

func TestLoadDotEnv_MissingFile(t *testing.T) {
	err := LoadDotEnv("/nonexistent/path/.env")
	if err != nil {
		t.Errorf("expected nil for missing file, got: %v", err)
	}
}

func TestLoadDotEnv_EmptyValue(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	_ = os.WriteFile(envFile, []byte("TEST_DOTENV_EMPTY=\n"), 0o644)

	unsetForTest(t, "TEST_DOTENV_EMPTY")

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	val, exists := os.LookupEnv("TEST_DOTENV_EMPTY")
	if !exists {
		t.Error("expected TEST_DOTENV_EMPTY to be set")
	}
	if val != "" {
		t.Errorf("got %q, want empty string", val)
	}
}

func TestLoadDotEnv_SpacesAroundEquals(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	_ = os.WriteFile(envFile, []byte("TEST_DOTENV_SPACES = spaced\n"), 0o644)

	unsetForTest(t, "TEST_DOTENV_SPACES")

	if err := LoadDotEnv(envFile); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	if got := os.Getenv("TEST_DOTENV_SPACES"); got != "spaced" {
		t.Errorf("got %q, want %q", got, "spaced")
	}
}
