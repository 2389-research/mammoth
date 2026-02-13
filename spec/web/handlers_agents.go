// ABOUTME: Web handlers for agent swarm lifecycle: start, pause, resume, and status.
// ABOUTME: Serves HTMX partials for agent LED indicators and status panels.
package web

import (
	"log"
	"net/http"

	"github.com/2389-research/mammoth/spec/server"
)

// AgentStatusData is the view-model for the agent_status partial.
type AgentStatusData struct {
	SpecID     string
	Running    bool
	Started    bool
	AgentCount int
}

// AgentLEDsData is the view-model for the agent_leds partial.
type AgentLEDsData struct {
	SpecID  string
	Running bool
	Started bool
}

// ProviderInfoView is the view-model for a single provider in the status partial.
type ProviderInfoView struct {
	Name      string
	HasAPIKey bool
	Model     string
}

// ProviderStatusData is the view-model for the provider_status partial.
type ProviderStatusData struct {
	DefaultProvider string
	DefaultModel    *string
	Providers       []ProviderInfoView
	AnyAvailable    bool
}

// swarmStatusData extracts common view-model fields from a SwarmHandle.
func swarmStatusData(specIDStr string, sh *server.SwarmHandle) AgentStatusData {
	return AgentStatusData{
		SpecID:     specIDStr,
		Running:    !sh.Orchestrator.IsPaused(),
		Started:    true,
		AgentCount: sh.Orchestrator.AgentCount(),
	}
}

// StartAgents starts the agent swarm for a spec and returns the agent_status partial.
// Uses TryStartAgents for atomic check-and-set to prevent concurrent requests from
// creating duplicate orchestrators.
func StartAgents(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		actor := state.GetActor(specID)
		if actor == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		specIDStr := specID.String()

		// If swarm already exists, report its status without checking LLM availability.
		// This avoids a spurious 503 if LLMClient was cleared after the swarm started.
		if existing := state.GetSwarm(specID); existing != nil {
			renderer.RenderPartial(w, "agent_status.html", swarmStatusData(specIDStr, existing))
			return
		}

		// Require an LLM provider to start a new swarm
		if state.LLMClient == nil {
			writeHTMLError(w, http.StatusServiceUnavailable,
				"No LLM provider configured. Set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY.")
			return
		}

		// TryStartAgents does atomic check-and-set: returns false if already running
		// or if the actor is missing.
		if !state.TryStartAgents(specID) {
			log.Printf("StartAgents: TryStartAgents returned false for spec %s (may already be running)", specIDStr)
		}

		// Report status (either freshly started or already running)
		if swarmHandle := state.GetSwarm(specID); swarmHandle != nil {
			renderer.RenderPartial(w, "agent_status.html", swarmStatusData(specIDStr, swarmHandle))
			return
		}

		renderer.RenderPartial(w, "agent_status.html", AgentStatusData{
			SpecID: specIDStr,
		})
	}
}

// PauseAgents pauses the agent swarm and returns the agent_status partial.
func PauseAgents(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		specIDStr := specID.String()
		swarmHandle := state.GetSwarm(specID)
		if swarmHandle != nil {
			swarmHandle.Orchestrator.Pause()
			renderer.RenderPartial(w, "agent_status.html", swarmStatusData(specIDStr, swarmHandle))
			return
		}

		renderer.RenderPartial(w, "agent_status.html", AgentStatusData{
			SpecID: specIDStr,
		})
	}
}

// ResumeAgents resumes a paused agent swarm and returns the agent_status partial.
func ResumeAgents(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		specIDStr := specID.String()
		swarmHandle := state.GetSwarm(specID)
		if swarmHandle != nil {
			swarmHandle.Orchestrator.Resume()
			renderer.RenderPartial(w, "agent_status.html", swarmStatusData(specIDStr, swarmHandle))
			return
		}

		renderer.RenderPartial(w, "agent_status.html", AgentStatusData{
			SpecID: specIDStr,
		})
	}
}

// AgentStatus returns the current agent status as an HTMX partial.
func AgentStatus(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		specIDStr := specID.String()
		swarmHandle := state.GetSwarm(specID)
		if swarmHandle != nil {
			renderer.RenderPartial(w, "agent_status.html", swarmStatusData(specIDStr, swarmHandle))
			return
		}

		renderer.RenderPartial(w, "agent_status.html", AgentStatusData{
			SpecID: specIDStr,
		})
	}
}

// AgentLEDs renders the agent LED indicators for the command bar.
func AgentLEDs(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		specIDStr := specID.String()
		swarmHandle := state.GetSwarm(specID)
		if swarmHandle != nil {
			renderer.RenderPartial(w, "agent_leds.html", AgentLEDsData{
				SpecID:  specIDStr,
				Running: !swarmHandle.Orchestrator.IsPaused(),
				Started: true,
			})
			return
		}

		renderer.RenderPartial(w, "agent_leds.html", AgentLEDsData{
			SpecID: specIDStr,
		})
	}
}

// ProviderStatus renders the provider status partial showing available LLM providers.
func ProviderStatus(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ps := state.ProviderStatus
		providers := make([]ProviderInfoView, len(ps.Providers))
		for i, p := range ps.Providers {
			providers[i] = ProviderInfoView{
				Name:      p.Name,
				HasAPIKey: p.HasAPIKey,
				Model:     p.Model,
			}
		}

		renderer.RenderPartial(w, "provider_status.html", ProviderStatusData{
			DefaultProvider: ps.DefaultProvider,
			DefaultModel:    ps.DefaultModel,
			Providers:       providers,
			AnyAvailable:    ps.AnyAvailable,
		})
	}
}
