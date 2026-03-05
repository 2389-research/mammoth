// ABOUTME: Core types for the MCP server: run status, active run state, pending questions, and config.
// ABOUTME: These types bridge MCP tool handlers to the attractor engine.
package mcp

import (
	"context"
	"sync"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

// RunStatus represents the lifecycle state of a pipeline run.
type RunStatus string

const (
	StatusRunning   RunStatus = "running"
	StatusPaused    RunStatus = "paused"
	StatusCompleted RunStatus = "completed"
	StatusFailed    RunStatus = "failed"
)

// PendingQuestion represents a human gate question awaiting an answer.
type PendingQuestion struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
	NodeID  string   `json:"node_id"`
}

// RunConfig holds the configuration for a pipeline run, serializable for disk persistence.
type RunConfig struct {
	RetryPolicy string `json:"retry_policy,omitempty"`
	Backend     string `json:"backend,omitempty"`
	BaseURL     string `json:"base_url,omitempty"`
}

// ActiveRun tracks a single pipeline execution in memory.
type ActiveRun struct {
	ID              string
	Status          RunStatus
	Source          string
	Config          RunConfig
	CurrentNode     string
	CurrentActivity string
	CompletedNodes  []string
	PendingQuestion *PendingQuestion
	EventBuffer     []attractor.EngineEvent
	Result          *attractor.RunResult
	Error           string
	CreatedAt       time.Time
	ArtifactDir     string
	CheckpointDir   string

	// cancel cancels the pipeline's context.
	cancel context.CancelFunc

	// answerCh delivers human gate answers from answer_question tool calls.
	answerCh chan string

	mu sync.RWMutex
}

const maxEventBuffer = 500
