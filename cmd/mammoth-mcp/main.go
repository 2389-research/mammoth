// ABOUTME: Entrypoint for the mammoth MCP server binary.
// ABOUTME: Serves attractor pipeline tools over stdio using the MCP protocol.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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
	// TODO: wire up MCP server
	return fmt.Errorf("not implemented")
}
