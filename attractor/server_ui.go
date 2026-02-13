// ABOUTME: HTMX web frontend for the PipelineServer dashboard and pipeline detail views.
// ABOUTME: Uses go:embed for HTML templates and serves a browser-friendly UI alongside the JSON API.
package attractor

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
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
	Error  string
}

// toolCallView is a view model for displaying a paired tool call in the UI.
type toolCallView struct {
	CallID, ToolName, NodeID, OutputSnippet string
	StartTime                               time.Time
	Duration                                time.Duration
	Completed                               bool
}

// tokenStatsView is a view model for aggregated token usage counters.
type tokenStatsView struct {
	InputTokens, OutputTokens, TotalTokens             int
	ReasoningTokens, CacheReadTokens, CacheWriteTokens int
	TurnCount                                          int
}

// activeNodeView is a view model for the currently-active pipeline node.
type activeNodeView struct {
	NodeID string
	Active bool
}

// toInt safely coerces an any value to int from int, int64, float64, or their pointer variants.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case *int:
		if n != nil {
			return *n
		}
		return 0
	case *int64:
		if n != nil {
			return int(*n)
		}
		return 0
	case *float64:
		if n != nil {
			return int(*n)
		}
		return 0
	default:
		return 0
	}
}

// formatNumber returns a comma-separated representation of n (e.g. 1234567 → "1,234,567").
func formatNumber(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		// Handle negative: format the absolute value and prepend '-'
		return "-" + formatNumber(-n)
	}
	if len(s) <= 3 {
		return s
	}
	// Insert commas from right to left
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// aggregateToolCalls pairs start/end events by call_id and returns views ordered most-recent-first.
func aggregateToolCalls(events []EngineEvent) []toolCallView {
	type pending struct {
		view  toolCallView
		order int // insertion order for stable sorting
	}
	calls := make(map[string]*pending)
	seq := 0

	for _, evt := range events {
		switch evt.Type {
		case EventAgentToolCallStart:
			callID := fmt.Sprintf("%v", evt.Data["call_id"])
			calls[callID] = &pending{
				view: toolCallView{
					CallID:    callID,
					ToolName:  fmt.Sprintf("%v", evt.Data["tool_name"]),
					NodeID:    evt.NodeID,
					StartTime: evt.Timestamp,
				},
				order: seq,
			}
			seq++
		case EventAgentToolCallEnd:
			callID := fmt.Sprintf("%v", evt.Data["call_id"])
			if p, ok := calls[callID]; ok {
				p.view.Completed = true
				p.view.Duration = evt.Timestamp.Sub(p.view.StartTime)
				if snip, ok := evt.Data["output_snippet"]; ok {
					p.view.OutputSnippet = fmt.Sprintf("%v", snip)
				}
			}
		}
	}

	result := make([]toolCallView, 0, len(calls))
	type ordered struct {
		view  toolCallView
		order int
	}
	sorted := make([]ordered, 0, len(calls))
	for _, p := range calls {
		sorted = append(sorted, ordered{view: p.view, order: p.order})
	}
	// Most recent first (highest order first)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].order > sorted[j].order
	})
	for _, s := range sorted {
		result = append(result, s.view)
	}
	return result
}

// aggregateTokenStats sums token fields from all agent.llm_turn events.
func aggregateTokenStats(events []EngineEvent) tokenStatsView {
	var stats tokenStatsView
	for _, evt := range events {
		if evt.Type != EventAgentLLMTurn {
			continue
		}
		stats.TurnCount++
		stats.InputTokens += toInt(evt.Data["input_tokens"])
		stats.OutputTokens += toInt(evt.Data["output_tokens"])
		stats.TotalTokens += toInt(evt.Data["total_tokens"])
		stats.ReasoningTokens += toInt(evt.Data["reasoning_tokens"])
		stats.CacheReadTokens += toInt(evt.Data["cache_read_tokens"])
		stats.CacheWriteTokens += toInt(evt.Data["cache_write_tokens"])
	}
	return stats
}

