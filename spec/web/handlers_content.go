// ABOUTME: Web handlers for document view, activity transcript, chat, and mission ticker.
// ABOUTME: Serves HTMX partials for the content tabs and handles chat/answer form submissions.
package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/server"
)

// chatMaxLength is the maximum allowed length for a chat message in characters.
const chatMaxLength = 10_000

// DocumentData is the view-model for the document partial.
type DocumentData struct {
	SpecID              string
	Title               string
	OneLiner            string
	Goal                string
	GoalHTML            template.HTML
	Description         *string
	DescriptionHTML     *template.HTML
	Constraints         *string
	ConstraintsHTML     *template.HTML
	SuccessCriteria     *string
	SuccessCriteriaHTML *template.HTML
	Risks               *string
	RisksHTML           *template.HTML
	Notes               *string
	NotesHTML           *template.HTML
	Lanes               []LaneData
}

// TranscriptEntry is the view-model for a single transcript message.
type TranscriptEntry struct {
	Sender         string
	SenderLabel    string
	Initial        string
	IsHuman        bool
	IsStep         bool
	IsContinuation bool
	RoleClass      string
	Content        string
	ContentHTML    template.HTML
	Timestamp      string
}

// QuestionData is the view-model for a pending question widget.
type QuestionData struct {
	Type        string // "boolean", "multiple_choice", "freeform"
	QuestionID  string
	Question    string
	Default     *bool
	Choices     []string
	AllowMulti  bool
	Placeholder string
}

// ActivityData is the view-model for the activity partial.
type ActivityData struct {
	SpecID          string
	ContainerID     string
	Transcript      []TranscriptEntry
	PendingQuestion *QuestionData
}

// Document renders the spec as a narrative document with markdown-rendered fields.
func Document(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var data DocumentData
		var found bool
		handle.ReadState(func(s *core.SpecState) {
			if s.Core == nil {
				return
			}
			found = true
			c := s.Core
			data = DocumentData{
				SpecID:   specID.String(),
				Title:    c.Title,
				OneLiner: c.OneLiner,
				Goal:     c.Goal,
				GoalHTML: template.HTML(RenderMarkdown(c.Goal)),
				Lanes:    docCardsByLane(specID.String(), s),
			}
			if c.Description != nil {
				data.Description = c.Description
				html := template.HTML(RenderMarkdown(*c.Description))
				data.DescriptionHTML = &html
			}
			if c.Constraints != nil {
				data.Constraints = c.Constraints
				html := template.HTML(RenderMarkdown(*c.Constraints))
				data.ConstraintsHTML = &html
			}
			if c.SuccessCriteria != nil {
				data.SuccessCriteria = c.SuccessCriteria
				html := template.HTML(RenderMarkdown(*c.SuccessCriteria))
				data.SuccessCriteriaHTML = &html
			}
			if c.Risks != nil {
				data.Risks = c.Risks
				html := template.HTML(RenderMarkdown(*c.Risks))
				data.RisksHTML = &html
			}
			if c.Notes != nil {
				data.Notes = c.Notes
				html := template.HTML(RenderMarkdown(*c.Notes))
				data.NotesHTML = &html
			}
		})

		if !found {
			writeHTMLError(w, http.StatusNotFound, "Spec has no core data.")
			return
		}

		renderer.RenderPartial(w, "document.html", data)
	}
}

// Activity renders the activity partial with the full transcript.
func Activity(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var data ActivityData
		handle.ReadState(func(s *core.SpecState) {
			entries := buildTranscriptEntries(s.Transcript, false)
			data = ActivityData{
				SpecID:          specID.String(),
				ContainerID:     "activity-transcript",
				Transcript:      entries,
				PendingQuestion: questionToViewData(s.PendingQuestion),
			}
		})

		renderer.RenderPartial(w, "activity.html", data)
	}
}

// ActivityTranscript renders only the transcript entries + question widget.
// Used as the SSE refresh target so chat input is preserved during live updates.
func ActivityTranscript(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		containerID := sanitizeContainerID(r.URL.Query().Get("container_id"))
		if containerID == "" {
			containerID = "activity-transcript"
		}

		isChat := containerID == "chat-transcript"

		var data ActivityData
		handle.ReadState(func(s *core.SpecState) {
			entries := buildTranscriptEntries(s.Transcript, isChat)
			markContinuations(entries)
			data = ActivityData{
				SpecID:          specID.String(),
				ContainerID:     containerID,
				Transcript:      entries,
				PendingQuestion: questionToViewData(s.PendingQuestion),
			}
		})

		if isChat {
			renderer.RenderPartial(w, "chat_transcript", data)
		} else {
			renderer.RenderPartial(w, "activity_transcript", data)
		}
	}
}

