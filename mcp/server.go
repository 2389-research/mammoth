// ABOUTME: Server type for the mammoth MCP server, bridging MCP tools to the attractor engine.
// ABOUTME: Holds references to the run registry, disk index, and data directory for pipeline execution.
package mcp

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
