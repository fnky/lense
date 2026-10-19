package main

import (
	"fmt"
	"strings"
	"time"

	"bitwarden-mini/internal/bw"
	"bitwarden-mini/internal/search"
	"bitwarden-mini/internal/totp"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// itemsView is Quick Access: the search, the items found, and the
// details of the one chosen.
func (a *app) itemsView(c *ui.Context, k ink) {
	// The keys of Quick Access. The search takes the copying keys itself,
	// as it would edit with them: elsewhere, the window does.
	debugReg = !a.search.Focused(c)
	if !a.search.Focused(c) {
		c.OnShortcut(ui.Cmd, ui.KeyC, a.copyPrimary)
		c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyC, a.copyPassword)
		c.OnShortcut(ui.Cmd|ui.Alt, ui.KeyC, a.copyCode)
	}
	c.OnShortcut(ui.Cmd, ui.KeyO, a.openDetail)
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyO, a.openInBitwarden)
	c.OnShortcut(ui.Alt, ui.KeyEnter, func() { a.fill(a.selected(), true) })
	c.OnShortcut(ui.Cmd, ui.KeyF, func() { a.search.Focus() })

	ui.Column(c).Fill().Children(func() {
		a.header(c, k)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).MinHeight(0).Children(func() {
			a.itemList(c, k)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Children(func() {
				switch it := a.selected(); {
				case a.prob != nil:
					ui.Column(c).Padding(16).Children(func() { problemBox(c.Key("prob"), k, a.prob) })
				case it != nil:
					a.details(c, k, it, false)
				default:
					a.noItem(c, k)
				}
			})
		})
		a.footer(c, k)
	})
	if a.confirm != nil {
		a.confirmView(c, k)
	}
}

// header is the search, and the vault it searches.
func (a *app) header(c *ui.Context, k ink) {
	ui.Row(c).Height(56).Padding(0, 10, 0, 18).Gap(12).AlignItems(ui.Center).DragWindow().
		BorderWidth(0, 0, 1, 0).BorderColor(k.line).Children(func() {
		ui.Icon(c, iconSearch).FontSize(19).TextColor(k.muted)
		in := ui.TextInputBase(c.Key("search"), &a.query).Grow(1).Shrink(1).FontSize(19).
			Placeholder("Search in " + a.vaultName()).Label("Search").Bind(&a.search)
		if a.unlocked() {
			in.AutoFocus()
		}
		in.OnShortcut(0, ui.KeyDown, func() { a.move(1) })
		in.OnShortcut(0, ui.KeyUp, func() { a.move(-1) })
		in.OnShortcut(0, ui.KeyPageDown, func() { a.move(8) })
		in.OnShortcut(0, ui.KeyPageUp, func() { a.move(-8) })
		if a.caretEnd {
			n := len([]rune(a.query))
			in.SetTextSelection(n, n)
			a.caretEnd = false
		}
		if start, end := in.TextSelection(); start != end {
			a.searchSel = true
		} else {
			a.searchSel = false
		}
		in.HandleInput(a.searchKey)
		in.OnShortcut(ui.Cmd, ui.KeyDown, func() { a.move(len(a.rows)) })
		in.OnShortcut(ui.Cmd, ui.KeyUp, func() { a.move(-len(a.rows)) })
		in.OnShortcut(0, ui.KeyTab, func() {
			// Complete the filter chosen, as a shell completes.
			if a.sel >= 0 && a.sel < len(a.rows) && a.rows[a.sel].comp != nil {
				a.activate()
			}
		})
		in.OnChange(a.queryChanged)
		a.vaultPicker(c.Key("vaults"), k)
		iconButton(c.Key("settings"), k, iconSettings, "Settings (⌘,)").OnClick(a.openSettings)
	})
}

// searchKey takes the keys the search would otherwise edit with: Return,
// and Command-C but to copy what is selected of the search.
func (a *app) searchKey(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	switch {
	case ev.Key == ui.KeyEnter && ev.Mods == 0:
		a.activate()
	case ev.Key == ui.KeyC && ev.Mods == ui.Cmd && !a.searchSel:
		a.copyPrimary()
	case ev.Key == ui.KeyC && ev.Mods == ui.Cmd|ui.Shift:
		a.copyPassword()
	case ev.Key == ui.KeyC && ev.Mods == ui.Cmd|ui.Alt:
		a.copyCode()
	default:
		return false
	}
	return true
}

