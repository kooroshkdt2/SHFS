package server

import (
	"archive/tar"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"hfs-go/internal/vfs"
)

// handleArchive creates and streams a TAR archive of a folder's contents.
func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request, node *vfs.Node) {
	if !node.IsFolder() {
		http.Error(w, "Not a folder", http.StatusBadRequest)
		return
	}

	if !node.CanArchive() {
		s.renderError(w, r, http.StatusForbidden, "Forbidden", "Archive not allowed.")
		return
	}

	archiveName := node.Name
	if archiveName == "/" || archiveName == "" {
		archiveName = "archive"
	}
	archiveName = strings.TrimSuffix(archiveName, "/") + ".tar"

	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, archiveName))

	tw := tar.NewWriter(w)
	defer tw.Close()

	s.addFolderToTar(tw, node, "")
}

func (s *Server) addFolderToTar(tw *tar.Writer, folder *vfs.Node, prefix string) {
	for _, child := range folder.Children {
		if child.HasFlag(vfs.FlagHidden) {
			continue
		}

		archivePath := filepath.Join(prefix, child.Name)

		if child.IsFolder() {
			hdr := &tar.Header{
				Name:     archivePath + "/",
				Mode:     0755,
				ModTime:  child.ModTime(),
				Typeflag: tar.TypeDir,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return
			}
			s.addFolderToTar(tw, child, archivePath)
			continue
		}

		if !child.IsFile() || child.RealPath == "" {
			continue
		}

		info, err := os.Stat(child.RealPath)
		if err != nil {
			continue
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			continue
		}
		hdr.Name = archivePath

		if err := tw.WriteHeader(hdr); err != nil {
			return
		}

		file, err := os.Open(child.RealPath)
		if err != nil {
			continue
		}

		io.Copy(tw, file)
		file.Close()
	}
}
