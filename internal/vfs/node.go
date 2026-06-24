// Package vfs provides the Virtual File System for HFS.
// It manages a tree of nodes mapping URL paths to filesystem paths.
package vfs

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NodeFlags define permissions and properties of a VFS node.
type NodeFlags int

const (
	FlagBrowsable  NodeFlags = 1 << iota // folder listing visible
	FlagArchivable                       // can be included in TAR archives
	FlagDeletable                        // can be deleted via web
	FlagUploadable                       // can receive uploads
	FlagDontLog                          // exclude from access log
	FlagHidden                           // invisible in listings
	FlagNew                              // marked as new
	FlagNoDefault                        // don't auto-serve default file
)

// NodeType distinguishes between file types in the VFS.
type NodeType int

const (
	TypeFile   NodeType = iota
	TypeFolder
	TypeLink
)

// Node represents a single entry in the VFS tree.
type Node struct {
	Name         string      `json:"name" yaml:"name"`
	VirtualPath  string      `json:"vpath" yaml:"vpath"`                    // URL path, e.g. "/music"
	RealPath     string      `json:"rpath,omitempty" yaml:"rpath,omitempty"` // filesystem path (empty for virtual folders)
	NodeType     NodeType    `json:"type" yaml:"type"`
	Flags        NodeFlags   `json:"flags" yaml:"flags,omitempty"`
	Children     []*Node     `json:"children,omitempty" yaml:"children,omitempty"`
	Comment      string      `json:"comment,omitempty" yaml:"comment,omitempty"`
	UploadFilter string      `json:"upload_filter,omitempty" yaml:"upload_filter,omitempty"`
	DefaultFile  string      `json:"default_file,omitempty" yaml:"default_file,omitempty"`
	Realm        string      `json:"realm,omitempty" yaml:"realm,omitempty"`
	Accounts     []*Account  `json:"accounts,omitempty" yaml:"accounts,omitempty"`
	DLCount      int64       `json:"dl_count" yaml:"dl_count,omitempty"`

	// Runtime-only, not persisted
	parent    *Node      `json:"-" yaml:"-"`
	modTime   time.Time  `json:"-" yaml:"-"`
	size      int64      `json:"-" yaml:"-"`
	IsScanned bool       `json:"-" yaml:"-"`
}

// Account is a per-node user account (similar to HFS usersInVFS).
type Account struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password,omitempty" yaml:"password,omitempty"`
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	IsGroup  bool   `json:"is_group,omitempty" yaml:"is_group,omitempty"`
	Notes    string `json:"notes,omitempty" yaml:"notes,omitempty"`
	Redirect string `json:"redirect,omitempty" yaml:"redirect,omitempty"`
}

// NewFolder creates a new folder node.
func NewFolder(name, realPath string) *Node {
	return &Node{
		Name:     name,
		RealPath: realPath,
		NodeType: TypeFolder,
		Flags:    FlagBrowsable | FlagArchivable,
		Children: make([]*Node, 0),
	}
}

// NewVirtualFolder creates a virtual folder (no real filesystem path).
func NewVirtualFolder(name, vpath string) *Node {
	return &Node{
		Name:        name,
		VirtualPath: vpath,
		NodeType:    TypeFolder,
		Flags:       FlagBrowsable | FlagArchivable | FlagDeletable | FlagUploadable,
		Children:    make([]*Node, 0),
	}
}

// NewFile creates a new file node from a real file path.
func NewFile(name, realPath string) *Node {
	info, err := os.Stat(realPath)
	if err != nil {
		return &Node{
			Name:     name,
			RealPath: realPath,
			NodeType: TypeFile,
		}
	}
	return &Node{
		Name:     name,
		RealPath: realPath,
		NodeType: TypeFile,
		size:     info.Size(),
		modTime:  info.ModTime(),
	}
}

// IsFolder returns true if the node is a folder.
func (n *Node) IsFolder() bool {
	return n.NodeType == TypeFolder
}

// IsFile returns true if the node is a regular file.
func (n *Node) IsFile() bool {
	return n.NodeType == TypeFile
}

// IsLink returns true if the node is a link/shortcut.
func (n *Node) IsLink() bool {
	return n.NodeType == TypeLink
}

