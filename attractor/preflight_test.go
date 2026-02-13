// ABOUTME: Tests for pre-execution validation that checks provider accessibility and pipeline requirements.
// ABOUTME: Covers RunPreflight, PreflightResult, BuildPreflightChecks, and HasCodergenNodes.
package attractor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestRunPreflightAllPass(t *testing.T) {
	checks := []PreflightCheck{
		{Name: "check-a", Check: func(ctx context.Context) error { return nil }},
		{Name: "check-b", Check: func(ctx context.Context) error { return nil }},
		{Name: "check-c", Check: func(ctx context.Context) error { return nil }},
	}

	result := RunPreflight(context.Background(), checks)

	if !result.OK() {
		t.Fatalf("expected all checks to pass, got failures: %v", result.Failed)
	}
	if len(result.Passed) != 3 {
		t.Errorf("expected 3 passed checks, got %d", len(result.Passed))
	}
	if len(result.Failed) != 0 {
		t.Errorf("expected 0 failures, got %d", len(result.Failed))
	}
}

func TestRunPreflightSomeFail(t *testing.T) {
	checks := []PreflightCheck{
		{Name: "passes", Check: func(ctx context.Context) error { return nil }},
		{Name: "fails-1", Check: func(ctx context.Context) error { return fmt.Errorf("boom") }},
		{Name: "fails-2", Check: func(ctx context.Context) error { return fmt.Errorf("kaboom") }},
	}

	result := RunPreflight(context.Background(), checks)

	if result.OK() {
		t.Fatal("expected failures but result.OK() returned true")
	}
	if len(result.Passed) != 1 {
		t.Errorf("expected 1 passed check, got %d", len(result.Passed))
	}
	if len(result.Failed) != 2 {
		t.Errorf("expected 2 failures, got %d", len(result.Failed))
	}

	// Verify failure details
	foundBoom := false
	foundKaboom := false
	for _, f := range result.Failed {
		if f.Name == "fails-1" && f.Reason == "boom" {
			foundBoom = true
		}
		if f.Name == "fails-2" && f.Reason == "kaboom" {
			foundKaboom = true
		}
	}
	if !foundBoom {
		t.Error("expected failure 'fails-1' with reason 'boom'")
	}
	if !foundKaboom {
		t.Error("expected failure 'fails-2' with reason 'kaboom'")
	}
}

func TestPreflightResultOK(t *testing.T) {
	tests := []struct {
		name   string
		result PreflightResult
		wantOK bool
	}{
		{
			name:   "no failures is OK",
			result: PreflightResult{Passed: []string{"a", "b"}, Failed: nil},
			wantOK: true,
		},
		{
			name:   "empty failures is OK",
			result: PreflightResult{Passed: []string{"a"}, Failed: []PreflightFailure{}},
			wantOK: true,
		},
		{
			name: "with failures is not OK",
			result: PreflightResult{
				Passed: []string{"a"},
				Failed: []PreflightFailure{{Name: "b", Reason: "broken"}},
			},
			wantOK: false,
		},
		{
			name:   "zero state is OK",
			result: PreflightResult{},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.OK(); got != tt.wantOK {
				t.Errorf("OK() = %v, want %v", got, tt.wantOK)
			}
		})
	}
}

func TestPreflightResultError(t *testing.T) {
	result := PreflightResult{
		Passed: []string{"a"},
		Failed: []PreflightFailure{
			{Name: "check-x", Reason: "missing config"},
			{Name: "check-y", Reason: "not reachable"},
		},
	}

	errStr := result.Error()

	if !strings.Contains(errStr, "check-x") {
		t.Errorf("error string should contain 'check-x', got: %s", errStr)
	}
	if !strings.Contains(errStr, "missing config") {
		t.Errorf("error string should contain 'missing config', got: %s", errStr)
	}
	if !strings.Contains(errStr, "check-y") {
		t.Errorf("error string should contain 'check-y', got: %s", errStr)
	}
	if !strings.Contains(errStr, "not reachable") {
		t.Errorf("error string should contain 'not reachable', got: %s", errStr)
	}
}

