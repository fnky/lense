package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"lense/internal/bw"
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

	// Material is what the window shows behind its content: "solid", or a
	// material of macOS, which blurs what is behind the window.
	Material string `json:"material,omitempty"`
	// Tooltips name the buttons of an icon as the pointer rests on them.
	Tooltips bool `json:"tooltips"`
	// AllowScreenCapture lets screenshots and screen recordings show the
	// windows, which macOS leaves them out of otherwise.
	AllowScreenCapture bool `json:"allowScreenCapture"`
	// AutoUpdates checks for updates once a day, and installs them, for
	// the restart: the one thing that takes the app online, so it's off
	// until the user turns it on.
	AutoUpdates bool `json:"autoUpdates"`

	// Vault is the vault shown: "" all of them, "personal", or an
	// organization's ID.
	Vault  string     `json:"vault,omitempty"`
	Window placements `json:"window"`
	// Recent are the IDs of the items used last, the last first.
	Recent []string `json:"recent,omitempty"`
	// Keys are the keys the user gave actions, by the actions' IDs, in
	// place of their defaults.
	Keys map[string][]string `json:"keys,omitempty"`
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
		Material:       "popover",
		Tooltips:       true,
	}
}

// materials are the materials the window may show, with their names in
// the settings: solid, then macOS's, as mygo.Vibrancy names them.
var materials = []struct{ id, label string }{
	{"solid", "Solid"},
	{"popover", "Popover"},
	{"menu", "Menu"},
	{"hud", "HUD"},
	{"sidebar", "Sidebar"},
	{"under-window", "Under Window"},
}

// translucent reports whether the window shows a material.
func (c *config) translucent() bool { return c.Material != "" && c.Material != "solid" }

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

// adoptConfig gives the app in dir the settings it had under its former
// name, Bitwarden Mini, as it starts without its own: once, as they are.
// The former folder stays, for the user to remove.
func adoptConfig(dir string) {
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err == nil {
		return
	}
	former := strings.Replace(filepath.Base(dir), "Lense", "Bitwarden Mini", 1)
	if former == filepath.Base(dir) {
		return
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(dir), former, "config.json"))
	if err != nil || !json.Valid(b) {
		return
	}
	if os.MkdirAll(dir, 0o700) == nil {
		_ = os.WriteFile(filepath.Join(dir, "config.json"), b, 0o600)
	}
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
