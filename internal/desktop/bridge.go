//go:build !headless
// +build !headless

package desktop

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"hfs-go/internal/auth"
	"hfs-go/internal/config"
	"hfs-go/internal/server"
	"hfs-go/internal/vfs"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App bridges the Go backend to the Wails frontend.
type App struct {
	ctx      context.Context
	srv      *server.Server
	cfg      *config.Config
	tree     *vfs.Tree
	running  atomic.Bool
}

func NewApp(srv *server.Server, cfg *config.Config, tree *vfs.Tree) *App {
	return &App{srv: srv, cfg: cfg, tree: tree}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.running.Store(true)
	log.Printf("HFS Desktop started")
}

func (a *App) Shutdown(ctx context.Context) {
	a.running.Store(false)
	log.Printf("HFS Desktop shutting down")
	a.srv.Shutdown()
}

// ---- VFS ----

func (a *App) GetTree() *vfs.Node { return a.tree.Root }

func (a *App) AddFolder(name, path, parent string) (*vfs.Node, error) {
	if parent == "" { parent = "/" }
	if name == "" && path != "" { name = filepath.Base(path) }
	if path == "" {
		var err error
		path, err = runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select folder to serve"})
		if err != nil || path == "" {
			return nil, fmt.Errorf("no folder selected")
		}
		if name == "" { name = filepath.Base(path) }
	}
	node, err := a.tree.AddRealFolder(name, path, parent)
	if err == nil { a.tree.Save(a.cfg.VFS.TreeFile) }
	return node, err
}

func (a *App) AddVirtualFolder(name, parent string) (*vfs.Node, error) {
	if parent == "" { parent = "/" }
	node, err := a.tree.AddVirtualFolder(name, parent)
	if err == nil { a.tree.Save(a.cfg.VFS.TreeFile) }
	return node, err
}

func (a *App) RemoveNode(vpath string) error {
	err := a.tree.RemoveNode(vpath)
	if err == nil { a.tree.Save(a.cfg.VFS.TreeFile) }
	return err
}

func (a *App) UpdateNode(vpath, comment string, flags int, uploadFilter string) error {
	node := a.tree.FindByURL(vpath)
	if node == nil { return fmt.Errorf("not found") }
	node.Comment = comment
	node.Flags = vfs.NodeFlags(flags)
	node.UploadFilter = uploadFilter
	a.tree.Save(a.cfg.VFS.TreeFile)
	return nil
}

func (a *App) GetNode(vpath string) *vfs.Node { return a.tree.FindByURL(vpath) }

func (a *App) HandleDrop(paths []string, parent string) ([]*vfs.Node, error) {
	if parent == "" { parent = "/" }
	var added []*vfs.Node
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil { continue }
		name := filepath.Base(p)
		if info.IsDir() {
			node, err := a.tree.AddRealFolder(name, p, parent)
			if err == nil { added = append(added, node) }
		} else {
			node, err := a.tree.AddRealFolder(name, p, parent)
			if err == nil { added = append(added, node) }
		}
	}
	if len(added) > 0 { a.tree.Save(a.cfg.VFS.TreeFile) }
	return added, nil
}

// ---- Server Control ----

func (a *App) GetServerURL() string {
	return fmt.Sprintf("http://localhost:%d", a.cfg.Server.Port)
}

func (a *App) OpenInBrowser() {
	runtime.BrowserOpenURL(a.ctx, a.GetServerURL())
}

func (a *App) GetPort() int { return a.cfg.Server.Port }

// ---- Stats ----

func (a *App) GetStats() map[string]interface{} {
	s := a.srv.GetStats()
	return map[string]interface{}{
		"uptime":           s.Uptime,
		"connections":      s.Connections,
		"bytesSent":        s.BytesSent,
		"bytesRecv":        s.BytesRecv,
		"hits":             s.HitsLogged,
		"downloads":        s.DownloadsLogged,
		"uploads":          s.UploadsLogged,
	}
}

// ConnItem is a connection entry returned to the frontend.
type ConnItem struct {
	Address  string `json:"address"`
	Port     int    `json:"port"`
	File     string `json:"file"`
	Status   string `json:"status"`
	Speed    int64  `json:"speed"`     // bytes/sec
	Percent  int    `json:"percent"`
	TimeLeft string `json:"timeLeft"`
	Sent     int64  `json:"sent"`
	Total    int64  `json:"total"`
}

