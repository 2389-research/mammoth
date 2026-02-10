// ABOUTME: HTMX web frontend for the PipelineServer dashboard and pipeline detail views.
// ABOUTME: Uses go:embed for HTML templates and serves a browser-friendly UI alongside the JSON API.
package attractor

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// pipelineSummary is a view model for displaying a pipeline in the dashboard list.
type pipelineSummary struct {
	ID        string
	Status    string
	CreatedAt string
}

// pipelineDetail is a view model for the pipeline detail page.
type pipelineDetail struct {
	ID     string
	Status string
}

// registerUIRoutes adds the HTML/HTMX frontend routes to the server mux.
// Called from registerRoutes in server.go.
func (s *PipelineServer) registerUIRoutes() {
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /ui/pipelines/{id}", s.handlePipelineView)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/graph-fragment", s.handleGraphFragment)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/questions-fragment", s.handleQuestionsFragment)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/status-fragment", s.handleStatusFragment)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/context-fragment", s.handleContextFragment)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/events-fragment", s.handleEventsFragment)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/events-stream", s.handleEventsStream)
}

// handleDashboard renders the main dashboard page with a list of all pipelines.
func (s *PipelineServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	summaries := make([]pipelineSummary, 0, len(s.pipelines))
	for _, run := range s.pipelines {
		run.mu.RLock()
		summaries = append(summaries, pipelineSummary{
			ID:        run.ID,
			Status:    run.Status,
			CreatedAt: run.CreatedAt.Format(time.RFC3339),
		})
		run.mu.RUnlock()
	}
	s.mu.RUnlock()

	// Sort by creation time descending (most recent first)
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedAt > summaries[j].CreatedAt
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		Pipelines []pipelineSummary
	}{
		Pipelines: summaries,
	}
	templates.ExecuteTemplate(w, "dashboard.html", data)
}

// handlePipelineView renders the pipeline detail page for a specific pipeline.
func (s *PipelineServer) handlePipelineView(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	detail := pipelineDetail{
		ID:     run.ID,
		Status: run.Status,
	}
	run.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "pipeline.html", detail)
}

// handleGraphFragment returns the SVG graph as an HTML fragment for HTMX swaps.
func (s *PipelineServer) handleGraphFragment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	source := run.Source
	result := run.Result
	run.mu.RUnlock()

	if source == "" {
		w.Write([]byte(`<div class="no-data">No graph available</div>`))
		return
	}

	graph, err := Parse(source)
	if err != nil {
		w.Write([]byte(`<div class="no-data">Failed to parse graph</div>`))
		return
	}

	// Generate DOT text with status if available
	var dotText string
	if result != nil && result.NodeOutcomes != nil && s.ToDOTWithStatus != nil {
		dotText = s.ToDOTWithStatus(graph, result.NodeOutcomes)
	} else if s.ToDOT != nil {
		dotText = s.ToDOT(graph)
	} else {
		dotText = source
	}

	// Render to SVG if renderer is available
	if s.RenderDOTSource != nil {
		data, err := s.RenderDOTSource(r.Context(), dotText, "svg")
		if err == nil {
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write(data)
			return
		}
	}

	// Fallback: show DOT text in a pre block
	w.Write([]byte(`<pre style="color:#9898a4;font-family:'DM Mono','SF Mono',monospace;font-size:12px;line-height:1.6">`))
	w.Write([]byte(template.HTMLEscapeString(dotText)))
	w.Write([]byte(`</pre>`))
}

// handleQuestionsFragment returns the pending questions as an HTML fragment for HTMX swaps.
func (s *PipelineServer) handleQuestionsFragment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	var pending []PendingQuestion
	for _, q := range run.Questions {
		if !q.Answered {
			pending = append(pending, q)
		}
	}
	run.mu.RUnlock()

	if len(pending) == 0 {
		w.Write([]byte(`<div class="no-data">No pending questions</div>`))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	for _, q := range pending {
		w.Write([]byte(`<div class="question-card">`))
		w.Write([]byte(`<div class="question-text">` + template.HTMLEscapeString(q.Question) + `</div>`))
		w.Write([]byte(`<div class="question-options">`))
		for _, opt := range q.Options {
			w.Write([]byte(`<button class="answer-btn" hx-post="/pipelines/` + id + `/questions/` + q.ID + `/answer" `))
			w.Write([]byte(`hx-vals='{"answer":"` + template.JSEscapeString(opt) + `"}' `))
			w.Write([]byte(`hx-target="#questions-container" hx-swap="innerHTML">` + template.HTMLEscapeString(opt) + `</button>`))
		}
		w.Write([]byte(`</div></div>`))
	}
}

// handleStatusFragment returns the status badge as an HTML fragment for HTMX polling.
func (s *PipelineServer) handleStatusFragment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	status := run.Status
	run.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if status == "running" {
		w.Write([]byte(`<span class="running-dot"></span>`))
	}
	w.Write([]byte(`<span class="badge badge-` + template.HTMLEscapeString(status) + `">` + template.HTMLEscapeString(status) + `</span>`))
}

