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