// deriveActiveNode finds the last stage.started node that has no matching stage.completed/failed.
func deriveActiveNode(events []EngineEvent) activeNodeView {
	started := make(map[string]bool)
	for _, evt := range events {
		switch evt.Type {
		case EventStageStarted:
			started[evt.NodeID] = true
		case EventStageCompleted, EventStageFailed:
			delete(started, evt.NodeID)
		}
	}
	// Find the last started node that's still active
	var lastActive string
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == EventStageStarted && started[events[i].NodeID] {
			lastActive = events[i].NodeID
			break
		}
	}
	if lastActive == "" {
		return activeNodeView{}
	}
	return activeNodeView{NodeID: lastActive, Active: true}
}

// sendSSE writes a named SSE event with properly formatted multi-line data.
func sendSSE(w io.Writer, eventType, data string) {
	if eventType != "" {
		fmt.Fprintf(w, "event: %s\n", eventType)
	}
	for _, line := range strings.Split(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", line)
	}
	fmt.Fprint(w, "\n")
}

// renderToolsFragmentHTML returns the tool activity feed as an HTML string.
func renderToolsFragmentHTML(events []EngineEvent) string {
	calls := aggregateToolCalls(events)
	if len(calls) == 0 {
		return `<div class="no-data">No tool calls yet</div>`
	}
	var buf bytes.Buffer
	for _, tc := range calls {
		writeToolCallHTML(&buf, tc)
	}
	return buf.String()
}

// renderTokensFragmentHTML returns the token counter bar as an HTML string.
func renderTokensFragmentHTML(events []EngineEvent) string {
	stats := aggregateTokenStats(events)
	var buf bytes.Buffer
	writeTokenStatsHTML(&buf, stats)
	return buf.String()
}

// renderActiveNodeFragmentHTML returns the active node indicator as an HTML string.
func renderActiveNodeFragmentHTML(events []EngineEvent) string {
	an := deriveActiveNode(events)
	if !an.Active {
		return `<span></span>`
	}
	return `<span class="active-node" style="color:var(--color-amber)">` + template.HTMLEscapeString(an.NodeID) + `</span>`
}

// renderStatusFragmentHTML returns the status badge as an HTML string.
func renderStatusFragmentHTML(status string) string {
	escaped := template.HTMLEscapeString(status)
	return `<span class="status-badge status-` + escaped + `"><span class="status-dot"></span>` + escaped + `</span>`
}

// renderQuestionsFragmentHTML returns the pending questions as an HTML string.
func renderQuestionsFragmentHTML(pipelineID string, questions []PendingQuestion) string {
	var pending []PendingQuestion
	for _, q := range questions {
		if !q.Answered {
			pending = append(pending, q)
		}
	}
	if len(pending) == 0 {
		return `<div class="no-data">No pending questions</div>`
	}
	var buf bytes.Buffer
	for _, q := range pending {
		buf.WriteString(`<div class="question-card">`)
		buf.WriteString(`<div class="question-text">` + template.HTMLEscapeString(q.Question) + `</div>`)
		buf.WriteString(`<div class="question-options">`)
		for _, opt := range q.Options {
			buf.WriteString(`<button class="answer-btn" hx-post="/pipelines/` + template.HTMLEscapeString(pipelineID) + `/questions/` + template.HTMLEscapeString(q.ID) + `/answer" `)
			buf.WriteString(`hx-vals='{"answer":"` + template.JSEscapeString(opt) + `"}' `)
			buf.WriteString(`hx-target="#questions-container" hx-swap="innerHTML">` + template.HTMLEscapeString(opt) + `</button>`)
		}
		buf.WriteString(`</div></div>`)
	}
	return buf.String()
}

