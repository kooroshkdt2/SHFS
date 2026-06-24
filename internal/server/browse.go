package server

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hfs-go/internal/auth"
	"hfs-go/internal/vfs"
)

// BrowseData is passed to browse templates.
type BrowseData struct {
	Folder        *vfs.Node
	Items         []ItemData
	Breadcrumbs   []Breadcrumb
	ServerVersion string
	ServerTime    string
	Uptime        string
	TotalSize     string
	NumFolders    int
	NumFiles      int
	Search        string
	SortBy        string
	SortRev       bool
	User          string
	CanUpload     bool
	CanDelete     bool
	CanArchive    bool
	URL           string
	ParentURL     string
}

// ItemData represents a file or folder in a listing.
type ItemData struct {
	Name     string
	URL      string
	Icon     string
	Size     string
	SizeRaw  int64
	Modified string
	Hits     int64
	IsFolder bool
	IsNew    bool
	Comment  string
	CanAccess bool
}

// Breadcrumb represents a path segment in navigation.
type Breadcrumb struct {
	Name string
	URL  string
	Last bool
}

// handleBrowse handles the main URL space: folder listings and file downloads.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	// Decode URL-encoded path (spaces, special chars)
	urlPath := r.URL.Path
	decodedPath, err := url.PathUnescape(urlPath)
	if err != nil {
		decodedPath = urlPath // fallback to raw path
	}

	// Check for static assets for the browse UI
	if strings.HasPrefix(decodedPath, "/~") {
		http.NotFound(w, r)
		return
	}

	// Find the VFS node using decoded path
	node := s.vfs.FindByURL(decodedPath)
	s.IncHits()

	// If not found
	if node == nil {
		s.renderError(w, r, http.StatusNotFound, "Not found", "The requested URL was not found on this server.")
		return
	}

	// For folders without trailing slash, redirect
	if node.IsFolder() && !strings.HasSuffix(decodedPath, "/") && decodedPath != "" {
		http.Redirect(w, r, decodedPath+"/", http.StatusMovedPermanently)
		return
	}

	// Check ban
	if banned, reason := s.IsBanned(r.RemoteAddr); banned {
		s.renderError(w, r, http.StatusForbidden, "Banned", reason)
		return
	}

	// If it's a file, serve it
	if node.IsFile() {
		s.serveFile(w, r, node)
		return
	}

	// It's a folder — handle POST (upload) and archive requests
	if node.IsFolder() {
		if r.Method == "POST" {
			s.handleUpload(w, r)
			return
		}
		if r.URL.Query().Get("mode") == "archive" {
			s.handleArchive(w, r, node)
			return
		}
		// Check if we should serve a default file
		if !node.HasFlag(vfs.FlagNoDefault) {
			if defaultFile := node.GetDefaultFile(); defaultFile != nil {
				s.serveFile(w, r, defaultFile)
				return
			}
		}
	}

	// Check browse permission
	if !node.CanBrowse() {
		s.renderError(w, r, http.StatusForbidden, "Forbidden", "This resource is not accessible.")
		return
	}

	// Serve folder listing
	s.serveFolderListing(w, r, node)
}

// serveFile handles file downloads with range support.
func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, node *vfs.Node) {
	if node.RealPath == "" {
		s.renderError(w, r, http.StatusNotFound, "Not found", "File has no real path.")
		return
	}

	// Check if file exists
	info, err := os.Stat(node.RealPath)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "Not found", "File not found on disk.")
		return
	}

	// Detect MIME type
	ext := filepath.Ext(node.Name)
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Content-Disposition header
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"`, node.Name))
	w.Header().Set("Accept-Ranges", "bytes")

	// Track this as a download
	s.IncDownloads()
	clientIP := getClientIP(r)
	s.logEvent("Download: %s %s — %s", clientIP, node.URL(), formatSize(node.Size()))

	// Serve the file (Go's http.ServeContent handles Range, If-Modified-Since, etc.)
	file, err := os.Open(node.RealPath)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Error", "Cannot open file.")
		return
	}
	defer file.Close()

	http.ServeContent(w, r, node.Name, info.ModTime(), file)
}

