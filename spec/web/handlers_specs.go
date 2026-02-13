// ABOUTME: Web handlers for spec listing, creation, import, and viewing.
// ABOUTME: Serves HTMX partials and full pages for the spec management UI.
package web

import (
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/server"
)

// SpecSummaryView is the view-model for a spec in the list.
type SpecSummaryView struct {
	SpecID    string
	Title     string
	OneLiner  string
	UpdatedAt string
}

// SpecViewData is the view-model for the full spec view partial.
type SpecViewData struct {
	SpecID   string
	Title    string
	OneLiner string
	Goal     string
	Lanes    []LaneData
}

// Index renders the full index.html page.
func Index(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderer.Render(w, "base", nil)
	}
}

// SpecList renders the spec_list partial with spec summaries.
func SpecList(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var specs []SpecSummaryView
		for _, specID := range state.ListActorIDs() {
			handle := state.GetActor(specID)
			if handle == nil {
				continue
			}
			handle.ReadState(func(s *core.SpecState) {
				if s.Core != nil {
					specs = append(specs, SpecSummaryView{
						SpecID:    specID.String(),
						Title:     s.Core.Title,
						OneLiner:  s.Core.OneLiner,
						UpdatedAt: s.Core.UpdatedAt.Format(time.RFC3339),
					})
				}
			})
		}
		if specs == nil {
			specs = []SpecSummaryView{}
		}
		renderer.RenderPartial(w, "spec_list.html", map[string]any{
			"Specs": specs,
		})
	}
}

// CreateSpecForm renders the create_spec_form partial.
func CreateSpecForm(renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderer.RenderPartial(w, "create_spec_form.html", nil)
	}
}

// CreateSpec creates a spec from form data and returns the spec_view partial.
func CreateSpec(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid form data.")
			return
		}

		description := r.FormValue("description")
		if strings.TrimSpace(description) == "" {
			writeHTMLError(w, http.StatusBadRequest, "Description must not be empty.")
			return
		}

		specID := core.NewULID()

		// Create directory structure
		specDir := filepath.Join(state.MammothHome, "specs", specID.String())
		if err := os.MkdirAll(specDir, 0o755); err != nil {
			log.Printf("failed to create spec directory: %v", err)
			writeHTMLError(w, http.StatusInternalServerError, "Failed to create spec directory.")
			return
		}

		// Spawn actor and send CreateSpec command
		handle := core.SpawnActor(specID, core.NewSpecState())
		title := extractPlaceholderTitle(description)
		events, err := handle.SendCommand(core.CreateSpecCommand{
			Title:    title,
			OneLiner: "",
			Goal:     "",
		})
		if err != nil {
			log.Printf("failed to create spec: %v", err)
			writeHTMLError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create spec: %v", err))
			return
		}

		// Persist initial events
		server.PersistEvents(specDir, events)

		// Append the user's free-text description to the transcript so the
		// manager agent can read it and parse it into structured fields.
		transcriptEvents, err := handle.SendCommand(core.AppendTranscriptCommand{
			Sender:  "human",
			Content: description,
		})
		if err != nil {
			log.Printf("failed to append transcript: %v", err)
		} else {
			server.PersistEvents(specDir, transcriptEvents)
		}

		// Register actor and event persister BEFORE reading state
		server.SpawnEventPersister(state, specID, handle)
		state.SetActor(specID, handle)

		// Auto-start agents if an LLM provider is configured
		state.TryStartAgents(specID)

		// Read the created state and render spec view
		actor := state.GetActor(specID)
		if actor == nil {
			writeHTMLError(w, http.StatusInternalServerError, "Spec created but not found.")
			return
		}

		var viewData SpecViewData
		actor.ReadState(func(s *core.SpecState) {
			if s.Core == nil {
				return
			}
			viewData = SpecViewData{
				SpecID:   specID.String(),
				Title:    s.Core.Title,
				OneLiner: s.Core.OneLiner,
				Goal:     s.Core.Goal,
				Lanes:    cardsByLane(specID.String(), s),
			}
		})

		if viewData.SpecID == "" {
			writeHTMLError(w, http.StatusInternalServerError, "Spec created but core data is missing.")
			return
		}

		// Set HX-Push-Url so the browser URL updates to the spec view
		w.Header().Set("HX-Push-Url", fmt.Sprintf("/web/specs/%s", viewData.SpecID))
		renderer.RenderPartial(w, "spec_view.html", viewData)
	}
}

// ImportSpecForm renders the import_spec_form partial.
func ImportSpecForm(renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		renderer.RenderPartial(w, "import_spec_form.html", nil)
	}
}

// ImportSpec handles spec import from pasted content. Stub for now.
func ImportSpec(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeHTMLError(w, http.StatusNotImplemented, "Import via LLM is not yet implemented. Use the API endpoint instead.")
	}
}

// SpecView renders the spec_view partial for a given spec.
func SpecView(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
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

		var viewData SpecViewData
		handle.ReadState(func(s *core.SpecState) {
			if s.Core == nil {
				return
			}
			viewData = SpecViewData{
				SpecID:   specID.String(),
				Title:    s.Core.Title,
				OneLiner: s.Core.OneLiner,
				Goal:     s.Core.Goal,
				Lanes:    cardsByLane(specID.String(), s),
			}
		})

		if viewData.SpecID == "" {
			writeHTMLError(w, http.StatusNotFound, "Spec has no core data.")
			return
		}

		renderer.RenderPartial(w, "spec_view.html", viewData)
	}
}

// extractPlaceholderTitle derives a title from free-text description.
// Takes the first sentence (ending in . ! ?) or first 60 chars, whichever is shorter.
func extractPlaceholderTitle(description string) string {
	trimmed := strings.TrimSpace(description)
	if trimmed == "" {
		return "Untitled Spec"
	}

	// Find first sentence-ending punctuation
	sentenceEnd := -1
	for i, ch := range trimmed {
		if ch == '.' || ch == '!' || ch == '?' {
			sentenceEnd = i + 1
			break
		}
	}
	if sentenceEnd < 0 {
		sentenceEnd = len(trimmed)
	}

	// Truncate to 60 characters (rune-safe)
	runes := []rune(trimmed)
	charBoundary := len(trimmed)
	if len(runes) > 60 {
		charBoundary = len(string(runes[:60]))
	}

	end := sentenceEnd
	if charBoundary < end {
		end = charBoundary
	}

	title := trimmed[:end]
	if end < len(trimmed) && !strings.ContainsAny(string(title[len(title)-1:]), ".!?") {
		title += "..."
	}
	return title
}

// parseSpecID extracts and validates the spec ID from the URL path.
// Writes an HTML error and returns false if invalid.
func parseSpecID(w http.ResponseWriter, r *http.Request) (ulid.ULID, bool) {
	idStr := chi.URLParam(r, "id")
	id, err := ulid.Parse(idStr)
	if err != nil {
		writeHTMLError(w, http.StatusBadRequest, "Invalid spec ID.")
		return ulid.ULID{}, false
	}
	return id, true
}

// writeHTMLError writes an error message as an HTML paragraph for HTMX consumption.
func writeHTMLError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<p class="error-msg">%s</p>`, html.EscapeString(msg))
}
