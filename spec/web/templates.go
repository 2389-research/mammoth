// ABOUTME: Template loading, rendering, and FuncMap for the mammoth spec builder web UI.
// ABOUTME: Provides TemplateRenderer that parses base + partials and renders named templates.
package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yuin/goldmark"
)

// TemplateRenderer loads and renders HTML templates for the web UI.
// Templates are parsed once at construction and reused for each request.
type TemplateRenderer struct {
	templates *template.Template
}

// NewTemplateRenderer parses all templates from the given directory.
// It expects a base.html layout and a partials/ subdirectory with partial templates.
func NewTemplateRenderer(templatesDir string) (*TemplateRenderer, error) {
	funcMap := buildFuncMap()

	// Parse base template and index page template.
	// index.html defines blocks ("title", "nav", "workspace") that override the
	// defaults in base.html via Go's {{ block }} / {{ define }} mechanism.
	basePath := filepath.Join(templatesDir, "base.html")
	indexPath := filepath.Join(templatesDir, "index.html")
	tmpl, err := template.New("base.html").Funcs(funcMap).ParseFiles(basePath, indexPath)
	if err != nil {
		return nil, fmt.Errorf("parse base/index templates: %w", err)
	}

	// Parse all partial templates
	partialsDir := filepath.Join(templatesDir, "partials")
	err = filepath.WalkDir(partialsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		_, parseErr := tmpl.ParseFiles(path)
		if parseErr != nil {
			return fmt.Errorf("parse partial %s: %w", path, parseErr)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse partial templates: %w", err)
	}

	return &TemplateRenderer{templates: tmpl}, nil
}

// NewTemplateRendererFromFS parses all templates from an fs.FS, reading from
// the "templates/" subtree. This allows the unified server to use the embedded
// filesystem instead of runtime filesystem paths.
func NewTemplateRendererFromFS(fsys fs.FS) (*TemplateRenderer, error) {
	funcMap := buildFuncMap()

	templatesFS, err := fs.Sub(fsys, "templates")
	if err != nil {
		return nil, fmt.Errorf("templates sub-FS: %w", err)
	}

	baseData, err := fs.ReadFile(templatesFS, "base.html")
	if err != nil {
		return nil, fmt.Errorf("read base.html: %w", err)
	}

	tmpl, err := template.New("base.html").Funcs(funcMap).Parse(string(baseData))
	if err != nil {
		return nil, fmt.Errorf("parse base.html: %w", err)
	}

	indexData, err := fs.ReadFile(templatesFS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}
	if _, err := tmpl.New("index.html").Parse(string(indexData)); err != nil {
		return nil, fmt.Errorf("parse index.html: %w", err)
	}

	entries, err := fs.ReadDir(templatesFS, "partials")
	if err != nil {
		return nil, fmt.Errorf("read partials dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		data, err := fs.ReadFile(templatesFS, "partials/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read partial %s: %w", entry.Name(), err)
		}
		if _, err := tmpl.New(entry.Name()).Parse(string(data)); err != nil {
			return nil, fmt.Errorf("parse partial %s: %w", entry.Name(), err)
		}
	}

	return &TemplateRenderer{templates: tmpl}, nil
}

// Render executes a named template (full page) and writes the result to w.
// The template is rendered inside the base layout.
func (r *TemplateRenderer) Render(w http.ResponseWriter, templateName string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.templates.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, fmt.Sprintf("template render error: %v", err), http.StatusInternalServerError)
	}
}

// RenderPartial executes a named partial template and writes the result to w.
// No base layout wrapping is applied.
func (r *TemplateRenderer) RenderPartial(w http.ResponseWriter, partialName string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.templates.ExecuteTemplate(w, partialName, data); err != nil {
		http.Error(w, fmt.Sprintf("partial render error: %v", err), http.StatusInternalServerError)
	}
}

