// Package server provides the HTTP server for HFS.
package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"hfs-go/internal/auth"
	"hfs-go/internal/config"
	"hfs-go/internal/vfs"
)

// Server is the HFS HTTP server.
type Server struct {
	cfg      *config.Config
	vfs      *vfs.Tree
	sessions *auth.SessionStore
	httpSrv  *http.Server
	mux      *http.ServeMux // stored for dynamic route registration

	// Stats
	startTime   time.Time
	mu          sync.RWMutex
	connections map[string]*ConnInfo
	bytesSent   int64
	bytesRecv   int64
	hitsLogged  int64
	downloadsLogged int64
	uploadsLogged   int64

	// Channels for progress broadcast
	progressCh chan ProgressEvent
	progressSubs map[chan ProgressEvent]struct{}
	progressMu   sync.Mutex

	// Log callback for UI notifications
	LogFn func(string)

	// Ban list (runtime, synced from config)
	bannedIPs map[string]string
	banMu     sync.RWMutex
}

// ConnInfo tracks a single connection.
type ConnInfo struct {
	Address     string    `json:"address"`
	Port        int       `json:"port"`
	ConnectedAt time.Time `json:"connected_at"`
	BytesSent   int64     `json:"bytes_sent"`
	BytesRecv   int64     `json:"bytes_recv"`
	RequestURL  string    `json:"request_url,omitempty"`
	User        string    `json:"user,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
	Speed       int64     `json:"speed"` // bytes/sec since last sample
	FilePath    string    `json:"file_path,omitempty"`
	lastSent    int64     `json:"-"`
	lastRecv    int64     `json:"-"`
	lastSample  time.Time `json:"-"`
}

// ProgressEvent represents an upload/download progress update.
type ProgressEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // "upload-start", "upload-progress", "upload-done", "download-start", "download-progress", "download-done"
	Filename  string `json:"filename"`
	Folder    string `json:"folder,omitempty"`
	Bytes     int64  `json:"bytes"`
	Total     int64  `json:"total"`
	Percent   int    `json:"percent"`
	Speed     int64  `json:"speed,omitempty"` // bytes/sec
}

// New creates a new Server.
func New(cfg *config.Config, tree *vfs.Tree) *Server {
	s := &Server{
		cfg:          cfg,
		vfs:          tree,
		sessions:     auth.NewSessionStore(cfg.Auth.SessionTimeout),
		startTime:    time.Now(),
		connections:  make(map[string]*ConnInfo),
		progressCh:   make(chan ProgressEvent, 256),
		progressSubs: make(map[chan ProgressEvent]struct{}),
		bannedIPs:    make(map[string]string),
	}

	// Load bans from config
	for _, ban := range cfg.Bans {
		s.bannedIPs[ban.Address] = ban.Reason
	}

	return s
}

// Start begins listening and serving HTTP requests.
// In headless mode, this blocks until a signal is received.
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.cfg.Server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	s.httpSrv = &http.Server{
		Handler:      s.routes(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // no timeout for large file transfers
		IdleTimeout:  120 * time.Second,
		ConnContext:  s.connContext,
	}

	log.Printf("HFS server started on http://%s", listener.Addr().String())

	// Start progress broadcaster
	go s.broadcastProgress()

	// Serve in background
	go func() {
		if err := s.httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	return nil
}

// StartAndWait starts the server and blocks until a shutdown signal.
func (s *Server) StartAndWait() error {
	if err := s.Start(); err != nil {
		return err
	}

	// Wait for signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("Received %v, shutting down...", sig)

	return s.Shutdown()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if s.httpSrv != nil {
		if err := s.httpSrv.Shutdown(ctx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}

// connContext adds per-connection tracking.
func (s *Server) connContext(ctx context.Context, c net.Conn) context.Context {
	addr := c.RemoteAddr().String()
	host, port, _ := net.SplitHostPort(addr)

	s.mu.Lock()
	s.connections[addr] = &ConnInfo{
		Address:     host,
		Port:        atoi(port),
		ConnectedAt: time.Now(),
	}
	s.mu.Unlock()

	// Clean up on disconnect
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		delete(s.connections, addr)
		s.mu.Unlock()
	}()

	return ctx
}

func atoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

// VFS returns the server's VFS tree.
func (s *Server) VFS() *vfs.Tree {
	return s.vfs
}

// Config returns the server's config.
func (s *Server) Config() *config.Config {
	return s.cfg
}

// Sessions returns the session store.
func (s *Server) Sessions() *auth.SessionStore {
	return s.sessions
}

// Stats returns current server statistics.
type Stats struct {
	Uptime          string `json:"uptime"`
	Connections     int    `json:"connections"`
	BytesSent       int64  `json:"bytes_sent"`
	BytesRecv       int64  `json:"bytes_recv"`
	HitsLogged      int64  `json:"hits_logged"`
	DownloadsLogged int64  `json:"downloads_logged"`
	UploadsLogged   int64  `json:"uploads_logged"`
}

// GetStats returns current server statistics.
func (s *Server) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	uptime := time.Since(s.startTime).Truncate(time.Second)
	return Stats{
		Uptime:          uptime.String(),
		Connections:     len(s.connections),
		BytesSent:       s.bytesSent,
		BytesRecv:       s.bytesRecv,
		HitsLogged:      s.hitsLogged,
		DownloadsLogged: s.downloadsLogged,
		UploadsLogged:   s.uploadsLogged,
	}
}

// GetConnections returns the list of active connections with computed speeds.
func (s *Server) GetConnections() []ConnInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	conns := make([]ConnInfo, 0, len(s.connections))
	for _, c := range s.connections {
		// Compute speed since last sample
		elapsed := now.Sub(c.lastSample).Seconds()
		if elapsed > 0.5 {
			c.Speed = int64(float64((c.BytesSent-c.lastSent)+(c.BytesRecv-c.lastRecv)) / elapsed)
			c.lastSent = c.BytesSent
			c.lastRecv = c.BytesRecv
			c.lastSample = now
		}
		conns = append(conns, *c)
	}
	return conns
}

// IsBanned checks if an IP is banned.
func (s *Server) IsBanned(addr string) (bool, string) {
	s.banMu.RLock()
	defer s.banMu.RUnlock()
	reason, ok := s.bannedIPs[addr]
	return ok, reason
}

func (s *Server) logEvent(format string, args ...interface{}) {
	if s.LogFn != nil {
		s.LogFn(fmt.Sprintf(format, args...))
	}
}

// BanIP adds an IP to the ban list.
func (s *Server) BanIP(addr, reason string) {
	s.banMu.Lock()
	s.bannedIPs[addr] = reason
	s.banMu.Unlock()
	s.cfg.AddBan(config.Ban{Address: addr, Reason: reason})
	s.cfg.Save()
}

// UnbanIP removes an IP from the ban list.
func (s *Server) UnbanIP(addr string) bool {
	s.banMu.Lock()
	_, ok := s.bannedIPs[addr]
	delete(s.bannedIPs, addr)
	s.banMu.Unlock()
	s.cfg.RemoveBan(addr)
	s.cfg.Save()
	return ok
}

// AddBytesSent tracks sent bytes.
func (s *Server) AddBytesSent(n int64) {
	s.mu.Lock()
	s.bytesSent += n
	s.mu.Unlock()
}

// AddBytesRecv tracks received bytes.
func (s *Server) AddBytesRecv(n int64) {
	s.mu.Lock()
	s.bytesRecv += n
	s.mu.Unlock()
}

// IncHits increments the hit counter.
func (s *Server) IncHits() {
	s.mu.Lock()
	s.hitsLogged++
	s.mu.Unlock()
}

// IncDownloads increments the download counter.
func (s *Server) IncDownloads() {
	s.mu.Lock()
	s.downloadsLogged++
	s.mu.Unlock()
}

// HandleFunc registers a custom HTTP handler on the server's mux.
func (s *Server) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if s.mux != nil {
		s.mux.HandleFunc(pattern, handler)
	}
}

// IncUploads increments the upload counter.
func (s *Server) IncUploads() {
	s.mu.Lock()
	s.uploadsLogged++
	s.mu.Unlock()
}