// renderContextFragmentHTML returns the pipeline context as an HTML string.
func renderContextFragmentHTML(result *RunResult) string {
	if result == nil || result.Context == nil {
		return `<div class="no-data">No context available yet</div>`
	}
	snap := result.Context.Snapshot()
	if len(snap) == 0 {
		return `<div class="no-data">Context is empty</div>`
	}
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString(`<div class="ctx-entries">`)
	for _, k := range keys {
		if len(k) > 0 && k[0] == '_' {
			continue
		}
		valStr := fmt.Sprintf("%v", snap[k])
		buf.WriteString(`<div class="ctx-row"><span class="ctx-key">` + template.HTMLEscapeString(k) + `</span>`)
		buf.WriteString(`<span class="ctx-val">` + template.HTMLEscapeString(valStr) + `</span></div>`)
	}
	buf.WriteString(`</div>`)
	return buf.String()
}

// renderGraphHTML returns the graph panel content as an HTML string.
// Accepts events to derive the active node and highlight it in the graph
// with a synthetic StatusRetry outcome (rendered as amber/yellow).
func (s *PipelineServer) renderGraphHTML(ctx context.Context, source string, result *RunResult, events []EngineEvent) string {
	if source == "" {
		return `<div class="no-data">No graph available</div>`
	}
	graph, err := Parse(source)
	if err != nil {
		return `<div class="no-data">Failed to parse graph</div>`
	}
	var dotText string
	if result != nil && result.NodeOutcomes != nil && s.ToDOTWithStatus != nil {
		// Copy outcomes to avoid mutating the original map
		augmented := make(map[string]*Outcome, len(result.NodeOutcomes))
		for k, v := range result.NodeOutcomes {
			augmented[k] = v
		}
		// Inject a synthetic StatusRetry outcome for the active node so it
		// renders with the amber/running color in the graph
		active := deriveActiveNode(events)
		if active.Active {
			if _, hasOutcome := augmented[active.NodeID]; !hasOutcome {
				augmented[active.NodeID] = &Outcome{Status: StatusRetry}
			}
		}
		dotText = s.ToDOTWithStatus(graph, augmented)
	} else if s.ToDOT != nil {
		dotText = s.ToDOT(graph)
	} else {
		dotText = source
	}
	if s.RenderDOTSource != nil {
		data, err := s.RenderDOTSource(ctx, dotText, "svg")
		if err == nil {
			return stripSVGDimensions(string(data))
		}
	}
	return `<pre style="color:var(--color-text-secondary);font-family:var(--font-mono);font-size:12px;line-height:1.6">` + template.HTMLEscapeString(dotText) + `</pre>`
}

// stripSVGDimensions removes width="..." and height="..." attributes from the
// root <svg> tag so that CSS can control the sizing. Preserves viewBox for
// proper aspect-ratio scaling.
func stripSVGDimensions(svg string) string {
	// Only strip from the opening <svg> tag (first occurrence)
	idx := strings.Index(svg, "<svg")
	if idx < 0 {
		return svg
	}
	end := strings.Index(svg[idx:], ">")
	if end < 0 {
		return svg
	}
	tag := svg[idx : idx+end+1]
	// Strip width="..." and height="..." (handles pt, px, em, %, or bare numbers)
	cleaned := stripAttr(tag, "width")
	cleaned = stripAttr(cleaned, "height")
	return svg[:idx] + cleaned + svg[idx+end+1:]
}

// stripAttr removes a single HTML attribute (name="value") from a tag string.
func stripAttr(tag, attr string) string {
	// Match: attr="value" with optional surrounding spaces
	prefix := attr + `="`
	start := strings.Index(tag, prefix)
	if start < 0 {
		return tag
	}
	valEnd := strings.Index(tag[start+len(prefix):], `"`)
	if valEnd < 0 {
		return tag
	}
	// Remove the attribute plus any leading whitespace
	attrEnd := start + len(prefix) + valEnd + 1
	// Trim leading space before the attribute
	trimStart := start
	for trimStart > 0 && tag[trimStart-1] == ' ' {
		trimStart--
	}
	return tag[:trimStart] + tag[attrEnd:]
}

