package web

import "testing"

func TestDetectBackendFromEnv_WithAPIKey(t *testing.T) {
	t.Setenv("MAMMOTH_BACKEND", "")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	backend := detectBackendFromEnv(false)
	if backend == nil {
		t.Fatal("expected non-nil backend when API key is set")
	}
}

func TestDetectBackendFromEnv_NoKeys(t *testing.T) {
	t.Setenv("MAMMOTH_BACKEND", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	backend := detectBackendFromEnv(false)
	if backend != nil {
		t.Fatalf("expected nil backend when no keys are set, got %T", backend)
	}
}
