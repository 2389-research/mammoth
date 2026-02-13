// ABOUTME: Tests for the HTTP pipeline server covering all REST endpoints.
// ABOUTME: Validates pipeline submission, status queries, SSE streaming, cancellation, questions, context retrieval, and event query endpoints.
package attractor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// serverTestHandler is a configurable NodeHandler for server tests.
type serverTestHandler struct {
	typeName  string
	executeFn func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error)
}

func (h *serverTestHandler) Type() string { return h.typeName }

func (h *serverTestHandler) Execute(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
	if h.executeFn != nil {
		return h.executeFn(ctx, node, pctx, store)
	}
	return &Outcome{Status: StatusSuccess}, nil
}

// buildServerTestRegistry creates a registry with serverTestHandler instances.
func buildServerTestRegistry(handlers ...*serverTestHandler) *HandlerRegistry {
	reg := NewHandlerRegistry()
	for _, h := range handlers {
		reg.Register(h)
	}
	return reg
}

// simpleDOTSource returns a minimal valid DOT pipeline for testing.
func simpleDOTSource() string {
	return `digraph test {
		start [shape=Mdiamond]
		step [shape=box, label="Step"]
		done [shape=Msquare]
		start -> step
		step -> done
	}`
}

// newServerTestEngine creates an engine with simple success handlers for server tests.
func newServerTestEngine() *Engine {
	reg := buildServerTestRegistry(
		&serverTestHandler{typeName: "start"},
		&serverTestHandler{typeName: "codergen"},
		&serverTestHandler{typeName: "exit"},
	)
	return NewEngine(EngineConfig{
		Handlers:     reg,
		Backend:      &fakeBackend{},
		DefaultRetry: RetryPolicyNone(),
	})
}

func TestPostPipelinesSubmitAndGetID(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// POST a pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.ID == "" {
		t.Error("expected non-empty pipeline ID")
	}
	if result.Status != "running" {
		t.Errorf("expected status 'running', got %q", result.Status)
	}
}

func TestPostPipelinesJSON(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	bodyObj := map[string]string{"source": simpleDOTSource()}
	bodyJSON, _ := json.Marshal(bodyObj)
	resp, err := http.Post(ts.URL+"/pipelines", "application/json", strings.NewReader(string(bodyJSON)))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.ID == "" {
		t.Error("expected non-empty pipeline ID")
	}
}

func TestGetPipelineStatusCompleted(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit a pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Wait for pipeline to complete
	deadline := time.Now().Add(5 * time.Second)
	var status PipelineStatus
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID)
		if err != nil {
			t.Fatalf("GET /pipelines/{id} failed: %v", err)
		}
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()

		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if status.Status != "completed" {
		t.Fatalf("expected status 'completed', got %q (error: %s)", status.Status, status.Error)
	}
	if len(status.CompletedNodes) == 0 {
		t.Error("expected completed nodes in status")
	}
}

func TestGetPipelineStatus404(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pipelines/nonexistent-id")
	if err != nil {
		t.Fatalf("GET /pipelines/{id} failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPostCancelPipeline(t *testing.T) {
	// Create an engine with a handler that blocks until cancelled
	var wg sync.WaitGroup
	wg.Add(1)
	reg := buildServerTestRegistry(
		&serverTestHandler{typeName: "start"},
		&serverTestHandler{
			typeName: "codergen",
			executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
				wg.Done()
				// Block until context is cancelled
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
		&serverTestHandler{typeName: "exit"},
	)
	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Wait until the handler is actually blocking
	wg.Wait()

	// Cancel the pipeline
	resp, err = http.Post(ts.URL+"/pipelines/"+submitResult.ID+"/cancel", "", nil)
	if err != nil {
		t.Fatalf("POST /pipelines/{id}/cancel failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	var cancelResult struct {
		Status string `json:"status"`
	}
	json.NewDecoder(resp.Body).Decode(&cancelResult)
	if cancelResult.Status != "cancelled" {
		t.Errorf("expected status 'cancelled', got %q", cancelResult.Status)
	}
}

func TestGetPipelineContext(t *testing.T) {
	// Use a handler that sets context values
	reg := buildServerTestRegistry(
		&serverTestHandler{typeName: "start"},
		&serverTestHandler{
			typeName: "codergen",
			executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
				return &Outcome{
					Status:         StatusSuccess,
					ContextUpdates: map[string]any{"my_key": "my_value"},
				}, nil
			},
		},
		&serverTestHandler{typeName: "exit"},
	)
	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID)
		if err != nil {
			t.Fatalf("GET status failed: %v", err)
		}
		var status PipelineStatus
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status.Status == "completed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Get context
	resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID + "/context")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/context failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var ctxSnapshot map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&ctxSnapshot); err != nil {
		t.Fatalf("failed to decode context: %v", err)
	}
	if ctxSnapshot["my_key"] != "my_value" {
		t.Errorf("expected context key 'my_key'='my_value', got %v", ctxSnapshot["my_key"])
	}
}

