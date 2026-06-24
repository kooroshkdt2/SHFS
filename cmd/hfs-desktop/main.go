//go:build !headless
// +build !headless

// HFS Go — Fyne Desktop App
//
// Build: go build ./cmd/hfs-desktop
// Run:   ./hfs-desktop

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"hfs-go/internal/config"
	"hfs-go/internal/desktop"
	"hfs-go/internal/server"
	"hfs-go/internal/vfs"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
)

func main() {
	port := flag.Int("port", 0, "HTTP port")
	root := flag.String("root", "", "Root folder")
	cfgFile := flag.String("config", "", "Config file path")
	flag.Parse()

	var cfg *config.Config
	var err error
	if *cfgFile != "" {
		cfg, err = config.LoadFile(*cfgFile)
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

	// Pre-check: is the port available?
	portAvailable := checkPortAvailable(cfg.Server.Port)

	a := app.NewWithID("com.kooroshkdt.shfs")
	w := a.NewWindow("SHFS ~ Simple HTTP File Server")

	ui := desktop.NewUI(w, srv, cfg, tree)
	ui.Build()
	w.SetContent(ui.Content())
	w.SetMainMenu(ui.BuildMenu())

	// Forward server log events to the UI
	srv.LogFn = func(msg string) {
		ui.LogCallback(msg)
	}
	w.Resize(fyne.NewSize(900, 600))
	w.SetMaster()

	// Start the HTTP server in background, notify UI of any errors
	serverErr := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			serverErr <- err
		}
	}()

	// Show port-busy dialog after window opens if port was taken
	if !portAvailable {
		time.Sleep(200 * time.Millisecond) // let window render
		dialog.ShowError(
			fmt.Errorf("Port %d is already in use.\n\nPlease choose a different port (click 'Port: %d' button) or stop the other program using this port.", cfg.Server.Port, cfg.Server.Port),
			w,
		)
	}

	// Watch for late server errors
	go func() {
		select {
		case err := <-serverErr:
			dialog.ShowError(fmt.Errorf("Server failed to start: %v", err), w)
		case <-time.After(3 * time.Second):
			// Server started OK
		}
	}()

	w.ShowAndRun()

	log.Println("Shutting down...")
	srv.Shutdown()
}

// checkPortAvailable returns true if the port can be listened on.
func checkPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
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
		log.Printf("Could not load VFS, starting fresh: %v", err)
		tree = vfs.New()
	}
	return tree, nil
}

