// ABOUTME: StreamModel is an inline Bubble Tea model for streaming pipeline progress to the terminal.
// ABOUTME: Displays node execution status, elapsed times, spinners, and agent activity feed (tool calls, LLM turns, text) without alt-screen.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/2389-research/mammoth/attractor"
	tea "github.com/charmbracelet/bubbletea"
)

// maxAgentLines limits the number of agent log lines retained per node.
const maxAgentLines = 20

// ResumeInfo holds state from a previous run that is being resumed.
type ResumeInfo struct {
	ResumedFrom   string   // node label where we're resuming from
	PreviousNodes []string // nodes completed in the previous run
}

// StreamOption configures optional StreamModel behavior.
type StreamOption func(*StreamModel)

// WithResumeInfo configures the StreamModel for a resume scenario, pre-marking
// previously completed nodes and showing a resume header.
func WithResumeInfo(info *ResumeInfo) StreamOption {
	return func(m *StreamModel) {
		if info == nil {
			return
		}
		m.resumeInfo = info
		// Pre-mark previous nodes as completed with a special duration marker
		for _, nodeID := range info.PreviousNodes {
			m.statuses[nodeID] = NodeCompleted
			m.durations[nodeID] = -1 // sentinel: signals "(previous run)" in view
		}
	}
}

// StreamModel is an inline (non-alt-screen) Bubble Tea model that displays
// pipeline progress as a streaming list of nodes with status indicators,
// elapsed times, and an optional verbose agent event feed.
type StreamModel struct {
	graph   *attractor.Graph
	engine  *attractor.Engine
	title   string
	ctx     context.Context
	cancel  context.CancelFunc
	verbose bool // reserved for future extra-detail output; agent activity always shown

	humanGate HumanGateModel

	// Node tracking
	nodeOrder []string                 // topological order for display
	statuses  map[string]NodeStatus    // per-node execution state
	startedAt map[string]time.Time     // per-node start time
	durations map[string]time.Duration // per-node elapsed duration

	// Agent event activity feed (shown under running nodes)
	agentLines map[string][]string // nodeID → recent agent log lines

	// Token and model tracking
	nodeTokens  map[string]int    // per-node accumulated token count
	nodeModels  map[string]string // per-node model name (from stage.completed)
	totalTokens int               // running total across all nodes

	// Tool call tracking
	nodeToolCalls  map[string]int // per-node tool call count
	totalToolCalls int            // running total tool calls

	// Output directory (from engine's workdir)
	workdir string

	// Spinner
	spinnerIdx int

	// Pipeline state
	pipelineStart time.Time
	total         int
	done          bool
	err           error
	resultCh      chan PipelineResultMsg // captures result for caller

	// Resume state
	resumeInfo *ResumeInfo    // non-nil when resuming from a previous run
	resumeCmd  func() tea.Cmd // override pipeline command for resume

	width int
}

// NewStreamModel creates a StreamModel for inline pipeline progress display.
// It computes a topological node order using Kahn's algorithm and initializes
// all nodes as pending. Optional StreamOption funcs configure resume behavior.
func NewStreamModel(
	graph *attractor.Graph,
	engine *attractor.Engine,
	title string,
	ctx context.Context,
	verbose bool,
	opts ...StreamOption,
) StreamModel {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	nodeOrder := topologicalOrder(graph)
	total := len(nodeOrder)

	statuses := make(map[string]NodeStatus, total)
	for _, id := range nodeOrder {
		statuses[id] = NodePending
	}

	m := StreamModel{
		graph:         graph,
		engine:        engine,
		title:         title,
		ctx:           ctx,
		cancel:        cancel,
		verbose:       verbose,
		humanGate:     NewHumanGateModel(),
		nodeOrder:     nodeOrder,
		statuses:      statuses,
		startedAt:     make(map[string]time.Time),
		durations:     make(map[string]time.Duration),
		agentLines:    make(map[string][]string),
		nodeTokens:    make(map[string]int),
		nodeModels:    make(map[string]string),
		nodeToolCalls: make(map[string]int),
		total:         total,
		resultCh:      make(chan PipelineResultMsg, 1),
	}

	for _, opt := range opts {
		opt(&m)
	}

	return m
}

