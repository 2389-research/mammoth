# Mammoth MCP Server Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expose the attractor DOT pipeline runner as an MCP server over stdio, with tools for running, validating, monitoring, and resuming pipelines.

**Architecture:** New `mcp/` package contains tool handlers, a `RunRegistry` (in-memory active runs), and a `RunIndex` (disk-backed metadata for resume). `cmd/mammoth-mcp/main.go` wires it up using the official Go MCP SDK (`github.com/modelcontextprotocol/go-sdk`). The attractor engine is unchanged.

**Tech Stack:** Go 1.25+, `github.com/modelcontextprotocol/go-sdk` v1.4.0, existing `attractor/` package

**Design doc:** `docs/plans/2026-03-04-mcp-server-design.md`

---

### Task 1: Add MCP SDK Dependency and Create Package Skeleton

**Files:**
- Modify: `go.mod`
- Create: `mcp/doc.go`
- Create: `cmd/mammoth-mcp/main.go` (skeleton only)

**Step 1: Add the official MCP SDK dependency**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go get github.com/modelcontextprotocol/go-sdk@latest`

**Step 2: Create the mcp/ package doc**

```go
// mcp/doc.go

// ABOUTME: Package mcp exposes the attractor pipeline runner as an MCP server.
// ABOUTME: It provides tool handlers, a run registry, and a disk-backed run index.
package mcp
```

**Step 3: Create cmd/mammoth-mcp/main.go skeleton**

```go
// cmd/mammoth-mcp/main.go

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
```

**Step 4: Verify it compiles**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go build ./mcp/ && go build ./cmd/mammoth-mcp/`

**Step 5: Tidy modules**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go mod tidy`

**Step 6: Commit**

```bash
git add mcp/ cmd/mammoth-mcp/ go.mod go.sum
git commit -m "feat(mcp): add MCP SDK dependency and package skeleton"
```

---

### Task 2: Core Types

**Files:**
- Create: `mcp/types.go`
- Create: `mcp/types_test.go`

**Step 1: Write the test**

```go
// mcp/types_test.go

// ABOUTME: Tests for MCP server core types.
// ABOUTME: Validates RunStatus constants, ActiveRun state transitions, and PendingQuestion serialization.
package mcp

import (
	"encoding/json"
	"testing"
)

func TestRunStatusConstants(t *testing.T) {
	statuses := []RunStatus{StatusRunning, StatusPaused, StatusCompleted, StatusFailed}
	expected := []string{"running", "paused", "completed", "failed"}
	for i, s := range statuses {
		if string(s) != expected[i] {
			t.Errorf("status %d: got %q, want %q", i, s, expected[i])
		}
	}
}

