// ABOUTME: Web handler for the undo operation on the kanban board.
// ABOUTME: Sends an Undo command and re-renders the board partial.
package web

import (
	"fmt"
	"net/http"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/server"
)

// Undo reverts the last undoable operation and re-renders the board.
func Undo(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
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

		if _, err := handle.SendCommand(core.UndoCommand{}); err != nil {
			writeHTMLError(w, http.StatusBadRequest, fmt.Sprintf("Undo failed: %v", err))
			return
		}

		// Return refreshed board
		var data BoardData
		handle.ReadState(func(s *core.SpecState) {
			data = BoardData{
				SpecID: specID.String(),
				Lanes:  cardsByLane(specID.String(), s),
			}
		})

		renderer.RenderPartial(w, "board.html", data)
	}
}
