// ABOUTME: Tests for the model catalog, covering lookup, listing, filtering, and registration.
// ABOUTME: Validates built-in model entries and custom model registration behavior.

package llm

import (
	"testing"
)

func TestGetModelInfoByExactID(t *testing.T) {
	catalog := DefaultCatalog()

	tests := []struct {
		id           string
		wantDisplay  string
		wantProvider string
	}{
		{"claude-opus-4-6", "Claude Opus 4.6", "anthropic"},
		{"claude-sonnet-4-5", "Claude Sonnet 4.5", "anthropic"},
		{"gpt-5.2", "GPT-5.2", "openai"},
		{"gpt-5.2-mini", "GPT-5.2 Mini", "openai"},
		{"gpt-5.2-codex", "GPT-5.2 Codex", "openai"},
		{"gemini-3-pro-preview", "Gemini 3 Pro (Preview)", "gemini"},
		{"gemini-3-flash-preview", "Gemini 3 Flash (Preview)", "gemini"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			info := catalog.GetModelInfo(tt.id)
			if info == nil {
				t.Fatalf("GetModelInfo(%q) returned nil, want model", tt.id)
			}
			if info.ID != tt.id {
				t.Errorf("ID = %q, want %q", info.ID, tt.id)
			}
			if info.DisplayName != tt.wantDisplay {
				t.Errorf("DisplayName = %q, want %q", info.DisplayName, tt.wantDisplay)
			}
			if info.Provider != tt.wantProvider {
				t.Errorf("Provider = %q, want %q", info.Provider, tt.wantProvider)
			}
		})
	}
}

func TestGetModelInfoByAlias(t *testing.T) {
	catalog := DefaultCatalog()

	tests := []struct {
		alias  string
		wantID string
	}{
		{"opus", "claude-opus-4-6"},
		{"claude-opus", "claude-opus-4-6"},
		{"sonnet", "claude-sonnet-4-5"},
		{"claude-sonnet", "claude-sonnet-4-5"},
		{"gpt5", "gpt-5.2"},
		{"gpt5-mini", "gpt-5.2-mini"},
		{"codex", "gpt-5.2-codex"},
		{"gemini-pro", "gemini-3-pro-preview"},
		{"gemini-3-pro", "gemini-3-pro-preview"},
		{"gemini-flash", "gemini-3-flash-preview"},
		{"gemini-3-flash", "gemini-3-flash-preview"},
	}

	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			info := catalog.GetModelInfo(tt.alias)
			if info == nil {
				t.Fatalf("GetModelInfo(%q) returned nil, want model with ID %q", tt.alias, tt.wantID)
			}
			if info.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", info.ID, tt.wantID)
			}
		})
	}
}

func TestGetModelInfoUnknown(t *testing.T) {
	catalog := DefaultCatalog()

	unknowns := []string{
		"nonexistent-model",
		"claude-4",
		"gpt-6",
		"",
	}

	for _, id := range unknowns {
		t.Run("unknown_"+id, func(t *testing.T) {
			info := catalog.GetModelInfo(id)
			if info != nil {
				t.Errorf("GetModelInfo(%q) = %+v, want nil", id, info)
			}
		})
	}
}

func TestListModelsByProvider(t *testing.T) {
	catalog := DefaultCatalog()

	tests := []struct {
		provider  string
		wantCount int
		wantIDs   []string
	}{
		{"anthropic", 2, []string{"claude-opus-4-6", "claude-sonnet-4-5"}},
		{"openai", 3, []string{"gpt-5.2", "gpt-5.2-mini", "gpt-5.2-codex"}},
		{"gemini", 2, []string{"gemini-3-pro-preview", "gemini-3-flash-preview"}},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			models := catalog.ListModels(tt.provider)
			if len(models) != tt.wantCount {
				t.Fatalf("ListModels(%q) returned %d models, want %d", tt.provider, len(models), tt.wantCount)
			}
			gotIDs := make(map[string]bool)
			for _, m := range models {
				gotIDs[m.ID] = true
				if m.Provider != tt.provider {
					t.Errorf("model %q has provider %q, want %q", m.ID, m.Provider, tt.provider)
				}
			}
			for _, wantID := range tt.wantIDs {
				if !gotIDs[wantID] {
					t.Errorf("ListModels(%q) missing model %q", tt.provider, wantID)
				}
			}
		})
	}
}

