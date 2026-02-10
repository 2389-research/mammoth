// ABOUTME: Tests for the HTMX web frontend served by PipelineServer.
// ABOUTME: Covers dashboard, pipeline detail view, graph fragment, questions fragment, and pipeline listing.
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
