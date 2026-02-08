// ABOUTME: Context fidelity modes controlling how much context is carried between pipeline nodes.
// ABOUTME: Implements precedence resolution: edge > node > graph default > "compact".
package attractor

// FidelityMode represents how much context is carried between nodes.
type FidelityMode string

const (
	FidelityFull          FidelityMode = "full"
	FidelityTruncate      FidelityMode = "truncate"
	FidelityCompact       FidelityMode = "compact"
	FidelitySummaryLow    FidelityMode = "summary:low"
	FidelitySummaryMedium FidelityMode = "summary:medium"
	FidelitySummaryHigh   FidelityMode = "summary:high"
)

// validFidelityModes is the authoritative set of recognized fidelity mode strings.
var validFidelityModes = map[string]bool{
	string(FidelityFull):          true,
	string(FidelityTruncate):      true,
	string(FidelityCompact):       true,
	string(FidelitySummaryLow):    true,
	string(FidelitySummaryMedium): true,
	string(FidelitySummaryHigh):   true,
}

// ValidFidelityModes returns the list of valid fidelity mode strings.
func ValidFidelityModes() []string {
	return []string{
		string(FidelityFull),
		string(FidelityTruncate),
		string(FidelityCompact),
		string(FidelitySummaryLow),
		string(FidelitySummaryMedium),
		string(FidelitySummaryHigh),
	}
}

// IsValidFidelity checks if a string is a valid fidelity mode.
func IsValidFidelity(mode string) bool {
	return validFidelityModes[mode]
}

// ResolveFidelity resolves the fidelity mode for a target node using precedence:
// 1. Edge fidelity attribute (on incoming edge)
// 2. Target node fidelity attribute
// 3. Graph default_fidelity attribute
// 4. Default: compact
func ResolveFidelity(edge *Edge, targetNode *Node, graph *Graph) FidelityMode {
	// Precedence 1: edge attribute
	if edge != nil && edge.Attrs != nil {
		if f, ok := edge.Attrs["fidelity"]; ok && IsValidFidelity(f) {
			return FidelityMode(f)
		}
	}

	// Precedence 2: target node attribute
	if targetNode != nil && targetNode.Attrs != nil {
		if f, ok := targetNode.Attrs["fidelity"]; ok && IsValidFidelity(f) {
			return FidelityMode(f)
		}
	}

	// Precedence 3: graph default
	if graph != nil && graph.Attrs != nil {
		if f, ok := graph.Attrs["default_fidelity"]; ok && IsValidFidelity(f) {
			return FidelityMode(f)
		}
	}

	// Precedence 4: hardcoded default
	return FidelityCompact
}
