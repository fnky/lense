// Lense is a companion to the Bitwarden desktop app in the menu
// bar: a window that a shortcut opens from anywhere, as Spotlight, to
// search the vault, copy from it and fill it into other apps. It reads
// the vault the desktop app keeps on the Mac, and asks the app to unlock
// it with Touch ID, without requests of its own to Bitwarden's servers.
package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"lense/internal/bw"
	"lense/internal/fill"
	"lense/internal/ipc"
	"lense/internal/macos"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// shell is what lives as long as the app: the tray icon, the window while
// it shows, the windows of items, and the app's state.
type shell struct {
	a       *app
	dir     string
	tray    *mygo.Tray
	content *ui.Content
	// win is Quick Access while it shows: a panel that takes the keyboard
	// without activating the app, as Spotlight's does, so that the app the
	// user was in stays in front, to fill.
	win      *mygo.Window
	details  map[string]*mygo.Window
	ownPID   int
	quitting bool
	// bar is AppKit's material over the bottom of win, which frosts what
	// scrolls under the footer, and footer the window over it that shows
	// the footer (footerView), as the action bar of Raycast shows.
	bar    uintptr
	footer *mygo.Window

	saving   sync.Mutex
	shortcut string // registered

	// moving is the user moving the window, with where the pointer and
	// the window were as it began; guideWin shows the guides it snaps to,
	// over the display the pointer is on.
	moving   windowDrag
	guideWin *mygo.Window
	guides   guides
}

type windowDrag struct {
	active bool
	from   mygo.Point
	x, y   int
}

func (s *shell) ready() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	s.dir = dir
	s.details = map[string]*mygo.Window{}
	s.ownPID = os.Getpid()
	adoptConfig(dir)
	cfg := loadConfig(dir)
	if cfg.AppID == "" {
		cfg.AppID = rand.Text()
	}
	a := newApp(cfg, &bitwardenApp{appID: cfg.AppID}, &macSystem{s: s})
	s.a = a
	// The version mygo build packaged, from mygo.json: the one updates
	// compare with.
	if a.version = mygo.App.Version(); a.version == "" {
		a.version = "dev"
	}
	a.post = func(fn func()) {
		mygo.RunOnMain(func() {
			fn()
			s.redraw()
		})
	}
	a.spawn = func(fn func()) { go fn() }
	a.saveCfg = func(cfg config) { go s.save(cfg) }
	a.onLock = func() {
		for id, w := range s.details {
			w.Destroy()
			delete(s.details, id)
		}
		s.updateTray()
	}
	a.onUnlock = s.updateTray
	a.setShortcut = s.registerShortcut
	a.exit = mygo.App.Quit
	a.relaunch = mygo.App.Relaunch
	a.upd = mygoUpdater{}
	a.updEnabled = a.upd.enabled()
	if a.configured() {
		a.page = pageItems
	}

	a.refocus = func() {
		if s.win != nil {
			s.win.Focus()
		}
	}
	a.applyMaterial = func() {
		if s.win != nil {
			s.applyMaterial(s.win)
		}
	}
	a.applyCapture = func() {
		s.allowCapture(s.win)
		s.allowCapture(s.footer)
		for _, w := range s.details {
			s.allowCapture(w)
		}
	}
	a.showBar = func(show bool) { mygo.RunOnMain(func() { s.showBar(show) }) }
	a.redrawBar = func() {
		if s.footer != nil {
			s.footer.Invalidate()
		}
	}
	s.content = ui.View(a.view)

	s.tray, err = mygo.NewTray(mygo.TrayOptions{
		Icon:           trayIcon(true),
		IconIsTemplate: true,
		ToolTip:        "Lense",
	})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	s.tray.OnClick(s.toggle)
	s.tray.OnRightClick(func() {
		s.tray.SetMenu(s.menu())
		defer s.tray.SetMenu(nil)
		s.tray.PopUpMenu()
	})

	if err := s.registerShortcut(cfg.Shortcut); err != nil {
		log.Printf("shortcut %s: %v", cfg.Shortcut, err)
		a.keyErr, a.keyErrID = fmt.Sprintf("%s is taken by another app. Choose another shortcut.", keySymbols(cfg.Shortcut)), "show"
	}

	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { s.quitting = true })
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		a.lock(true)
		s.tray.Destroy()
	})
	mygo.Power.OnSuspend(func() { s.sleeping() })
	mygo.Power.OnLockScreen(func() { s.sleeping() })

	a.reload()
	go s.watch()
	// Automatic updates check soon after launch, then once a day.
	time.AfterFunc(firstUpdateCheck, func() {
		a.post(a.autoUpdate)
		for range time.Tick(updateInterval) {
			a.post(a.autoUpdate)
		}
	})
	switch {
	case !a.configured():
		// Nothing to search yet: show how to set it up.
		mygo.RunOnMain(func() {
			a.startSetup()
			s.show()
		})
	case !mygo.App.WasOpenedAtLogin():
		// Opened, it shows at once, as its shortcut does, loading.
		mygo.RunOnMain(s.show)
	}
}