// writeToolCallHTML writes a single tool call as an HTML card fragment.
// Cards with output longer than 120 chars are expandable on click.
func writeToolCallHTML(w io.Writer, tc toolCallView) {
	statusClass := "tool-running"
	durationStr := "running"
	if tc.Completed {
		statusClass = "tool-done"
		durationStr = fmt.Sprintf("%dms", tc.Duration.Milliseconds())
	}

	needsExpand := len(tc.OutputSnippet) > 120

	if needsExpand {
		w.Write([]byte(`<div class="tool-card ` + statusClass + `" id="tool-` + template.HTMLEscapeString(tc.CallID) + `" onclick="this.classList.toggle('expanded')">`))
	} else {
		w.Write([]byte(`<div class="tool-card ` + statusClass + `" id="tool-` + template.HTMLEscapeString(tc.CallID) + `">`))
	}
	w.Write([]byte(`<div class="tool-header">`))
	w.Write([]byte(`<span class="tool-dot"></span>`))
	w.Write([]byte(`<span class="tool-name">` + template.HTMLEscapeString(tc.ToolName) + `</span>`))
	if tc.NodeID != "" {
		w.Write([]byte(`<span class="tool-node">` + template.HTMLEscapeString(tc.NodeID) + `</span>`))
	}
	w.Write([]byte(`<span class="tool-duration">` + template.HTMLEscapeString(durationStr) + `</span>`))
	w.Write([]byte(`</div>`))
	if tc.OutputSnippet != "" {
		if needsExpand {
			// Truncated preview (visible by default)
			snip := tc.OutputSnippet[:120]
			w.Write([]byte(`<p class="tool-output">` + template.HTMLEscapeString(snip) + `&hellip;</p>`))
			// Full output (hidden until expanded)
			w.Write([]byte(`<div class="tool-output-full">` + template.HTMLEscapeString(tc.OutputSnippet) + `</div>`))
		} else {
			w.Write([]byte(`<p class="tool-output">` + template.HTMLEscapeString(tc.OutputSnippet) + `</p>`))
		}
	}
	w.Write([]byte(`</div>`))
}

// writeTokenStatsHTML writes the token counters as a flex row.
func writeTokenStatsHTML(w io.Writer, stats tokenStatsView) {
	counters := []struct {
		Label string
		Value int
	}{
		{"Input", stats.InputTokens},
		{"Output", stats.OutputTokens},
		{"Total", stats.TotalTokens},
		{"Reasoning", stats.ReasoningTokens},
		{"Cache read", stats.CacheReadTokens},
		{"Cache write", stats.CacheWriteTokens},
		{"Turns", stats.TurnCount},
	}
	w.Write([]byte(`<div class="token-stats">`))
	for _, c := range counters {
		w.Write([]byte(`<div class="token-counter">`))
		w.Write([]byte(`<span class="token-value">` + formatNumber(c.Value) + `</span>`))
		w.Write([]byte(`<span class="token-label">` + c.Label + `</span>`))
		w.Write([]byte(`</div>`))
	}
	w.Write([]byte(`</div>`))
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
	s.mux.HandleFunc("GET /ui/pipelines/{id}/dashboard-stream", s.handleDashboardStream)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/tools-fragment", s.handleToolsFragment)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/tokens-fragment", s.handleTokensFragment)
	s.mux.HandleFunc("GET /ui/pipelines/{id}/active-node-fragment", s.handleActiveNodeFragment)
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
	_ = templates.ExecuteTemplate(w, "dashboard.html", data)
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
		Error:  run.Error,
	}
	run.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.ExecuteTemplate(w, "pipeline.html", detail)
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
	events := make([]EngineEvent, len(run.Events))
	copy(events, run.Events)
	run.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(s.renderGraphHTML(r.Context(), source, result, events)))
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
			w.Write([]byte(`<button class="answer-btn" hx-post="/pipelines/` + template.HTMLEscapeString(id) + `/questions/` + template.HTMLEscapeString(q.ID) + `/answer" `))
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
	escaped := template.HTMLEscapeString(status)
	w.Write([]byte(`<span class="status-badge status-` + escaped + `"><span class="status-dot"></span>` + escaped + `</span>`))
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
	w.Write([]byte(`<div class="ctx-entries">`))
	for _, k := range keys {
		// Skip internal context keys
		if len(k) > 0 && k[0] == '_' {
			continue
		}
		valStr := fmt.Sprintf("%v", snap[k])
		w.Write([]byte(`<div class="ctx-row"><span class="ctx-key">` + template.HTMLEscapeString(k) + `</span>`))
		w.Write([]byte(`<span class="ctx-val">` + template.HTMLEscapeString(valStr) + `</span></div>`))
	}
	w.Write([]byte(`</div>`))
}

