// ABOUTME: StreamModel is an inline Bubble Tea model for streaming pipeline progress to the terminal.
// ABOUTME: Displays node execution status, elapsed times, spinners, and optional verbose agent events without alt-screen.
package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/2389-research/mammoth/attractor"
	tea "github.com/charmbracelet/bubbletea"
)

// maxAgentLines limits the number of verbose agent log lines retained per node.
const maxAgentLines = 5

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
			m.completed++
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
	source  string
	ctx     context.Context
	cancel  context.CancelFunc
	verbose bool

	humanGate HumanGateModel

	// Node tracking
	nodeOrder []string                   // topological order for display
	statuses  map[string]NodeStatus      // per-node execution state
	startedAt map[string]time.Time       // per-node start time
	durations map[string]time.Duration   // per-node elapsed duration

	// Agent events (verbose mode)
	agentLines map[string][]string // nodeID → recent agent log lines

	// Spinner
	spinnerIdx int

	// Pipeline state
	pipelineStart time.Time
	completed     int
	total         int
	done          bool
	err           error
	resultCh      chan PipelineResultMsg // captures result for caller

	// Resume state
	resumeInfo *ResumeInfo // non-nil when resuming from a previous run
	resumeCmd  func() tea.Cmd // override pipeline command for resume

	width int
}

// NewStreamModel creates a StreamModel for inline pipeline progress display.
// It computes a topological node order using Kahn's algorithm and initializes
// all nodes as pending. Optional StreamOption funcs configure resume behavior.
func NewStreamModel(
	graph *attractor.Graph,
	engine *attractor.Engine,
	source string,
	ctx context.Context,
	verbose bool,
	opts ...StreamOption,
) StreamModel {
	cancel := func() {} // no-op default; caller may replace via ctx
	if ctx == nil {
		ctx = context.Background()
	}

	nodeOrder := topologicalOrder(graph)
	total := len(nodeOrder)

	statuses := make(map[string]NodeStatus, total)
	for _, id := range nodeOrder {
		statuses[id] = NodePending
	}

	m := StreamModel{
		graph:      graph,
		engine:     engine,
		source:     source,
		ctx:        ctx,
		cancel:     cancel,
		verbose:    verbose,
		humanGate:  NewHumanGateModel(),
		nodeOrder:  nodeOrder,
		statuses:   statuses,
		startedAt:  make(map[string]time.Time),
		durations:  make(map[string]time.Duration),
		agentLines: make(map[string][]string),
		total:      total,
		resultCh:   make(chan PipelineResultMsg, 1),
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
		pipelineCmd = RunPipelineCmd(m.ctx, m.engine, m.source)
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
		b.WriteString(fmt.Sprintf("🦣 mammoth — %s (resuming from %s)\n\n", m.source, m.resumeInfo.ResumedFrom))
	} else {
		b.WriteString(fmt.Sprintf("🦣 mammoth — %s\n\n", m.source))
	}

	// Node list
	for _, id := range m.nodeOrder {
		status := m.statuses[id]
		label := m.nodeLabelByID(id)
		line := m.renderNodeLine(id, label, status)
		b.WriteString(line)
		b.WriteString("\n")

		// Verbose: show agent lines under running nodes
		if m.verbose && status == NodeRunning {
			if lines, ok := m.agentLines[id]; ok {
				for _, al := range lines {
					b.WriteString(PendingStyle.Render(fmt.Sprintf("      %s", al)))
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

	return b.String()
}

// handleEngineEvent processes engine lifecycle events.
func (m StreamModel) handleEngineEvent(msg EngineEventMsg) (tea.Model, tea.Cmd) {
	evt := msg.Event

	switch evt.Type {
	case attractor.EventPipelineStarted:
		m.pipelineStart = time.Now()

	case attractor.EventStageStarted:
		m.statuses[evt.NodeID] = NodeRunning
		m.startedAt[evt.NodeID] = time.Now()

	case attractor.EventStageCompleted:
		m.statuses[evt.NodeID] = NodeCompleted
		m.completed++
		if start, ok := m.startedAt[evt.NodeID]; ok {
			m.durations[evt.NodeID] = time.Since(start)
		}

	case attractor.EventStageFailed:
		m.statuses[evt.NodeID] = NodeFailed
		if start, ok := m.startedAt[evt.NodeID]; ok {
			m.durations[evt.NodeID] = time.Since(start)
		}

	case attractor.EventAgentToolCallStart:
		if m.verbose {
			toolName, _ := evt.Data["tool_name"]
			line := fmt.Sprintf("tool: %v", toolName)
			m.appendAgentLine(evt.NodeID, line)
		}

	case attractor.EventAgentToolCallEnd:
		if m.verbose {
			toolName, _ := evt.Data["tool_name"]
			durMs, _ := evt.Data["duration_ms"]
			line := fmt.Sprintf("tool: %v done (%vms)", toolName, durMs)
			m.appendAgentLine(evt.NodeID, line)
		}

	case attractor.EventAgentLLMTurn:
		if m.verbose {
			if inputTok, ok := evt.Data["input_tokens"]; ok {
				outputTok, _ := evt.Data["output_tokens"]
				line := fmt.Sprintf("llm turn (in:%v out:%v)", inputTok, outputTok)
				m.appendAgentLine(evt.NodeID, line)
			} else {
				tokens, _ := evt.Data["tokens"]
				line := fmt.Sprintf("llm turn (%v tokens)", tokens)
				m.appendAgentLine(evt.NodeID, line)
			}
		}

	case attractor.EventAgentSteering:
		if m.verbose {
			msg, _ := evt.Data["message"]
			line := fmt.Sprintf("steering: %v", msg)
			m.appendAgentLine(evt.NodeID, line)
		}
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
		return m, tea.Quit
	}

	return m, nil
}

// renderNodeLine renders a single node's status line.
func (m StreamModel) renderNodeLine(id, label string, status NodeStatus) string {
	switch status {
	case NodeRunning:
		frame := SpinnerFrames[m.spinnerIdx%len(SpinnerFrames)]
		return RunningStyle.Render(fmt.Sprintf("  %s %s", frame, label)) +
			RunningStyle.Render("  running...")

	case NodeCompleted:
		dur := m.durations[id]
		if dur < 0 {
			// Sentinel value: node was completed in a previous run
			return PendingStyle.Render(fmt.Sprintf("  ✓ %s", label)) +
				PendingStyle.Render("  (previous run)")
		}
		durStr := formatDuration(dur)
		return CompletedStyle.Render(fmt.Sprintf("  ✓ %s", label)) +
			CompletedStyle.Render(fmt.Sprintf("  %s", durStr))

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

// renderProgressLine renders the bottom progress/completion line.
func (m StreamModel) renderProgressLine() string {
	elapsed := time.Since(m.pipelineStart)
	elapsedStr := formatDuration(elapsed)

	if m.done {
		if m.err != nil {
			return FailedStyle.Render(
				fmt.Sprintf("  ✗ %d/%d complete · %s · FAILED: %v", m.completed, m.total, elapsedStr, m.err))
		}
		return CompletedStyle.Render(
			fmt.Sprintf("  ✓ %d/%d complete · %s", m.completed, m.total, elapsedStr))
	}

	return PendingStyle.Render(
		fmt.Sprintf("  %d/%d complete · %s elapsed", m.completed, m.total, elapsedStr))
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

// appendAgentLine adds a verbose agent log line for a node, keeping a bounded buffer.
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
