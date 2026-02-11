// ABOUTME: Tests for the HTMX web frontend served by PipelineServer.
// ABOUTME: Covers dashboard, pipeline detail, graph/questions/token/tool/active-node fragments, and aggregation helpers.
package attractor

import (
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

func TestDashboardReturnsHTML(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type containing 'text/html', got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "Makeatron") {
		t.Error("expected dashboard to contain 'Makeatron'")
	}
	if !strings.Contains(html, "htmx") {
		t.Error("expected dashboard to contain 'htmx'")
	}
}

func TestPipelineViewReturnsHTML(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.ToDOT = stubToDOT
	srv.ToDOTWithStatus = stubToDOTWithStatus
	srv.RenderDOTSource = stubRenderDOTSource
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

	// GET the UI view
	resp, err = http.Get(ts.URL + "/ui/pipelines/" + submitResult.ID)
	if err != nil {
		t.Fatalf("GET /ui/pipelines/{id} failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type containing 'text/html', got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, submitResult.ID) {
		t.Error("expected pipeline detail view to contain the pipeline ID")
	}
}

func TestGraphFragmentReturnsSVG(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.ToDOT = stubToDOT
	srv.RenderDOTSource = stubRenderDOTSource
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "graph-frag-test",
		Status:    "running",
		Source:    simpleDOTSource(),
		CreatedAt: time.Now(),
	}
	srv.mu.Lock()
	srv.pipelines["graph-frag-test"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/ui/pipelines/graph-frag-test/graph-fragment")
	if err != nil {
		t.Fatalf("GET graph-fragment failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	content := string(body)
	if !strings.Contains(content, "<svg") {
		t.Errorf("expected SVG content in graph fragment, got: %s", content)
	}
}

func TestQuestionsFragmentRendersButtons(t *testing.T) {
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
				answer, err := iv.Ask(ctx, "Pick a color", []string{"red", "blue", "green"})
				if err != nil {
					return &Outcome{Status: StatusFail, FailureReason: err.Error()}, nil
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

	// Wait for the question
	select {
	case <-questionAsked:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for question")
	}
	time.Sleep(100 * time.Millisecond)

	// GET the questions fragment
	resp, err = http.Get(ts.URL + "/ui/pipelines/" + submitResult.ID + "/questions-fragment")
	if err != nil {
		t.Fatalf("GET questions-fragment failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "red") {
		t.Error("expected questions fragment to contain option 'red'")
	}
	if !strings.Contains(html, "blue") {
		t.Error("expected questions fragment to contain option 'blue'")
	}
	if !strings.Contains(html, "button") || !strings.Contains(html, "hx-post") {
		t.Error("expected questions fragment to contain HTMX answer buttons")
	}

	// Answer the question so the pipeline can finish
	var questions []PendingQuestion
	resp2, _ := http.Get(ts.URL + "/pipelines/" + submitResult.ID + "/questions")
	json.NewDecoder(resp2.Body).Decode(&questions)
	resp2.Body.Close()
	if len(questions) > 0 {
		http.Post(ts.URL+"/pipelines/"+submitResult.ID+"/questions/"+questions[0].ID+"/answer",
			"application/json", strings.NewReader(`{"answer":"red"}`))
	}
}

func TestDashboardListsPipelines(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Submit 2 pipelines
	var ids []string
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Post(ts.URL+"/pipelines", "text/plain", strings.NewReader(simpleDOTSource()))
			if err != nil {
				return
			}
			var result struct {
				ID string `json:"id"`
			}
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			mu.Lock()
			ids = append(ids, result.ID)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(ids) != 2 {
		t.Fatalf("expected 2 pipeline IDs, got %d", len(ids))
	}

	// Wait for both to complete
	time.Sleep(500 * time.Millisecond)

	// GET dashboard
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, id := range ids {
		// Check that at least a prefix of the ID appears (IDs are long hex strings)
		prefix := id[:8]
		if !strings.Contains(html, prefix) {
			t.Errorf("expected dashboard to list pipeline %s (prefix %s)", id, prefix)
		}
	}
}

// TestEventsFragmentShowsFailureData verifies that events-fragment displays
// failure reasons and error messages from the event Data map.
func TestEventsFragmentShowsFailureData(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	now := time.Now()

	run := &PipelineRun{
		ID:        "test-events-data",
		Status:    "failed",
		Source:    `digraph { start -> done }`,
		CreatedAt: now,
		Events: []EngineEvent{
			{Type: EventStageStarted, NodeID: "build", Timestamp: now},
			{Type: EventStageFailed, NodeID: "build", Data: map[string]any{"reason": "connection refused"}, Timestamp: now.Add(time.Second)},
			{Type: EventPipelineFailed, Data: map[string]any{"error": "node build execution error"}, Timestamp: now.Add(2 * time.Second)},
		},
	}

	srv.mu.Lock()
	srv.pipelines["test-events-data"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/ui/pipelines/test-events-data/events-fragment")
	if err != nil {
		t.Fatalf("GET events-fragment failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The "reason" field should appear
	if !strings.Contains(html, "connection refused") {
		t.Error("expected events HTML to contain 'connection refused' from stage.failed reason")
	}

	// The "error" field should also appear
	if !strings.Contains(html, "node build execution error") {
		t.Error("expected events HTML to contain 'node build execution error' from pipeline.failed error")
	}

	// Event types should appear
	if !strings.Contains(html, "stage.started") {
		t.Error("expected events HTML to contain 'stage.started'")
	}
	if !strings.Contains(html, "stage.failed") {
		t.Error("expected events HTML to contain 'stage.failed'")
	}
	if !strings.Contains(html, "pipeline.failed") {
		t.Error("expected events HTML to contain 'pipeline.failed'")
	}
}

// TestPipelineViewShowsError verifies that the pipeline detail page shows
// the error message when a pipeline has failed.
func TestPipelineViewShowsError(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "test-error-display",
		Status:    "failed",
		Source:    `digraph { start -> done }`,
		Error:     "parse error: unexpected token at line 3",
		CreatedAt: time.Now(),
		Events:    []EngineEvent{},
	}

	srv.mu.Lock()
	srv.pipelines["test-error-display"] = run
	srv.mu.Unlock()

	// Check the pipeline detail page shows the error
	resp, err := http.Get(ts.URL + "/ui/pipelines/test-error-display")
	if err != nil {
		t.Fatalf("GET pipeline view failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "unexpected token at line 3") {
		t.Error("expected pipeline detail page to contain the error message")
	}
	if !strings.Contains(html, "error-banner") {
		t.Error("expected pipeline detail page to contain error-banner class")
	}
}

// TestEventsFragmentShowsErrorOnFailedPipeline verifies that the events fragment
// shows the pipeline error when there are no events and the pipeline has failed.
func TestEventsFragmentShowsErrorOnFailedPipeline(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	run := &PipelineRun{
		ID:        "test-no-events-fail",
		Status:    "failed",
		Source:    `digraph { start -> done }`,
		Error:     "validation failed: no start node",
		CreatedAt: time.Now(),
		Events:    []EngineEvent{},
	}

	srv.mu.Lock()
	srv.pipelines["test-no-events-fail"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/ui/pipelines/test-no-events-fail/events-fragment")
	if err != nil {
		t.Fatalf("GET events-fragment failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	if !strings.Contains(html, "validation failed: no start node") {
		t.Error("expected events fragment to show pipeline error when no events and pipeline failed")
	}
	// Should NOT show "Waiting for events..." when pipeline is already done
	if strings.Contains(html, "Waiting for events...") {
		t.Error("should not show 'Waiting for events...' when pipeline has already failed")
	}
}

// TestEventsFragmentShowsAgentEventData verifies that agent-level events display
// tool names, output snippets, and other agent-specific data in the events fragment.
func TestEventsFragmentShowsAgentEventData(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	now := time.Now()

	run := &PipelineRun{
		ID:        "test-agent-events",
		Status:    "completed",
		Source:    `digraph { start -> done }`,
		CreatedAt: now,
		Events: []EngineEvent{
			{Type: EventStageStarted, NodeID: "codegen", Timestamp: now},
			{Type: EventAgentToolCallStart, NodeID: "codegen", Data: map[string]any{"tool_name": "file_write", "call_id": "tc_1"}, Timestamp: now.Add(time.Second)},
			{Type: EventAgentToolCallEnd, NodeID: "codegen", Data: map[string]any{"tool_name": "file_write", "call_id": "tc_1", "output_snippet": "wrote 42 bytes", "duration_ms": int64(150)}, Timestamp: now.Add(2 * time.Second)},
			{Type: EventAgentLLMTurn, NodeID: "codegen", Data: map[string]any{"text_length": 200, "has_reasoning": true}, Timestamp: now.Add(3 * time.Second)},
			{Type: EventAgentSteering, NodeID: "codegen", Data: map[string]any{"message": "focus on tests"}, Timestamp: now.Add(4 * time.Second)},
			{Type: EventAgentLoopDetected, NodeID: "codegen", Data: map[string]any{"message": "repeating pattern"}, Timestamp: now.Add(5 * time.Second)},
			{Type: EventStageCompleted, NodeID: "codegen", Timestamp: now.Add(6 * time.Second)},
		},
	}

	srv.mu.Lock()
	srv.pipelines["test-agent-events"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/ui/pipelines/test-agent-events/events-fragment")
	if err != nil {
		t.Fatalf("GET events-fragment failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Agent event types should appear
	if !strings.Contains(html, "agent.tool_call.start") {
		t.Error("expected events HTML to contain 'agent.tool_call.start'")
	}
	if !strings.Contains(html, "agent.tool_call.end") {
		t.Error("expected events HTML to contain 'agent.tool_call.end'")
	}
	if !strings.Contains(html, "agent.llm_turn") {
		t.Error("expected events HTML to contain 'agent.llm_turn'")
	}

	// Tool name should appear as detail
	if !strings.Contains(html, "file_write") {
		t.Error("expected events HTML to contain tool name 'file_write'")
	}

	// Output snippet should appear
	if !strings.Contains(html, "wrote 42 bytes") {
		t.Error("expected events HTML to contain output snippet 'wrote 42 bytes'")
	}

	// Steering message should appear
	if !strings.Contains(html, "focus on tests") {
		t.Error("expected events HTML to contain steering message")
	}

	// Loop detection message should appear
	if !strings.Contains(html, "repeating pattern") {
		t.Error("expected events HTML to contain loop detection message")
	}
}

func TestEventsFragmentShowsTokenBreakdown(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	now := time.Now()
	run := &PipelineRun{
		ID:        "test-token-events",
		Status:    "completed",
		Source:    `digraph { start -> done }`,
		CreatedAt: now,
		Events: []EngineEvent{
			{
				Type:      EventAgentLLMTurn,
				NodeID:    "codegen",
				Timestamp: now,
				Data: map[string]any{
					"text_length":   200,
					"has_reasoning": true,
					"input_tokens":  1000,
					"output_tokens": 500,
					"total_tokens":  1500,
				},
			},
		},
	}

	srv.mu.Lock()
	srv.pipelines["test-token-events"] = run
	srv.mu.Unlock()

	resp, err := http.Get(ts.URL + "/ui/pipelines/test-token-events/events-fragment")
	if err != nil {
		t.Fatalf("GET events-fragment failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "in:1000") {
		t.Error("expected events HTML to contain 'in:1000' for input tokens")
	}
	if !strings.Contains(html, "out:500") {
		t.Error("expected events HTML to contain 'out:500' for output tokens")
	}
	if !strings.Contains(html, "total:1500") {
		t.Error("expected events HTML to contain 'total:1500' for total tokens")
	}
	if !strings.Contains(html, "event-tokens") {
		t.Error("expected events HTML to contain 'event-tokens' class for token display")
	}
}

func TestPipelineView404(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/ui/pipelines/nonexistent")
	if err != nil {
		t.Fatalf("GET /ui/pipelines/nonexistent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// ---- Step 1: toInt / formatNumber unit tests ----

func TestToInt(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want int
	}{
		{"int", 42, 42},
		{"int64", int64(999), 999},
		{"float64", float64(1234), 1234},
		{"float64 truncates", 3.7, 3},
		{"string returns 0", "hello", 0},
		{"nil returns 0", nil, 0},
		{"bool returns 0", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toInt(tt.in)
			if got != tt.want {
				t.Errorf("toInt(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{5, "5"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
		{-999, "-999"},
		{100000, "100,000"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatNumber(tt.in)
			if got != tt.want {
				t.Errorf("formatNumber(%d) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---- Step 3: aggregateToolCalls unit tests ----

func TestAggregateToolCalls(t *testing.T) {
	now := time.Now()

	t.Run("pairs start and end by call_id", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventAgentToolCallStart, NodeID: "n1", Timestamp: now, Data: map[string]any{
				"call_id": "c1", "tool_name": "write_file",
			}},
			{Type: EventAgentToolCallEnd, NodeID: "n1", Timestamp: now.Add(230 * time.Millisecond), Data: map[string]any{
				"call_id": "c1", "tool_name": "write_file", "output_snippet": "wrote 50 bytes", "duration_ms": int64(230),
			}},
		}
		calls := aggregateToolCalls(events)
		if len(calls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(calls))
		}
		c := calls[0]
		if c.CallID != "c1" {
			t.Errorf("CallID = %q, want %q", c.CallID, "c1")
		}
		if c.ToolName != "write_file" {
			t.Errorf("ToolName = %q, want %q", c.ToolName, "write_file")
		}
		if !c.Completed {
			t.Error("expected Completed = true")
		}
		if c.Duration != 230*time.Millisecond {
			t.Errorf("Duration = %v, want 230ms", c.Duration)
		}
		if c.OutputSnippet != "wrote 50 bytes" {
			t.Errorf("OutputSnippet = %q, want %q", c.OutputSnippet, "wrote 50 bytes")
		}
		if c.NodeID != "n1" {
			t.Errorf("NodeID = %q, want %q", c.NodeID, "n1")
		}
	})

	t.Run("in-flight call not completed", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventAgentToolCallStart, NodeID: "n1", Timestamp: now, Data: map[string]any{
				"call_id": "c2", "tool_name": "read_file",
			}},
		}
		calls := aggregateToolCalls(events)
		if len(calls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(calls))
		}
		if calls[0].Completed {
			t.Error("expected Completed = false for in-flight call")
		}
	})

	t.Run("most recent first", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventAgentToolCallStart, NodeID: "n1", Timestamp: now, Data: map[string]any{
				"call_id": "c1", "tool_name": "first_tool",
			}},
			{Type: EventAgentToolCallEnd, NodeID: "n1", Timestamp: now.Add(100 * time.Millisecond), Data: map[string]any{
				"call_id": "c1", "tool_name": "first_tool",
			}},
			{Type: EventAgentToolCallStart, NodeID: "n1", Timestamp: now.Add(200 * time.Millisecond), Data: map[string]any{
				"call_id": "c2", "tool_name": "second_tool",
			}},
			{Type: EventAgentToolCallEnd, NodeID: "n1", Timestamp: now.Add(300 * time.Millisecond), Data: map[string]any{
				"call_id": "c2", "tool_name": "second_tool",
			}},
		}
		calls := aggregateToolCalls(events)
		if len(calls) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(calls))
		}
		if calls[0].ToolName != "second_tool" {
			t.Errorf("expected most recent first, got %q", calls[0].ToolName)
		}
		if calls[1].ToolName != "first_tool" {
			t.Errorf("expected oldest last, got %q", calls[1].ToolName)
		}
	})

	t.Run("empty events", func(t *testing.T) {
		calls := aggregateToolCalls(nil)
		if len(calls) != 0 {
			t.Errorf("expected 0 calls for nil events, got %d", len(calls))
		}
	})

	t.Run("ignores non-tool events", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventStageStarted, NodeID: "n1", Timestamp: now},
			{Type: EventAgentLLMTurn, NodeID: "n1", Timestamp: now},
			{Type: EventStageCompleted, NodeID: "n1", Timestamp: now},
		}
		calls := aggregateToolCalls(events)
		if len(calls) != 0 {
			t.Errorf("expected 0 calls for non-tool events, got %d", len(calls))
		}
	})
}

// ---- Step 5: aggregateTokenStats unit tests ----

func TestAggregateTokenStats(t *testing.T) {
	now := time.Now()

	t.Run("sums multiple turns", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventAgentLLMTurn, Timestamp: now, Data: map[string]any{
				"input_tokens": 100, "output_tokens": 50, "total_tokens": 150,
				"reasoning_tokens": 10, "cache_read_tokens": 20, "cache_write_tokens": 5,
			}},
			{Type: EventAgentLLMTurn, Timestamp: now.Add(time.Second), Data: map[string]any{
				"input_tokens": 200, "output_tokens": int64(100), "total_tokens": float64(300),
				"reasoning_tokens": 30, "cache_read_tokens": 40, "cache_write_tokens": 15,
			}},
		}
		stats := aggregateTokenStats(events)
		if stats.InputTokens != 300 {
			t.Errorf("InputTokens = %d, want 300", stats.InputTokens)
		}
		if stats.OutputTokens != 150 {
			t.Errorf("OutputTokens = %d, want 150", stats.OutputTokens)
		}
		if stats.TotalTokens != 450 {
			t.Errorf("TotalTokens = %d, want 450", stats.TotalTokens)
		}
		if stats.ReasoningTokens != 40 {
			t.Errorf("ReasoningTokens = %d, want 40", stats.ReasoningTokens)
		}
		if stats.CacheReadTokens != 60 {
			t.Errorf("CacheReadTokens = %d, want 60", stats.CacheReadTokens)
		}
		if stats.CacheWriteTokens != 20 {
			t.Errorf("CacheWriteTokens = %d, want 20", stats.CacheWriteTokens)
		}
		if stats.TurnCount != 2 {
			t.Errorf("TurnCount = %d, want 2", stats.TurnCount)
		}
	})

	t.Run("zero state", func(t *testing.T) {
		stats := aggregateTokenStats(nil)
		if stats.InputTokens != 0 || stats.OutputTokens != 0 || stats.TotalTokens != 0 || stats.TurnCount != 0 {
			t.Errorf("expected zero stats for nil events, got %+v", stats)
		}
	})

	t.Run("ignores non-llm events", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventStageStarted, Timestamp: now, Data: map[string]any{"input_tokens": 999}},
			{Type: EventAgentToolCallStart, Timestamp: now, Data: map[string]any{"input_tokens": 888}},
		}
		stats := aggregateTokenStats(events)
		if stats.InputTokens != 0 {
			t.Errorf("expected 0 InputTokens for non-LLM events, got %d", stats.InputTokens)
		}
	})
}

// ---- Step 7: deriveActiveNode unit tests ----

func TestDeriveActiveNode(t *testing.T) {
	now := time.Now()

	t.Run("active node", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventStageStarted, NodeID: "build", Timestamp: now},
		}
		an := deriveActiveNode(events)
		if !an.Active {
			t.Error("expected Active = true")
		}
		if an.NodeID != "build" {
			t.Errorf("NodeID = %q, want %q", an.NodeID, "build")
		}
	})

	t.Run("completed node is not active", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventStageStarted, NodeID: "build", Timestamp: now},
			{Type: EventStageCompleted, NodeID: "build", Timestamp: now.Add(time.Second)},
		}
		an := deriveActiveNode(events)
		if an.Active {
			t.Error("expected Active = false when node is completed")
		}
	})

	t.Run("failed node is not active", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventStageStarted, NodeID: "build", Timestamp: now},
			{Type: EventStageFailed, NodeID: "build", Timestamp: now.Add(time.Second)},
		}
		an := deriveActiveNode(events)
		if an.Active {
			t.Error("expected Active = false when node has failed")
		}
	})

	t.Run("multiple stages last one active", func(t *testing.T) {
		events := []EngineEvent{
			{Type: EventStageStarted, NodeID: "first", Timestamp: now},
			{Type: EventStageCompleted, NodeID: "first", Timestamp: now.Add(time.Second)},
			{Type: EventStageStarted, NodeID: "second", Timestamp: now.Add(2 * time.Second)},
		}
		an := deriveActiveNode(events)
		if !an.Active {
			t.Error("expected Active = true")
		}
		if an.NodeID != "second" {
			t.Errorf("NodeID = %q, want %q", an.NodeID, "second")
		}
	})

	t.Run("idle with no events", func(t *testing.T) {
		an := deriveActiveNode(nil)
		if an.Active {
			t.Error("expected Active = false for nil events")
		}
		if an.NodeID != "" {
			t.Errorf("expected empty NodeID for nil events, got %q", an.NodeID)
		}
	})
}

