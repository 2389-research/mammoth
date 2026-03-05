// ABOUTME: Server type for the mammoth MCP server, bridging MCP tools to the attractor engine.
// ABOUTME: Holds references to the run registry, disk index, and data directory for pipeline execution.
package mcp

import mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

// Server is the mammoth MCP server. It owns the run registry, disk index,
// and data directory, and registers tool handlers on an MCP SDK server.
type Server struct {
	registry *RunRegistry
	index    *RunIndex
	dataDir  string
}

// NewServer creates a new mammoth MCP Server backed by the given data directory.
// The directory is used to store run metadata, checkpoints, and artifacts.
func NewServer(dataDir string) *Server {
	return &Server{
		registry: NewRunRegistry(),
		index:    NewRunIndex(dataDir),
		dataDir:  dataDir,
	}
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
