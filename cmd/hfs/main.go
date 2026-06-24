//go:build headless
// +build headless

// HFS Go — Headless mode entry point.
//
// Build: go build -tags headless ./cmd/hfs
// Usage: ./hfs --port 8080 --root /data

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"hfs-go/internal/config"
	"hfs-go/internal/server"
	"hfs-go/internal/vfs"
)

var (
	port       = flag.Int("port", 0, "HTTP port (0 = use config)")
	root       = flag.String("root", "", "Root folder to serve (empty = use VFS tree)")
	configFile = flag.String("config", "", "Path to config file")
)

func main() {
	flag.Parse()

	var cfg *config.Config
	var err error
	if *configFile != "" {
		cfg, err = config.LoadFile(*configFile)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}
	if *port > 0 {
		cfg.Server.Port = *port
	}

	tree, err := setupVFS(cfg, *root)
	if err != nil {
		log.Fatalf("VFS error: %v", err)
	}

	srv := server.New(cfg, tree)
	log.Printf("Starting HFS Go v0.1.0 (headless mode)")
	log.Printf("Listening on port %d", srv.Config().Server.Port)

	if err := srv.StartAndWait(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func setupVFS(cfg *config.Config, cliRoot string) (*vfs.Tree, error) {
	rootPath := cliRoot
	if rootPath == "" {
		rootPath = cfg.VFS.Root
	}
	if rootPath != "" {
		absRoot, err := filepath.Abs(rootPath)
		if err != nil {
			return nil, fmt.Errorf("resolve root path: %w", err)
		}
		if _, err := os.Stat(absRoot); err != nil {
			return nil, fmt.Errorf("root path %q: %w", absRoot, err)
		}
		log.Printf("Serving from: %s", absRoot)
		return vfs.NewFromPath(absRoot)
	}
	treeFile := cfg.VFS.TreeFile
	if !filepath.IsAbs(treeFile) {
		treeFile = filepath.Join(cfg.GetConfigDir(), treeFile)
	}
	tree, err := vfs.LoadTree(treeFile)
	if err != nil {
		log.Printf("Could not load VFS tree, starting fresh: %v", err)
		tree = vfs.New()
	}
	log.Printf("VFS tree loaded with %d root children", len(tree.Root.Children))
	return tree, nil
}