// menu is the tray icon's menu.
func (s *shell) menu() *mygo.Menu {
	a := s.a
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Open Lense", Accelerator: s.shortcut, Click: func(*mygo.MenuItem, *mygo.Window) { s.summon() }},
		{Label: "Lock", Disabled: !a.unlocked(), Click: func(*mygo.MenuItem, *mygo.Window) { a.lock(false); s.redraw() }},
		mygo.Separator(),
		{Label: "Open Bitwarden", Click: func(*mygo.MenuItem, *mygo.Window) { a.openBitwarden() }},
		{Label: "Settings…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings(); s.show() }},
		mygo.Separator(),
		{Label: "Quit Lense", Click: func(*mygo.MenuItem, *mygo.Window) { a.quit() }},
	})
}

// registerShortcut makes accel open the window from anywhere, in place of
// the shortcut before.
func (s *shell) registerShortcut(accel string) error {
	if accel == s.shortcut {
		return nil
	}
	if err := mygo.GlobalShortcut.Register(accel, s.toggle); err != nil {
		return err
	}
	if s.shortcut != "" {
		mygo.GlobalShortcut.Unregister(s.shortcut)
	}
	s.shortcut = accel
	return nil
}

func (s *shell) redraw() {
	if s.win != nil {
		s.win.Invalidate()
	}
	for _, w := range s.details {
		w.Invalidate()
	}
}

func (s *shell) updateTray() {
	if s.tray != nil {
		_ = s.tray.SetIcon(trayIcon(!s.a.unlocked()), true)
	}
}

// toggle shows the window, or hides it.
func (s *shell) toggle() {
	if s.win != nil {
		s.hide()
		return
	}
	s.summon()
}

// summon shows the window as the user asked for it, by its shortcut or the
// menu bar: locked, it asks for Touch ID.
func (s *shell) summon() { s.open(true) }

// show shows the window, as the app opens, or has something to show: it
// asks for nothing.
func (s *shell) show() { s.open(false) }

// open shows the window where it was last on the display the pointer is
// on, as it was: with the search and the item chosen, unless the vault
// locked meanwhile. Locked, it asks for Touch ID when ask.
func (s *shell) open(ask bool) {
	a := s.a
	if s.win != nil {
		s.win.Focus()
		a.shown(ask)
		return
	}
	if fa := macos.FrontmostApp(); fa.PID != s.ownPID {
		a.setFront(fa)
	}
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	work := rect{d.WorkArea.X, d.WorkArea.Y, d.WorkArea.Width, d.WorkArea.Height}
	x, y := a.cfg.Window.place(d.ID, work, windowWidth, windowHeight)
	w := mygo.NewFlyout(mygo.FlyoutOptions{
		Tray:      s.tray,
		Width:     windowWidth,
		Height:    windowHeight,
		Focusable: true,
		Shadow:    true,
		Hidden:    true,
		Content:   s.content,
		// The inspector of development builds would take Shift-Cmd-C.
		Page: mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
	})
	w.SetPosition(x, y)
	s.applyMaterial(w)
	s.allowCapture(w)
	s.addBar(w)
	w.OnClose(func(e *mygo.CloseEvent) {
		// Like Spotlight, it goes as the user goes elsewhere, but not to
		// the prompts of Touch ID.
		if a.holdOpen > 0 && !s.quitting {
			e.PreventDefault()
			return
		}
		s.remember(w)
	})
	w.OnClosed(func() {
		if s.win == w {
			s.win = nil
			a.hidden()
			s.redraw() // the windows of items, which reveal with it
			// The footer's window closes with it.
			s.bar, s.footer, a.nativeBar = 0, nil, false
			a.recordID = ""
			s.moving.active = false
			s.hideGuides()
		}
	})
	s.win = w
	w.Show()
	if a.unlocked() && a.page == pageItems {
		a.search.Focus()
	}
	a.shown(ask)
	s.redraw()
}

