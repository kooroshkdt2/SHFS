// Package config provides YAML configuration loading and management for HFS.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds all HFS configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	VFS      VFSConfig      `yaml:"vfs"`
	Auth     AuthConfig     `yaml:"auth"`
	Accounts []Account      `yaml:"accounts"`
	Log      LogConfig      `yaml:"log"`
	Layout   LayoutConfig   `yaml:"layout"`
	Bans     []Ban          `yaml:"bans"`

	mu       sync.RWMutex  `yaml:"-"`
	filePath string         `yaml:"-"`
	configDir string        `yaml:"-"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port               int `yaml:"port"`
	MaxConnections     int `yaml:"max_connections"`
	MaxConnectionsPerIP int `yaml:"max_connections_per_ip"`
	MaxBandwidthKbps   int `yaml:"max_bandwidth_kbps"`
	MaxDownloads       int `yaml:"max_downloads"`
	MaxDownloadsPerIP  int `yaml:"max_downloads_per_ip"`
}

// VFSConfig holds virtual file system settings.
type VFSConfig struct {
	Root            string `yaml:"root" json:"root"`                    // real root folder path
	TreeFile        string `yaml:"tree_file" json:"tree_file"`          // VFS persistence file name
	AnonymousUpload bool   `yaml:"anonymous_upload" json:"anonymous_upload"` // allow upload without auth
	UploadEnabled   bool   `yaml:"upload_enabled" json:"upload_enabled"`     // global upload on/off
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Realm          string        `yaml:"realm"`
	SessionTimeout time.Duration `yaml:"session_timeout"`
	DefaultAdmin   string        `yaml:"default_admin"`
}

// Account represents a user account.
type Account struct {
	Username    string   `yaml:"username" json:"username"`
	Password    string   `yaml:"password" json:"-"` // bcrypt hash
	Permissions []string `yaml:"permissions" json:"permissions"`
	Notes       string   `yaml:"notes,omitempty" json:"notes,omitempty"`
	Enabled     bool     `yaml:"enabled" json:"enabled"`
	IsGroup     bool     `yaml:"is_group,omitempty" json:"is_group,omitempty"`
	Redirect    string   `yaml:"redirect,omitempty" json:"redirect,omitempty"`
}

// Ban represents an IP ban entry.
type Ban struct {
	Address string `yaml:"address" json:"address"`
	Reason  string `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// LayoutConfig holds saved window layout.
type LayoutConfig struct {
	Width        int     `yaml:"width" json:"width"`
	Height       int     `yaml:"height" json:"height"`
	VSFSplit     float64 `yaml:"vfs_split" json:"vfs_split"`         // VFS panel / log panel (0.0-1.0)
	CenterSplit  float64 `yaml:"center_split" json:"center_split"`   // center / bottom
	BottomSplit  float64 `yaml:"bottom_split" json:"bottom_split"`   // connections / status
}

// LogConfig holds logging settings.
type LogConfig struct {
	File           string `yaml:"file"`
	ApacheFormat   bool   `yaml:"apache_format"`
	LogUploads     bool   `yaml:"log_uploads"`
	LogDownloads   bool   `yaml:"log_downloads"`
	LogConnections bool   `yaml:"log_connections"`
	LogRequests    bool   `yaml:"log_requests"`
	LogReplies     bool   `yaml:"log_replies"`
}

// Defaults returns a Config with sensible default values.
func Defaults() *Config {
	return &Config{
		Server: ServerConfig{
			Port:               8080,
			MaxConnections:     0,
			MaxConnectionsPerIP: 0,
			MaxBandwidthKbps:   0,
			MaxDownloads:       0,
			MaxDownloadsPerIP:  0,
		},
		VFS: VFSConfig{
			TreeFile:        "vfs.yaml",
			AnonymousUpload: false,
			UploadEnabled:   true,
		},
		Auth: AuthConfig{
			Realm:          "HFS",
			SessionTimeout: 24 * time.Hour,
		},
		Layout: LayoutConfig{
			Width:       900,
			Height:      600,
			VSFSplit:    0.4,
			CenterSplit: 0.70,
			BottomSplit: 0.82,
		},
		Log: LogConfig{
			LogUploads:     true,
			LogDownloads:   true,
			LogConnections: true,
			LogRequests:    true,
			LogReplies:     true,
		},
	}
}

// ConfigDir returns the configuration directory (./hfs-configs).
func ConfigDir() string {
	return "hfs-configs"
}

// Load reads configuration from the standard location.
// It creates a default config if none exists.
func Load() (*Config, error) {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	cfgPath := filepath.Join(dir, "config.yaml")
	return LoadFile(cfgPath)
}

