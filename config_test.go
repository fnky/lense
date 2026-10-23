package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdoptsTheFormerConfig(t *testing.T) {
	root := t.TempDir()
	former := filepath.Join(root, "Bitwarden Mini Dev")
	if err := saveConfig(former, config{Email: "ada@example.com", Shortcut: "Cmd+Shift+K"}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "Lense Dev")
	adoptConfig(dir)
	if cfg := loadConfig(dir); cfg.Email != "ada@example.com" || cfg.Shortcut != "Cmd+Shift+K" {
		t.Fatalf("adopted %+v", cfg)
	}
	// Once: its own settings stay its own.
	if err := saveConfig(dir, config{Email: "grace@example.com"}); err != nil {
		t.Fatal(err)
	}
	adoptConfig(dir)
	if cfg := loadConfig(dir); cfg.Email != "grace@example.com" {
		t.Fatalf("adopted again: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(former, "config.json")); err != nil {
		t.Errorf("the former settings went: %v", err)
	}
	// Under another name, there's nothing to adopt.
	other := filepath.Join(root, "lense")
	adoptConfig(other)
	if _, err := os.Stat(filepath.Join(other, "config.json")); err == nil {
		t.Error("adopted settings from nowhere")
	}
}
