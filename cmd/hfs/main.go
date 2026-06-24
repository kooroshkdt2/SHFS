//go:build headless
// +build headless

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"hfs-go/internal/config"
	"hfs-go/internal/debug"
	"hfs-go/internal/server"
	"hfs-go/internal/vfs"
)

var (
	port       = flag.Int("port", 0, "HTTP port (0 = use config)")
	root       = flag.String("root", "", "Root folder to serve")
	configFile = flag.String("config", "", "Path to config file")
	debugFlag  = flag.Bool("debug", false, "Write debug log to config dir")
)

func main() {
	// VERY FIRST: capture any crash before Sentry can init
	debug.EarlyCrashLog()

	flag.Parse()

	var cfg *config.Config
	var err error

	// Init Sentry
	debug.InitSentry()
	defer debug.Close()

	// Load config
	if *configFile != "" {
		cfg, err = config.LoadFile(*configFile)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		debug.CaptureError(fmt.Errorf("config load: %w", err))
		log.Fatalf("Config error: %v", err)
	}

	// Init debug log
	if *debugFlag || os.Getenv("SHFS_DEBUG") != "" {
		debug.InitDebugLog(cfg.GetConfigDir())
	}
	debug.Debug("SHFS headless starting")
	debug.Debug("Config dir: %s", cfg.GetConfigDir())
	debug.Debug("Config VFS root: %q", cfg.VFS.Root)
	debug.Debug("CLI root flag: %q", *root)

	if *port > 0 {
		cfg.Server.Port = *port
	}

	// Setup VFS
	tree, err := setupVFS(cfg, *root)
	if err != nil {
		debug.CaptureError(fmt.Errorf("VFS setup: %w", err))
		log.Fatalf("VFS error: %v", err)
	}

	srv := server.New(cfg, tree)
	debug.Debug("Server created, port=%d", cfg.Server.Port)
	log.Printf("Starting SHFS v%s (headless) on port %d", debug.Version, cfg.Server.Port)

	// Wire server log callback to debug
	srv.LogFn = func(msg string) {
		debug.Debug("SRV: %s", msg)
	}

	if err := srv.StartAndWait(); err != nil {
		debug.CaptureError(fmt.Errorf("server start: %w", err))
		log.Fatalf("Server error: %v", err)
	}
}

func setupVFS(cfg *config.Config, cliRoot string) (*vfs.Tree, error) {
	rootPath := cliRoot
	if rootPath == "" {
		rootPath = cfg.VFS.Root
	}

	debug.Debug("setupVFS: raw root=%q", rootPath)
	// Normalize: backslashes -> forward slashes, then clean
	rootPath = filepath.ToSlash(filepath.Clean(rootPath))
	debug.Debug("setupVFS: normalized root=%q", rootPath)

	if rootPath != "" && rootPath != "." {
		absRoot, err := filepath.Abs(rootPath)
		debug.Debug("setupVFS: filepath.Abs(%q) = %q (err=%v)", rootPath, absRoot, err)
		if err != nil {
			debug.CaptureError(fmt.Errorf("abs path %q: %w", rootPath, err))
			return nil, fmt.Errorf("resolve root path %q: %w", rootPath, err)
		}

		info, statErr := os.Stat(absRoot)
		debug.Debug("setupVFS: os.Stat(%q) = %v (err=%v)", absRoot, info, statErr)
		if statErr != nil {
			debug.CaptureError(fmt.Errorf("stat root %q: %w", absRoot, statErr))
			return nil, fmt.Errorf("root path %q: %w", absRoot, statErr)
		}

		log.Printf("Serving from: %s", absRoot)
		debug.Debug("Creating VFS from path: %s", absRoot)
		return vfs.NewFromPath(absRoot)
	}

	// No root specified — load persistent VFS
	treeFile := cfg.VFS.TreeFile
	if !filepath.IsAbs(treeFile) {
		treeFile = filepath.Join(cfg.GetConfigDir(), treeFile)
	}
	debug.Debug("setupVFS: loading persisted tree from %s", treeFile)
	tree, err := vfs.LoadTree(treeFile)
	if err != nil {
		debug.Debug("setupVFS: load failed, starting fresh: %v", err)
		tree = vfs.New()
	}
	log.Printf("VFS tree loaded with %d root children", len(tree.Root.Children))
	return tree, nil
}