// ChatPanel renders the full-width Chat tab content.
func ChatPanel(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var data ActivityData
		handle.ReadState(func(s *core.SpecState) {
			entries := buildTranscriptEntries(s.Transcript, true)
			markContinuations(entries)
			data = ActivityData{
				SpecID:          specID.String(),
				ContainerID:     "chat-transcript",
				Transcript:      entries,
				PendingQuestion: questionToViewData(s.PendingQuestion),
			}
		})

		renderer.RenderPartial(w, "chat_panel.html", data)
	}
}

// Chat handles a free-text message from the human, appends it to the transcript,
// and returns the refreshed transcript partial.
func Chat(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		if err := r.ParseForm(); err != nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid form data.")
			return
		}

		message := strings.TrimSpace(r.FormValue("message"))
		if message == "" {
			writeHTMLError(w, http.StatusBadRequest, "Message cannot be empty.")
			return
		}
		if len([]rune(message)) > chatMaxLength {
			writeHTMLError(w, http.StatusBadRequest, fmt.Sprintf("Message too long (max %d characters).", chatMaxLength))
			return
		}

		cmd := core.AppendTranscriptCommand{
			Sender:  "human",
			Content: message,
		}

		if _, err := handle.SendCommand(cmd); err != nil {
			writeHTMLError(w, http.StatusBadRequest, fmt.Sprintf("Failed to send message: %v", err))
			return
		}

		// Wake the agent swarm so the manager responds promptly
		if swarmHandle := state.GetSwarm(specID); swarmHandle != nil {
			swarmHandle.Orchestrator.NotifyHumanMessage()
		}

		// Determine container_id from HX-Target header
		containerID := sanitizeContainerID(
			strings.TrimPrefix(r.Header.Get("HX-Target"), "#"),
		)

		renderTranscriptResponse(w, handle, specID.String(), containerID, renderer)
	}
}

// AnswerQuestion handles a response to a pending question from the user.
func AnswerQuestion(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		if err := r.ParseForm(); err != nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid form data.")
			return
		}

		questionIDStr := r.FormValue("question_id")
		questionID, err := ulid.Parse(questionIDStr)
		if err != nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid question ID.")
			return
		}

		answer := r.FormValue("answer")

		cmd := core.AnswerQuestionCommand{
			QuestionID: questionID,
			Answer:     answer,
		}

		if _, err := handle.SendCommand(cmd); err != nil {
			writeHTMLError(w, http.StatusBadRequest, fmt.Sprintf("Failed to answer: %v", err))
			return
		}

		// Determine container_id from HX-Target header
		containerID := sanitizeContainerID(
			strings.TrimPrefix(r.Header.Get("HX-Target"), "#"),
		)

		renderTranscriptResponse(w, handle, specID.String(), containerID, renderer)
	}
}

// Ticker renders the mission ticker with the last 10 transcript entries.
func Ticker(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var data struct {
			SpecID          string
			TickerEntries   []TranscriptEntry
			PendingQuestion *QuestionData
		}
		handle.ReadState(func(s *core.SpecState) {
			// Take last 10 transcript entries
			entries := s.Transcript
			start := 0
			if len(entries) > 10 {
				start = len(entries) - 10
			}
			data.SpecID = specID.String()
			data.TickerEntries = make([]TranscriptEntry, 0, 10)
			for _, m := range entries[start:] {
				data.TickerEntries = append(data.TickerEntries, toTranscriptEntry(&m))
			}
			data.PendingQuestion = questionToViewData(s.PendingQuestion)
		})

		renderer.RenderPartial(w, "mission_ticker.html", data)
	}
}

// renderTranscriptResponse renders the appropriate transcript partial based on container_id.
func renderTranscriptResponse(w http.ResponseWriter, handle *core.SpecActorHandle, specID string, containerID string, renderer *TemplateRenderer) {
	isChat := containerID == "chat-transcript"
	isTicker := containerID == "mission-ticker"

	if isTicker {
		var data struct {
			SpecID          string
			TickerEntries   []TranscriptEntry
			PendingQuestion *QuestionData
		}
		handle.ReadState(func(s *core.SpecState) {
			entries := s.Transcript
			start := 0
			if len(entries) > 10 {
				start = len(entries) - 10
			}
			data.SpecID = specID
			data.TickerEntries = make([]TranscriptEntry, 0, 10)
			for _, m := range entries[start:] {
				data.TickerEntries = append(data.TickerEntries, toTranscriptEntry(&m))
			}
			data.PendingQuestion = nil
		})
		renderer.RenderPartial(w, "mission_ticker.html", data)
		return
	}

	var data ActivityData
	handle.ReadState(func(s *core.SpecState) {
		entries := buildTranscriptEntries(s.Transcript, isChat)
		markContinuations(entries)
		data = ActivityData{
			SpecID:          specID,
			ContainerID:     containerID,
			Transcript:      entries,
			PendingQuestion: questionToViewData(s.PendingQuestion),
		}
	})

	if isChat {
		renderer.RenderPartial(w, "chat_transcript", data)
	} else {
		renderer.RenderPartial(w, "activity_transcript", data)
	}
}

