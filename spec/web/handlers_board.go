// ABOUTME: Web handlers for the kanban board: viewing, creating, editing, and deleting cards.
// ABOUTME: Serves HTMX partials that update the board in-place via swap targets.
package web

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"

	"github.com/2389-research/mammoth/spec/core"
	"github.com/2389-research/mammoth/spec/server"
)

// LaneData groups cards by lane name for template rendering.
type LaneData struct {
	Name  string
	Cards []CardData
}

// CardData is the view-model for a single card in a template.
type CardData struct {
	SpecID    string
	CardID    string
	CardType  string
	Title     string
	Body      string
	BodyHTML  template.HTML
	Lane      string
	Order     float64
	CreatedBy string
	UpdatedAt string
}

// BoardData is the view-model for the board partial.
type BoardData struct {
	SpecID string
	Lanes  []LaneData
}

// CardFormData is the view-model for the card creation/edit form.
type CardFormData struct {
	SpecID   string
	CardID   *string
	Title    string
	CardType string
	Body     string
	Lane     string
}

// Board renders the board partial showing all lanes and cards.
func Board(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
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

// CreateCardForm renders an empty card creation form.
func CreateCardForm(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		renderer.RenderPartial(w, "card_form.html", CardFormData{
			SpecID:   idStr,
			CardID:   nil,
			Title:    "",
			CardType: "idea",
			Body:     "",
			Lane:     "Ideas",
		})
	}
}

// CreateCard creates a new card from form data and returns the refreshed board.
func CreateCard(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
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

		title := r.FormValue("title")
		cardType := r.FormValue("card_type")
		body := r.FormValue("body")
		lane := r.FormValue("lane")

		var bodyPtr *string
		if strings.TrimSpace(body) != "" {
			bodyPtr = &body
		}
		var lanePtr *string
		if strings.TrimSpace(lane) != "" {
			lanePtr = &lane
		}

		cmd := core.CreateCardCommand{
			CardType:  cardType,
			Title:     title,
			Body:      bodyPtr,
			Lane:      lanePtr,
			CreatedBy: "human",
		}

		if _, err := handle.SendCommand(cmd); err != nil {
			writeHTMLError(w, http.StatusBadRequest, fmt.Sprintf("Failed to create card: %v", err))
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

// EditCardForm renders the card edit form pre-filled with existing card data.
func EditCardForm(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		cardIDStr := chi.URLParam(r, "card_id")
		cardID, err := ulid.Parse(cardIDStr)
		if err != nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid card ID.")
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var formData CardFormData
		var found bool
		handle.ReadState(func(s *core.SpecState) {
			card, exists := s.Cards.Get(cardID)
			if !exists {
				return
			}
			found = true
			body := ""
			if card.Body != nil {
				body = *card.Body
			}
			formData = CardFormData{
				SpecID:   specID.String(),
				CardID:   &cardIDStr,
				Title:    card.Title,
				CardType: card.CardType,
				Body:     body,
				Lane:     card.Lane,
			}
		})

		if !found {
			writeHTMLError(w, http.StatusNotFound, "Card not found.")
			return
		}

		renderer.RenderPartial(w, "card_form.html", formData)
	}
}

// UpdateCard updates an existing card and returns the single card partial.
func UpdateCard(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		cardIDStr := chi.URLParam(r, "card_id")
		cardID, err := ulid.Parse(cardIDStr)
		if err != nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid card ID.")
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

		title := r.FormValue("title")
		cardType := r.FormValue("card_type")
		body := r.FormValue("body")

		// Build UpdateCard command with OptionalField for body
		cmd := core.UpdateCardCommand{
			CardID:    cardID,
			Title:     &title,
			CardType:  &cardType,
			UpdatedBy: "human",
		}

		// Set body using OptionalField semantics
		if strings.TrimSpace(body) != "" {
			cmd.Body = core.Present(body)
		} else {
			cmd.Body = core.Null[string]()
		}

		if _, err := handle.SendCommand(cmd); err != nil {
			writeHTMLError(w, http.StatusBadRequest, fmt.Sprintf("Failed to update card: %v", err))
			return
		}

		// Return the updated card partial
		var cardData *CardData
		handle.ReadState(func(s *core.SpecState) {
			card, exists := s.Cards.Get(cardID)
			if !exists {
				return
			}
			cd := cardDataFromCard(specID.String(), &card)
			cardData = &cd
		})

		if cardData == nil {
			writeHTMLError(w, http.StatusNotFound, "Card not found after update.")
			return
		}

		renderer.RenderPartial(w, "card", cardData)
	}
}

