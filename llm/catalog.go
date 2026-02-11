// ABOUTME: Model catalog providing metadata about known LLM models across providers.
// ABOUTME: Supports lookup by ID or alias, listing by provider, capability filtering, and custom model registration.

package llm

// ModelInfo describes a single LLM model's capabilities and metadata.
type ModelInfo struct {
	ID                   string // e.g., "claude-opus-4-6"
	Provider             string // e.g., "anthropic"
	DisplayName          string // e.g., "Claude Opus 4.6"
	ContextWindow        int    // max total tokens
	MaxOutput            int    // max output tokens, 0 if unknown
	SupportsTools        bool
	SupportsVision       bool
	SupportsReasoning    bool
	InputCostPerMillion  float64  // USD per 1M input tokens, 0 if unknown
	OutputCostPerMillion float64  // USD per 1M output tokens, 0 if unknown
	Aliases              []string // shorthand names
}

// Catalog holds a collection of ModelInfo entries and supports lookup and filtering.
type Catalog struct {
	models []ModelInfo
}

// builtinModels returns the default set of known models as of February 2026.
func builtinModels() []ModelInfo {
	return []ModelInfo{
		// Anthropic
		{
			ID:                "claude-opus-4-6",
			Provider:          "anthropic",
			DisplayName:       "Claude Opus 4.6",
			ContextWindow:     200000,
			SupportsTools:     true,
			SupportsVision:    true,
			SupportsReasoning: true,
			Aliases:           []string{"opus", "claude-opus"},
		},
		{
			ID:                "claude-sonnet-4-5",
			Provider:          "anthropic",
			DisplayName:       "Claude Sonnet 4.5",
			ContextWindow:     200000,
			SupportsTools:     true,
			SupportsVision:    true,
			SupportsReasoning: true,
			Aliases:           []string{"sonnet", "claude-sonnet"},
		},

		// OpenAI
		{
			ID:                "gpt-5.2",
			Provider:          "openai",
			DisplayName:       "GPT-5.2",
			ContextWindow:     1047576,
			SupportsTools:     true,
			SupportsVision:    true,
			SupportsReasoning: true,
			Aliases:           []string{"gpt5"},
		},
		{
			ID:                "gpt-5.2-mini",
			Provider:          "openai",
			DisplayName:       "GPT-5.2 Mini",
			ContextWindow:     1047576,
			SupportsTools:     true,
			SupportsVision:    true,
			SupportsReasoning: true,
			Aliases:           []string{"gpt5-mini"},
		},
		{
			ID:                "gpt-5.2-codex",
			Provider:          "openai",
			DisplayName:       "GPT-5.2 Codex",
			ContextWindow:     1047576,
			SupportsTools:     true,
			SupportsVision:    true,
			SupportsReasoning: true,
			Aliases:           []string{"codex"},
		},

		// Gemini
		{
			ID:                "gemini-3-pro-preview",
			Provider:          "gemini",
			DisplayName:       "Gemini 3 Pro (Preview)",
			ContextWindow:     1048576,
			SupportsTools:     true,
			SupportsVision:    true,
			SupportsReasoning: true,
			Aliases:           []string{"gemini-pro", "gemini-3-pro"},
		},
		{
			ID:                "gemini-3-flash-preview",
			Provider:          "gemini",
			DisplayName:       "Gemini 3 Flash (Preview)",
			ContextWindow:     1048576,
			SupportsTools:     true,
			SupportsVision:    true,
			SupportsReasoning: true,
			Aliases:           []string{"gemini-flash", "gemini-3-flash"},
		},
	}
}

// DefaultCatalog returns a new Catalog pre-populated with built-in model definitions.
// Each call returns an independent copy so registrations on one catalog do not affect others.
func DefaultCatalog() *Catalog {
	return &Catalog{
		models: builtinModels(),
	}
}

// GetModelInfo looks up a model by its canonical ID or any of its aliases.
// Returns nil if no matching model is found.
func (c *Catalog) GetModelInfo(modelID string) *ModelInfo {
	for i := range c.models {
		if c.models[i].ID == modelID {
			return &c.models[i]
		}
		for _, alias := range c.models[i].Aliases {
			if alias == modelID {
				return &c.models[i]
			}
		}
	}
	return nil
}

// ListModels returns all models matching the given provider.
// If provider is empty, all models in the catalog are returned.
func (c *Catalog) ListModels(provider string) []ModelInfo {
	var result []ModelInfo
	for _, m := range c.models {
		if provider == "" || m.Provider == provider {
			result = append(result, m)
		}
	}
	return result
}

// GetLatestModel returns the first model in the catalog for the given provider,
// optionally filtered by a capability string ("reasoning", "vision", or "tools").
// Returns nil if no model matches.
func (c *Catalog) GetLatestModel(provider string, capability string) *ModelInfo {
	for i := range c.models {
		m := &c.models[i]
		if m.Provider != provider {
			continue
		}
		if !matchesCapability(m, capability) {
			continue
		}
		return m
	}
	return nil
}

// matchesCapability checks whether a model supports the requested capability.
// An empty capability string matches any model.
func matchesCapability(m *ModelInfo, capability string) bool {
	switch capability {
	case "":
		return true
	case "reasoning":
		return m.SupportsReasoning
	case "vision":
		return m.SupportsVision
	case "tools":
		return m.SupportsTools
	default:
		return false
	}
}

// Register adds a model to the catalog. If a model with the same ID already exists,
// it is replaced.
func (c *Catalog) Register(model ModelInfo) {
	for i := range c.models {
		if c.models[i].ID == model.ID {
			c.models[i] = model
			return
		}
	}
	c.models = append(c.models, model)
}
