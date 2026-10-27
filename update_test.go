package main

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

// fakeUpdater is the app's releases: it finds found, a version newer than
// the app's, none when "", or fails with checkErr; its update installs,
// or fails with installErr.
type fakeUpdater struct {
	mu                   sync.Mutex
	found                string
	checkErr, installErr error
	checks, installs     int
}

func (u *fakeUpdater) enabled() bool { return true }

func (u *fakeUpdater) check(context.Context) (availableUpdate, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checks++
	if u.checkErr != nil || u.found == "" {
		return nil, u.checkErr
	}
	return fakeUpdate{u, u.found}, nil
}

type fakeUpdate struct {
	u *fakeUpdater
	v string
}

func (f fakeUpdate) version() string { return f.v }

func (f fakeUpdate) install(_ context.Context, progress func(done, total int64)) error {
	f.u.mu.Lock()
	defer f.u.mu.Unlock()
	f.u.installs++
	progress(50, 100)
	if f.u.installErr != nil {
		return f.u.installErr
	}
	progress(100, 100)
	return nil
}

// settingsTop opens the settings, all of them in the window.
func settingsTop(h *harness) {
	h.t.Helper()
	h.tt.SetSize(windowWidth, 2400) // all of the settings, unscrolled
	h.key(ui.Cmd, ui.KeyComma)
}

func TestSoftwareUpdateComesFirst(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	settingsTop(h)
	h.has("Software Update", "Lense test", "Not checked for updates yet.", "Check for Updates…", "Automatic updates")
	upd, _ := h.tt.Find("Software Update")
	acc, _ := h.tt.Find("Account")
	if upd.Y >= acc.Y {
		t.Fatalf("Software Update at %v, below Account at %v", upd.Y, acc.Y)
	}
	if h.a.cfg.AutoUpdates || h.upd.checks != 0 {
		t.Fatalf("automatic updates %v, checks %d: Lense went online by itself", h.a.cfg.AutoUpdates, h.upd.checks)
	}
	snapshot(t, h.tt, "software-update")
}

func TestCheckForUpdatesByHand(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	settingsTop(h)
	// Up to date.
	h.click("Check for Updates…")
	h.has("Lense is up to date", "Lense test · checked just now", "Check for Updates…")
	// A newer version waits for the user, then downloads, for the restart.
	h.upd.found = "9.9.9"
	h.click("Check for Updates…")
	h.has("Lense 9.9.9 is available", "You have Lense test.", "Update Now")
	if h.upd.installs != 0 {
		t.Fatal("installed without asking")
	}
	snapshot(t, h.tt, "software-update-available")
	h.click("Update Now")
	h.has("Lense 9.9.9 is ready", "It installs as Lense restarts.", "Restart Now")
	if h.upd.installs != 1 || h.a.update.progress != 1 {
		t.Fatalf("installs %d, progress %v", h.upd.installs, h.a.update.progress)
	}
	// Ready, it checks no more.
	h.a.checkForUpdates(false)
	if h.upd.checks != 2 {
		t.Fatalf("checked again: %d", h.upd.checks)
	}
	h.click("Restart Now")
	if h.relaunched != 1 {
		t.Fatalf("relaunched %d", h.relaunched)
	}
}

func TestUpdatesThatFail(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	settingsTop(h)
	h.upd.checkErr = errors.New("no network")
	h.click("Check for Updates…")
	h.has("Couldn't check for updates", "no network", "Check for Updates…")
	h.upd.checkErr, h.upd.found, h.upd.installErr = nil, "9.9.9", errors.New("bad signature")
	h.click("Check for Updates…")
	h.click("Update Now")
	h.has("Couldn't download Lense 9.9.9", "bad signature", "Check for Updates…")
	if h.relaunched != 0 {
		t.Fatal("relaunched into a failed update")
	}
}

func TestAutomaticUpdates(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	settingsTop(h)
	// Off, they don't check.
	h.a.autoUpdate()
	if h.upd.checks != 0 {
		t.Fatalf("checked %d times with automatic updates off", h.upd.checks)
	}
	// Turned on, they check at once and install what they find, without
	// asking; Check for Updates… is for when they're off.
	var saved []config
	h.a.saveCfg = func(c config) { saved = append(saved, c) }
	h.upd.found = "9.9.9"
	// The switch at the end of the setting's row, beside its two lines.
	label, ok := h.tt.Find("Automatic updates")
	if !ok {
		t.Fatalf("no setting in %q", h.tt.Texts())
	}
	h.tt.ClickAt(windowWidth-28-18, label.Y+17)
	h.settle()
	if !h.a.cfg.AutoUpdates || len(saved) != 1 || !saved[0].AutoUpdates {
		t.Fatalf("automatic %v, saved %+v", h.a.cfg.AutoUpdates, saved)
	}
	if h.upd.checks != 1 || h.upd.installs != 1 {
		t.Fatalf("checks %d, installs %d", h.upd.checks, h.upd.installs)
	}
	h.has("Lense 9.9.9 is ready", "Restart Now")
	if h.tt.HasText("Check for Updates…") || h.tt.HasText("Update Now") {
		t.Error("buttons of updating by hand show with automatic updates on")
	}
	// Ready, the daily check leaves it be.
	h.a.autoUpdate()
	if h.upd.checks != 1 {
		t.Fatalf("checks %d", h.upd.checks)
	}
}

func TestCopiesThatCantUpdate(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.a.updEnabled = false
	settingsTop(h)
	h.has("Software Update", "This copy of Lense can't update itself. Install new versions from github.com/fnky/lense.")
	if h.tt.HasText("Check for Updates…") || h.tt.HasText("Automatic updates") {
		t.Error("offers updates it can't install")
	}
	h.a.cfg.AutoUpdates = true
	h.a.autoUpdate()
	if h.upd.checks != 0 {
		t.Fatalf("checked %d times", h.upd.checks)
	}
}