func TestPreflightResultErrorNoFailures(t *testing.T) {
	result := PreflightResult{Passed: []string{"a"}}
	errStr := result.Error()
	if errStr != "" {
		t.Errorf("expected empty error string for no failures, got: %s", errStr)
	}
}

func TestBuildPreflightChecksNoCodergen(t *testing.T) {
	// Graph with only start and exit nodes — no codergen nodes
	graph := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{{From: "start", To: "end"}},
	}
	cfg := EngineConfig{Backend: nil}

	checks := BuildPreflightChecks(graph, cfg)

	// No codergen nodes, so no backend check should be generated
	for _, c := range checks {
		if c.Name == "codergen-backend" {
			t.Error("should not have codergen-backend check when no codergen nodes exist")
		}
	}
}

func TestBuildPreflightChecksCodergenNoBackend(t *testing.T) {
	// Graph with a codergen node (box shape) but no backend configured
	graph := &Graph{
		Nodes: map[string]*Node{
			"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"code":   {ID: "code", Attrs: map[string]string{"shape": "box", "label": "Write code"}},
			"finish": {ID: "finish", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "code"},
			{From: "code", To: "finish"},
		},
	}
	cfg := EngineConfig{Backend: nil}

	checks := BuildPreflightChecks(graph, cfg)

	// Should have a codergen-backend check that fails
	found := false
	for _, c := range checks {
		if c.Name == "codergen-backend" {
			found = true
			err := c.Check(context.Background())
			if err == nil {
				t.Error("codergen-backend check should fail when backend is nil")
			}
			if !strings.Contains(err.Error(), "no backend configured") {
				t.Errorf("error should mention 'no backend configured', got: %s", err.Error())
			}
		}
	}
	if !found {
		t.Error("expected codergen-backend check to be present")
	}
}

func TestBuildPreflightChecksCodergenWithBackend(t *testing.T) {
	// Graph with codergen node and a backend configured
	graph := &Graph{
		Nodes: map[string]*Node{
			"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"code":   {ID: "code", Attrs: map[string]string{"shape": "box", "label": "Write code"}},
			"finish": {ID: "finish", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "code"},
			{From: "code", To: "finish"},
		},
	}
	cfg := EngineConfig{Backend: &stubBackend{}}

	checks := BuildPreflightChecks(graph, cfg)

	// Should have a backend-configured check that passes
	found := false
	for _, c := range checks {
		if c.Name == "backend-configured" {
			found = true
			err := c.Check(context.Background())
			if err != nil {
				t.Errorf("backend-configured check should pass, got error: %v", err)
			}
		}
	}
	if !found {
		t.Error("expected backend-configured check to be present")
	}

	// Should NOT have the failing codergen-backend check
	for _, c := range checks {
		if c.Name == "codergen-backend" {
			t.Error("should not have codergen-backend failure check when backend is configured")
		}
	}
}

func TestBuildPreflightChecksEnvRequired(t *testing.T) {
	// Node with env_required attribute
	graph := &Graph{
		Nodes: map[string]*Node{
			"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
			"code":  {ID: "code", Attrs: map[string]string{"shape": "box", "env_required": "TEST_PREFLIGHT_API_KEY"}},
			"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
		},
		Edges: []*Edge{
			{From: "start", To: "code"},
			{From: "code", To: "end"},
		},
	}
	cfg := EngineConfig{Backend: &stubBackend{}}

	// Ensure the env var is NOT set
	os.Unsetenv("TEST_PREFLIGHT_API_KEY")

	checks := BuildPreflightChecks(graph, cfg)

	// Find the env check
	found := false
	for _, c := range checks {
		if strings.Contains(c.Name, "TEST_PREFLIGHT_API_KEY") {
			found = true
			err := c.Check(context.Background())
			if err == nil {
				t.Error("env check should fail when env var is not set")
			}
		}
	}
	if !found {
		t.Error("expected env check for TEST_PREFLIGHT_API_KEY")
	}

	// Now set the env var and rebuild checks
	os.Setenv("TEST_PREFLIGHT_API_KEY", "some-value")
	defer os.Unsetenv("TEST_PREFLIGHT_API_KEY")

	checks = BuildPreflightChecks(graph, cfg)
	for _, c := range checks {
		if strings.Contains(c.Name, "TEST_PREFLIGHT_API_KEY") {
			err := c.Check(context.Background())
			if err != nil {
				t.Errorf("env check should pass when env var is set, got: %v", err)
			}
		}
	}
}

