// ABOUTME: Server type for the mammoth MCP server, bridging MCP tools to the tracker pipeline engine.
// ABOUTME: Holds references to the run registry, disk index, and data directory for pipeline execution.
package mcp

import (
	"github.com/2389-research/tracker/agent"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is the mammoth MCP server. It owns the run registry, disk index,
// and data directory, and registers tool handlers on an MCP SDK server.
type Server struct {
	registry  *RunRegistry
	index     *RunIndex
	dataDir   string
	llmClient agent.Completer
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithLLMClient sets the LLM client for pipeline execution via MCP.
func WithLLMClient(client agent.Completer) ServerOption {
	return func(s *Server) {
		s.llmClient = client
	}
}

// NewServer creates a new mammoth MCP Server backed by the given data directory.
// The directory is used to store run metadata, checkpoints, and artifacts.
func NewServer(dataDir string, opts ...ServerOption) *Server {
	s := &Server{
		registry: NewRunRegistry(),
		index:    NewRunIndex(dataDir),
		dataDir:  dataDir,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// RegisterTools registers all mammoth pipeline tools on the given MCP SDK server.
// Call this after creating the MCP server and before starting to serve.
func (s *Server) RegisterTools(srv *mcpsdk.Server) {
	s.registerValidatePipeline(srv)
	s.registerRunPipeline(srv)
	s.registerGetRunStatus(srv)
	s.registerGetRunEvents(srv)
	s.registerGetRunLogs(srv)
	s.registerAnswerQuestion(srv)
	s.registerResumePipeline(srv)
}
