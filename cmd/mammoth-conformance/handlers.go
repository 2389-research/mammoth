// ABOUTME: List-handlers command for the conformance CLI, outputting registered handler types.
// ABOUTME: Queries the default handler registry and returns a sorted JSON array of type strings.
package main

import (
	"encoding/json"
	"os"
	"sort"

	"github.com/2389-research/mammoth/attractor"
)

// getHandlerTypes returns a sorted list of all handler type strings from the default registry.
func getHandlerTypes() []string {
	registry := attractor.DefaultHandlerRegistry()
	all := registry.All()
	types := make([]string, 0, len(all))
	for typeName := range all {
		types = append(types, typeName)
	}
	sort.Strings(types)
	return types
}

// cmdListHandlers writes the sorted handler types as a JSON array to stdout.
// Always returns 0.
func cmdListHandlers() int {
	types := getHandlerTypes()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(types)
	return 0
}