func TestGetPipelineContext404(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pipelines/nonexistent/context")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/context failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetPipelineEventsSSE(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Connect to SSE stream
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/pipelines/"+submitResult.ID+"/events", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/events failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %q", resp.Header.Get("Content-Type"))
	}

	// Read events until stream closes or timeout
	scanner := bufio.NewScanner(resp.Body)
	var events []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, line)
		}
	}

	if len(events) == 0 {
		t.Error("expected at least one SSE event")
	}
}

func TestPostPipelinesInvalidDOT(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Post invalid DOT source — should be rejected immediately with 400
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader("this is not valid DOT"))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for invalid DOT, got %d", resp.StatusCode)
	}

	var errResp struct {
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&errResp)

	if errResp.Error == "" {
		t.Error("expected error message for invalid DOT")
	}
	if !strings.Contains(errResp.Error, "parse") {
		t.Errorf("expected error to mention 'parse', got %q", errResp.Error)
	}
}

// TestPostPipelinesNoStartNode tests that a graph without a start node (Mdiamond)
// is rejected at submission time with a validation error.
func TestPostPipelinesNoStartNode(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Valid DOT syntax but no start node (Mdiamond)
	source := `digraph test { a -> b; b [shape=Msquare] }`
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(source))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for missing start node, got %d", resp.StatusCode)
	}

	var errResp struct {
		Error string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&errResp)

	if errResp.Error == "" {
		t.Error("expected error message for missing start node")
	}
	if !strings.Contains(errResp.Error, "validation") {
		t.Errorf("expected error to mention 'validation', got %q", errResp.Error)
	}
}

func TestGetPipelineQuestions(t *testing.T) {
	// Create a pipeline with a handler that asks a question
	questionAsked := make(chan struct{})
	reg := buildServerTestRegistry(
		&serverTestHandler{typeName: "start"},
		&serverTestHandler{
			typeName: "codergen",
			executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
				// Signal that we're about to ask a question
				interviewer := pctx.Get("_interviewer")
				if interviewer == nil {
					return &Outcome{Status: StatusSuccess}, nil
				}
				iv, ok := interviewer.(Interviewer)
				if !ok {
					return &Outcome{Status: StatusSuccess}, nil
				}

				close(questionAsked)
				answer, err := iv.Ask(ctx, "What color?", []string{"red", "blue"})
				if err != nil {
					return &Outcome{
						Status:        StatusFail,
						FailureReason: err.Error(),
					}, nil
				}
				return &Outcome{
					Status:         StatusSuccess,
					ContextUpdates: map[string]any{"color": answer},
				}, nil
			},
		},
		&serverTestHandler{typeName: "exit"},
	)
	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Wait for the question to be asked
	select {
	case <-questionAsked:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for question to be asked")
	}

	// Small delay to let the question register
	time.Sleep(100 * time.Millisecond)

	// Get pending questions
	resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID + "/questions")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/questions failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var questions []PendingQuestion
	if err := json.NewDecoder(resp.Body).Decode(&questions); err != nil {
		t.Fatalf("failed to decode questions: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("expected 1 pending question, got %d", len(questions))
	}
	if questions[0].Question != "What color?" {
		t.Errorf("expected question 'What color?', got %q", questions[0].Question)
	}
	if questions[0].Answered {
		t.Error("expected question to be unanswered")
	}
}

