// ABOUTME: Tests for the list-handlers command, verifying handler type discovery and JSON output.
// ABOUTME: Checks that getHandlerTypes returns sorted, non-empty list matching default registry.
package main

import (
	"os"
	"strings"
	"testing"
)

func TestGetHandlerTypes_NonEmpty(t *testing.T) {
	types := getHandlerTypes()
	if len(types) == 0 {
		t.Fatal("getHandlerTypes returned empty list")
	}
}

func TestGetHandlerTypes_Sorted(t *testing.T) {
	types := getHandlerTypes()
	for i := 1; i < len(types); i++ {
		if types[i] < types[i-1] {
			t.Errorf("types not sorted: %q comes after %q", types[i], types[i-1])
		}
	}
}

func TestGetHandlerTypes_ContainsExpected(t *testing.T) {
	types := getHandlerTypes()
	expected := []string{"start", "exit", "codergen", "wait.human", "conditional", "parallel"}
	typeSet := make(map[string]bool)
	for _, ty := range types {
		typeSet[ty] = true
	}
	for _, want := range expected {
		if !typeSet[want] {
			t.Errorf("expected handler type %q not found in %v", want, types)
		}
	}
}

func TestCmdListHandlers_ExitCode(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	exitCode := cmdListHandlers()

	w.Close()
	os.Stdout = oldStdout

	if exitCode != 0 {
		t.Errorf("exit code = %d, want 0", exitCode)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])
	if !strings.Contains(output, "codergen") {
		t.Errorf("output should contain codergen: %s", output)
	}
	if !strings.Contains(output, "start") {
		t.Errorf("output should contain start: %s", output)
	}
}
