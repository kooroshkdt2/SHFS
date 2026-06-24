//go:build !headless
// +build !headless

// HFS Go — Desktop mode entry point (Wails v2)
//
// The frontend is embedded in the Wails window and communicates
// with the HTTP server via fetch() with CORS enabled.

package main

import (
	"embed"
	"fmt"
	"log"
	"os"

	"hfs-go/internal/config"
	"hfs-go/internal/desktop"
	"hfs-go/internal/server"
	"hfs-go/internal/vfs"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed frontend/*
var frontendAssets embed.FS

func main() {
	// Load configuration
	var cfg *config.Config
	var err error
	if len(os.Args) > 1 && os.Args[1] != "" && os.Args[1][0] != '-' {
		cfg, err = config.LoadFile(os.Args[1])
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	for i, arg := range os.Args {
		if arg == "--port" && i+1 < len(os.Args) {
			fmt.Sscanf(os.Args[i+1], "%d", &cfg.Server.Port)
		}
	}

	// Setup VFS
	tree, err := setupVFS(cfg)
	if err != nil {
		log.Fatalf("VFS error: %v", err)
	}

	// Create the HTTP server (with CORS for Wails webview)
	srv := server.New(cfg, tree)

	// Start HTTP server in background (skip during Wails bindings generation)
	if os.Getenv("WAILS_BINDING_GEN") != "1" {
		go func() {
			if err := srv.Start(); err != nil {
				log.Printf("Server start error: %v", err)
			}
		}()
	}

	// Create the desktop bridge (native features: drag-drop, dialogs)
	app := desktop.NewApp(srv, cfg, tree)

	// Run Wails desktop app
	err = wails.Run(&options.App{
		Title:     "HFS — HTTP File Server",
		Width:     1024,
		Height:    700,
		MinWidth:  800,
		MinHeight: 500,
		AssetServer: &assetserver.Options{
			Assets: frontendAssets,
		},
		BackgroundColour: &options.RGBA{R: 240, G: 242, B: 245, A: 255},
		OnStartup:        app.Startup,
		OnShutdown:       app.Shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Mac: &mac.Options{
			About: &mac.AboutInfo{
				Title:   "HFS",
				Message: "HTTP File Server — Cross-platform rewrite in Go",
			},
		},
		Linux: &linux.Options{
			ProgramName: "hfs",
		},
	})

	if err != nil {
		log.Fatalf("Wails error: %v", err)
	}
}

func setupVFS(cfg *config.Config) (*vfs.Tree, error) {
	rootPath := cfg.VFS.Root
	if rootPath != "" {
		if _, err := os.Stat(rootPath); err != nil {
			return nil, err
		}
		tree, err := vfs.NewFromPath(rootPath)
		if err != nil {
			return nil, err
		}
		log.Printf("Serving from: %s", rootPath)
		return tree, nil
	}

	treeFile := cfg.VFS.TreeFile
	tree, err := vfs.LoadTree(treeFile)
	if err != nil {
		log.Printf("Could not load VFS, starting fresh: %v", err)
		tree = vfs.New()
	}
	return tree, nil
}