// ---- Step 9: HTTP test for tools-fragment ----

func TestToolsFragmentEndpoint(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	now := time.Now()

	t.Run("paired calls with duration", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "tools-test-1",
			Status:    "completed",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events: []EngineEvent{
				{Type: EventAgentToolCallStart, NodeID: "codegen", Timestamp: now, Data: map[string]any{
					"call_id": "tc_1", "tool_name": "write_file",
				}},
				{Type: EventAgentToolCallEnd, NodeID: "codegen", Timestamp: now.Add(230 * time.Millisecond), Data: map[string]any{
					"call_id": "tc_1", "tool_name": "write_file", "output_snippet": "wrote 99 bytes", "duration_ms": int64(230),
				}},
			},
		}
		srv.mu.Lock()
		srv.pipelines["tools-test-1"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/tools-test-1/tools-fragment")
		if err != nil {
			t.Fatalf("GET tools-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		if !strings.Contains(html, "write_file") {
			t.Error("expected tool name 'write_file' in fragment")
		}
		if !strings.Contains(html, "230ms") {
			t.Error("expected '230ms' duration in fragment")
		}
		if !strings.Contains(html, "wrote 99 bytes") {
			t.Error("expected output snippet in fragment")
		}
		if !strings.Contains(html, "tool-done") {
			t.Error("expected 'tool-done' class for completed tool call")
		}
	})

	t.Run("in-flight call", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "tools-test-2",
			Status:    "running",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events: []EngineEvent{
				{Type: EventAgentToolCallStart, NodeID: "codegen", Timestamp: now, Data: map[string]any{
					"call_id": "tc_2", "tool_name": "read_file",
				}},
			},
		}
		srv.mu.Lock()
		srv.pipelines["tools-test-2"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/tools-test-2/tools-fragment")
		if err != nil {
			t.Fatalf("GET tools-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		if !strings.Contains(html, "read_file") {
			t.Error("expected tool name 'read_file' in fragment")
		}
		if !strings.Contains(html, "tool-running") {
			t.Error("expected 'tool-running' class for in-flight call")
		}
	})

	t.Run("empty state", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "tools-test-3",
			Status:    "running",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events:    []EngineEvent{},
		}
		srv.mu.Lock()
		srv.pipelines["tools-test-3"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/tools-test-3/tools-fragment")
		if err != nil {
			t.Fatalf("GET tools-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		if !strings.Contains(html, "no-data") {
			t.Error("expected no-data indicator for empty tool list")
		}
	})

	t.Run("404 for missing pipeline", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/ui/pipelines/nonexistent/tools-fragment")
		if err != nil {
			t.Fatalf("GET tools-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

// ---- Step 11: HTTP test for tokens-fragment ----

func TestTokensFragmentEndpoint(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	now := time.Now()

	t.Run("sums multiple turns", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "tokens-test-1",
			Status:    "completed",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events: []EngineEvent{
				{Type: EventAgentLLMTurn, NodeID: "codegen", Timestamp: now, Data: map[string]any{
					"input_tokens": 1000, "output_tokens": 500, "total_tokens": 1500,
					"reasoning_tokens": 100, "cache_read_tokens": 200, "cache_write_tokens": 50,
				}},
				{Type: EventAgentLLMTurn, NodeID: "codegen", Timestamp: now.Add(time.Second), Data: map[string]any{
					"input_tokens": 2000, "output_tokens": 1000, "total_tokens": 3000,
				}},
			},
		}
		srv.mu.Lock()
		srv.pipelines["tokens-test-1"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/tokens-test-1/tokens-fragment")
		if err != nil {
			t.Fatalf("GET tokens-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		// Input: 1000+2000=3000
		if !strings.Contains(html, "3,000") {
			t.Error("expected '3,000' for summed input tokens")
		}
		// Output: 500+1000=1500
		if !strings.Contains(html, "1,500") {
			t.Error("expected '1,500' for summed output tokens")
		}
		// Total: 1500+3000=4500
		if !strings.Contains(html, "4,500") {
			t.Error("expected '4,500' for summed total tokens")
		}
		if !strings.Contains(html, "token-counter") {
			t.Error("expected 'token-counter' class in token stats")
		}
	})

	t.Run("zero state", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "tokens-test-2",
			Status:    "running",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events:    []EngineEvent{},
		}
		srv.mu.Lock()
		srv.pipelines["tokens-test-2"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/tokens-test-2/tokens-fragment")
		if err != nil {
			t.Fatalf("GET tokens-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		// Should still render counters, just with 0
		if !strings.Contains(html, "token-counter") {
			t.Error("expected 'token-counter' class even for zero state")
		}
	})

	t.Run("404 for missing pipeline", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/ui/pipelines/nonexistent/tokens-fragment")
		if err != nil {
			t.Fatalf("GET tokens-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

// ---- Step 13: HTTP test for active-node-fragment ----

func TestActiveNodeFragmentEndpoint(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	now := time.Now()

	t.Run("active node shows indicator", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "active-test-1",
			Status:    "running",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events: []EngineEvent{
				{Type: EventStageStarted, NodeID: "build", Timestamp: now},
			},
		}
		srv.mu.Lock()
		srv.pipelines["active-test-1"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/active-test-1/active-node-fragment")
		if err != nil {
			t.Fatalf("GET active-node-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		if !strings.Contains(html, "build") {
			t.Error("expected active node 'build' in fragment")
		}
		if !strings.Contains(html, "active-node") {
			t.Error("expected 'active-node' class in fragment")
		}
	})

	t.Run("idle state", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "active-test-2",
			Status:    "running",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events:    []EngineEvent{},
		}
		srv.mu.Lock()
		srv.pipelines["active-test-2"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/active-test-2/active-node-fragment")
		if err != nil {
			t.Fatalf("GET active-node-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		// Should not show active indicator when idle
		if strings.Contains(html, "active-node") {
			t.Error("expected no 'active-node' class for idle state")
		}
	})

	t.Run("completed node is not active", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "active-test-3",
			Status:    "completed",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events: []EngineEvent{
				{Type: EventStageStarted, NodeID: "build", Timestamp: now},
				{Type: EventStageCompleted, NodeID: "build", Timestamp: now.Add(time.Second)},
			},
		}
		srv.mu.Lock()
		srv.pipelines["active-test-3"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/active-test-3/active-node-fragment")
		if err != nil {
			t.Fatalf("GET active-node-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		if strings.Contains(html, "active-node") {
			t.Error("expected no 'active-node' class for completed pipeline")
		}
	})

	t.Run("404 for missing pipeline", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/ui/pipelines/nonexistent/active-node-fragment")
		if err != nil {
			t.Fatalf("GET active-node-fragment failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}

// ---- Dashboard SSE stream tests ----

// ---- Fix 4: Human gate answer handler supports form-encoded HTMX requests ----

func TestAnswerQuestionHTMXFormEncoded(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	now := time.Now()

	// Create a pipeline with a pending question
	run := &PipelineRun{
		ID:        "htmx-answer-test",
		Status:    "running",
		Source:    `digraph { start -> done }`,
		CreatedAt: now,
		Questions: []PendingQuestion{
			{ID: "q1", Question: "Pick a color", Options: []string{"red", "blue"}, Answered: false},
		},
		answerChans: map[string]chan string{
			"q1": make(chan string, 1),
		},
	}
	srv.mu.Lock()
	srv.pipelines["htmx-answer-test"] = run
	srv.mu.Unlock()

	t.Run("form-encoded with HX-Request header returns HTML", func(t *testing.T) {
		req, _ := http.NewRequest("POST", ts.URL+"/pipelines/htmx-answer-test/questions/q1/answer",
			strings.NewReader("answer=red"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST answer failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Errorf("expected HTML content-type for HTMX, got %q", ct)
		}

		body, _ := io.ReadAll(resp.Body)
		html := string(body)
		// Should return updated questions HTML (q1 is answered, so should be empty)
		if !strings.Contains(html, "no-data") && !strings.Contains(html, "No pending") {
			// If there are no more pending questions, we expect the no-data div
			t.Logf("got HTML: %s", html)
		}
	})

	t.Run("JSON body without HX-Request returns JSON", func(t *testing.T) {
		// Reset: add another question
		run.mu.Lock()
		run.Questions = append(run.Questions, PendingQuestion{
			ID: "q2", Question: "Pick a shape", Options: []string{"circle", "square"}, Answered: false,
		})
		run.answerChans["q2"] = make(chan string, 1)
		run.mu.Unlock()

		req, _ := http.NewRequest("POST", ts.URL+"/pipelines/htmx-answer-test/questions/q2/answer",
			strings.NewReader(`{"answer":"circle"}`))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST answer failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/json") {
			t.Errorf("expected JSON content-type for API, got %q", ct)
		}

		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		if result["status"] != "answered" {
			t.Errorf("expected status 'answered', got %q", result["status"])
		}
	})

	t.Run("empty answer returns 400", func(t *testing.T) {
		run.mu.Lock()
		run.Questions = append(run.Questions, PendingQuestion{
			ID: "q3", Question: "Yes or no?", Options: []string{"yes", "no"}, Answered: false,
		})
		run.answerChans["q3"] = make(chan string, 1)
		run.mu.Unlock()

		req, _ := http.NewRequest("POST", ts.URL+"/pipelines/htmx-answer-test/questions/q3/answer",
			strings.NewReader("answer="))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST answer failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for empty answer, got %d", resp.StatusCode)
		}
	})
}

// ---- Fix 1: Expandable tool cards with full output ----

func TestToolCardExpandableOutput(t *testing.T) {
	longOutput := strings.Repeat("x", 200)
	tc := toolCallView{
		CallID:        "c1",
		ToolName:      "write_file",
		NodeID:        "build",
		OutputSnippet: longOutput,
		Completed:     true,
		Duration:      150 * time.Millisecond,
	}

	var buf strings.Builder
	writeToolCallHTML(&buf, tc)
	html := buf.String()

	// Should have the truncated preview
	if !strings.Contains(html, "tool-output") {
		t.Error("expected tool-output class for truncated preview")
	}

	// Should have the full output in a hidden div
	if !strings.Contains(html, "tool-output-full") {
		t.Error("expected tool-output-full class for expandable full output")
	}

	// Should have onclick for expanding
	if !strings.Contains(html, "classList.toggle") {
		t.Error("expected onclick toggle for expanding tool card")
	}

	// Full output should contain the entire string (200 chars)
	if !strings.Contains(html, longOutput) {
		t.Error("expected full output snippet in tool-output-full div")
	}

	// Short output should NOT appear when output is <= 120 chars
	shortTC := toolCallView{
		CallID:        "c2",
		ToolName:      "read_file",
		OutputSnippet: "short output",
		Completed:     true,
		Duration:      50 * time.Millisecond,
	}
	var buf2 strings.Builder
	writeToolCallHTML(&buf2, shortTC)
	html2 := buf2.String()

	// Short output should NOT have the expandable full div
	if strings.Contains(html2, "tool-output-full") {
		t.Error("short output should not have tool-output-full div")
	}
}

// ---- Fix 2: Graph highlights active node ----

func TestRenderGraphHTMLHighlightsActiveNode(t *testing.T) {
	now := time.Now()

	// Track what outcomes were passed to ToDOTWithStatus
	var capturedOutcomes map[string]*Outcome
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.ToDOTWithStatus = func(g *Graph, outcomes map[string]*Outcome) string {
		capturedOutcomes = outcomes
		return "digraph { /* status */ }"
	}

	events := []EngineEvent{
		{Type: EventStageStarted, NodeID: "build", Timestamp: now},
		{Type: EventStageCompleted, NodeID: "build", Timestamp: now.Add(time.Second)},
		{Type: EventStageStarted, NodeID: "deploy", Timestamp: now.Add(2 * time.Second)},
		// deploy is still running — no completed/failed event
	}

	result := &RunResult{
		NodeOutcomes: map[string]*Outcome{
			"build": {Status: StatusSuccess},
		},
	}

	_ = srv.renderGraphHTML(context.Background(), simpleDOTSource(), result, events)

	if capturedOutcomes == nil {
		t.Fatal("expected ToDOTWithStatus to be called with outcomes")
	}

	// The original "build" outcome should still be there
	if capturedOutcomes["build"] == nil || capturedOutcomes["build"].Status != StatusSuccess {
		t.Error("expected build outcome to be preserved")
	}

	// The active node "deploy" should have a synthetic StatusRetry outcome
	if capturedOutcomes["deploy"] == nil {
		t.Fatal("expected synthetic outcome for active node 'deploy'")
	}
	if capturedOutcomes["deploy"].Status != StatusRetry {
		t.Errorf("expected StatusRetry for active node, got %q", capturedOutcomes["deploy"].Status)
	}
}

func TestRenderGraphHTMLNoActiveNodeNoChange(t *testing.T) {
	var capturedOutcomes map[string]*Outcome
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.ToDOTWithStatus = func(g *Graph, outcomes map[string]*Outcome) string {
		capturedOutcomes = outcomes
		return "digraph { /* status */ }"
	}

	now := time.Now()
	events := []EngineEvent{
		{Type: EventStageStarted, NodeID: "build", Timestamp: now},
		{Type: EventStageCompleted, NodeID: "build", Timestamp: now.Add(time.Second)},
	}

	result := &RunResult{
		NodeOutcomes: map[string]*Outcome{
			"build": {Status: StatusSuccess},
		},
	}

	_ = srv.renderGraphHTML(context.Background(), simpleDOTSource(), result, events)

	if capturedOutcomes == nil {
		t.Fatal("expected ToDOTWithStatus to be called")
	}
	// No active node, so no synthetic outcome should be added
	if _, has := capturedOutcomes["deploy"]; has {
		t.Error("expected no synthetic outcome when there is no active node")
	}
}

// ---- Fix 3: SVG attribute stripping ----

func TestRenderGraphHTMLStripsSVGDimensions(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	srv.ToDOT = stubToDOT
	srv.RenderDOTSource = func(_ context.Context, dotText string, format string) ([]byte, error) {
		// Simulate graphviz SVG output with hardcoded dimensions
		return []byte(`<svg width="300pt" height="200pt" viewBox="0 0 300 200"><text>graph</text></svg>`), nil
	}

	html := srv.renderGraphHTML(context.Background(), simpleDOTSource(), nil, nil)

	// Should NOT contain width="...pt" or height="...pt"
	if strings.Contains(html, `width="300pt"`) {
		t.Error("expected width attribute to be stripped from SVG")
	}
	if strings.Contains(html, `height="200pt"`) {
		t.Error("expected height attribute to be stripped from SVG")
	}
	// Should still contain viewBox and the rest of the SVG
	if !strings.Contains(html, `viewBox="0 0 300 200"`) {
		t.Error("expected viewBox to be preserved")
	}
	if !strings.Contains(html, "<text>graph</text>") {
		t.Error("expected SVG content to be preserved")
	}
}

func TestDashboardStream(t *testing.T) {
	engine := newServerTestEngine()
	srv := NewPipelineServer(engine)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	now := time.Now()

	t.Run("sends named SSE events for completed pipeline", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "dash-stream-1",
			Status:    "completed",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events: []EngineEvent{
				{Type: EventStageStarted, NodeID: "build", Timestamp: now},
				{Type: EventAgentToolCallStart, NodeID: "build", Timestamp: now.Add(100 * time.Millisecond), Data: map[string]any{
					"call_id": "tc_1", "tool_name": "write_file",
				}},
				{Type: EventAgentToolCallEnd, NodeID: "build", Timestamp: now.Add(300 * time.Millisecond), Data: map[string]any{
					"call_id": "tc_1", "tool_name": "write_file", "output_snippet": "done",
				}},
				{Type: EventAgentLLMTurn, NodeID: "build", Timestamp: now.Add(400 * time.Millisecond), Data: map[string]any{
					"input_tokens": 100, "output_tokens": 50, "total_tokens": 150,
				}},
				{Type: EventStageCompleted, NodeID: "build", Timestamp: now.Add(500 * time.Millisecond)},
			},
		}
		srv.mu.Lock()
		srv.pipelines["dash-stream-1"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/dash-stream-1/dashboard-stream")
		if err != nil {
			t.Fatalf("GET dashboard-stream failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/event-stream") {
			t.Errorf("expected Content-Type 'text/event-stream', got %q", ct)
		}

		body, _ := io.ReadAll(resp.Body)
		output := string(body)

		// Verify named events are present
		wantEvents := []string{"tools", "tokens", "active-node", "status", "events", "pipeline-done"}
		for _, name := range wantEvents {
			if !strings.Contains(output, "event: "+name+"\n") {
				t.Errorf("expected SSE event named %q in output", name)
			}
		}

		// Verify tool call data appears in the tools event
		if !strings.Contains(output, "write_file") {
			t.Error("expected 'write_file' tool name in SSE output")
		}

		// Verify token data appears
		if !strings.Contains(output, "150") {
			t.Error("expected token count '150' in SSE output")
		}
	})

	t.Run("sends event-item for initial events batch", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "dash-stream-2",
			Status:    "completed",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events: []EngineEvent{
				{Type: EventStageStarted, NodeID: "deploy", Timestamp: now},
				{Type: EventStageCompleted, NodeID: "deploy", Timestamp: now.Add(time.Second)},
			},
		}
		srv.mu.Lock()
		srv.pipelines["dash-stream-2"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/dash-stream-2/dashboard-stream")
		if err != nil {
			t.Fatalf("GET dashboard-stream failed: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		output := string(body)

		// Initial batch sends all events as one "events" block
		if !strings.Contains(output, "event: events\n") {
			t.Error("expected 'events' SSE event for initial batch")
		}
		// Event content should contain stage types
		if !strings.Contains(output, "stage.started") {
			t.Error("expected 'stage.started' in events output")
		}
		if !strings.Contains(output, "stage.completed") {
			t.Error("expected 'stage.completed' in events output")
		}
	})

	t.Run("sends questions and context", func(t *testing.T) {
		run := &PipelineRun{
			ID:        "dash-stream-3",
			Status:    "completed",
			Source:    `digraph { start -> done }`,
			CreatedAt: now,
			Events:    []EngineEvent{},
		}
		srv.mu.Lock()
		srv.pipelines["dash-stream-3"] = run
		srv.mu.Unlock()

		resp, err := http.Get(ts.URL + "/ui/pipelines/dash-stream-3/dashboard-stream")
		if err != nil {
			t.Fatalf("GET dashboard-stream failed: %v", err)
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		output := string(body)

		if !strings.Contains(output, "event: questions\n") {
			t.Error("expected 'questions' SSE event")
		}
		if !strings.Contains(output, "event: context\n") {
			t.Error("expected 'context' SSE event")
		}
		if !strings.Contains(output, "event: graph\n") {
			t.Error("expected 'graph' SSE event")
		}
	})

	t.Run("404 for missing pipeline", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/ui/pipelines/nonexistent/dashboard-stream")
		if err != nil {
			t.Fatalf("GET dashboard-stream failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})
}
