package main

import (
	"hash/fnv"
	"time"

	"github.com/egoist/mygo/ui"
)

// The window's metrics.
const (
	windowWidth  = 760
	windowHeight = 480
	listWidth    = 296
	radius       = 16
)

// ink is the window's colors, from the theme, with Bitwarden's blue.
type ink struct {
	text, muted, faint ui.Color
	hover, press, line ui.Color
	accent, onAccent   ui.Color
	bg, pane, card     ui.Color
	danger, warn, okay ui.Color
	selected           ui.Color // what is chosen: translucent gray, as Raycast's
	// chrome is under the bars, raised under what floats over the
	// window: dialogs, menus, Large Type.
	chrome, raised ui.Color
	// vibrant is whether the window shows a material.
	vibrant bool
}

func inkOf(t *ui.Theme) ink {
	k := ink{
		text:     t.Text,
		muted:    t.TextMuted,
		faint:    t.Text.Alpha(0.36),
		hover:    t.Text.Alpha(0.05),
		press:    t.Text.Alpha(0.09),
		line:     t.Text.Alpha(0.09),
		accent:   ui.Hex("#175DDC"),
		onAccent: ui.RGB(255, 255, 255),
		bg:       ui.Hex("#FFFFFF"),
		pane:     ui.Hex("#F5F6F8"),
		card:     ui.Hex("#FFFFFF"),
		danger:   ui.Hex("#C83522"),
		warn:     ui.Hex("#B4730E"),
		okay:     ui.Hex("#1F8A4C"),
	}
	if t.Dark {
		k.accent = ui.Hex("#3D7FF0")
		k.bg = ui.Hex("#1F2023")
		k.pane = ui.Hex("#18191B")
		k.card = ui.Hex("#26282B")
		k.danger = ui.Hex("#FF6B5A")
		k.warn = ui.Hex("#F0B44C")
		k.okay = ui.Hex("#4CCF7F")
	}
	k.selected = ui.RGBA(0, 0, 0, 0.07)
	if t.Dark {
		k.selected = ui.RGBA(255, 255, 255, 0.1)
	}
	k.chrome, k.raised = k.bg, k.bg
	return k
}

// over makes k the colors over the window's material: tints, where solid
// colors would hide it.
func (k ink) over(dark bool) ink {
	// The panes show the material as it is, and the bars mostly hide what
	// scrolls under them, frosted, as the material can't be laid over it.
	k.bg, k.pane, k.vibrant = ui.Transparent, ui.Transparent, true
	if dark {
		k.card = ui.RGBA(255, 255, 255, 0.06)
		k.chrome = ui.RGBA(30, 31, 34, 0.85)
		k.raised = ui.RGBA(38, 39, 42, 0.97)
	} else {
		k.card = ui.RGBA(255, 255, 255, 0.5)
		k.chrome = ui.RGBA(250, 250, 252, 0.85)
		k.raised = ui.RGBA(255, 255, 255, 0.97)
	}
	return k
}

// theme is the system's, with Bitwarden's blue for its accent.
func theme(t *ui.Theme) *ui.Theme {
	th := *t
	k := inkOf(t)
	th.Accent = k.accent
	th.AccentHover = k.accent.Mix(ui.RGB(0, 0, 0), 0.1)
	th.AccentPressed = k.accent.Mix(ui.RGB(0, 0, 0), 0.2)
	th.AccentText = k.onAccent
	th.Focus = k.accent.Alpha(0.5)
	th.Selection = k.accent.Alpha(0.3)
	return &th
}

