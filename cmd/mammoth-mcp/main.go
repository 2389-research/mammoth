// ABOUTME: Entrypoint for the mammoth MCP server binary.
// ABOUTME: Serves attractor pipeline tools over stdio using the MCP protocol.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	mammothmcp "github.com/2389-research/mammoth/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "mammoth-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	// Determine data directory.
	dataDir := os.Getenv("MAMMOTH_DATA_DIR")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("determine home directory: %w", err)
		}
		dataDir = filepath.Join(home, ".mammoth", "mcp-runs")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	// Create mammoth server.
	ms := mammothmcp.NewServer(dataDir)

	// Create MCP protocol server.
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "mammoth-mcp",
		Version: "v0.1.0",
	}, nil)

	// Register all tools.
	ms.RegisterTools(srv)

	// Serve over stdio.
	return srv.Run(ctx, &mcpsdk.StdioTransport{})
}
