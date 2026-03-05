// ABOUTME: Compilation smoke test for the mammoth-mcp binary.
// ABOUTME: Verifies that the binary compiles and core types are wired correctly.
package main

import (
	"testing"

	mammothmcp "github.com/2389-research/mammoth/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCompilationSmoke(t *testing.T) {
	// Verify that the mammoth MCP server can be created and tools registered.
	ms := mammothmcp.NewServer(t.TempDir())
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-mcp-test",
		Version: "v0.0.1-test",
	}, nil)
	ms.RegisterTools(srv)

	// If we get here without panicking, the compilation and wiring is correct.
}
