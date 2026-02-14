// ABOUTME: HTTP handler methods for all server endpoints
// ABOUTME: Covers landing page, session CRUD, graph mutations, undo/redo, export, and validation

package editor

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/2389-research/mammoth/dot"
	"github.com/go-chi/chi/v5"
)

const basePathHeader = "X-Mammoth-Editor-Base-Path"
const projectPathHeader = "X-Mammoth-Project-Path"
const buildStartPathHeader = "X-Mammoth-Build-Start-Path"

func basePathFromRequest(r *http.Request) string {
	base := strings.TrimSpace(r.Header.Get(basePathHeader))
	base = strings.TrimSuffix(base, "/")
	if base == "/" {
		return ""
	}
	return base
}

func templateDataFromRequest(r *http.Request) TemplateData {
	return TemplateData{
		BasePath:       basePathFromRequest(r),
		ProjectPath:    strings.TrimSpace(r.Header.Get(projectPathHeader)),
		BuildStartPath: strings.TrimSpace(r.Header.Get(buildStartPathHeader)),
	}
}

// handleLanding renders the landing page with the upload form.
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	data := templateDataFromRequest(r)
	s.renderPage(w, data, http.StatusOK)
}

// handleCreateSession creates a new session from posted DOT source or file upload.
// Enforces a 10MB body limit and returns 413 if exceeded.
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	// Enforce 10MB body limit to prevent oversized uploads
	const maxBodySize = 10 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	if err := r.ParseMultipartForm(maxBodySize); err != nil {
		if err.Error() == "http: request body too large" {
			s.renderPage(w, TemplateData{Error: "Upload too large (max 10MB)"}, http.StatusRequestEntityTooLarge)
			return
		}
		// Fall back to regular form parsing
		if err := r.ParseForm(); err != nil {
			s.renderPage(w, TemplateData{Error: "failed to parse form"}, http.StatusUnprocessableEntity)
			return
		}
	}

	dotSource := r.FormValue("dot")

	// Try file upload if no textarea content
	if dotSource == "" {
		file, _, err := r.FormFile("file")
		if err == nil {
			defer file.Close()
			content, err := io.ReadAll(file)
			if err != nil {
				s.renderPage(w, TemplateData{Error: "Upload too large (max 10MB)"}, http.StatusRequestEntityTooLarge)
				return
			}
			dotSource = string(content)
		}
	}

	if strings.TrimSpace(dotSource) == "" {
		s.renderPage(w, TemplateData{Error: "DOT source is required"}, http.StatusUnprocessableEntity)
		return
	}

	sess, err := s.store.Create(dotSource)
	if err != nil {
		s.renderPage(w, TemplateData{Error: fmt.Sprintf("Invalid DOT: %v", err)}, http.StatusUnprocessableEntity)
		return
	}

	base := basePathFromRequest(r)
	http.Redirect(w, r, base+"/sessions/"+sess.ID, http.StatusSeeOther)
}

// handleEditorPage renders the editor for an existing session.
func (s *Server) handleEditorPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	sess.RLock()
	defer sess.RUnlock()

	data := TemplateData{
		Session:   sess,
		SessionID: id,
	}
	reqData := templateDataFromRequest(r)
	data.BasePath = reqData.BasePath
	data.ProjectPath = reqData.ProjectPath
	data.BuildStartPath = reqData.BuildStartPath
	s.renderEditor(w, data, http.StatusOK)
}

// handleExport returns the raw DOT as a downloadable file.
// Sanitizes the graph name for use as a filename to prevent path traversal and injection.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	sess.RLock()
	filename := sanitizeFilename(sess.Graph.Name)
	rawDOT := sess.RawDOT
	sess.RUnlock()

	w.Header().Set("Content-Type", "text/vnd.graphviz")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(rawDOT))
}

// handleValidate re-runs the linter and returns diagnostics.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	sess.Validate()

	sess.RLock()
	defer sess.RUnlock()

	data := TemplateData{
		Session:   sess,
		SessionID: id,
	}
	reqData := templateDataFromRequest(r)
	data.BasePath = reqData.BasePath
	data.ProjectPath = reqData.ProjectPath
	data.BuildStartPath = reqData.BuildStartPath
	s.renderPartial(w, "diagnostics", data, http.StatusOK)
}

