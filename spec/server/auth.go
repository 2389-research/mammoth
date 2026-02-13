// ABOUTME: Bearer token authentication middleware for API and web routes.
// ABOUTME: Supports Authorization header and mammoth_token cookie for browser sessions.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// AuthMiddleware returns an http.Handler middleware that validates bearer tokens
// on /api/* and /web/* routes. Static assets, health checks, and the login
// endpoint pass through unprotected. For browser sessions, the middleware also
// accepts a mammoth_token cookie (set by the /login endpoint).
func AuthMiddleware(token string) func(http.Handler) http.Handler {
	expected := "Bearer " + token
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// Exempt paths that don't need auth
			if path == "/" || path == "/health" || path == "/login" ||
				strings.HasPrefix(path, "/static/") {
				next.ServeHTTP(w, r)
				return
			}

			// Only authenticate /api and /web routes
			needsAuth := strings.HasPrefix(path, "/api/") || path == "/api" ||
				strings.HasPrefix(path, "/web/") || path == "/web"
			if !needsAuth {
				next.ServeHTTP(w, r)
				return
			}

			// Check Authorization header (API clients)
			auth := r.Header.Get("Authorization")
			if subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) == 1 {
				next.ServeHTTP(w, r)
				return
			}

			// Check cookie (browser sessions)
			if cookie, err := r.Cookie("mammoth_token"); err == nil {
				if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) == 1 {
					next.ServeHTTP(w, r)
					return
				}
			}

			// For HTMX requests or API calls, return JSON 401
			if r.Header.Get("HX-Request") == "true" ||
				strings.HasPrefix(path, "/api/") ||
				!strings.Contains(r.Header.Get("Accept"), "text/html") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
				return
			}

			// For browser navigation, redirect to login
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		})
	}
}

// LoginHandler validates a token query parameter and sets a session cookie.
// GET /login?token=xxx validates and sets the cookie, then redirects to /.
// GET /login without a token shows a minimal login prompt.
func LoginHandler(expectedToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.URL.Query().Get("token")
		if token == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>mammoth-specd — Login</title></head><body style="font-family:sans-serif;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0"><div style="text-align:center"><h1>mammoth-specd</h1><p>Authentication required.</p><p style="color:#888">Append <code>?token=YOUR_TOKEN</code> to this URL.</p></div></body></html>`))
			return
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>mammoth-specd — Login</title></head><body style="font-family:sans-serif;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0"><div style="text-align:center"><h1>mammoth-specd</h1><p style="color:#ff6b6b">Invalid token.</p></div></body></html>`))
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "mammoth_token",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}
