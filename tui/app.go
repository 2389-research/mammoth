// ABOUTME: Top-level Bubble Tea AppModel that orchestrates all TUI sub-panels into a unified layout.
// ABOUTME: Implements tea.Model (Init, Update, View) and routes messages to graph, detail, log, status bar, and human gate panels.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/2389-research/mammoth/attractor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FocusTarget indicates which panel currently has keyboard focus.
type FocusTarget int

const (
	FocusGraph FocusTarget = iota
	FocusLog
)

// AppModel is the top-level Bubble Tea model that composes all TUI sub-panels
// and routes messages between them.
type AppModel struct {
	graph     GraphPanelModel
	detail    DetailPanelModel
	log       LogPanelModel
	statusBar StatusBarModel
	humanGate HumanGateModel

	engine *attractor.Engine
	source string          // DOT source to execute
	ctx    context.Context // cancellation context for engine execution

	focus     FocusTarget
	done      bool  // pipeline finished
	err       error // pipeline error (if any)
	completed int   // count of completed nodes
	width     int
	height    int
}

// NewAppModel creates an AppModel with all sub-models initialized from the given graph.
func NewAppModel(g *attractor.Graph, engine *attractor.Engine, source string, ctx context.Context) AppModel {
	totalNodes := 0
	graphName := ""
	if g != nil {
		totalNodes = len(g.Nodes)
		graphName = g.Name
	}

	return AppModel{
		graph:     NewGraphPanelModel(g),
		detail:    NewDetailPanelModel(),
		log:       NewLogPanelModel(200),
		statusBar: NewStatusBarModel(graphName, totalNodes),
		humanGate: NewHumanGateModel(),
		engine:    engine,
		source:    source,
		ctx:       ctx,
		focus:     FocusGraph,
	}
}

// HumanGate returns a pointer to the AppModel's HumanGateModel for external
// wiring (e.g. attaching it as the engine's Interviewer). Channels in the
// HumanGateModel are reference types, so the pointer remains valid even after
// Bubble Tea copies the AppModel by value.
func (m *AppModel) HumanGate() *HumanGateModel {
	return &m.humanGate
}

// Init implements tea.Model. Returns a batch of initial commands to start the
// pipeline, listen for human gate requests, and begin the tick loop.
func (m AppModel) Init() tea.Cmd {
	// statusBar.Start() is called in handleEngineEvent on EventPipelineStarted,
	// which works correctly via the returned mutated model. Calling it here on
	// a value receiver would discard the mutation.
	return tea.Batch(
		RunPipelineCmd(m.ctx, m.engine, m.source),
		WaitForHumanGateCmd(m.humanGate.RequestChan()),
		TickCmd(100*time.Millisecond),
	)
}

// Update implements tea.Model. Routes incoming messages to the appropriate
// sub-panel and returns the updated model with any follow-up commands.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)

	case EngineEventMsg:
		return m.handleEngineEvent(msg)

	case PipelineResultMsg:
		return m.handlePipelineResult(msg)

	case TickMsg:
		return m.handleTick(msg)

	case HumanGateRequestMsg:
		return m.handleHumanGateRequest(msg)

	case tea.KeyMsg:
		return m.handleKeyMsg(msg)
	}

	return m, nil
}

// View implements tea.Model. Renders the full TUI layout with all panels.
func (m AppModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	// Minimum terminal size guard to prevent layout overflow
	if m.width < 40 || m.height < 10 {
		return fmt.Sprintf("Terminal too small (%dx%d). Minimum: 40x10.", m.width, m.height)
	}

	// Layout calculations
	statusBarHeight := 1
	graphHeight := (m.height - statusBarHeight) * 40 / 100
	if graphHeight < 3 {
		graphHeight = 3
	}
	bottomHeight := m.height - statusBarHeight - graphHeight
	if bottomHeight < 3 {
		bottomHeight = 3
	}

	detailWidth := m.width * 40 / 100
	if detailWidth < 10 {
		detailWidth = 10
	}
	logWidth := m.width - detailWidth
	if logWidth < 10 {
		logWidth = 10
	}

	// Update panel sizes
	m.graph.SetWidth(m.width)
	m.detail.SetSize(detailWidth, bottomHeight)
	m.log.SetSize(logWidth, bottomHeight)
	m.statusBar.SetWidth(m.width)

	// Render graph panel (top, full width)
	graphView := m.graph.View()

	// Render bottom section: detail (or human gate) on left, log on right
	var leftPanel string
	if m.humanGate.IsActive() {
		leftPanel = m.humanGate.View()
	} else {
		leftPanel = m.detail.View()
	}
	rightPanel := m.log.View()

	bottomView := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)

	// Render status bar with done info
	var statusView string
	if m.done {
		if m.err != nil {
			statusView = m.statusBar.View() + " " + FailedStyle.Render(fmt.Sprintf("FAILED: %v", m.err))
		} else {
			statusView = m.statusBar.View() + " " + CompletedStyle.Render("DONE")
		}
	} else {
		statusView = m.statusBar.View()
	}

	// Assemble full view
	var b strings.Builder
	b.WriteString(graphView)
	b.WriteString("\n")
	b.WriteString(bottomView)
	b.WriteString("\n")
	b.WriteString(statusView)

	return b.String()
}

