package main

import (
	"bitwarden-mini/internal/bw"

	"github.com/egoist/mygo/ui"
)

// lockedView unlocks the vault: with Touch ID, of this Mac or through the
// Bitwarden app, or with the master password, once the desktop app's data
// is read: until then, its buttons wait.
func (a *app) lockedView(c *ui.Context, k ink) {
	loading := !a.loaded
	a.dragHandle(ui.Row(c).Height(44).Padding(0, 10).AlignItems(ui.Center)).Children(func() {
		ui.Spacer(c)
		letThrough(iconButton(c.Key("settings"), k, iconSettings, "Settings (⌘,)")).OnClick(a.openSettings)
	})
	ui.Column(c).Grow(1).Center().Padding(0, 0, 44, 0).Children(func() {
		ui.Column(c).Width(340).Gap(14).AlignItems(ui.Center).Children(func() {
			shieldTile(c, k, 56)
			ui.Column(c).Gap(3).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "Bitwarden Mini is locked").FontSize(17).FontWeight(650)
				ui.Text(c, a.cfg.Email+" · "+a.cfg.Server.Host()).FontSize(12).TextColor(k.muted).SingleLine()
			})
			switch {
			case loading:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(14, 14)
					ui.Text(c, "Loading…").FontSize(12).TextColor(k.muted)
				})
			case a.lockProb != nil:
				problemBox(c.Key("prob"), k, a.lockProb).AlignSelf(ui.Stretch)
			}
			if a.touchIDOffered() {
				label := "Unlock with Touch ID"
				switch {
				case a.opening:
					label = "Opening Bitwarden…"
				case a.busy == "touch" && a.sealed == nil:
					label = "Confirm in Bitwarden…"
				case a.busy == "touch":
					label = "Waiting for Touch ID…"
				}
				primaryButton(c.Key("touch"), k, label, a.busy != "" || loading).AlignSelf(ui.Stretch).Padding(7, 12).
					OnClick(func() { a.unlockTouchID(true) })
			}
			// The button as tall as the field.
			ui.Row(c).Gap(8).AlignSelf(ui.Stretch).AlignItems(ui.Stretch).Children(func() {
				in := ui.TextInput(c.Key("password"), &a.password).Password().Grow(1).
					Placeholder("Master password").Label("Master password").Bind(&a.pwField).
					Disabled(a.busy == "password").OnSubmit(a.unlockPassword)
				if !a.touchIDOffered() {
					in.AutoFocus()
				}
				if a.busy == "password" {
					ui.Spinner(c).Size(16, 16).AlignSelf(ui.Center)
				} else {
					pushButton(c.Key("unlock"), k, "Unlock", a.password == "" || a.busy != "" || loading).OnClick(a.unlockPassword)
				}
			})
			ui.Text(c, "Your master password unlocks the vault on this Mac, without going online.").
				FontSize(11).TextColor(k.faint).TextAlign(ui.Center).LineHeight(1.35)
		})
	})
}

// setupView chooses the account: the email it signs in with, and its
// server, which must be those of an account of the Bitwarden app.
func (a *app) setupView(c *ui.Context, k ink) {
	a.dragHandle(ui.Row(c).Height(44).Padding(0, 10).AlignItems(ui.Center)).Children(func() {
		if a.configured() {
			letThrough(textButton(c.Key("back"), k, "Settings", iconBack)).OnClick(func() { a.page = pageSettings })
		}
		ui.Spacer(c)
	})
	ui.Row(c).Grow(1).Padding(0, 48, 28, 48).Gap(40).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Width(250).Gap(12).Children(func() {
			shieldTile(c, k, 52)
			ui.Text(c, "Connect to Bitwarden").FontSize(20).FontWeight(650)
			ui.Text(c, "Bitwarden Mini searches the vault the Bitwarden app keeps on this Mac, and asks the app to unlock it with Touch ID. It never connects to Bitwarden's servers itself.").
				FontSize(12.5).TextColor(k.muted).LineHeight(1.45)
			if a.store != nil {
				accs := a.store.Accounts()
				if len(accs) > 0 {
					ui.Text(c, "Signed in to Bitwarden").FontSize(11).FontWeight(600).TextColor(k.faint).Margin(8, 0, 0, 0)
					for _, acc := range accs {
						a.accountChoice(c.Key(acc.ID), k, acc)
					}
				}
			}
		})
		ui.Column(c).Grow(1).Gap(14).Children(func() {
			ui.Field(c, "Email", func() {
				ui.TextInput(c.Key("email"), &a.setupEmail).Placeholder("you@example.com").Label("Email").
					AutoFocus().Disabled(a.connecting).OnSubmit(a.connect)
			})
			ui.Field(c, "Server", func() {
				ui.Segmented(c.Key("region"), &a.setupRegion, "bitwarden.com", "bitwarden.eu", "Self-hosted").Label("Server").
					OnChange(func() { a.setupProb = nil })
			})
			if a.setupRegion == 2 {
				ui.Field(c, "Server address", func() {
					ui.TextInput(c.Key("url"), &a.setupURL).Placeholder("https://vault.example.com").Label("Server address").
						Disabled(a.connecting).OnSubmit(a.connect)
				})
			}
			if a.setupProb != nil {
				problemBox(c.Key("prob"), k, a.setupProb)
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				busy := a.connecting || !a.loaded // or the desktop app's data loading
				primaryButton(c.Key("continue"), k, "Continue", busy).OnClick(a.connect)
				if busy {
					ui.Spinner(c).Size(14, 14)
				}
			})
		})
	})
}

// accountChoice fills the setup with an account of the desktop app.
func (a *app) accountChoice(c *ui.Context, k ink, acc bw.Account) {
	b := ui.ButtonBase(c).Padding(6, 8).Gap(8).Radius(8).AlignItems(ui.Center).Justify(ui.Start).Margin(0, -8).Label(acc.Email)
	if b.Hovered() {
		b.Background(k.hover)
	}
	b.OnClick(func() {
		a.setupEmail, a.setupRegion, a.setupURL, a.setupProb = acc.Email, regionIndex(acc.Server), acc.Server.URL, nil
	})
	b.Children(func() {
		avatar(c, k, acc.Email, 1, 24)
		ui.Column(c).Shrink(1).MinWidth(0).Children(func() {
			ui.Text(c, acc.Email).FontSize(12).SingleLine()
			ui.Text(c, acc.Server.Host()).FontSize(11).TextColor(k.muted).SingleLine()
		})
	})
}
