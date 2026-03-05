// ABOUTME: Package mcp exposes the attractor pipeline runner as an MCP server.
// ABOUTME: It provides tool handlers, a run registry, and a disk-backed run index.
package mcp

import (
	// Anchor the MCP SDK dependency so go mod tidy retains it.
	// Real usage arrives in subsequent tasks.
	_ "github.com/modelcontextprotocol/go-sdk/mcp"
)