// addBar lays AppKit's material over the bottom of w, and the window that
// shows the footer over it, which follows w and closes with it: a window
// of its own, as the material frosts what w draws under it. The view shows
// them as it shows the footer (app.syncBar).
func (s *shell) addBar(w *mygo.Window) {
	s.bar = macos.BarMaterial(w.NativeHandle(), footerHeight, radius, macos.MaterialHeaderView)
	if s.bar == 0 {
		return // the footer draws in w
	}
	macos.ShowBar(s.bar, false)
	s.footer = mygo.NewFlyout(mygo.FlyoutOptions{
		Parent:  w,
		Anchor:  mygo.Rectangle{Y: windowHeight - footerHeight, Width: windowWidth},
		Width:   windowWidth,
		Height:  footerHeight,
		Hidden:  true,
		Content: ui.View(s.a.footerView),
		Page:    mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
	})
	s.allowCapture(s.footer)
	s.a.nativeBar, s.a.barShown = true, false
}

// showBar shows the footer over AppKit's material, or hides both.
func (s *shell) showBar(show bool) {
	if s.bar == 0 || s.footer == nil {
		return
	}
	macos.ShowBar(s.bar, show)
	if show {
		s.footer.Show()
	} else {
		s.footer.Hide()
	}
}

// allowCapture lets screen captures show w, or has macOS leave it out of
// them, as the settings say.
func (s *shell) allowCapture(w *mygo.Window) {
	if w != nil {
		macos.AllowCapture(w.NativeHandle(), s.a.cfg.AllowScreenCapture)
	}
}

// applyMaterial shows the material chosen behind the window's content, its
// corners rounded as the content's, or none.
func (s *shell) applyMaterial(w *mygo.Window) {
	if !s.a.cfg.translucent() {
		w.SetVibrancy(mygo.VibrancyNone)
		return
	}
	w.SetVibrancy(mygo.Vibrancy(s.a.cfg.Material))
	macos.RoundMaterial(w.NativeHandle(), radius)
}

// hide closes the window: the keyboard goes back to the app the user was
// in, which stayed in front.
func (s *shell) hide() {
	if s.win != nil {
		s.win.Close()
	}
}

// remember records where the window is, on its display.
func (s *shell) remember(w *mygo.Window) {
	b := w.Bounds()
	d := mygo.Screen.DisplayMatching(b)
	work := rect{d.WorkArea.X, d.WorkArea.Y, d.WorkArea.Width, d.WorkArea.Height}
	s.a.cfg.Window.remember(d.ID, work, rect{b.X, b.Y, b.Width, b.Height})
	s.a.changed()
}

// drag moves the window with the pointer, from the display it's on to
// another: near the guides of the display the pointer is on, which it
// shows, it snaps to them. Its place is saved for the display it ends on.
func (s *shell) drag(p dragPhase) {
	w := s.win
	if w == nil {
		return
	}
	switch p {
	case dragStart:
		x, y := w.Position()
		s.moving = windowDrag{active: true, from: mygo.Screen.CursorScreenPoint(), x: x, y: y}
		s.guides = guides{}
	case dragMove:
		if !s.moving.active {
			return
		}
		cur := mygo.Screen.CursorScreenPoint()
		d := mygo.Screen.DisplayNearestPoint(cur)
		work := rect{d.WorkArea.X, d.WorkArea.Y, d.WorkArea.Width, d.WorkArea.Height}
		x, y, g := snap(s.moving.x+cur.X-s.moving.from.X, s.moving.y+cur.Y-s.moving.from.Y, windowWidth, windowHeight, work)
		g.carry(s.guides, time.Now())
		w.SetPosition(x, y)
		s.showGuides(g)
	case dragEnd:
		if !s.moving.active {
			return
		}
		s.moving.active = false
		s.hideGuides()
		s.remember(w)
	}
}

// showGuides shows the guides over the work area of their display, under
// the window, and lets the pointer through them.
func (s *shell) showGuides(g guides) {
	s.guides = g
	b := mygo.Rectangle{X: g.work.X, Y: g.work.Y, Width: g.work.W, Height: g.work.H}
	if s.guideWin == nil {
		s.guideWin = mygo.NewWindow(mygo.WindowOptions{
			Title:             "Lense Guides",
			X:                 b.X,
			Y:                 b.Y,
			Width:             b.Width,
			Height:            b.Height,
			Frameless:         true,
			Transparent:       true,
			Hidden:            true,
			AlwaysOnTop:       true,
			DisableShadow:     true,
			DisableResize:     true,
			DisableMinimize:   true,
			DisableMaximize:   true,
			DisableFullScreen: true,
			Content:           ui.View(s.guidesView),
			Page:              mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
		})
		s.guideWin.SetIgnoreMouseEvents(true)
		s.guideWin.SetVisibleOnAllWorkspaces(true)
	}
	if s.guideWin.Bounds() != b {
		s.guideWin.SetBounds(b)
	}
	if !s.guideWin.IsVisible() {
		// Without the keyboard, which the window keeps.
		s.guideWin.ShowInactive()
	}
	s.guideWin.Invalidate()
}