// handleUpdateDOT replaces the session's DOT source.
// Enforces a 10MB body limit matching createSession.
func (s *Server) handleUpdateDOT(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Enforce 10MB body limit to prevent oversized updates
	const maxBodySize = 10 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	if err := r.ParseForm(); err != nil {
		if err.Error() == "http: request body too large" {
			http.Error(w, "Request body too large (max 10MB)", http.StatusRequestEntityTooLarge)
			return
		}
		s.renderError(w, r, id, sess, "failed to parse form")
		return
	}
	dotSource := r.FormValue("dot")

	if err := sess.UpdateDOT(dotSource); err != nil {
		s.renderError(w, r, id, sess, fmt.Sprintf("Update failed: %v", err))
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleUpdateNode updates attributes on an existing node.
func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "nodeId")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, id, sess, "failed to parse form")
		return
	}
	attrs := extractAttrs(r)

	if err := sess.UpdateNode(nodeID, attrs); err != nil {
		s.renderError(w, r, id, sess, fmt.Sprintf("Update node failed: %v", err))
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleAddNode adds a new node to the graph.
func (s *Server) handleAddNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, id, sess, "failed to parse form")
		return
	}
	nodeID := r.FormValue("id")
	attrs := extractAttrs(r)

	if err := sess.AddNode(nodeID, attrs); err != nil {
		s.renderError(w, r, id, sess, fmt.Sprintf("Add node failed: %v", err))
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleDeleteNode removes a node and its connected edges.
func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "nodeId")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := sess.RemoveNode(nodeID); err != nil {
		s.renderError(w, r, id, sess, fmt.Sprintf("Delete node failed: %v", err))
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleAddEdge adds a new edge between two nodes.
func (s *Server) handleAddEdge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, id, sess, "failed to parse form")
		return
	}
	from := r.FormValue("from")
	to := r.FormValue("to")
	attrs := extractAttrs(r)

	if err := sess.AddEdge(from, to, attrs); err != nil {
		s.renderError(w, r, id, sess, fmt.Sprintf("Add edge failed: %v", err))
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleUpdateEdge updates attributes on an existing edge.
func (s *Server) handleUpdateEdge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	edgeID := chi.URLParam(r, "edgeId")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, id, sess, "failed to parse form")
		return
	}
	attrs := extractAttrs(r)

	if err := sess.UpdateEdge(edgeID, attrs); err != nil {
		s.renderError(w, r, id, sess, fmt.Sprintf("Update edge failed: %v", err))
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleDeleteEdge removes an edge by its stable ID.
func (s *Server) handleDeleteEdge(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	edgeID := chi.URLParam(r, "edgeId")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := sess.RemoveEdge(edgeID); err != nil {
		s.renderError(w, r, id, sess, fmt.Sprintf("Delete edge failed: %v", err))
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleUpdateGraphAttrs updates graph-level attributes.
func (s *Server) handleUpdateGraphAttrs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, id, sess, "failed to parse form")
		return
	}
	attrs := extractAttrs(r)

	if err := sess.UpdateGraphAttrs(attrs); err != nil {
		s.renderError(w, r, id, sess, fmt.Sprintf("Update attrs failed: %v", err))
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleUndo reverts the last mutation.
func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := sess.Undo(); err != nil {
		sess.RLock()
		data := templateDataFromRequest(r)
		data.Session = sess
		data.SessionID = id
		data.Error = err.Error()
		s.renderPartial(w, "diagnostics", data, http.StatusUnprocessableEntity)
		sess.RUnlock()
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleRedo reapplies a previously undone mutation.
func (s *Server) handleRedo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	if err := sess.Redo(); err != nil {
		sess.RLock()
		data := templateDataFromRequest(r)
		data.Session = sess
		data.SessionID = id
		data.Error = err.Error()
		s.renderPartial(w, "diagnostics", data, http.StatusUnprocessableEntity)
		sess.RUnlock()
		return
	}

	s.renderAllPartials(w, r, id, sess)
}

// handleNodeEditForm returns the node_edit_form partial for inline editing of a node.
func (s *Server) handleNodeEditForm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	nodeID := chi.URLParam(r, "nodeId")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	sess.RLock()
	defer sess.RUnlock()

	node, found := sess.Graph.Nodes[nodeID]
	if !found {
		http.NotFound(w, r)
		return
	}

	data := NodeEditData{
		SessionID: id,
		BasePath:  basePathFromRequest(r),
		NodeID:    nodeID,
		Node:      node,
	}
	s.renderPartial(w, "node_edit_form", data, http.StatusOK)
}

// handleEdgeEditForm returns the edge_edit_form partial for inline editing of an edge.
func (s *Server) handleEdgeEditForm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	edgeID := chi.URLParam(r, "edgeId")
	sess, ok := s.store.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	sess.RLock()
	defer sess.RUnlock()

	var edge *dot.Edge
	for _, e := range sess.Graph.Edges {
		if e.ID == edgeID {
			edge = e
			break
		}
	}
	if edge == nil {
		http.NotFound(w, r)
		return
	}

	data := EdgeEditData{
		SessionID: id,
		BasePath:  basePathFromRequest(r),
		EdgeID:    edgeID,
		Edge:      edge,
	}
	s.renderPartial(w, "edge_edit_form", data, http.StatusOK)
}