// view is the window: the account to set up, the vault locked, its items,
// the settings or the editor.
func (a *app) view(c *ui.Context) {
	c.SetTheme(theme(c.Theme()))
	t := c.Theme()
	k := inkOf(t)
	if c.Vibrancy() {
		k = k.over(t.Dark)
	}
	c.Root().Background(ui.Transparent)

	a.windowKeys(c)

	// The panel's shadow outlines it with a hairline: no border of its own.
	box := ui.Column(c).Fill().FontSize(13).TextColor(k.text).Radius(radius).Clip()
	if !c.Vibrancy() {
		box.Background(k.bg)
	}
	box.Children(func() {
		switch {
		case !a.configured() || a.page == pageSetup:
			a.setupView(c, k)
		case a.page == pageSettings:
			a.settingsView(c, k)
		case !a.unlocked():
			a.lockedView(c, k)
		default:
			a.itemsView(c, k)
		}
	})
	a.syncBar()
}

// syncBar has the shell show the footer over AppKit's material as Quick
// Access shows it, as it shows now, or hide it.
func (a *app) syncBar() {
	if !a.nativeBar {
		return
	}
	if on := a.footerShows(); on != a.barShown {
		a.barShown = on
		a.showBar(on)
	}
	if a.barShown {
		a.redrawBar()
	}
}

// windowKeys gives the window the keys of the actions where it is. The
// search takes its keys itself while it has the keyboard, as it would
// edit with some; a key being recorded, or the master password asked for,
// takes them all.
func (a *app) windowKeys(c *ui.Context) {
	items := a.configured() && a.page == pageItems && a.unlocked()
	if a.recordID != "" || a.confirm != nil || items && a.search.Focused(c) {
		return
	}
	for _, act := range actions {
		if act.scope == inGlobal || !items && act.scope != inWindow || items && !act.scope.covers(a.mode) {
			continue
		}
		id := act.id
		for _, s := range a.cfg.keysOf(id) {
			if b, ok := parseBinding(s); ok {
				c.OnShortcut(b.mods, b.key, func() { a.press(b.mods, b.key) })
			}
		}
	}
}

func (a *app) openSettings() {
	if a.page == pageSettings {
		return
	}
	a.recordID, a.keyErr = "", ""
	a.page = pageSettings
}

// iconButton is a borderless button showing an icon.
func iconButton(c *ui.Context, k ink, svg *ui.SVG, label string) ui.Element {
	b := ui.ButtonBase(c).Size(28, 28).Center().Radius(7).Label(label).Tooltip(label)
	switch {
	case b.Pressed():
		b.Background(k.press)
	case b.Hovered():
		b.Background(k.hover)
	}
	b.Children(func() {
		ic := ui.Icon(c, svg).FontSize(15).TextColor(k.muted)
		if b.Hovered() {
			ic.TextColor(k.text)
		}
	})
	return b
}

// textButton is a borderless button of a label, and an icon before it.
func textButton(c *ui.Context, k ink, label string, svg *ui.SVG) ui.Element {
	b := ui.ButtonBase(c).Padding(4, 8).Gap(5).Radius(6).FontSize(12).FontWeight(500).AlignItems(ui.Center)
	b.TextColor(k.muted)
	switch {
	case b.Pressed():
		b.Background(k.press).TextColor(k.text)
	case b.Hovered():
		b.Background(k.hover).TextColor(k.text)
	}
	b.Children(func() {
		if svg != nil {
			ui.Icon(c, svg).FontSize(13)
		}
		ui.Text(c, label).NoWrap()
	})
	return b
}

// problemBox says what went wrong, and offers to fix it.
func problemBox(c *ui.Context, k ink, p *problem) ui.Element {
	box := ui.Row(c).Gap(10).Padding(10, 12).Radius(10).Background(k.danger.Alpha(0.08)).AlignItems(ui.Start)
	box.Children(func() {
		ui.Icon(c, iconAlert).FontSize(16).TextColor(k.danger).Margin(1, 0, 0, 0)
		ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
			ui.Text(c, p.title).FontWeight(600).LineHeight(1.3)
			if p.detail != "" {
				ui.Text(c, p.detail).FontSize(12).TextColor(k.muted).LineHeight(1.4)
			}
			if p.fix != "" && p.do != nil {
				ui.Button(c.Key("fix"), p.fix).AlignSelf(ui.Start).Margin(4, 0, 0, 0).OnClick(p.do)
			}
		})
	})
	return box
}

