package vfs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Tree manages the complete Virtual File System.
type Tree struct {
	Root    *Node `yaml:"root" json:"root"`
	mu      sync.RWMutex

	// Path to persistent VFS file
	filePath string
}

// New creates an empty VFS tree.
func New() *Tree {
	root := NewVirtualFolder("/", "/")
	root.Flags = FlagBrowsable | FlagArchivable | FlagDeletable | FlagUploadable
	return &Tree{Root: root}
}

// NewFromPath creates a VFS tree from a real folder path.
func NewFromPath(realPath string) (*Tree, error) {
	t := New()
	if realPath == "" {
		return t, nil
	}

	info, err := os.Stat(realPath)
	if err != nil {
		return nil, fmt.Errorf("stat root path %q: %w", realPath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root path %q is not a directory", realPath)
	}

	t.Root.RealPath = realPath
	t.Root.Name = filepath.Base(realPath)
	if t.Root.Name == "" || t.Root.Name == "." {
		t.Root.Name = "/"
	}

	if err := t.Scan(); err != nil {
		return nil, fmt.Errorf("scan root: %w", err)
	}
	return t, nil
}

// Scan reads the real filesystem and populates the VFS tree.
// Existing nodes preserve their metadata (comments, permissions, etc.).
func (t *Tree) Scan() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Root.ScanRealFolder()
}

// FindByURL resolves a URL path to a VFS node.
func (t *Tree) FindByURL(url string) *Node {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Root.FindByURL(url)
}

// AddRealFolder adds a real folder path to the VFS at the given URL path.
func (t *Tree) AddRealFolder(name, realPath, parentVPath string) (*Node, error) {
	info, err := os.Stat(realPath)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", realPath, err)
	}

	parent := t.FindByURL(parentVPath)
	if parent == nil || !parent.IsFolder() {
		return nil, fmt.Errorf("parent %q not found or not a folder", parentVPath)
	}

	if name == "" {
		name = filepath.Base(realPath)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Check for duplicate
	if existing := parent.FindChild(name); existing != nil {
		return existing, nil
	}

	var node *Node
	if info.IsDir() {
		node = NewFolder(name, realPath)
		node.Flags = FlagBrowsable | FlagArchivable | FlagDeletable | FlagUploadable
	} else {
		node = NewFile(name, realPath)
	}

	node.VirtualPath = filepath.Join(parentVPath, name)
	parent.AddChild(node)

	// If it's a folder, scan its children
	if info.IsDir() {
		node.ScanRealFolder()
	}

	return node, nil
}

