# Cold Launch Landing Page Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the minimal home page with a full-width "Cold Launch" marketing landing page that showcases Mammoth's end-to-end pipeline and stack architecture.

**Architecture:** Standalone HTML template (no nav rail layout.html wrapper) served at `/`. Existing project-list home moves to the app interior. New CSS with Cold Launch design tokens, vanilla JS for teletype animations and scroll reveals.

**Tech Stack:** Go html/template, embedded static assets, vanilla CSS/JS, Google Fonts (Space Grotesk, IBM Plex Mono)

---

### Task 1: Add landing page template engine support

The landing page is standalone (no nav rail), so the TemplateEngine needs a way to render templates without wrapping them in layout.html.

**Files:**
- Modify: `web/templates.go`
- Test: `web/templates_test.go`

**Step 1: Write the failing test**

Add to `web/templates_test.go`:

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./web/ -run TestRenderStandalone -v`
Expected: FAIL — RenderStandalone does not exist, landing.html does not exist

**Step 3: Write minimal implementation**

In `web/templates.go`, add a `standalone` map and `RenderStandalone` method:

```go
// In TemplateEngine struct, add:
// standalone map[string]*template.Template

// In NewTemplateEngine, after the pages loop, add:
standalonePages := []string{
    "landing.html",
}
engine.standalone = make(map[string]*template.Template)
for _, page := range standalonePages {
    t, err := template.New(page).Funcs(funcs).ParseFS(
        templateFS, "templates/"+page,
    )
    if err != nil {
        return nil, fmt.Errorf("parsing standalone template %s: %w", page, err)
    }
    engine.standalone[page] = t
}

// Add method:
func (e *TemplateEngine) RenderStandalone(w http.ResponseWriter, name string, data any) error {
    t, ok := e.standalone[name]
    if !ok {
        return fmt.Errorf("standalone template %q not found", name)
    }
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    return t.Execute(w, data)
}
```

Also create a minimal `web/templates/landing.html` placeholder (just enough to pass the test):

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>{{.Title}} - mammoth</title>
</head>
<body>
    <h1>The Software Factory That Fits in Your Pocket</h1>
</body>
</html>
```

**Step 4: Run test to verify it passes**

Run: `go test ./web/ -run TestRenderStandalone -v`
Expected: PASS

**Step 5: Commit**

```
feat(web): add standalone template rendering for landing page
```

---

### Task 2: Route `/` to landing page, move project list

Change the router so `/` serves the landing page and the old home (project list) is accessible at `/projects` as HTML.

**Files:**
- Modify: `web/server.go`
- Test: `web/server_test.go`

**Step 1: Write the failing tests**

Add to `web/server_test.go`:

```go
func TestServerLanding(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	// Landing page should NOT have nav rail.
	if strings.Contains(body, "nav-rail") {
		t.Error("landing page should not contain nav-rail")
	}
	// Should contain mammoth branding.
	if !strings.Contains(body, "mammoth") {
		t.Error("expected mammoth branding")
	}
	// Should contain the landing headline.
	if !strings.Contains(body, "Software Factory") {
		t.Error("expected landing page headline")
	}
}

func TestServerProjectsPageHTML(t *testing.T) {
	srv := newTestServer(t)

	if _, err := srv.store.Create("alpha"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "alpha") {
		t.Error("expected project name in project list page")
	}
	if !strings.Contains(body, "nav-rail") {
		t.Error("project list should use the nav rail layout")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./web/ -run "TestServerLanding|TestServerProjectsPageHTML" -v`
Expected: FAIL — `/` still renders old home with nav-rail; `/projects` returns JSON

**Step 3: Write implementation**

In `web/server.go`:

1. Add `handleLanding` method:
```go
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	data := PageData{Title: "Home"}
	if err := s.templates.RenderStandalone(w, "landing.html", data); err != nil {
		log.Printf("component=web.server action=render_failed view=landing err=%v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
```

2. In `buildRouter`, change `r.Get("/", s.handleHome)` to `r.Get("/", s.handleLanding)`

3. Modify `handleProjectList` to support both JSON and HTML:
```go
func (s *Server) handleProjectList(w http.ResponseWriter, r *http.Request) {
	projects := s.store.List()
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(projects)
		return
	}
	data := PageData{
		Title:    "Projects",
		Projects: projects,
	}
	if err := s.templates.Render(w, "home.html", data); err != nil {
		log.Printf("component=web.server action=render_failed view=home err=%v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./web/ -run "TestServerLanding|TestServerProjectsPageHTML|TestServerProjectList|TestServerHome" -v`