// vaultPicker chooses the vault to search: all, the personal one, or an
// organization's.
func (a *app) vaultPicker(c *ui.Context, k ink) {
	b := ui.ButtonBase(c).Padding(5, 8, 5, 9).Gap(6).Radius(8).AlignItems(ui.Center).
		FontSize(12).FontWeight(500).TextColor(k.muted).Label("Vault").Tooltip("Choose the vault to search")
	if b.Hovered() || b.Pressed() {
		b.Background(k.hover).TextColor(k.text)
	}
	b.Children(func() {
		ic := iconVault
		if a.cfg.Vault != "" && a.cfg.Vault != "personal" {
			ic = iconOrg
		}
		ui.Icon(c, ic).FontSize(14)
		ui.Text(c, a.vaultName()).NoWrap().MaxWidth(150).SingleLine()
		ui.Icon(c, iconChevron).FontSize(11)
	})
	b.Menu(func(m *ui.Menu) {
		if m.Item("All Vaults").Checked(a.cfg.Vault == "").Chosen() {
			a.chooseVault("")
		}
		m.Separator()
		if m.Item("My Vault").Checked(a.cfg.Vault == "personal").Chosen() {
			a.chooseVault("personal")
		}
		if a.vault != nil {
			for _, o := range a.vault.Organizations {
				if m.Item(o.Name).Checked(a.cfg.Vault == o.ID).Chosen() {
					a.chooseVault(o.ID)
				}
			}
		}
	})
}

// itemList lists the rows found.
func (a *app) itemList(c *ui.Context, k ink) {
	list := ui.List(c.Key("items"), &a.list, len(a.rows), func(i int) { a.rowView(c, k, i) }).
		Width(listWidth).Shrink(0).Padding(6, 8).Background(k.pane).BorderWidth(0, 1, 0, 0).BorderColor(k.line).Label("Items")
	list.Children(func() {
		if len(a.rows) > 0 {
			return
		}
		ui.Column(c).Padding(24, 16).Gap(4).AlignItems(ui.Center).Children(func() {
			if a.query == "" {
				ui.Text(c, "No items in "+a.vaultName()).FontWeight(600).TextAlign(ui.Center)
				return
			}
			ui.Text(c, "No results").FontWeight(600)
			q := search.Parse(a.query)
			switch {
			case len(q.Bad) > 0:
				ui.Text(c, "“"+q.Bad[0]+"” is not a filter. Type = to see them.").FontSize(12).TextColor(k.muted).TextAlign(ui.Center).LineHeight(1.35)
			case a.cfg.Vault != "":
				ui.Text(c, "Only "+a.vaultName()+" is searched.").FontSize(12).TextColor(k.muted)
				textButton(c.Key("all"), k, "Search All Vaults", nil).OnClick(func() { a.chooseVault("") })
			}
		})
	})
}

func (a *app) rowView(c *ui.Context, k ink, i int) {
	r := a.rows[i]
	if r.header != "" {
		ui.Row(c).Padding(10, 10, 4, 10).Background(k.pane).Children(func() {
			ui.Text(c, strings.ToUpper(r.header)).FontSize(10.5).FontWeight(650).LetterSpacing(0.4).TextColor(k.faint)
		})
		return
	}
	chosen := i == a.sel
	b := ui.ButtonBase(c).Padding(7, 10).Gap(10).Radius(8).AlignItems(ui.Center).Selected(chosen)
	switch {
	case chosen:
		b.Background(k.selected)
	case b.Pressed():
		b.Background(k.press)
	case b.Hovered():
		b.Background(k.hover)
	}
	if b.Pressed() || b.Clicked() {
		a.choose(i)
	}
	if b.DoubleClicked() {
		a.activate()
	}
	title, sub := k.text, k.muted
	if chosen {
		title, sub = k.onAccent, k.selMuted
	}
	if r.comp != nil {
		b.Label(r.comp.Label).Children(func() {
			ui.Row(c).Size(28, 28).Radius(7).Center().Background(sub.Alpha(0.12)).Children(func() {
				ui.Icon(c, iconSearch).FontSize(14).TextColor(sub)
			})
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, r.comp.Label).FontWeight(500).TextColor(title).SingleLine()
				ui.Text(c, r.comp.Detail).FontSize(11.5).TextColor(sub).SingleLine()
			})
		})
		return
	}
	it := r.item
	b.Label(it.Name).Children(func() {
		avatar(c, k, it.Name, int(it.Type), 30)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, it.Name).FontWeight(500).TextColor(title).SingleLine()
			if s := subtitle(it); s != "" {
				ui.Text(c, s).FontSize(11.5).TextColor(sub).SingleLine()
			}
		})
		if it.Favorite {
			ui.Icon(c, iconStar).FontSize(11).TextColor(sub)
		}
	})
}

