// ABOUTME: Tool call hooks that run shell commands before/after LLM tool calls per spec section 9.7.
// ABOUTME: Pre-hooks can skip tool calls (non-zero exit); post-hooks are for logging/auditing.
package attractor

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// hookTimeout is the maximum time a hook command is allowed to run.
const hookTimeout = 30 * time.Second

// ToolCallMeta contains metadata about a tool call, passed to hooks via env vars.
type ToolCallMeta struct {
	ToolName string
	NodeID   string
	Input    string // optional JSON input
}

// ToolCallResult contains the result of a tool call execution.
type ToolCallResult struct {
	Output   string
	ExitCode int
}

// PreHookResult indicates whether a pre-hook wants to skip the tool call.
type PreHookResult struct {
	Skip   bool
	Reason string
}

// ToolCallHooks holds the pre and post hook shell commands for tool calls.
type ToolCallHooks struct {
	PreCommand  string
	PostCommand string
}

// RunPre executes the pre-hook command. If the command exits non-zero, the tool
// call should be skipped. An empty PreCommand is a no-op that returns Skip=false.
func (h *ToolCallHooks) RunPre(ctx context.Context, meta ToolCallMeta) PreHookResult {
	if h.PreCommand == "" {
		return PreHookResult{Skip: false}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", h.PreCommand)
	cmd.Env = buildHookEnv(meta, nil)

	err := cmd.Run()
	if err != nil {
		exitCode := extractExitCode(err)
		return PreHookResult{
			Skip:   true,
			Reason: fmt.Sprintf("pre-hook exited with code %d", exitCode),
		}
	}

	return PreHookResult{Skip: false}
}

// RunPost executes the post-hook command for logging/auditing. Failures are
// swallowed and do not affect the pipeline. An empty PostCommand is a no-op.
func (h *ToolCallHooks) RunPost(ctx context.Context, meta ToolCallMeta, result ToolCallResult) {
	if h.PostCommand == "" {
		return
	}

	cmdCtx, cancel := context.WithTimeout(ctx, hookTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", h.PostCommand)
	cmd.Env = buildHookEnv(meta, &result)

	// Fire-and-forget: errors are swallowed
	_ = cmd.Run()
}

// buildHookEnv constructs an isolated environment with only ATTRACTOR_* vars.
// This prevents accidental exposure of secrets from the parent process env.
// If result is non-nil, ATTRACTOR_TOOL_OUTPUT and ATTRACTOR_TOOL_EXIT_CODE are included.
func buildHookEnv(meta ToolCallMeta, result *ToolCallResult) []string {
	env := []string{
		"ATTRACTOR_TOOL_NAME=" + meta.ToolName,
		"ATTRACTOR_NODE_ID=" + meta.NodeID,
		"ATTRACTOR_TOOL_INPUT=" + meta.Input,
	}
	if result != nil {
		env = append(env,
			"ATTRACTOR_TOOL_OUTPUT="+result.Output,
			fmt.Sprintf("ATTRACTOR_TOOL_EXIT_CODE=%d", result.ExitCode),
		)
	}
	return env
}

// ResolveToolCallHooks resolves hook commands from node and graph attributes.
// Node-level attributes override graph-level attributes for each hook independently.
func ResolveToolCallHooks(node *Node, graph *Graph) *ToolCallHooks {
	hooks := &ToolCallHooks{}

	// Start with graph-level defaults
	if graph != nil && graph.Attrs != nil {
		hooks.PreCommand = graph.Attrs["tool_hooks.pre"]
		hooks.PostCommand = graph.Attrs["tool_hooks.post"]
	}

	// Node-level overrides
	if node != nil && node.Attrs != nil {
		if pre, ok := node.Attrs["tool_hooks.pre"]; ok {
			hooks.PreCommand = pre
		}
		if post, ok := node.Attrs["tool_hooks.post"]; ok {
			hooks.PostCommand = post
		}
	}

	return hooks
}