// sanitizeFilename strips path separators, control chars, and quotes from a graph name
// to produce a safe filename. Falls back to "graph.dot" if the result is empty.
func sanitizeFilename(name string) string {
	if name == "" {
		return "graph.dot"
	}

	// Strip path separators, control characters, and quotes
	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || r == '"' || r == '\'' || r < 32 || r == 127 {
			continue
		}
		b.WriteRune(r)
	}

	sanitized := strings.TrimSpace(b.String())
	if sanitized == "" {
		return "graph.dot"
	}

	return sanitized + ".dot"
}

// extractAttrs pulls key-value pairs from form data where keys are prefixed with "attr_".
func extractAttrs(r *http.Request) map[string]string {
	attrs := make(map[string]string)
	for key, values := range r.Form {
		if strings.HasPrefix(key, "attr_") && len(values) > 0 {
			attrName := strings.TrimPrefix(key, "attr_")
			attrs[attrName] = values[0]
		}
	}
	return attrs
}

// renderPage renders the full layout with landing content using the landing template set.
func (s *Server) renderPage(w http.ResponseWriter, data TemplateData, status int) {
	var buf bytes.Buffer
	if err := s.landingTmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// renderEditor renders the full layout with editor content using the editor template set.
func (s *Server) renderEditor(w http.ResponseWriter, data TemplateData, status int) {
	var buf bytes.Buffer
	if err := s.editorTmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// renderAllPartials renders all htmx partials concatenated for swap responses.
// Holds a read lock on the session to prevent concurrent map iteration panics.
func (s *Server) renderAllPartials(w http.ResponseWriter, r *http.Request, sessionID string, sess *Session) {
	sess.RLock()
	defer sess.RUnlock()

	data := TemplateData{
		Session:   sess,
		SessionID: sessionID,
	}
	reqData := templateDataFromRequest(r)
	data.BasePath = reqData.BasePath
	data.ProjectPath = reqData.ProjectPath
	data.BuildStartPath = reqData.BuildStartPath

	var buf bytes.Buffer
	partials := []string{"code_editor", "graph_viewer", "property_panel", "diagnostics"}
	for _, name := range partials {
		if err := s.templates.ExecuteTemplate(&buf, name, data); err != nil {
			http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

// renderPartial renders a single named template partial.
func (s *Server) renderPartial(w http.ResponseWriter, name string, data interface{}, status int) {
	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
		return
	}
	if status > 0 {
		w.WriteHeader(status)
	}
	w.Write(buf.Bytes())
}

// renderError renders partials with an error message and a 422 status.
// Holds a read lock on the session to prevent concurrent map iteration panics.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, sessionID string, sess *Session, errMsg string) {
	sess.RLock()
	defer sess.RUnlock()

	data := TemplateData{
		Session:   sess,
		SessionID: sessionID,
		Error:     errMsg,
	}
	reqData := templateDataFromRequest(r)
	data.BasePath = reqData.BasePath
	data.ProjectPath = reqData.ProjectPath
	data.BuildStartPath = reqData.BuildStartPath
	w.WriteHeader(http.StatusUnprocessableEntity)

	var buf bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buf, "diagnostics", data); err != nil {
		w.Write([]byte(errMsg))
		return
	}
	w.Write(buf.Bytes())
}