func (s *shell) hideGuides() {
	if s.guideWin != nil && s.guideWin.IsVisible() {
		s.guideWin.Hide()
	}
}

func (s *shell) guidesView(c *ui.Context) {
	c.Root().Background(ui.Transparent)
	g, accent := s.guides, inkOf(c.Theme()).accent
	ui.Box(c).Fill().PassThrough().Draw(func(p *ui.Painter, r ui.Rect) { drawGuides(p, r, g, accent) })
}

// openDetail opens an item in a window of its own.
func (s *shell) openDetail(id string) {
	a := s.a
	if w, ok := s.details[id]; ok {
		w.Show()
		w.Focus()
		return
	}
	it := a.vault.Item(id)
	if it == nil {
		return
	}
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:     it.Name,
		Width:     440,
		Height:    560,
		MinWidth:  360,
		MinHeight: 320,
		Content:   ui.View(func(c *ui.Context) { a.detailWindow(c, id) }),
		Page:      mygo.PageOptions{DevTools: mygo.DevToolsDisabled},
	})
	s.details[id] = w
	s.allowCapture(w)
	w.OnClosed(func() {
		delete(s.details, id)
		delete(a.windowBands, id)
		a.secureField("item:"+id, false)
	})
	s.hide()
	w.Focus()
}

// watch reads the desktop app's data again as it changes, as the app
// syncs, and locks the vault after the time idle the user chose.
func (s *shell) watch() {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for range tick.C {
		mygo.RunOnMain(func() {
			a := s.a
			if a.store != nil {
				if st, err := os.Stat(a.store.Path); err == nil && !st.ModTime().Equal(a.store.ModTime) {
					a.reload()
				}
			} else if a.storeErr != nil {
				a.reload()
			}
			if a.unlocked() && a.cfg.AutoLock > 0 && mygo.Power.IdleTime() >= time.Duration(a.cfg.AutoLock)*time.Minute {
				a.lock(false)
				s.redraw()
			}
		})
	}
}

// sleeping locks the vault as the Mac sleeps or its screen locks.
func (s *shell) sleeping() {
	if s.a.cfg.LockOnSleep && s.a.unlocked() {
		s.a.lock(false)
		s.hide()
		s.redraw()
	}
}

// save writes the config, one at a time.
func (s *shell) save(cfg config) {
	s.saving.Lock()
	defer s.saving.Unlock()
	if err := saveConfig(s.dir, cfg); err != nil {
		log.Print("config: ", err)
	}
}

// bitwardenApp is the Bitwarden desktop app on this Mac.
type bitwardenApp struct{ appID string }

func (b *bitwardenApp) load() (*bw.Store, error) {
	p, err := bw.FindData()
	if err != nil {
		return nil, err
	}
	return bw.Open(p)
}

func (b *bitwardenApp) unlock(ctx context.Context, userID string) (bw.SymmetricKey, error) {
	c, err := ipc.Dial(ctx, b.appID, userID)
	if err != nil {
		return bw.SymmetricKey{}, err
	}
	defer c.Close()
	if st, err := c.Status(ctx, userID); err == nil && st != ipc.StatusAvailable {
		return bw.SymmetricKey{}, statusError{st}
	}
	return c.Unlock(ctx, userID)
}

// listens asks the app how Touch ID unlocks the account, waiting a few
// seconds for its answer, as it may not answer yet as it starts.
func (b *bitwardenApp) listens(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	c, err := ipc.Dial(ctx, b.appID, userID)
	if err != nil {
		return err
	}
	defer c.Close()
	_, err = c.Status(ctx, userID)
	return err
}

func (b *bitwardenApp) running() bool   { return macos.IsRunning(macos.BitwardenBundleID) }
func (b *bitwardenApp) installed() bool { return macos.IsInstalled(macos.BitwardenBundleID) }
func (b *bitwardenApp) open() error     { return macos.OpenApp(macos.BitwardenBundleID) }
func (b *bitwardenApp) launch() error   { return macos.LaunchHidden(macos.BitwardenBundleID) }

