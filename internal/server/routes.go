package server

import (
	"net/http"
)

// routes registers all HTTP routes and returns the handler.
func (s *Server) routes() http.Handler {
	s.mux = http.NewServeMux()

	// Browse and file serving — catch-all for /
	s.mux.HandleFunc("/", s.handleBrowse)

	// Static assets
	s.mux.HandleFunc("/~img", s.handleIcon)

	// Admin panel (headless mode) — SPA + static assets
	s.mux.HandleFunc("/admin/", s.serveAdminAsset)
	s.mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})

	// REST API
	s.mux.HandleFunc("/api/vfs/tree", s.handleAPIVFSTree)
	s.mux.HandleFunc("/api/vfs/folders", s.handleAPIFolders)
	s.mux.HandleFunc("/api/vfs/nodes/", s.handleAPINodes)
	s.mux.HandleFunc("/api/server/stats", s.handleAPIStats)
	s.mux.HandleFunc("/api/server/connections", s.handleAPIConnections)
	s.mux.HandleFunc("/api/progress", s.handleProgress)
	s.mux.HandleFunc("/api/config", s.handleAPIConfig)
	s.mux.HandleFunc("/api/accounts", s.handleAPIAccounts)
	s.mux.HandleFunc("/api/search", s.handleAPISearch)

	// Login
	s.mux.HandleFunc("/~login", s.handleLogin)

	// Favicon
	s.mux.HandleFunc("/favicon.ico", s.handleFavicon)

	// Wrap with middleware (CORS first so all responses get headers)
	var h http.Handler = s.mux
	h = s.loggingMiddleware(h)
	h = s.recoveryMiddleware(h)
	h = corsMiddleware(h)
	return h
}