Expected: PASS (update TestServerHome to expect the landing page content)

**Step 5: Update existing test**

`TestServerHome` currently checks `/` for "mammoth" — update to check for landing-specific content instead.

**Step 6: Commit**

```
feat(web): route landing page at / and move project list to /projects
```

---

### Task 3: Create landing page CSS (Cold Launch tokens + sections)

Build the complete landing page stylesheet with Cold Launch design tokens.

**Files:**
- Create: `web/static/css/landing.css`

**Step 1: Create the CSS file**

The file should define:
- Landing-specific CSS custom properties (Cold Launch palette)
- Full-bleed sections alternating dark/paper backgrounds
- Hero section (centered, large type, CTA buttons)
- Pipeline journey cards (4-card grid with connecting lines)
- Stack architecture tiers (pyramid layout)
- How-it-works steps (3-column)
- Telemetry preview (monospace scrolling area)
- CTA section (centered)
- Footer (3-column)
- Tractor-feed perforated edge decoration
- Responsive breakpoints
- Scroll-reveal animation classes
- Teletype animation keyframes

Key CSS variables to define:
```css
:root {
    --cl-dark: #0B1821;
    --cl-dark-secondary: #1B3A4B;
    --cl-paper: #F4F1EC;
    --cl-amber: #D4651A;
    --cl-text: #1A1A18;
    --cl-muted: #8B9DAF;
    --cl-font-display: 'Space Grotesk', system-ui, sans-serif;
    --cl-font-mono: 'IBM Plex Mono', monospace;
    --cl-font-body: 'DM Sans', system-ui, sans-serif;
}
```

**Step 2: Verify embed picks it up**

The existing `//go:embed static/css/*.css` glob in `static_embed.go` already covers `static/css/landing.css`, so no changes needed.

**Step 3: Commit**

```
feat(web): add Cold Launch landing page styles
```

---

### Task 4: Create landing page JavaScript (teletype + scroll reveals)

Build vanilla JS for the landing page animations.

**Files:**
- Create: `web/static/js/landing.js`

**Step 1: Create the JS file**

Implement:
1. **Teletype animation** — types out the pipeline phase strip character by character with a blinking cursor
2. **Scroll reveal** — IntersectionObserver that adds `.revealed` class to elements with `[data-reveal]` when they enter viewport
3. **Telemetry simulation** — auto-scrolling fake build log with typewriter-timed new lines
4. **Stagger animation** — sequential reveal of child elements

Key functions:
```javascript
// Teletype: types text into element character by character
function teletype(element, text, speed)

// Scroll reveal: IntersectionObserver for fade-in on scroll
function initScrollReveal()

// Telemetry: simulated build log with auto-scroll
function initTelemetryPreview(container, lines)
```

**Step 2: Verify embed picks it up**

The existing `//go:embed static/js/*.js` glob covers this.

**Step 3: Commit**

```
feat(web): add landing page animations (teletype, scroll reveals, telemetry sim)
```

---

### Task 5: Build the full landing.html template

Replace the placeholder with the complete 7-section landing page.

**Files:**
- Modify: `web/templates/landing.html`

**Step 1: Write the failing test**

Add to `web/templates_test.go`:

```go
func TestLandingPageSections(t *testing.T) {
	engine, err := NewTemplateEngine()
	if err != nil {
		t.Fatalf("failed to create template engine: %v", err)
	}

	rec := httptest.NewRecorder()
	data := PageData{Title: "Home"}
	if err := engine.RenderStandalone(rec, "landing.html", data); err != nil {
		t.Fatalf("failed to render landing: %v", err)
	}

	body := rec.Body.String()

	sections := []struct {
		name    string
		marker  string
	}{
		{"hero", "cl-hero"},
		{"pipeline", "cl-pipeline"},
		{"stack", "cl-stack"},
		{"how-it-works", "cl-how"},
		{"telemetry", "cl-telemetry"},
		{"cta", "cl-cta"},
		{"footer", "cl-footer"},
	}
	for _, sec := range sections {
		if !strings.Contains(body, sec.marker) {
			t.Errorf("expected %s section (class %q) in landing page", sec.name, sec.marker)
		}
	}

	// Font loading.
	if !strings.Contains(body, "Space+Grotesk") {
		t.Error("expected Space Grotesk font import")
	}
	if !strings.Contains(body, "IBM+Plex+Mono") {
		t.Error("expected IBM Plex Mono font import")
	}

	// CSS and JS references.
	if !strings.Contains(body, "landing.css") {
		t.Error("expected landing.css stylesheet link")
	}
	if !strings.Contains(body, "landing.js") {
		t.Error("expected landing.js script reference")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./web/ -run TestLandingPageSections -v`