// serveFolderListing renders a directory listing.
func (s *Server) serveFolderListing(w http.ResponseWriter, r *http.Request, node *vfs.Node) {
	// Scan real folder if needed
	if node.IsRealFolder() && !node.IsScanned {
		node.ScanRealFolder()
	}

	// Build breadcrumbs
	breadcrumbs := s.buildBreadcrumbs(node)

	// Build item list
	items := make([]ItemData, 0, len(node.Children))
	for _, child := range node.Children {
		if child.HasFlag(vfs.FlagHidden) {
			continue
		}

		icon := "/~img_file"
		if child.IsFolder() {
			icon = "/~img_folder"
		} else if strings.HasSuffix(strings.ToLower(child.Name), ".zip") ||
			strings.HasSuffix(strings.ToLower(child.Name), ".tar") ||
			strings.HasSuffix(strings.ToLower(child.Name), ".gz") {
			icon = "/~img_archive"
		}

		size := formatSize(child.Size())
		if child.IsFolder() {
			size = "folder"
		}

		item := ItemData{
			Name:      child.Name,
			URL:       child.URL(),
			Icon:      icon,
			Size:      size,
			SizeRaw:   child.Size(),
			Modified:  child.ModTime().Format("2006-01-02 15:04:05"),
			Hits:      child.DLCount,
			IsFolder:  child.IsFolder(),
			IsNew:     child.HasFlag(vfs.FlagNew),
			Comment:   child.Comment,
			CanAccess: true,
		}
		items = append(items, item)
	}

	// Sort items (folders first, then by name)
	// TODO: respect sortBy and sortRev query params

	// Check user
	user := s.getUser(r)

	data := BrowseData{
		Folder:        node,
		Items:         items,
		Breadcrumbs:   breadcrumbs,
		ServerVersion: "HFS Go 0.1.0",
		ServerTime:    time.Now().Format("2006-01-02 15:04:05"),
		Uptime:        time.Since(s.startTime).Truncate(time.Second).String(),
		TotalSize:     formatSize(node.TotalSize()),
		NumFolders:    node.CountFolders(),
		NumFiles:      node.CountFiles(),
		Search:        r.URL.Query().Get("search"),
		User:          user,
		CanUpload:     node.CanUpload(),
		CanDelete:     node.CanDelete(),
		CanArchive:    node.CanArchive(),
		URL:           node.URL(),
	}

	if node.GetParent() != nil {
		data.ParentURL = node.GetParent().URL()
	}

	// Try to load custom template first, fall back to defaults
	tmpl := template.New("browse.html")
	tmpl.Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	})

	customPath := filepath.Join(s.cfg.GetConfigDir(), "templates", "browse.html")
	defaultPath := filepath.Join("web", "templates", "browse.html")
	if _, err := os.Stat(customPath); err == nil {
		tmpl = template.Must(tmpl.ParseFiles(customPath))
	} else if _, err := os.Stat(defaultPath); err == nil {
		tmpl = template.Must(tmpl.ParseFiles(defaultPath))
	} else {
		// Use in-memory fallback
		tmpl = template.Must(tmpl.Parse(browseTemplateFallback))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

// serveFile is handled above, but we also need the raw file serving without listing.
func (s *Server) serveRawFile(w http.ResponseWriter, r *http.Request, realPath string) {
	info, err := os.Stat(realPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(realPath)
	if err != nil {
		http.Error(w, "Cannot open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	http.ServeContent(w, r, filepath.Base(realPath), info.ModTime(), file)
}

func (s *Server) buildBreadcrumbs(node *vfs.Node) []Breadcrumb {
	// Build path from root to current node
	var parts []*vfs.Node
	current := node
	for current != nil {
		parts = append([]*vfs.Node{current}, parts...)
		current = current.GetParent()
	}

	breadcrumbs := make([]Breadcrumb, len(parts))
	for i, n := range parts {
		breadcrumbs[i] = Breadcrumb{
			Name: n.Name,
			URL:  n.URL(),
			Last: i == len(parts)-1,
		}
	}
	return breadcrumbs
}

func (s *Server) getUser(r *http.Request) string {
	user, _, _ := r.BasicAuth()
	if user == "" {
		user, _ = s.sessions.GetUser(r)
	}
	return user
}

// formatSize returns a human-readable byte size.
func formatSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// loadTemplate tries loading a template from several locations, falling back to inline.
func loadTemplate(name, inlineDefault string) *template.Template {
	tmpl := template.New(name)
	tmpl.Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	})

	// Try relative paths
	for _, path := range []string{
		filepath.Join("web", "templates", name),
		filepath.Join("templates", name),
	} {
		if _, err := os.Stat(path); err == nil {
			return template.Must(tmpl.ParseFiles(path))
		}
	}

	// Use inline fallback
	return template.Must(tmpl.Parse(inlineDefault))
}

// renderError renders an error page.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, code int, title, message string) {
	w.WriteHeader(code)

	tmpl := loadTemplate("error.html", errorTemplateFallback)

	data := struct {
		Title   string
		Message string
		Code    int
		Version string
		Time    string
	}{
		Title:   title,
		Message: message,
		Code:    code,
		Version: "HFS Go 0.1.0",
		Time:    time.Now().Format("2006-01-02 15:04:05"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("error template: %v", err)
		http.Error(w, title, code)
	}
}

const browseTemplateFallback = `<!DOCTYPE html><html><head><title>HFS {{.Folder.Name}}</title>
<meta charset="utf-8"><style>body{font-family:sans-serif;margin:2em;background:#f0f2f5}
a{color:#47c;text-decoration:none}table{width:100%;border-collapse:collapse;background:#fff}
th{background:#47c;color:#fff;padding:.5em}td{padding:.5em;border-top:1px solid #eee}
#panel{float:left;width:200px;margin-right:2em}fieldset{border:1px solid #ddd;border-radius:6px;padding:.8em;margin-bottom:1em}
</style></head><body>
<div id="panel"><fieldset><legend>Folder</legend>
{{range .Breadcrumbs}}{{if .Last}}<span>{{.Name}}</span>{{else}}<a href="{{.URL}}">{{.Name}}</a> /{{end}}{{end}}
<br>{{.NumFolders}} folders, {{.NumFiles}} files, {{.TotalSize}}</fieldset>
{{if .CanUpload}}<fieldset><legend>Upload</legend><form method="post" enctype="multipart/form-data">
<input type="file" name="file" multiple><button type="submit">Upload</button></form></fieldset>{{end}}
<fieldset><legend>Info</legend>HFS Go v0.1.0<br>Uptime: {{.Uptime}}</fieldset></div>
<div id="main">
{{if .Items}}<table><tr><th>Name<th>Size<th>Modified<th>Hits</tr>
{{range .Items}}<tr><td><a href="{{.URL}}">{{if .IsFolder}}&#128193;{{else}}&#128196;{{end}} {{.Name}}{{if .IsFolder}}/{{end}}</a>
<td>{{.Size}}<td>{{.Modified}}<td>{{.Hits}}</tr>
{{end}}</table>{{else}}<p>No files in this folder</p>{{end}}</div>
</body></html>`

const errorTemplateFallback = `<!DOCTYPE html><html><head><title>{{.Title}} - HFS</title>
<meta charset="utf-8"><style>body{font-family:sans-serif;text-align:center;padding:3em;background:#f0f2f5}
h1{color:#e74c3c}p{color:#666}a{color:#47c}</style></head><body>
<h1>{{.Code}}: {{.Title}}</h1><p>{{.Message}}</p><p><a href="/">Go to root</a></p>
<div style="font-size:11px;color:#999;margin-top:2em">{{.Version}} &bull; {{.Time}}</div>
</body></html>`

const adminFallbackHTML = `<!DOCTYPE html><html><head><title>HFS Admin</title>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{font-family:system-ui,sans-serif;margin:2em;background:#f0f2f5;color:#333}
h1{color:#47c}.card{background:#fff;padding:1.5em;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.08);margin:1em 0}
table{width:100%;border-collapse:collapse}th{background:#47c;color:#fff;padding:.5em}td{padding:.5em;border-top:1px solid #eee}
button{padding:.4em 1em;background:#47c;color:#fff;border:0;border-radius:4px;cursor:pointer}
input{padding:.5em;border:1px solid #ddd;border-radius:4px;width:100%}
.stats{display:flex;gap:1em;flex-wrap:wrap}.stat{background:#fff;padding:1em;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.08);min-width:150px}
.stat .label{font-size:11px;color:#888}.stat .value{font-size:1.5em;font-weight:700;color:#47c}
</style></head><body>
<h1>HFS Admin</h1>
<p>Admin panel files not found in frontend/ directory.</p>
<p>Place <code>frontend/index.html</code>, <code>app.js</code>, and <code>app.css</code> in the working directory for the full admin experience.</p>
<div class="card"><h3>Quick Actions</h3>
<p><a href="/">Browse Files</a></p>
<p><button onclick="fetch('/api/server/stats').then(r=>r.json()).then(d=>alert(JSON.stringify(d,null,2)))">Server Stats</button></p>
</div></body></html>`

const loginTemplateFallback = `<!DOCTYPE html><html><head><title>Login - HFS</title>
<meta charset="utf-8"><style>body{font-family:sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;background:#f0f2f5}
.box{background:#fff;padding:3em;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.1);text-align:center}
h1{color:#47c}input{display:block;width:100%;padding:.6em;margin:.5em 0;border:1px solid #ddd;border-radius:4px}
button{width:100%;padding:.6em;background:#47c;color:#fff;border:0;border-radius:4px;cursor:pointer;font-size:14px}
</style></head><body><div class="box"><h1>{{.Realm}}</h1>
<form method="post"><input name="username" placeholder="Username" autofocus required>
<input type="password" name="password" placeholder="Password" required>
<button type="submit">Login</button></form></div></body></html>`

// handleIcon serves icon images.
func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	// Return a simple SVG icon placeholder
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	icon := `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
		<rect width="16" height="16" fill="#47c" rx="2"/>
	</svg>`
	io.WriteString(w, icon)
}

// handleFavicon serves the favicon.
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	icon := `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
		<rect width="16" height="16" fill="#47c" rx="2"/>
		<text x="8" y="13" font-size="12" fill="white" text-anchor="middle" font-family="sans-serif">H</text>
	</svg>`
	io.WriteString(w, icon)
}

