// ABOUTME: Entrypoint for the mammoth MCP server binary.
// ABOUTME: Serves tracker pipeline tools over stdio using the MCP protocol.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	mammothmcp "github.com/2389-research/mammoth/mcp"
	trackerllm "github.com/2389-research/tracker/llm"
	"github.com/2389-research/tracker/llm/anthropic"
	"github.com/2389-research/tracker/llm/google"
	"github.com/2389-research/tracker/llm/openai"
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

	// Build LLM client from environment variables for pipeline execution.
	var serverOpts []mammothmcp.ServerOption
	if llmClient, err := buildLLMClient(); err == nil && llmClient != nil {
		serverOpts = append(serverOpts, mammothmcp.WithLLMClient(llmClient))
	}

	// Create mammoth server.
	ms := mammothmcp.NewServer(dataDir, serverOpts...)

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

// buildLLMClient constructs a tracker LLM client from environment variables.
// Returns nil, nil when no API keys are set.
func buildLLMClient() (*trackerllm.Client, error) {
	constructors := map[string]func(string) (trackerllm.ProviderAdapter, error){
		"anthropic": func(key string) (trackerllm.ProviderAdapter, error) {
			var opts []anthropic.Option
			if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
				opts = append(opts, anthropic.WithBaseURL(base))
			}
			return anthropic.New(key, opts...), nil
		},
		"openai": func(key string) (trackerllm.ProviderAdapter, error) {
			var opts []openai.Option
			if base := os.Getenv("OPENAI_BASE_URL"); base != "" {
				opts = append(opts, openai.WithBaseURL(base))
			}
			return openai.New(key, opts...), nil
		},
		"gemini": func(key string) (trackerllm.ProviderAdapter, error) {
			var opts []google.Option
			if base := os.Getenv("GEMINI_BASE_URL"); base != "" {
				opts = append(opts, google.WithBaseURL(base))
			}
			return google.New(key, opts...), nil
		},
	}

	client, err := trackerllm.NewClientFromEnv(constructors)
	if err != nil {
		return nil, nil
	}

	if client != nil {
		client.AddMiddleware(trackerllm.NewRetryMiddleware(
			trackerllm.WithMaxRetries(3),
			trackerllm.WithBaseDelay(2*time.Second),
		))
	}

	return client, nil
}
