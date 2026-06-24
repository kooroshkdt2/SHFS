# SHFS — Simple HTTP File Server

A cross-platform HTTP file server with a native desktop GUI, built in Go.
Drag & drop files, manage a virtual file system tree, and share files over HTTP
in seconds — no configuration needed.

Created by **Koorosh_KDT**.

## Features

- **Cross-platform** — Windows, macOS, and Linux desktop apps
- **Headless mode** — server-only binary for containers and Raspberry Pi
- **Drag & drop** — add files/folders instantly to the server
- **Virtual File System** — organize real and virtual folders in a tree
- **Resumable downloads** — HTTP range requests supported
- **Upload support** — with optional anonymous upload and per-folder permissions
- **Live bandwidth graph** — pink=outgoing, yellow=incoming
- **Connection monitor** — see active transfers with real-time speed
- **System tray** — minimize to tray, keep serving in background
- **Config persistence** — settings and VFS tree saved between sessions

## Screenshot

```
┌──────────────────────────────────────────────────────────┐
│ [Settings] [Help]                          SHFS ~ Simple │
├──────────────────────────────────────────────────────────┤
│ [Port: 8080] │ [Stop] │ [+ Add]                           │
├──────────────────────────────────────────────────────────┤
│ [Open in browser]  [http://localhost:8080             ]    │
├──────────────────────────────────────────────────────────┤
│ ▂▃▅▆▇█▇▅▃▁  Pink=out  Yellow=in                        │
├──────────────────────┬───────────────────────────────────┤
│ Virtual File System  │ Log                               │
│ ▼ / (root)           │ 15:04:32  Server started          │
│   📁 documents/      │ 15:04:35  Download: song.mp3      │
│   📁 music/          │ 15:04:38  Upload: photo.jpg       │
│   📄 index.html      │                                   │
├──────────────────────┴───────────────────────────────────┤
│ IP Address     File           Status  Speed     Time      │
│ 192.168.1.5    /music/song    xfer    2.3 MB/s  15:04    │
├──────────────────────────────────────────────────────────┤
│ In: 142 MB | Out: 2.1 GB | 512 KB/s | 3 conns | Up: 2h   │
└──────────────────────────────────────────────────────────┘
```

## Installation

### Download Pre-built Binaries

Go to [Releases](https://github.com/kooroshkdt2/SHFS/releases) and download
the binary for your platform.

### Build from Source

```bash
# Requirements: Go 1.22+, X11/GL dev libraries (Linux), Xcode (macOS)

# Desktop app with GUI
git clone https://github.com/kooroshkdt2/SHFS.git
cd SHFS
go build -o shfs ./cmd/hfs-desktop

# Headless server (no GUI)
go build -tags headless -o shfs-headless ./cmd/hfs
```

### Linux Dependencies

```bash
# Ubuntu/Debian
sudo apt install libx11-dev libgl1-mesa-dev libxrandr-dev \
  libxcursor-dev libxinerama-dev libxi-dev libxxf86vm-dev

# Fedora
sudo dnf install libX11-devel mesa-libGL-devel libXrandr-devel \
  libXcursor-devel libXinerama-devel libXi-devel libXxf86vm-devel
```

## Usage

### Desktop Mode
```bash
./shfs                          # Start with GUI (default port 8080)
./shfs --port 9090              # Custom port
./shfs --root /home/user/share  # Serve from specific folder
```

### Headless Mode (Server Only)
```bash
./shfs-headless                 # Start server on port 8080
./shfs-headless --port 9090     # Custom port
./shfs-headless --root /data    # Serve from specific folder
```

### Web Admin Panel
When running in headless mode, access the admin panel at:
```
http://localhost:8080/admin/
```

## REST API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/vfs/tree` | GET | Full VFS tree |
| `/api/vfs/folders` | POST | Add real or virtual folder |
| `/api/vfs/nodes/*` | PATCH | Update node properties |
| `/api/vfs/nodes/*` | DELETE | Remove node from VFS |
| `/api/server/stats` | GET | Server statistics |
| `/api/server/connections` | GET | Active connections |
| `/api/config` | GET/PUT | Read/update configuration |
| `/api/accounts` | GET/POST/DELETE | Account management |
| `/api/search?q=term` | GET | Search VFS |
| `/api/progress` | GET | SSE transfer progress |

## Configuration

Config is stored in `~/.config/shfs/config.yaml`:

```yaml
server:
  port: 8080
  max_connections: 0        # 0 = unlimited
  max_bandwidth_kbps: 0
vfs:
  root: ""                  # Root folder path
  tree_file: vfs.yaml       # VFS persistence file
  anonymous_upload: false
  upload_enabled: true
auth:
  realm: "SHFS"
accounts: []
```

## License

GPLv2 — See [LICENSE](LICENSE) file.

## Credits

Created by **Koorosh_KDT** — A modern Go rewrite of the classic
[HFS ~ HTTP File Server](https://www.rejetto.com/hfs/) by Massimo Melina.

---

⭐ If you find this useful, please star the repo!
