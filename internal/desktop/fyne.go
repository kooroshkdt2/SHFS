//go:build !headless
// +build !headless

package desktop

import (
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"hfs-go/internal/config"
	"hfs-go/internal/server"
	"hfs-go/internal/vfs"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// UI is the main desktop user interface.
type UI struct {
	win     fyne.Window
	app     fyne.App
	deskApp desktop.App
	srv     *server.Server
	cfg     *config.Config
	tree    *vfs.Tree
	running bool

	// Layout containers
	toolbarWidget *fyne.Container
	urlBar        *fyne.Container
	statusBar     *fyne.Container

	// Split containers (for saving/restoring layout)
	centerSplit *container.Split
	mainSplit   *container.Split
	bottomSplit *container.Split

	// Toolbar widgets
	portBtn  *widget.Button
	urlEntry *widget.Entry
	startBtn *widget.Button
	graphCanvas *fyne.Container

	// VFS Tree
	vfsTree      *widget.Tree
	treeData     *vfsTreeData
	actionPerms  *widget.Button
	actionRemove *widget.Button
	actionUpload *widget.Button
	actionLabel  *widget.Label

	// Log
	logBox *widget.Entry

	// Connections
	connList *widget.List
	connData []server.ConnInfo

	// Status bar
	statusIn     *widget.Label
	statusOut    *widget.Label
	statusSpeed  *widget.Label
	statusConns  *widget.Label
	statusUptime *widget.Label

	// Graph data
	graphData []graphPoint
	content   *fyne.Container
}

type graphPoint struct{ out, in int64 }

func NewUI(win fyne.Window, srv *server.Server, cfg *config.Config, tree *vfs.Tree) *UI {
	ui := &UI{
		win:    win,
		app:    fyne.CurrentApp(),
		srv:    srv,
		cfg:    cfg,
		tree:   tree,
		running: true,
		graphData: make([]graphPoint, 0, 120),
	}
	ui.treeData = &vfsTreeData{ui: ui}
	return ui
}

func (ui *UI) Build() {
	ui.buildActionButtons()
	ui.buildToolbar()
	ui.buildURLBar()
	ui.buildGraph()
	ui.buildVFSTree()
	ui.buildLog()
	ui.buildConnections()
	ui.buildStatusBar()
	ui.buildLayout()
	ui.setupDragDrop()
	ui.setupCloseIntercept()
	ui.setupKeyboardShortcuts()

	// Save VFS tree on startup
	ui.saveState()
	go ui.pollLoop()
}

// ---- Action buttons for selected VFS node ----
func (ui *UI) buildActionButtons() {
	ui.actionLabel = widget.NewLabel("")
	ui.actionLabel.Alignment = fyne.TextAlignCenter

	ui.actionPerms = widget.NewButton("Permissions...", func() {
		if ui.treeData.selected == "" { return }
		node := ui.tree.FindByURL(ui.treeData.selected)
		if node != nil && node.IsFolder() { ui.showPermissionsDialog(node) }
	})
	ui.actionPerms.Importance = widget.LowImportance

	ui.actionUpload = widget.NewButton("Upload...", func() {
		if ui.treeData.selected == "" { return }
		node := ui.tree.FindByURL(ui.treeData.selected)
		if node != nil {
			if node.HasFlag(vfs.FlagUploadable) {
				node.ClearFlag(vfs.FlagUploadable)
			} else {
				node.SetFlag(vfs.FlagUploadable)
			}
			ui.saveState()
			ui.vfsTree.Refresh()
			ui.updateActionButtons()
		}
	})
	ui.actionUpload.Importance = widget.LowImportance

	ui.actionRemove = widget.NewButton("Remove", func() {
		if ui.treeData.selected == "" || ui.treeData.selected == "/" { return }
		node := ui.tree.FindByURL(ui.treeData.selected)
		if node == nil { return }
		dialog.ShowConfirm("Remove", "Remove '"+node.Name+"' from VFS?\n(Files on disk are not deleted)", func(ok bool) {
			if !ok { return }
			ui.tree.RemoveNode(ui.treeData.selected)
			ui.saveState()
			ui.vfsTree.Refresh()
			ui.log("Removed: " + node.Name)
			ui.treeData.selected = ""
			ui.updateActionButtons()
		}, ui.win)
	})
	ui.actionRemove.Importance = widget.WarningImportance

	// Hide all initially
	ui.actionPerms.Hide()
	ui.actionRemove.Hide()
	ui.actionUpload.Hide()
}

// ---- Keyboard shortcuts ----
func (ui *UI) setupKeyboardShortcuts() {
	ui.win.Canvas().SetOnTypedKey(func(ke *fyne.KeyEvent) {
		switch ke.Name {
		case fyne.KeyDelete, fyne.KeyBackspace:
			if ui.treeData.selected != "" && ui.treeData.selected != "/" {
				node := ui.tree.FindByURL(ui.treeData.selected)
				if node != nil {
					ui.tree.RemoveNode(ui.treeData.selected)
					ui.saveState()
					ui.vfsTree.Refresh()
					ui.log("Removed: " + node.Name)
					ui.treeData.selected = ""
					ui.updateActionButtons()
				}
			}
		case fyne.KeyF5:
			ui.vfsTree.Refresh()
			ui.log("Tree refreshed")
		case fyne.KeyEscape:
			ui.treeData.selected = ""
			ui.updateActionButtons()
		}
	})
}

// ---- Top Toolbar (matching original: Menu | Port | Mode | Start/Stop) ----
func (ui *UI) buildToolbar() {
	ui.portBtn = widget.NewButton(fmt.Sprintf("Port: %d", ui.cfg.Server.Port), func() {
		e := widget.NewEntry()
		e.SetText(fmt.Sprintf("%d", ui.cfg.Server.Port))
		dialog.ShowCustomConfirm("Change Port", "OK", "Cancel", e, func(ok bool) {
			if !ok { return }
			var p int
			if _, err := fmt.Sscanf(e.Text, "%d", &p); err == nil && p > 0 && p < 65536 {
				ui.cfg.Server.Port = p
				ui.cfg.Save()
				ui.portBtn.SetText(fmt.Sprintf("Port: %d", p))
				ui.urlEntry.SetText(fmt.Sprintf("http://localhost:%d", p))
				ui.log("Port changed to " + e.Text + ". Restart required.")
			}
		}, ui.win)
	})

	ui.startBtn = widget.NewButton("Stop", func() {
		ui.running = !ui.running
		if ui.running {
			ui.startBtn.SetText("Stop")
			ui.log("Server started")
		} else {
			ui.startBtn.SetText("Start")
			ui.log("Server stopped")
		}
	})
	ui.startBtn.Importance = widget.HighImportance

	addBtn := widget.NewButton("+ Add", func() { ui.showAddFolderDialog() })
	ui.portBtn.Importance = widget.LowImportance

	tb := container.NewHBox(
		ui.portBtn,
		widget.NewSeparator(),
		ui.startBtn,
		widget.NewSeparator(),
		addBtn,
	)
	ui.toolbarWidget = container.NewPadded(tb)
}

// ---- URL Bar ----
func (ui *UI) buildURLBar() {
	browseBtn := widget.NewButton("Open in browser", func() {
		addr := fmt.Sprintf("http://localhost:%d", ui.cfg.Server.Port)
		if u, err := url.Parse(addr); err == nil {
			ui.app.OpenURL(u)
		}
		ui.log("Opening " + addr)
	})

	ui.urlEntry = widget.NewEntry()
	ui.urlEntry.SetText(fmt.Sprintf("http://localhost:%d", ui.cfg.Server.Port))
	ui.urlEntry.OnChanged = func(s string) {
		ui.urlEntry.SetText(fmt.Sprintf("http://localhost:%d", ui.cfg.Server.Port))
	}

	ui.urlBar = container.NewBorder(nil, nil, browseBtn, nil, ui.urlEntry)
}

// ---- Bandwidth Graph (30px, pink=out, yellow=in) ----
func (ui *UI) buildGraph() {
	bg := canvas.NewRectangle(color.NRGBA{0, 0, 0, 255})
	ui.graphCanvas = container.NewMax(bg)
	ui.graphCanvas.Resize(fyne.NewSize(100, 30))
}

// ---- VFS Tree (matching original filesBox) ----
func (ui *UI) buildVFSTree() {
	ui.vfsTree = widget.NewTree(
		ui.treeData.childUIDs,
		ui.treeData.isBranch,
		ui.treeData.createNode,
		ui.treeData.updateNode,
	)
	ui.vfsTree.OpenBranch("/")
	ui.vfsTree.OnSelected = func(uid widget.TreeNodeID) {
		ui.treeData.selected = uid
		ui.updateActionButtons()
	}
}

func (ui *UI) updateActionButtons() {
	sel := ui.treeData.selected
	if sel == "" {
		ui.actionPerms.Hide()
		ui.actionRemove.Hide()
		ui.actionUpload.Hide()
		ui.actionLabel.SetText("")
		return
	}
	node := ui.tree.FindByURL(sel)
	if node == nil { return }

	ui.actionLabel.SetText("Selected: " + node.Name)
	ui.actionPerms.Show()
	ui.actionRemove.Show()

	if node.IsFolder() {
		ui.actionUpload.Show()
		if node.HasFlag(vfs.FlagUploadable) {
			ui.actionUpload.SetText("Disable Upload")
		} else {
			ui.actionUpload.SetText("Enable Upload")
		}
	} else {
		ui.actionUpload.Hide()
	}
	if sel == "/" {
		ui.actionRemove.Disable()
	} else {
		ui.actionRemove.Enable()
	}
}

// ---- Log ----
func (ui *UI) buildLog() {
	ui.logBox = widget.NewMultiLineEntry()
	ui.logBox.Wrapping = fyne.TextWrapWord
	ui.logBox.TextStyle = fyne.TextStyle{Monospace: true}
	ui.logBox.Disable() // read-only
}

// ---- Connections (matching original connBox with columns) ----
func (ui *UI) buildConnections() {
	ui.connList = widget.NewList(
		func() int { return len(ui.connData) + 1 },
		func() fyne.CanvasObject {
			return widget.NewLabel("IP Address — placeholder text for sizing purposes")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			l := obj.(*widget.Label)
			if id == 0 {
				l.SetText(fmt.Sprintf("%-22s %-28s %-12s %10s %8s",
					"IP Address", "File", "Status", "Speed", "Time"))
				l.TextStyle = fyne.TextStyle{Bold: true}
				return
			}
			id--
			if id >= len(ui.connData) { return }
			c := ui.connData[id]
			status := "idle"
			if c.Speed > 1024 { status = "xfer" }
			fn := c.FilePath
			if fn == "" { fn = c.RequestURL }
			if len(fn) > 26 { fn = fn[:26] }
			l.SetText(fmt.Sprintf("%-22s %-28s %-12s %8s/s %8s",
				c.Address, fn, status, formatBytes(c.Speed), c.ConnectedAt.Format("15:04")))
			l.TextStyle = fyne.TextStyle{Monospace: true}
		},
	)
}

// ---- Status Bar ----
func (ui *UI) buildStatusBar() {
	ui.statusIn = widget.NewLabel("In: 0 B")
	ui.statusOut = widget.NewLabel("Out: 0 B")
	ui.statusSpeed = widget.NewLabel("0 B/s")
	ui.statusConns = widget.NewLabel("0 conns")
	ui.statusUptime = widget.NewLabel("Up: 0s")

	ui.statusBar = container.NewHBox(
		ui.statusIn, widget.NewSeparator(),
		ui.statusOut, widget.NewSeparator(),
		ui.statusSpeed, widget.NewSeparator(),
		ui.statusConns, widget.NewSeparator(),
		ui.statusUptime,
	)
}

// ---- Layout: matches original HFS ----
func (ui *UI) buildLayout() {
	// Top group: toolbar + URL bar + graph (28px)
	topBox := container.NewVBox(
		container.NewPadded(ui.toolbarWidget),
		container.NewPadded(ui.urlBar),
		container.NewPadded(ui.graphCanvas),
	)

	// Center: VFS tree (left) + Log (right)
	vfsHeader := widget.NewLabelWithStyle("Virtual File System", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	vfsActions := container.NewVBox(
		ui.actionLabel,
		container.NewHBox(ui.actionPerms, ui.actionUpload, ui.actionRemove),
	)
	vfsPanel := container.NewBorder(vfsHeader, vfsActions, nil, nil, ui.vfsTree)
	logHeader := widget.NewLabelWithStyle("Log", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	logScroll := container.NewVScroll(ui.logBox)
	logPanel := container.NewBorder(logHeader, nil, nil, nil, logScroll)

	ui.centerSplit = container.NewHSplit(vfsPanel, logPanel)
	ui.centerSplit.SetOffset(ui.cfg.Layout.VSFSplit)

	// Bottom: connections + status bar
	connHeader := widget.NewLabelWithStyle("Active Connections", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	connPanel := container.NewBorder(connHeader, nil, nil, nil, ui.connList)

	ui.bottomSplit = container.NewVSplit(connPanel, container.NewPadded(ui.statusBar))
	ui.bottomSplit.SetOffset(ui.cfg.Layout.BottomSplit)

	// Main vertical split: center / bottom
	ui.mainSplit = container.NewVSplit(ui.centerSplit, ui.bottomSplit)
	ui.mainSplit.SetOffset(ui.cfg.Layout.CenterSplit)

	ui.content = container.NewBorder(topBox, nil, nil, nil, ui.mainSplit)
}

// ---- Menu bar ----
func (ui *UI) BuildMenu() *fyne.MainMenu {
	uploadItem := fyne.NewMenuItem("Upload Enabled", nil)
	uploadItem.Checked = ui.cfg.VFS.UploadEnabled
	uploadItem.Action = func() {
		ui.cfg.VFS.UploadEnabled = !ui.cfg.VFS.UploadEnabled
		uploadItem.Checked = ui.cfg.VFS.UploadEnabled
		ui.cfg.Save()
	}

	anonItem := fyne.NewMenuItem("Anonymous Upload", nil)
	anonItem.Checked = ui.cfg.VFS.AnonymousUpload
	anonItem.Action = func() {
		ui.cfg.VFS.AnonymousUpload = !ui.cfg.VFS.AnonymousUpload
		anonItem.Checked = ui.cfg.VFS.AnonymousUpload
		ui.cfg.Save()
	}

	refreshItem := fyne.NewMenuItem("Refresh Tree\tF5", func() { ui.vfsTree.Refresh() })
	exitItem := fyne.NewMenuItem("Exit", func() { ui.app.Quit() })

	settingsMenu := fyne.NewMenu("Settings",
		uploadItem,
		anonItem,
		fyne.NewMenuItemSeparator(),
		refreshItem,
		fyne.NewMenuItemSeparator(),
		exitItem,
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("About", func() {
			dialog.ShowInformation("SHFS ~ Simple HTTP File Server",
				"A cross-platform HTTP file server.\n\n"+
				"Created by Koorosh_KDT\n"+
				"GitHub: github.com/kooroshkdt2/SHFS\n\n"+
				"Select a folder in the VFS tree to see actions.\n"+
				"Use Settings menu for global options.\n"+
				"Close window to minimize to tray.", ui.win)
		}),
	)

	return fyne.NewMainMenu(settingsMenu, helpMenu)
}

// ---- Tray (minimize to tray on close) ----
func (ui *UI) setupCloseIntercept() {
	ui.win.SetCloseIntercept(func() {
		if ui.deskApp != nil {
			ui.win.Hide()
			ui.log("Minimized to tray. Right-click tray icon to restore or quit.")
		} else {
			ui.running = false
			ui.win.Close()
		}
	})
}

// SetupTray initializes the system tray. Safe to call on systems without
// tray support — recovers from panics and skips when D-Bus is unavailable.
func (ui *UI) SetupTray() {
	defer func() {
		if r := recover(); r != nil {
			ui.log(fmt.Sprintf("System tray not available: %v", r))
			ui.deskApp = nil
		}
	}()

	// On Linux only: check that D-Bus is available. systray requires D-Bus
	// and will panic on shutdown (nativeEnd -> dbus.Conn.Close on nil)
	// if initialized without a desktop session bus.
	// Windows/macOS don't use D-Bus — tray always works there.
	if runtime.GOOS == "linux" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		ui.log("No D-Bus session — system tray disabled")
		return
	}

	if desk, ok := ui.app.(desktop.App); ok {
		ui.deskApp = desk
		show := fyne.NewMenuItem("Show Window", func() {
			ui.win.Show()
			ui.win.RequestFocus()
		})
		quit := fyne.NewMenuItem("Quit SHFS", func() {
			ui.running = false
			ui.win.Close()
		})
		m := fyne.NewMenu("SHFS", show, fyne.NewMenuItemSeparator(), quit)
		desk.SetSystemTrayMenu(m)
		desk.SetSystemTrayIcon(ResourceShfsIcon())
	}
}

// ---- Save / Restore state ----
func (ui *UI) saveState() {
	ui.saveLayout()
	path := ui.cfg.VFS.TreeFile
	if path == "" { path = "vfs.yaml" }
	if !filepath.IsAbs(path) {
		path = filepath.Join(ui.cfg.GetConfigDir(), path)
	}
	ui.tree.Save(path)
	ui.cfg.Save()
}

// saveLayout saves window size and split positions to config.
func (ui *UI) saveLayout() {
	sz := ui.win.Canvas().Size()
	if sz.Width > 0 && sz.Height > 0 {
		ui.cfg.Layout.Width = int(sz.Width)
		ui.cfg.Layout.Height = int(sz.Height)
	}
	if ui.centerSplit != nil {
		ui.cfg.Layout.VSFSplit = ui.centerSplit.Offset
	}
	if ui.mainSplit != nil {
		ui.cfg.Layout.CenterSplit = ui.mainSplit.Offset
	}
	if ui.bottomSplit != nil {
		ui.cfg.Layout.BottomSplit = ui.bottomSplit.Offset
	}
}

// ---- Drag & Drop ----
func (ui *UI) setupDragDrop() {
	type droppable interface {
		SetOnDropped(func(fyne.Position, []fyne.URI))
	}
	if dw, ok := interface{}(ui.win).(droppable); ok {
		dw.SetOnDropped(func(pos fyne.Position, uris []fyne.URI) {
			added := 0
			for _, uri := range uris {
				p := filepath.Clean(uri.Path())
				n := filepath.Base(p)
				if _, err := ui.tree.AddRealFolder(n, p, "/"); err == nil {
					added++
				}
			}
			if added > 0 {
				ui.saveState()
				fyne.DoAndWait(func() { ui.vfsTree.Refresh() })
				ui.log(fmt.Sprintf("Added %d item(s) via drag & drop", added))
			}
		})
	}
}

// ---- Permissions dialog ----
func (ui *UI) showPermissionsDialog(node *vfs.Node) {
	var checks []*widget.Check
	flags := []struct {
		name string
		flag vfs.NodeFlags
	}{
		{"Browsable", vfs.FlagBrowsable},
		{"Archivable", vfs.FlagArchivable},
		{"Deletable", vfs.FlagDeletable},
		{"Uploadable", vfs.FlagUploadable},
	}
	for _, f := range flags {
		flag := f.flag
		ch := widget.NewCheck(f.name, func(b bool) {
			if b { node.SetFlag(flag) } else { node.ClearFlag(flag) }
		})
		ch.SetChecked(node.HasFlag(flag))
		checks = append(checks, ch)
	}

	content := container.NewVBox(
		widget.NewLabel("Permissions: "+node.Name),
		checks[0], checks[1], checks[2], checks[3],
	)
	dialog.ShowCustomConfirm("Node Permissions", "Save", "Cancel", content, func(ok bool) {
		if ok { ui.saveState(); ui.vfsTree.Refresh() }
	}, ui.win)
}

// ---- Add Folder dialog ----
func (ui *UI) showAddFolderDialog() {
	pathEntry := widget.NewEntry()
	pathEntry.SetPlaceHolder("/path/to/folder")
	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Name (optional)")

	folders := ui.collectFolders()
	opts := make([]string, len(folders))
	for i, n := range folders { opts[i] = n.URL() }
	parentSel := widget.NewSelect(opts, func(s string) {})
	if len(opts) > 0 { parentSel.SetSelected("/") }

	browseBtn := widget.NewButton("Browse...", func() {
		dialog.ShowFolderOpen(func(lu fyne.ListableURI, err error) {
			if err == nil && lu != nil { pathEntry.SetText(lu.Path()) }
		}, ui.win)
	})

	content := container.NewVBox(
		widget.NewLabel("Add Folder to VFS"),
		container.NewHBox(browseBtn, widget.NewLabel("or type path:")),
		pathEntry,
		widget.NewLabel("Name:"), nameEntry,
		widget.NewLabel("Parent:"), parentSel,
	)

	dialog.ShowCustomConfirm("Add Folder", "Add", "Cancel", content, func(ok bool) {
		if !ok { return }
		path := pathEntry.Text
		name := nameEntry.Text
		parent := parentSel.Selected
		if path == "" && name == "" { return }
		if name == "" { name = filepath.Base(path) }
		if parent == "" { parent = "/" }
		if _, err := ui.tree.AddRealFolder(name, path, parent); err != nil {
			dialog.ShowError(err, ui.win)
			return
		}
		ui.saveState()
		ui.vfsTree.Refresh()
		ui.log(fmt.Sprintf("Added: %s", name))
	}, ui.win)
}

func (ui *UI) collectFolders() []*vfs.Node {
	var f []*vfs.Node
	ui.tree.Walk(func(n *vfs.Node, d int) error {
		if n.IsFolder() { f = append(f, n) }
		return nil
	})
	return f
}

// ---- Log callback from server ----
func (ui *UI) LogCallback(msg string) {
	fyne.DoAndWait(func() { ui.log(msg) })
}

// BringToFront shows and focuses the window (single-instance signal).
func (ui *UI) BringToFront() {
	fyne.DoAndWait(func() {
		ui.win.Show()
		ui.win.RequestFocus()
	})
}

func (ui *UI) log(msg string) {
	now := time.Now().Format("15:04:05")
	line := now + "  " + msg + "\n"
	ui.logBox.SetText(ui.logBox.Text + line)

	// Rotate log: when over 500 lines, trim oldest ~100 to keep ~400
	if n := strings.Count(ui.logBox.Text, "\n"); n > 500 {
		lines := strings.Split(ui.logBox.Text, "\n")
		keep := n - 400 // drop oldest lines
		if keep < 0 { keep = 0 }
		if keep < len(lines) {
			ui.logBox.SetText(strings.Join(lines[keep:], "\n"))
		}
	}

	// Auto-scroll to bottom
	ui.logBox.CursorRow = strings.Count(ui.logBox.Text, "\n")
}

// ---- Poll loop ----
var lastIn, lastOut int64

func (ui *UI) pollLoop() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	ticks := 0
	for range t.C {
		ticks++
		stats := ui.srv.GetStats()
		conns := ui.srv.GetConnections()
		outNow, inNow := stats.BytesSent, stats.BytesRecv

		// Calculate deltas since last tick (2s)
		deltaOut := int64(0)
		deltaIn := int64(0)
		if lastOut > 0 || lastIn > 0 {
			deltaOut = outNow - lastOut
			deltaIn = inNow - lastIn
		}
		// Speed = bytes/sec (deltas are over 2s, so divide by 2)
		spd := (deltaOut + deltaIn) / 2
		lastOut, lastIn = outNow, inNow

		// Graph uses raw deltas per tick
		ui.graphData = append(ui.graphData, graphPoint{out: deltaOut, in: deltaIn})
		if len(ui.graphData) > 120 { ui.graphData = ui.graphData[1:] }

		gc := make([]graphPoint, len(ui.graphData))
		copy(gc, ui.graphData)

		fyne.DoAndWait(func() {
			ui.statusConns.SetText(fmt.Sprintf("%d conns", stats.Connections))
			ui.statusUptime.SetText("Up: " + stats.Uptime)
			ui.statusIn.SetText("In: " + formatBytes(stats.BytesRecv))
			ui.statusOut.SetText("Out: " + formatBytes(stats.BytesSent))
			if spd > 0 { ui.statusSpeed.SetText(formatBytes(spd) + "/s") }
			ui.connData = conns
			ui.connList.Refresh()
			ui.redrawGraph(gc)

			// Update tray icon with live bandwidth bars
			if ui.deskApp != nil && spd > 0 {
				maxBW := spd * 2
				if maxBW < 1024 { maxBW = 1024 }
				icon := BandwidthTrayIcon(deltaOut, deltaIn, maxBW)
				ui.deskApp.SetSystemTrayIcon(icon)
			}

			// Save layout periodically (every 30s) so window size/split
			// positions persist across restarts
			if ticks%15 == 0 {
				ui.saveLayout()
				ui.cfg.Save()
			}
		})
	}
}

func (ui *UI) redrawGraph(data []graphPoint) {
	objs := make([]fyne.CanvasObject, 0, len(data)*2)
	maxV := int64(1024)
	for _, p := range data {
		if p.out > maxV { maxV = p.out }
		if p.in > maxV { maxV = p.in }
	}
	h := float32(28)
	for i, p := range data {
		outH := float32(p.out) / float32(maxV) * h
		inH := float32(p.in) / float32(maxV) * h
		outBar := canvas.NewRectangle(color.NRGBA{0xE0, 0x90, 0xA0, 0xFF}) // pink = out
		inBar := canvas.NewRectangle(color.NRGBA{0xE0, 0xD0, 0x90, 0xFF})  // yellow = in
		outBar.Resize(fyne.NewSize(3, outH))
		outBar.Move(fyne.NewPos(float32(i)*4, h-outH))
		inBar.Resize(fyne.NewSize(3, inH))
		inBar.Move(fyne.NewPos(float32(i)*4, h-outH-inH))
		objs = append(objs, outBar, inBar)
		_ = inBar
	}
	// Keep the black background
	bg := canvas.NewRectangle(color.NRGBA{0, 0, 0, 255})
	bg.Resize(fyne.NewSize(float32(len(data))*4+10, 30))
	all := []fyne.CanvasObject{bg}
	all = append(all, objs...)
	ui.graphCanvas.Objects = all
	ui.graphCanvas.Refresh()
}

func (ui *UI) Content() *fyne.Container { return ui.content }

// ---- VFS Tree Data ----
type vfsTreeData struct {
	ui       *UI
	selected string
}

func (d *vfsTreeData) childUIDs(uid widget.TreeNodeID) []widget.TreeNodeID {
	node := d.ui.tree.FindByURL(uid)
	if node == nil || !node.IsFolder() { return nil }
	if node.IsRealFolder() && !node.IsScanned { node.ScanRealFolder() }
	ids := make([]widget.TreeNodeID, len(node.Children))
	for i, c := range node.Children { ids[i] = c.URL() }
	sort.Slice(ids, func(i, j int) bool {
		ni, nj := d.ui.tree.FindByURL(ids[i]), d.ui.tree.FindByURL(ids[j])
		if ni == nil || nj == nil { return false }
		if ni.IsFolder() != nj.IsFolder() { return ni.IsFolder() }
		return strings.ToLower(ni.Name) < strings.ToLower(nj.Name)
	})
	return ids
}

func (d *vfsTreeData) isBranch(uid widget.TreeNodeID) bool {
	n := d.ui.tree.FindByURL(uid)
	return n != nil && n.IsFolder()
}

func (d *vfsTreeData) createNode(branch bool) fyne.CanvasObject {
	return container.NewHBox(
		widget.NewIcon(theme.FolderIcon()),
		widget.NewLabel("placeholder"),
	)
}

func (d *vfsTreeData) updateNode(uid widget.TreeNodeID, branch bool, obj fyne.CanvasObject) {
	node := d.ui.tree.FindByURL(uid)
	if node == nil { return }
	c := obj.(*fyne.Container)
	icon := c.Objects[0].(*widget.Icon)
	label := c.Objects[1].(*widget.Label)
	if node.IsFolder() {
		icon.SetResource(theme.FolderIcon())
		label.SetText(node.Name + "/")
	} else {
		icon.SetResource(theme.FileIcon())
		label.SetText(node.Name)
	}
}

// ---- Helpers ----

func formatBytes(n int64) string {
	if n < 1024 { return fmt.Sprintf("%d B", n) }
	div, exp := int64(1024), 0
	for m := n / 1024; m >= 1024; m /= 1024 { div *= 1024; exp++ }
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