// buildFuncMap creates the template FuncMap with helper functions for rendering.
func buildFuncMap() template.FuncMap {
	return template.FuncMap{
		"markdown":     markdownToHTML,
		"timeAgo":      timeAgo,
		"json":         jsonEncode,
		"truncate":     truncate,
		"cardTypeIcon": cardTypeIcon,
		"safeHTML":     safeHTML,
		"dict":         dict,
	}
}

// markdownToHTML converts a markdown string to HTML using goldmark.
// The output is sanitized to remove script tags and dangerous URL schemes.
func markdownToHTML(input string) template.HTML {
	var buf bytes.Buffer
	md := goldmark.New()
	if err := md.Convert([]byte(input), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(input))
	}
	return template.HTML(sanitizeHTML(buf.String()))
}

// timeAgo formats a time as a relative duration string (e.g. "5m ago", "2h ago").
func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}

// jsonEncode marshals a value to JSON for embedding in script contexts.
// Returns template.JS so html/template does not double-escape the output.
// This is safe because json.Marshal escapes all JS-dangerous characters.
func jsonEncode(v any) (template.JS, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return template.JS(data), nil
}

// truncate shortens a string to at most maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// cardTypeIcon returns an emoji for a given card type.
func cardTypeIcon(cardType string) string {
	switch cardType {
	case "idea", "inspiration", "vibes":
		return "\U0001f4a1"
	case "task":
		return "\U0001f4cb"
	case "plan":
		return "\U0001f5fa\ufe0f"
	case "decision":
		return "\u2696\ufe0f"
	case "constraint", "spec_constraint":
		return "\U0001f512"
	case "risk":
		return "\u26a0\ufe0f"
	case "assumption":
		return "\U0001f914"
	case "open_question":
		return "\u2753"
	case "success_criteria":
		return "\u2705"
	default:
		return "\U0001f4cc"
	}
}

// safeHTML marks a string as safe HTML, preventing double-escaping in templates.
func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

// dict creates a map[string]any from alternating key-value pairs.
// Useful for passing multiple values into sub-templates.
// Usage: {{ template "partial" (dict "key1" val1 "key2" val2) }}
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments (%d)", len(pairs))
	}
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key at position %d is not a string", i)
		}
		m[key] = pairs[i+1]
	}
	return m, nil
}

// RenderMarkdown is an exported helper that converts markdown to HTML.
// Used by handlers that need pre-rendered HTML before template execution.
// The output is sanitized to remove script tags and dangerous URL schemes.
func RenderMarkdown(input string) string {
	var buf bytes.Buffer
	md := goldmark.New()
	if err := md.Convert([]byte(input), &buf); err != nil {
		return template.HTMLEscapeString(input)
	}
	return sanitizeHTML(buf.String())
}

// scriptTagPattern matches <script> tags and their contents (case-insensitive).
var scriptTagPattern = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)

// dangerousSchemePattern matches javascript: and vbscript: URL schemes in attributes
// (case-insensitive, allowing whitespace within the scheme).
var dangerousSchemePattern = regexp.MustCompile(`(?i)(href|src|action)\s*=\s*["']?\s*(javascript|vbscript)\s*:`)

// sanitizeHTML removes dangerous HTML constructs from goldmark-rendered output.
// It strips <script> tags and their contents, and neutralizes javascript:/vbscript: URLs.
func sanitizeHTML(html string) string {
	// Remove <script> tags and their contents
	html = scriptTagPattern.ReplaceAllString(html, "")

	// Replace dangerous URL schemes with a safe empty fragment
	html = dangerousSchemePattern.ReplaceAllStringFunc(html, func(match string) string {
		// Find the attribute name (href, src, action) and preserve it
		eqIdx := strings.IndexByte(match, '=')
		if eqIdx < 0 {
			return match
		}
		attr := strings.TrimSpace(match[:eqIdx])
		// Determine the quote style used (or none)
		rest := match[eqIdx+1:]
		quote := ""
		trimmed := strings.TrimLeft(rest, " \t")
		if len(trimmed) > 0 && (trimmed[0] == '"' || trimmed[0] == '\'') {
			quote = string(trimmed[0])
		}
		return attr + "=" + quote + "#blocked"
	})

	return html
}
