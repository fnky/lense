package main

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"lense/internal/bw"
	"lense/internal/search"
	"lense/internal/totp"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// footerHeight is the height of the footer, which the content scrolls
// under.
const footerHeight = 36

// itemsView is Quick Access: the search, the items found, and the
// details of the one chosen. The search keeps the keyboard: the keys act
// on the results, the fields of the item or the vaults, as the mode says.
func (a *app) itemsView(c *ui.Context, k ink) {
	ui.Column(c).Fill().Children(func() {
		a.header(c, k)
		// The list and the details go down to the window's bottom, and
		// scroll under the footer, which floats over them.
		a.listOff = a.listBand.offset(c)
		ui.Box(c.Key("body")).Grow(1).MinHeight(0).Children(func() {
			ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
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
	})
	if a.mode == modeVaults {
		a.vaultMenu(c, k)
	}
	if a.large != nil {
		a.largeTypeView(c, k)
	}
	if a.confirm != nil {
		a.confirmView(c, k)
	}
}

// header is the search, and the vault it searches.
func (a *app) header(c *ui.Context, k ink) {
	a.dragHandle(ui.Row(c).Height(56).Padding(0, 10, 0, 18).Gap(12).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(k.line)).Children(func() {
		ui.Icon(c, iconSearch).FontSize(19).TextColor(k.muted)
		in := ui.TextInputBase(c.Key("search"), &a.query).Grow(1).Shrink(1).FontSize(19).
			Placeholder("Search in " + a.vaultName()).Label("Search").Bind(&a.search)
		if a.unlocked() {
			in.AutoFocus()
		}
		if a.caretEnd {
			n := len([]rune(a.query))
			in.SetTextSelection(n, n)
			a.caretEnd = false
		}
		start, end := in.TextSelection()
		a.searchSel, a.searchCaret = start != end, end
		in.HandleInput(a.searchKey)
		in.OnChange(a.queryChanged)
		a.vaultPicker(c.Key("vaults"), k)
		letThrough(iconButton(c.Key("settings"), k, iconSettings, "Settings ("+a.cfg.keyHint("settings")+")")).OnClick(a.openSettings)
	})
}

// vaultPicker shows the vault searched, and opens the vault selector.
func (a *app) vaultPicker(c *ui.Context, k ink) {
	b := letThrough(ui.ButtonBase(c).Padding(5, 8, 5, 9).Gap(6).Radius(8).AlignItems(ui.Center).
		FontSize(12).FontWeight(500).TextColor(k.muted).Label("Vault").
		Description("Choose the vault to search (" + a.cfg.keyHint("choose-vault") + ")"))
	if b.Hovered() || b.Pressed() || a.mode == modeVaults {
		b.Background(k.hover).TextColor(k.text)
	}
	b.OnClick(func() {
		a.openVaults()
		a.search.Focus()
	})
	b.Children(func() {
		ui.Icon(c, vaultIcon(a.cfg.Vault)).FontSize(14)
		ui.Text(c, a.vaultName()).NoWrap().MaxWidth(150).SingleLine()
		ui.Icon(c, iconChevron).FontSize(11)
	})
}

func vaultIcon(id string) *ui.SVG {
	switch {
	case id == "" || id == "personal":
		return iconVault
	case strings.HasPrefix(id, "folder:"):
		return iconFolder
	case strings.HasPrefix(id, "collection:"):
		return iconFolder
	}
	return iconOrg
}

// vaultMenu is the vault selector: the vaults, and their folders and
// collections, which the arrows go through and Return chooses.
func (a *app) vaultMenu(c *ui.Context, k ink) {
	// A press beside it puts it away.
	ui.Box(c.Key("vault-backdrop")).Absolute().Top(0).Left(0).Right(0).Bottom(0).OnClick(func() {
		a.setMode(modeResults)
		a.search.Focus()
	})
	panel := ui.Column(c.Key("vault-menu")).Absolute().Top(52).Right(46).Width(300).MaxHeight(windowHeight-100).
		Padding(6).Radius(12).Background(k.raised).Border(1, k.line).Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.25)).
		Label("Vaults")
	panel.Children(func() {
		view := a.stretchy(ui.Scroll(c.Key("vault-scroll")), &a.vaultBand)
		off := a.vaultBand.offset(c)
		view.Children(func() {
			ui.Column(c).Gap(1).Translate(0, off).Children(func() {
				for _, v := range a.visibleVaults() {
					a.vaultRow(c.Key("v:"+v.id), k, v)
				}
			})
		})
	})
}