// palette colors the tiles of items by their names.
var palette = []string{"#175DDC", "#0E8A6B", "#C2410C", "#7C3AED", "#BE185D", "#0369A1", "#4D7C0F", "#B45309", "#475569"}

// avatar is an item's tile: the first letter of a login's name on a color
// of its own, the icon of the other kinds.
func avatar(c *ui.Context, k ink, name string, kind int, size float32) ui.Element {
	h := fnv.New32a()
	h.Write([]byte(name))
	color := ui.Hex(palette[h.Sum32()%uint32(len(palette))])
	tile := ui.Row(c).Size(size, size).Radius(size * 0.26).Center().Shrink(0)
	tile.Children(func() {
		if kind == 1 && name != "" {
			tile.Background(color)
			letter := []rune(name)[0]
			ui.Text(c, string(letter)).FontSize(size * 0.48).FontWeight(650).TextColor(ui.RGB(255, 255, 255))
			return
		}
		tile.Background(k.text.Alpha(0.07))
		ui.Icon(c, itemIcon(kind)).FontSize(size * 0.52).TextColor(k.muted)
	})
	return tile
}

// keycap shows a shortcut in the footer: its keys, then what it does.
func keycap(c *ui.Context, k ink, keys, label string) {
	ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
		ui.Text(c, keys).FontSize(11).FontWeight(600).TextColor(k.muted).Padding(1, 5).Radius(4).Background(k.text.Alpha(0.06)).NoWrap()
		ui.Text(c, label).FontSize(11.5).TextColor(k.muted).NoWrap()
	})
}

// shieldTile is the app's mark.
func shieldTile(c *ui.Context, k ink, size float32) ui.Element {
	tile := ui.Row(c).Size(size, size).Radius(size*0.26).Center().
		Gradient(k.accent.Mix(ui.RGB(255, 255, 255), 0.12), k.accent.Mix(ui.RGB(0, 0, 0), 0.12), 180).
		Shadow(0, 3, 10, 0, k.accent.Alpha(0.3))
	tile.Children(func() {
		ui.Icon(c, iconShield).FontSize(size * 0.6).TextColor(ui.RGB(255, 255, 255))
	})
	return tile
}

// flashing reports whether the footer's message still shows, and asks for
// a frame as it goes.
func (a *app) flashing(c *ui.Context) bool {
	if a.flash == "" {
		return false
	}
	left := 3*time.Second - a.now().Sub(a.flashAt)
	if left <= 0 {
		return false
	}
	c.After(left)
	return true
}

// dragPhase is a step of the user moving the window.
type dragPhase int

const (
	dragStart dragPhase = iota
	dragMove
	dragEnd
)

// dragHandle makes e move the window as the pointer drags it, where no
// control in it takes the press: the controls let presses through to the
// handlers of what holds them, and so take theirs first (letThrough). The
// window snaps to the guides the shell shows as it goes.
func (a *app) dragHandle(e ui.Element) ui.Element {
	return e.HandleInput(func(ev ui.InputEvent) bool {
		switch {
		case ev.Kind == ui.InputPointerDown && ev.Button == 0:
			a.sys.drag(dragStart)
		case ev.Kind == ui.InputPointerMove && ev.Button == 0:
			a.sys.drag(dragMove)
		case ev.Kind == ui.InputPointerUp && ev.Button == 0:
			a.sys.drag(dragEnd)
		default:
			return false
		}
		return true
	})
}

// letThrough makes a control of a drag handle the first to see the input
// on it, which it takes as controls do, rather than the handle: an
// element's input goes to the innermost handler under the pointer. Only
// the controls of drag handles have it, as it would take the scrolling of
// the views around a control from theirs too.
func letThrough(e ui.Element) ui.Element {
	return e.HandleInput(func(ui.InputEvent) bool { return false })
}
