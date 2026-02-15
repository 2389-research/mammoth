package web

import (
	"strings"
)

type specBuilderOptions struct {
	HumanReview     bool
	ScenarioTesting bool
	TDD             bool
}

func parseSpecBuilderOptions(formValue func(string) string) specBuilderOptions {
	return specBuilderOptions{
		HumanReview:     isTruthy(formValue("opt_human_review")),
		ScenarioTesting: isTruthy(formValue("opt_scenario_testing")),
		TDD:             isTruthy(formValue("opt_tdd")),
	}
}

func (o specBuilderOptions) ToConstraintText() string {
	lines := []string{
		"[mammoth.option.human_review=" + boolString(o.HumanReview) + "]",
		"[mammoth.option.scenario_testing=" + boolString(o.ScenarioTesting) + "]",
		"[mammoth.option.tdd=" + boolString(o.TDD) + "]",
	}

	// Human-readable hints retained for context in planning prompts.
	var hints []string
	if o.HumanReview {
		hints = append(hints, "- Include human review gates for risky or high-impact stages.")
	}
	if o.ScenarioTesting {
		hints = append(hints, "- Include scenario/integration test validation before ship.")
	}
	if o.TDD {
		hints = append(hints, "- Prefer test-driven development with unit tests before implementation.")
	}
	if len(hints) > 0 {
		lines = append(lines, hints...)
	}
	return strings.Join(lines, "\n")
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func mergeSpecOptionConstraints(existing string, opts specBuilderOptions) string {
	lines := strings.Split(existing, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[mammoth.option.") && strings.HasSuffix(trimmed, "]") {
			continue
		}
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}

	optionBlock := strings.Split(opts.ToConstraintText(), "\n")
	merged := make([]string, 0, len(optionBlock)+len(filtered))
	merged = append(merged, optionBlock...)
	merged = append(merged, filtered...)
	return strings.Join(merged, "\n")
}

func optionsFromConstraints(raw string) specBuilderOptions {
	opts := specBuilderOptions{
		HumanReview:     true,
		ScenarioTesting: true,
		TDD:             true,
	}
	if strings.TrimSpace(raw) == "" {
		return opts
	}
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[mammoth.option.") || !strings.HasSuffix(trimmed, "]") {
			continue
		}
		body := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
		parts := strings.SplitN(body, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.EqualFold(strings.TrimSpace(parts[1]), "true")
		switch key {
		case "mammoth.option.human_review":
			opts.HumanReview = val
		case "mammoth.option.scenario_testing":
			opts.ScenarioTesting = val
		case "mammoth.option.tdd":
			opts.TDD = val
		}
	}
	return opts
}
