// ABOUTME: TemplateEngine loads embedded HTML templates and renders them with Go's html/template.
// ABOUTME: Templates are embedded at compile time via go:embed for zero runtime path issues.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
)

//go:embed templates/*.html
var templateFS embed.FS

// PageData holds all data passed to templates for rendering.
type PageData struct {
	Title       string
	Project     *Project
	Projects    []*Project
	Mode        string // "idea" or "dot" for project_new
	ActivePhase string // current wizard phase for highlighting
	Diagnostics DiagnosticsView
}

// TemplateEngine loads and renders embedded HTML templates.
type TemplateEngine struct {
	templates  map[string]*template.Template
	standalone map[string]*template.Template
}

// templateFuncs returns the FuncMap available to all templates.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"lower": strings.ToLower,
		"eq":    func(a, b string) bool { return a == b },
	}
}

// NewTemplateEngine parses all embedded templates and returns a ready-to-use engine.
// Each page template is parsed together with the layout so that the layout wraps every page.
func NewTemplateEngine() (*TemplateEngine, error) {
	funcs := templateFuncs()

	pages := []string{
		"home.html",
		"project_new.html",
		"project_overview.html",
		"build_view.html",
		"final_view.html",
	}

	engine := &TemplateEngine{
		templates:  make(map[string]*template.Template),
		standalone: make(map[string]*template.Template),
	}

	for _, page := range pages {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(
			templateFS,
			"templates/layout.html",
			"templates/"+page,
		)
		if err != nil {
			return nil, fmt.Errorf("parsing template %s: %w", page, err)
		}
		engine.templates[page] = t
	}

	// Standalone templates are rendered without the layout wrapper.
	// Used for pages that need full control of their HTML.
	standalonePages := []string{}

	for _, page := range standalonePages {
		t, err := template.New(page).Funcs(funcs).ParseFS(
			templateFS,
			"templates/"+page,
		)
		if err != nil {
			return nil, fmt.Errorf("parsing standalone template %s: %w", page, err)
		}
		engine.standalone[page] = t
	}

	return engine, nil
}

// Render executes the named template with the given data and writes the result
// to w. It sets the Content-Type header to text/html.
func (e *TemplateEngine) Render(w http.ResponseWriter, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.ExecuteTemplate(w, "layout.html", data)
}

// RenderTo executes the named template with the given data and writes the
// result to an arbitrary io.Writer (useful for testing without HTTP).
func (e *TemplateEngine) RenderTo(w io.Writer, name string, data any) error {
	t, ok := e.templates[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}

	return t.ExecuteTemplate(w, "layout.html", data)
}

// RenderStandalone executes a standalone template (no layout wrapping) and
// writes the result to w. It sets the Content-Type header to text/html.
func (e *TemplateEngine) RenderStandalone(w http.ResponseWriter, name string, data any) error {
	t, ok := e.standalone[name]
	if !ok {
		return fmt.Errorf("standalone template %q not found", name)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.Execute(w, data)
}

// RenderStandaloneTo executes a standalone template (no layout wrapping) and
// writes the result to an arbitrary io.Writer.
func (e *TemplateEngine) RenderStandaloneTo(w io.Writer, name string, data any) error {
	t, ok := e.standalone[name]
	if !ok {
		return fmt.Errorf("standalone template %q not found", name)
	}

	return t.Execute(w, data)
}