func TestPostAnswerToQuestion(t *testing.T) {
	// Create a pipeline with a handler that asks a question
	questionAsked := make(chan struct{})
	reg := buildServerTestRegistry(
		&serverTestHandler{typeName: "start"},
		&serverTestHandler{
			typeName: "codergen",
			executeFn: func(ctx context.Context, node *Node, pctx *Context, store *ArtifactStore) (*Outcome, error) {
				interviewer := pctx.Get("_interviewer")
				if interviewer == nil {
					return &Outcome{Status: StatusSuccess}, nil
				}
				iv, ok := interviewer.(Interviewer)
				if !ok {
					return &Outcome{Status: StatusSuccess}, nil
				}

				close(questionAsked)
				answer, err := iv.Ask(ctx, "Pick a number", []string{"1", "2", "3"})
				if err != nil {
					return &Outcome{
						Status:        StatusFail,
						FailureReason: err.Error(),
					}, nil
				}
				return &Outcome{
					Status:         StatusSuccess,
					ContextUpdates: map[string]any{"picked": answer},
				}, nil
			},
		},
		&serverTestHandler{typeName: "exit"},
	)
	engine := NewEngine(EngineConfig{
		Backend:      &fakeBackend{},
		Handlers:     reg,
		DefaultRetry: RetryPolicyNone(),
	})

	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Wait for the question
	select {
	case <-questionAsked:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for question")
	}

	time.Sleep(100 * time.Millisecond)

	// Get the question ID
	resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID + "/questions")
	if err != nil {
		t.Fatalf("GET questions failed: %v", err)
	}
	var questions []PendingQuestion
	json.NewDecoder(resp.Body).Decode(&questions)
	resp.Body.Close()

	if len(questions) == 0 {
		t.Fatal("expected at least 1 question")
	}
	qid := questions[0].ID

	// Answer the question
	answerBody := `{"answer": "2"}`
	resp, err = http.Post(ts.URL+"/pipelines/"+submitResult.ID+"/questions/"+qid+"/answer", "application/json", strings.NewReader(answerBody))
	if err != nil {
		t.Fatalf("POST answer failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Wait for pipeline to complete
	deadline := time.Now().Add(5 * time.Second)
	var status PipelineStatus
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID)
		if err != nil {
			t.Fatalf("GET status failed: %v", err)
		}
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if status.Status != "completed" {
		t.Fatalf("expected 'completed', got %q (error: %s)", status.Status, status.Error)
	}

	// Verify the answer was used
	resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID + "/context")
	if err != nil {
		t.Fatalf("GET context failed: %v", err)
	}
	var ctxSnap map[string]any
	json.NewDecoder(resp.Body).Decode(&ctxSnap)
	resp.Body.Close()

	if ctxSnap["picked"] != "2" {
		t.Errorf("expected picked='2', got %v", ctxSnap["picked"])
	}
}

