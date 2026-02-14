// ABOUTME: Adapter layer connecting the editor package to the unified project-scoped web flow.
// ABOUTME: Mounts editor handlers under /projects/{projectID}/editor and keeps project DOT in sync.
package web

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

const editorBasePathHeader = "X-Mammoth-Editor-Base-Path"
const editorProjectPathHeader = "X-Mammoth-Project-Path"
const editorBuildStartHeader = "X-Mammoth-Build-Start-Path"

func (s *Server) editorRouter(r chi.Router) {
	r.Get("/", s.handleProjectEditorEntry)
	r.Handle("/*", s.editorProxyHandler())
}

func (s *Server) handleProjectEditorEntry(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	p, ok := s.store.Get(projectID)
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}

	sessionID, err := s.ensureProjectEditorSession(projectID, p)
	if err != nil {
		http.Error(w, "failed to initialize editor", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, projectEditorBasePath(projectID)+"/sessions/"+sessionID, http.StatusSeeOther)
}

func (s *Server) editorProxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		if _, ok := s.store.Get(projectID); !ok {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}

		basePath := projectEditorBasePath(projectID)
		subPath := strings.TrimPrefix(r.URL.Path, basePath)
		if subPath == "" {
			subPath = "/"
		}
		if !strings.HasPrefix(subPath, "/") {
			subPath = "/" + subPath
		}

		r2 := r.Clone(r.Context())
		u := *r.URL
		u.Path = subPath
		if r.URL.RawPath != "" {
			u.RawPath = subPath
		}
		r2.URL = &u
		r2.RequestURI = subPath
		r2.Header = r.Header.Clone()
		r2.Header.Set(editorBasePathHeader, basePath)
		r2.Header.Set(editorProjectPathHeader, "/projects/"+projectID)
		r2.Header.Set(editorBuildStartHeader, "/projects/"+projectID+"/build/start")

		rw := &editorProxyResponseWriter{ResponseWriter: w}
		s.editorServer.ServeHTTP(rw, r2)

		if loc := rw.Header().Get("Location"); strings.HasPrefix(loc, "/sessions/") {
			rw.Header().Set("Location", basePath+loc)
		}

		sessionID := extractEditorSessionID(subPath)
		if sessionID == "" {
			sessionID = extractEditorSessionID(rw.Header().Get("Location"))
		}
		if sessionID != "" {
			if err := s.syncProjectFromEditorSession(projectID, sessionID); err != nil {
				log.Printf("editor sync: project=%s session=%s err=%v", projectID, sessionID, err)
			} else {
				s.editorMu.Lock()
				s.editorByProj[projectID] = sessionID
				s.editorMu.Unlock()
			}
		}
	})
}

type editorProxyResponseWriter struct {
	http.ResponseWriter
	code int
}

func (rw *editorProxyResponseWriter) WriteHeader(code int) {
	rw.code = code
	rw.ResponseWriter.WriteHeader(code)
}

func (s *Server) ensureProjectEditorSession(projectID string, p *Project) (string, error) {
	s.editorMu.Lock()
	if sessionID := s.editorByProj[projectID]; sessionID != "" {
		if _, ok := s.editorStore.Get(sessionID); ok {
			s.editorMu.Unlock()
			// Keep project phase/diagnostics in sync when returning to an existing session.
			_ = s.syncProjectFromEditorSession(projectID, sessionID)
			return sessionID, nil
		}
	}
	s.editorMu.Unlock()

	// If DOT isn't present yet but a spec exists, export it so the editor starts
	// from the real spec graph instead of a placeholder.
	if strings.TrimSpace(p.DOT) == "" && p.SpecID != "" {
		if err := s.syncProjectFromSpec(projectID, p); err == nil {
			if refreshed, ok := s.store.Get(projectID); ok {
				p = refreshed
			}
		}
	}

	dotSrc := strings.TrimSpace(p.DOT)
	if dotSrc == "" {
		dotSrc = defaultProjectDOT(p.Name)
	}

	sess, err := s.editorStore.Create(dotSrc)
	if err != nil {
		// Fallback to a known-valid graph so the editor can still open.
		sess, err = s.editorStore.Create(defaultProjectDOT(p.Name))
		if err != nil {
			return "", fmt.Errorf("create editor session: %w", err)
		}
	}

	s.editorMu.Lock()
	// Another request may have initialized a session while we were creating one.
	if sessionID := s.editorByProj[projectID]; sessionID != "" {
		if _, ok := s.editorStore.Get(sessionID); ok {
			s.editorMu.Unlock()
			_ = s.syncProjectFromEditorSession(projectID, sessionID)
			return sessionID, nil
		}
	}
	s.editorByProj[projectID] = sess.ID
	s.editorMu.Unlock()
	if err := s.syncProjectFromEditorSession(projectID, sess.ID); err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (s *Server) syncProjectFromEditorSession(projectID, sessionID string) error {
	sess, ok := s.editorStore.Get(sessionID)
	if !ok {
		return fmt.Errorf("session not found")
	}

	sess.RLock()
	rawDOT := sess.RawDOT
	diags := append([]string(nil), formatDiagnostics(sess.Diagnostics)...)
	sess.RUnlock()

	p, ok := s.store.Get(projectID)
	if !ok {
		return fmt.Errorf("project not found")
	}
	p.DOT = rawDOT
	p.Diagnostics = diags
	p.Phase = PhaseEdit
	if err := s.store.Update(p); err != nil {
		return fmt.Errorf("update project: %w", err)
	}

	return nil
}

func extractEditorSessionID(path string) string {
	u, err := url.Parse(path)
	if err == nil {
		path = u.Path
	}

	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "sessions" && parts[i+1] != "" {
			return parts[i+1]
		}
	}
	return ""
}

func projectEditorBasePath(projectID string) string {
	return "/projects/" + projectID + "/editor"
}

func defaultProjectDOT(name string) string {
	label := strings.TrimSpace(name)
	if label == "" {
		label = "pipeline"
	}
	return "digraph pipeline {\n" +
		"  graph [goal=\"" + strings.ReplaceAll(label, "\"", "") + "\"]\n" +
		"  start [shape=Mdiamond]\n" +
		"  done [shape=Msquare]\n" +
		"  start -> done\n" +
		"}\n"
}
