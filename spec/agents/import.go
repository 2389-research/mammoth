// ABOUTME: LLM-powered spec import -- parses arbitrary text into structured spec commands.
// ABOUTME: Sends content to an LLM, extracts JSON with spec metadata and cards, converts to Commands.
package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/2389-research/mux/llm"

	"github.com/2389-research/mammoth/spec/core"
)

// ImportResult is the result of parsing input content via the LLM.
// Contains the core spec metadata and any cards extracted from the source material.
type ImportResult struct {
	Spec   ImportSpec    `json:"spec"`
	Update *ImportUpdate `json:"update,omitempty"`
	Cards  []ImportCard  `json:"cards"`
}

// ImportSpec holds core spec identity fields extracted from the input.
type ImportSpec struct {
	Title    string `json:"title"`
	OneLiner string `json:"one_liner"`
	Goal     string `json:"goal"`
}

// ImportUpdate holds optional extended spec metadata extracted from the input.
type ImportUpdate struct {
	Description     *string `json:"description,omitempty"`
	Constraints     *string `json:"constraints,omitempty"`
	SuccessCriteria *string `json:"success_criteria,omitempty"`
	Risks           *string `json:"risks,omitempty"`
	Notes           *string `json:"notes,omitempty"`
}

// ImportCard is a card extracted from the input material.
type ImportCard struct {
	CardType string  `json:"card_type"`
	Title    string  `json:"title"`
	Body     *string `json:"body,omitempty"`
	Lane     *string `json:"lane,omitempty"`
}

// ParseWithLLM sends content to an LLM and parses the response into an ImportResult.
// sourceHint is an optional format hint (e.g. "dot", "yaml", "markdown")
// that helps the LLM understand the input format.
func ParseWithLLM(ctx context.Context, content, sourceHint string, client llm.Client, model string) (*ImportResult, error) {
	systemPrompt := BuildImportSystemPrompt(sourceHint)

	// Use generous output limit — modern models support 64K+ output tokens.
	// The default (4096) is far too small for structured JSON extraction from
	// large specs that may produce dozens of cards.
	req := &llm.Request{
		Model:     model,
		System:    systemPrompt,
		Messages:  []llm.Message{llm.NewUserMessage(content)},
		MaxTokens: 32768,
	}

	response, err := client.CreateMessage(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	text := response.TextContent()
	if text == "" {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	return ExtractJSON(text)
}

// BuildImportSystemPrompt builds the system prompt that instructs the LLM to extract spec structure.
func BuildImportSystemPrompt(sourceHint string) string {
	formatNote := ""
	if sourceHint != "" {
		formatNote = fmt.Sprintf("The input is in %s format. ", sourceHint)
	}

	return fmt.Sprintf(`You are a spec structure extractor. %sAnalyze the input and extract a structured specification.

Output ONLY valid JSON with this exact schema (no markdown, no commentary):

{
  "spec": {
    "title": "Short title for the spec",
    "one_liner": "One sentence summary",
    "goal": "What this spec aims to achieve"
  },
  "update": {
    "description": "Detailed description (optional, null if not present)",
    "constraints": "Known constraints (optional)",
    "success_criteria": "How to measure success (optional)",
    "risks": "Known risks (optional)",
    "notes": "Additional notes (optional)"
  },
  "cards": [
    {
      "card_type": "idea|task|plan|decision|constraint|risk",
      "title": "Card title",
      "body": "Card body/details (optional)",
      "lane": "Ideas|Spec|Backlog (optional, defaults to Ideas)"
    }
  ]
}

Card type guidance:
- "idea": Creative concepts, features, possibilities
- "task": Concrete work items, implementation steps
- "plan": High-level strategies or approaches
- "decision": Choices that have been made or need to be made
- "constraint": Limitations or requirements that must be honored
- "risk": Potential problems or concerns

Extract as many meaningful cards as the input warrants. Every distinct idea, task, requirement, or concern should be its own card.

If the input is minimal, create at least one card capturing the core concept.`, formatNote)
}

// ExtractJSON parses JSON from LLM output using a 3-tier strategy:
// 1. Try parsing the entire text as JSON
// 2. Strip markdown code fences and try again
// 3. Find the first '{' to last '}' and try that substring
func ExtractJSON(text string) (*ImportResult, error) {
	// Tier 1: Try raw JSON parse
	var result ImportResult
	if err := json.Unmarshal([]byte(text), &result); err == nil {
		return &result, nil
	}

	// Tier 2: Strip code fences
	stripped := stripCodeFences(text)
	if err := json.Unmarshal([]byte(stripped), &result); err == nil {
		return &result, nil
	}

	// Tier 3: Find first { to last }
	firstBrace := strings.Index(text, "{")
	lastBrace := strings.LastIndex(text, "}")
	if firstBrace >= 0 && lastBrace > firstBrace {
		substring := text[firstBrace : lastBrace+1]
		if err := json.Unmarshal([]byte(substring), &result); err == nil {
			return &result, nil
		}
	}

	return nil, fmt.Errorf("failed to parse LLM response as ImportResult JSON")
}

// stripCodeFences removes markdown code fences from text.
func stripCodeFences(text string) string {
	var lines []string
	inFence := false

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence || trimmed != "" {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

// ToCommands converts an ImportResult into a slice of Commands suitable for the
// event-sourcing pipeline. Produces: one CreateSpec, optionally one UpdateSpecCore,
// and one CreateCard per card. All commands use created_by: "import".
func ToCommands(result *ImportResult) []core.Command {
	var commands []core.Command

	// CreateSpec command
	commands = append(commands, core.CreateSpecCommand{
		Title:    result.Spec.Title,
		OneLiner: result.Spec.OneLiner,
		Goal:     result.Spec.Goal,
	})

	// UpdateSpecCore command (if any update fields are present)
	if result.Update != nil {
		hasContent := result.Update.Description != nil ||
			result.Update.Constraints != nil ||
			result.Update.SuccessCriteria != nil ||
			result.Update.Risks != nil ||
			result.Update.Notes != nil

		if hasContent {
			commands = append(commands, core.UpdateSpecCoreCommand{
				Description:     result.Update.Description,
				Constraints:     result.Update.Constraints,
				SuccessCriteria: result.Update.SuccessCriteria,
				Risks:           result.Update.Risks,
				Notes:           result.Update.Notes,
			})
		}
	}

	// CreateCard commands
	for _, card := range result.Cards {
		commands = append(commands, core.CreateCardCommand{
			CardType:  card.CardType,
			Title:     card.Title,
			Body:      card.Body,
			Lane:      card.Lane,
			CreatedBy: "import",
		})
	}

	return commands
}