// subtitle is the line under an item's name.
func subtitle(it *bw.Item) string {
	switch {
	case it.Login != nil && it.Login.Username != "":
		return it.Login.Username
	case it.Login != nil && len(it.Login.URIs) > 0:
		return search.Host(it.Login.URIs[0].URI)
	case it.Card != nil:
		s := it.Card.Brand
		if n := len(it.Card.Number); n >= 4 {
			s = strings.TrimSpace(s + " •••• " + it.Card.Number[n-4:])
		}
		return s
	case it.Identity != nil:
		return it.Identity.FullName()
	case it.SSHKey != nil:
		return it.SSHKey.Fingerprint
	case it.Type == bw.TypeSecureNote:
		line, _, _ := strings.Cut(it.Notes, "\n")
		return line
	}
	return it.Type.String()
}

func (a *app) noItem(c *ui.Context, k ink) {
	ui.Column(c).Fill().Center().Gap(6).Padding(24).Children(func() {
		ui.Icon(c, iconShield).FontSize(36).TextColor(k.text.Alpha(0.12))
		ui.Text(c, "Type to search your vault").TextColor(k.muted)
		ui.Text(c, "Filters: =favorite =tag: =category: =vault: =passkey =location: =untagged").
			FontSize(11.5).TextColor(k.faint).TextAlign(ui.Center).LineHeight(1.4).MaxWidth(320)
	})
}

// footer shows what the keys do, or the message of what just happened.
func (a *app) footer(c *ui.Context, k ink) {
	ui.Row(c).Height(36).Padding(0, 14).Gap(14).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(k.line).
		Background(k.pane).Children(func() {
		if a.flashing(c) {
			ui.Icon(c, iconCheck).FontSize(13).TextColor(k.okay)
			ui.Text(c, a.flash).FontSize(12).TextColor(k.text).SingleLine().Shrink(1)
			return
		}
		it := a.selected()
		switch {
		case it == nil && a.sel >= 0:
			keycap(c, k, "↩", "Use filter")
		case it == nil:
			keycap(c, k, "esc", "Close")
		default:
			if it.Login != nil && it.Login.Password != "" && a.front.app.PID != 0 {
				name := a.front.app.Name
				if name == "" {
					name = "app"
				}
				keycap(c, k, "↩", "Fill in "+name)
			}
			if f, ok := primary(it); ok {
				keycap(c, k, "⌘C", "Copy "+f.label)
			}
			if f, ok := secretOf(it); ok {
				keycap(c, k, "⇧⌘C", "Copy "+f.label)
			}
			if _, _, ok := a.code(it); ok {
				keycap(c, k, "⌥⌘C", "Copy code")
			}
		}
		ui.Spacer(c)
		if it != nil {
			textButton(c.Key("open-bw"), k, "Open in Bitwarden", iconExternal).Tooltip("⇧⌘O").OnClick(a.openInBitwarden)
		}
	})
}

