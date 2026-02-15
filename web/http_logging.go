// ABOUTME: HTTP logging middleware for the unified web server with consistent log.Printf style.
// ABOUTME: Replaces chi's default logger format to align request logs with agent/runtime logs.
package web

import (
	"log"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += n
	return n, err
}

func webRequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		log.Printf("web request method=%s path=%s status=%d bytes=%d duration=%s remote=%s",
			r.Method,
			r.URL.Path,
			status,
			rec.bytes,
			time.Since(start).Round(time.Microsecond),
			r.RemoteAddr,
		)
	})
}