// LoadFile reads configuration from a specific path.
func LoadFile(path string) (*Config, error) {
	cfg := Defaults()
	cfg.filePath = path
	cfg.configDir = filepath.Dir(path)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := cfg.Save(); err != nil {
				return cfg, fmt.Errorf("save defaults: %w", err)
			}
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	// Parse YAML, with Windows path fallback
	err = yaml.Unmarshal(data, cfg)
	if err != nil {
		// Windows paths like F:\folder break YAML because \ is an escape char.
		// Try pre-processing: double-up any lone backslashes.
		fixed := fixWindowsPaths(data)
		if err2 := yaml.Unmarshal(fixed, cfg); err2 != nil {
			return cfg, fmt.Errorf("parse config: %w (hint: use forward slashes or single quotes for Windows paths)", err)
		}
	}

	// Normalize all path fields after parsing (always replace backslashes)
	cfg.VFS.Root = strings.ReplaceAll(cfg.VFS.Root, "\\", "/")
	cfg.VFS.TreeFile = strings.ReplaceAll(cfg.VFS.TreeFile, "\\", "/")
	cfg.Log.File = strings.ReplaceAll(cfg.Log.File, "\\", "/")

	// Ensure defaults for zero values
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Auth.Realm == "" {
		cfg.Auth.Realm = "HFS"
	}
	if cfg.Auth.SessionTimeout == 0 {
		cfg.Auth.SessionTimeout = 24 * time.Hour
	}
	if cfg.VFS.TreeFile == "" {
		cfg.VFS.TreeFile = "vfs.yaml"
	}

	return cfg, nil
}

// fixWindowsPaths pre-processes YAML to handle Windows paths like F:\folder
// where backslash is treated as an escape character in double-quoted strings.
// Strategy: replace EVERY backslash with forward slash in quoted values.
// This is safe because forward slashes work on all OSes and never need escaping.
func fixWindowsPaths(data []byte) []byte {
	s := string(data)
	inQuotes := false
	quoteChar := byte(0)
	var result []byte

	for i := 0; i < len(s); i++ {
		c := s[i]

		// Track if we're inside a quoted string
		if (c == '"' || c == '\'') && (i == 0 || s[i-1] != '\\') {
			if !inQuotes {
				inQuotes = true
				quoteChar = c
			} else if c == quoteChar {
				inQuotes = false
			}
		}

		// Replace backslash with forward slash inside double-quoted strings
		if c == '\\' && inQuotes && i+1 < len(s) {
			next := s[i+1]
			// Keep valid YAML escapes as-is
			if next == '\\' || next == '"' || next == '\'' ||
				next == 'n' || next == 't' || next == 'r' ||
				next == '/' || next == ' ' {
				result = append(result, c)
				result = append(result, next)
				i++ // skip next char
				continue
			}
			// Replace Windows path backslash with forward slash
			result = append(result, '/')
			continue
		}

		result = append(result, c)
	}
	return result
}

// Save writes the current configuration to disk.
func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if c.filePath == "" {
		c.filePath = filepath.Join(c.configDir, "config.yaml")
	}

	if err := os.WriteFile(c.filePath, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// ConfigDir returns the directory containing the configuration file.
func (c *Config) GetConfigDir() string {
	return c.configDir
}

// AddAccount adds a new account to the configuration.
func (c *Config) AddAccount(acct Account) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Accounts = append(c.Accounts, acct)
	return nil
}

// RemoveAccount removes an account by username.
func (c *Config) RemoveAccount(username string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, a := range c.Accounts {
		if a.Username == username {
			c.Accounts = append(c.Accounts[:i], c.Accounts[i+1:]...)
			return true
		}
	}
	return false
}

// FindAccount returns an account by username, or nil.
func (c *Config) FindAccount(username string) *Account {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for i := range c.Accounts {
		if c.Accounts[i].Username == username {
			return &c.Accounts[i]
		}
	}
	return nil
}

// HasPermission checks if an account has a specific permission.
func (a *Account) HasPermission(perm string) bool {
	for _, p := range a.Permissions {
		if p == "admin" || p == perm {
			return true
		}
	}
	return false
}

// IsAdmin returns true if the account has admin permissions.
func (a *Account) IsAdmin() bool {
	return a.HasPermission("admin")
}

// AddBan adds an IP ban.
func (c *Config) AddBan(ban Ban) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Bans = append(c.Bans, ban)
}

// RemoveBan removes a ban by address.
func (c *Config) RemoveBan(address string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, b := range c.Bans {
		if b.Address == address {
			c.Bans = append(c.Bans[:i], c.Bans[i+1:]...)
			return true
		}
	}
	return false
}

// IsBanned checks if an address is banned.
func (c *Config) IsBanned(address string) (bool, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, b := range c.Bans {
		if b.Address == address {
			return true, b.Reason
		}
	}
	return false, ""
}