func (a *app) vaultRow(c *ui.Context, k ink, v vaultChoice) {
	cursor := v.id == a.vaultCursor
	row := ui.Row(c).Padding(5, 8, 5, 4+float32(v.depth)*18).Gap(6).Radius(7).AlignItems(ui.Center).Label(v.name)
	title, sub := k.text, k.faint
	switch {
	case cursor:
		row.Background(k.selected)
	case row.Hovered():
		row.Background(k.hover)
	}
	if cursor {
		row.ScrollIntoView()
	}
	row.OnClick(func() {
		a.chooseVault(v.id)
		a.setMode(modeResults)
		a.search.Focus()
	})
	row.Children(func() {
		if v.kids {
			// The chevron shows or hides what the vault holds, as Right
			// and Left do, rather than choosing it.
			open := a.vaultOpen[v.id]
			label := "Expand " + v.name
			if open {
				label = "Collapse " + v.name
			}
			tw := ui.ButtonBase(c.Key("disclose")).Size(18, 18).Center().Radius(4).Label(label)
			if tw.Hovered() {
				tw.Background(k.hover)
			}
			tw.OnClick(func() {
				a.vaultOpen[v.id] = !open
				a.vaultCursor = v.id
				a.search.Focus()
			})
			tw.Children(func() {
				ic := ui.Icon(c, iconChevron).FontSize(11).TextColor(sub)
				if !open {
					ic.Rotate(-90)
				}
			})
		} else {
			ui.Box(c).Size(18, 18)
		}
		ui.Icon(c, vaultIcon(v.id)).FontSize(14).TextColor(title)
		ui.Text(c, v.name).TextColor(title).SingleLine().Grow(1).Shrink(1)
		if v.id == a.cfg.Vault {
			ui.Icon(c, iconCheck).FontSize(13).TextColor(title)
		}
		if v.num > 0 {
			if hint := a.cfg.keyHint(fmt.Sprintf("vault-%d", v.num)); hint != "" {
				ui.Text(c, hint).FontSize(11).TextColor(sub).NoWrap()
			}
		}
	})
}

// itemList lists the rows found.
func (a *app) itemList(c *ui.Context, k ink) {
	list := ui.List(c.Key("items"), &a.list, len(a.rows), func(i int) { a.rowView(c, k, i) }).
		Width(listWidth).Shrink(0).Padding(headerFade, 8, 6+footerHeight, 8).ScrollbarInsets(0, 0, footerHeight, 0).
		Background(k.pane).BorderWidth(0, 1, 0, 0).BorderColor(k.line).Label("Items")
	a.stretchy(list, &a.listBand)
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
				textButton(c.Key("all"), k, "Search All Vaults", nil).OnClick(func() { a.chooseVault(""); a.search.Focus() })
			}
		})
	})
}