// ResultCh returns a channel that receives the pipeline result after the
// program exits. The caller should read from this after tea.Program.Run()
// completes.
func (m *StreamModel) ResultCh() <-chan PipelineResultMsg {
	return m.resultCh
}

// HumanGate returns a pointer to the StreamModel's HumanGateModel for external
// wiring (e.g. attaching it as the engine's Interviewer).
func (m *StreamModel) HumanGate() *HumanGateModel {
	return &m.humanGate
}

// SetResumeCmd replaces the default RunPipelineCmd with a custom command
// for resume scenarios. Must be called before the program starts.
func (m *StreamModel) SetResumeCmd(fn func() tea.Cmd) {
	m.resumeCmd = fn
}

// Init implements tea.Model. Returns a batch of initial commands to start the
// pipeline, listen for human gate requests, and begin the tick loop.
func (m StreamModel) Init() tea.Cmd {
	var pipelineCmd tea.Cmd
	if m.resumeCmd != nil {
		pipelineCmd = m.resumeCmd()
	} else {
		pipelineCmd = RunPipelineGraphCmd(m.ctx, m.engine, m.graph)
	}
	return tea.Batch(
		pipelineCmd,
		WaitForHumanGateCmd(m.humanGate.RequestChan()),
		TickCmd(100*time.Millisecond),
	)
}

