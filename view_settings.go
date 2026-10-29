package main

import (
	"fmt"
	"slices"
	"time"

	"lense/internal/macos"
	"lense/internal/search"

	"github.com/egoist/mygo/ui"
)

// settingsView is the settings: the account, locking, the clipboard, the
// shortcut and filling.
func (a *app) settingsView(c *ui.Context, k ink) {
	a.dragHandle(ui.Row(c).Height(48).Padding(0, 12).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(k.line)).Children(func() {
		if a.configured() {
			letThrough(textButton(c.Key("back"), k, "Done", iconBack)).OnClick(func() { a.rec = nil; a.page = pageItems })
		}
		ui.Text(c, "Settings").FontSize(14).FontWeight(650).Grow(1).TextAlign(ui.Center).Margin(0, 60, 0, 0)
	})
	view := a.stretchy(ui.Scroll(c.Key("settings")).Grow(1), &a.settingsBand)
	off := a.settingsBand.offset(c)
	view.Children(func() {
		ui.Column(c).Padding(18, 28, 22, 28).Gap(22).Translate(0, off).Children(func() {
			a.softwareUpdateSection(c.Key("update"), k)
			section(c, k, "General", func() {
				atLogin := a.sys.openAtLogin()
				switchRow(c.Key("login"), k, "Launch Lense at login", "It waits in the menu bar from then on, for its shortcut.", &atLogin, func() {
					a.loginErr = ""
					if err := a.sys.setOpenAtLogin(atLogin); err != nil {
						a.loginErr = "macOS didn't let Lense change its login items: " + err.Error()
					}
				})
				if a.loginErr != "" {
					ui.Text(c, a.loginErr).FontSize(11.5).TextColor(k.danger).LineHeight(1.35)
				}
			})
			section(c, k, "Account", func() {
				settingRow(c, k, a.cfg.Email, a.accountStatus(), func() {
					pushButton(c.Key("change"), k, "Change…", false).OnClick(a.startSetup)
				})
				if a.vault != nil || a.sealed != nil {
					settingRow(c, k, "Lock now", "Forgets the vault, and the key Touch ID unlocks with, until you unlock it again.", func() {
						pushButton(c.Key("lock"), k, "Lock", false).OnClick(func() { a.lock(true); a.page = pageItems })
					})
				}
			})
			section(c, k, "Security", func() {
				minutes := []int{0, 1, 5, 10, 15, 30, 60, 240}
				labels := []string{"Never", "After 1 minute", "After 5 minutes", "After 10 minutes", "After 15 minutes", "After 30 minutes", "After 1 hour", "After 4 hours"}
				sel := labels[max(0, slices.Index(minutes, a.cfg.AutoLock))]
				settingRow(c, k, "Lock when idle", "When the Mac has had no input for this long.", func() {
					picker(c.Key("autolock"), k, &sel, labels).Width(170).OnChange(func() {
						a.cfg.AutoLock = minutes[slices.Index(labels, sel)]
						a.changed()
					})
				})
				switchRow(c.Key("sleep"), k, "Lock when the Mac sleeps", "Or when its screen locks.", &a.cfg.LockOnSleep, a.changed)
				detail := "After Lense locks itself, your fingerprint unlocks it until it quits."
				if !a.sys.biometrics() {
					detail = "This Mac has no Touch ID set up."
				}
				switchRow(c.Key("touch"), k, "Unlock with Touch ID", detail, &a.cfg.TouchID, a.changed)
				switchRow(c.Key("capture"), k, "Allow screen capture", "Lets screenshots and screen recordings show Lense, which macOS leaves out of them otherwise.",
					&a.cfg.AllowScreenCapture, func() {
						a.changed()
						a.applyCapture()
					})
			})
			section(c, k, "Appearance", func() {
				var labels []string
				sel := materials[0].label
				for _, m := range materials {
					labels = append(labels, m.label)
					if m.id == a.cfg.Material {
						sel = m.label
					}
				}
				settingRow(c, k, "Window material", "What shows through behind the window, blurred, as macOS's own panels do. Solid shows none.", func() {
					picker(c.Key("material"), k, &sel, labels).Width(170).OnChange(func() {
						for _, m := range materials {
							if m.label == sel {
								a.cfg.Material = m.id
							}
						}
						a.changed()
						a.applyMaterial()
					})
				})
				switchRow(c, k, "Show tooltips", "Names the buttons of an icon as the pointer rests on them.", &a.cfg.Tooltips, a.changed)
			})
			section(c, k, "Clipboard", func() {
				secs := []int{0, 30, 60, 90, 120, 300}
				labels := []string{"Never", "After 30 seconds", "After 1 minute", "After 90 seconds", "After 2 minutes", "After 5 minutes"}
				i := slices.Index(secs, a.cfg.ClearClipboard)
				if i < 0 {
					i = 3
				}
				sel := labels[i]
				settingRow(c, k, "Clear copied secrets", "Passwords and codes leave the clipboard, unless you copied something else since.", func() {
					picker(c.Key("clear"), k, &sel, labels).Width(170).OnChange(func() {
						a.cfg.ClearClipboard = secs[slices.Index(labels, sel)]
						a.changed()
					})
				})
			})
			section(c, k, "Filling", func() {
				trusted := a.sys.trusted(false)
				detail := fmt.Sprintf("%s types your username and password into the page you came from, when it's one of the login's websites, or into the app; elsewhere, and with %s, into the website it opens.",
					a.cfg.keyHint("default"), a.cfg.keyHint("open-fill"))
				settingRow(c, k, "Accessibility access", detail, func() {
					if trusted {
						ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, iconCheck).FontSize(13).TextColor(k.okay)
							ui.Text(c, "Allowed").FontSize(12).TextColor(k.muted)
						})
						return
					}
					pushButton(c.Key("allow"), k, "Allow…", false).OnClick(func() {
						a.sys.trusted(true)
						a.sys.openURL(macos.AccessibilitySettingsURL)
					})
				})
				if !trusted {
					c.After(2 * time.Second) // notice as it's allowed
				}
				a.uriMatchRow(c, k)
				switchRow(c.Key("totp"), k, "Copy the one-time password", "After filling a login that has one, for the page that asks for it next.", &a.cfg.CopyTOTP, a.changed)
			})
			a.shortcutsSection(c.Key("keys"), k)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(4, 0, 0, 0).FontSize(11).TextColor(k.faint).Children(func() {
				ui.Text(c, "Lense "+a.version).Grow(1)
				textButton(c.Key("thanks"), k, "Acknowledgements", nil).OnClick(func() { a.page = pageAcknowledgements })
				textButton(c.Key("quit"), k, "Quit Lense", nil).OnClick(a.quit)
			})
		})
	})
}