func (a *app) rowView(c *ui.Context, k ink, i int) {
	r := a.rows[i]
	if r.header != "" {
		// The header of the section on top pins over the rows scrolling
		// under it, which it hides: with the pane's color, or over the
		// window's material, where it has none, by the rows fading out
		// under it (underHeader).
		head := ui.Row(c).Height(sectionHeight).Padding(10, 10, 4, 10)
		// Stretched past the end, the rows move up under the header pinned
		// over them, which stays.
		if a.listOff >= 0 || i != a.pinnedHeader() {
			head.Translate(0, a.listOff)
		}
		if !k.vibrant {
			head.Background(k.pane)
		}
		head.Children(func() {
			ui.Text(c, strings.ToUpper(r.header)).FontSize(10.5).FontWeight(650).LetterSpacing(0.4).TextColor(k.faint)
		})
		return
	}
	// The rows take the pointer, not the keyboard, which the search keeps,
	// clicked too: typing still searches.
	chosen := i == a.sel
	b := ui.Row(c).Padding(7, 10).Gap(10).Radius(8).AlignItems(ui.Center).Selected(chosen).Translate(0, a.listOff).KeepFocus()
	if k.vibrant && a.rows[0].header != "" {
		b.Effect(underHeader{})
	}
	inDetails := chosen && a.mode == modeDetails
	switch {
	case inDetails:
		b.Background(k.hover) // the keys are on its fields
	case chosen:
		b.Background(k.selected)
	case b.Pressed():
		b.Background(k.press)
	case b.Hovered():
		b.Background(k.hover)
	}
	if b.Pressed() || b.Clicked() {
		if i != a.sel {
			a.setMode(modeResults)
		}
		a.choose(i)
	}
	if b.DoubleClicked() {
		a.activate()
	}
	title, sub := k.text, k.muted
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

// pinnedHeader returns the row of the header pinned over the list's top in
// the frame before, the section's of the first row showing: -1 for none.
func (a *app) pinnedHeader() int {
	first, _ := a.list.Visible()
	for h := min(first, len(a.rows)-1); h >= 0; h-- {
		if a.rows[h].header != "" {
			return h
		}
	}
	return -1
}

// sectionHeight is the height of a section's header in the list.
const sectionHeight = 28

// underHeader fades a row of the list out as it scrolls under the header
// pinned at the list's top: wholly under the header, and over headerFade
// DIPs below it. The list clips the row to its box, whose top the header
// pins to, which the painter sees from the row moved off its place (band).
type underHeader struct{}

// headerFade is the list's top padding, by which the first rows are below
// the first header: they show wholly until they scroll.
const headerFade = 6

var fadeUnder = shader.MustCompile(`package main

// Cut is where the row starts to show, in DIPs from its top, and Fade how
// far below that it shows wholly.
var Cut float
var Fade float

func ColorEffect(pos vec2, color vec4) vec4 {
	return color * clamp((pos.y-Cut)/Fade, 0, 1)
}
`)

func (underHeader) EffectMargin() ui.Point { return ui.Point{} }

func (underHeader) PaintEffect(p *ui.Painter, box ui.Rect, content *ui.Bitmap, inset ui.Point) {
	cut := p.Visible().Y + sectionHeight - box.Y
	fx := fadeUnder.Effect(shader.Options{Uniforms: map[string]any{"Cut": cut, "Fade": float32(headerFade)}})
	fx.PaintEffect(p, box, content, inset)
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
		if it.Card.Last4 != "" {
			s = strings.TrimSpace(s + " •••• " + it.Card.Last4)
		}
		return s
	case it.Identity != nil:
		return it.Identity.FullName()
	case it.SSHKey != nil:
		return it.SSHKey.Fingerprint
	}
	return it.Type.String()
}

func (a *app) noItem(c *ui.Context, k ink) {
	ui.Column(c).Fill().Center().Gap(6).Padding(24).Children(func() {
		ui.Icon(c, iconLens).FontSize(36).TextColor(k.text.Alpha(0.12))
		ui.Text(c, "Type to search your vault").TextColor(k.muted)
		ui.Text(c, "Filters: =favorite =tag: =folder: =collection: =category: =vault: =passkey =location: =untagged").
			FontSize(11.5).TextColor(k.faint).TextAlign(ui.Center).LineHeight(1.4).MaxWidth(320)
	})
}