// details shows the fields of an item, which a click copies, a drag drops
// into another app, and Return or Space copy from the keyboard. In its own
// window, own is true.
func (a *app) details(c *ui.Context, k ink, it *bw.Item, own bool) {
	reveal := a.reveal[it.ID] || c.Modifiers()&ui.Alt != 0 && !it.Reprompt
	ui.Scroll(c.Key("detail-"+it.ID)).Fill().Padding(18, 18, 16, 18).Gap(16).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			avatar(c, k, it.Name, int(it.Type), 44)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, it.Name).FontSize(17).FontWeight(650).SingleLine().Shrink(1)
					if it.Favorite {
						ui.Icon(c, iconStar).FontSize(13).TextColor(k.warn)
					}
				})
				ui.Text(c, a.where(it)).FontSize(12).TextColor(k.muted).SingleLine()
			})
			if !own {
				iconButton(c.Key("window"), k, iconExternal, "Open in Window (⌘O)").OnClick(func() { a.sys.openDetail(it.ID) })
			}
		})
		if it.Err != nil {
			problemBox(c.Key("err"), k, &problem{title: "Part of this item can't be decrypted", detail: it.Err.Error()})
		}
		fields := a.fieldsOf(it)
		if len(fields) > 0 {
			group(c.Key("fields"), k, func() {
				for i, f := range fields {
					a.fieldRow(c.Key(i), k, it, f, reveal, i > 0)
				}
			})
		}
		if l := it.Login; l != nil && len(l.Passkeys) > 0 {
			group(c.Key("passkeys"), k, func() {
				for i, p := range l.Passkeys {
					ui.Row(c.Key(i)).Padding(10, 12).Gap(10).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, iconPasskey).FontSize(16).TextColor(k.accent)
						ui.Column(c).Gap(1).Grow(1).Children(func() {
							ui.Text(c, "passkey").FontSize(11).TextColor(k.muted)
							name := p.RpID
							if p.UserName != "" {
								name = p.UserName + " · " + p.RpID
							}
							ui.Text(c, name).SingleLine()
						})
						if !p.Created.IsZero() {
							ui.Text(c, "Created "+p.Created.Format("Jan 2, 2006")).FontSize(11).TextColor(k.faint)
						}
					})
				}
			})
		}
		if it.Notes != "" && it.Type != bw.TypeSecureNote {
			group(c.Key("notes"), k, func() {
				a.fieldRow(c.Key("n"), k, it, field{label: "notes", value: it.Notes, kind: "note"}, reveal, false)
			})
		}
		ui.Text(c, "Modified "+it.Revised.Local().Format("January 2, 2006 at 15:04")).FontSize(11).TextColor(k.faint).AlignSelf(ui.Center)
	})
}

// where says what an item is and where it is kept.
func (a *app) where(it *bw.Item) string {
	parts := []string{it.Type.String()}
	if a.vault == nil {
		return parts[0]
	}
	if it.OrganizationID != "" {
		parts = append(parts, a.vault.OrganizationName(it.OrganizationID))
		for _, id := range it.CollectionIDs {
			if n := a.vault.CollectionName(id); n != "" {
				parts = append(parts, n)
				break
			}
		}
	} else {
		parts = append(parts, "My Vault")
	}
	if n := a.vault.FolderName(it.FolderID); n != "" {
		parts = append(parts, n)
	}
	return strings.Join(parts, " · ")
}

// group is a card of fields.
func group(c *ui.Context, k ink, fn func()) {
	ui.Column(c).Radius(10).Background(k.card).Border(1, k.line).Clip().Children(fn)
}