// IsRealFolder returns true if the folder has a real filesystem path.
func (n *Node) IsRealFolder() bool {
	return n.IsFolder() && n.RealPath != ""
}

// IsVirtual returns true if the node has no real filesystem path.
func (n *Node) IsVirtual() bool {
	return n.RealPath == ""
}

// HasFlag checks if a specific flag is set.
func (n *Node) HasFlag(f NodeFlags) bool {
	return n.Flags&f != 0
}

// SetFlag sets a flag on the node.
func (n *Node) SetFlag(f NodeFlags) {
	n.Flags |= f
}

// ClearFlag removes a flag from the node.
func (n *Node) ClearFlag(f NodeFlags) {
	n.Flags &^= f
}

// Size returns the file size (for files) or recalculated total (for folders).
func (n *Node) Size() int64 {
	if n.IsFile() {
		if n.size == 0 && n.RealPath != "" {
			if info, err := os.Stat(n.RealPath); err == nil {
				n.size = info.Size()
			}
		}
		return n.size
	}
	var total int64
	for _, child := range n.Children {
		total += child.Size()
	}
	return total
}

// ModTime returns the last modification time.
func (n *Node) ModTime() time.Time {
	if n.modTime.IsZero() && n.RealPath != "" {
		if info, err := os.Stat(n.RealPath); err == nil {
			n.modTime = info.ModTime()
		}
	}
	return n.modTime
}

// URL returns the URL path for this node.
func (n *Node) URL() string {
	if n.VirtualPath != "" {
		return n.VirtualPath
	}
	if n.parent == nil {
		return "/"
	}
	parentURL := n.parent.URL()
	if parentURL == "/" {
		return "/" + n.Name
	}
	return parentURL + "/" + n.Name
}

// PathTill returns the path from this node up to (but not including) ancestor.
func (n *Node) PathTill(ancestor *Node) string {
	if n == ancestor || n.parent == nil {
		return n.Name
	}
	return filepath.Join(n.parent.PathTill(ancestor), n.Name)
}

// GetParent returns the parent node.
func (n *Node) GetParent() *Node {
	return n.parent
}

// FindChild finds a direct child by name (case-insensitive on Windows, case-sensitive elsewhere).
func (n *Node) FindChild(name string) *Node {
	for _, child := range n.Children {
		if strings.EqualFold(child.Name, name) {
			return child
		}
	}
	return nil
}

// FindByURL resolves a URL path to a node. Returns nil if not found.
func (n *Node) FindByURL(url string) *Node {
	if url == "/" || url == "" {
		return n
	}
	// Clean the path
	url = strings.TrimPrefix(filepath.Clean(url), "/")
	if url == "." || url == "" {
		return n
	}
	parts := strings.Split(url, "/")
	current := n
	for _, part := range parts {
		if part == "" {
			continue
		}
		found := false
		for _, child := range current.Children {
			if strings.EqualFold(child.Name, part) {
				current = child
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return current
}

// GetDefaultFile returns the default file for a folder (e.g., index.html).
func (n *Node) GetDefaultFile() *Node {
	if !n.IsFolder() {
		return nil
	}
	if n.HasFlag(FlagNoDefault) {
		return nil
	}
	defaults := []string{"index.html", "index.htm", "default.html", "default.htm"}
	for _, name := range defaults {
		if child := n.FindChild(name); child != nil && child.IsFile() {
			return child
		}
	}
	return nil
}

// AddChild adds a child node to this folder.
func (n *Node) AddChild(child *Node) {
	child.parent = n
	n.Children = append(n.Children, child)
}

// RemoveChild removes a child by name.
func (n *Node) RemoveChild(name string) bool {
	for i, child := range n.Children {
		if strings.EqualFold(child.Name, name) {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			return true
		}
	}
	return false
}

// DontLog returns true if this node or any ancestor has the DontLog flag.
func (n *Node) DontLog() bool {
	if n.HasFlag(FlagDontLog) {
		return true
	}
	if n.parent != nil {
		return n.parent.DontLog()
	}
	return false
}

// HasRecursive checks recursively if any node up the tree has a flag set.
func (n *Node) HasRecursive(flag NodeFlags) bool {
	if n.HasFlag(flag) {
		return true
	}
	if n.parent != nil {
		return n.parent.HasRecursive(flag)
	}
	return false
}
