package vfs

import (
	"os"
	"path/filepath"
	"strings"
)

// AccessResult describes the outcome of an access check.
type AccessResult int

const (
	AccessGranted AccessResult = iota
	AccessDenied
	AccessUnauthorized
	AccessNotFound
)

// AccessFor checks if a request can access this node.
// user and pwd are the credentials, empty user means anonymous.
func (n *Node) AccessFor(user, pwd string) AccessResult {
	// Check node-level accounts first
	if len(n.Accounts) > 0 {
		// If there are node-level accounts, anonymous is denied
		if user == "" {
			return AccessUnauthorized
		}
		found := false
		for _, a := range n.Accounts {
			if !a.Enabled {
				continue
			}
			if a.IsGroup {
				// Groups don't match directly - they represent permissions
				// Look for accounts that are members of this group
				found = true
			} else if a.Username == user {
				found = true
				break
			}
		}
		if !found {
			return AccessUnauthorized
		}
	}
	return AccessGranted
}

// CanBrowse returns true if this node's contents can be listed.
func (n *Node) CanBrowse() bool {
	if !n.IsFolder() {
		return false
	}
	return n.HasFlag(FlagBrowsable)
}

// CanUpload returns true if files can be uploaded to this node.
func (n *Node) CanUpload() bool {
	if !n.IsFolder() || n.IsVirtual() {
		return n.HasFlag(FlagUploadable)
	}
	return n.HasFlag(FlagUploadable) && n.RealPath != ""
}

// CanDelete returns true if this node can be deleted.
func (n *Node) CanDelete() bool {
	return n.HasFlag(FlagDeletable)
}

// CanArchive returns true if this node can be archived.
func (n *Node) CanArchive() bool {
	return n.HasFlag(FlagArchivable)
}

// IsDLForbidden returns true if downloads from this node are forbidden.
func (n *Node) IsDLForbidden() bool {
	// A node is DL-forbidden if it's not browsable and not archivable
	// This matches HFS semantics where DL means access to the file content
	return !n.HasFlag(FlagBrowsable) && !n.HasFlag(FlagArchivable)
}

// ShouldCountAsDownload returns whether serving this file counts as a download stat.
func (n *Node) ShouldCountAsDownload() bool {
	return n.IsFile() && !n.DontLog()
}

// MatchUploadFilter checks if a filename passes this node's upload filter.
func (n *Node) MatchUploadFilter(filename string) bool {
	if n.UploadFilter == "" {
		return true
	}
	// Simple glob match
	matched, err := filepath.Match(n.UploadFilter, filename)
	if err != nil {
		return false
	}
	return matched
}

// IsExtension checks if filename has a specific extension.
func IsExtension(filename, ext string) bool {
	return strings.EqualFold(filepath.Ext(filename), ext)
}

// ValidFilename checks for illegal characters in filenames.
func ValidFilename(name string) bool {
	if name == "" {
		return false
	}
	illegal := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, c := range illegal {
		if strings.Contains(name, c) {
			return false
		}
	}
	return true
}

// ScanRealFolder reads a real directory and creates child nodes.
func (n *Node) ScanRealFolder() error {
	if n.RealPath == "" {
		return nil
	}

	entries, err := os.ReadDir(n.RealPath)
	if err != nil {
		return err
	}

	// Build map of existing children for merging
	existing := make(map[string]*Node)
	for _, child := range n.Children {
		key := strings.ToLower(child.Name)
		existing[key] = child
	}

	// Track which existing children were found in the scan
	seen := make(map[string]bool)

	for _, entry := range entries {
		name := entry.Name()
		key := strings.ToLower(name)

		// Skip hidden files (optional, could be a flag)
		if strings.HasPrefix(name, ".") {
			continue
		}

		seen[key] = true

		// If child already exists in VFS, keep it (preserves metadata)
		if existingChild, ok := existing[key]; ok {
			// Update real path if changed
			existingChild.RealPath = filepath.Join(n.RealPath, name)
			continue
		}

		fullPath := filepath.Join(n.RealPath, name)
		if entry.IsDir() {
			child := NewFolder(name, fullPath)
			n.AddChild(child)
		} else {
			child := NewFile(name, fullPath)
			n.AddChild(child)
		}
	}

	n.IsScanned = true
	return nil
}
