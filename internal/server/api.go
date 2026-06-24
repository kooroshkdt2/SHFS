package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"hfs-go/internal/auth"
	"hfs-go/internal/config"
	"hfs-go/internal/vfs"
)

// ---------------------------------------------------------------------------
// VFS Tree
// ---------------------------------------------------------------------------

func (s *Server) handleAPIVFSTree(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"root": s.vfs.Root})
}

// ---------------------------------------------------------------------------
// VFS Folders
// ---------------------------------------------------------------------------

func (s *Server) handleAPIFolders(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeJSON(w, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Name     string `json:"name"`
		RealPath string `json:"real_path"`
		Parent   string `json:"parent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.Parent == "" {
		req.Parent = "/"
	}

	var node *vfs.Node
	var err error

	if req.RealPath != "" {
		node, err = s.vfs.AddRealFolder(req.Name, req.RealPath, req.Parent)
	} else {
		node, err = s.vfs.AddVirtualFolder(req.Name, req.Parent)
	}
	if err != nil {
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "node": node})
}

// ---------------------------------------------------------------------------
// VFS Nodes (CRUD on individual nodes)
// ---------------------------------------------------------------------------

func (s *Server) handleAPINodes(w http.ResponseWriter, r *http.Request) {
	vpath := strings.TrimPrefix(r.URL.Path, "/api/vfs/nodes")
	if vpath == "" || vpath == "/" {
		writeJSON(w, map[string]string{"error": "no node path specified"})
		return
	}

	switch r.Method {
	case "DELETE":
		if err := s.vfs.RemoveNode(vpath); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]bool{"ok": true})

	case "PATCH":
		var req struct {
			Comment      string        `json:"comment"`
			Flags        vfs.NodeFlags `json:"flags"`
			UploadFilter string        `json:"upload_filter"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]string{"error": "invalid JSON"})
			return
		}
		node := s.vfs.FindByURL(vpath)
		if node == nil {
			writeJSON(w, map[string]string{"error": "node not found"})
			return
		}
		if req.Comment != "" {
			node.Comment = req.Comment
		}
		if req.Flags != 0 {
			node.Flags = req.Flags
		}
		if req.UploadFilter != "" {
			node.UploadFilter = req.UploadFilter
		}
		writeJSON(w, map[string]interface{}{"ok": true, "node": node})

	default:
		writeJSON(w, map[string]string{"error": "method not allowed"})
	}
}

// ---------------------------------------------------------------------------
// Server Stats
// ---------------------------------------------------------------------------

func (s *Server) handleAPIStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.GetStats())
}

// ---------------------------------------------------------------------------
// Connections
// ---------------------------------------------------------------------------

func (s *Server) handleAPIConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, s.GetConnections())
	case "DELETE":
		writeJSON(w, map[string]bool{"ok": true})
	default:
		writeJSON(w, map[string]string{"error": "method not allowed"})
	}
}

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

func (s *Server) handleAPIConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, s.cfg)
	case "PUT":
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			writeJSON(w, map[string]string{"error": "invalid JSON"})
			return
		}
		if port, ok := updates["port"].(float64); ok {
			s.cfg.Server.Port = int(port)
		}
		if err := s.cfg.Save(); err != nil {
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	default:
		writeJSON(w, map[string]string{"error": "method not allowed"})
	}
}

// ---------------------------------------------------------------------------
// Accounts
// ---------------------------------------------------------------------------

func (s *Server) handleAPIAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		writeJSON(w, s.cfg.Accounts)
	case "POST":
		var req struct {
			Username    string   `json:"username"`
			Password    string   `json:"password"`
			Permissions []string `json:"permissions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]string{"error": "invalid JSON"})
			return
		}
		hashed, err := auth.HashPassword(req.Password)
		if err != nil {
			writeJSON(w, map[string]string{"error": "hash failed"})
			return
		}
		acct := config.Account{
			Username:    req.Username,
			Password:    hashed,
			Permissions: req.Permissions,
			Enabled:     true,
		}
		s.cfg.AddAccount(acct)
		s.cfg.Save()
		writeJSON(w, map[string]bool{"ok": true})
	case "DELETE":
		username := strings.TrimPrefix(r.URL.Path, "/api/accounts/")
		if username == "" {
			writeJSON(w, map[string]string{"error": "no username"})
			return
		}
		s.cfg.RemoveAccount(username)
		s.cfg.Save()
		writeJSON(w, map[string]bool{"ok": true})
	default:
		writeJSON(w, map[string]string{"error": "method not allowed"})
	}
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	recursive := r.URL.Query().Get("recursive") == "1" || r.URL.Query().Get("recursive") == "true"
	start := r.URL.Query().Get("start")
	if start == "" {
		start = "/"
	}
	results := s.vfs.Search(q, recursive, start)
	if results == nil {
		results = []*vfs.Node{}
	}
	writeJSON(w, results)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("json encode error: %v", err)
	}
}