// macSystem is macOS, and the shell.
type macSystem struct {
	s *shell

	mu    sync.Mutex
	bio   bool
	bioAt time.Time
}

func (m *macSystem) copy(text string, secret bool) {
	count := macos.CopySecret(text, secret)
	if d := m.s.a.cfg.ClearClipboard; secret && d > 0 && count >= 0 {
		time.AfterFunc(time.Duration(d)*time.Second, func() {
			mygo.RunOnMain(func() { macos.ClearIfUnchanged(count) })
		})
	}
}

func (m *macSystem) openURL(u string) { go mygo.Shell.OpenExternal(u) }

// biometrics asks LocalAuthentication at most every half a minute, as the
// view asks every frame.
func (m *macSystem) biometrics() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Since(m.bioAt) > 30*time.Second {
		m.bio, m.bioAt = macos.BiometricsAvailable(), time.Now()
	}
	return m.bio
}

func (m *macSystem) seal(secret []byte) (sealedSecret, error) {
	s, err := macos.Seal(secret)
	if err != nil {
		return nil, err
	}
	return macSealed{s}, nil
}

// macSealed is a secret the Secure Enclave sealed.
type macSealed struct{ s *macos.Sealed }

func (m macSealed) open(reason string) ([]byte, error) {
	return m.s.Open(reason, "Use Master Password")
}
func (m macSealed) release() { m.s.Release() }

func (m *macSystem) trusted(prompt bool) bool { return macos.Trusted(prompt) }

func (m *macSystem) fill(ctx context.Context, target macos.App, req fillRequest) error {
	kb := fill.Mac{Open: func(u string) { mygo.Shell.OpenExternal(u) }}
	return fill.Fill(ctx, kb, target, fill.Request{Username: req.username, Password: req.password, Page: req.page, Matches: req.matches})
}

// fieldFocused reports whether a text field has the focus in the app,
// where Return fills.
func (m *macSystem) fieldFocused(app macos.App) bool {
	return macos.Trusted(false) && macos.FocusedFieldOf(app.PID) != macos.FieldNone
}

func (m *macSystem) defaultBrowser() string { return macos.DefaultBrowser() }

func (m *macSystem) pageURL(ctx context.Context, browser macos.App) string {
	u, _ := macos.BrowserURL(ctx, browser.BundleID)
	return u
}

func (m *macSystem) drag(p dragPhase)    { m.s.drag(p) }
func (m *macSystem) secureInput(on bool) { macos.SecureInput(on) }
func (m *macSystem) scrollPhase() (phase, momentum macos.Phase) {
	return macos.ScrollPhase()
}

// mygoUpdater updates the app with mygo.Updater, from the releases that
// mygo.json's updates name.
type mygoUpdater struct{}

func (mygoUpdater) enabled() bool { return mygo.Updater.Enabled() }

func (mygoUpdater) check(ctx context.Context) (availableUpdate, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	return mygoUpdate{up}, nil
}

type mygoUpdate struct{ up *mygo.Update }

func (u mygoUpdate) version() string { return u.up.Version }

func (u mygoUpdate) install(ctx context.Context, progress func(done, total int64)) error {
	return u.up.Install(ctx, progress)
}

func (m *macSystem) show()                { mygo.RunOnMain(m.s.show) }
func (m *macSystem) hide()                { mygo.RunOnMain(m.s.hide) }
func (m *macSystem) openDetail(id string) { mygo.RunOnMain(func() { m.s.openDetail(id) }) }

func main() {
	// Debuggers, and what reads memory through them, keep out of the
	// vault: LENSE_DEBUG lets them in, to debug the app. memguard keeps
	// the system from dumping its memory already.
	if os.Getenv("LENSE_DEBUG") == "" {
		if err := macos.DenyDebuggers(); err != nil {
			log.Print(err)
		}
	}
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running one takes over
	}
	s := &shell{}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	// No menu bar: its key equivalents, as the Edit menu's Copy, would take
	// the keys of Quick Access before its window. Text fields cut, copy,
	// paste and undo by themselves.
	mygo.App.SetMenu(mygo.NewMenu(nil))
	// The window hides rather than closes; keep running without it.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnSecondInstance(func([]string, string) { s.show() })
	// Opened again while it runs, it shows too.
	mygo.App.OnActivate(func(bool) { s.show() })
	mygo.App.WhenReady(s.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