// writeEventHTML writes a single engine event as an HTML fragment.
func writeEventHTML(w io.Writer, evt EngineEvent) {
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
		} else if errVal, ok := evt.Data["error"]; ok {
			dataHTML = `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%v", errVal)) + `</span>`
		} else if status, ok := evt.Data["status"]; ok {
			dataHTML = `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%v", status)) + `</span>`
		} else if attempt, ok := evt.Data["attempt"]; ok {
			dataHTML = `<span class="event-detail">attempt ` + template.HTMLEscapeString(fmt.Sprintf("%v", attempt)) + `</span>`
		}

		// Agent-level event data: show tool names, output snippets, messages
		if toolName, ok := evt.Data["tool_name"]; ok {
			dataHTML += `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%v", toolName)) + `</span>`
		}
		if outputSnip, ok := evt.Data["output_snippet"]; ok {
			snip := fmt.Sprintf("%v", outputSnip)
			if len(snip) > 100 {
				snip = snip[:100]
			}
			dataHTML += `<span class="event-output">` + template.HTMLEscapeString(snip) + `</span>`
		}
		if msg, ok := evt.Data["message"]; ok {
			dataHTML += `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%v", msg)) + `</span>`
		}
		if durationMs, ok := evt.Data["duration_ms"]; ok {
			dataHTML += `<span class="event-detail">` + template.HTMLEscapeString(fmt.Sprintf("%vms", durationMs)) + `</span>`
		}
		// Token breakdown for LLM turn events
		if inputTok, ok := evt.Data["input_tokens"]; ok {
			dataHTML += `<span class="event-tokens">` + template.HTMLEscapeString(
				fmt.Sprintf("in:%v out:%v total:%v", inputTok, evt.Data["output_tokens"], evt.Data["total_tokens"]),
			) + `</span>`
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
	status := run.Status
	runErr := run.Error
	run.mu.RUnlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(events) == 0 {
		if (status == "failed" || status == "cancelled") && runErr != "" {
			w.Write([]byte(`<div class="error-banner">` + template.HTMLEscapeString(runErr) + `</div>`))
		} else if status == "failed" || status == "cancelled" {
			w.Write([]byte(`<div class="no-data">Pipeline ` + template.HTMLEscapeString(status) + ` with no events recorded</div>`))
		} else {
			w.Write([]byte(`<div class="no-data">Waiting for events...</div>`))
		}
		return
	}
	for _, evt := range events {
		writeEventHTML(w, evt)
	}
}

// handleToolsFragment returns the aggregated tool call feed as an HTML fragment.
func (s *PipelineServer) handleToolsFragment(w http.ResponseWriter, r *http.Request) {
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

	calls := aggregateToolCalls(events)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if len(calls) == 0 {
		w.Write([]byte(`<div class="no-data">No tool calls yet</div>`))
		return
	}
	for _, tc := range calls {
		writeToolCallHTML(w, tc)
	}
}

// handleTokensFragment returns the aggregated token stats as an HTML fragment.
func (s *PipelineServer) handleTokensFragment(w http.ResponseWriter, r *http.Request) {
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

	stats := aggregateTokenStats(events)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeTokenStatsHTML(w, stats)
}

// handleActiveNodeFragment returns the active node indicator as an HTML fragment.
func (s *PipelineServer) handleActiveNodeFragment(w http.ResponseWriter, r *http.Request) {
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

	an := deriveActiveNode(events)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !an.Active {
		w.Write([]byte(`<span></span>`))
		return
	}
	w.Write([]byte(`<span class="active-node" style="color:var(--color-amber)">` + template.HTMLEscapeString(an.NodeID) + `</span>`))
}

