// ABOUTME: Condition expression language for edge guards in the pipeline graph.
// ABOUTME: Evaluates clauses like "outcome = success && context.mode = prod" against Outcome and Context.
package attractor

import (
	"strings"
)

// EvaluateCondition evaluates a condition expression against an outcome and context.
// Condition grammar: Clause ('&&' Clause)*
// Clause: Key Operator Literal
// Key: 'outcome' | 'preferred_label' | 'context.' Path | bare identifier
// Operator: '=' | '!='
// An empty or whitespace-only condition evaluates to true (unconditional edge).
func EvaluateCondition(condition string, outcome *Outcome, ctx *Context) bool {
	trimmed := strings.TrimSpace(condition)
	if trimmed == "" {
		return true
	}

	clauses := strings.Split(trimmed, "&&")
	for _, clause := range clauses {
		if !evaluateClause(strings.TrimSpace(clause), outcome, ctx) {
			return false
		}
	}
	return true
}

// evaluateClause evaluates a single "key op literal" clause.
func evaluateClause(clause string, outcome *Outcome, ctx *Context) bool {
	// Try != first (longer operator)
	if idx := strings.Index(clause, "!="); idx >= 0 {
		key := strings.TrimSpace(clause[:idx])
		literal := strings.TrimSpace(clause[idx+2:])
		resolved := resolveKey(key, outcome, ctx)
		return resolved != literal
	}

	// Try =
	if idx := strings.Index(clause, "="); idx >= 0 {
		key := strings.TrimSpace(clause[:idx])
		literal := strings.TrimSpace(clause[idx+1:])
		resolved := resolveKey(key, outcome, ctx)
		return resolved == literal
	}

	// No operator found -- clause is malformed, treat as false
	return false
}

// resolveKey resolves a key to its string value from outcome or context.
// "outcome" -> outcome.Status
// "preferred_label" -> outcome.PreferredLabel
// "context.X" -> ctx.GetString("context.X") with fallback to ctx.GetString("X")
// bare key -> ctx.GetString(key)
func resolveKey(key string, outcome *Outcome, ctx *Context) string {
	switch key {
	case "outcome":
		return string(outcome.Status)
	case "preferred_label":
		return outcome.PreferredLabel
	default:
		if strings.HasPrefix(key, "context.") {
			// First try the full key including "context." prefix
			val := ctx.GetString(key, "")
			if val != "" {
				return val
			}
			// Fall back to the part after "context."
			suffix := key[len("context."):]
			return ctx.GetString(suffix, "")
		}
		return ctx.GetString(key, "")
	}
}

// ValidateConditionSyntax checks whether a condition string is syntactically valid.
// Returns true if the condition can be parsed, false otherwise.
func ValidateConditionSyntax(condition string) bool {
	trimmed := strings.TrimSpace(condition)
	if trimmed == "" {
		return true
	}

	clauses := strings.Split(trimmed, "&&")
	for _, clause := range clauses {
		c := strings.TrimSpace(clause)
		if c == "" {
			return false
		}
		// Must contain = or !=
		if !strings.Contains(c, "=") {
			return false
		}
		// Check for invalid operators (like >> or <<)
		hasValidOp := false
		if idx := strings.Index(c, "!="); idx >= 0 {
			key := strings.TrimSpace(c[:idx])
			if key != "" {
				hasValidOp = true
			}
		} else if idx := strings.Index(c, "="); idx >= 0 {
			key := strings.TrimSpace(c[:idx])
			if key != "" {
				hasValidOp = true
			}
		}
		if !hasValidOp {
			return false
		}
	}
	return true
}
