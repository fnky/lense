package main

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// softwareUpdateSection shows how the app stands with its updates, as
// macOS's Software Update does: the version and what it's doing, the
// switch of automatic updates, and, without them, Check for Updates….
func (a *app) softwareUpdateSection(c *ui.Context, k ink) {
	section(c, k, "Software Update", func() {
		u := &a.update
		title, detail := "Lense "+a.version, ""
		switch {
		case !a.canUpdate():
			detail = "This copy of Lense can't update itself. Install new versions from github.com/fnky/lense."
		case u.state == updateChecking:
			title = "Checking for updates…"
		case u.state == updateUpToDate:
			title, detail = "Lense is up to date", "Lense "+a.version+" · checked "+ago(a.now(), u.checked)
		case u.state == updateAvailable:
			title, detail = "Lense "+u.found.version()+" is available", "You have Lense "+a.version+"."
		case u.state == updateDownloading:
			title, detail = "Downloading Lense "+u.found.version()+"…", fmt.Sprintf("%d%%", int(u.progress*100))
		case u.state == updateReady:
			title, detail = "Lense "+u.found.version()+" is ready", "It installs as Lense restarts."
		case u.state == updateFailed:
			title, detail = u.failed, u.err
		default:
			detail = "Not checked for updates yet."
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			markTile(c, 40)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, title).FontWeight(600)
				if u.state == updateDownloading {
					ui.Progress(c, u.progress).MaxWidth(240)
				}
				if detail != "" {
					ui.Text(c, detail).FontSize(11.5).TextColor(k.muted).LineHeight(1.35)
				}
			})
			if !a.canUpdate() {
				return
			}
			switch u.state {
			case updateChecking:
				ui.Spinner(c).Size(16, 16)
			case updateAvailable:
				primaryButton(c.Key("update-now"), k, "Update Now", false).OnClick(a.installUpdate)
			case updateReady:
				primaryButton(c.Key("restart"), k, "Restart Now", false).OnClick(a.restartToUpdate)
			case updateDownloading:
			default:
				if !a.cfg.AutoUpdates {
					pushButton(c.Key("check"), k, "Check for Updates…", false).OnClick(func() { a.checkForUpdates(false) })
				}
			}
		})
		if a.canUpdate() {
			switchRow(c.Key("autoupdate"), k, "Automatic updates",
				"Checks for updates once a day and downloads them, to install as Lense restarts. It's all Lense goes online for: to its releases on GitHub.",
				&a.cfg.AutoUpdates, func() {
					a.changed()
					a.autoUpdate()
				})
		}
	})
}