// buildTranscriptEntries converts core transcript messages to view-model entries.
// If chatOnly is true, only human and manager messages are included.
func buildTranscriptEntries(messages []core.TranscriptMessage, chatOnly bool) []TranscriptEntry {
	entries := make([]TranscriptEntry, 0, len(messages))
	for i := range messages {
		m := &messages[i]
		if chatOnly && !isChatParticipant(m.Sender) {
			continue
		}
		entries = append(entries, toTranscriptEntry(m))
	}
	return entries
}

// toTranscriptEntry converts a single core TranscriptMessage to a view-model entry.
func toTranscriptEntry(m *core.TranscriptMessage) TranscriptEntry {
	senderLabel, isHuman, roleClass := senderDisplay(m.Sender)
	initial := "?"
	runes := []rune(senderLabel)
	if len(runes) > 0 {
		initial = string(runes[0:1])
	}
	contentHTML := template.HTML(RenderMarkdown(m.Content))

	return TranscriptEntry{
		Sender:         m.Sender,
		SenderLabel:    senderLabel,
		Initial:        initial,
		IsHuman:        isHuman,
		IsStep:         m.Kind.IsStep(),
		IsContinuation: false,
		RoleClass:      roleClass,
		Content:        m.Content,
		ContentHTML:    contentHTML,
		Timestamp:      m.Timestamp.Format("15:04:05"),
	}
}

// markContinuations marks consecutive entries from the same sender as continuations.
// The first entry in a run keeps IsContinuation=false; subsequent entries from
// the same sender get IsContinuation=true so the template can skip the avatar.
func markContinuations(entries []TranscriptEntry) {
	for i := 1; i < len(entries); i++ {
		if entries[i].Sender == entries[i-1].Sender && !entries[i].IsStep && !entries[i-1].IsStep {
			entries[i].IsContinuation = true
		}
	}
}

// isChatParticipant returns true if the sender is part of the human-manager conversation.
func isChatParticipant(sender string) bool {
	return sender == "human" || strings.HasPrefix(sender, "manager-")
}

// senderDisplay derives a display label, human flag, and CSS class from a raw sender ID.
func senderDisplay(sender string) (label string, isHuman bool, roleClass string) {
	if sender == "human" {
		return "You", true, "human"
	}

	// Agent IDs look like "manager-01JTEST..." or "brainstormer-01JTEST..."
	role := sender
	if idx := strings.IndexByte(sender, '-'); idx >= 0 {
		role = sender[:idx]
	}

	switch role {
	case "manager":
		return "Orchestrator", false, "manager"
	case "brainstormer":
		return "Researcher", false, "brainstormer"
	case "planner":
		return "Architect", false, "planner"
	case "dot_generator":
		return "Dot Generator", false, "dot-generator"
	case "critic":
		return "Critic", false, "critic"
	default:
		// Capitalize first letter
		runes := []rune(role)
		if len(runes) > 0 {
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		}
		return string(runes), false, normalizeCSSClass(role)
	}
}

// normalizeCSSClass converts a string to a valid CSS class name.
func normalizeCSSClass(raw string) string {
	var result strings.Builder
	for _, ch := range strings.ToLower(raw) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			result.WriteRune(ch)
		} else {
			result.WriteRune('-')
		}
	}
	return result.String()
}

// sanitizeContainerID validates and sanitizes a container_id value.
// Only known IDs are accepted; anything else falls back to "chat-transcript"
// to prevent XSS via user-controlled values.
func sanitizeContainerID(raw string) string {
	switch raw {
	case "activity-transcript", "chat-transcript", "mission-ticker":
		return raw
	default:
		return "chat-transcript"
	}
}

// questionToViewData converts a core UserQuestion into the template-friendly QuestionData.
// Returns nil if the question is nil.
func questionToViewData(q core.UserQuestion) *QuestionData {
	if q == nil {
		return nil
	}

	switch v := q.(type) {
	case core.BooleanQuestion:
		return &QuestionData{
			Type:       "boolean",
			QuestionID: v.QID.String(),
			Question:   v.Question,
			Default:    v.Default,
		}
	case core.MultipleChoiceQuestion:
		return &QuestionData{
			Type:       "multiple_choice",
			QuestionID: v.QID.String(),
			Question:   v.Question,
			Choices:    v.Choices,
			AllowMulti: v.AllowMulti,
		}
	case core.FreeformQuestion:
		placeholder := ""
		if v.Placeholder != nil {
			placeholder = *v.Placeholder
		}
		return &QuestionData{
			Type:        "freeform",
			QuestionID:  v.QID.String(),
			Question:    v.Question,
			Placeholder: placeholder,
		}
	default:
		return nil
	}
}