// fieldsOf lists the fields an item shows, in order.
func (a *app) fieldsOf(it *bw.Item) []field {
	var fs []field
	add := func(label, value string, secret bool) {
		if value != "" {
			fs = append(fs, field{label: label, value: value, secret: secret})
		}
	}
	if l := it.Login; l != nil {
		add("username", l.Username, false)
		add("password", l.Password, true)
		if _, _, ok := a.code(it); ok {
			fs = append(fs, field{label: "one-time password", kind: "totp", secret: true})
		}
		for _, u := range l.URIs {
			if u.URI != "" {
				fs = append(fs, field{label: "website", value: u.URI, kind: "uri", uri: u.URI})
			}
		}
	}
	if cd := it.Card; cd != nil {
		add("cardholder name", cd.CardholderName, false)
		add("brand", cd.Brand, false)
		add("number", cd.Number, true)
		if cd.ExpMonth != "" || cd.ExpYear != "" {
			add("expiration", fmt.Sprintf("%02s / %s", cd.ExpMonth, cd.ExpYear), false)
		}
		add("security code", cd.Code, true)
	}
	if id := it.Identity; id != nil {
		add("name", id.FullName(), false)
		add("email", id.Email, false)
		add("phone", id.Phone, false)
		add("username", id.Username, false)
		add("company", id.Company, false)
		var addr []string
		for _, p := range []string{id.Address1, id.Address2, id.Address3, strings.TrimSpace(id.PostalCode + " " + id.City), id.State, id.Country} {
			if strings.TrimSpace(p) != "" {
				addr = append(addr, p)
			}
		}
		if len(addr) > 0 {
			fs = append(fs, field{label: "address", value: strings.Join(addr, "\n"), kind: "note"})
		}
		add("social security number", id.SSN, true)
		add("passport number", id.PassportNumber, true)
		add("license number", id.LicenseNumber, true)
	}
	if k := it.SSHKey; k != nil {
		add("public key", k.PublicKey, false)
		add("fingerprint", k.Fingerprint, false)
		add("private key", k.PrivateKey, true)
	}
	if it.Type == bw.TypeSecureNote && it.Notes != "" {
		fs = append(fs, field{label: "note", value: it.Notes, kind: "note"})
	}
	for _, f := range it.Fields {
		switch f.Type {
		case bw.FieldBoolean:
			v := "No"
			if f.Value == "true" {
				v = "Yes"
			}
			add(f.Name, v, false)
		case bw.FieldHidden:
			add(f.Name, f.Value, true)
		case bw.FieldText:
			add(f.Name, f.Value, false)
		}
	}
	return fs
}

// fieldRow is a field of an item: its label over its value. A click
// copies it; it drags into the fields of other apps.
func (a *app) fieldRow(c *ui.Context, k ink, it *bw.Item, f field, reveal, divider bool) {
	if f.kind == "totp" {
		code, left, ok := a.code(it)
		if !ok {
			return
		}
		f.value = code
		c.After(left%time.Second + 10*time.Millisecond)
	}
	shown := f.value
	switch {
	case f.secret && f.kind != "totp" && !reveal:
		shown = strings.Repeat("•", min(max(len([]rune(f.value)), 8), 16))
	case f.kind == "totp":
		shown = totp.Format(f.value)
	case f.label == "number" && it.Card != nil:
		shown = cardNumber(f.value)
	}
	row := ui.ButtonBase(c).Padding(9, 12).Gap(10).AlignItems(ui.Center).Label(f.label).
		Tooltip("Click to copy, or drag into a field")
	if divider {
		row.BorderWidth(1, 0, 0, 0).BorderColor(k.line)
	}
	if row.Hovered() || row.Focused() {
		row.Background(k.hover)
	}
	if !f.secret || reveal || f.kind == "totp" || !it.Reprompt {
		// Drop the value into the username and password fields of other
		// apps: the item asks for nothing more first.
		row.DragData(transfer.TextData(f.value))
	}
	row.OnClick(func() { a.copyField(it, f) })
	row.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, f.label).FontSize(11).TextColor(k.muted).SingleLine()
			val := ui.Text(c, shown)
			switch {
			case f.kind == "note":
				val.LineHeight(1.4).Wrap()
			case f.kind == "totp":
				val.Font("monospace").FontSize(15).FontWeight(600).FontFeatures("tnum").LetterSpacing(0.5)
			case f.secret:
				val.Font("monospace").SingleLine()
			case f.kind == "uri":
				val.TextColor(k.accent).SingleLine()
			default:
				val.SingleLine()
			}
		})
		if f.kind == "totp" {
			_, left, _ := a.code(it)
			countdown(c, k, left, 30)
		}
		hovered := row.Hovered() || row.Focused()
		if f.secret && f.kind != "totp" {
			ic, label := iconEye, "Reveal"
			if reveal {
				ic, label = iconEyeOff, "Conceal"
			}
			iconButton(c.Key("reveal"), k, ic, label+" (hold ⌥)").OnClick(func() {
				if reveal {
					delete(a.reveal, it.ID)
					return
				}
				a.guarded(it, func() { a.reveal[it.ID] = true })
			})
		}
		if f.kind == "uri" {
			u := f.uri
			if !strings.Contains(u, "://") {
				u = "https://" + u
			}
			iconButton(c.Key("open"), k, iconExternal, "Open").OnClick(func() { a.sys.openURL(u) })
		}
		cp := iconButton(c.Key("copy"), k, iconCopy, "Copy").OnClick(func() { a.copyField(it, f) })
		if !hovered {
			cp.Opacity(0)
		}
	})
}

