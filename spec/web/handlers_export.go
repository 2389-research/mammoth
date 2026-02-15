// ABOUTME: Web handlers for exporting specs as Markdown, YAML, and DOT files.
// ABOUTME: Serves both in-page artifact views and downloadable file attachments.
package web

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/2389-research/mammoth/dot"
	"github.com/2389-research/mammoth/spec/core"
	coreexport "github.com/2389-research/mammoth/spec/core/export"
	"github.com/2389-research/mammoth/spec/export"
	"github.com/2389-research/mammoth/spec/server"
)

// ArtifactsData is the view-model for the artifacts tab partial.
type ArtifactsData struct {
	SpecID          string
	MarkdownContent string
	YAMLContent     string
	DOTContent      string
}

// DiagramData is the view-model for the diagram tab partial.
type DiagramData struct {
	SpecID     string
	DOTContent string
	Steps      []DiagramStep
}

// DiagramStep is a per-node execution summary shown under the diagram graph.
type DiagramStep struct {
	Index    int
	NodeID   string
	Label    string
	NodeType string
	Prompt   string
	Outgoing []DiagramStepEdge
}

// DiagramStepEdge represents one outgoing edge from a node in the step list.
type DiagramStepEdge struct {
	To    string
	Label string
}

// exportDOTSafe wraps export.ExportDOT and returns an error comment on failure.
func exportDOTSafe(s *core.SpecState) string {
	return coreexport.ExportDOT(s)
}

// Artifacts renders the artifacts tab with all three export formats.
func Artifacts(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var data ArtifactsData
		handle.ReadState(func(s *core.SpecState) {
			data.SpecID = specID.String()
			data.MarkdownContent = export.ExportMarkdown(s)
			yamlContent, err := export.ExportYAML(s)
			if err != nil {
				data.YAMLContent = fmt.Sprintf("# YAML export error: %v", err)
			} else {
				data.YAMLContent = yamlContent
			}
			data.DOTContent = exportDOTSafe(s)
		})

		renderer.RenderPartial(w, "artifacts.html", data)
	}
}

// Diagram renders the diagram tab with a DOT graph visualization.
func Diagram(state *server.AppState, renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var data DiagramData
		handle.ReadState(func(s *core.SpecState) {
			data = DiagramData{
				SpecID:     specID.String(),
				DOTContent: exportDOTSafe(s),
			}
			data.Steps = buildDiagramSteps(data.DOTContent)
		})

		renderer.RenderPartial(w, "diagram.html", data)
	}
}

func buildDiagramSteps(dotContent string) []DiagramStep {
	dotContent = strings.TrimSpace(dotContent)
	if dotContent == "" {
		return nil
	}
	g, err := dot.Parse(dotContent)
	if err != nil || g == nil {
		return nil
	}

	ordered := orderedNodeIDs(g)
	if len(ordered) == 0 {
		return nil
	}

	steps := make([]DiagramStep, 0, len(ordered))
	for i, nodeID := range ordered {
		n := g.FindNode(nodeID)
		if n == nil {
			continue
		}
		label := strings.TrimSpace(n.Attrs["label"])
		if label == "" {
			label = nodeID
		}
		step := DiagramStep{
			Index:    i + 1,
			NodeID:   nodeID,
			Label:    label,
			NodeType: strings.TrimSpace(n.Attrs["type"]),
			Prompt:   strings.TrimSpace(n.Attrs["prompt"]),
			Outgoing: make([]DiagramStepEdge, 0),
		}
		for _, e := range g.Edges {
			if e.From != nodeID {
				continue
			}
			step.Outgoing = append(step.Outgoing, DiagramStepEdge{
				To:    e.To,
				Label: strings.TrimSpace(e.Attrs["label"]),
			})
		}
		steps = append(steps, step)
	}
	return steps
}

