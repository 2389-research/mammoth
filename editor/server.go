// ABOUTME: HTTP server struct with chi router, session store, and template engine
// ABOUTME: Configures all routes, static file serving, and wires handler methods

package editor

import (
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/2389-research/mammoth/dot"
	"github.com/go-chi/chi/v5"
)

// TemplateData holds the data passed to HTML templates for rendering pages and partials.
type TemplateData struct {
	Session        *Session
	SessionID      string
	BasePath       string
	ProjectPath    string
	BuildStartPath string
	DotFixPath     string
	Error          string
}

// NodeEditData holds the data passed to the node_edit_form template partial.
type NodeEditData struct {
	SessionID string
	BasePath  string
	NodeID    string
	Node      *dot.Node
}

// EdgeEditData holds the data passed to the edge_edit_form template partial.
type EdgeEditData struct {
	SessionID string
	BasePath  string
	EdgeID    string
	Edge      *dot.Edge
}

// Server holds the chi router, session store, and parsed templates.
// The templates field holds the shared partials. The landingTmpl and editorTmpl
// fields hold page-specific template sets that each define their own "content" block.
type Server struct {
	router      chi.Router
	store       *Store
	templates   *template.Template
	landingTmpl *template.Template
	editorTmpl  *template.Template
}

// NewServer creates a Server with all routes configured and templates parsed.
func NewServer(store *Store, templateDir string, staticDir string) *Server {
	s := &Server{
		store: store,
	}

	// Parse shared templates: layout + partials
	shared := template.Must(template.ParseGlob(filepath.Join(templateDir, "partials", "*.html")))
	template.Must(shared.ParseFiles(filepath.Join(templateDir, "layout.html")))
	s.templates = shared

	// Build page-specific template sets by cloning shared templates
	// and adding the page that defines "content"
	landingClone := template.Must(shared.Clone())
	template.Must(landingClone.ParseFiles(filepath.Join(templateDir, "landing.html")))
	s.landingTmpl = landingClone

	editorClone := template.Must(shared.Clone())
	template.Must(editorClone.ParseFiles(filepath.Join(templateDir, "editor.html")))
	s.editorTmpl = editorClone

	// Build router
	r := chi.NewRouter()

	// Static files
	fileServer := http.FileServer(http.Dir(staticDir))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Landing page
	r.Get("/", s.handleLanding)

	// Session lifecycle
	r.Post("/sessions", s.handleCreateSession)
	r.Get("/sessions/{id}", s.handleEditorPage)
	r.Get("/sessions/{id}/export", s.handleExport)
	r.Get("/sessions/{id}/validate", s.handleValidate)

	// Edit form handlers (return partials for inline editing)
	r.Get("/sessions/{id}/nodes/{nodeId}/edit", s.handleNodeEditForm)
	r.Get("/sessions/{id}/node-edit", s.handleNodeEditFormByQuery)
	r.Get("/sessions/{id}/edges/{edgeId}/edit", s.handleEdgeEditForm)
	r.Get("/sessions/{id}/edge-edit", s.handleEdgeEditFormByQuery)

	// Mutation handlers
	r.Post("/sessions/{id}/dot", s.handleUpdateDOT)
	r.Post("/sessions/{id}/nodes/{nodeId}", s.handleUpdateNode)
	r.Post("/sessions/{id}/nodes", s.handleAddNode)
	r.Delete("/sessions/{id}/nodes/{nodeId}", s.handleDeleteNode)
	r.Post("/sessions/{id}/edges", s.handleAddEdge)
	r.Post("/sessions/{id}/edges/{edgeId}", s.handleUpdateEdge)
	r.Delete("/sessions/{id}/edges/{edgeId}", s.handleDeleteEdge)
	r.Post("/sessions/{id}/attrs", s.handleUpdateGraphAttrs)
	r.Post("/sessions/{id}/undo", s.handleUndo)
	r.Post("/sessions/{id}/redo", s.handleRedo)

	s.router = r
	return s
}

// ServeHTTP implements the http.Handler interface, delegating to the chi router.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}
