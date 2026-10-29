package main

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// mygoLicense is MyGo's license, which every copy of an app built with it
// carries.
//
//go:embed licenses/mygo.txt
var mygoLicense string

// acknowledgementsView thanks Bitwarden, whose desktop app Lense works
// with, and MyGo, which it is built with, with its license.
func (a *app) acknowledgementsView(c *ui.Context, k ink) {
	a.dragHandle(ui.Row(c).Height(48).Padding(0, 12).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(k.line)).Children(func() {
		letThrough(textButton(c.Key("back"), k, "Settings", iconBack)).OnClick(func() { a.page = pageSettings })
		ui.Text(c, "Acknowledgements").FontSize(14).FontWeight(650).Grow(1).TextAlign(ui.Center).Margin(0, 80, 0, 0)
	})
	view := a.stretchy(ui.Scroll(c.Key("acknowledgements")).Grow(1), &a.thanksBand)
	off := a.thanksBand.offset(c)
	a.thanksBand.barInsets(view, 0, 0, 0, 0)
	view.Children(func() {
		ui.Column(c).Padding(18, 28, 22, 28).Gap(22).Translate(0, off).Children(func() {
			section(c, k, "Bitwarden", func() {
				ui.Text(c, "Lense works with the Bitwarden® desktop app: it reads the vault the app keeps on your Mac, and asks it to unlock with Touch ID. Thanks to Bitwarden, whose open-source apps show how.").
					TextColor(k.muted).LineHeight(1.4)
				ui.Text(c, "Bitwarden is a trademark or registered trademark of Bitwarden, Inc. in the United States and/or other countries. Lense is not affiliated with or endorsed by Bitwarden, Inc.").
					FontSize(11).TextColor(k.faint).LineHeight(1.4)
				a.link(c.Key("bitwarden"), k, "bitwarden.com", "https://bitwarden.com")
			})
			section(c, k, "MyGo", func() {
				ui.Text(c, "Lense is built with MyGo, the framework for native apps in Go, under its MIT license:").
					TextColor(k.muted).LineHeight(1.4)
				ui.Text(c, mygoLicense).Font("monospace").FontSize(10.5).TextColor(k.muted).LineHeight(1.35).Selectable()
				a.link(c.Key("mygo"), k, "github.com/egoist/mygo", "https://github.com/egoist/mygo")
			})
		})
	})
}

// link is a button that opens a website in the browser.
func (a *app) link(c *ui.Context, k ink, label, url string) {
	textButton(c, k, label, iconExternal).AlignSelf(ui.Start).Margin(0, -8).Label("Open " + label).
		OnClick(func() { a.sys.openURL(url) })
}
