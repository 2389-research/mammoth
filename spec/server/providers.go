// ABOUTME: LLM provider status detection from environment variables.
// ABOUTME: Checks for Anthropic, OpenAI, and Gemini API keys without exposing secrets.
package server

import "os"

// ProviderInfo describes the status of a single LLM provider.
type ProviderInfo struct {
	Name      string  `json:"name"`
	HasAPIKey bool    `json:"has_api_key"`
	Model     string  `json:"model"`
	BaseURL   *string `json:"base_url,omitempty"`
}

// ProviderStatus is the aggregated provider availability for the UI.
type ProviderStatus struct {
	DefaultProvider string         `json:"default_provider"`
	DefaultModel    *string        `json:"default_model,omitempty"`
	Providers       []ProviderInfo `json:"providers"`
	AnyAvailable    bool           `json:"any_available"`
}

// DetectProviders checks environment variables to determine which LLM providers are configured.
func DetectProviders() ProviderStatus {
	defaultProvider := nonEmptyEnvOr("MAMMOTH_DEFAULT_PROVIDER", "anthropic")
	defaultModel := nonEmptyEnv("MAMMOTH_DEFAULT_MODEL")

	providers := []ProviderInfo{
		checkProvider("anthropic", "ANTHROPIC_API_KEY", "ANTHROPIC_MODEL", "ANTHROPIC_BASE_URL", "claude-sonnet-4-5-20250929"),
		checkProvider("openai", "OPENAI_API_KEY", "OPENAI_MODEL", "OPENAI_BASE_URL", "gpt-4o"),
		checkProvider("gemini", "GEMINI_API_KEY", "GEMINI_MODEL", "GEMINI_BASE_URL", "gemini-2.0-flash"),
	}

	anyAvailable := false
	for _, p := range providers {
		if p.HasAPIKey {
			anyAvailable = true
			break
		}
	}

	var modelPtr *string
	if defaultModel != "" {
		modelPtr = &defaultModel
	}

	return ProviderStatus{
		DefaultProvider: defaultProvider,
		DefaultModel:    modelPtr,
		Providers:       providers,
		AnyAvailable:    anyAvailable,
	}
}

func checkProvider(name, keyVar, modelVar, baseURLVar, defaultModel string) ProviderInfo {
	hasKey := nonEmptyEnv(keyVar) != ""
	model := nonEmptyEnvOr(modelVar, defaultModel)
	baseURL := nonEmptyEnv(baseURLVar)

	var baseURLPtr *string
	if baseURL != "" {
		baseURLPtr = &baseURL
	}

	return ProviderInfo{
		Name:      name,
		HasAPIKey: hasKey,
		Model:     model,
		BaseURL:   baseURLPtr,
	}
}

func nonEmptyEnvOr(key, defaultVal string) string {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	return v
}
