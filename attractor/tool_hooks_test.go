// ABOUTME: Tests for tool_hooks.pre and tool_hooks.post attributes per spec section 9.7.
// ABOUTME: Covers pre-hook skip, post-hook execution, empty hooks, env vars, and resolution.
package attractor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestToolCallHooksRunPre(t *testing.T) {
	tests := []struct {
		name       string
		preCommand string
		wantSkip   bool
		// If non-empty, check that this marker file was created
		markerFile string
	}{
		{
			name:       "empty command is no-op",
			preCommand: "",
			wantSkip:   false,
		},
		{
			name:       "exit 0 proceeds",
			preCommand: "exit 0",
			wantSkip:   false,
		},
		{
			name:       "exit 1 skips",
			preCommand: "exit 1",
			wantSkip:   true,
		},
		{
			name:       "touch marker file and proceed",
			preCommand: "marker_placeholder",
			wantSkip:   false,
			markerFile: "pre_marker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.preCommand
			if tt.markerFile != "" {
				marker := filepath.Join(t.TempDir(), tt.markerFile)
				cmd = "touch " + marker
				tt.markerFile = marker
			}

			hooks := ToolCallHooks{PreCommand: cmd}
			meta := ToolCallMeta{
				ToolName: "test_tool",
				NodeID:   "node1",
			}

			result := hooks.RunPre(context.Background(), meta)

			if result.Skip != tt.wantSkip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.wantSkip)
			}

			if tt.markerFile != "" {
				if _, err := os.Stat(tt.markerFile); err != nil {
					t.Errorf("marker file %q was not created: %v", tt.markerFile, err)
				}
			}
		})
	}
}

func TestToolCallHooksRunPost(t *testing.T) {
	tests := []struct {
		name        string
		postCommand string
		markerFile  string
	}{
		{
			name:        "empty command is no-op",
			postCommand: "",
		},
		{
			name:        "touch marker file",
			postCommand: "marker_placeholder",
			markerFile:  "post_marker",
		},
		{
			name:        "exit 1 does not panic",
			postCommand: "exit 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.postCommand
			var markerPath string
			if tt.markerFile != "" {
				markerPath = filepath.Join(t.TempDir(), tt.markerFile)
				cmd = "touch " + markerPath
			}

			hooks := ToolCallHooks{PostCommand: cmd}
			meta := ToolCallMeta{
				ToolName: "test_tool",
				NodeID:   "node1",
			}
			result := ToolCallResult{
				Output:   "some output",
				ExitCode: 0,
			}

			// Should not panic
			hooks.RunPost(context.Background(), meta, result)

			if markerPath != "" {
				if _, err := os.Stat(markerPath); err != nil {
					t.Errorf("marker file %q was not created: %v", markerPath, err)
				}
			}
		})
	}
}

func TestPreHookReceivesEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "env_output")

	hooks := ToolCallHooks{
		PreCommand: "echo $ATTRACTOR_TOOL_NAME > " + envFile,
	}
	meta := ToolCallMeta{
		ToolName: "my_special_tool",
		NodeID:   "node42",
		Input:    `{"key":"value"}`,
	}

	result := hooks.RunPre(context.Background(), meta)
	if result.Skip {
		t.Fatal("expected Skip=false for echo command")
	}

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	got := string(content)
	// echo adds a trailing newline
	want := "my_special_tool\n"
	if got != want {
		t.Errorf("env var content = %q, want %q", got, want)
	}
}

func TestPostHookReceivesEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, "post_env_output")

	hooks := ToolCallHooks{
		PostCommand: "echo $ATTRACTOR_TOOL_OUTPUT > " + envFile,
	}
	meta := ToolCallMeta{
		ToolName: "some_tool",
		NodeID:   "node99",
	}
	result := ToolCallResult{
		Output:   "hello_world",
		ExitCode: 42,
	}

	hooks.RunPost(context.Background(), meta, result)

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	got := string(content)
	want := "hello_world\n"
	if got != want {
		t.Errorf("env var content = %q, want %q", got, want)
	}
}

func TestPreHookReceivesStdinJSON(t *testing.T) {
	tmpDir := t.TempDir()
	stdinFile := filepath.Join(tmpDir, "stdin_data")

	// The hook command reads all of stdin and writes it to a file.
	hooks := ToolCallHooks{
		PreCommand: "cat > " + stdinFile,
	}
	meta := ToolCallMeta{
		ToolName: "grep_tool",
		NodeID:   "nodeA",
		Input:    `{"pattern":"foo"}`,
	}

	result := hooks.RunPre(context.Background(), meta)
	if result.Skip {
		t.Fatal("expected Skip=false")
	}

	content, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("failed to read stdin file: %v", err)
	}

	var got ToolCallMeta
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("failed to unmarshal stdin JSON: %v (raw: %s)", err, content)
	}
	if got.ToolName != meta.ToolName || got.NodeID != meta.NodeID || got.Input != meta.Input {
		t.Errorf("stdin JSON mismatch: got %+v, want %+v", got, meta)
	}
}

