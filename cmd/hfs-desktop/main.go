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
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"hfs-go/internal/config"
	"hfs-go/internal/debug"
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
	defer debug.Close()

	// Init Sentry for crash reporting
	debug.InitSentry()

	var cfg *config.Config
	var err error
	if *cfgFile != "" {
		cfg, err = config.LoadFile(*cfgFile)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		debug.CaptureError(fmt.Errorf("config: %w", err))
		log.Fatalf("Config error: %v", err)
	}

	// Init debug log
	debug.InitDebugLog(cfg.GetConfigDir())
	debug.Debug("SHFS desktop starting")
	debug.Debug("OS: %s config dir: %s", runtime.GOOS, cfg.GetConfigDir())
	debug.Debug("Config root: %q", cfg.VFS.Root)

	if *port > 0 {
		cfg.Server.Port = *port
	}

	tree, err := setupVFS(cfg, *root)
	if err != nil {
		debug.CaptureError(fmt.Errorf("VFS: %w", err))
		log.Fatalf("VFS error: %v", err)
	}

	// ---- Single instance check ----
	if instanceAlreadyRunning(cfg.Server.Port) {
		log.Println("Another instance is already running. Bringing it to front.")
		os.Exit(0)
	}
	lockFile := filepath.Join(cfg.GetConfigDir(), "instance.lock")
	acquireLock(lockFile)
	defer os.Remove(lockFile)

	srv := server.New(cfg, tree)

	// Pre-check: is the port available?
	portAvailable := checkPortAvailable(cfg.Server.Port)

	a := app.NewWithID("com.kooroshkdt.shfs")
	w := a.NewWindow("SHFS ~ Simple HTTP File Server")

	ui := desktop.NewUI(w, srv, cfg, tree)
	ui.Build()
	w.SetContent(ui.Content())
	w.SetMainMenu(ui.BuildMenu())
	w.SetIcon(desktop.ResourceShfsIcon())

	// Register /api/show endpoint so second instance can bring this window up
	srv.HandleFunc("/api/show", func(w2 http.ResponseWriter, r *http.Request) {
		ui.BringToFront()
		w2.WriteHeader(http.StatusOK)
		w2.Write([]byte("ok"))
	})

	// Forward server log events to UI and debug
	srv.LogFn = func(msg string) {
		debug.Debug("SRV: %s", msg)
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
		time.Sleep(200 * time.Millisecond)
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
		}
	}()

	// Show window first, then set up tray after a delay (avoids Windows auto-hide)
	w.Show()
	w.RequestFocus()
	go func() {
		time.Sleep(2 * time.Second) // wait for window to fully render
		fyne.DoAndWait(func() {
			ui.SetupTray()
		})
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

// instanceAlreadyRunning checks if another instance is already running
// by trying to reach the HTTP server on the configured port.
func instanceAlreadyRunning(port int) bool {
	url := fmt.Sprintf("http://localhost:%d/api/show", port)
	resp, err := http.Get(url)
	if err != nil {
		return false // no response → no instance running
	}
	resp.Body.Close()
	return true
}

// acquireLock writes a PID lock file.
func acquireLock(lockFile string) {
	os.WriteFile(lockFile, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func setupVFS(cfg *config.Config, cliRoot string) (*vfs.Tree, error) {
	rootPath := cliRoot
	if rootPath == "" {
		rootPath = cfg.VFS.Root
	}
	rootPath = filepath.ToSlash(filepath.Clean(rootPath))
	log.Printf("Config root path: %q", rootPath)

	if rootPath != "" && rootPath != "." {
		absRoot, err := filepath.Abs(rootPath)
		if err != nil {
			return nil, fmt.Errorf("resolve root path %q: %w", rootPath, err)
		}
		log.Printf("Resolved to: %q", absRoot)
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

