package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"hfs-go/internal/vfs"
)

// UploadResult holds the result of a single uploaded file.
type UploadResult struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Speed  int64  `json:"speed"`
	Error  string `json:"error,omitempty"`
	URL    string `json:"url,omitempty"`
}

// handleUpload processes multipart file uploads to a VFS folder.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.serveFolderListing(w, r, s.vfs.FindByURL(r.URL.Path))
		return
	}

	urlPath := r.URL.Path
	node := s.vfs.FindByURL(urlPath)
	if node == nil {
		http.Error(w, "Folder not found", http.StatusNotFound)
		return
	}

	if !node.IsFolder() {
		http.Error(w, "Cannot upload to a file", http.StatusBadRequest)
		return
	}

	if !node.CanUpload() {
		http.Error(w, "Upload not allowed", http.StatusForbidden)
		return
	}

	if !node.IsRealFolder() {
		http.Error(w, "Cannot upload to virtual folder", http.StatusBadRequest)
		return
	}

	// Parse multipart form (max 1GB)
	if err := r.ParseMultipartForm(1 << 30); err != nil {
		http.Error(w, fmt.Sprintf("Parse form: %v", err), http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()

	results := make([]UploadResult, 0)

	// Process each uploaded file
	for _, files := range r.MultipartForm.File {
		for _, fileHeader := range files {
			result := UploadResult{
				Name: fileHeader.Filename,
			}

			// Validate filename
			if !vfs.ValidFilename(fileHeader.Filename) {
				result.Error = "Invalid filename"
				results = append(results, result)
				continue
			}

			// Check upload filter
			if !node.MatchUploadFilter(fileHeader.Filename) {
				result.Error = "File type not allowed"
				results = append(results, result)
				continue
			}

			destPath := filepath.Join(node.RealPath, fileHeader.Filename)

			// Open source file
			src, err := fileHeader.Open()
			if err != nil {
				result.Error = fmt.Sprintf("Cannot read upload: %v", err)
				results = append(results, result)
				continue
			}

			// Create destination file
			dst, err := os.Create(destPath)
			if err != nil {
				src.Close()
				result.Error = fmt.Sprintf("Cannot create file: %v", err)
				results = append(results, result)
				continue
			}

			// Copy with progress tracking
			written, err := io.Copy(dst, src)
			src.Close()
			dst.Close()

			if err != nil {
				result.Error = fmt.Sprintf("Write error: %v", err)
				os.Remove(destPath) // clean up failed upload
				results = append(results, result)
				continue
			}

			result.Size = written
			result.URL = filepath.Join(urlPath, fileHeader.Filename)

			// Add to VFS tree
			newNode := vfs.NewFile(fileHeader.Filename, destPath)
			node.AddChild(newNode)

			s.IncUploads()
			if s.cfg.Log.LogUploads {
			clientIP := getClientIP(r)
			s.logEvent("Upload: %s %s — %s", clientIP, fileHeader.Filename, formatSize(written))
				log.Printf("Uploaded %s (%s) to %s", fileHeader.Filename, formatSize(written), urlPath)
			}

			results = append(results, result)
		}
	}

	// Render upload results page
	s.renderUploadResults(w, r, results, node)
}

func (s *Server) renderUploadResults(w http.ResponseWriter, r *http.Request, results []UploadResult, folder *vfs.Node) {
	type pageData struct {
		Results []UploadResult
		Folder  *vfs.Node
		OK      int
		Failed  int
	}

	data := pageData{
		Results: results,
		Folder:  folder,
	}

	for _, r := range results {
		if r.Error == "" {
			data.OK++
		} else {
			data.Failed++
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Use a simple inline response for now
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>Upload Results</title>
<meta charset="utf-8"><style>body{font-family:sans-serif;margin:2em}
li{list-style-image:url(%%IMG%%)}
a.back{display:block;margin-top:1em}</style></head><body>
<h1>Upload Results</h1>
<p>%d files uploaded, %d failed</p>
<a href="%s" class="back">Back</a><ul>`, data.OK, data.Failed, folder.URL())

	for _, r := range results {
		if r.Error != "" {
			fmt.Fprintf(w, `<li><strong>%s</strong>: %s</li>`, r.Name, r.Error)
		} else {
			fmt.Fprintf(w, `<li><a href="%s">%s</a> (%s)</li>`, r.URL, r.Name, formatSize(r.Size))
		}
	}

	fmt.Fprintf(w, `</ul><a href="%s" class="back">Back</a></body></html>`, folder.URL())
}