func orderedNodeIDs(g *dot.Graph) []string {
	if g == nil || len(g.Nodes) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(g.Nodes))
	ordered := make([]string, 0, len(g.Nodes))
	var q []string
	if start := g.FindStartNode(); start != nil && start.ID != "" {
		q = append(q, start.ID)
	}

	for len(q) > 0 {
		id := q[0]
		q = q[1:]
		if seen[id] {
			continue
		}
		if g.FindNode(id) == nil {
			continue
		}
		seen[id] = true
		ordered = append(ordered, id)

		next := make([]string, 0, 4)
		for _, e := range g.Edges {
			if e.From == id && e.To != "" && !seen[e.To] {
				next = append(next, e.To)
			}
		}
		sort.Strings(next)
		q = append(q, next...)
	}

	remaining := g.NodeIDs()
	for _, id := range remaining {
		if !seen[id] {
			ordered = append(ordered, id)
		}
	}
	return ordered
}

// ExportMarkdown serves the spec as a downloadable Markdown file.
func ExportMarkdown(state *server.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var content string
		handle.ReadState(func(s *core.SpecState) {
			content = export.ExportMarkdown(s)
		})

		w.Header().Set("Content-Type", "text/markdown")
		w.Header().Set("Content-Disposition", `attachment; filename="spec.md"`)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, content)
	}
}

// ExportYAML serves the spec as a downloadable YAML file.
func ExportYAML(state *server.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var content string
		var exportErr error
		handle.ReadState(func(s *core.SpecState) {
			content, exportErr = export.ExportYAML(s)
		})

		if exportErr != nil {
			log.Printf("YAML export failed for spec %s: %v", specID, exportErr)
			writeHTMLError(w, http.StatusInternalServerError, "Failed to export YAML.")
			return
		}

		w.Header().Set("Content-Type", "text/yaml")
		w.Header().Set("Content-Disposition", `attachment; filename="spec.yaml"`)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, content)
	}
}

// ExportDOT serves the spec as a downloadable DOT graph file.
func ExportDOT(state *server.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var content string
		handle.ReadState(func(s *core.SpecState) {
			content = exportDOTSafe(s)
		})

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="spec.dot"`)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, content)
	}
}

// Regenerate exports all formats to disk and returns a confirmation HTML snippet.
func Regenerate(state *server.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		specID, ok := parseSpecID(w, r)
		if !ok {
			return
		}

		handle := state.GetActor(specID)
		if handle == nil {
			writeHTMLError(w, http.StatusNotFound, "Spec not found.")
			return
		}

		var markdownContent, yamlContent, dotContent string
		var specTitle string
		handle.ReadState(func(s *core.SpecState) {
			markdownContent = export.ExportMarkdown(s)
			yaml, err := export.ExportYAML(s)
			if err != nil {
				yamlContent = fmt.Sprintf("# YAML export error: %v", err)
			} else {
				yamlContent = yaml
			}
			dotContent = exportDOTSafe(s)
			if s.Core != nil {
				specTitle = s.Core.Title
			}
		})

		if specTitle == "" {
			specTitle = "spec"
		}

		// Write to $MAMMOTH_HOME/specs/<spec_id>/exports/
		exportsDir := filepath.Join(state.MammothHome, "specs", specID.String(), "exports")
		if err := os.MkdirAll(exportsDir, 0o755); err != nil {
			log.Printf("failed to create exports directory: %v", err)
		} else {
			slug := toSlug(specTitle)
			if err := os.WriteFile(filepath.Join(exportsDir, slug+".md"), []byte(markdownContent), 0o644); err != nil {
				log.Printf("failed to write markdown export: %v", err)
			}
			if err := os.WriteFile(filepath.Join(exportsDir, slug+".yaml"), []byte(yamlContent), 0o644); err != nil {
				log.Printf("failed to write YAML export: %v", err)
			}
			if err := os.WriteFile(filepath.Join(exportsDir, slug+".dot"), []byte(dotContent), 0o644); err != nil {
				log.Printf("failed to write DOT export: %v", err)
			}
			log.Printf("regenerated exports for spec %s at %s", specID, exportsDir)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `<span class="regen-confirm">Exports saved successfully.</span>`)
	}
}

// toSlug converts a title to a URL-safe slug for filenames.
func toSlug(title string) string {
	var result strings.Builder
	for _, ch := range strings.ToLower(title) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			result.WriteRune(ch)
		} else {
			result.WriteRune('-')
		}
	}
	slug := result.String()
	// Trim trailing hyphens
	slug = strings.TrimRight(slug, "-")
	if slug == "" {
		return "spec"
	}
	return slug
}