func (a *app) quit() {
	a.lock(true)
	a.exit()
}

// accountStatus says where the account is and how the vault stands.
func (a *app) accountStatus() string {
	s := a.cfg.Server.Host()
	if a.store != nil {
		if acc, ok := a.store.FindAccount(a.cfg.Email); ok && !acc.LastSync.IsZero() {
			s += " · Bitwarden synced " + ago(a.now(), acc.LastSync)
		}
	}
	return s
}

// uriMatches are the ways a login's websites may match a page, as
// Bitwarden's default URI match detection offers them.
var uriMatches = []struct {
	match int
	label string
}{
	{search.MatchDomain, "Base domain"},
	{search.MatchHost, "Host"},
	{search.MatchStartsWith, "Starts with"},
	{search.MatchExact, "Exact"},
	{search.MatchNever, "Never"},
}

// uriMatchRow chooses how a login's websites match a page where they
// don't say: for the suggestions, and the pages Lense fills.
func (a *app) uriMatchRow(c *ui.Context, k ink) {
	labels := make([]string, len(uriMatches))
	sel := uriMatches[0].label
	for i, m := range uriMatches {
		labels[i] = m.label
		if m.match == a.cfg.URIMatch {
			sel = m.label
		}
	}
	settingRow(c, k, "Default URI match detection", "How a login's websites match the page you're on, where they don't say: Lense suggests the logins it matches, and fills no other page. Base domain is recommended.", func() {
		picker(c.Key("urimatch"), k, &sel, labels).Width(170).OnChange(func() {
			a.cfg.URIMatch = uriMatches[slices.Index(labels, sel)].match
			a.changed()
			a.rebuild(true)
		})
	})
	if a.cfg.URIMatch == search.MatchStartsWith {
		ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, iconAlert).FontSize(13).TextColor(k.warn).Margin(1, 0, 0, 0)
			ui.Text(c, "'Starts with' is an advanced option with increased risk of exposing credentials.").
				FontSize(11.5).TextColor(k.warn).LineHeight(1.35).Shrink(1)
		})
	}
}

func section(c *ui.Context, k ink, title string, build func()) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(11).FontWeight(650).TextColor(k.faint)
		ui.Column(c).Gap(12).Padding(12, 14).Radius(10).Background(k.card).Border(1, k.line).Children(build)
	})
}

// settingRow is a setting: its name and what it does, and its control.
func settingRow(c *ui.Context, k ink, label, detail string, control func()) {
	ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label).SingleLine()
			if detail != "" {
				ui.Text(c, detail).FontSize(11.5).TextColor(k.muted).LineHeight(1.35)
			}
		})
		control()
	})
}

// switchRow is a setting turned on and off.
func switchRow(c *ui.Context, k ink, label, detail string, on *bool, changed func()) {
	settingRow(c, k, label, detail, func() {
		ui.Switch(c, on).Label(label).OnChange(changed)
	})
}

// ago says how long ago t was, in few words.
func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
	return "on " + t.Local().Format("Jan 2")
}