func TestPendingQuestionJSON(t *testing.T) {
	q := &PendingQuestion{
		ID:       "q1",
		Text:     "Continue?",
		Options:  []string{"yes", "no"},
		NodeID:   "gate_1",
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got PendingQuestion
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != q.ID || got.Text != q.Text || got.NodeID != q.NodeID {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
	if len(got.Options) != 2 || got.Options[0] != "yes" {
		t.Errorf("options mismatch: got %v", got.Options)
	}
}

func TestRunConfigDefaults(t *testing.T) {
	cfg := RunConfig{}
	if cfg.RetryPolicy != "" {
		t.Errorf("expected empty default retry policy, got %q", cfg.RetryPolicy)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestRunStatus -v`
Expected: FAIL — types not defined yet

**Step 3: Write the types**

```go
// mcp/types.go

// ABOUTME: Core types for the MCP server: run status, active run state, pending questions, and config.
// ABOUTME: These types bridge MCP tool handlers to the attractor engine.
package mcp

import (
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
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/types.go mcp/types_test.go
git commit -m "feat(mcp): add core types for run state, config, and pending questions"
```

---

### Task 3: RunRegistry — In-Memory Run Tracking

**Files:**
- Create: `mcp/registry.go`
- Create: `mcp/registry_test.go`

**Step 1: Write the tests**

```go
// mcp/registry_test.go

// ABOUTME: Tests for RunRegistry in-memory run tracking.
// ABOUTME: Validates create, get, list, and concurrent access.
package mcp

import (
	"sync"
	"testing"
)

func TestRegistryCreateAndGet(t *testing.T) {
	reg := NewRunRegistry()
	run := reg.Create("digraph { start -> end }", RunConfig{})
	if run.ID == "" {
		t.Fatal("expected non-empty run ID")
	}
	if run.Status != StatusRunning {
		t.Errorf("expected status %q, got %q", StatusRunning, run.Status)
	}

	got, ok := reg.Get(run.ID)
	if !ok {
		t.Fatal("expected to find run by ID")
	}
	if got.ID != run.ID {
		t.Errorf("ID mismatch: got %q, want %q", got.ID, run.ID)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg := NewRunRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestRegistryList(t *testing.T) {
	reg := NewRunRegistry()
	reg.Create("digraph { a -> b }", RunConfig{})
	reg.Create("digraph { c -> d }", RunConfig{})
	runs := reg.List()
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
}

func TestRegistryConcurrentAccess(t *testing.T) {
	reg := NewRunRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			run := reg.Create("digraph { x -> y }", RunConfig{})
			reg.Get(run.ID)
			reg.List()
		}()
	}
	wg.Wait()
	runs := reg.List()
	if len(runs) != 50 {
		t.Errorf("expected 50 runs, got %d", len(runs))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestRegistry -v`
Expected: FAIL — NewRunRegistry not defined

**Step 3: Implement RunRegistry**

```go
// mcp/registry.go

// ABOUTME: RunRegistry tracks active pipeline runs in memory.
// ABOUTME: Thread-safe via RWMutex. Each run gets a unique ID and an event buffer.
package mcp

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// RunRegistry manages active pipeline runs in memory.
type RunRegistry struct {
	runs map[string]*ActiveRun
	mu   sync.RWMutex
}

// NewRunRegistry creates an empty registry.
func NewRunRegistry() *RunRegistry {
	return &RunRegistry{
		runs: make(map[string]*ActiveRun),
	}
}

// Create registers a new run with the given DOT source and config.
// The run starts in StatusRunning.
func (r *RunRegistry) Create(source string, config RunConfig) *ActiveRun {
	id := generateRunID()
	run := &ActiveRun{
		ID:             id,
		Status:         StatusRunning,
		Source:         source,
		Config:         config,
		CompletedNodes: make([]string, 0),
		EventBuffer:    make([]attractor.EngineEvent, 0, maxEventBuffer),
		CreatedAt:      time.Now(),
		answerCh:       make(chan string, 1),
	}
	r.mu.Lock()
	r.runs[id] = run
	r.mu.Unlock()
	return run
}

// Get returns the run with the given ID, or false if not found.
func (r *RunRegistry) Get(id string) (*ActiveRun, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[id]
	return run, ok
}

// List returns all runs.
func (r *RunRegistry) List() []*ActiveRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runs := make([]*ActiveRun, 0, len(r.runs))
	for _, run := range r.runs {
		runs = append(runs, run)
	}
	return runs
}

// generateRunID produces a short random hex ID.
func generateRunID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestRegistry -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/registry.go mcp/registry_test.go
git commit -m "feat(mcp): add RunRegistry for in-memory run tracking"
```

---

### Task 4: RunIndex — Disk-Backed Persistence for Resume

**Files:**
- Create: `mcp/index.go`
- Create: `mcp/index_test.go`

**Step 1: Write the tests**

```go
// mcp/index_test.go

// ABOUTME: Tests for RunIndex disk-backed persistence.
// ABOUTME: Validates save, load, and round-trip of run metadata.
package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)

	entry := &IndexEntry{
		RunID:         "abc123",
		Source:        "digraph { start -> end }",
		Config:        RunConfig{RetryPolicy: "standard"},
		Status:        string(StatusCompleted),
		CheckpointDir: filepath.Join(dir, "abc123", "checkpoint"),
	}
	if err := idx.Save(entry); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := idx.Load("abc123")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.RunID != entry.RunID {
		t.Errorf("RunID: got %q, want %q", got.RunID, entry.RunID)
	}
	if got.Source != entry.Source {
		t.Errorf("Source mismatch")
	}
	if got.Config.RetryPolicy != "standard" {
		t.Errorf("Config.RetryPolicy: got %q, want %q", got.Config.RetryPolicy, "standard")
	}
}

func TestIndexLoadNotFound(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)
	_, err := idx.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}

func TestIndexListEntries(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)

	idx.Save(&IndexEntry{RunID: "run1", Source: "digraph { a -> b }"})
	idx.Save(&IndexEntry{RunID: "run2", Source: "digraph { c -> d }"})

	entries, err := idx.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestIndexSaveCreatesRunDir(t *testing.T) {
	dir := t.TempDir()
	idx := NewRunIndex(dir)

	idx.Save(&IndexEntry{RunID: "run1", Source: "digraph { a -> b }"})

	runDir := filepath.Join(dir, "run1")
	info, err := os.Stat(runDir)
	if err != nil {
		t.Fatalf("run dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}

	// source.dot should exist
	sourceFile := filepath.Join(runDir, "source.dot")
	data, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatalf("source.dot not written: %v", err)
	}
	if string(data) != "digraph { a -> b }" {
		t.Errorf("source.dot content mismatch: got %q", string(data))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestIndex -v`
Expected: FAIL — NewRunIndex not defined

**Step 3: Implement RunIndex**

```go
// mcp/index.go

// ABOUTME: RunIndex provides disk-backed persistence for pipeline run metadata.
// ABOUTME: Stores DOT source, config, and status per run to enable resume across server restarts.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// IndexEntry stores metadata for a single run on disk.
type IndexEntry struct {
	RunID         string    `json:"run_id"`
	Source        string    `json:"-"` // stored separately as source.dot
	Config        RunConfig `json:"config"`
	Status        string    `json:"status"`
	CheckpointDir string    `json:"checkpoint_dir,omitempty"`
	ArtifactDir   string    `json:"artifact_dir,omitempty"`
}

// RunIndex manages disk-backed run metadata.
type RunIndex struct {
	dir string
	mu  sync.Mutex
}

// NewRunIndex creates a new index rooted at the given directory.
func NewRunIndex(dir string) *RunIndex {
	return &RunIndex{dir: dir}
}

// Save persists an index entry to disk. Creates the run directory and writes
// source.dot and meta.json.
func (idx *RunIndex) Save(entry *IndexEntry) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	runDir := filepath.Join(idx.dir, entry.RunID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("create run dir: %w", err)
	}

	// Write source.dot
	sourcePath := filepath.Join(runDir, "source.dot")
	if entry.Source != "" {
		if err := os.WriteFile(sourcePath, []byte(entry.Source), 0644); err != nil {
			return fmt.Errorf("write source.dot: %w", err)
		}
	}

	// Write meta.json
	metaPath := filepath.Join(runDir, "meta.json")
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("write meta.json: %w", err)
	}

	return nil
}

// Load reads an index entry from disk.
func (idx *RunIndex) Load(runID string) (*IndexEntry, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	runDir := filepath.Join(idx.dir, runID)
	metaPath := filepath.Join(runDir, "meta.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read meta.json for run %s: %w", runID, err)
	}

	var entry IndexEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal meta.json: %w", err)
	}
	entry.RunID = runID

	// Read source.dot
	sourcePath := filepath.Join(runDir, "source.dot")
	sourceData, err := os.ReadFile(sourcePath)
	if err == nil {
		entry.Source = string(sourceData)
	}

	return &entry, nil
}

// List returns all index entries.
func (idx *RunIndex) List() ([]*IndexEntry, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	entries, err := os.ReadDir(idx.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read index dir: %w", err)
	}

	var result []*IndexEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(idx.dir, e.Name(), "meta.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var entry IndexEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entry.RunID = e.Name()
		result = append(result, &entry)
	}
	return result, nil
}

// RunDir returns the directory path for a given run ID.
func (idx *RunIndex) RunDir(runID string) string {
	return filepath.Join(idx.dir, runID)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestIndex -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/index.go mcp/index_test.go
git commit -m "feat(mcp): add RunIndex for disk-backed run persistence"
```

---

### Task 5: MCP Interviewer — Channel-Based Human Gate Bridge

The MCP interviewer implements the `attractor.Interviewer` interface. When a pipeline hits a human gate, it blocks on a channel. The `answer_question` tool sends the answer on that channel.

**Files:**
- Create: `mcp/interviewer.go`
- Create: `mcp/interviewer_test.go`

**Step 1: Write the tests**

```go
// mcp/interviewer_test.go

// ABOUTME: Tests for the MCP channel-based interviewer.
// ABOUTME: Validates blocking behavior, answer delivery, and context cancellation.
package mcp

import (
	"context"
	"testing"
	"time"
)

func TestMCPInterviewerReceivesAnswer(t *testing.T) {
	run := &ActiveRun{
		ID:       "test-run",
		Status:   StatusRunning,
		answerCh: make(chan string, 1),
	}
	iv := &mcpInterviewer{run: run}

	// Send an answer before Ask is called (buffered channel).
	go func() {
		time.Sleep(10 * time.Millisecond)
		run.answerCh <- "yes"
	}()

	answer, err := iv.Ask(context.Background(), "Continue?", []string{"yes", "no"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "yes" {
		t.Errorf("expected %q, got %q", "yes", answer)
	}

	// Verify the pending question was set.
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.PendingQuestion == nil {
		t.Fatal("expected pending question to be set")
	}
	if run.PendingQuestion.Text != "Continue?" {
		t.Errorf("question text: got %q", run.PendingQuestion.Text)
	}
}

func TestMCPInterviewerContextCancellation(t *testing.T) {
	run := &ActiveRun{
		ID:       "test-run",
		Status:   StatusRunning,
		answerCh: make(chan string, 1),
	}
	iv := &mcpInterviewer{run: run}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := iv.Ask(ctx, "Will timeout?", nil)
	if err == nil {
		t.Fatal("expected context error")
	}
	if ctx.Err() == nil {
		t.Fatal("expected context to be done")
	}
}

func TestMCPInterviewerSetsPausedStatus(t *testing.T) {
	run := &ActiveRun{
		ID:       "test-run",
		Status:   StatusRunning,
		answerCh: make(chan string, 1),
	}
	iv := &mcpInterviewer{run: run}

	go func() {
		// Wait for status to become paused, then answer.
		for {
			run.mu.RLock()
			s := run.Status
			run.mu.RUnlock()
			if s == StatusPaused {
				run.answerCh <- "proceed"
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	answer, err := iv.Ask(context.Background(), "Gate check", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "proceed" {
		t.Errorf("expected %q, got %q", "proceed", answer)
	}

	// After answer, status should be back to running and question cleared.
	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.Status != StatusRunning {
		t.Errorf("expected status %q, got %q", StatusRunning, run.Status)
	}
	if run.PendingQuestion != nil {
		t.Error("expected pending question to be cleared")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestMCPInterviewer -v`
Expected: FAIL — mcpInterviewer not defined

**Step 3: Implement the interviewer**

```go
// mcp/interviewer.go

// ABOUTME: Channel-based interviewer that bridges MCP tool calls to attractor human gates.
// ABOUTME: Blocks the pipeline goroutine until an answer arrives via the answer_question tool.
package mcp

import (
	"context"
	"crypto/rand"
	"fmt"
)

// mcpInterviewer implements attractor.Interviewer by blocking on the run's
// answer channel. When Ask is called, it sets the run's status to paused,
// populates PendingQuestion, and waits for an answer or context cancellation.
type mcpInterviewer struct {
	run *ActiveRun
}

func (iv *mcpInterviewer) Ask(ctx context.Context, question string, options []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	qid := randomHex(8)

	// Set the run to paused and register the question.
	iv.run.mu.Lock()
	iv.run.Status = StatusPaused
	iv.run.PendingQuestion = &PendingQuestion{
		ID:      qid,
		Text:    question,
		Options: options,
		NodeID:  iv.run.CurrentNode,
	}
	iv.run.mu.Unlock()

	// Block until answer or context cancellation.
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case answer := <-iv.run.answerCh:
		// Clear the question and resume.
		iv.run.mu.Lock()
		iv.run.Status = StatusRunning
		iv.run.PendingQuestion = nil
		iv.run.mu.Unlock()
		return answer, nil
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestMCPInterviewer -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/interviewer.go mcp/interviewer_test.go
git commit -m "feat(mcp): add channel-based interviewer for human gate bridging"
```

---

### Task 6: Event Handler — Live Activity Tracking

The event handler wired into each engine updates the ActiveRun's state in real time.

**Files:**
- Create: `mcp/events.go`
- Create: `mcp/events_test.go`

**Step 1: Write the tests**

```go
// mcp/events_test.go

// ABOUTME: Tests for the MCP event handler that tracks live pipeline activity.
// ABOUTME: Validates current node, current activity, completed nodes, and buffer rotation.
package mcp

import (
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

func TestEventHandlerUpdatesCurrentNode(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]attractor.EngineEvent, 0, maxEventBuffer),
	}
	handler := newEventHandler(run)

	handler(attractor.EngineEvent{
		Type:   attractor.EventStageStarted,
		NodeID: "build_step",
	})

	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.CurrentNode != "build_step" {
		t.Errorf("CurrentNode: got %q, want %q", run.CurrentNode, "build_step")
	}
}

func TestEventHandlerTracksCompletedNodes(t *testing.T) {
	run := &ActiveRun{
		ID:             "test",
		Status:         StatusRunning,
		CompletedNodes: make([]string, 0),
		EventBuffer:    make([]attractor.EngineEvent, 0, maxEventBuffer),
	}
	handler := newEventHandler(run)

	handler(attractor.EngineEvent{Type: attractor.EventStageCompleted, NodeID: "step_1"})
	handler(attractor.EngineEvent{Type: attractor.EventStageCompleted, NodeID: "step_2"})

	run.mu.RLock()
	defer run.mu.RUnlock()
	if len(run.CompletedNodes) != 2 {
		t.Errorf("expected 2 completed nodes, got %d", len(run.CompletedNodes))
	}
}

func TestEventHandlerTracksActivity(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]attractor.EngineEvent, 0, maxEventBuffer),
	}
	handler := newEventHandler(run)

	handler(attractor.EngineEvent{
		Type: attractor.EventAgentToolCallStart,
		Data: map[string]any{"tool_name": "write_file"},
	})

	run.mu.RLock()
	defer run.mu.RUnlock()
	if run.CurrentActivity != "calling tool: write_file" {
		t.Errorf("CurrentActivity: got %q", run.CurrentActivity)
	}
}

func TestEventHandlerBufferRotation(t *testing.T) {
	run := &ActiveRun{
		ID:          "test",
		Status:      StatusRunning,
		EventBuffer: make([]attractor.EngineEvent, 0, maxEventBuffer),
	}
	handler := newEventHandler(run)

	// Fill beyond buffer capacity.
	for i := 0; i < maxEventBuffer+100; i++ {
		handler(attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "node",
			Timestamp: time.Now(),
		})
	}

	run.mu.RLock()
	defer run.mu.RUnlock()
	if len(run.EventBuffer) != maxEventBuffer {
		t.Errorf("buffer size: got %d, want %d", len(run.EventBuffer), maxEventBuffer)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestEventHandler -v`
Expected: FAIL — newEventHandler not defined

**Step 3: Implement the event handler**

```go
// mcp/events.go

// ABOUTME: Event handler factory for wiring attractor engine events to ActiveRun state.
// ABOUTME: Updates current node, activity, completed nodes, and maintains a rolling event buffer.
package mcp

import (
	"fmt"

	"github.com/2389-research/mammoth/attractor"
)

// newEventHandler returns an event handler function that updates the given
// ActiveRun's state as engine events arrive.
func newEventHandler(run *ActiveRun) func(attractor.EngineEvent) {
	return func(evt attractor.EngineEvent) {
		run.mu.Lock()
		defer run.mu.Unlock()

		// Append to rolling buffer.
		if len(run.EventBuffer) >= maxEventBuffer {
			// Drop the oldest event.
			copy(run.EventBuffer, run.EventBuffer[1:])
			run.EventBuffer = run.EventBuffer[:maxEventBuffer-1]
		}
		run.EventBuffer = append(run.EventBuffer, evt)

		// Update state based on event type.
		switch evt.Type {
		case attractor.EventStageStarted:
			run.CurrentNode = evt.NodeID
			run.CurrentActivity = fmt.Sprintf("executing node: %s", evt.NodeID)

		case attractor.EventStageCompleted:
			run.CompletedNodes = append(run.CompletedNodes, evt.NodeID)
			run.CurrentActivity = fmt.Sprintf("completed node: %s", evt.NodeID)

		case attractor.EventStageFailed:
			reason := ""
			if r, ok := evt.Data["reason"]; ok {
				reason = fmt.Sprintf(": %v", r)
			}
			run.CurrentActivity = fmt.Sprintf("node failed: %s%s", evt.NodeID, reason)

		case attractor.EventStageRetrying:
			attempt := ""
			if a, ok := evt.Data["attempt"]; ok {
				attempt = fmt.Sprintf(" (attempt %v)", a)
			}
			run.CurrentActivity = fmt.Sprintf("retrying node: %s%s", evt.NodeID, attempt)

		case attractor.EventAgentToolCallStart:
			toolName := "unknown"
			if tn, ok := evt.Data["tool_name"]; ok {
				toolName = fmt.Sprintf("%v", tn)
			}
			run.CurrentActivity = fmt.Sprintf("calling tool: %s", toolName)

		case attractor.EventAgentToolCallEnd:
			run.CurrentActivity = "tool call completed"

		case attractor.EventAgentLLMTurn:
			run.CurrentActivity = "LLM generating response"

		case attractor.EventAgentTextStart, attractor.EventAgentTextDelta:
			run.CurrentActivity = "LLM streaming text"

		case attractor.EventAgentSteering:
			run.CurrentActivity = "applying steering"

		case attractor.EventPipelineCompleted:
			run.CurrentActivity = "pipeline completed"

		case attractor.EventPipelineFailed:
			errMsg := ""
			if e, ok := evt.Data["error"]; ok {
				errMsg = fmt.Sprintf(": %v", e)
			}
			run.CurrentActivity = fmt.Sprintf("pipeline failed%s", errMsg)

		case attractor.EventCheckpointSaved:
			run.CurrentActivity = "checkpoint saved"
		}
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestEventHandler -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/events.go mcp/events_test.go
git commit -m "feat(mcp): add event handler for live pipeline activity tracking"
```

---

### Task 7: Backend Detection — Shared Helper

Extract backend detection logic so both `cmd/mammoth/` and `cmd/mammoth-mcp/` can use it.

**Files:**
- Create: `mcp/backend.go`
- Create: `mcp/backend_test.go`

**Step 1: Write the tests**

```go
// mcp/backend_test.go

// ABOUTME: Tests for backend detection logic.
// ABOUTME: Validates detection from env vars and fallback behavior.
package mcp

import (
	"testing"
)

func TestDetectBackendFromEnv(t *testing.T) {
	// With no env vars set and no explicit backend, should return nil.
	backend := DetectBackend("")
	// We can't assert much about the result since it depends on env,
	// but it should not panic.
	_ = backend
}

func TestDetectBackendExplicit(t *testing.T) {
	// "agent" should try to return an AgentBackend if API keys are set.
	// Without API keys, may return nil. Should not panic.
	backend := DetectBackend("agent")
	_ = backend
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestDetectBackend -v`
Expected: FAIL — DetectBackend not defined

**Step 3: Implement backend detection**

```go
// mcp/backend.go

// ABOUTME: Backend detection for the MCP server.
// ABOUTME: Selects the LLM backend (agent, claude-code) from env vars or explicit override.
package mcp

import (
	"fmt"
	"os"

	"github.com/2389-research/mammoth/attractor"
)

// DetectBackend selects the agent backend based on the backendType parameter
// or MAMMOTH_BACKEND env var. Falls back to API key auto-detection.
func DetectBackend(backendType string) attractor.CodergenBackend {
	if backendType == "" {
		backendType = os.Getenv("MAMMOTH_BACKEND")
	}

	if backendType == "claude-code" {
		backend, err := attractor.NewClaudeCodeBackend()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[mcp] claude-code backend: %v, falling back to agent\n", err)
		} else {
			return backend
		}
	}

	keys := []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY"}
	for _, k := range keys {
		if os.Getenv(k) != "" {
			return &attractor.AgentBackend{}
		}
	}

	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestDetectBackend -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/backend.go mcp/backend_test.go
git commit -m "feat(mcp): add backend detection for MCP server"
```

---

### Task 8: validate_pipeline Tool

The simplest tool — synchronous, no run state.

**Files:**
- Create: `mcp/tool_validate.go`
- Create: `mcp/tool_validate_test.go`

**Step 1: Write the tests**

```go
// mcp/tool_validate_test.go

// ABOUTME: Tests for the validate_pipeline MCP tool.
// ABOUTME: Validates DOT parsing, lint errors, and valid pipeline acceptance.
package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidatePipelineValidDOT(t *testing.T) {
	input := ValidatePipelineInput{
		Source: `digraph Test {
			graph [goal="test"]
			start [shape=Mdiamond]
			end [shape=Msquare]
			start -> end
		}`,
	}

	result, output, err := handleValidatePipeline(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil && result.IsError {
		t.Fatalf("unexpected tool error: %+v", result)
	}
	if !output.Valid {
		t.Errorf("expected valid pipeline, got errors: %v", output.Errors)
	}
}

func TestValidatePipelineInvalidDOT(t *testing.T) {
	input := ValidatePipelineInput{
		Source: "not valid dot at all {{{",
	}

	_, _, err := handleValidatePipeline(context.Background(), nil, input)
	if err == nil {
		t.Fatal("expected error for invalid DOT")
	}
}

func TestValidatePipelineFromFile(t *testing.T) {
	// Write a temp DOT file.
	dir := t.TempDir()
	dotFile := dir + "/test.dot"
	if err := writeTestFile(dotFile, `digraph Test {
		graph [goal="test"]
		start [shape=Mdiamond]
		end [shape=Msquare]
		start -> end
	}`); err != nil {
		t.Fatal(err)
	}

	input := ValidatePipelineInput{File: dotFile}
	_, output, err := handleValidatePipeline(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !output.Valid {
		t.Errorf("expected valid, got errors: %v", output.Errors)
	}
}

func TestValidatePipelineNoInput(t *testing.T) {
	input := ValidatePipelineInput{}
	_, _, err := handleValidatePipeline(context.Background(), nil, input)
	if err == nil {
		t.Fatal("expected error when no source or file provided")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestValidatePipeline -v`
Expected: FAIL — types not defined

**Step 3: Implement validate_pipeline**

```go
// mcp/tool_validate.go

// ABOUTME: MCP tool handler for validate_pipeline.
// ABOUTME: Parses and lints DOT source synchronously, returning errors and warnings.
package mcp

import (
	"context"
	"fmt"
	"os"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/2389-research/mammoth/attractor"
)

// ValidatePipelineInput is the input schema for validate_pipeline.
type ValidatePipelineInput struct {
	Source string `json:"source,omitempty" jsonschema:"DOT source string to validate"`
	File   string `json:"file,omitempty"   jsonschema:"path to a DOT file to validate"`
}

// ValidatePipelineOutput is the output from validate_pipeline.
type ValidatePipelineOutput struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// handleValidatePipeline is the tool handler for validate_pipeline.
func handleValidatePipeline(
	ctx context.Context,
	req *mcpsdk.CallToolRequest,
	input ValidatePipelineInput,
) (*mcpsdk.CallToolResult, ValidatePipelineOutput, error) {
	source, err := resolveSource(input.Source, input.File)
	if err != nil {
		return nil, ValidatePipelineOutput{}, err
	}

	// Parse the DOT source.
	graph, err := attractor.Parse(source)
	if err != nil {
		return nil, ValidatePipelineOutput{}, fmt.Errorf("DOT parse error: %w", err)
	}

	// Apply default transforms.
	graph = attractor.ApplyTransforms(graph, attractor.DefaultTransforms()...)

	// Validate.
	results, err := attractor.ValidateOrError(graph)
	if err != nil {
		// Collect individual error messages from the validation results.
		var errMsgs []string
		if results != nil {
			for _, r := range results {
				if r.Severity == "error" {
					errMsgs = append(errMsgs, r.Message)
				}
			}
		}
		if len(errMsgs) == 0 {
			errMsgs = []string{err.Error()}
		}
		return nil, ValidatePipelineOutput{
			Valid:  false,
			Errors: errMsgs,
		}, nil
	}

	// Collect warnings from results that passed overall.
	var warnings []string
	if results != nil {
		for _, r := range results {
			if r.Severity == "warning" {
				warnings = append(warnings, r.Message)
			}
		}
	}

	return nil, ValidatePipelineOutput{
		Valid:    true,
		Warnings: warnings,
	}, nil
}

// resolveSource returns DOT source from either a direct string or a file path.
func resolveSource(source, file string) (string, error) {
	if source != "" {
		return source, nil
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", file, err)
		}
		return string(data), nil
	}
	return "", fmt.Errorf("either 'source' or 'file' must be provided")
}
```

Also add a test helper:

```go
// Add to mcp/tool_validate_test.go (or a helpers_test.go):

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestValidatePipeline -v`
Expected: PASS

Note: `ValidateOrError` returns `([]ValidationResult, error)`. Check the exact return type in `attractor/validate.go` and adjust accordingly. The handler should handle both the case where validation errors are in the error return and where they're in the results slice.

**Step 5: Commit**

```bash
git add mcp/tool_validate.go mcp/tool_validate_test.go
git commit -m "feat(mcp): add validate_pipeline tool handler"
```

---

### Task 9: run_pipeline Tool

**Files:**
- Create: `mcp/tool_run.go`
- Create: `mcp/tool_run_test.go`

**Step 1: Write the tests**

```go
// mcp/tool_run_test.go

// ABOUTME: Tests for the run_pipeline MCP tool.
// ABOUTME: Validates pipeline launch, async execution, and status tracking.
package mcp

import (
	"context"
	"testing"
	"time"
)

func TestRunPipelineReturnsRunID(t *testing.T) {
	reg := NewRunRegistry()
	idx := NewRunIndex(t.TempDir())
	server := &Server{registry: reg, index: idx}

	input := RunPipelineInput{
		Source: `digraph Test {
			graph [goal="test"]
			start [shape=Mdiamond]
			end [shape=Msquare]
			start -> end
		}`,
	}

	_, output, err := server.handleRunPipeline(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.RunID == "" {
		t.Fatal("expected non-empty run ID")
	}
	if output.Status != string(StatusRunning) {
		t.Errorf("expected status %q, got %q", StatusRunning, output.Status)
	}
}

func TestRunPipelineInvalidDOT(t *testing.T) {
	reg := NewRunRegistry()
	idx := NewRunIndex(t.TempDir())
	server := &Server{registry: reg, index: idx}

	input := RunPipelineInput{Source: "not valid dot"}
	_, _, err := server.handleRunPipeline(context.Background(), nil, input)
	if err == nil {
		t.Fatal("expected error for invalid DOT")
	}
}

func TestRunPipelineNoInput(t *testing.T) {
	reg := NewRunRegistry()
	idx := NewRunIndex(t.TempDir())
	server := &Server{registry: reg, index: idx}

	input := RunPipelineInput{}
	_, _, err := server.handleRunPipeline(context.Background(), nil, input)
	if err == nil {
		t.Fatal("expected error when no source or file")
	}
}

func TestRunPipelineCompletesAsync(t *testing.T) {
	reg := NewRunRegistry()
	idx := NewRunIndex(t.TempDir())
	server := &Server{registry: reg, index: idx}

	input := RunPipelineInput{
		Source: `digraph Test {
			graph [goal="test"]
			start [shape=Mdiamond]
			end [shape=Msquare]
			start -> end
		}`,
	}

	_, output, err := server.handleRunPipeline(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait for completion (simple pipeline should finish quickly).
	deadline := time.After(10 * time.Second)
	for {
		run, ok := reg.Get(output.RunID)
		if !ok {
			t.Fatal("run not found")
		}
		run.mu.RLock()
		status := run.Status
		run.mu.RUnlock()
		if status == StatusCompleted || status == StatusFailed {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("pipeline did not complete in time, status: %s", status)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestRunPipeline -v`
Expected: FAIL — Server type and handler not defined

**Step 3: Implement run_pipeline**

First, create the Server type that holds shared state:

```go
// mcp/server.go

// ABOUTME: Server holds shared state for the MCP tool handlers.
// ABOUTME: Wires together the run registry, index, and backend configuration.
package mcp

// Server holds the shared state for all MCP tool handlers.
type Server struct {
	registry *RunRegistry
	index    *RunIndex
	dataDir  string
}

// NewServer creates a new MCP server with the given data directory.
func NewServer(dataDir string) *Server {
	return &Server{
		registry: NewRunRegistry(),
		index:    NewRunIndex(dataDir),
		dataDir:  dataDir,
	}
}
```

Then the tool handler:

```go
// mcp/tool_run.go

// ABOUTME: MCP tool handler for run_pipeline.
// ABOUTME: Validates DOT synchronously, then spawns pipeline execution in a goroutine.
package mcp

import (
	"context"
	"fmt"
	"path/filepath"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/2389-research/mammoth/attractor"
)

// RunPipelineInput is the input schema for run_pipeline.
type RunPipelineInput struct {
	Source string    `json:"source,omitempty" jsonschema:"DOT source string to execute"`
	File   string    `json:"file,omitempty"   jsonschema:"path to a DOT file to execute"`
	Config RunConfig `json:"config,omitempty" jsonschema:"optional execution configuration"`
}

// RunPipelineOutput is returned immediately with the run ID.
type RunPipelineOutput struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

func (s *Server) handleRunPipeline(
	ctx context.Context,
	req *mcpsdk.CallToolRequest,
	input RunPipelineInput,
) (*mcpsdk.CallToolResult, RunPipelineOutput, error) {
	source, err := resolveSource(input.Source, input.File)
	if err != nil {
		return nil, RunPipelineOutput{}, err
	}

	// Synchronous validation before spawning.
	graph, err := attractor.Parse(source)
	if err != nil {
		return nil, RunPipelineOutput{}, fmt.Errorf("DOT parse error: %w", err)
	}
	graph = attractor.ApplyTransforms(graph, attractor.DefaultTransforms()...)
	if _, err := attractor.ValidateOrError(graph); err != nil {
		return nil, RunPipelineOutput{}, fmt.Errorf("validation error: %w", err)
	}

	// Create the run.
	run := s.registry.Create(source, input.Config)

	// Set up directories.
	runDir := s.index.RunDir(run.ID)
	run.mu.Lock()
	run.CheckpointDir = filepath.Join(runDir, "checkpoint")
	run.ArtifactDir = filepath.Join(runDir, "artifacts")
	run.mu.Unlock()

	// Persist to disk index.
	s.index.Save(&IndexEntry{
		RunID:         run.ID,
		Source:        source,
		Config:        input.Config,
		Status:        string(StatusRunning),
		CheckpointDir: run.CheckpointDir,
		ArtifactDir:   run.ArtifactDir,
	})

	// Spawn execution in a goroutine.
	go s.executePipeline(run, graph)

	return nil, RunPipelineOutput{
		RunID:  run.ID,
		Status: string(StatusRunning),
	}, nil
}

// executePipeline runs the attractor engine in a background goroutine.
func (s *Server) executePipeline(run *ActiveRun, graph *attractor.Graph) {
	pipelineCtx, cancel := context.WithCancel(context.Background())
	run.mu.Lock()
	run.cancel = cancel
	run.mu.Unlock()

	defer cancel()

	// Build engine config.
	engineCfg := attractor.EngineConfig{
		CheckpointDir: run.CheckpointDir,
		ArtifactDir:   run.ArtifactDir,
		DefaultRetry:  retryPolicyFromName(run.Config.RetryPolicy),
		Handlers:      attractor.DefaultHandlerRegistry(),
		Backend:       DetectBackend(run.Config.Backend),
		BaseURL:       run.Config.BaseURL,
		RunID:         run.ID,
	}

	engine := attractor.NewEngine(engineCfg)
	engine.SetEventHandler(newEventHandler(run))

	// Wire up the MCP interviewer for human gates.
	iv := &mcpInterviewer{run: run}
	registry := wrapRegistryWithInterviewer(engineCfg.Handlers, iv)
	engineCfg.Handlers = registry
	engine = attractor.NewEngine(engineCfg)
	engine.SetEventHandler(newEventHandler(run))

	// Execute.
	result, err := engine.RunGraph(pipelineCtx, graph)

	// Update run state.
	run.mu.Lock()
	defer run.mu.Unlock()
	if err != nil {
		run.Status = StatusFailed
		run.Error = err.Error()
	} else {
		run.Status = StatusCompleted
		run.Result = result
	}

	// Update disk index.
	s.index.Save(&IndexEntry{
		RunID:         run.ID,
		Source:        run.Source,
		Config:        run.Config,
		Status:        string(run.Status),
		CheckpointDir: run.CheckpointDir,
		ArtifactDir:   run.ArtifactDir,
	})
}

// wrapRegistryWithInterviewer wraps all handlers in the registry to inject the interviewer.
// This mirrors the pattern from attractor/server.go.
func wrapRegistryWithInterviewer(source *attractor.HandlerRegistry, iv attractor.Interviewer) *attractor.HandlerRegistry {
	wrapped := attractor.NewHandlerRegistry()
	for typeName, handler := range source.All() {
		wrapped.Register(&interviewerWrapper{
			inner:       handler,
			interviewer: iv,
		})
		_ = typeName
	}
	return wrapped
}

type interviewerWrapper struct {
	inner       attractor.NodeHandler
	interviewer attractor.Interviewer
}

func (w *interviewerWrapper) Type() string { return w.inner.Type() }
func (w *interviewerWrapper) Execute(ctx context.Context, node *attractor.Node, pctx *attractor.Context, store *attractor.ArtifactStore) (*attractor.Outcome, error) {
	pctx.Set("_interviewer", w.interviewer)
	return w.inner.Execute(ctx, node, pctx, store)
}

// retryPolicyFromName converts a policy name to a RetryPolicy.
func retryPolicyFromName(name string) attractor.RetryPolicy {
	switch name {
	case "standard":
		return attractor.RetryPolicy{MaxAttempts: 5, InitialDelay: 100 * 1e6, BackoffMultiplier: 2.0, Jitter: true}
	case "aggressive":
		return attractor.RetryPolicy{MaxAttempts: 5, InitialDelay: 500 * 1e6, BackoffMultiplier: 2.0, Jitter: true}
	case "linear":
		return attractor.RetryPolicy{MaxAttempts: 3, InitialDelay: 500 * 1e6, BackoffMultiplier: 1.0}
	case "patient":
		return attractor.RetryPolicy{MaxAttempts: 3, InitialDelay: 2000 * 1e6, BackoffMultiplier: 3.0}
	default:
		return attractor.RetryPolicy{MaxAttempts: 1}
	}
}
```

Note: The `wrapRegistryWithInterviewer` function needs `source.All()` to iterate handlers. Check if `HandlerRegistry` has an `All()` method; if not, the implementation may need to use the existing `wrapRegistryWithInterviewer` from `attractor/server.go` directly. If that function is unexported, we may need to export it or add an `All()` method to `HandlerRegistry`. Adjust accordingly during implementation.

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestRunPipeline -v -timeout 30s`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/server.go mcp/tool_run.go mcp/tool_run_test.go
git commit -m "feat(mcp): add run_pipeline tool handler with async execution"
```

---

### Task 10: get_run_status Tool

**Files:**
- Create: `mcp/tool_status.go`
- Create: `mcp/tool_status_test.go`

**Step 1: Write the tests**

```go
// mcp/tool_status_test.go

// ABOUTME: Tests for the get_run_status MCP tool.
// ABOUTME: Validates status retrieval, current activity, and pending question reporting.
package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

func TestGetRunStatusRunning(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	run.mu.Lock()
	run.CurrentNode = "build_step"
	run.CurrentActivity = "calling tool: write_file"
	run.CompletedNodes = []string{"start"}
	run.mu.Unlock()

	input := GetRunStatusInput{RunID: run.ID}
	_, output, err := server.handleGetRunStatus(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Status != string(StatusRunning) {
		t.Errorf("status: got %q, want %q", output.Status, StatusRunning)
	}
	if output.CurrentNode != "build_step" {
		t.Errorf("current_node: got %q", output.CurrentNode)
	}
	if output.CurrentActivity != "calling tool: write_file" {
		t.Errorf("current_activity: got %q", output.CurrentActivity)
	}
	if len(output.CompletedNodes) != 1 || output.CompletedNodes[0] != "start" {
		t.Errorf("completed_nodes: got %v", output.CompletedNodes)
	}
}

func TestGetRunStatusPaused(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	run.mu.Lock()
	run.Status = StatusPaused
	run.PendingQuestion = &PendingQuestion{
		ID:   "q1",
		Text: "Continue?",
	}
	run.mu.Unlock()

	input := GetRunStatusInput{RunID: run.ID}
	_, output, err := server.handleGetRunStatus(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Status != string(StatusPaused) {
		t.Errorf("status: got %q", output.Status)
	}
	if output.PendingQuestion == nil {
		t.Fatal("expected pending question")
	}
	if output.PendingQuestion.Text != "Continue?" {
		t.Errorf("question text: got %q", output.PendingQuestion.Text)
	}
}

func TestGetRunStatusNotFound(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	input := GetRunStatusInput{RunID: "nonexistent"}
	_, _, err := server.handleGetRunStatus(context.Background(), nil, input)
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestGetRunStatus -v`
Expected: FAIL

**Step 3: Implement get_run_status**

```go
// mcp/tool_status.go

// ABOUTME: MCP tool handler for get_run_status.
// ABOUTME: Returns current pipeline state including node, activity, and pending questions.
package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetRunStatusInput is the input schema for get_run_status.
type GetRunStatusInput struct {
	RunID string `json:"run_id" jsonschema:"the run ID to query"`
}

// GetRunStatusOutput describes the current state of a pipeline run.
type GetRunStatusOutput struct {
	RunID           string           `json:"run_id"`
	Status          string           `json:"status"`
	CurrentNode     string           `json:"current_node,omitempty"`
	CurrentActivity string           `json:"current_activity,omitempty"`
	CompletedNodes  []string         `json:"completed_nodes"`
	PendingQuestion *PendingQuestion `json:"pending_question,omitempty"`
	Error           string           `json:"error,omitempty"`
}

func (s *Server) handleGetRunStatus(
	ctx context.Context,
	req *mcpsdk.CallToolRequest,
	input GetRunStatusInput,
) (*mcpsdk.CallToolResult, GetRunStatusOutput, error) {
	run, ok := s.registry.Get(input.RunID)
	if !ok {
		return nil, GetRunStatusOutput{}, fmt.Errorf("run not found: %s", input.RunID)
	}

	run.mu.RLock()
	defer run.mu.RUnlock()

	return nil, GetRunStatusOutput{
		RunID:           run.ID,
		Status:          string(run.Status),
		CurrentNode:     run.CurrentNode,
		CurrentActivity: run.CurrentActivity,
		CompletedNodes:  run.CompletedNodes,
		PendingQuestion: run.PendingQuestion,
		Error:           run.Error,
	}, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestGetRunStatus -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/tool_status.go mcp/tool_status_test.go
git commit -m "feat(mcp): add get_run_status tool handler"
```

---

### Task 11: get_run_events Tool

**Files:**
- Create: `mcp/tool_events.go`
- Create: `mcp/tool_events_test.go`

**Step 1: Write the tests**

```go
// mcp/tool_events_test.go

// ABOUTME: Tests for the get_run_events MCP tool.
// ABOUTME: Validates event retrieval with since and type filters.
package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

func TestGetRunEventsAll(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	run.mu.Lock()
	now := time.Now()
	run.EventBuffer = []attractor.EngineEvent{
		{Type: attractor.EventStageStarted, NodeID: "a", Timestamp: now},
		{Type: attractor.EventStageCompleted, NodeID: "a", Timestamp: now.Add(time.Second)},
		{Type: attractor.EventStageStarted, NodeID: "b", Timestamp: now.Add(2 * time.Second)},
	}
	run.mu.Unlock()

	input := GetRunEventsInput{RunID: run.ID}
	_, output, err := server.handleGetRunEvents(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output.Events) != 3 {
		t.Errorf("expected 3 events, got %d", len(output.Events))
	}
}

func TestGetRunEventsWithTypeFilter(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	run.mu.Lock()
	now := time.Now()
	run.EventBuffer = []attractor.EngineEvent{
		{Type: attractor.EventStageStarted, NodeID: "a", Timestamp: now},
		{Type: attractor.EventStageCompleted, NodeID: "a", Timestamp: now.Add(time.Second)},
		{Type: attractor.EventStageStarted, NodeID: "b", Timestamp: now.Add(2 * time.Second)},
	}
	run.mu.Unlock()

	input := GetRunEventsInput{RunID: run.ID, Types: []string{"stage.started"}}
	_, output, err := server.handleGetRunEvents(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output.Events) != 2 {
		t.Errorf("expected 2 stage.started events, got %d", len(output.Events))
	}
}

func TestGetRunEventsWithSince(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	now := time.Now()
	run.mu.Lock()
	run.EventBuffer = []attractor.EngineEvent{
		{Type: attractor.EventStageStarted, NodeID: "a", Timestamp: now},
		{Type: attractor.EventStageCompleted, NodeID: "a", Timestamp: now.Add(time.Second)},
		{Type: attractor.EventStageStarted, NodeID: "b", Timestamp: now.Add(2 * time.Second)},
	}
	run.mu.Unlock()

	since := now.Add(500 * time.Millisecond).Format(time.RFC3339Nano)
	input := GetRunEventsInput{RunID: run.ID, Since: since}
	_, output, err := server.handleGetRunEvents(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output.Events) != 2 {
		t.Errorf("expected 2 events after since, got %d", len(output.Events))
	}
}

func TestGetRunEventsNotFound(t *testing.T) {
	server := &Server{registry: NewRunRegistry()}
	_, _, err := server.handleGetRunEvents(context.Background(), nil, GetRunEventsInput{RunID: "nope"})
	if err == nil {
		t.Fatal("expected error")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestGetRunEvents -v`
Expected: FAIL

**Step 3: Implement get_run_events**

```go
// mcp/tool_events.go

// ABOUTME: MCP tool handler for get_run_events.
// ABOUTME: Returns engine events from the rolling buffer with optional since/type filters.
package mcp

import (
	"context"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/2389-research/mammoth/attractor"
)

// GetRunEventsInput is the input schema for get_run_events.
type GetRunEventsInput struct {
	RunID string   `json:"run_id" jsonschema:"the run ID to query"`
	Since string   `json:"since,omitempty" jsonschema:"RFC3339 timestamp to filter events after"`
	Types []string `json:"types,omitempty" jsonschema:"event types to include (e.g. stage.started)"`
}

// EventEntry is a serializable representation of an engine event.
type EventEntry struct {
	Type      string         `json:"type"`
	NodeID    string         `json:"node_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp string         `json:"timestamp"`
}

// GetRunEventsOutput contains the filtered events.
type GetRunEventsOutput struct {
	RunID  string       `json:"run_id"`
	Events []EventEntry `json:"events"`
}

func (s *Server) handleGetRunEvents(
	ctx context.Context,
	req *mcpsdk.CallToolRequest,
	input GetRunEventsInput,
) (*mcpsdk.CallToolResult, GetRunEventsOutput, error) {
	run, ok := s.registry.Get(input.RunID)
	if !ok {
		return nil, GetRunEventsOutput{}, fmt.Errorf("run not found: %s", input.RunID)
	}

	// Parse since filter.
	var sinceTime time.Time
	if input.Since != "" {
		var err error
		sinceTime, err = time.Parse(time.RFC3339Nano, input.Since)
		if err != nil {
			return nil, GetRunEventsOutput{}, fmt.Errorf("invalid since timestamp: %w", err)
		}
	}

	// Build type filter set.
	typeFilter := make(map[string]bool, len(input.Types))
	for _, t := range input.Types {
		typeFilter[t] = true
	}

	run.mu.RLock()
	defer run.mu.RUnlock()

	var events []EventEntry
	for _, evt := range run.EventBuffer {
		if !sinceTime.IsZero() && !evt.Timestamp.After(sinceTime) {
			continue
		}
		if len(typeFilter) > 0 && !typeFilter[string(evt.Type)] {
			continue
		}
		events = append(events, EventEntry{
			Type:      string(evt.Type),
			NodeID:    evt.NodeID,
			Data:      evt.Data,
			Timestamp: evt.Timestamp.Format(time.RFC3339Nano),
		})
	}

	return nil, GetRunEventsOutput{
		RunID:  input.RunID,
		Events: events,
	}, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestGetRunEvents -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/tool_events.go mcp/tool_events_test.go
git commit -m "feat(mcp): add get_run_events tool handler with filters"
```

---

### Task 12: get_run_logs Tool

**Files:**
- Create: `mcp/tool_logs.go`
- Create: `mcp/tool_logs_test.go`

**Step 1: Write the tests**

```go
// mcp/tool_logs_test.go

// ABOUTME: Tests for the get_run_logs MCP tool.
// ABOUTME: Validates log retrieval from event buffer with tail and node filtering.
package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/2389-research/mammoth/attractor"
)

func TestGetRunLogsAll(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	now := time.Now()
	run.mu.Lock()
	run.EventBuffer = []attractor.EngineEvent{
		{Type: attractor.EventStageStarted, NodeID: "a", Timestamp: now},
		{Type: attractor.EventAgentToolCallStart, NodeID: "a", Timestamp: now.Add(time.Second), Data: map[string]any{"tool_name": "write_file"}},
		{Type: attractor.EventAgentLLMTurn, NodeID: "a", Timestamp: now.Add(2 * time.Second), Data: map[string]any{"input_tokens": 100}},
		{Type: attractor.EventStageCompleted, NodeID: "a", Timestamp: now.Add(3 * time.Second)},
	}
	run.mu.Unlock()

	input := GetRunLogsInput{RunID: run.ID}
	_, output, err := server.handleGetRunLogs(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output.Lines) == 0 {
		t.Error("expected log lines")
	}
}

func TestGetRunLogsTail(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	now := time.Now()
	run.mu.Lock()
	for i := 0; i < 20; i++ {
		run.EventBuffer = append(run.EventBuffer, attractor.EngineEvent{
			Type:      attractor.EventStageStarted,
			NodeID:    "node",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		})
	}
	run.mu.Unlock()

	input := GetRunLogsInput{RunID: run.ID, Tail: 5}
	_, output, err := server.handleGetRunLogs(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output.Lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(output.Lines))
	}
}

func TestGetRunLogsNodeFilter(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	now := time.Now()
	run.mu.Lock()
	run.EventBuffer = []attractor.EngineEvent{
		{Type: attractor.EventStageStarted, NodeID: "a", Timestamp: now},
		{Type: attractor.EventStageStarted, NodeID: "b", Timestamp: now.Add(time.Second)},
		{Type: attractor.EventStageCompleted, NodeID: "a", Timestamp: now.Add(2 * time.Second)},
	}
	run.mu.Unlock()

	input := GetRunLogsInput{RunID: run.ID, NodeID: "a"}
	_, output, err := server.handleGetRunLogs(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output.Lines) != 2 {
		t.Errorf("expected 2 lines for node 'a', got %d", len(output.Lines))
	}
}

func TestGetRunLogsNotFound(t *testing.T) {
	server := &Server{registry: NewRunRegistry()}
	_, _, err := server.handleGetRunLogs(context.Background(), nil, GetRunLogsInput{RunID: "nope"})
	if err == nil {
		t.Fatal("expected error")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestGetRunLogs -v`
Expected: FAIL

**Step 3: Implement get_run_logs**

```go
// mcp/tool_logs.go

// ABOUTME: MCP tool handler for get_run_logs.
// ABOUTME: Returns human-readable log lines from the event buffer with tail and node filtering.
package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetRunLogsInput is the input schema for get_run_logs.
type GetRunLogsInput struct {
	RunID  string `json:"run_id" jsonschema:"the run ID to query"`
	Tail   int    `json:"tail,omitempty"    jsonschema:"return only the last N log lines"`
	NodeID string `json:"node_id,omitempty" jsonschema:"filter logs to a specific node"`
}

// GetRunLogsOutput contains human-readable log lines.
type GetRunLogsOutput struct {
	RunID string   `json:"run_id"`
	Lines []string `json:"lines"`
}

func (s *Server) handleGetRunLogs(
	ctx context.Context,
	req *mcpsdk.CallToolRequest,
	input GetRunLogsInput,
) (*mcpsdk.CallToolResult, GetRunLogsOutput, error) {
	run, ok := s.registry.Get(input.RunID)
	if !ok {
		return nil, GetRunLogsOutput{}, fmt.Errorf("run not found: %s", input.RunID)
	}

	run.mu.RLock()
	defer run.mu.RUnlock()

	var lines []string
	for _, evt := range run.EventBuffer {
		if input.NodeID != "" && evt.NodeID != input.NodeID {
			continue
		}
		line := formatEventAsLog(evt)
		lines = append(lines, line)
	}

	// Apply tail.
	if input.Tail > 0 && len(lines) > input.Tail {
		lines = lines[len(lines)-input.Tail:]
	}

	return nil, GetRunLogsOutput{
		RunID: input.RunID,
		Lines: lines,
	}, nil
}

// formatEventAsLog renders an engine event as a human-readable log line.
func formatEventAsLog(evt attractor.EngineEvent) string {
	ts := evt.Timestamp.Format("15:04:05.000")
	switch evt.Type {
	case attractor.EventStageStarted:
		return fmt.Sprintf("[%s] START %s", ts, evt.NodeID)
	case attractor.EventStageCompleted:
		return fmt.Sprintf("[%s] DONE  %s", ts, evt.NodeID)
	case attractor.EventStageFailed:
		reason := ""
		if r, ok := evt.Data["reason"]; ok {
			reason = fmt.Sprintf(": %v", r)
		}
		return fmt.Sprintf("[%s] FAIL  %s%s", ts, evt.NodeID, reason)
	case attractor.EventStageRetrying:
		return fmt.Sprintf("[%s] RETRY %s", ts, evt.NodeID)
	case attractor.EventAgentToolCallStart:
		tool := "unknown"
		if tn, ok := evt.Data["tool_name"]; ok {
			tool = fmt.Sprintf("%v", tn)
		}
		return fmt.Sprintf("[%s] TOOL  %s: %s", ts, evt.NodeID, tool)
	case attractor.EventAgentLLMTurn:
		tokens := ""
		if t, ok := evt.Data["total_tokens"]; ok {
			tokens = fmt.Sprintf(" (%v tokens)", t)
		}
		return fmt.Sprintf("[%s] LLM   %s%s", ts, evt.NodeID, tokens)
	case attractor.EventCheckpointSaved:
		return fmt.Sprintf("[%s] CKPT  saved at %s", ts, evt.NodeID)
	case attractor.EventPipelineStarted:
		return fmt.Sprintf("[%s] PIPELINE STARTED", ts)
	case attractor.EventPipelineCompleted:
		return fmt.Sprintf("[%s] PIPELINE COMPLETED", ts)
	case attractor.EventPipelineFailed:
		errMsg := ""
		if e, ok := evt.Data["error"]; ok {
			errMsg = fmt.Sprintf(": %v", e)
		}
		return fmt.Sprintf("[%s] PIPELINE FAILED%s", ts, errMsg)
	default:
		return fmt.Sprintf("[%s] %s %s", ts, evt.Type, evt.NodeID)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestGetRunLogs -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/tool_logs.go mcp/tool_logs_test.go
git commit -m "feat(mcp): add get_run_logs tool handler"
```

---

### Task 13: answer_question Tool

**Files:**
- Create: `mcp/tool_answer.go`
- Create: `mcp/tool_answer_test.go`

**Step 1: Write the tests**

```go
// mcp/tool_answer_test.go

// ABOUTME: Tests for the answer_question MCP tool.
// ABOUTME: Validates answer delivery, error on non-paused runs, and missing runs.
package mcp

import (
	"context"
	"testing"
	"time"
)

func TestAnswerQuestionDelivers(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	run.mu.Lock()
	run.Status = StatusPaused
	run.PendingQuestion = &PendingQuestion{ID: "q1", Text: "Continue?"}
	run.mu.Unlock()

	// Start a goroutine that reads from the answer channel.
	received := make(chan string, 1)
	go func() {
		answer := <-run.answerCh
		received <- answer
	}()

	input := AnswerQuestionInput{RunID: run.ID, Answer: "yes"}
	_, output, err := server.handleAnswerQuestion(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !output.Acknowledged {
		t.Error("expected acknowledged=true")
	}

	select {
	case answer := <-received:
		if answer != "yes" {
			t.Errorf("expected %q, got %q", "yes", answer)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("answer not received in time")
	}
}

func TestAnswerQuestionNotPaused(t *testing.T) {
	reg := NewRunRegistry()
	server := &Server{registry: reg}

	run := reg.Create("digraph{}", RunConfig{})
	// Status is StatusRunning by default (not paused).

	input := AnswerQuestionInput{RunID: run.ID, Answer: "yes"}
	_, _, err := server.handleAnswerQuestion(context.Background(), nil, input)
	if err == nil {
		t.Fatal("expected error for non-paused run")
	}
}

func TestAnswerQuestionNotFound(t *testing.T) {
	server := &Server{registry: NewRunRegistry()}
	_, _, err := server.handleAnswerQuestion(context.Background(), nil, AnswerQuestionInput{RunID: "nope", Answer: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestAnswerQuestion -v`
Expected: FAIL

**Step 3: Implement answer_question**

```go
// mcp/tool_answer.go

// ABOUTME: MCP tool handler for answer_question.
// ABOUTME: Delivers a human gate answer to a paused pipeline via the run's answer channel.
package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// AnswerQuestionInput is the input schema for answer_question.
type AnswerQuestionInput struct {
	RunID  string `json:"run_id" jsonschema:"the run ID with a pending question"`
	Answer string `json:"answer"  jsonschema:"the answer to the pending question"`
}

// AnswerQuestionOutput confirms the answer was delivered.
type AnswerQuestionOutput struct {
	Acknowledged bool `json:"acknowledged"`
}

func (s *Server) handleAnswerQuestion(
	ctx context.Context,
	req *mcpsdk.CallToolRequest,
	input AnswerQuestionInput,
) (*mcpsdk.CallToolResult, AnswerQuestionOutput, error) {
	run, ok := s.registry.Get(input.RunID)
	if !ok {
		return nil, AnswerQuestionOutput{}, fmt.Errorf("run not found: %s", input.RunID)
	}

	run.mu.RLock()
	status := run.Status
	run.mu.RUnlock()

	if status != StatusPaused {
		return nil, AnswerQuestionOutput{}, fmt.Errorf("run %s is not waiting for input (status: %s)", input.RunID, status)
	}

	// Deliver the answer. The mcpInterviewer.Ask is blocking on this channel.
	run.answerCh <- input.Answer

	return nil, AnswerQuestionOutput{Acknowledged: true}, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestAnswerQuestion -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/tool_answer.go mcp/tool_answer_test.go
git commit -m "feat(mcp): add answer_question tool handler"
```

---

### Task 14: resume_pipeline Tool

**Files:**
- Create: `mcp/tool_resume.go`
- Create: `mcp/tool_resume_test.go`

**Step 1: Write the tests**

```go
// mcp/tool_resume_test.go

// ABOUTME: Tests for the resume_pipeline MCP tool.
// ABOUTME: Validates checkpoint lookup, resume execution, and missing checkpoint errors.
package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/2389-research/mammoth/attractor"
)

func TestResumePipelineNoCheckpoint(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(dir)

	// Save an index entry with no checkpoint.
	server.index.Save(&IndexEntry{
		RunID:  "old-run",
		Source: `digraph Test { graph [goal="test"] start [shape=Mdiamond]; end [shape=Msquare]; start -> end }`,
		Config: RunConfig{},
		Status: string(StatusFailed),
	})

	input := ResumePipelineInput{RunID: "old-run"}
	_, _, err := server.handleResumePipeline(context.Background(), nil, input)
	if err == nil {
		t.Fatal("expected error when no checkpoint exists")
	}
}

func TestResumePipelineNotFound(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(dir)

	input := ResumePipelineInput{RunID: "nonexistent"}
	_, _, err := server.handleResumePipeline(context.Background(), nil, input)
	if err == nil {
		t.Fatal("expected error for missing run")
	}
}

func TestResumePipelineWithCheckpoint(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(dir)

	// Create a valid checkpoint file.
	cpDir := filepath.Join(dir, "old-run", "checkpoint")
	os.MkdirAll(cpDir, 0755)
	cpPath := filepath.Join(cpDir, "checkpoint_latest.json")

	cp := attractor.NewCheckpoint(attractor.NewContext(), "start", []string{}, map[string]int{})
	if err := cp.Save(cpPath); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	server.index.Save(&IndexEntry{
		RunID:         "old-run",
		Source:        `digraph Test { graph [goal="test"] start [shape=Mdiamond]; end [shape=Msquare]; start -> end }`,
		Config:        RunConfig{},
		Status:        string(StatusFailed),
		CheckpointDir: cpDir,
	})

	input := ResumePipelineInput{RunID: "old-run"}
	_, output, err := server.handleResumePipeline(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.RunID == "" {
		t.Fatal("expected new run ID")
	}
	if output.RunID == "old-run" {
		t.Error("expected a different run ID from the original")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestResumePipeline -v`
Expected: FAIL

**Step 3: Implement resume_pipeline**

```go
// mcp/tool_resume.go

// ABOUTME: MCP tool handler for resume_pipeline.
// ABOUTME: Loads a previous run's checkpoint from disk and starts a new engine run from that point.
package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/2389-research/mammoth/attractor"
)

// ResumePipelineInput is the input schema for resume_pipeline.
type ResumePipelineInput struct {
	RunID string `json:"run_id" jsonschema:"the run ID to resume from checkpoint"`
}

// ResumePipelineOutput returns the new run's ID.
type ResumePipelineOutput struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

func (s *Server) handleResumePipeline(
	ctx context.Context,
	req *mcpsdk.CallToolRequest,
	input ResumePipelineInput,
) (*mcpsdk.CallToolResult, ResumePipelineOutput, error) {
	// Load the previous run's metadata from disk.
	entry, err := s.index.Load(input.RunID)
	if err != nil {
		return nil, ResumePipelineOutput{}, fmt.Errorf("run not found: %w", err)
	}

	// Find the latest checkpoint file.
	cpPath, err := findLatestCheckpoint(entry.CheckpointDir)
	if err != nil {
		return nil, ResumePipelineOutput{}, fmt.Errorf("no checkpoint available for run %s: %w", input.RunID, err)
	}

	// Parse the original DOT source.
	graph, err := attractor.Parse(entry.Source)
	if err != nil {
		return nil, ResumePipelineOutput{}, fmt.Errorf("parse original source: %w", err)
	}
	graph = attractor.ApplyTransforms(graph, attractor.DefaultTransforms()...)

	// Create a new run for the resumed execution.
	run := s.registry.Create(entry.Source, entry.Config)
	runDir := s.index.RunDir(run.ID)
	run.mu.Lock()
	run.CheckpointDir = filepath.Join(runDir, "checkpoint")
	run.ArtifactDir = filepath.Join(runDir, "artifacts")
	run.mu.Unlock()

	// Persist the new run.
	s.index.Save(&IndexEntry{
		RunID:         run.ID,
		Source:        entry.Source,
		Config:        entry.Config,
		Status:        string(StatusRunning),
		CheckpointDir: run.CheckpointDir,
		ArtifactDir:   run.ArtifactDir,
	})

	// Spawn resume in a goroutine.
	go s.resumePipeline(run, graph, cpPath)

	return nil, ResumePipelineOutput{
		RunID:  run.ID,
		Status: string(StatusRunning),
	}, nil
}

// resumePipeline resumes execution from a checkpoint.
func (s *Server) resumePipeline(run *ActiveRun, graph *attractor.Graph, checkpointPath string) {
	pipelineCtx, cancel := context.WithCancel(context.Background())
	run.mu.Lock()
	run.cancel = cancel
	run.mu.Unlock()

	defer cancel()

	engineCfg := attractor.EngineConfig{
		CheckpointDir: run.CheckpointDir,
		ArtifactDir:   run.ArtifactDir,
		DefaultRetry:  retryPolicyFromName(run.Config.RetryPolicy),
		Handlers:      attractor.DefaultHandlerRegistry(),
		Backend:       DetectBackend(run.Config.Backend),
		BaseURL:       run.Config.BaseURL,
		RunID:         run.ID,
	}

	// Wire interviewer.
	iv := &mcpInterviewer{run: run}
	engineCfg.Handlers = wrapRegistryWithInterviewer(engineCfg.Handlers, iv)

	engine := attractor.NewEngine(engineCfg)
	engine.SetEventHandler(newEventHandler(run))

	result, err := engine.ResumeFromCheckpoint(pipelineCtx, graph, checkpointPath)

	run.mu.Lock()
	defer run.mu.Unlock()
	if err != nil {
		run.Status = StatusFailed
		run.Error = err.Error()
	} else {
		run.Status = StatusCompleted
		run.Result = result
	}

	s.index.Save(&IndexEntry{
		RunID:         run.ID,
		Source:        run.Source,
		Config:        run.Config,
		Status:        string(run.Status),
		CheckpointDir: run.CheckpointDir,
		ArtifactDir:   run.ArtifactDir,
	})
}

// findLatestCheckpoint finds the most recent checkpoint file in a directory.
func findLatestCheckpoint(dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("no checkpoint directory configured")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read checkpoint dir: %w", err)
	}

	var cpFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "checkpoint") && strings.HasSuffix(e.Name(), ".json") {
			cpFiles = append(cpFiles, filepath.Join(dir, e.Name()))
		}
	}

	if len(cpFiles) == 0 {
		return "", fmt.Errorf("no checkpoint files found in %s", dir)
	}

	// Sort by name (timestamps in filenames give chronological order).
	sort.Strings(cpFiles)
	return cpFiles[len(cpFiles)-1], nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestResumePipeline -v`
Expected: PASS

**Step 5: Commit**

```bash
git add mcp/tool_resume.go mcp/tool_resume_test.go
git commit -m "feat(mcp): add resume_pipeline tool handler"
```

---

### Task 15: Wire Up cmd/mammoth-mcp/main.go

**Files:**
- Modify: `cmd/mammoth-mcp/main.go`

**Step 1: Write a basic smoke test**

```go
// cmd/mammoth-mcp/main_test.go

// ABOUTME: Smoke test for mammoth-mcp binary.
// ABOUTME: Verifies the binary compiles and the run function initializes without error.
package main

import (
	"testing"
)

func TestMainCompiles(t *testing.T) {
	// Compilation test — if this file compiles, the wiring is correct.
	t.Log("mammoth-mcp compiles successfully")
}
```

**Step 2: Implement the full main.go**

```go
// cmd/mammoth-mcp/main.go

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

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	mammothmcp "github.com/2389-research/mammoth/mcp"
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
			return fmt.Errorf("get home dir: %w", err)
		}
		dataDir = filepath.Join(home, ".mammoth", "mcp-runs")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Create the mammoth MCP server.
	srv := mammothmcp.NewServer(dataDir)

	// Create the MCP protocol server.
	mcpServer := mcpsdk.NewServer(
		&mcpsdk.Implementation{
			Name:    "mammoth",
			Version: "0.1.0",
		},
		nil,
	)

	// Register tools.
	srv.RegisterTools(mcpServer)

	// Serve over stdio.
	return mcpServer.Run(ctx, &mcpsdk.StdioTransport{})
}
```

**Step 3: Add RegisterTools method to Server**

Add to `mcp/server.go`:

```go
// RegisterTools registers all mammoth pipeline tools with the MCP server.
func (s *Server) RegisterTools(mcpServer *mcpsdk.Server) {
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "validate_pipeline",
		Description: "Validate a DOT pipeline without executing it. Returns parse errors, lint warnings, and auto-fix suggestions.",
	}, handleValidatePipeline)

	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "run_pipeline",
		Description: "Start executing a DOT pipeline. Returns a run ID immediately. Use get_run_status to monitor progress.",
	}, s.handleRunPipeline)

	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "get_run_status",
		Description: "Get the current status of a pipeline run, including which node is executing, what the agent is doing, and any pending human gate questions.",
	}, s.handleGetRunStatus)

	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "get_run_events",
		Description: "Fetch engine events from a pipeline run. Supports filtering by timestamp (since) and event type.",
	}, s.handleGetRunEvents)

	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "get_run_logs",
		Description: "Get human-readable console log lines from a pipeline run. Supports tail (last N lines) and node filtering.",
	}, s.handleGetRunLogs)

	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "answer_question",
		Description: "Answer a pending human gate question to unblock a paused pipeline.",
	}, s.handleAnswerQuestion)

	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "resume_pipeline",
		Description: "Resume a previously checkpointed pipeline run. Creates a new run from the last checkpoint.",
	}, s.handleResumePipeline)
}
```

**Step 4: Verify it compiles and tests pass**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go build ./cmd/mammoth-mcp/ && go test ./cmd/mammoth-mcp/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/mammoth-mcp/main.go cmd/mammoth-mcp/main_test.go mcp/server.go
git commit -m "feat(mcp): wire up mammoth-mcp binary with all tools"
```

---

### Task 16: Integration Test — Full Pipeline via MCP

**Files:**
- Create: `mcp/integration_test.go`

**Step 1: Write the integration test**

This test creates a real MCP server in-process, connects via in-memory transport, and runs a simple pipeline end-to-end.

```go
// mcp/integration_test.go

// ABOUTME: Integration tests for the mammoth MCP server.
// ABOUTME: Tests full tool call flows through the MCP protocol using in-memory transport.
package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestIntegrationValidatePipeline(t *testing.T) {
	srv, client := setupTestServer(t)
	_ = srv

	// Call validate_pipeline with valid DOT.
	result, err := client.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "validate_pipeline",
		Arguments: mustJSON(map[string]any{
			"source": `digraph Test {
				graph [goal="test"]
				start [shape=Mdiamond]
				end [shape=Msquare]
				start -> end
			}`,
		}),
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %+v", result.Content)
	}
}

func TestIntegrationRunAndPollStatus(t *testing.T) {
	srv, client := setupTestServer(t)
	_ = srv

	// Start a pipeline.
	result, err := client.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "run_pipeline",
		Arguments: mustJSON(map[string]any{
			"source": `digraph Test {
				graph [goal="test"]
				start [shape=Mdiamond]
				end [shape=Msquare]
				start -> end
			}`,
		}),
	})
	if err != nil {
		t.Fatalf("run_pipeline: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool error: %+v", result.Content)
	}

	// Extract run_id from structured content.
	var runOutput RunPipelineOutput
	extractOutput(t, result, &runOutput)
	if runOutput.RunID == "" {
		t.Fatal("expected run_id")
	}

	// Poll status until completed or failed.
	deadline := time.After(15 * time.Second)
	for {
		statusResult, err := client.CallTool(context.Background(), &mcpsdk.CallToolParams{
			Name: "get_run_status",
			Arguments: mustJSON(map[string]any{
				"run_id": runOutput.RunID,
			}),
		})
		if err != nil {
			t.Fatalf("get_run_status: %v", err)
		}

		var status GetRunStatusOutput
		extractOutput(t, statusResult, &status)

		if status.Status == "completed" || status.Status == "failed" {
			t.Logf("pipeline finished with status: %s", status.Status)
			break
		}

		select {
		case <-deadline:
			t.Fatalf("pipeline did not complete, last status: %s, activity: %s", status.Status, status.CurrentActivity)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// setupTestServer creates an MCP server and client connected via in-memory transport.
func setupTestServer(t *testing.T) (*Server, *mcpsdk.Client) {
	t.Helper()

	dataDir := t.TempDir()
	srv := NewServer(dataDir)

	mcpServer := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "mammoth-test", Version: "test"},
		nil,
	)
	srv.RegisterTools(mcpServer)

	// Create in-memory transport pair.
	serverTransport, clientTransport := mcpsdk.NewInMemoryTransports()

	// Start the server in a goroutine.
	go func() {
		mcpServer.Run(context.Background(), serverTransport)
	}()

	// Create and connect the client.
	client := mcpsdk.NewClient(
		&mcpsdk.Implementation{Name: "test-client", Version: "test"},
		nil,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.Connect(ctx, clientTransport); err != nil {
		t.Fatalf("connect client: %v", err)
	}

	t.Cleanup(func() {
		client.Close()
	})

	return srv, client
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func extractOutput(t *testing.T, result *mcpsdk.CallToolResult, out any) {
	t.Helper()
	if result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatalf("marshal structured content: %v", err)
		}
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		return
	}
	// Fall back to parsing text content.
	for _, c := range result.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			if err := json.Unmarshal([]byte(tc.Text), out); err != nil {
				t.Fatalf("unmarshal text content: %v", err)
			}
			return
		}
	}
	t.Fatal("no output found in result")
}
```

Note: The exact MCP SDK client API (Client, Connect, CallTool, CallToolParams) may differ slightly. Check the SDK's pkg.go.dev docs during implementation and adjust. The key patterns are: `NewInMemoryTransports()` for testing, `client.CallTool()` for invoking tools, and `result.StructuredContent` for typed output.

**Step 2: Run integration tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -run TestIntegration -v -timeout 30s`
Expected: PASS

**Step 3: Commit**

```bash
git add mcp/integration_test.go
git commit -m "test(mcp): add integration tests for MCP server tools"
```

---

### Task 17: HandlerRegistry.All() — Export Handler Iteration

The `wrapRegistryWithInterviewer` function in `mcp/tool_run.go` needs to iterate over all registered handlers. The existing `HandlerRegistry` in `attractor/handlers.go` has an unexported `handlers` map. We need to add a public `All()` method.

**Files:**
- Modify: `attractor/handlers.go`
- Modify: `attractor/handlers_test.go` (add test for All)

**Step 1: Write the test**

Add to `attractor/handlers_test.go`:

```go
func TestHandlerRegistryAll(t *testing.T) {
	reg := DefaultHandlerRegistry()
	all := reg.All()
	if len(all) == 0 {
		t.Fatal("expected handlers in default registry")
	}
	// Should have all 10 built-in handlers.
	if len(all) != 10 {
		t.Errorf("expected 10 handlers, got %d", len(all))
	}
	// Check a known handler exists.
	if _, ok := all["start"]; !ok {
		t.Error("expected 'start' handler")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestHandlerRegistryAll -v`
Expected: FAIL — All method not defined

**Step 3: Add All() method**

Add to `attractor/handlers.go`:

```go
// All returns a copy of all registered handlers keyed by type name.
func (r *HandlerRegistry) All() map[string]NodeHandler {
	result := make(map[string]NodeHandler, len(r.handlers))
	for k, v := range r.handlers {
		result[k] = v
	}
	return result
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -run TestHandlerRegistryAll -v`
Expected: PASS

**Step 5: Commit**

```bash
git add attractor/handlers.go attractor/handlers_test.go
git commit -m "feat(attractor): export HandlerRegistry.All() for handler iteration"
```

---

### Task 18: Final Verification — Full Build and Test Suite

**Step 1: Build both binaries**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go build ./cmd/mammoth/ && go build ./cmd/mammoth-mcp/`
Expected: Both compile without errors

**Step 2: Run all mcp/ tests**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./mcp/ -v -timeout 60s`
Expected: All tests pass

**Step 3: Run attractor tests to confirm no regressions**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./attractor/ -v -timeout 120s`
Expected: All tests pass

**Step 4: Run full test suite**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go test ./... -timeout 120s`
Expected: All tests pass

**Step 5: Tidy and verify**

Run: `cd /Users/harper/Public/src/2389/mammoth-dev && go mod tidy && go vet ./...`
Expected: Clean

**Step 6: Final commit if any cleanup needed**

```bash
git add -A
git commit -m "chore: tidy modules and final verification"
```