// GetConnections returns active connections with speed info.
func (a *App) GetConnections() []ConnItem {
	conns := a.srv.GetConnections()
	items := make([]ConnItem, 0, len(conns))
	for _, c := range conns {
		elapsed := time.Since(c.ConnectedAt).Seconds()
		speed := int64(0)
		if elapsed > 0 {
			speed = int64(float64(c.BytesSent) / elapsed)
		}
		status := "idle"
		if speed > 1024 {
			status = "transferring"
		}
		if c.RequestURL != "" {
			status = "browsing"
		}
		items = append(items, ConnItem{
			Address:  c.Address,
			Port:     c.Port,
			File:     c.RequestURL,
			Status:   status,
			Speed:    speed,
			Sent:     c.BytesSent,
		})
	}
	return items
}

// GetTotals returns aggregate speed info.
func (a *App) GetTotals() map[string]interface{} {
	conns := a.srv.GetConnections()
	var totalSpeed int64
	var totalSent int64
	var totalRecv int64
	for _, c := range conns {
		elapsed := time.Since(c.ConnectedAt).Seconds()
		if elapsed > 0 {
			totalSpeed += int64(float64(c.BytesSent+c.BytesRecv) / elapsed)
		}
		totalSent += c.BytesSent
		totalRecv += c.BytesRecv
	}
	return map[string]interface{}{
		"totalSpeed": totalSpeed,
		"totalSent":  totalSent,
		"totalRecv":  totalRecv,
	}
}

// ---- Config ----

func (a *App) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"port":          a.cfg.Server.Port,
		"maxConn":       a.cfg.Server.MaxConnections,
		"maxBW":         a.cfg.Server.MaxBandwidthKbps,
		"realm":         a.cfg.Auth.Realm,
	}
}

func (a *App) UpdateConfig(port, maxConn, maxBW int, realm string) error {
	if port > 0 && port < 65536 { a.cfg.Server.Port = port }
	if maxConn >= 0 { a.cfg.Server.MaxConnections = maxConn }
	if maxBW >= 0 { a.cfg.Server.MaxBandwidthKbps = maxBW }
	if realm != "" { a.cfg.Auth.Realm = realm }
	return a.cfg.Save()
}

// ---- Accounts ----

func (a *App) GetAccounts() []config.Account { return a.cfg.Accounts }

func (a *App) AddAccount(username, password string, permissions []string) error {
	hashed, err := auth.HashPassword(password)
	if err != nil { return err }
	a.cfg.AddAccount(config.Account{
		Username: username, Password: hashed,
		Permissions: permissions, Enabled: true,
	})
	return a.cfg.Save()
}

func (a *App) DeleteAccount(username string) error {
	a.cfg.RemoveAccount(username)
	return a.cfg.Save()
}

// ---- Dialogs ----

func (a *App) PickFolder() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Select folder"})
}

// ---- Status Bar Data ----

type StatusBarData struct {
	Port        int    `json:"port"`
	ServerURL   string `json:"serverUrl"`
	Running     bool   `json:"running"`
	Connections int    `json:"connections"`
	TotalIn     int64  `json:"totalIn"`
	TotalOut    int64  `json:"totalOut"`
	TotalSpeed  int64  `json:"totalSpeed"`
	Uptime      string `json:"uptime"`
}

func (a *App) GetStatusBar() StatusBarData {
	s := a.srv.GetStats()
	totals := a.GetTotals()
	return StatusBarData{
		Port:        a.cfg.Server.Port,
		ServerURL:   a.GetServerURL(),
		Running:     a.running.Load(),
		Connections: s.Connections,
		TotalIn:     totals["totalRecv"].(int64),
		TotalOut:    totals["totalSent"].(int64),
		TotalSpeed:  totals["totalSpeed"].(int64),
		Uptime:      s.Uptime,
	}
}

// formatBytesNum is used in the bridge for computing totals.
func formatBytesNum(n int64) string {
	if n < 1024 { return fmt.Sprintf("%d B", n) }
	div, exp := int64(1024), 0
	for m := n / 1024; m >= 1024; m /= 1024 { div *= 1024; exp++ }
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
