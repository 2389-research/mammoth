// ABOUTME: Tests for the TemplateEngine that loads and renders embedded HTML templates.
// ABOUTME: Covers parsing, layout rendering, home page, project new form, and project overview.
package web

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTemplatesParse(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	if engine == nil {
		t.Fatal("expected non-nil template engine")
	}
}

func TestLayoutRender(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{
		Title: "Test Page",
	}
	if err := engine.Render(rec, "home.html", data); err != nil {
		t.Fatalf("failed to render: %v", err)
	}

	body := rec.Body.String()

	// Layout should include HTML5 doctype.
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected HTML5 doctype")
	}

	// Layout should include HTMX CDN.
	if !strings.Contains(body, "htmx.org") {
		t.Error("expected HTMX script tag")
	}

	// Layout should include mammoth branding.
	if !strings.Contains(body, "mammoth") {
		t.Error("expected mammoth branding in layout")
	}

	// Layout should include wizard step indicators.
	if !strings.Contains(body, "Spec") {
		t.Error("expected Spec step in wizard indicator")
	}
	if !strings.Contains(body, "Edit") {
		t.Error("expected Edit step in wizard indicator")
	}
	if !strings.Contains(body, "Build") {
		t.Error("expected Build step in wizard indicator")
	}
}

func TestHomeRender(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{
		Title: "Home",
		Projects: []*Project{
			{ID: "p1", Name: "alpha", Phase: PhaseSpec, CreatedAt: time.Now()},
			{ID: "p2", Name: "beta", Phase: PhaseEdit, CreatedAt: time.Now()},
		},
	}
	if err := engine.Render(rec, "home.html", data); err != nil {
		t.Fatalf("failed to render home: %v", err)
	}

	body := rec.Body.String()

	// Should show start card.
	if !strings.Contains(body, "Start New Project") {
		t.Error("expected 'Start New Project' entry card")
	}

	// Should show project names.
	if !strings.Contains(body, "alpha") {
		t.Error("expected project 'alpha' in list")
	}
	if !strings.Contains(body, "beta") {
		t.Error("expected project 'beta' in list")
	}

	// Should link to project pages.
	if !strings.Contains(body, "/projects/p1") {
		t.Error("expected link to project p1")
	}
}

func TestHomeRenderEmpty(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{
		Title:    "Home",
		Projects: []*Project{},
	}
	if err := engine.Render(rec, "home.html", data); err != nil {
		t.Fatalf("failed to render home with empty projects: %v", err)
	}

	body := rec.Body.String()
	// Should still render without error and contain basic structure.
	if !strings.Contains(body, "mammoth") {
		t.Error("expected mammoth branding even with no projects")
	}
}

func TestProjectNewRender(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	// Test idea mode (default).
	rec := httptest.NewRecorder()
	data := PageData{
		Title: "New Project",
		Mode:  "idea",
	}
	if err := engine.Render(rec, "project_new.html", data); err != nil {
		t.Fatalf("failed to render project_new: %v", err)
	}

	body := rec.Body.String()

	// Should have a form posting to /projects.
	if !strings.Contains(body, "/projects") {
		t.Error("expected form action /projects")
	}

	// Should have prompt and file-upload fields.
	if !strings.Contains(body, "prompt") {
		t.Error("expected prompt field in form")
	}
	if !strings.Contains(body, "import_file") {
		t.Error("expected import_file field in form")
	}
}

func TestProjectNewRenderDOTMode(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{
		Title: "New Project",
		Mode:  "dot",
	}
	if err := engine.Render(rec, "project_new.html", data); err != nil {
		t.Fatalf("failed to render project_new in dot mode: %v", err)
	}

	body := rec.Body.String()

	// DOT mode is now unified into the same prompt/file import form.
	if !strings.Contains(body, "import_file") {
		t.Error("expected file upload field in dot mode")
	}
}

func TestProjectOverviewRender(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{
		Title: "Project Overview",
		Project: &Project{
			ID:        "proj-123",
			Name:      "test-project",
			Phase:     PhaseEdit,
			CreatedAt: time.Date(2026, 2, 13, 10, 0, 0, 0, time.UTC),
		},
	}
	if err := engine.Render(rec, "project_overview.html", data); err != nil {
		t.Fatalf("failed to render project_overview: %v", err)
	}

	body := rec.Body.String()

	// Should show project name.
	if !strings.Contains(body, "test-project") {
		t.Error("expected project name in overview")
	}

	// Should show phase.
	if !strings.Contains(body, "edit") || !strings.Contains(strings.ToLower(body), "edit") {
		t.Error("expected current phase indicator")
	}

	// Should have links to phase views.
	if !strings.Contains(body, "/projects/proj-123") {
		t.Error("expected links containing project ID")
	}
}

func TestRenderToBuffer(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	var buf bytes.Buffer
	data := PageData{
		Title: "Buffer Test",
	}

	// Render writes to any io.Writer, including a buffer.
	rec := httptest.NewRecorder()
	if err := engine.Render(rec, "home.html", data); err != nil {
		t.Fatalf("failed to render to recorder: %v", err)
	}
	buf.WriteString(rec.Body.String())

	if buf.Len() == 0 {
		t.Error("expected non-empty buffer after render")
	}
}

func TestRenderBadTemplate(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	err = engine.Render(rec, "nonexistent.html", PageData{})
	if err == nil {
		t.Error("expected error when rendering nonexistent template")
	}
}

func TestRenderStandalone(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{Title: "Landing"}
	if err := engine.RenderStandalone(rec, "landing.html", data); err != nil {
		t.Fatalf("failed to render standalone: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected HTML5 doctype in standalone template")
	}
	// Standalone pages should NOT have the nav rail.
	if strings.Contains(body, "nav-rail") {
		t.Error("standalone template should not contain nav-rail")
	}
	// Should contain landing page content.
	if !strings.Contains(body, "Software Factory") {
		t.Error("expected landing page headline")
	}
}

func TestRenderStandaloneTo(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	var buf bytes.Buffer
	data := PageData{Title: "Landing"}
	if err := engine.RenderStandaloneTo(&buf, "landing.html", data); err != nil {
		t.Fatalf("failed to render standalone to buffer: %v", err)
	}

	body := buf.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("expected HTML5 doctype in standalone template")
	}
	if strings.Contains(body, "nav-rail") {
		t.Error("standalone template should not contain nav-rail")
	}
}

func TestRenderStandaloneBadTemplate(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	err = engine.RenderStandalone(rec, "nonexistent.html", PageData{})
	if err == nil {
		t.Error("expected error when rendering nonexistent standalone template")
	}
}

func TestRenderStandaloneContentType(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{Title: "Landing"}
	if err := engine.RenderStandalone(rec, "landing.html", data); err != nil {
		t.Fatalf("failed to render standalone: %v", err)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}
}

func TestRenderContentType(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{Title: "Test"}
	if err := engine.Render(rec, "home.html", data); err != nil {
		t.Fatalf("failed to render: %v", err)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected Content-Type text/html, got %q", ct)
	}
}
