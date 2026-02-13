// ABOUTME: Tests for the tool registry factory function BuildRegistry.
// ABOUTME: Validates that all 7 domain tools are registered and retrievable by name.
package tools

import (
	"sort"
	"sync/atomic"
	"testing"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mux/tool"
)

func makeTestActor() *core.SpecActorHandle {
	specID := core.NewULID()
	return core.SpawnActor(specID, core.NewSpecState())
}

func TestBuildRegistryRegistersAll7Tools(t *testing.T) {
	actor := makeTestActor()
	pending := &atomic.Bool{}
	registry := BuildRegistry(actor, pending, "test-agent", &atomic.Bool{})

	if registry.Count() != 7 {
		t.Errorf("expected 7 tools, got %d", registry.Count())
	}

	names := registry.List()
	sort.Strings(names)

	expected := []string{
		"ask_user_boolean",
		"ask_user_freeform",
		"ask_user_multiple_choice",
		"emit_diff_summary",
		"emit_narration",
		"read_state",
		"write_commands",
	}
	sort.Strings(expected)

	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("expected name %q at index %d, got %q", name, i, names[i])
		}
	}
}

func TestRegistryToolsAreRetrievableByName(t *testing.T) {
	actor := makeTestActor()
	pending := &atomic.Bool{}
	registry := BuildRegistry(actor, pending, "test-agent", &atomic.Bool{})

	toolNames := []string{
		"read_state",
		"write_commands",
		"emit_narration",
		"emit_diff_summary",
		"ask_user_boolean",
		"ask_user_multiple_choice",
		"ask_user_freeform",
	}

	for _, name := range toolNames {
		got, ok := registry.Get(name)
		if !ok {
			t.Errorf("tool '%s' should be in registry", name)
			continue
		}
		if got.Name() != name {
			t.Errorf("expected tool name '%s', got '%s'", name, got.Name())
		}
	}
}

func TestAllToolsImplementSchemaProvider(t *testing.T) {
	actor := makeTestActor()
	pending := &atomic.Bool{}
	registry := BuildRegistry(actor, pending, "test-agent", &atomic.Bool{})

	for _, toolObj := range registry.All() {
		sp, ok := toolObj.(tool.SchemaProvider)
		if !ok {
			t.Errorf("tool '%s' does not implement SchemaProvider", toolObj.Name())
			continue
		}
		schema := sp.InputSchema()
		if schema == nil {
			t.Errorf("tool '%s' returned nil schema", toolObj.Name())
		}
		if schema["type"] != "object" {
			t.Errorf("tool '%s' schema type should be 'object', got '%v'", toolObj.Name(), schema["type"])
		}
	}
}

func TestAllToolsReturnFalseForRequiresApproval(t *testing.T) {
	actor := makeTestActor()
	pending := &atomic.Bool{}
	registry := BuildRegistry(actor, pending, "test-agent", &atomic.Bool{})

	for _, toolObj := range registry.All() {
		if toolObj.RequiresApproval(nil) {
			t.Errorf("tool '%s' should not require approval", toolObj.Name())
		}
	}
}
