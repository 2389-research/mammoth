// ABOUTME: Exports a SpecState as a structured YAML document matching spec.yaml format.
// ABOUTME: Uses gopkg.in/yaml.v3 for serialization with deterministic ordering.
package export

import (
	"fmt"

	"github.com/2389-research/mammoth/spec/core"
	"gopkg.in/yaml.v3"
)

// YamlCard is a serializable YAML representation of a single card within a lane.
type YamlCard struct {
	ID        string   `yaml:"id"`
	CardType  string   `yaml:"type"`
	Title     string   `yaml:"title"`
	Body      string   `yaml:"body,omitempty"`
	Order     float64  `yaml:"order"`
	Refs      []string `yaml:"refs,omitempty"`
	CreatedBy string   `yaml:"created_by"`
}

// YamlLane is a serializable YAML representation of a lane containing cards.
type YamlLane struct {
	Name  string     `yaml:"name"`
	Cards []YamlCard `yaml:"cards"`
}

// YamlSpec is the top-level serializable YAML representation of the spec state.
type YamlSpec struct {
	Name            string     `yaml:"name"`
	Version         string     `yaml:"version"`
	OneLiner        string     `yaml:"one_liner"`
	Goal            string     `yaml:"goal"`
	Description     string     `yaml:"description,omitempty"`
	Constraints     string     `yaml:"constraints,omitempty"`
	SuccessCriteria string     `yaml:"success_criteria,omitempty"`
	Risks           string     `yaml:"risks,omitempty"`
	Notes           string     `yaml:"notes,omitempty"`
	Lanes           []YamlLane `yaml:"lanes"`
}

// ExportYAML exports the spec state as structured YAML matching the spec.yaml format.
//
// Uses the same deterministic ordering as the Markdown exporter: Ideas, Plan,
// Spec first, then extra lanes alphabetically. Cards within lanes sorted by
// order then card_id.
func ExportYAML(state *core.SpecState) (string, error) {
	if state.Core == nil {
		return "", fmt.Errorf("SpecState must have a core to export YAML")
	}
	c := state.Core

	cardsByLane := groupCardsByLane(state)
	orderedLanes := orderedLaneNames(state, cardsByLane)

	yamlLanes := make([]YamlLane, 0, len(orderedLanes))
	for _, laneName := range orderedLanes {
		var yamlCards []YamlCard
		if cards, ok := cardsByLane[laneName]; ok {
			yamlCards = make([]YamlCard, 0, len(cards))
			for _, card := range cards {
				yc := YamlCard{
					ID:        card.CardID.String(),
					CardType:  card.CardType,
					Title:     card.Title,
					Order:     card.Order,
					CreatedBy: card.CreatedBy,
				}
				if card.Body != nil {
					yc.Body = *card.Body
				}
				if len(card.Refs) > 0 {
					yc.Refs = card.Refs
				}
				yamlCards = append(yamlCards, yc)
			}
		}
		if yamlCards == nil {
			yamlCards = []YamlCard{}
		}
		yamlLanes = append(yamlLanes, YamlLane{
			Name:  laneName,
			Cards: yamlCards,
		})
	}

	spec := YamlSpec{
		Name:     c.Title,
		Version:  "0.1",
		OneLiner: c.OneLiner,
		Goal:     c.Goal,
		Lanes:    yamlLanes,
	}
	if c.Description != nil {
		spec.Description = *c.Description
	}
	if c.Constraints != nil {
		spec.Constraints = *c.Constraints
	}
	if c.SuccessCriteria != nil {
		spec.SuccessCriteria = *c.SuccessCriteria
	}
	if c.Risks != nil {
		spec.Risks = *c.Risks
	}
	if c.Notes != nil {
		spec.Notes = *c.Notes
	}

	data, err := yaml.Marshal(&spec)
	if err != nil {
		return "", fmt.Errorf("yaml marshal: %w", err)
	}
	return string(data), nil
}
