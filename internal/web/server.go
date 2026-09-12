// Package web implements the BBS admin REST API: JWT-authenticated
// sysop login and BBS configuration editing, plus serving the built
// SvelteKit admin UI.
package web

import (
	"net/http"
	"os"
	"path/filepath"

	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// Server holds the dependencies shared by all admin API handlers.
type Server struct {
	Users         *user.Store
	BBSConfigPath string
	JWTSecret     []byte
	StaticDir     string
}

// Routes builds the HTTP handler for the admin API and static UI.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.Handle("GET /api/config", s.requireAuth(http.HandlerFunc(s.handleGetConfig)))
	mux.Handle("PUT /api/config", s.requireAuth(http.HandlerFunc(s.handlePutConfig)))

	if s.StaticDir != "" {
		if _, err := os.Stat(s.StaticDir); err == nil {
			mux.Handle("/", spaFileServer(s.StaticDir))
		}
	}

	return withCORS(mux)
}

// spaFileServer serves files from dir, falling back to index.html for
// any path that doesn't match a real file. The SvelteKit build here
// runs as a client-side-only SPA (see web/vite.config.ts), so
// client-side routes like /login and /settings only exist as
// in-browser router state, not as files on disk.
func spaFileServer(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fullPath := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}

// withCORS allows the SvelteKit dev server (a different origin during
// development) to call the API. The admin API is protected by JWT
// bearer auth rather than cookies, so a permissive origin reflection
// carries no CSRF risk.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