// DeleteCard deletes a card and returns empty content so HTMX removes the element.
func DeleteCard(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		cardIDStr := chi.URLParam(r, "card_id")
		cardID, err := ulid.Parse(cardIDStr)
		if err != nil {
			writeHTMLError(w, http.StatusBadRequest, "Invalid card ID.")
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		cmd := core.DeleteCardCommand{
			CardID:    cardID,
			UpdatedBy: "human",
		}

		if _, err := handle.SendCommand(cmd); err != nil {
			writeHTMLError(w, http.StatusBadRequest, fmt.Sprintf("Failed to delete card: %v", err))
			return
		}

		// Return empty content so HTMX removes the card element
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
	}
}

// cardsByLane groups cards by lane for template rendering, following the
// deterministic ordering: Ideas, Plan, Spec first, then extra lanes alphabetically.
func cardsByLane(specID string, state *core.SpecState) []LaneData {
	defaultLanes := []string{"Ideas", "Plan", "Spec"}

	// Group cards by lane
	byLane := make(map[string][]CardData)
	state.Cards.Range(func(_ ulid.ULID, card core.Card) bool {
		cd := cardDataFromCard(specID, &card)
		byLane[card.Lane] = append(byLane[card.Lane], cd)
		return true
	})

	// Sort cards within each lane by order
	for lane := range byLane {
		cards := byLane[lane]
		sort.SliceStable(cards, func(i, j int) bool {
			return cards[i].Order < cards[j].Order
		})
		byLane[lane] = cards
	}

	var lanes []LaneData

	// Default lanes first
	for _, laneName := range defaultLanes {
		cards := byLane[laneName]
		if cards == nil {
			cards = []CardData{}
		}
		lanes = append(lanes, LaneData{
			Name:  laneName,
			Cards: cards,
		})
	}

	// Extra lanes alphabetically
	var extraLanes []string
	for lane := range byLane {
		isDefault := false
		for _, dl := range defaultLanes {
			if lane == dl {
				isDefault = true
				break
			}
		}
		if !isDefault {
			extraLanes = append(extraLanes, lane)
		}
	}
	sort.Strings(extraLanes)

	for _, laneName := range extraLanes {
		lanes = append(lanes, LaneData{
			Name:  laneName,
			Cards: byLane[laneName],
		})
	}

	return lanes
}

// docCardsByLane returns lanes for the document view, excluding "Ideas".
// The document is the polished spec output; ideas belong on the board only.
func docCardsByLane(specID string, state *core.SpecState) []LaneData {
	all := cardsByLane(specID, state)
	filtered := make([]LaneData, 0, len(all))
	for _, lane := range all {
		if lane.Name == "Ideas" {
			continue
		}
		filtered = append(filtered, lane)
	}
	return filtered
}

// cardDataFromCard converts a core.Card to a CardData view-model.
func cardDataFromCard(specID string, card *core.Card) CardData {
	body := ""
	var bodyHTML template.HTML
	if card.Body != nil {
		body = *card.Body
		bodyHTML = template.HTML(RenderMarkdown(body))
	}
	return CardData{
		SpecID:    specID,
		CardID:    card.CardID.String(),
		CardType:  card.CardType,
		Title:     card.Title,
		Body:      body,
		BodyHTML:  bodyHTML,
		Lane:      card.Lane,
		Order:     card.Order,
		CreatedBy: card.CreatedBy,
		UpdatedAt: card.UpdatedAt.Format("15:04:05"),
	}
}