func TestCancelNonexistentPipeline(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/pipelines/nonexistent/cancel", "", nil)
	if err != nil {
		t.Fatalf("POST cancel failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// --- Event Query Endpoint Tests ---

// setupServerWithEvents creates a PipelineServer with an EventQuery backed by FSRunStateStore,
// pre-populated with known events. Returns the test server, pipeline ID, and the events.
func setupServerWithEvents(t *testing.T) (*httptest.Server, string, []EngineEvent) {
	t.Helper()

	store := newTestStore(t)
	state := newTestRunState(t)

	if err := store.Create(state); err != nil {
		t.Fatalf("Create run state failed: %v", err)
	}

	baseTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)

	events := []EngineEvent{
		{Type: EventPipelineStarted, NodeID: "", Data: map[string]any{"pipeline": "test"}, Timestamp: baseTime},
		{Type: EventStageStarted, NodeID: "node_a", Data: map[string]any{"step": 1}, Timestamp: baseTime.Add(1 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_a", Data: map[string]any{"step": 1}, Timestamp: baseTime.Add(2 * time.Minute)},
		{Type: EventStageStarted, NodeID: "node_b", Data: map[string]any{"step": 2}, Timestamp: baseTime.Add(3 * time.Minute)},
		{Type: EventStageRetrying, NodeID: "node_b", Data: map[string]any{"attempt": 2}, Timestamp: baseTime.Add(4 * time.Minute)},
		{Type: EventStageCompleted, NodeID: "node_b", Data: map[string]any{"step": 2}, Timestamp: baseTime.Add(5 * time.Minute)},
		{Type: EventCheckpointSaved, NodeID: "node_b", Data: nil, Timestamp: baseTime.Add(6 * time.Minute)},
		{Type: EventPipelineCompleted, NodeID: "", Data: nil, Timestamp: baseTime.Add(7 * time.Minute)},
	}

	for _, evt := range events {
		if err := store.AddEvent(state.ID, evt); err != nil {
			t.Fatalf("AddEvent failed: %v", err)
		}
	}

	eventQuery := NewFSEventQuery(store)

	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.SetEventQuery(eventQuery)

	// Register the pipeline in the server so it recognizes the ID
	run := &PipelineRun{
		ID:        state.ID,
		Status:    "completed",
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines[state.ID] = run
	srv.mu.Unlock()

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return ts, state.ID, events
}

func TestServerEventQueryNoFilter(t *testing.T) {
	ts, pipelineID, events := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/query")
	if err != nil {
		t.Fatalf("GET /events/query failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var result EventQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Events) != len(events) {
		t.Errorf("expected %d events, got %d", len(events), len(result.Events))
	}
	if result.Total != len(events) {
		t.Errorf("expected total %d, got %d", len(events), result.Total)
	}
}

func TestServerEventQueryFilterByType(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/query?type=stage.started")
	if err != nil {
		t.Fatalf("GET /events/query?type failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result EventQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Events) != 2 {
		t.Errorf("expected 2 stage.started events, got %d", len(result.Events))
	}
	for _, evt := range result.Events {
		if evt.Type != EventStageStarted {
			t.Errorf("expected type %q, got %q", EventStageStarted, evt.Type)
		}
	}
}

func TestServerEventQueryFilterByNode(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/query?node=node_b")
	if err != nil {
		t.Fatalf("GET /events/query?node failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result EventQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Events) != 4 {
		t.Errorf("expected 4 events for node_b, got %d", len(result.Events))
	}
	for _, evt := range result.Events {
		if evt.NodeID != "node_b" {
			t.Errorf("expected NodeID 'node_b', got %q", evt.NodeID)
		}
	}
}

func TestServerEventQueryFilterByTimeRange(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	since := "2025-06-15T10:02:00Z"
	until := "2025-06-15T10:05:00Z"

	url := fmt.Sprintf("%s/pipelines/%s/events/query?since=%s&until=%s", ts.URL, pipelineID, since, until)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /events/query?since&until failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result EventQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Events at 2min, 3min, 4min, 5min = 4 events
	if len(result.Events) != 4 {
		t.Errorf("expected 4 events in time range, got %d", len(result.Events))
	}
}

func TestServerEventQueryPagination(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/query?limit=3&offset=2")
	if err != nil {
		t.Fatalf("GET /events/query?limit&offset failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result EventQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Events) != 3 {
		t.Errorf("expected 3 events with limit=3 offset=2, got %d", len(result.Events))
	}
	// Total should reflect all matching events, not the paginated count
	if result.Total != 8 {
		t.Errorf("expected total 8, got %d", result.Total)
	}
}

func TestServerEventQueryInvalidSince(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/query?since=not-a-date")
	if err != nil {
		t.Fatalf("GET /events/query?since=invalid failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid since, got %d", resp.StatusCode)
	}
}

func TestServerEventQueryInvalidUntil(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/query?until=not-a-date")
	if err != nil {
		t.Fatalf("GET /events/query?until=invalid failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid until, got %d", resp.StatusCode)
	}
}

func TestServerEventQuery404(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pipelines/nonexistent/events/query")
	if err != nil {
		t.Fatalf("GET /events/query failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServerEventQueryNoEventQuery(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Register a pipeline but don't set an EventQuery
	run := &PipelineRun{
		ID:        "test-run",
		Status:    "completed",
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["test-run"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/test-run/events/query")
	if err != nil {
		t.Fatalf("GET /events/query failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no event query configured, got %d", resp.StatusCode)
	}
}

// --- Event Tail Endpoint Tests ---

func TestServerEventTailDefault(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/tail")
	if err != nil {
		t.Fatalf("GET /events/tail failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result EventTailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Default n=10, but only 8 events exist
	if len(result.Events) != 8 {
		t.Errorf("expected 8 events (all, since fewer than default 10), got %d", len(result.Events))
	}
}

func TestServerEventTailWithN(t *testing.T) {
	ts, pipelineID, events := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/tail?n=3")
	if err != nil {
		t.Fatalf("GET /events/tail?n=3 failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result EventTailResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Events) != 3 {
		t.Fatalf("expected 3 tail events, got %d", len(result.Events))
	}

	// Should be the last 3 events
	if result.Events[0].Type != events[5].Type {
		t.Errorf("expected tail[0] type %q, got %q", events[5].Type, result.Events[0].Type)
	}
	if result.Events[2].Type != events[7].Type {
		t.Errorf("expected tail[2] type %q, got %q", events[7].Type, result.Events[2].Type)
	}
}

func TestServerEventTailInvalidN(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/tail?n=abc")
	if err != nil {
		t.Fatalf("GET /events/tail?n=abc failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid n, got %d", resp.StatusCode)
	}
}

func TestServerEventTail404(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pipelines/nonexistent/events/tail")
	if err != nil {
		t.Fatalf("GET /events/tail failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServerEventTailNoEventQuery(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "test-run",
		Status:    "completed",
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["test-run"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/test-run/events/tail")
	if err != nil {
		t.Fatalf("GET /events/tail failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no event query configured, got %d", resp.StatusCode)
	}
}

// --- Event Summary Endpoint Tests ---

func TestServerEventSummary(t *testing.T) {
	ts, pipelineID, _ := setupServerWithEvents(t)

	resp, err := http.Get(ts.URL + "/pipelines/" + pipelineID + "/events/summary")
	if err != nil {
		t.Fatalf("GET /events/summary failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result EventSummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result.TotalEvents != 8 {
		t.Errorf("expected TotalEvents=8, got %d", result.TotalEvents)
	}

	// Check ByType
	if result.ByType["stage.started"] != 2 {
		t.Errorf("expected ByType[stage.started]=2, got %d", result.ByType["stage.started"])
	}
	if result.ByType["stage.completed"] != 2 {
		t.Errorf("expected ByType[stage.completed]=2, got %d", result.ByType["stage.completed"])
	}
	if result.ByType["pipeline.started"] != 1 {
		t.Errorf("expected ByType[pipeline.started]=1, got %d", result.ByType["pipeline.started"])
	}

	// Check ByNode
	if result.ByNode["node_a"] != 2 {
		t.Errorf("expected ByNode[node_a]=2, got %d", result.ByNode["node_a"])
	}
	if result.ByNode["node_b"] != 4 {
		t.Errorf("expected ByNode[node_b]=4, got %d", result.ByNode["node_b"])
	}

	// Check timestamps
	if result.FirstEvent == "" {
		t.Error("expected non-empty FirstEvent timestamp")
	}
	if result.LastEvent == "" {
		t.Error("expected non-empty LastEvent timestamp")
	}
}

func TestServerEventSummary404(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pipelines/nonexistent/events/summary")
	if err != nil {
		t.Fatalf("GET /events/summary failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServerEventSummaryNoEventQuery(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "test-run",
		Status:    "completed",
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["test-run"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/test-run/events/summary")
	if err != nil {
		t.Fatalf("GET /events/summary failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when no event query configured, got %d", resp.StatusCode)
	}
}

// --- Graph Rendering Endpoint Tests ---

// stubToDOT is a minimal DOT serializer for testing that produces a simple digraph string.
func stubToDOT(g *Graph) string {
	return fmt.Sprintf("digraph %s { /* stub */ }", g.Name)
}

// stubToDOTWithStatus adds a status comment to the DOT output for testing.
func stubToDOTWithStatus(g *Graph, outcomes map[string]*Outcome) string {
	return fmt.Sprintf("digraph %s { /* status */ }", g.Name)
}

// stubRenderDOTSource returns fake SVG or PNG output for testing the HTTP endpoint.
func stubRenderDOTSource(_ context.Context, dotText string, format string) ([]byte, error) {
	switch format {
	case "svg":
		return []byte("<svg><text>test</text></svg>"), nil
	case "png":
		// PNG signature + minimal data
		return []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00}, nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

func newGraphTestServer() *PipelineServer {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.ToDOT = stubToDOT
	srv.ToDOTWithStatus = stubToDOTWithStatus
	srv.RenderDOTSource = stubRenderDOTSource
	return srv
}

func TestGetPipelineGraphDOTFormat(t *testing.T) {
	srv := newGraphTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "graph-test",
		Status:    "running",
		Source:    simpleDOTSource(),
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["graph-test"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/graph-test/graph?format=dot")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/graph?format=dot failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	if resp.Header.Get("Content-Type") != "text/vnd.graphviz" {
		t.Errorf("expected Content-Type 'text/vnd.graphviz', got %q", resp.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(resp.Body)
	dot := string(body)
	if !strings.Contains(dot, "digraph") {
		t.Errorf("expected DOT output containing 'digraph', got:\n%s", dot)
	}
}

func TestGetPipelineGraph404(t *testing.T) {
	srv := newGraphTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pipelines/nonexistent/graph")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/graph failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetPipelineGraphInvalidFormat(t *testing.T) {
	srv := newGraphTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "graph-test",
		Status:    "running",
		Source:    simpleDOTSource(),
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["graph-test"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/graph-test/graph?format=gif")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/graph?format=gif failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported format, got %d", resp.StatusCode)
	}
}

func TestGetPipelineGraphWithCompletedStatus(t *testing.T) {
	srv := newGraphTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:     "graph-status-test",
		Status: "completed",
		Source: simpleDOTSource(),
		Result: &RunResult{
			NodeOutcomes: map[string]*Outcome{
				"start": {Status: StatusSuccess},
				"step":  {Status: StatusSuccess},
				"done":  {Status: StatusSuccess},
			},
			CompletedNodes: []string{"start", "step", "done"},
		},
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["graph-status-test"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/graph-status-test/graph?format=dot")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/graph?format=dot failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	dot := string(body)

	// Should use the status overlay function since pipeline has outcomes
	if !strings.Contains(dot, "status") {
		t.Errorf("expected status-overlaid DOT output, got:\n%s", dot)
	}
}

func TestGetPipelineGraphDefaultFormatIsSVG(t *testing.T) {
	srv := newGraphTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "graph-default-test",
		Status:    "running",
		Source:    simpleDOTSource(),
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["graph-default-test"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/graph-default-test/graph")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/graph failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "image/svg+xml" {
		t.Errorf("expected Content-Type 'image/svg+xml', got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<svg") {
		t.Errorf("expected SVG content, got:\n%s", string(body))
	}
}

func TestGetPipelineGraphPNGFormat(t *testing.T) {
	srv := newGraphTestServer()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "graph-png-test",
		Status:    "running",
		Source:    simpleDOTSource(),
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["graph-png-test"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/graph-png-test/graph?format=png")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/graph?format=png failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	if resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("expected Content-Type 'image/png', got %q", resp.Header.Get("Content-Type"))
	}
}

func TestGetPipelineGraphNoRendererFallback(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	// Set ToDOT but not RenderDOTSource - SVG should fallback to DOT text
	srv.ToDOT = stubToDOT
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "graph-fallback-test",
		Status:    "running",
		Source:    simpleDOTSource(),
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["graph-fallback-test"] = run
	srv.mu.Unlock()

	// Request SVG with no renderer configured - should fallback to DOT
	resp, err := http.Get(ts.URL + "/pipelines/graph-fallback-test/graph?format=svg")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/graph?format=svg failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Should fallback to DOT content type
	if resp.Header.Get("Content-Type") != "text/vnd.graphviz" {
		t.Errorf("expected Content-Type 'text/vnd.graphviz', got %q", resp.Header.Get("Content-Type"))
	}
}

func TestGetPipelineGraphNoFuncsConfigured(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	// No render functions configured
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "graph-nofunc-test",
		Status:    "running",
		Source:    simpleDOTSource(),
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["graph-nofunc-test"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/pipelines/graph-nofunc-test/graph?format=dot")
	if err != nil {
		t.Fatalf("GET /pipelines/{id}/graph?format=dot failed: %v", err)
	}
	defer resp.Body.Close()

	// With no render functions, the server falls back to the original source
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "digraph") {
		t.Errorf("expected DOT content with digraph, got:\n%s", string(body))
	}
}

// --- List Pipelines Endpoint Tests ---

func TestGetPipelinesListReturnsEmptyArray(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pipelines")
	if err != nil {
		t.Fatalf("GET /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []PipelineStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

func TestGetPipelinesListReturnsPipelines(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit a pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID)
		if err != nil {
			break
		}
		var status PipelineStatus
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// GET /pipelines should return the submitted pipeline
	resp, err = http.Get(ts.URL + "/pipelines")
	if err != nil {
		t.Fatalf("GET /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result []PipelineStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(result))
	}
	if result[0].ID != submitResult.ID {
		t.Errorf("expected ID %q, got %q", submitResult.ID, result[0].ID)
	}
}

func TestGetPipelinesListSortedByCreatedAt(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)

	// Manually insert runs with known timestamps
	now := time.Now()
	runs := []*PipelineRun{
		{ID: "run-c", Status: "completed", CreatedAt: now.Add(-1 * time.Hour)},
		{ID: "run-a", Status: "completed", CreatedAt: now.Add(-3 * time.Hour)},
		{ID: "run-b", Status: "completed", CreatedAt: now.Add(-2 * time.Hour)},
	}

	srv.mu.Lock()
	for _, r := range runs {
		srv.pipelines[r.ID] = r
	}
	srv.mu.Unlock()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/pipelines")
	if err != nil {
		t.Fatalf("GET /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	var result []PipelineStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 pipelines, got %d", len(result))
	}

	// Should be sorted by CreatedAt descending (most recent first)
	if result[0].ID != "run-c" {
		t.Errorf("expected first pipeline to be 'run-c' (most recent), got %q", result[0].ID)
	}
	if result[2].ID != "run-a" {
		t.Errorf("expected last pipeline to be 'run-a' (oldest), got %q", result[2].ID)
	}
}

// --- Server Persistence Tests ---

func TestServerSetRunStateStore(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)

	dir := t.TempDir()
	store, err := NewFSRunStateStore(filepath.Join(dir, "runs"))
	if err != nil {
		t.Fatalf("NewFSRunStateStore failed: %v", err)
	}

	srv.SetRunStateStore(store)

	// Verify the store was set (by checking it doesn't panic on submit)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	// Wait for pipeline to complete so the persist goroutine finishes before cleanup
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID)
		if err != nil {
			break
		}
		var status PipelineStatus
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Allow persist goroutine to flush
	time.Sleep(50 * time.Millisecond)
}

func TestServerPersistsRunOnSubmit(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)

	dir := t.TempDir()
	store, err := NewFSRunStateStore(filepath.Join(dir, "runs"))
	if err != nil {
		t.Fatalf("NewFSRunStateStore failed: %v", err)
	}
	srv.SetRunStateStore(store)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit a pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID)
		if err != nil {
			break
		}
		var status PipelineStatus
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify run was persisted to disk
	persisted, err := store.Get(submitResult.ID)
	if err != nil {
		t.Fatalf("store.Get failed: %v", err)
	}
	if persisted.ID != submitResult.ID {
		t.Errorf("persisted ID mismatch: got %q, want %q", persisted.ID, submitResult.ID)
	}
	if persisted.Source != simpleDOTSource() {
		t.Error("expected source to be persisted")
	}
}

func TestServerPersistsFinalStatus(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)

	dir := t.TempDir()
	store, err := NewFSRunStateStore(filepath.Join(dir, "runs"))
	if err != nil {
		t.Fatalf("NewFSRunStateStore failed: %v", err)
	}
	srv.SetRunStateStore(store)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)
	resp.Body.Close()

	// Wait for completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID)
		if err != nil {
			break
		}
		var status PipelineStatus
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Small delay for the final persist to flush
	time.Sleep(100 * time.Millisecond)

	persisted, err := store.Get(submitResult.ID)
	if err != nil {
		t.Fatalf("store.Get failed: %v", err)
	}
	if persisted.Status != "completed" {
		t.Errorf("expected persisted status 'completed', got %q", persisted.Status)
	}
}

func TestServerLoadPersistedRuns(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSRunStateStore(filepath.Join(dir, "runs"))
	if err != nil {
		t.Fatalf("NewFSRunStateStore failed: %v", err)
	}

	// Pre-populate the store with a completed run
	runState := &RunState{
		ID:             "persisted-run-1",
		PipelineFile:   "test.dot",
		Status:         "completed",
		Source:         simpleDOTSource(),
		StartedAt:      time.Now().Add(-1 * time.Hour),
		CurrentNode:    "done",
		CompletedNodes: []string{"start", "step", "done"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
	}
	if err := store.Create(runState); err != nil {
		t.Fatalf("store.Create failed: %v", err)
	}

	// Create a fresh server and load persisted runs
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.SetRunStateStore(store)
	if err := srv.LoadPersistedRuns(); err != nil {
		t.Fatalf("LoadPersistedRuns failed: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// The pre-populated run should be visible in the list
	resp, err := http.Get(ts.URL + "/pipelines")
	if err != nil {
		t.Fatalf("GET /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	var result []PipelineStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(result))
	}
	if result[0].ID != "persisted-run-1" {
		t.Errorf("expected ID 'persisted-run-1', got %q", result[0].ID)
	}
	if result[0].Status != "completed" {
		t.Errorf("expected status 'completed', got %q", result[0].Status)
	}
}

func TestServerLoadPersistedRunGetByID(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFSRunStateStore(filepath.Join(dir, "runs"))
	if err != nil {
		t.Fatalf("NewFSRunStateStore failed: %v", err)
	}

	// Pre-populate with a run that has source
	runState := &RunState{
		ID:             "old-run",
		PipelineFile:   "old.dot",
		Status:         "completed",
		Source:         simpleDOTSource(),
		StartedAt:      time.Now().Add(-2 * time.Hour),
		CurrentNode:    "done",
		CompletedNodes: []string{"start", "step", "done"},
		Context:        map[string]any{},
		Events:         []EngineEvent{},
	}
	if err := store.Create(runState); err != nil {
		t.Fatalf("store.Create failed: %v", err)
	}

	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.SetRunStateStore(store)
	if err := srv.LoadPersistedRuns(); err != nil {
		t.Fatalf("LoadPersistedRuns failed: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Should be able to GET the persisted run by ID
	resp, err := http.Get(ts.URL + "/pipelines/old-run")
	if err != nil {
		t.Fatalf("GET /pipelines/old-run failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var status PipelineStatus
	json.NewDecoder(resp.Body).Decode(&status)
	if status.ID != "old-run" {
		t.Errorf("expected ID 'old-run', got %q", status.ID)
	}
}

func TestPipelineCreatesRunDirWithPipelineID(t *testing.T) {
	artifactsBase := t.TempDir()

	reg := buildServerTestRegistry(
		&serverTestHandler{typeName: "start"},
		&serverTestHandler{typeName: "codergen"},
		&serverTestHandler{typeName: "exit"},
	)
	engine := NewEngine(EngineConfig{
		Backend:          &fakeBackend{},
		Handlers:         reg,
		DefaultRetry:     RetryPolicyNone(),
		ArtifactsBaseDir: artifactsBase,
	})

	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit a pipeline
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	var result struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()

	if result.ID == "" {
		t.Fatal("expected non-empty pipeline ID")
	}

	// Wait for pipeline to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + result.ID)
		if err != nil {
			break
		}
		var status PipelineStatus
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()
		if status.Status == "completed" || status.Status == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify the run directory was created under artifactsBase/<pipelineID>
	runDir := filepath.Join(artifactsBase, result.ID)
	info, err := os.Stat(runDir)
	if err != nil {
		t.Fatalf("expected run directory %q to exist: %v", runDir, err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", runDir)
	}

	// Verify nodes subdirectory exists
	nodesDir := filepath.Join(runDir, "nodes")
	info, err = os.Stat(nodesDir)
	if err != nil {
		t.Fatalf("expected nodes dir %q to exist: %v", nodesDir, err)
	}
	if !info.IsDir() {
		t.Fatal("expected nodes dir to be a directory")
	}

	// Verify the PipelineRun has the artifact dir set
	srv.mu.RLock()
	run := srv.pipelines[result.ID]
	srv.mu.RUnlock()
	if run.ArtifactDir != runDir {
		t.Errorf("expected run.ArtifactDir=%q, got %q", runDir, run.ArtifactDir)
	}
}
