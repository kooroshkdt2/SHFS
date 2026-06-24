package vfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewTree(t *testing.T) {
	tree := New()
	if tree.Root == nil {
		t.Fatal("root should not be nil")
	}
	if tree.Root.Name != "/" {
		t.Errorf("root name = %q, want /", tree.Root.Name)
	}
}

func TestTreeFromPath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)
	os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("nested"), 0644)

	tree, err := NewFromPath(dir)
	if err != nil {
		t.Fatalf("NewFromPath: %v", err)
	}

	// Root-level children (only scanned one level by default)
	if len(tree.Root.Children) < 1 {
		t.Errorf("expected at least 1 child, got %d", len(tree.Root.Children))
	}

	// Find by URL (root level)
	node := tree.FindByURL("/test.txt")
	if node == nil {
		t.Fatal("test.txt not found")
	}
	if !node.IsFile() {
		t.Error("test.txt should be a file")
	}

	// Subfolder should exist
	subNode := tree.FindByURL("/sub")
	if subNode == nil {
		t.Fatal("sub/ not found")
	}
	if !subNode.IsFolder() {
		t.Error("sub/ should be a folder")
	}
}

func TestAddVirtualFolder(t *testing.T) {
	tree := New()
	node, err := tree.AddVirtualFolder("music", "/")
	if err != nil {
		t.Fatalf("AddVirtualFolder: %v", err)
	}
	if node.Name != "music" {
		t.Errorf("name = %q, want music", node.Name)
	}
	if node.IsVirtual() != true {
		t.Error("virtual folder should report IsVirtual() = true")
	}
}

func TestRemoveNode(t *testing.T) {
	tree := New()
	tree.AddVirtualFolder("temp", "/")
	tree.AddVirtualFolder("keep", "/")

	if err := tree.RemoveNode("/temp"); err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}

	if node := tree.FindByURL("/temp"); node != nil {
		t.Error("/temp should be removed")
	}
	if node := tree.FindByURL("/keep"); node == nil {
		t.Error("/keep should still exist")
	}
}

func TestSearch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0644)
	os.WriteFile(filepath.Join(dir, "world.txt"), []byte("hi"), 0644)
	os.WriteFile(filepath.Join(dir, "other.bin"), []byte("hi"), 0644)

	tree, _ := NewFromPath(dir)

	results := tree.Search("*.txt", false, "/")
	if len(results) != 2 {
		t.Errorf("search for *.txt: expected 2, got %d", len(results))
	}

	results = tree.Search("hello", false, "/")
	if len(results) != 1 {
		t.Errorf("search for hello: expected 1, got %d", len(results))
	}
}

func TestNodeFlags(t *testing.T) {
	// NewFolder sets FlagBrowsable | FlagArchivable by default
	n := NewFolder("test", "/tmp")
	if !n.HasFlag(FlagBrowsable) {
		t.Error("new folder should be browsable by default")
	}
	if !n.HasFlag(FlagArchivable) {
		t.Error("new folder should be archivable by default")
	}

	n.ClearFlag(FlagBrowsable)
	if n.HasFlag(FlagBrowsable) {
		t.Error("should not be browsable after clearing flag")
	}

	n.SetFlag(FlagHidden)
	if !n.HasFlag(FlagHidden) {
		t.Error("should be hidden after setting flag")
	}
}

func TestDefaultFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>"), 0644)
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("data"), 0644)

	tree, _ := NewFromPath(dir)

	def := tree.Root.GetDefaultFile()
	if def == nil {
		t.Fatal("should find index.html as default file")
	}
	if def.Name != "index.html" {
		t.Errorf("default = %q, want index.html", def.Name)
	}
}

func TestCountMethods(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bb"), 0644)
	os.MkdirAll(filepath.Join(dir, "folder"), 0755)

	tree, _ := NewFromPath(dir)

	if n := tree.Root.CountFiles(); n != 2 {
		t.Errorf("CountFiles = %d, want 2", n)
	}
	if n := tree.Root.CountFolders(); n != 1 {
		t.Errorf("CountFolders = %d, want 1", n)
	}
	if n := tree.Root.CountItems(); n != 3 {
		t.Errorf("CountItems = %d, want 3", n)
	}
}

func TestURL(t *testing.T) {
	tree := New()
	music, _ := tree.AddVirtualFolder("music", "/")
	jazz, _ := tree.AddVirtualFolder("jazz", "/music")

	if u := tree.Root.URL(); u != "/" {
		t.Errorf("root URL = %q, want /", u)
	}
	if u := music.URL(); u != "/music" {
		t.Errorf("music URL = %q, want /music", u)
	}
	if u := jazz.URL(); u != "/music/jazz" {
		t.Errorf("jazz URL = %q, want /music/jazz", u)
	}
}