// footer is the bar at the bottom of Quick Access, which the list and the
// details scroll under. Where AppKit's material frosts what is under it,
// as under Raycast's action bar (nativeBar), the shell shows the bar in a
// window of its own over the material (footerView); else it draws here.
func (a *app) footer(c *ui.Context, k ink) {
	if a.nativeBar {
		return
	}
	// What scrolls under the footer frosts, as content under a bar does on
	// macOS 26. Over the window's material, what is under it is mostly
	// transparent, which a blur can't hide: the bar is tinted there, and
	// what scrolls to it fades into it, as under macOS's soft scroll edge.
	edge := ui.Box(c.Key("footer-edge")).Absolute().Left(0).Right(0).Bottom(0).PassThrough()
	if k.vibrant {
		edge.Height(footerHeight + 12).Material(glass.ScrollEdge{Bottom: true, Background: k.chrome})
	} else {
		edge.Height(footerHeight).Material(glass.ScrollEdge{Hard: true, Bottom: true, Background: k.chrome})
	}
	bar := ui.Row(c.Key("footer")).Absolute().Left(0).Right(0).Bottom(0).Height(footerHeight)
	if k.vibrant {
		bar.Background(k.chrome).BorderWidth(1, 0, 0, 0).BorderColor(k.line)
	}
	a.footerBar(c, k, bar)
}

// footerView is the footer in the window of its own the shell shows it in,
// over AppKit's material, which it draws nothing over but a hairline.
func (a *app) footerView(c *ui.Context) {
	c.SetTheme(theme(c.Theme()))
	k := inkOf(c.Theme())
	k.tips = a.cfg.Tooltips
	c.Root().Background(ui.Transparent).FontSize(13).TextColor(k.text)
	if !a.footerShows() {
		return
	}
	a.footerBar(c, k, ui.Row(c.Key("footer")).Fill().BorderWidth(1, 0, 0, 0).BorderColor(k.line))
}

// footerShows reports whether Quick Access shows its footer: over its
// items, without Large Type or the master password asked for over them.
func (a *app) footerShows() bool {
	return a.configured() && a.page == pageItems && a.unlocked() && a.large == nil && a.confirm == nil
}

// footerBar fills bar with what the keys do where they act, or the message
// of what just happened.
func (a *app) footerBar(c *ui.Context, k ink, bar ui.Element) {
	hint := func(id, label string) {
		if h := a.cfg.keyHint(id); h != "" {
			keycap(c, k, h, label)
		}
	}
	bar.Padding(0, 14).Gap(14).AlignItems(ui.Center).Clip()
	a.dragHandle(bar).Children(func() {
		if a.flashing(c) {
			ui.Icon(c, iconCheck).FontSize(13).TextColor(k.okay)
			ui.Text(c, a.flash).FontSize(12).TextColor(k.text).SingleLine().Shrink(1)
			return
		}
		it := a.selected()
		switch {
		case a.mode == modeVaults:
			hint("vault-choose", "Search the vault")
			hint("vault-expand", "Show collections")
			hint("vault-collapse", "Back")
		case a.mode == modeDetails && it != nil:
			if _, f, ok := a.focusedField(); ok {
				hint("field-copy", "Copy "+f.label)
				if f.secret && f.kind != "totp" && !a.reveal[fieldKey(it.ID, a.fieldSel)] {
					hint("field-reveal", "Reveal")
				} else {
					hint("field-reveal", "Large Type")
				}
			}
			hint("field-back", "Back")
		case it == nil && a.sel >= 0:
			hint("default", "Use filter")
		case it == nil:
			hint("close", "Close")
		default:
			if name := a.fillTarget(it); name != "" {
				hint("default", "Fill in "+name)
			} else if it.Login != nil && !it.Login.Password.Empty() {
				hint("default", "Copy password")
			}
			if f, ok := primary(it); ok {
				hint("copy-username", "Copy "+f.label)
			}
			if f, ok := secretOf(it); ok {
				hint("copy-password", "Copy "+f.label)
			}
			if _, _, ok := a.code(it); ok {
				hint("copy-code", "Copy code")
			}
			hint("details", "Fields")
		}
		ui.Spacer(c)
		if it != nil && a.mode != modeVaults {
			letThrough(textButton(c.Key("open-bw"), k, "Open in Bitwarden", iconExternal)).
				Description(a.cfg.keyHint("open-bitwarden")).OnClick(a.openInBitwarden)
		}
	})
}

