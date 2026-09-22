package api

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"runtime"
	"strings"
	"time"

	"trackstar/internal/apperr"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.DB.Health(ctx); err != nil {
		s.Logger.Error("health check failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	if !currentUser(r.Context()).IsAdmin {
		s.fail(w, r, apperr.Forbidden("administrators only"))
		return
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	largest, err := s.Stories.LargestSection(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":               s.Version,
		"go_version":            runtime.Version(),
		"database_driver":       s.DB.Driver(),
		"database_size_bytes":   s.DB.SizeBytes(),
		"uptime_seconds":        int64(time.Since(s.started).Seconds()),
		"memory_sys_bytes":      mem.Sys,
		"goroutines":            runtime.NumGoroutine(),
		"position_rebalances":   s.Stories.Rebalances.Load(),
		"largest_section":       largest,
		"event_streams":         s.Events.Subscribers(0),
		"event_streams_dropped": s.Events.Dropped.Load(),
	})
}

// frontendHandler serves the embedded single-page app: real files as they
// are, everything else falls back to index.html.
func (s *Server) frontendHandler() http.Handler {
	files := http.FileServerFS(s.Frontend)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := fs.Stat(s.Frontend, name); name != "" && err == nil && !info.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// Vite fingerprints everything under assets/.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(s.Frontend, "index.html")
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Trackstar API is running, but this binary was built without the frontend.\nRun `make build` (or `make dev` for development).\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
