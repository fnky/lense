package macos

import "testing"

func TestClaudeIsABrowserOfItsOwnPages(t *testing.T) {
	const claude = "com.anthropic.claudefordesktop"
	if !IsBrowser(claude) || browsers[claude] != "ax" {
		t.Fatal("Claude's browser pane isn't a browser's")
	}
	for u, own := range map[string]bool{
		"file:///Applications/Claude.app/Contents/Resources/app.asar/.vite/renderer/main_window/index.html": true,
		"https://claude.ai/new":             true,
		"https://claude.ai":                 true,
		"https://claude.ai.example.com/":    false,
		"https://github.com/login":          false,
		"http://127.0.0.1:47321/login.html": false,
	} {
		if got := ownPage(claude, u); got != own {
			t.Errorf("%s: own %v, want %v", u, got, own)
		}
	}
	if ownPage("company.thebrowser.dia", "https://claude.ai/new") {
		t.Error("a browser's page of claude.ai is its own")
	}
}