// handleWindowSize updates dimensions on all panels.
func (m AppModel) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	return m, nil
}

// handleEngineEvent routes engine lifecycle events to the appropriate sub-panels.
func (m AppModel) handleEngineEvent(msg EngineEventMsg) (tea.Model, tea.Cmd) {
	evt := msg.Event

	// Always append to log panel
	m.log.Append(evt)

	switch evt.Type {
	case attractor.EventPipelineStarted:
		m.statusBar.Start()

	case attractor.EventStageStarted:
		m.graph.SetNodeStatus(evt.NodeID, NodeRunning)
		m.statusBar.SetActiveNode(evt.NodeID)

		// Build node detail from graph metadata
		detail := m.buildNodeDetail(evt.NodeID, NodeRunning)
		m.detail.SetActiveNode(detail)

	case attractor.EventStageCompleted:
		m.graph.SetNodeStatus(evt.NodeID, NodeCompleted)
		m.completed++
		m.statusBar.SetCompleted(m.completed)

	case attractor.EventStageFailed:
		m.graph.SetNodeStatus(evt.NodeID, NodeFailed)

	case attractor.EventStageRetrying:
		// Logged only (already appended above)
	}

	return m, nil
}

// handlePipelineResult marks the pipeline as done and stores any error.
func (m AppModel) handlePipelineResult(msg PipelineResultMsg) (tea.Model, tea.Cmd) {
	m.done = true
	m.err = msg.Err
	m.statusBar.SetActiveNode("")
	m.detail.Clear()
	return m, nil
}

// handleTick advances the spinner and returns a new tick if the pipeline is still running.
func (m AppModel) handleTick(_ TickMsg) (tea.Model, tea.Cmd) {
	m.graph.AdvanceSpinner()
	if m.done {
		return m, nil
	}
	return m, TickCmd(100 * time.Millisecond)
}

// handleHumanGateRequest activates the human gate dialog.
func (m AppModel) handleHumanGateRequest(msg HumanGateRequestMsg) (tea.Model, tea.Cmd) {
	m.humanGate.SetActive(msg.Question, msg.Options)
	return m, nil
}

// handleKeyMsg processes keyboard input, routing to human gate or app-level shortcuts.
func (m AppModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// When human gate is active, route keys there
	if m.humanGate.IsActive() {
		if msg.Type == tea.KeyEnter {
			m.humanGate.Submit()
			return m, WaitForHumanGateCmd(m.humanGate.RequestChan())
		}
		m.humanGate = m.humanGate.Update(msg)
		return m, nil
	}

	// App-level key bindings
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.focus = m.nextFocus()
		m.log.SetFocused(m.focus == FocusLog)
		return m, nil
	}

	return m, nil
}

// nextFocus cycles the focus target between graph and log.
func (m AppModel) nextFocus() FocusTarget {
	switch m.focus {
	case FocusGraph:
		return FocusLog
	case FocusLog:
		return FocusGraph
	default:
		return FocusGraph
	}
}

// buildNodeDetail constructs a NodeDetail from graph metadata for the given node.
func (m AppModel) buildNodeDetail(nodeID string, status NodeStatus) NodeDetail {
	detail := NodeDetail{
		Name:   nodeID,
		Status: status,
	}

	if m.graph.graph != nil {
		node := m.graph.graph.FindNode(nodeID)
		if node != nil {
			if label, ok := node.Attrs["label"]; ok && label != "" {
				detail.Name = label
			}
			detail.HandlerType = attractor.ShapeToHandlerType(node.Attrs["shape"])
		}
	}

	return detail
}
