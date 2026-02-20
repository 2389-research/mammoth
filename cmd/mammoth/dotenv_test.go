// ABOUTME: Tests for the .env file loader that reads KEY=VALUE pairs into the process environment.
// ABOUTME: Covers plain values, quoted values, comments, empty lines, and no-clobber behavior.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempEnv(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDotEnvSetsVariables(t *testing.T) {
	path := writeTempEnv(t, "TEST_DOTENV_A=hello\nTEST_DOTENV_B=world\n")
	t.Setenv("TEST_DOTENV_A", "")
	t.Setenv("TEST_DOTENV_B", "")
	os.Unsetenv("TEST_DOTENV_A")
	os.Unsetenv("TEST_DOTENV_B")

	loadDotEnv(path)

	if got := os.Getenv("TEST_DOTENV_A"); got != "hello" {
		t.Errorf("expected TEST_DOTENV_A=hello, got %q", got)
	}
	if got := os.Getenv("TEST_DOTENV_B"); got != "world" {
		t.Errorf("expected TEST_DOTENV_B=world, got %q", got)
	}
}

func TestLoadDotEnvDoubleQuotedValues(t *testing.T) {
	path := writeTempEnv(t, `TEST_DOTENV_Q="quoted value"`)
	t.Setenv("TEST_DOTENV_Q", "")
	os.Unsetenv("TEST_DOTENV_Q")

	loadDotEnv(path)

	if got := os.Getenv("TEST_DOTENV_Q"); got != "quoted value" {
		t.Errorf("expected TEST_DOTENV_Q='quoted value', got %q", got)
	}
}

func TestLoadDotEnvSingleQuotedValues(t *testing.T) {
	path := writeTempEnv(t, `TEST_DOTENV_S='single quoted'`)
	t.Setenv("TEST_DOTENV_S", "")
	os.Unsetenv("TEST_DOTENV_S")

	loadDotEnv(path)

	if got := os.Getenv("TEST_DOTENV_S"); got != "single quoted" {
		t.Errorf("expected TEST_DOTENV_S='single quoted', got %q", got)
	}
}

func TestLoadDotEnvSkipsComments(t *testing.T) {
	path := writeTempEnv(t, "# this is a comment\nTEST_DOTENV_C=yes\n# another comment\n")
	t.Setenv("TEST_DOTENV_C", "")
	os.Unsetenv("TEST_DOTENV_C")

	loadDotEnv(path)

	if got := os.Getenv("TEST_DOTENV_C"); got != "yes" {
		t.Errorf("expected TEST_DOTENV_C=yes, got %q", got)
	}
}

func TestLoadDotEnvSkipsEmptyLines(t *testing.T) {
	path := writeTempEnv(t, "\n\nTEST_DOTENV_E=present\n\n")
	t.Setenv("TEST_DOTENV_E", "")
	os.Unsetenv("TEST_DOTENV_E")

	loadDotEnv(path)

	if got := os.Getenv("TEST_DOTENV_E"); got != "present" {
		t.Errorf("expected TEST_DOTENV_E=present, got %q", got)
	}
}

func TestLoadDotEnvDoesNotClobberExisting(t *testing.T) {
	path := writeTempEnv(t, "TEST_DOTENV_X=from_file")
	t.Setenv("TEST_DOTENV_X", "already_set")

	loadDotEnv(path)

	if got := os.Getenv("TEST_DOTENV_X"); got != "already_set" {
		t.Errorf("expected existing env var to be preserved, got %q", got)
	}
}

func TestLoadDotEnvMissingFileIsNoOp(t *testing.T) {
	// Should not panic or error when the file doesn't exist.
	loadDotEnv("/tmp/this-env-file-definitely-does-not-exist")
}

func TestLoadDotEnvExportPrefix(t *testing.T) {
	path := writeTempEnv(t, "export TEST_DOTENV_EX=exported\n")
	t.Setenv("TEST_DOTENV_EX", "")
	os.Unsetenv("TEST_DOTENV_EX")

	loadDotEnv(path)

	if got := os.Getenv("TEST_DOTENV_EX"); got != "exported" {
		t.Errorf("expected TEST_DOTENV_EX=exported, got %q", got)
	}
}

func TestLoadDotEnvValueWithEquals(t *testing.T) {
	path := writeTempEnv(t, "TEST_DOTENV_EQ=a=b=c\n")
	t.Setenv("TEST_DOTENV_EQ", "")
	os.Unsetenv("TEST_DOTENV_EQ")

	loadDotEnv(path)

	if got := os.Getenv("TEST_DOTENV_EQ"); got != "a=b=c" {
		t.Errorf("expected TEST_DOTENV_EQ=a=b=c, got %q", got)
	}
}

func TestLoadDotEnvAutoLoadsXDGConfig(t *testing.T) {
	configDir := t.TempDir()
	mammothDir := filepath.Join(configDir, "mammoth")
	os.MkdirAll(mammothDir, 0755)
	configPath := filepath.Join(mammothDir, "config.env")
	os.WriteFile(configPath, []byte("TEST_XDG_AUTO_LOAD=from_xdg\n"), 0644)

	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("TEST_XDG_AUTO_LOAD", "")
	os.Unsetenv("TEST_XDG_AUTO_LOAD")

	loadDotEnvAuto()

	if got := os.Getenv("TEST_XDG_AUTO_LOAD"); got != "from_xdg" {
		t.Errorf("expected TEST_XDG_AUTO_LOAD=from_xdg, got %q", got)
	}
}