// Update implements tea.Model. Routes incoming messages to appropriate handlers.
func (m StreamModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case EngineEventMsg:
		return m.handleEngineEvent(msg)

	case PipelineResultMsg:
		return m.handlePipelineResult(msg)

	case TickMsg:
		return m.handleTick()

	case HumanGateRequestMsg:
		m.humanGate.SetActive(msg.Question, msg.Options)
		if msg.NodeID != "" {
			label := m.nodeLabelByID(msg.NodeID)
			position := ""
			if idx := m.nodeIndexByID(msg.NodeID); idx >= 0 {
				position = fmt.Sprintf("step %d/%d", idx+1, len(m.nodeOrder))
			}
			m.humanGate.SetNodeContext(msg.NodeID, label, position)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	return m, nil
}

// View implements tea.Model. Renders the inline streaming progress display.
func (m StreamModel) View() string {
	var b strings.Builder

	// Header — show resume info when resuming
	if m.resumeInfo != nil && m.resumeInfo.ResumedFrom != "" {
		b.WriteString(fmt.Sprintf("🦣 mammoth — %s (resuming from %s)\n\n", m.title, m.resumeInfo.ResumedFrom))
	} else {
		b.WriteString(fmt.Sprintf("🦣 mammoth — %s\n\n", m.title))
	}

	// Node list
	for _, id := range m.nodeOrder {
		status := m.statuses[id]
		label := m.nodeLabelByID(id)
		line := m.renderNodeLine(id, label, status)
		b.WriteString(line)
		b.WriteString("\n")

		// Show agent activity lines under running nodes
		if status == NodeRunning {
			if lines, ok := m.agentLines[id]; ok {
				for _, al := range lines {
					b.WriteString(fmt.Sprintf("      %s", al))
					b.WriteString("\n")
				}
			}
		}
	}

	// Blank line before progress/human gate
	b.WriteString("\n")

	// Human gate view (if active) or progress line
	if m.humanGate.IsActive() {
		b.WriteString(m.humanGate.View())
		b.WriteString("\n")
	} else {
		b.WriteString(m.renderProgressLine())
		b.WriteString("\n")
	}

	// Summary block after pipeline completes
	if m.done {
		b.WriteString(m.renderSummary())
	}

	return b.String()
}

// handleEngineEvent processes engine lifecycle events.
func (m StreamModel) handleEngineEvent(msg EngineEventMsg) (tea.Model, tea.Cmd) {
	evt := msg.Event

	switch evt.Type {
	case attractor.EventPipelineStarted:
		m.pipelineStart = time.Now()
		if evt.Data != nil {
			if wd, ok := evt.Data["workdir"].(string); ok {
				m.workdir = wd
			}
		}

	case attractor.EventStageStarted:
		m.statuses[evt.NodeID] = NodeRunning
		m.startedAt[evt.NodeID] = time.Now()

	case attractor.EventStageCompleted:
		m.statuses[evt.NodeID] = NodeCompleted
		if start, ok := m.startedAt[evt.NodeID]; ok {
			m.durations[evt.NodeID] = time.Since(start)
		}
		// Capture model name and token counts from codergen.* data
		if evt.Data != nil {
			if model, ok := evt.Data["codergen.model"]; ok {
				if s, ok := model.(string); ok {
					m.nodeModels[evt.NodeID] = s
				}
			}
			// Backfill tokens from stage completion if EventAgentLLMTurn
			// didn't already provide them (e.g. claude-code backend error paths).
			if m.nodeTokens[evt.NodeID] == 0 {
				if tok, ok := evt.Data["codergen.tokens_used"]; ok {
					tokens := toInt(tok)
					m.nodeTokens[evt.NodeID] += tokens
					m.totalTokens += tokens
				}
			}
		}

	case attractor.EventStageFailed:
		m.statuses[evt.NodeID] = NodeFailed
		if start, ok := m.startedAt[evt.NodeID]; ok {
			m.durations[evt.NodeID] = time.Since(start)
		}

	case attractor.EventAgentToolCallStart:
		m.nodeToolCalls[evt.NodeID]++
		m.totalToolCalls++
		if evt.Data == nil {
			break
		}
		toolName := fmt.Sprintf("%v", evt.Data["tool_name"])
		line := LogAgentToolStyle.Render(fmt.Sprintf("▸ %s", toolName))
		// Show formatted arguments for common tools
		if args, ok := evt.Data["arguments"].(string); ok && args != "" {
			detail := formatToolArgs(toolName, args)
			if detail != "" {
				line += PendingStyle.Render(fmt.Sprintf(" %s", detail))
			}
		}
		m.appendAgentLine(evt.NodeID, line)

	case attractor.EventAgentToolCallEnd:
		if evt.Data == nil {
			break
		}
		toolName := fmt.Sprintf("%v", evt.Data["tool_name"])
		durMs := evt.Data["duration_ms"]
		// Show output snippet if available
		if snippet, ok := evt.Data["output_snippet"].(string); ok && snippet != "" {
			short := truncateOneLine(snippet, 80)
			line := LogAgentToolStyle.Render(fmt.Sprintf("  %s", toolName)) +
				PendingStyle.Render(fmt.Sprintf(" → %s (%vms)", short, durMs))
			m.appendAgentLine(evt.NodeID, line)
		} else {
			line := LogAgentToolStyle.Render(fmt.Sprintf("  %s done", toolName)) +
				PendingStyle.Render(fmt.Sprintf(" (%vms)", durMs))
			m.appendAgentLine(evt.NodeID, line)
		}

	case attractor.EventAgentTextDelta:
		if evt.Data == nil {
			break
		}
		if text, ok := evt.Data["text"].(string); ok && text != "" {
			// Show a truncated preview of streaming agent text
			short := truncateOneLine(text, 100)
			if short != "" {
				line := PendingStyle.Render(fmt.Sprintf("  %s", short))
				m.appendAgentLine(evt.NodeID, line)
			}
		}

	case attractor.EventAgentLLMTurn:
		if evt.Data == nil {
			break
		}
		// Accumulate tokens
		turnTokens := 0
		if totalTok, ok := evt.Data["total_tokens"]; ok && toInt(totalTok) > 0 {
			turnTokens = toInt(totalTok)
		} else {
			if inputTok, ok := evt.Data["input_tokens"]; ok {
				turnTokens += toInt(inputTok)
			}
			if outputTok, ok := evt.Data["output_tokens"]; ok {
				turnTokens += toInt(outputTok)
			}
		}
		if turnTokens == 0 {
			if tok, ok := evt.Data["tokens"]; ok {
				turnTokens = toInt(tok)
			}
		}
		m.nodeTokens[evt.NodeID] += turnTokens
		m.totalTokens += turnTokens

		if inputTok, ok := evt.Data["input_tokens"]; ok {
			outputTok := evt.Data["output_tokens"]
			line := LogAgentTurnStyle.Render(fmt.Sprintf("  llm turn (in:%v out:%v)", inputTok, outputTok))
			m.appendAgentLine(evt.NodeID, line)
		} else {
			tokens := evt.Data["tokens"]
			line := LogAgentTurnStyle.Render(fmt.Sprintf("  llm turn (%v tokens)", tokens))
			m.appendAgentLine(evt.NodeID, line)
		}

	case attractor.EventAgentSteering:
		if evt.Data == nil {
			break
		}
		msg := evt.Data["message"]
		line := LogAgentSteeringStyle.Render(fmt.Sprintf("  steering: %v", msg))
		m.appendAgentLine(evt.NodeID, line)

	case attractor.EventAgentLoopDetected:
		if evt.Data == nil {
			break
		}
		msg := evt.Data["message"]
		line := LogRetryStyle.Render(fmt.Sprintf("  ⚠ loop detected: %v", msg))
		m.appendAgentLine(evt.NodeID, line)
	}

	return m, nil
}

// handlePipelineResult marks the pipeline as done and writes the result to the channel.
func (m StreamModel) handlePipelineResult(msg PipelineResultMsg) (tea.Model, tea.Cmd) {
	m.done = true
	m.err = msg.Err

	// Non-blocking write to result channel
	select {
	case m.resultCh <- msg:
	default:
	}

	return m, tea.Quit
}

// handleTick advances the spinner and returns a new tick if still running.
func (m StreamModel) handleTick() (tea.Model, tea.Cmd) {
	m.spinnerIdx++
	if m.done {
		return m, nil
	}
	return m, TickCmd(100 * time.Millisecond)
}

// handleKeyMsg processes keyboard input.
func (m StreamModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When human gate is active, route keys there
	if m.humanGate.IsActive() {
		if msg.Type == tea.KeyEnter {
			m.humanGate.Submit()
			return m, WaitForHumanGateCmd(m.humanGate.RequestChan())
		}
		m.humanGate = m.humanGate.Update(msg)
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		m.cancel()
		m.done = true
		m.err = context.Canceled
		return m, tea.Quit
	}

	return m, nil
}

// renderNodeLine renders a single node's status line.
func (m StreamModel) renderNodeLine(id, label string, status NodeStatus) string {
	switch status {
	case NodeRunning:
		frame := SpinnerFrames[m.spinnerIdx%len(SpinnerFrames)]
		suffix := "  running..."
		if tok := m.nodeTokens[id]; tok > 0 {
			suffix = fmt.Sprintf("  running...  %s tok", formatTokenCount(tok))
		}
		return RunningStyle.Render(fmt.Sprintf("  %s %s", frame, label)) +
			RunningStyle.Render(suffix)

	case NodeCompleted:
		dur := m.durations[id]
		if dur < 0 {
			// Sentinel value: node was completed in a previous run
			return PendingStyle.Render(fmt.Sprintf("  ✓ %s", label)) +
				PendingStyle.Render("  (previous run)")
		}
		durStr := formatDuration(dur)
		extra := ""
		if model := m.nodeModels[id]; model != "" {
			extra += fmt.Sprintf(" · %s", shortModelName(model))
		}
		if tok := m.nodeTokens[id]; tok > 0 {
			extra += fmt.Sprintf(" · %s tok", formatTokenCount(tok))
		}
		return CompletedStyle.Render(fmt.Sprintf("  ✓ %s", label)) +
			CompletedStyle.Render(fmt.Sprintf("  %s%s", durStr, extra))

	case NodeFailed:
		dur := m.durations[id]
		durStr := formatDuration(dur)
		return FailedStyle.Render(fmt.Sprintf("  ✗ %s", label)) +
			FailedStyle.Render(fmt.Sprintf("  failed (%s)", durStr))

	case NodeSkipped:
		return SkippedStyle.Render(fmt.Sprintf("  – %s", label)) +
			SkippedStyle.Render("  skipped")

	default: // NodePending
		return PendingStyle.Render(fmt.Sprintf("    %s", label))
	}
}

// completedCount returns the number of nodes currently in completed status.
func (m StreamModel) completedCount() int {
	n := 0
	for _, s := range m.statuses {
		if s == NodeCompleted {
			n++
		}
	}
	return n
}

// currentNodeLabel returns the label of the currently running node, or empty string.
func (m StreamModel) currentNodeLabel() string {
	for _, id := range m.nodeOrder {
		if m.statuses[id] == NodeRunning {
			return m.nodeLabelByID(id)
		}
	}
	return ""
}

// renderProgressLine renders the bottom progress/completion line.
func (m StreamModel) renderProgressLine() string {
	elapsed := time.Since(m.pipelineStart)
	elapsedStr := formatDuration(elapsed)
	completed := m.completedCount()

	tokenSuffix := ""
	if m.totalTokens > 0 {
		tokenSuffix = fmt.Sprintf(" · %s tokens", formatTokenCount(m.totalTokens))
	}

	if m.done {
		if m.err != nil {
			return FailedStyle.Render(
				fmt.Sprintf("  ✗ %d/%d complete · %s · FAILED: %v", completed, m.total, elapsedStr, m.err))
		}
		return CompletedStyle.Render(
			fmt.Sprintf("  ✓ %d/%d complete · %s%s", completed, m.total, elapsedStr, tokenSuffix))
	}

	nodeSuffix := ""
	if label := m.currentNodeLabel(); label != "" {
		nodeSuffix = fmt.Sprintf(" · %s", label)
	}

	return PendingStyle.Render(
		fmt.Sprintf("  %d/%d complete · %s elapsed%s%s", completed, m.total, elapsedStr, tokenSuffix, nodeSuffix))
}

// renderSummary renders the post-run summary block with node counts, models,
// tokens, tool calls, and total duration.
func (m StreamModel) renderSummary() string {
	var b strings.Builder

	// Count passed, failed, ran — exclude nodes from a previous run (duration sentinel < 0)
	passed, failed, ran := 0, 0, 0
	for _, id := range m.nodeOrder {
		status := m.statuses[id]
		switch status {
		case NodeCompleted:
			if m.durations[id] < 0 {
				// Node was completed in a previous run (resume); skip
				continue
			}
			passed++
			ran++
		case NodeFailed:
			failed++
			ran++
		case NodeSkipped:
			// skipped nodes were not run
		default:
			// pending nodes that never started
		}
	}

	// Collect unique models with node counts
	modelCounts := make(map[string]int)
	for _, model := range m.nodeModels {
		short := shortModelName(model)
		modelCounts[short]++
	}
	var modelParts []string
	// Sort for deterministic output
	var modelNames []string
	for name := range modelCounts {
		modelNames = append(modelNames, name)
	}
	sort.Strings(modelNames)
	for _, name := range modelNames {
		count := modelCounts[name]
		noun := "nodes"
		if count == 1 {
			noun = "node"
		}
		modelParts = append(modelParts, fmt.Sprintf("%s (%d %s)", name, count, noun))
	}

	// Duration
	elapsed := time.Since(m.pipelineStart)
	elapsedStr := formatDuration(elapsed)

	// Separator line
	separator := "── Summary ──────────────────────────────"
	separatorStyle := CompletedStyle
	if m.err != nil {
		separatorStyle = FailedStyle
	}
	b.WriteString(fmt.Sprintf("\n  %s\n", separatorStyle.Render(separator)))

	// Nodes line
	b.WriteString(fmt.Sprintf("  %s    %d ran · %d passed · %d failed\n",
		PendingStyle.Render("Nodes"),
		ran, passed, failed))

	// Models line (only if we have model data)
	if len(modelParts) > 0 {
		b.WriteString(fmt.Sprintf("  %s   %s\n",
			PendingStyle.Render("Models"),
			strings.Join(modelParts, " · ")))
	}

	// Tokens line
	if m.totalTokens > 0 {
		b.WriteString(fmt.Sprintf("  %s   %s total\n",
			PendingStyle.Render("Tokens"),
			formatTokenCount(m.totalTokens)))
	}

	// Tools line
	if m.totalToolCalls > 0 {
		b.WriteString(fmt.Sprintf("  %s    %d calls\n",
			PendingStyle.Render("Tools"),
			m.totalToolCalls))
	}

	// Duration line
	b.WriteString(fmt.Sprintf("  %s %s\n",
		PendingStyle.Render("Duration"),
		elapsedStr))

	// Output directory line
	if m.workdir != "" {
		b.WriteString(fmt.Sprintf("  %s   %s\n",
			PendingStyle.Render("Output"),
			m.workdir))
	}

	return b.String()
}

// nodeLabelByID returns the display label for a node, falling back to the node ID.
func (m StreamModel) nodeLabelByID(id string) string {
	if m.graph == nil {
		return id
	}
	node := m.graph.FindNode(id)
	if node == nil {
		return id
	}
	return nodeLabel(node)
}

// nodeIndexByID returns the zero-based index of a node in the topological display order.
// Returns -1 if the node is not found.
func (m StreamModel) nodeIndexByID(id string) int {
	for i, n := range m.nodeOrder {
		if n == id {
			return i
		}
	}
	return -1
}

// appendAgentLine adds an agent log line for a node, keeping a bounded buffer.
func (m *StreamModel) appendAgentLine(nodeID, line string) {
	lines := m.agentLines[nodeID]
	if len(lines) >= maxAgentLines {
		lines = lines[1:]
	}
	m.agentLines[nodeID] = append(lines, line)
}

// topologicalOrder computes a flat topological ordering of graph nodes using
// Kahn's algorithm. Nodes at the same topological level are sorted alphabetically.
func topologicalOrder(graph *attractor.Graph) []string {
	if graph == nil || len(graph.Nodes) == 0 {
		return nil
	}

	// Compute in-degree for each node
	inDegree := make(map[string]int)
	for _, id := range graph.NodeIDs() {
		inDegree[id] = 0
	}
	for _, edge := range graph.Edges {
		inDegree[edge.To]++
	}

	// Collect zero in-degree nodes as the first frontier
	var queue []string
	for _, id := range graph.NodeIDs() {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	sort.Strings(queue)

	var order []string

	for len(queue) > 0 {
		// Process current level
		order = append(order, queue...)

		// Build next frontier
		var next []string
		for _, nodeID := range queue {
			for _, edge := range graph.OutgoingEdges(nodeID) {
				inDegree[edge.To]--
				if inDegree[edge.To] == 0 {
					next = append(next, edge.To)
				}
			}
		}
		sort.Strings(next)
		queue = next
	}

	return order
}

// toInt converts an interface{} value to int, handling common numeric types and pointer variants.
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

// shortModelName extracts a short display name from a full model identifier.
// Claude models are shortened to their tier name (sonnet, opus, haiku).
// Other models are returned as-is.
func shortModelName(model string) string {
	if model == "" {
		return ""
	}
	for _, tier := range []string{"opus", "sonnet", "haiku"} {
		if strings.Contains(model, tier) {
			return tier
		}
	}
	return model
}

// formatTokenCount formats a token count with comma separators for readability.
func formatTokenCount(n int) string {
	if n < 0 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	// Insert commas from the right
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// formatToolArgs extracts a short display string from a tool's JSON arguments.
// For shell/bash tools it shows the command; for file tools it shows the path.
func formatToolArgs(toolName, argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	switch toolName {
	case "shell", "bash", "execute_command":
		if cmd, ok := args["command"].(string); ok {
			return "$ " + truncateOneLine(cmd, 80)
		}
	case "write_file", "read_file", "edit_file":
		if path, ok := args["path"].(string); ok {
			return path
		}
	case "grep":
		if pattern, ok := args["pattern"].(string); ok {
			return fmt.Sprintf("/%s/", truncateOneLine(pattern, 60))
		}
	case "glob":
		if pattern, ok := args["pattern"].(string); ok {
			return pattern
		}
	case "apply_patch":
		return "(patch)"
	}
	return ""
}

// truncateOneLine takes the first line of s, trims whitespace, and truncates to maxLen.
func truncateOneLine(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	// Take first non-empty line
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(trimmed) > maxLen {
			return trimmed[:maxLen-1] + "…"
		}
		return trimmed
	}
	return ""
}

// formatDuration formats a duration as a human-readable string like "0.1s" or "2.3s".
func formatDuration(d time.Duration) string {
	secs := d.Seconds()
	if secs < 10 {
		return fmt.Sprintf("%.1fs", secs)
	}
	if secs < 60 {
		return fmt.Sprintf("%.0fs", secs)
	}
	mins := int(secs) / 60
	remainSecs := int(secs) % 60
	return fmt.Sprintf("%dm%02ds", mins, remainSecs)
}
