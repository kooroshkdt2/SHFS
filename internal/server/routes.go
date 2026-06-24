package server

import (
	"net/http"
)

// routes registers all HTTP routes and returns the handler.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Browse and file serving — catch-all for /
	mux.HandleFunc("/", s.handleBrowse)

	// Static assets
	mux.HandleFunc("/~img", s.handleIcon)

	// Admin panel (headless mode) — SPA + static assets
	mux.HandleFunc("/admin/", s.serveAdminAsset)
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})

	// REST API
	mux.HandleFunc("/api/vfs/tree", s.handleAPIVFSTree)
	mux.HandleFunc("/api/vfs/folders", s.handleAPIFolders)
	mux.HandleFunc("/api/vfs/nodes/", s.handleAPINodes)
	mux.HandleFunc("/api/server/stats", s.handleAPIStats)
	mux.HandleFunc("/api/server/connections", s.handleAPIConnections)
	mux.HandleFunc("/api/progress", s.handleProgress)
	mux.HandleFunc("/api/config", s.handleAPIConfig)
	mux.HandleFunc("/api/accounts", s.handleAPIAccounts)
	mux.HandleFunc("/api/search", s.handleAPISearch)

	// Login
	mux.HandleFunc("/~login", s.handleLogin)

	// Favicon
	mux.HandleFunc("/favicon.ico", s.handleFavicon)

	// Wrap with middleware (CORS first so all responses get headers)
	var h http.Handler = mux
	h = s.loggingMiddleware(h)
	h = s.recoveryMiddleware(h)
	h = corsMiddleware(h)
	return h
}
