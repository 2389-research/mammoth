// ABOUTME: HTTP handler that auto-fixes DOT graph validation errors using an LLM.
// ABOUTME: Parses, validates, and rewrites the project DOT via the configured backend.
package web

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/2389-research/mammoth/attractor"
	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/dot/validator"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleDOTFix(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		httpError(w, "project not found", 404)
		return
	}
	if strings.TrimSpace(p.DOT) == "" {
		httpError(w, "no DOT graph to fix", 400)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	fixedDOT, err := s.dotFixer(ctx, p)
	if err != nil {
		p.Phase = PhaseEdit
		p.Diagnostics = prependErrorDiagnostic(p.Diagnostics, fmt.Sprintf("error: [agent_fix_failed] %v", err))
		if updateErr := s.store.Update(p); updateErr != nil {
			log.Printf("component=web.dot_fix action=update_project_failed project_id=%s err=%v", projectID, updateErr)
			httpError(w, "failed to persist diagnostics", 500)
			return
		}
		http.Redirect(w, r, "/projects/"+projectID, http.StatusSeeOther)
		return
	}

	g, parseErr := dot.Parse(fixedDOT)
	if parseErr != nil {
		p.Phase = PhaseEdit
		p.Diagnostics = prependErrorDiagnostic(p.Diagnostics,
			fmt.Sprintf("error: [agent_fix_invalid_dot] agent returned invalid DOT: %v", parseErr))
		if updateErr := s.store.Update(p); updateErr != nil {
			log.Printf("component=web.dot_fix action=update_project_failed project_id=%s err=%v", projectID, updateErr)
			httpError(w, "failed to persist diagnostics", 500)
			return
		}
		http.Redirect(w, r, "/projects/"+projectID, http.StatusSeeOther)
		return
	}

	diags := validator.Lint(g)
	p.DOT = fixedDOT
	p.Phase = PhaseEdit
	p.Diagnostics = formatDiagnostics(diags)
	if hasErrors(diags) {
		p.Diagnostics = prependBuildBlockedSummary(
			p.Diagnostics,
			countSeverity(diags, "error"),
			countSeverity(diags, "warning"),
		)
	}
	if err := s.store.Update(p); err != nil {
		log.Printf("component=web.dot_fix action=update_project_failed project_id=%s err=%v", projectID, err)
		httpError(w, "failed to update project", 500)
		return
	}

	// Keep an existing editor session in sync with the fixed DOT.
	s.editorMu.Lock()
	sessionID := s.editorByProj[projectID]
	s.editorMu.Unlock()
	if sessionID != "" {
		if sess, ok := s.editorStore.Get(sessionID); ok {
			if err := sess.UpdateDOT(fixedDOT); err != nil {
				log.Printf("component=web.dot_fix action=sync_editor_session_failed project_id=%s session_id=%s err=%v", projectID, sessionID, err)
			}
		}
	}

	http.Redirect(w, r, projectEditorBasePath(projectID), http.StatusSeeOther)
}

func (s *Server) fixDOTWithAgent(ctx context.Context, p *Project) (string, error) {
	backend := detectBackendFromEnv(false)
	if backend == nil {
		return "", fmt.Errorf("no LLM backend configured; set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY")
	}

	result, err := backend.RunAgent(ctx, attractor.AgentRunConfig{
		Prompt:       buildDOTFixPrompt(p.DOT, p.Diagnostics),
		Goal:         "Return a corrected DOT graph that resolves validation errors.",
		NodeID:       "dot_fix",
		Provider:     os.Getenv("MAMMOTH_DEFAULT_PROVIDER"),
		Model:        os.Getenv("MAMMOTH_DEFAULT_MODEL"),
		WorkDir:      p.DataDir,
		MaxTurns:     8,
		FidelityMode: "compact",
	})
	if err != nil {
		return "", fmt.Errorf("agent fix run failed: %w", err)
	}
	if result == nil || strings.TrimSpace(result.Output) == "" {
		return "", fmt.Errorf("agent returned empty response")
	}

	dotText, err := extractDOTFromAgentOutput(result.Output)
	if err != nil {
		return "", fmt.Errorf("failed to extract DOT from agent response: %w", err)
	}
	return dotText, nil
}

func buildDOTFixPrompt(dotSource string, diagnostics []string) string {
	var b strings.Builder
	b.WriteString("Fix this Graphviz DOT pipeline for mammoth.\n")
	b.WriteString("Return ONLY corrected DOT text. No markdown. No explanation.\n")
	b.WriteString("Keep node intent and prompts unless required to satisfy validation.\n\n")
	if len(diagnostics) > 0 {
		b.WriteString("Current validation diagnostics:\n")
		for _, d := range diagnostics {
			b.WriteString("- ")
			b.WriteString(d)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("DOT to fix:\n")
	b.WriteString(dotSource)
	b.WriteString("\n")
	return b.String()
}

func extractDOTFromAgentOutput(output string) (string, error) {
	s := strings.TrimSpace(output)
	lower := strings.ToLower(s)
	start := strings.Index(lower, "digraph")
	if start < 0 {
		return "", fmt.Errorf("response does not contain digraph")
	}
	openRel := strings.Index(s[start:], "{")
	if openRel < 0 {
		return "", fmt.Errorf("digraph body not found")
	}
	open := start + openRel
	end, ok := findMatchingBrace(s, open)
	if !ok {
		return "", fmt.Errorf("unterminated graph body")
	}
	return strings.TrimSpace(s[start : end+1]), nil
}

func findMatchingBrace(s string, open int) (int, bool) {
	depth := 0
	inSingle := false
	inDouble := false
	escape := false
	for i := open; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if inSingle || inDouble {
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return -1, false
}

func prependErrorDiagnostic(diags []string, msg string) []string {
	out := make([]string, 0, len(diags)+1)
	out = append(out, msg)
	out = append(out, diags...)
	return out
}

func httpError(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	_, _ = w.Write([]byte(msg))
}