func TestListModelsAll(t *testing.T) {
	catalog := DefaultCatalog()
	models := catalog.ListModels("")
	// Should have all 7 built-in models
	if len(models) != 7 {
		t.Errorf("ListModels(\"\") returned %d models, want 7", len(models))
	}
}

func TestListModelsUnknownProvider(t *testing.T) {
	catalog := DefaultCatalog()
	models := catalog.ListModels("unknown-provider")
	if len(models) != 0 {
		t.Errorf("ListModels(\"unknown-provider\") returned %d models, want 0", len(models))
	}
}

func TestGetLatestModelByProvider(t *testing.T) {
	catalog := DefaultCatalog()

	providers := []string{"anthropic", "openai", "gemini"}

	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			model := catalog.GetLatestModel(provider, "")
			if model == nil {
				t.Fatalf("GetLatestModel(%q, \"\") returned nil", provider)
			}
			if model.Provider != provider {
				t.Errorf("Provider = %q, want %q", model.Provider, provider)
			}
		})
	}
}

func TestGetLatestModelWithCapabilityFilter(t *testing.T) {
	catalog := DefaultCatalog()

	// All built-in models support all capabilities, so each filter should return a result
	capabilities := []string{"reasoning", "vision", "tools"}

	for _, cap := range capabilities {
		t.Run("anthropic_"+cap, func(t *testing.T) {
			model := catalog.GetLatestModel("anthropic", cap)
			if model == nil {
				t.Fatalf("GetLatestModel(\"anthropic\", %q) returned nil", cap)
			}
			if model.Provider != "anthropic" {
				t.Errorf("Provider = %q, want \"anthropic\"", model.Provider)
			}
			switch cap {
			case "reasoning":
				if !model.SupportsReasoning {
					t.Error("expected SupportsReasoning to be true")
				}
			case "vision":
				if !model.SupportsVision {
					t.Error("expected SupportsVision to be true")
				}
			case "tools":
				if !model.SupportsTools {
					t.Error("expected SupportsTools to be true")
				}
			}
		})
	}
}

func TestGetLatestModelNoMatch(t *testing.T) {
	catalog := DefaultCatalog()

	// Unknown provider should return nil
	model := catalog.GetLatestModel("unknown-provider", "")
	if model != nil {
		t.Errorf("GetLatestModel(\"unknown-provider\", \"\") = %+v, want nil", model)
	}
}

func TestGetLatestModelCapabilityNoMatch(t *testing.T) {
	// Register a model that lacks reasoning, then filter for reasoning
	catalog := DefaultCatalog()
	catalog.Register(ModelInfo{
		ID:                "test-basic-model",
		Provider:          "testprovider",
		DisplayName:       "Test Basic",
		ContextWindow:     4096,
		SupportsTools:     false,
		SupportsVision:    false,
		SupportsReasoning: false,
	})

	model := catalog.GetLatestModel("testprovider", "reasoning")
	if model != nil {
		t.Errorf("GetLatestModel(\"testprovider\", \"reasoning\") = %+v, want nil", model)
	}

	// But without capability filter, should find it
	model = catalog.GetLatestModel("testprovider", "")
	if model == nil {
		t.Fatal("GetLatestModel(\"testprovider\", \"\") returned nil, want model")
	}
	if model.ID != "test-basic-model" {
		t.Errorf("ID = %q, want \"test-basic-model\"", model.ID)
	}
}