// cardNumber spaces a card's number in groups of four.
func cardNumber(n string) string {
	var b strings.Builder
	for i, r := range strings.ReplaceAll(n, " ", "") {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// countdown is a ring that empties as a one-time password expires.
func countdown(c *ui.Context, k ink, left time.Duration, period float32) {
	frac := float32(left.Seconds()) / period
	color := k.accent
	if left < 6*time.Second {
		color = k.danger
	}
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Text(c, fmt.Sprintf("%ds", int(left.Seconds()+0.5))).FontSize(11).FontFeatures("tnum").TextColor(k.muted)
		track := k.line
		ui.Box(c).Size(16, 16).Draw(func(p *ui.Painter, r ui.Rect) {
			cx, cy := r.X+r.W/2, r.Y+r.H/2
			p.StrokePath((&ui.Path{}).Circle(cx, cy, 6.5), 2.5, track)
			p.StrokePath((&ui.Path{}).Circle(cx, cy, 6.5).Trim(0, frac), 2.5, color)
		})
	})
}

// confirmView asks for the master password before showing or using the
// secrets of an item that asks for it.
func (a *app) confirmView(c *ui.Context, k ink) {
	open := true
	ui.DialogBase(c.Key("confirm"), &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.25))
		panel.Width(340).Padding(18).Gap(12).Radius(14).Background(k.bg).Shadow(0, 8, 30, 0, ui.RGBA(0, 0, 0, 0.3))
		panel.Children(func() {
			ui.Text(c, "Enter your master password").FontSize(15).FontWeight(650)
			name := ""
			if a.vault != nil {
				if it := a.vault.Item(a.confirmItem); it != nil {
					name = it.Name
				}
			}
			ui.Text(c, name+" asks for your master password before showing its secrets.").FontSize(12).TextColor(k.muted).LineHeight(1.4)
			ui.TextInput(c.Key("pw"), &a.confirmPw).Password().Placeholder("Master password").Label("Master password").
				Bind(&a.confirmH).AutoFocus().Disabled(a.busy != "").OnSubmit(a.confirmPassword)
			if a.confirmErr != "" {
				ui.Text(c, a.confirmErr).FontSize(12).TextColor(k.danger)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c.Key("cancel"), "Cancel").OnClick(a.closeConfirm)
				ui.PrimaryButton(c.Key("ok"), "Continue").Disabled(a.confirmPw == "" || a.busy != "").OnClick(a.confirmPassword)
			})
		})
	})
	if !open {
		a.closeConfirm()
	}
}

// detailWindow is an item in a window of its own, which ⌘O opens.
func (a *app) detailWindow(c *ui.Context, id string) {
	c.SetTheme(theme(c.Theme()))
	k := inkOf(c.Theme())
	c.Root().Background(k.bg).FontSize(13).TextColor(k.text)
	var it *bw.Item
	if a.unlocked() {
		it = a.vault.Item(id)
	}
	if it == nil {
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, "This item is no longer in your vault.").TextColor(k.muted)
		})
		return
	}
	c.OnShortcut(ui.Cmd, ui.KeyC, func() {
		if f, ok := primary(it); ok {
			a.copyField(it, f)
		}
	})
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyC, func() {
		if f, ok := secretOf(it); ok {
			a.copyField(it, f)
		}
	})
	c.OnShortcut(ui.Cmd|ui.Alt, ui.KeyC, func() {
		if code, _, ok := a.code(it); ok {
			a.copyField(it, field{label: "one-time password", value: code, secret: true})
		}
	})
	a.details(c, k, it, true)
	if a.confirm != nil && a.confirmItem == id {
		a.confirmView(c, k)
	}
}

var debugReg bool