// handleDashboardStream provides a unified SSE stream for all dashboard panels.
// On first connect, sends current state for all panels. Then pushes changes as they occur.
// Named events: tools, tokens, active-node, status, questions, context, graph, events, event-item, pipeline-done.
func (s *PipelineServer) handleDashboardStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	run, ok := s.pipelines[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "pipeline not found", http.StatusNotFound)
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

	sentEventCount := 0
	lastStatus := ""
	lastQuestionCount := 0
	lastGraphRender := time.Time{}
	first := true

	for {
		run.mu.RLock()
		events := make([]EngineEvent, len(run.Events))
		copy(events, run.Events)
		status := run.Status
		source := run.Source
		result := run.Result
		questions := make([]PendingQuestion, len(run.Questions))
		copy(questions, run.Questions)
		run.mu.RUnlock()

		pendingCount := 0
		for _, q := range questions {
			if !q.Answered {
				pendingCount++
			}
		}

		if first {
			// Send all panels on initial connect
			sendSSE(w, "tools", renderToolsFragmentHTML(events))
			sendSSE(w, "tokens", renderTokensFragmentHTML(events))
			sendSSE(w, "active-node", renderActiveNodeFragmentHTML(events))
			sendSSE(w, "status", renderStatusFragmentHTML(status))
			sendSSE(w, "questions", renderQuestionsFragmentHTML(id, questions))
			sendSSE(w, "context", renderContextFragmentHTML(result))
			sendSSE(w, "graph", s.renderGraphHTML(r.Context(), source, result, events))

			// Send all existing events as one batch
			var buf bytes.Buffer
			for _, evt := range events {
				writeEventHTML(&buf, evt)
			}
			if buf.Len() > 0 {
				sendSSE(w, "events", buf.String())
			} else {
				sendSSE(w, "events", `<div class="no-data">Waiting for events...</div>`)
			}

			sentEventCount = len(events)
			lastStatus = status
			lastQuestionCount = pendingCount
			lastGraphRender = time.Now()
			first = false
			flusher.Flush()
		} else {
			eventsChanged := len(events) != sentEventCount
			statusChanged := status != lastStatus
			questionsChanged := pendingCount != lastQuestionCount

			if eventsChanged {
				// Send new events individually
				for i := sentEventCount; i < len(events); i++ {
					var evtBuf bytes.Buffer
					writeEventHTML(&evtBuf, events[i])
					sendSSE(w, "event-item", evtBuf.String())
				}
				sentEventCount = len(events)

				// Update aggregated panels
				sendSSE(w, "tools", renderToolsFragmentHTML(events))
				sendSSE(w, "tokens", renderTokensFragmentHTML(events))
				sendSSE(w, "active-node", renderActiveNodeFragmentHTML(events))
				sendSSE(w, "context", renderContextFragmentHTML(result))
				flusher.Flush()
			}

			if statusChanged {
				sendSSE(w, "status", renderStatusFragmentHTML(status))
				lastStatus = status
				flusher.Flush()
			}

			if questionsChanged {
				sendSSE(w, "questions", renderQuestionsFragmentHTML(id, questions))
				lastQuestionCount = pendingCount
				flusher.Flush()
			}

			// Graph rendering is expensive; throttle to every 3s
			if time.Since(lastGraphRender) > 3*time.Second && (eventsChanged || statusChanged) {
				sendSSE(w, "graph", s.renderGraphHTML(r.Context(), source, result, events))
				lastGraphRender = time.Now()
				flusher.Flush()
			}
		}

		// Pipeline done — send final event and close
		if status == "completed" || status == "failed" || status == "cancelled" {
			sendSSE(w, "pipeline-done", `{"status":"`+status+`"}`)
			flusher.Flush()
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
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
		currentEvents := make([]EngineEvent, len(run.Events))
		copy(currentEvents, run.Events)
		status := run.Status
		run.mu.RUnlock()

		// Send any new events as HTML fragments
		for sentCount < len(currentEvents) {
			var buf bytes.Buffer
			writeEventHTML(&buf, currentEvents[sentCount])
			sendSSE(w, "", buf.String())
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