func TestRegisterCustomModel(t *testing.T) {
	catalog := DefaultCatalog()

	custom := ModelInfo{
		ID:                   "custom-llm-v1",
		Provider:             "custom",
		DisplayName:          "Custom LLM v1",
		ContextWindow:        32000,
		MaxOutput:            4096,
		SupportsTools:        true,
		SupportsVision:       false,
		SupportsReasoning:    false,
		InputCostPerMillion:  1.50,
		OutputCostPerMillion: 3.00,
		Aliases:              []string{"custom", "custom-v1"},
	}

	catalog.Register(custom)

	// Should be findable by ID
	info := catalog.GetModelInfo("custom-llm-v1")
	if info == nil {
		t.Fatal("GetModelInfo(\"custom-llm-v1\") returned nil after Register")
	}
	if info.DisplayName != "Custom LLM v1" {
		t.Errorf("DisplayName = %q, want \"Custom LLM v1\"", info.DisplayName)
	}
	if info.ContextWindow != 32000 {
		t.Errorf("ContextWindow = %d, want 32000", info.ContextWindow)
	}
	if info.MaxOutput != 4096 {
		t.Errorf("MaxOutput = %d, want 4096", info.MaxOutput)
	}
	if info.InputCostPerMillion != 1.50 {
		t.Errorf("InputCostPerMillion = %f, want 1.50", info.InputCostPerMillion)
	}
	if info.OutputCostPerMillion != 3.00 {
		t.Errorf("OutputCostPerMillion = %f, want 3.00", info.OutputCostPerMillion)
	}

	// Should be findable by alias
	info = catalog.GetModelInfo("custom")
	if info == nil {
		t.Fatal("GetModelInfo(\"custom\") returned nil after Register")
	}
	if info.ID != "custom-llm-v1" {
		t.Errorf("ID = %q, want \"custom-llm-v1\"", info.ID)
	}

	info = catalog.GetModelInfo("custom-v1")
	if info == nil {
		t.Fatal("GetModelInfo(\"custom-v1\") returned nil after Register")
	}
	if info.ID != "custom-llm-v1" {
		t.Errorf("ID = %q, want \"custom-llm-v1\"", info.ID)
	}

	// Should appear in ListModels
	models := catalog.ListModels("custom")
	if len(models) != 1 {
		t.Fatalf("ListModels(\"custom\") returned %d models, want 1", len(models))
	}
	if models[0].ID != "custom-llm-v1" {
		t.Errorf("ID = %q, want \"custom-llm-v1\"", models[0].ID)
	}

	// Total count should increase
	all := catalog.ListModels("")
	if len(all) != 8 {
		t.Errorf("ListModels(\"\") returned %d models, want 8 (7 built-in + 1 custom)", len(all))
	}
}

func TestBuiltInModelProperties(t *testing.T) {
	catalog := DefaultCatalog()

	t.Run("claude-opus-4-6", func(t *testing.T) {
		m := catalog.GetModelInfo("claude-opus-4-6")
		if m == nil {
			t.Fatal("model not found")
		}
		if m.ContextWindow != 200000 {
			t.Errorf("ContextWindow = %d, want 200000", m.ContextWindow)
		}
		if !m.SupportsTools {
			t.Error("expected SupportsTools = true")
		}
		if !m.SupportsVision {
			t.Error("expected SupportsVision = true")
		}
		if !m.SupportsReasoning {
			t.Error("expected SupportsReasoning = true")
		}
	})

	t.Run("gpt-5.2", func(t *testing.T) {
		m := catalog.GetModelInfo("gpt-5.2")
		if m == nil {
			t.Fatal("model not found")
		}
		if m.ContextWindow != 1047576 {
			t.Errorf("ContextWindow = %d, want 1047576", m.ContextWindow)
		}
		if !m.SupportsTools {
			t.Error("expected SupportsTools = true")
		}
		if !m.SupportsVision {
			t.Error("expected SupportsVision = true")
		}
		if !m.SupportsReasoning {
			t.Error("expected SupportsReasoning = true")
		}
	})

	t.Run("gemini-3-pro-preview", func(t *testing.T) {
		m := catalog.GetModelInfo("gemini-3-pro-preview")
		if m == nil {
			t.Fatal("model not found")
		}
		if m.ContextWindow != 1048576 {
			t.Errorf("ContextWindow = %d, want 1048576", m.ContextWindow)
		}
		if !m.SupportsTools {
			t.Error("expected SupportsTools = true")
		}
		if !m.SupportsVision {
			t.Error("expected SupportsVision = true")
		}
		if !m.SupportsReasoning {
			t.Error("expected SupportsReasoning = true")
		}
	})
}

func TestDefaultCatalogReturnsSeparateInstances(t *testing.T) {
	c1 := DefaultCatalog()
	c2 := DefaultCatalog()

	c1.Register(ModelInfo{
		ID:       "only-in-c1",
		Provider: "test",
	})

	// c2 should not have the model registered in c1
	if info := c2.GetModelInfo("only-in-c1"); info != nil {
		t.Error("DefaultCatalog() should return independent instances; c2 should not see c1's registration")
	}
}