// AddVirtualFolder creates a new virtual folder in the VFS.
func (t *Tree) AddVirtualFolder(name, parentVPath string) (*Node, error) {
	parent := t.FindByURL(parentVPath)
	if parent == nil || !parent.IsFolder() {
		return nil, fmt.Errorf("parent %q not found or not a folder", parentVPath)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Check for duplicate
	if existing := parent.FindChild(name); existing != nil {
		return existing, nil
	}

	node := NewVirtualFolder(name, filepath.Join(parentVPath, name))
	parent.AddChild(node)
	return node, nil
}

// RemoveNode removes a node from the VFS by URL path.
// For real files/folders, this only removes from VFS, not from disk.
func (t *Tree) RemoveNode(vpath string) error {
	if vpath == "/" || vpath == "" {
		return fmt.Errorf("cannot remove root")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.Root.FindByURL(vpath)
	if node == nil {
		return fmt.Errorf("node %q not found", vpath)
	}
	if node.parent == nil {
		return fmt.Errorf("cannot remove root node")
	}

	node.parent.RemoveChild(node.Name)
	return nil
}

// MoveNode moves a node to a new parent.
func (t *Tree) MoveNode(srcVPath, dstParentVPath string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.Root.FindByURL(srcVPath)
	if node == nil {
		return fmt.Errorf("source %q not found", srcVPath)
	}
	if node.parent == nil {
		return fmt.Errorf("cannot move root")
	}

	dstParent := t.Root.FindByURL(dstParentVPath)
	if dstParent == nil || !dstParent.IsFolder() {
		return fmt.Errorf("destination %q not found or not a folder", dstParentVPath)
	}

	// Remove from old parent
	node.parent.RemoveChild(node.Name)
	// Add to new parent
	dstParent.AddChild(node)

	// If the node has a real path and is moving to a folder with a real path, move on disk
	if node.RealPath != "" && dstParent.RealPath != "" {
		newRealPath := filepath.Join(dstParent.RealPath, node.Name)
		if err := os.Rename(node.RealPath, newRealPath); err == nil {
			node.RealPath = newRealPath
		}
	}

	return nil
}

// RenameNode renames a VFS node.
func (t *Tree) RenameNode(vpath, newName string) error {
	if newName == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if !ValidFilename(newName) {
		return fmt.Errorf("invalid filename: %q", newName)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.Root.FindByURL(vpath)
	if node == nil {
		return fmt.Errorf("node %q not found", vpath)
	}
	if node.parent == nil {
		return fmt.Errorf("cannot rename root")
	}

	// Check for duplicate in same parent
	if sibling := node.parent.FindChild(newName); sibling != nil && sibling != node {
		return fmt.Errorf("%q already exists", newName)
	}

	oldName := node.Name
	node.Name = newName

	// Rename on disk if real path
	if node.RealPath != "" {
		newRealPath := filepath.Join(filepath.Dir(node.RealPath), newName)
		if err := os.Rename(node.RealPath, newRealPath); err != nil {
			node.Name = oldName // rollback
			return fmt.Errorf("rename on disk: %w", err)
		}
		node.RealPath = newRealPath
	}

	return nil
}

// Mkdir creates a real subdirectory under the given VFS node.
func (t *Tree) Mkdir(vpath string) error {
	node := t.FindByURL(vpath)
	if node == nil {
		return fmt.Errorf("node %q not found", vpath)
	}
	if !node.IsFolder() {
		return fmt.Errorf("%q is not a folder", vpath)
	}
	if !node.HasFlag(FlagUploadable) {
		return fmt.Errorf("cannot create in %q: upload not allowed", vpath)
	}

	targetDir := node.RealPath
	if targetDir == "" {
		return fmt.Errorf("node %q has no real path", vpath)
	}

	return os.MkdirAll(targetDir, 0755)
}

// Search finds nodes matching a query, optionally recursively.
func (t *Tree) Search(query string, recursive bool, startVPath string) []*Node {
	start := t.FindByURL(startVPath)
	if start == nil {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	var results []*Node
	query = strings.ToLower(query)

	// Convert query to glob: *term* if no wildcards
	if !strings.ContainsAny(query, "*?") {
		query = "*" + query + "*"
	}

	var search func(n *Node, recurse bool)
	search = func(n *Node, recurse bool) {
		for _, child := range n.Children {
			if matched, err := filepath.Match(query, strings.ToLower(child.Name)); err == nil && matched {
				results = append(results, child)
			}
			if recurse && child.IsFolder() {
				search(child, true)
			}
		}
	}

	search(start, recursive)
	return results
}

// Save persists the VFS tree to a YAML file.
func (t *Tree) Save(filePath string) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	t.filePath = filePath

	data, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal vfs: %w", err)
	}
	return os.WriteFile(filePath, data, 0644)
}

// LoadTree loads a VFS tree from a YAML file.
func LoadTree(filePath string) (*Tree, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("read vfs file: %w", err)
	}

	t := &Tree{}
	if err := yaml.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("parse vfs file: %w", err)
	}

	// Ensure root exists
	if t.Root == nil {
		t.Root = NewVirtualFolder("/", "/")
	}

	// Rebuild parent references
	t.rebuildParents(t.Root, nil)

	// Re-scan real folders to pick up new/deleted files
	if err := t.Scan(); err != nil {
		// Non-fatal: scan failed but we have the persisted tree
		_ = err
	}

	return t, nil
}

// rebuildParents walks the tree and sets parent pointers.
func (t *Tree) rebuildParents(node *Node, parent *Node) {
	node.parent = parent
	for _, child := range node.Children {
		t.rebuildParents(child, node)
	}
}

// GetFilePath returns the path where the tree was last saved/loaded.
func (t *Tree) GetFilePath() string {
	return t.filePath
}

// Walk traverses the tree depth-first, calling fn for each node.
func (t *Tree) Walk(fn func(n *Node, depth int) error) error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return walk(t.Root, 0, fn)
}

func walk(n *Node, depth int, fn func(*Node, int) error) error {
	if err := fn(n, depth); err != nil {
		return err
	}
	for _, child := range n.Children {
		if err := walk(child, depth+1, fn); err != nil {
			return err
		}
	}
	return nil
}

// CountFolders returns the number of folder nodes under this node.
func (n *Node) CountFolders() int {
	count := 0
	for _, child := range n.Children {
		if child.IsFolder() {
			count++
		}
	}
	return count
}

// CountFiles returns the number of file nodes under this node.
func (n *Node) CountFiles() int {
	count := 0
	for _, child := range n.Children {
		if child.IsFile() {
			count++
		}
	}
	return count
}

// TotalSize returns the total size of all files under this node.
func (n *Node) TotalSize() int64 {
	var total int64
	for _, child := range n.Children {
		if child.IsFile() {
			total += child.Size()
		}
	}
	return total
}

// CountItems returns the total number of items (files + folders) under this node.
func (n *Node) CountItems() int {
	return len(n.Children)
}