// handleLogin handles the login page and form submission.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		user := r.FormValue("username")
		pass := r.FormValue("password")

		if s.checkCredentials(user, pass) {
			s.sessions.CreateSession(w, user)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	// Check if already logged in
	if user, ok := s.sessions.GetUser(r); ok {
		log.Printf("User %s is already logged in", user)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	tmpl := loadTemplate("login.html", loginTemplateFallback)
	// Also check user config directory
	customPath := filepath.Join(s.cfg.GetConfigDir(), "templates", "login.html")
	if _, err := os.Stat(customPath); err == nil {
		tmpl = template.Must(tmpl.ParseFiles(customPath))
	}

	data := struct {
		Realm   string
		Version string
		Error   string
	}{
		Realm:   s.cfg.Auth.Realm,
		Version: "HFS Go 0.1.0",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		log.Printf("login template error: %v", err)
	}
}

func (s *Server) checkCredentials(user, pass string) bool {
	acct := s.cfg.FindAccount(user)
	if acct == nil || !acct.Enabled {
		return false
	}
	return auth.CheckPassword(acct.Password, pass)
}

// serveAdminAsset serves admin panel files: /admin/ -> index.html, /admin/app.js, /admin/app.css
func (s *Server) serveAdminAsset(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path

	// Determine which file to serve
	assetPath := "index.html"
	switch {
	case strings.HasSuffix(urlPath, "app.js"):
		assetPath = "app.js"
	case strings.HasSuffix(urlPath, "app.css"):
		assetPath = "app.css"
	default:
		assetPath = "index.html"
	}

	filePath := filepath.Join("frontend", assetPath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Fallback
		if assetPath == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(adminFallbackHTML))
			return
		}
		http.NotFound(w, r)
		return
	}

	switch {
	case strings.HasSuffix(assetPath, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	case strings.HasSuffix(assetPath, ".css"):
		w.Header().Set("Content-Type", "text/css")
	default:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.Write(data)
}

// getClientIP extracts the real client IP from a request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
