package web

import (
	"context"
	"os"
	"strings"

	"github.com/2389-research/mammoth/attractor"
)

// configureBuildInterviewer wires a default interviewer for wait.human nodes
// so review gates do not fail when running in the unified web flow.
func configureBuildInterviewer(reg *attractor.HandlerRegistry) {
	if reg == nil {
		return
	}
	h := reg.Get("wait.human")
	if h == nil {
		return
	}
	waitHandler, ok := h.(*attractor.WaitForHumanHandler)
	if !ok {
		return
	}
	waitHandler.Interviewer = attractor.NewCallbackInterviewer(func(_ context.Context, _ string, options []string) (string, error) {
		defaultAnswer := strings.TrimSpace(os.Getenv("MAMMOTH_HUMAN_GATE_DEFAULT"))
		if defaultAnswer == "" {
			defaultAnswer = "[Y] Yes"
		}
		if len(options) == 0 {
			return defaultAnswer, nil
		}

		normalized := normalizeHumanAnswer(defaultAnswer)
		for _, opt := range options {
			if normalizeHumanAnswer(opt) == normalized {
				return opt, nil
			}
		}
		for _, opt := range options {
			if strings.Contains(normalizeHumanAnswer(opt), normalized) || strings.Contains(normalized, normalizeHumanAnswer(opt)) {
				return opt, nil
			}
		}
		return options[0], nil
	})
}

func normalizeHumanAnswer(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, "[]")
	v = strings.ToLower(v)
	return strings.TrimSpace(v)
}
