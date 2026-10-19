package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"bitwarden-mini/internal/bw"
)

// defaultShortcut opens the window from anywhere.
const defaultShortcut = "Cmd+Shift+Space"

// config is what the app remembers between launches. It holds no secret:
// the vault and its keys stay with the Bitwarden desktop app.
type config struct {
	// The account, as the user gave it, and its server.
	Email  string    `json:"email,omitempty"`
	Server bw.Server `json:"server"`

	Shortcut string `json:"shortcut,omitempty"`
	// AutoLock is how many idle minutes lock the app; 0 never.
	AutoLock int `json:"autoLock"`
	// TouchID lets Touch ID unlock the app after it locked itself, until
	// it quits, without asking the desktop app.
	TouchID bool `json:"touchId"`
	// LockOnSleep locks as the Mac sleeps or its screen locks.
	LockOnSleep bool `json:"lockOnSleep"`
	// ClearClipboard is how many seconds copied secrets stay on the
	// clipboard; 0 forever.
	ClearClipboard int `json:"clearClipboard"`

	// Vault is the vault shown: "" all of them, "personal", or an
	// organization's ID.
	Vault  string     `json:"vault,omitempty"`
	Window placements `json:"window"`
	// Recent are the IDs of the items used last, the last first.
	Recent []string `json:"recent,omitempty"`
	// AppID names this app to the desktop app, as its browser extensions
	// name themselves.
	AppID string `json:"appId,omitempty"`
}

func defaultConfig() config {
	return config{
		Server:         bw.Server{Region: bw.RegionUS},
		Shortcut:       defaultShortcut,
		AutoLock:       10,
		TouchID:        true,
		LockOnSleep:    true,
		ClearClipboard: 90,
	}
}

func loadConfig(dir string) config {
	cfg := defaultConfig()
	if b, err := os.ReadFile(filepath.Join(dir, "config.json")); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.Shortcut == "" {
		cfg.Shortcut = defaultShortcut
	}
	if cfg.Server.Region == "" {
		cfg.Server.Region = bw.RegionUS
	}
	return cfg
}

func saveConfig(dir string, cfg config) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "config.json.tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "config.json"))
}

// used records that the user used an item, for the recent ones.
func (c *config) used(id string) {
	recent := []string{id}
	for _, r := range c.Recent {
		if r != id && len(recent) < 12 {
			recent = append(recent, r)
		}
	}
	c.Recent = recent
}