func TestPostHookReceivesStdinJSON(t *testing.T) {
	tmpDir := t.TempDir()
	stdinFile := filepath.Join(tmpDir, "post_stdin_data")

	hooks := ToolCallHooks{
		PostCommand: "cat > " + stdinFile,
	}
	meta := ToolCallMeta{
		ToolName: "write_tool",
		NodeID:   "nodeB",
		Input:    `{"path":"out.txt"}`,
	}
	result := ToolCallResult{
		Output:   "wrote 42 bytes",
		ExitCode: 0,
	}

	errMsg := hooks.RunPost(context.Background(), meta, result)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}

	content, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("failed to read stdin file: %v", err)
	}

	var got postHookInput
	if err := json.Unmarshal(content, &got); err != nil {
		t.Fatalf("failed to unmarshal stdin JSON: %v (raw: %s)", err, content)
	}
	if got.Meta.ToolName != meta.ToolName || got.Meta.NodeID != meta.NodeID {
		t.Errorf("meta mismatch: got %+v, want %+v", got.Meta, meta)
	}
	if got.Result.Output != result.Output || got.Result.ExitCode != result.ExitCode {
		t.Errorf("result mismatch: got %+v, want %+v", got.Result, result)
	}
}

func TestPreHookErrorRecording(t *testing.T) {
	hooks := ToolCallHooks{
		PreCommand: "echo 'something went wrong' >&2; exit 1",
	}
	meta := ToolCallMeta{
		ToolName: "fail_tool",
		NodeID:   "nodeErr",
	}

	result := hooks.RunPre(context.Background(), meta)
	if !result.Skip {
		t.Fatal("expected Skip=true on failure")
	}
	if result.Error == "" {
		t.Fatal("expected non-empty Error on failure")
	}
	if result.Reason == "" {
		t.Fatal("expected non-empty Reason on failure")
	}
}

func TestPostHookReturnsErrorOnFailure(t *testing.T) {
	hooks := ToolCallHooks{
		PostCommand: "echo 'post failed' >&2; exit 1",
	}
	meta := ToolCallMeta{
		ToolName: "some_tool",
		NodeID:   "nodePost",
	}
	result := ToolCallResult{Output: "ok", ExitCode: 0}

	errMsg := hooks.RunPost(context.Background(), meta, result)
	if errMsg == "" {
		t.Fatal("expected non-empty error message on post-hook failure")
	}
}

func TestResolveToolCallHooks(t *testing.T) {
	tests := []struct {
		name       string
		nodeAttrs  map[string]string
		graphAttrs map[string]string
		wantPre    string
		wantPost   string
		nilNode    bool
		nilGraph   bool
		nilAttrs   bool
	}{
		{
			name:       "graph-level only",
			nodeAttrs:  map[string]string{},
			graphAttrs: map[string]string{"tool_hooks.pre": "echo graph_pre", "tool_hooks.post": "echo graph_post"},
			wantPre:    "echo graph_pre",
			wantPost:   "echo graph_post",
		},
		{
			name:       "node-level override",
			nodeAttrs:  map[string]string{"tool_hooks.pre": "echo node_pre", "tool_hooks.post": "echo node_post"},
			graphAttrs: map[string]string{"tool_hooks.pre": "echo graph_pre", "tool_hooks.post": "echo graph_post"},
			wantPre:    "echo node_pre",
			wantPost:   "echo node_post",
		},
		{
			name:       "node overrides pre, graph provides post",
			nodeAttrs:  map[string]string{"tool_hooks.pre": "echo node_pre"},
			graphAttrs: map[string]string{"tool_hooks.pre": "echo graph_pre", "tool_hooks.post": "echo graph_post"},
			wantPre:    "echo node_pre",
			wantPost:   "echo graph_post",
		},
		{
			name:       "neither set",
			nodeAttrs:  map[string]string{},
			graphAttrs: map[string]string{},
			wantPre:    "",
			wantPost:   "",
		},
		{
			name:       "nil node attrs",
			nilAttrs:   true,
			graphAttrs: map[string]string{"tool_hooks.pre": "echo graph_pre"},
			wantPre:    "echo graph_pre",
			wantPost:   "",
		},
		{
			name:      "nil graph attrs",
			nodeAttrs: map[string]string{"tool_hooks.post": "echo node_post"},
			nilGraph:  false,
			wantPre:   "",
			wantPost:  "echo node_post",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &Node{ID: "test_node", Attrs: tt.nodeAttrs}
			if tt.nilAttrs {
				node.Attrs = nil
			}

			graph := &Graph{Attrs: tt.graphAttrs}
			if tt.graphAttrs == nil {
				graph.Attrs = nil
			}

			hooks := ResolveToolCallHooks(node, graph)

			if hooks.PreCommand != tt.wantPre {
				t.Errorf("PreCommand = %q, want %q", hooks.PreCommand, tt.wantPre)
			}
			if hooks.PostCommand != tt.wantPost {
				t.Errorf("PostCommand = %q, want %q", hooks.PostCommand, tt.wantPost)
			}
		})
	}
}