// details shows the fields of an item, which a click copies, a drag drops
// into another app, and the keys go through after Right. In its own
// window, own is true.
func (a *app) details(c *ui.Context, k ink, it *bw.Item, own bool) {
	// Holding Option peeks at every secret field.
	peek := c.Modifiers()&ui.Alt != 0 && !it.Reprompt
	bottom := float32(16)
	if !own {
		bottom += footerHeight // under the footer
	}
	b := &a.detailBand
	if own {
		b = a.windowBand(it.ID)
	}
	b.show(it.ID)
	view := a.stretchy(ui.Scroll(c.Key("detail-"+it.ID)).Fill().ScrollbarInsets(0, 0, bottom-16, 0), b)
	off := b.offset(c)
	view.Children(func() {
		ui.Column(c).Padding(18, 18, bottom, 18).Gap(16).Translate(0, off).Children(func() {
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
					iconButton(c.Key("window"), k, iconExternal, "Open in Window ("+a.cfg.keyHint("open-window")+")").
						OnClick(func() { a.sys.openDetail(it.ID) })
				}
			})
			if it.Err != nil {
				problemBox(c.Key("err"), k, &problem{title: "Part of this item can't be decrypted", detail: it.Err.Error()})
			}
			fields := a.fieldsOf(it)
			if len(fields) > 0 {
				group(c.Key("fields"), k, func() {
					for i, f := range fields {
						focused := !own && a.mode == modeDetails && a.fieldSel == i
						key := fieldKey(it.ID, i)
						a.fieldRow(c.Key(i), k, it, f, key, peek || a.reveal[key], i > 0, focused)
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
			ui.Text(c, "Modified "+it.Revised.Local().Format("January 2, 2006 at 15:04")).FontSize(11).TextColor(k.faint).AlignSelf(ui.Center)
		})
	})
}

// windowBand is the rubber band of the details of item id in a window of
// their own.
func (a *app) windowBand(id string) *band {
	if a.windowBands == nil {
		a.windowBands = map[string]*band{}
	}
	b := a.windowBands[id]
	if b == nil {
		b = &band{}
		a.windowBands[id] = b
	}
	return b
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

// fieldsOf lists the fields an item shows, in the order the keys go
// through them.
func (a *app) fieldsOf(it *bw.Item) []field {
	var fs []field
	add := func(label, value string, secret bool) {
		if value != "" {
			fs = append(fs, field{label: label, value: value, secret: secret})
		}
	}
	// What the vault keeps encrypted, which the field decrypts as it shows.
	sec := func(label string, s bw.Secret) {
		if !s.Empty() {
			fs = append(fs, field{label: label, sec: s, secret: true})
		}
	}
	if l := it.Login; l != nil {
		add("username", l.Username, false)
		sec("password", l.Password)
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
		sec("number", cd.Number)
		if cd.ExpMonth != "" || cd.ExpYear != "" {
			add("expiration", fmt.Sprintf("%02s / %s", cd.ExpMonth, cd.ExpYear), false)
		}
		sec("security code", cd.Code)
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
		sec("social security number", id.SSN)
		sec("passport number", id.PassportNumber)
		sec("license number", id.LicenseNumber)
	}
	if k := it.SSHKey; k != nil {
		add("public key", k.PublicKey, false)
		add("fingerprint", k.Fingerprint, false)
		sec("private key", k.PrivateKey)
	}
	if it.Type == bw.TypeSecureNote && !it.Notes.Empty() {
		fs = append(fs, field{label: "note", sec: it.Notes, kind: "note"})
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
			sec(f.Name, f.Secret)
		case bw.FieldText:
			add(f.Name, f.Value, false)
		}
	}
	if !it.Notes.Empty() && it.Type != bw.TypeSecureNote {
		fs = append(fs, field{label: "notes", sec: it.Notes, kind: "note"})
	}
	return fs
}

// fieldRow is a field of an item, named key: its label over its value. A
// click copies it; it drags into the fields of other apps. reveal shows
// it if it is secret; focused is whether the keys are on it.
func (a *app) fieldRow(c *ui.Context, k ink, it *bw.Item, f field, key string, reveal, divider, focused bool) {
	if f.kind == "totp" {
		code, left, ok := a.code(it)
		if !ok {
			return
		}
		f.value = code
		c.After(left%time.Second + 10*time.Millisecond)
	}
	// Concealed, a secret shows as dots, and decrypts not even for their
	// number: as many as hide most.
	var shown string
	switch {
	case f.secret && f.kind != "totp" && !reveal:
		shown = strings.Repeat("•", 10)
	case f.kind == "totp":
		shown = totp.Format(f.value)
	case f.label == "number" && it.Card != nil:
		shown = cardNumber(a.shownValue(key, f))
	default:
		shown = a.shownValue(key, f)
	}
	// The row takes the pointer, not the keyboard, which the search keeps,
	// clicked too: typing still searches.
	row := ui.Row(c).Padding(9, 12).Gap(10).AlignItems(ui.Center).Label(f.label).
		Description("Click to copy, or drag into a field").KeepFocus()
	if divider {
		row.BorderWidth(1, 0, 0, 0).BorderColor(k.line)
	}
	switch {
	case focused:
		row.Background(k.selected)
		row.ScrollIntoView()
	case row.Hovered():
		row.Background(k.hover)
	}
	if !f.secret || reveal || f.kind == "totp" || !it.Reprompt {
		// Drop the value into the username and password fields of other
		// apps: the item asks for nothing more first.
		// It decrypts as the drag starts, and only then.
		row.DragDataFrom(func() transfer.Data {
			v, _ := f.open()
			return transfer.TextData(v)
		})
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
		// The end of every row is alike: Copy, which fades in as the
		// pointer comes, then the row's button, if it has one, so that
		// nothing moves from row to row. A code's countdown shows in the
		// button's place, its seconds before it in Copy's, which fade out
		// as Copy fades in.
		shown := float32(0)
		if row.Hovered() || focused {
			shown = 1
		}
		shown = row.Animate("copy", shown, 120*time.Millisecond)
		ui.Row(c.Key("copy-slot")).Size(28, 28).AlignItems(ui.Center).Justify(ui.End).Children(func() {
			if f.kind == "totp" {
				_, left, _ := a.code(it)
				// Next to the ring, as far from it as the ring is inside
				// its place.
				ui.Text(c, fmt.Sprintf("%ds", int(left.Seconds()+0.5))).FontSize(11).FontFeatures("tnum").
					TextColor(k.muted).NoWrap().Translate(11, 0).Opacity(1 - shown)
			}
			iconButton(c.Key("copy"), k, iconCopy, "Copy").Absolute().Top(0).Left(0).Opacity(shown).OnClick(func() {
				a.copyField(it, f)
				a.search.Focus()
			})
		})
		switch {
		case f.kind == "totp":
			_, left, _ := a.code(it)
			ui.Row(c).Size(28, 28).Center().Children(func() { countdown(c, k, left, 30) })
		case f.secret:
			ic, label := iconEye, "Reveal"
			if reveal {
				ic, label = iconEyeOff, "Conceal"
			}
			iconButton(c.Key("reveal"), k, ic, label+" (hold ⌥)").OnClick(func() {
				if reveal {
					delete(a.reveal, key)
					delete(a.shownValues, key)
				} else {
					a.guarded(it, func() { a.reveal[key] = true })
				}
				a.search.Focus()
			})
		case f.kind == "uri":
			u := f.uri
			if !strings.Contains(u, "://") {
				u = "https://" + u
			}
			// A website opens and fills, as Shift-Return does the first;
			// another address only opens.
			if web := strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://"); web && it.Login != nil {
				iconButton(c.Key("open"), k, iconExternal, "Open and Fill").OnClick(func() { a.fillInto(it, u) })
			} else {
				iconButton(c.Key("open"), k, iconExternal, "Open").OnClick(func() { a.sys.openURL(u) })
			}
		default:
			ui.Box(c).Size(28, 28)
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
func countdown(c *ui.Context, k ink, left time.Duration, period float32) ui.Element {
	frac := float32(left.Seconds()) / period
	color := k.accent
	if left < 6*time.Second {
		color = k.danger
	}
	track := k.line
	return ui.Box(c).Size(16, 16).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		p.StrokePath((&ui.Path{}).Circle(cx, cy, 6.5), 2.5, track)
		p.StrokePath((&ui.Path{}).Circle(cx, cy, 6.5).Trim(0, frac), 2.5, color)
	})
}

// largeTypeView shows a value big, each character in a cell numbered by
// its place, digits and symbols in colors of their own, to read it to
// someone or type it on another device. Any key or press puts it away.
func (a *app) largeTypeView(c *ui.Context, k ink) {
	lt := a.large
	ui.Box(c.Key("large-backdrop")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radius).
		Background(ui.RGBA(0, 0, 0, 0.45)).OnClick(func() { a.large = nil })
	ui.Column(c.Key("large")).Absolute().Top(40).Left(30).Right(30).Bottom(40).Center().Children(func() {
		ui.Column(c).Padding(18, 22).Gap(14).Radius(14).Background(k.raised).MaxWidth(windowWidth-60).
			Shadow(0, 10, 40, 0, ui.RGBA(0, 0, 0, 0.35)).Children(func() {
			ui.Row(c).Gap(12).Children(func() {
				ui.Text(c, lt.item+" · "+lt.label).FontSize(12).TextColor(k.muted).SingleLine().Grow(1).Shrink(1)
				ui.Text(c, "Press any key to close").FontSize(11).TextColor(k.faint).NoWrap()
			})
			runes := []rune(lt.value)
			if len(runes) > 64 || strings.ContainsRune(lt.value, '\n') {
				ui.Text(c, lt.value).FontSize(22).Font("monospace").LineHeight(1.35).Wrap()
				return
			}
			ui.Row(c).Wrap().Gap(2).Justify(ui.Center).Children(func() {
				for i, r := range runes {
					color := k.text
					switch {
					case unicode.IsDigit(r):
						color = k.accent
					case !unicode.IsLetter(r):
						color = k.danger
					}
					ui.Column(c.Key(i)).Width(30).AlignItems(ui.Center).Padding(4, 0).Radius(6).
						Background(k.text.Alpha(0.04)).Children(func() {
						ui.Text(c, string(r)).Font("monospace").FontSize(30).FontWeight(600).TextColor(color)
						ui.Text(c, fmt.Sprint(i+1)).FontSize(9).TextColor(k.faint)
					})
				}
			})
		})
	})
}

// confirmView asks for the master password before showing or using the
// secrets of an item that asks for it.
func (a *app) confirmView(c *ui.Context, k ink) {
	open := true
	ui.DialogBase(c.Key("confirm"), &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.25))
		panel.Width(340).Padding(18).Gap(12).Radius(14).Background(k.raised).Shadow(0, 8, 30, 0, ui.RGBA(0, 0, 0, 0.3))
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
				pushButton(c.Key("cancel"), k, "Cancel", false).OnClick(a.closeConfirm)
				primaryButton(c.Key("ok"), k, "Continue", a.confirmPw == "" || a.busy != "").OnClick(a.confirmPassword)
			})
		})
	})
	if !open {
		a.closeConfirm()
	}
}

// detailWindow is an item in a window of its own, which ⌘O opens. The
// copying keys work there too.
func (a *app) detailWindow(c *ui.Context, id string) {
	c.SetTheme(theme(c.Theme()))
	k := inkOf(c.Theme())
	k.tips = a.cfg.Tooltips
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
	if a.confirm == nil {
		for id, do := range map[string]func(){
			"copy-username": func() { a.copyPrimaryOf(it) },
			"copy-password": func() { a.copyPasswordOf(it) },
			"copy-code":     func() { a.copyCodeOf(it) },
		} {
			for _, s := range a.cfg.keysOf(id) {
				if b, ok := parseBinding(s); ok {
					c.OnShortcut(b.mods, b.key, do)
				}
			}
		}
	}
	a.details(c, k, it, true)
	if a.confirm != nil && a.confirmItem == id {
		a.confirmView(c, k)
	}
	a.secureField("item:"+id, a.confirm != nil && a.confirmItem == id && a.confirmH.Focused(c))
}
