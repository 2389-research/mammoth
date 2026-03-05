// ABOUTME: Tests for the CLI entrypoint dispatch routing and usage handling.
// ABOUTME: Verifies unknown commands, missing args, missing file args, and correct subcommand routing.
package main

import (
	"os"
	"testing"
)

func TestDispatch_UnknownCommand(t *testing.T) {
	// Capture stderr (unknown command writes usage to stderr)
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := dispatch([]string{"mammoth-conformance", "bogus"})

	w.Close()
	os.Stderr = oldStderr

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for unknown command", exitCode)
	}
}

func TestDispatch_MissingArgs(t *testing.T) {
	// Capture stderr for printUsage
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := dispatch([]string{"mammoth-conformance"})

	w.Close()
	os.Stderr = oldStderr

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for missing args", exitCode)
	}
}

func TestDispatch_ParseMissingFile(t *testing.T) {
	// Capture stderr (missing arg writes usage to stderr)
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := dispatch([]string{"mammoth-conformance", "parse"})

	w.Close()
	os.Stderr = oldStderr

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for parse without file", exitCode)
	}
}

func TestDispatch_ValidateMissingFile(t *testing.T) {
	// Capture stderr (missing arg writes usage to stderr)
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := dispatch([]string{"mammoth-conformance", "validate"})

	w.Close()
	os.Stderr = oldStderr

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for validate without file", exitCode)
	}
}

func TestDispatch_RunMissingFile(t *testing.T) {
	// Capture stderr (missing arg writes usage to stderr)
	oldStderr := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w

	exitCode := dispatch([]string{"mammoth-conformance", "run"})

	w.Close()
	os.Stderr = oldStderr

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for run without file", exitCode)
	}
}

func TestDispatch_ParseNonexistentFile(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := dispatch([]string{"mammoth-conformance", "parse", "/nonexistent/file.dot"})

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 for nonexistent file", exitCode)
	}
}

func TestDispatch_ListHandlers(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := dispatch([]string{"mammoth-conformance", "list-handlers"})

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0 for list-handlers", exitCode)
	}
}