Expected: FAIL — placeholder landing.html lacks sections

**Step 3: Write the full template**

Build `web/templates/landing.html` with all 7 sections per the design doc:

1. **Hero** — Dark bg, eyebrow "ONE BINARY. ENTIRE FACTORY.", headline, subheadline, CTAs, animated telemetry strip
2. **Pipeline Journey** — Paper bg, 4 phase cards (01-04), dashed connectors, pulsing LEDs
3. **Stack Architecture** — Dark bg, 3 console tiers (LLM Client → Agent Loop → Attractor Engine)
4. **How It Works** — Paper bg, 3 numbered steps (Install, Describe, Build)
5. **Live Feel** — Dark bg, simulated telemetry output with typewriter timing
6. **CTA** — Dark bg, "Ready to launch?", amber button
7. **Footer** — Darkest bg, mammoth wordmark, links, 2389 Research credit, perforated edge

The template is self-contained (full HTML document, not a `{{define "content"}}` block).

**Step 4: Run test to verify it passes**

Run: `go test ./web/ -run TestLandingPageSections -v`
Expected: PASS

**Step 5: Commit**

```
feat(web): build complete Cold Launch landing page with all 7 sections
```

---

### Task 6: Update nav rail links and existing tests

Update the layout nav to link "Home" to `/projects` (the app interior). Fix any broken tests from routing changes.

**Files:**
- Modify: `web/templates/layout.html`
- Modify: `web/templates/home.html` (update hero text to be app-interior appropriate)
- Modify: `web/server_test.go` (fix TestServerHome expectations)
- Modify: `web/templates_test.go` (fix TestHomeRender if needed)

**Step 1: Update layout.html**

In the nav rail links, change the Home link from `/` to `/projects`:
```html
<a href="/projects" class="web-rail-link{{if eq .Title "Projects"}} active{{end}}">Projects</a>
```

**Step 2: Update home.html hero text**

Change "Welcome to mammoth" to "Your Projects" since this is now the app-interior projects page, not the marketing landing.

**Step 3: Fix tests**

- `TestServerHome`: Update to test the landing page at `/` (already covered by TestServerLanding, so this test can be updated or removed)
- `TestHomeRender`: Update expected content to match updated home.html
- `TestLayoutRender`: Should still pass since it renders home.html which still uses layout

**Step 4: Run all web tests**

Run: `go test ./web/ -v`
Expected: ALL PASS

**Step 5: Commit**

```
refactor(web): update nav links and home page for landing/app split
```

---

### Task 7: Integration test — full landing page flow

Write an integration test that verifies the end-to-end flow: landing page → click "Launch a Pipeline" → project creation.

**Files:**
- Modify: `web/server_test.go`

**Step 1: Write the test**

```go
func TestLandingToProjectFlow(t *testing.T) {
	srv := newTestServer(t)

	// 1. Landing page renders at /.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("landing: expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/projects/new") {
		t.Fatal("landing page should link to /projects/new")
	}

	// 2. New project page renders.
	req2 := httptest.NewRequest(http.MethodGet, "/projects/new", nil)
	req2.Header.Set("Accept", "text/html")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("new project: expected 200, got %d", rec2.Code)
	}

	// 3. Projects list page renders at /projects with HTML.
	req3 := httptest.NewRequest(http.MethodGet, "/projects", nil)
	req3.Header.Set("Accept", "text/html")
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("projects: expected 200, got %d", rec3.Code)
	}
	if !strings.Contains(rec3.Body.String(), "nav-rail") {
		t.Fatal("projects page should use nav rail layout")
	}
}
```

**Step 2: Run test**

Run: `go test ./web/ -run TestLandingToProjectFlow -v`
Expected: PASS

**Step 3: Run full test suite**

Run: `go test ./web/ -v`
Expected: ALL PASS

**Step 4: Commit**

```
test(web): add landing-to-project integration flow test
```

---

### Task 8: Final polish — run all tests and verify

**Step 1: Run all tests**

Run: `go test ./... -count=1`
Expected: ALL PASS

**Step 2: Manual check — verify the server starts**

Run: `go build ./cmd/mammoth/ && echo "build OK"`

**Step 3: Commit any remaining fixes**

```
chore(web): landing page cleanup and polish
```
