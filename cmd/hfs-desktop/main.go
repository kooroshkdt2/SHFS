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
	// VERY FIRST: set up crash log BEFORE anything else.
	// On Windows with -H windowsgui, all stderr is lost — this captures panics to a file.
	debug.EarlyCrashLog()
	// Catch any panic and flush to crash log before exit
	defer debug.RecoverPanic()

	// Enable software OpenGL via Mesa llvmpipe when no GPU is available.
	// Critical for VMs, RDP sessions, and systems without 3D acceleration.
	// Place Mesa's opengl32.dll + libgallium_wgl.dll next to the .exe.
	if os.Getenv("GALLIUM_DRIVER") == "" {
		os.Setenv("GALLIUM_DRIVER", "llvmpipe")
	}

	debug.Debug("main: starting, parsing flags")
	port := flag.Int("port", 0, "HTTP port")
	root := flag.String("root", "", "Root folder")
	cfgFile := flag.String("config", "", "Config file path")
	flag.Parse()

	// Init Sentry for crash reporting
	debug.Debug("main: initializing Sentry")
	debug.InitSentry()
	defer debug.Close()

	var cfg *config.Config
	var err error
	if *cfgFile != "" {
		cfg, err = config.LoadFile(*cfgFile)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		debug.CaptureFatal(fmt.Errorf("config: %w", err))
		log.Fatalf("Config error: %v", err)
	}

	// Init debug log
	debug.InitDebugLog(cfg.GetConfigDir())
	debug.Debug("SHFS desktop v%s starting", debug.Version)
	debug.Debug("OS: %s Arch: %s", runtime.GOOS, runtime.GOARCH)
	debug.Debug("Config dir: %s", cfg.GetConfigDir())
	debug.Debug("Config root: %q", cfg.VFS.Root)

	if *port > 0 {
		cfg.Server.Port = *port
	}

	debug.Debug("main: setting up VFS")
	tree, err := setupVFS(cfg, *root)
	if err != nil {
		debug.CaptureFatal(fmt.Errorf("VFS: %w", err))
		log.Fatalf("VFS error: %v", err)
	}

	// ---- Single instance check ----
	debug.Debug("main: checking single instance on port %d", cfg.Server.Port)
	if instanceAlreadyRunning(cfg.Server.Port) {
		debug.Debug("main: another instance is running, exiting")
		log.Println("Another instance is already running. Bringing it to front.")
		os.Exit(0)
	}
	lockFile := filepath.Join(cfg.GetConfigDir(), "instance.lock")
	acquireLock(lockFile)
	defer os.Remove(lockFile)

	debug.Debug("main: creating server")
	srv := server.New(cfg, tree)

	// Pre-check: is the port available?
	portAvailable := checkPortAvailable(cfg.Server.Port)

	// ---- Fyne app init (most likely crash point on Windows) ----
	debug.Debug("main: creating Fyne app")
	a := app.NewWithID("com.kooroshkdt.shfs")
	debug.Debug("main: Fyne app created OK, creating window")
	w := a.NewWindow("SHFS ~ Simple HTTP File Server")
	debug.Debug("main: window created OK, building UI")

	ui := desktop.NewUI(w, srv, cfg, tree)
	ui.Build()
	debug.Debug("main: UI built OK")
	w.SetContent(ui.Content())
	w.SetMainMenu(ui.BuildMenu())
	w.SetIcon(desktop.ResourceShfsIcon())
	debug.Debug("main: window configured OK")

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
	debug.Debug("main: starting HTTP server in background")
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

	// Show the window
	debug.Debug("main: showing window")
	w.Show()
	w.RequestFocus()

	debug.Debug("main: entering event loop (ShowAndRun)")
	w.ShowAndRun()

	debug.Debug("main: event loop exited, shutting down")
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

	debug.Debug("setupVFS: raw root=%q", rootPath)
	rootPath = filepath.ToSlash(filepath.Clean(rootPath))
	debug.Debug("setupVFS: normalized root=%q", rootPath)

	if rootPath != "" && rootPath != "." {
		absRoot, err := filepath.Abs(rootPath)
		debug.Debug("setupVFS: filepath.Abs(%q) = %q (err=%v)", rootPath, absRoot, err)
		if err != nil {
			return nil, fmt.Errorf("resolve root path %q: %w", rootPath, err)
		}

		info, statErr := os.Stat(absRoot)
		debug.Debug("setupVFS: os.Stat(%q) err=%v", absRoot, statErr)
		if statErr != nil {
			return nil, fmt.Errorf("root path %q: %w", absRoot, statErr)
		}
		_ = info

		debug.Debug("setupVFS: creating VFS from path %q", absRoot)
		log.Printf("Serving from: %s", absRoot)
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
	debug.Debug("setupVFS: VFS loaded with %d root children", len(tree.Root.Children))
	return tree, nil
}