func TestHasCodergenNodes(t *testing.T) {
	tests := []struct {
		name  string
		graph *Graph
		want  bool
	}{
		{
			name: "box shape is codergen",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"code":  {ID: "code", Attrs: map[string]string{"shape": "box"}},
					"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: true,
		},
		{
			name: "no shape defaults to codergen",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start":  {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"noattr": {ID: "noattr", Attrs: map[string]string{}},
					"end":    {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: true,
		},
		{
			name: "only start and exit",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: false,
		},
		{
			name: "diamond is conditional not codergen",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"check": {ID: "check", Attrs: map[string]string{"shape": "diamond"}},
					"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: false,
		},
		{
			name: "parallelogram is tool not codergen",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"tool":  {ID: "tool", Attrs: map[string]string{"shape": "parallelogram"}},
					"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: false,
		},
		{
			name: "hexagon is human not codergen",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"human": {ID: "human", Attrs: map[string]string{"shape": "hexagon"}},
					"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: false,
		},
		{
			name: "explicit type overrides shape",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					// Box shape but explicit type=tool should NOT be codergen
					"special": {ID: "special", Attrs: map[string]string{"shape": "box", "type": "tool"}},
					"end":     {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: false,
		},
		{
			name: "unknown shape defaults to codergen",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start":   {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"mystery": {ID: "mystery", Attrs: map[string]string{"shape": "egg"}},
					"end":     {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: true,
		},
		{
			name: "component is parallel not codergen",
			graph: &Graph{
				Nodes: map[string]*Node{
					"start": {ID: "start", Attrs: map[string]string{"shape": "Mdiamond"}},
					"par":   {ID: "par", Attrs: map[string]string{"shape": "component"}},
					"end":   {ID: "end", Attrs: map[string]string{"shape": "Msquare"}},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasCodergenNodes(tt.graph)
			if got != tt.want {
				t.Errorf("HasCodergenNodes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasCodergenNodesEmpty(t *testing.T) {
	// Empty graph
	graph := &Graph{Nodes: map[string]*Node{}}
	if HasCodergenNodes(graph) {
		t.Error("empty graph should not have codergen nodes")
	}

	// Nil nodes map
	graph2 := &Graph{}
	if HasCodergenNodes(graph2) {
		t.Error("nil nodes map should not have codergen nodes")
	}
}

func TestRunPreflightContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	checks := []PreflightCheck{
		{
			Name: "ctx-check",
			Check: func(ctx context.Context) error {
				// Should see cancelled context
				return ctx.Err()
			},
		},
	}

	result := RunPreflight(ctx, checks)

	if result.OK() {
		t.Error("expected failure when context is cancelled")
	}
	if len(result.Failed) != 1 {
		t.Fatalf("expected 1 failure, got %d", len(result.Failed))
	}
	if result.Failed[0].Name != "ctx-check" {
		t.Errorf("expected failure name 'ctx-check', got %s", result.Failed[0].Name)
	}
	if !strings.Contains(result.Failed[0].Reason, "cancel") {
		t.Errorf("expected cancellation reason, got: %s", result.Failed[0].Reason)
	}
}

// stubBackend is a test double that implements CodergenBackend for preflight tests.
type stubBackend struct{}

func (s *stubBackend) RunAgent(ctx context.Context, config AgentRunConfig) (*AgentRunResult, error) {
	return &AgentRunResult{Success: true, Output: "stub"}, nil
}
