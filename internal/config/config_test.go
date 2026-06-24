package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Auth.Realm != "HFS" {
		t.Errorf("default realm = %q, want HFS", cfg.Auth.Realm)
	}
}

func TestLoadSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := Defaults()
	cfg.filePath = path
	cfg.configDir = dir
	cfg.Server.Port = 9090

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if loaded.Server.Port != 9090 {
		t.Errorf("loaded port = %d, want 9090", loaded.Server.Port)
	}
}

func TestAccounts(t *testing.T) {
	cfg := Defaults()
	cfg.AddAccount(Account{
		Username:    "admin",
		Password:    "$2a$hash",
		Permissions: []string{"admin"},
		Enabled:     true,
	})

	found := cfg.FindAccount("admin")
	if found == nil {
		t.Fatal("account not found")
	}
	if !found.HasPermission("admin") {
		t.Error("admin should have admin permission")
	}
	// "admin" permission grants everything — this is by design
	if !found.HasPermission("upload") {
		t.Error("admin should have upload permission (admin grants all)")
	}

	cfg.RemoveAccount("admin")
	if cfg.FindAccount("admin") != nil {
		t.Error("account should be removed")
	}
}

func TestBans(t *testing.T) {
	cfg := Defaults()
	cfg.AddBan(Ban{Address: "192.168.1.1", Reason: "test"})

	banned, reason := cfg.IsBanned("192.168.1.1")
	if !banned {
		t.Error("IP should be banned")
	}
	if reason != "test" {
		t.Errorf("reason = %q, want test", reason)
	}

	cfg.RemoveBan("192.168.1.1")
	if banned, _ := cfg.IsBanned("192.168.1.1"); banned {
		t.Error("IP should not be banned anymore")
	}
}

func TestDefaultConfigCreation(t *testing.T) {
	// Ensure we can load without a file
	tmp := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmp)
	defer os.Unsetenv("XDG_CONFIG_HOME")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("default port = %d", cfg.Server.Port)
	}
}
