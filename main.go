// Bitwarden Mini is a companion to the Bitwarden desktop app in the menu
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

	"bitwarden-mini/internal/bw"
	"bitwarden-mini/internal/ipc"
	"bitwarden-mini/internal/macos"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const version = "0.1.0"

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

	saving   sync.Mutex
	shortcut string // registered
}

func (s *shell) ready() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	s.dir = dir
	s.details = map[string]*mygo.Window{}
	s.ownPID = os.Getpid()
	cfg := loadConfig(dir)
	if cfg.AppID == "" {
		cfg.AppID = rand.Text()
	}
	a := newApp(cfg, &bitwardenApp{appID: cfg.AppID}, &macSystem{s: s})
	s.a = a
	a.version = version
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
	if a.configured() {
		a.page = pageItems
	}

	a.refocus = func() {
		if s.win != nil {
			s.win.Focus()
		}
	}
	s.content = ui.View(a.view)

	s.tray, err = mygo.NewTray(mygo.TrayOptions{
		Icon:           trayIcon(true),
		IconIsTemplate: true,
		ToolTip:        "Bitwarden Mini",
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
	if !a.configured() {
		// Nothing to search yet: show how to set it up.
		mygo.RunOnMain(func() {
			a.startSetup()
			s.show()
		})
	}
}

// menu is the tray icon's menu.
func (s *shell) menu() *mygo.Menu {
	a := s.a
	return mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Open Bitwarden Mini", Accelerator: s.shortcut, Click: func(*mygo.MenuItem, *mygo.Window) { s.show() }},
		{Label: "Lock", Disabled: !a.unlocked(), Click: func(*mygo.MenuItem, *mygo.Window) { a.lock(false); s.redraw() }},
		mygo.Separator(),
		{Label: "Open Bitwarden", Click: func(*mygo.MenuItem, *mygo.Window) { a.openBitwarden() }},
		{Label: "Settings…", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings(); s.show() }},
		mygo.Separator(),
		{Label: "Quit Bitwarden Mini", Click: func(*mygo.MenuItem, *mygo.Window) { a.quit() }},
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
	s.show()
}

// show shows the window where it was last on the display the pointer is
// on, as it was: with the search and the item chosen, unless the vault
// locked meanwhile.
func (s *shell) show() {
	a := s.a
	if s.win != nil {
		s.win.Focus()
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
			a.recordID = ""
		}
	})
	s.win = w
	w.Show()
	if a.unlocked() && a.page == pageItems {
		a.search.Focus()
	}
	a.shown()
	s.redraw()
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
	w.OnClosed(func() { delete(s.details, id) })
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

func (b *bitwardenApp) running() bool   { return macos.IsRunning(macos.BitwardenBundleID) }
func (b *bitwardenApp) installed() bool { return macos.IsInstalled(macos.BitwardenBundleID) }
func (b *bitwardenApp) open() error     { return macos.OpenApp(macos.BitwardenBundleID) }

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

func (m *macSystem) authenticate(reason string) error {
	return macos.Authenticate(reason, "Use Master Password")
}

func (m *macSystem) trusted(prompt bool) bool { return macos.Trusted(prompt) }

func (m *macSystem) fill(ctx context.Context, target macos.App, req fillRequest) error {
	return fillInto(ctx, target, req, func(u string) { mygo.Shell.OpenExternal(u) })
}

// fieldFocused reports whether a text field has the focus in the app,
// where Return fills.
func (m *macSystem) fieldFocused(app macos.App) bool {
	return macos.Trusted(false) && macos.FocusedFieldOf(app.PID) != macos.FieldNone
}

func (m *macSystem) pageURL(ctx context.Context, browser macos.App) string {
	u, _ := macos.BrowserURL(ctx, browser.BundleID)
	return u
}

func (m *macSystem) show()                { mygo.RunOnMain(m.s.show) }
func (m *macSystem) hide()                { mygo.RunOnMain(m.s.hide) }
func (m *macSystem) openDetail(id string) { mygo.RunOnMain(func() { m.s.openDetail(id) }) }

func main() {
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
	mygo.App.WhenReady(s.ready)
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