// handleContextFragment returns pipeline context key-value pairs as an HTML fragment.
func (s *PipelineServer) handleContextFragment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	result := run.Result
	run.mu.RUnlock()

	if result == nil || result.Context == nil {
		w.Write([]byte(`<div class="no-data">No context available yet</div>`))
		return
	}

	snap := result.Context.Snapshot()
	if len(snap) == 0 {
		w.Write([]byte(`<div class="no-data">Context is empty</div>`))
		return
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<table class="ctx-table">`))
	for _, k := range keys {
		// Skip internal context keys
		if len(k) > 0 && k[0] == '_' {
			continue
		}
		valStr := fmt.Sprintf("%v", snap[k])
		w.Write([]byte(`<tr><td class="ctx-key">` + template.HTMLEscapeString(k) + `</td>`))
		w.Write([]byte(`<td class="ctx-val">` + template.HTMLEscapeString(valStr) + `</td></tr>`))
	}
	w.Write([]byte(`</table>`))
}

// writeEventHTML writes a single engine event as an HTML fragment.
func writeEventHTML(w http.ResponseWriter, evt EngineEvent) {
	typeClass := "event-type"
	nodeHTML := ""
	if evt.NodeID != "" {
		nodeHTML = `<span class="event-node">` + template.HTMLEscapeString(evt.NodeID) + `</span>`
	}
	dataHTML := ""
	if len(evt.Data) > 0 {
		// Show select useful fields, not the entire map
		if reason, ok := evt.Data["reason"]; ok {
			dataHTML = `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%v", reason)) + `</span>`
		} else if status, ok := evt.Data["status"]; ok {
			dataHTML = `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%v", status)) + `</span>`
		} else if attempt, ok := evt.Data["attempt"]; ok {
			dataHTML = `<span class="event-detail">attempt ` + template.HTMLEscapeString(fmt.Sprintf("%v", attempt)) + `</span>`
		}
	}
	timeStr := evt.Timestamp.Format("15:04:05")
	w.Write([]byte(`<div class="event-item">`))
	w.Write([]byte(`<span class="event-time">` + timeStr + `</span>`))
	w.Write([]byte(`<span class="` + typeClass + `">` + template.HTMLEscapeString(string(evt.Type)) + `</span>`))
	w.Write([]byte(nodeHTML))
	w.Write([]byte(dataHTML))
	w.Write([]byte(`</div>`))
}

// handleEventsFragment returns all existing events as HTML (for initial page load).
func (s *PipelineServer) handleEventsFragment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
		return
	}

	run.mu.RLock()
	events := make([]EngineEvent, len(run.Events))
	copy(events, run.Events)
	run.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(events) == 0 {
		w.Write([]byte(`<div class="no-data">Waiting for events...</div>`))
		return
	}
	for _, evt := range events {
		writeEventHTML(w, evt)
	}
}

// handleEventsStream sends engine events as HTML fragments over SSE for live streaming.
func (s *PipelineServer) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "pipeline not found"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Start from current event count (events-fragment already loaded existing ones)
	run.mu.RLock()
	sentCount := len(run.Events)
	run.mu.RUnlock()

	for {
		run.mu.RLock()
		currentEvents := run.Events
		status := run.Status
		run.mu.RUnlock()

		// Send any new events as HTML fragments
		for sentCount < len(currentEvents) {
			evt := currentEvents[sentCount]
			// Build the HTML fragment as the SSE data payload
			typeStr := template.HTMLEscapeString(string(evt.Type))
			nodeHTML := ""
			if evt.NodeID != "" {
				nodeHTML = `<span class="event-node">` + template.HTMLEscapeString(evt.NodeID) + `</span>`
			}
			dataHTML := ""
			if len(evt.Data) > 0 {
				if reason, ok := evt.Data["reason"]; ok {
					dataHTML = `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%v", reason)) + `</span>`
				} else if s, ok := evt.Data["status"]; ok {
					dataHTML = `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%v", s)) + `</span>`
				} else if attempt, ok := evt.Data["attempt"]; ok {
					dataHTML = `<span class="event-detail">attempt ` + template.HTMLEscapeString(fmt.Sprintf("%v", attempt)) + `</span>`
				}
			}
			timeStr := evt.Timestamp.Format("15:04:05")
			html := `<div class="event-item">` +
				`<span class="event-time">` + timeStr + `</span>` +
				`<span class="event-type">` + typeStr + `</span>` +
				nodeHTML + dataHTML +
				`</div>`
			fmt.Fprintf(w, "data: %s\n\n", html)
			flusher.Flush()
			sentCount++
		}

		// If pipeline is done, send a final status event and close
		if status == "completed" || status == "failed" || status == "cancelled" {
			statusJSON, _ := json.Marshal(map[string]string{"status": status})
			fmt.Fprintf(w, "event: status\ndata: %s\n\n", statusJSON)
			flusher.Flush()
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}
