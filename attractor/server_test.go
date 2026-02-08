// ABOUTME: Tests for the HTTP pipeline server covering all REST endpoints.
// ABOUTME: Validates pipeline submission, status queries, SSE streaming, cancellation, questions, and context retrieval.
package attractor

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

	// Post invalid DOT source
	resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader("this is not valid DOT"))
	if err != nil {
		t.Fatalf("POST /pipelines failed: %v", err)
	}
	defer resp.Body.Close()

	// The server should still accept it (returns 202) -- the error shows up in status
	// OR it could return a 400 if it validates eagerly. Let's check.
	// Since Run() is async, the server accepts and starts; error appears in status.
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted (async execution), got %d", resp.StatusCode)
	}

	var submitResult struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&submitResult)

	// Wait for pipeline to fail
	deadline := time.Now().Add(5 * time.Second)
	var status PipelineStatus
	for time.Now().Before(deadline) {
		resp, err = http.Get(ts.URL + "/pipelines/" + submitResult.ID)
		if err != nil {
			t.Fatalf("GET /pipelines/{id} failed: %v", err)
		}
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()

		if status.Status == "failed" || status.Status == "completed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if status.Status != "failed" {
		t.Errorf("expected status 'failed' for invalid DOT, got %q", status.Status)
	}
	if status.Error == "" {
		t.Error("expected error message for invalid DOT pipeline")
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
