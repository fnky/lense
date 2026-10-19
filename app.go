package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"bitwarden-mini/internal/bw"
	"bitwarden-mini/internal/ipc"
	"bitwarden-mini/internal/macos"
	"bitwarden-mini/internal/search"
	"bitwarden-mini/internal/totp"

	"github.com/egoist/mygo/ui"
)

// desktop is the Bitwarden desktop app: the vault it keeps on this Mac,
// and unlocking it with Touch ID, which it does for its browser
// extensions. Bitwarden Mini makes no request of Bitwarden's servers.
type desktop interface {
	load() (*bw.Store, error)
	// unlock asks the app to unlock the account with Touch ID, and
	// returns its user key.
	unlock(ctx context.Context, userID string) (bw.SymmetricKey, error)
	running() bool
	installed() bool
	open() error
}

// system is what the app asks of macOS and of its shell.
type system interface {
	// copy puts text on the clipboard; a secret is marked concealed and
	// cleared after a while.
	copy(text string, secret bool)
	openURL(url string)
	// biometrics reports whether Touch ID can authenticate the user.
	biometrics() bool
	// authenticate asks for Touch ID, and waits: run it in the
	// background.
	authenticate(reason string) error
	// trusted reports whether the app may type into others, asking the
	// user to let it with prompt.
	trusted(prompt bool) bool
	// fill types a login into the app the user was in, or into the page
	// it opens first: run it in the background.
	fill(ctx context.Context, target macos.App, req fillRequest) error
	// pageURL returns the address of the page a browser shows: run it in
	// the background.
	pageURL(ctx context.Context, browser macos.App) string
	show()
	hide()
	openDetail(id string)
}

// fillRequest is a login to type into another app.
type fillRequest struct {
	username, password string
	page               string // a page to open first, and fill once it shows
}

// errNoField means no field to fill had the keyboard focus.
var errNoField = errors.New("no field to fill has the focus")

type page int

const (
	pageItems page = iota
	pageSetup
	pageSettings
	pageEdit
)

// problem is something that went wrong, said so that the user can fix it.
type problem struct {
	title  string
	detail string
	fix    string // the label of the action that fixes it
	do     func()
}

// front is the app the user was in as the window showed, which Fill
// types into and suggestions are for.
type front struct {
	app macos.App
	url string // the page shown, when it is a browser
}

// row is a row of the list: a section's header, an item, or a filter
// that finishes what is typed.
type row struct {
	header string
	item   *bw.Item
	comp   *search.Completion
}

func (r row) key() any {
	switch {
	case r.item != nil:
		return "i:" + r.item.ID
	case r.comp != nil:
		return "c:" + r.comp.Label
	}
	return "h:" + r.header
}

// app is the state of Bitwarden Mini. Everything in it is read and changed
// on the main thread: work in other goroutines posts its results back
// with post.
type app struct {
	cfg     config
	version string
	now     func() time.Time
	post    func(func()) // runs a function on the main thread, then redraws
	spawn   func(func()) // starts work in the background
	saveCfg func(config)
	sys     system
	desk    desktop
	// onLock and onUnlock tell the shell, to update the tray and close
	// the windows of items.
	onLock, onUnlock func()
	// setShortcut registers a new global shortcut, or says why it can't.
	setShortcut func(accel string) error

	page page

	// What the desktop app keeps.
	store    *bw.Store
	storeErr error

	// Setting up the account.
	setupEmail  string
	setupRegion int // 0 bitwarden.com, 1 bitwarden.eu, 2 self-hosted
	setupURL    string
	setupProb   *problem
	connecting  bool

	// The vault, decrypted; locked hides it until Touch ID shows it
	// again, or forgets it when nil.
	vault    *bw.Vault
	index    *search.Index
	locked   bool
	busy     string // an unlock under way: "touch", "password"
	password string
	lockProb *problem
	pwField  ui.Handle
	// tried is whether Touch ID was tried as the window showed, locked.
	tried bool

	// The items.
	query  string
	rows   []row
	sel    int // the chosen row, or -1
	list   ui.ListState
	search ui.Handle
	// searchSel is whether some of the search is selected, which
	// Command-C copies then.
	searchSel bool
	caretEnd  bool // puts the caret after the search, which changed
	front     front
	// reveal shows the secrets of an item; reprompted is the items whose
	// master password was asked for, as their owners asked.
	reveal     map[string]bool
	reprompted map[string]bool
	flash      string
	flashAt    time.Time
	prob       *problem // over the items, as a fill that failed
	// confirm asks for the master password before going on, for an item
	// that asks for it.
	confirm     func()
	confirmItem string
	confirmPw   string
	confirmErr  string
	confirmH    ui.Handle

	// holdOpen keeps the window open as it loses the focus: to Touch ID,
	// or the desktop app's prompt.
	holdOpen int

	// Settings.
	recording   bool
	shortcutErr string

	// exit quits the app.
	exit func()
}

func newApp(cfg config, desk desktop, sys system) *app {
	a := &app{
		cfg:         cfg,
		now:         time.Now,
		post:        func(fn func()) { fn() },
		spawn:       func(fn func()) { fn() },
		saveCfg:     func(config) {},
		sys:         sys,
		desk:        desk,
		onLock:      func() {},
		onUnlock:    func() {},
		setShortcut: func(string) error { return nil },
		exit:        func() {},
		sel:         -1,
		locked:      true,
		reveal:      map[string]bool{},
		reprompted:  map[string]bool{},
	}
	a.list.Key = func(i int) any {
		if i < len(a.rows) {
			return a.rows[i].key()
		}
		return i
	}
	a.list.Header = func(i int) bool { return i < len(a.rows) && a.rows[i].header != "" }
	a.list.Label = func(i int) string {
		if i < len(a.rows) && a.rows[i].item != nil {
			return a.rows[i].item.Name
		}
		return ""
	}
	if cfg.Email == "" {
		a.page = pageSetup
	}
	return a
}

// unlocked reports whether the vault shows.
func (a *app) unlocked() bool { return a.vault != nil && !a.locked }

// configured reports whether the user chose an account.
func (a *app) configured() bool { return a.cfg.Email != "" }

// changed saves the config.
func (a *app) changed() { a.saveCfg(a.cfg) }

// say shows a short message in the footer.
func (a *app) say(format string, args ...any) {
	a.flash = fmt.Sprintf(format, args...)
	a.flashAt = a.now()
}

// --- The desktop app's data ---

// reload reads the desktop app's data again, and decrypts the vault anew
// when it changed.
func (a *app) reload() {
	a.spawn(func() {
		st, err := a.desk.load()
		a.post(func() { a.setStore(st, err) })
	})
}

func (a *app) setStore(st *bw.Store, err error) {
	if err != nil {
		a.store, a.storeErr = nil, err
		return
	}
	same := a.store != nil && a.store.Path == st.Path && a.store.ModTime.Equal(st.ModTime)
	a.store, a.storeErr = st, nil
	if same || a.vault == nil {
		return
	}
	// The desktop app synced: decrypt what changed with the key in hand.
	old := a.vault
	key := bw.SymmetricKey{Enc: slices.Clone(old.UserKey().Enc), MAC: slices.Clone(old.UserKey().MAC)}
	userID := old.UserID
	a.spawn(func() {
		v, err := st.Decrypt(userID, key)
		key.Wipe()
		a.post(func() {
			if a.vault != old {
				v.Lock() // locked, or replaced, meanwhile
				return
			}
			if err != nil {
				// The key no longer opens the vault: its keys were rotated.
				a.lock(true)
				a.lockProb = &problem{
					title:  "Your vault's keys changed",
					detail: "Unlock again to read what Bitwarden synced.",
				}
				return
			}
			a.vault, a.index = v, search.NewIndex(v)
			old.Lock()
			a.rebuild(true)
		})
	})
}

// account returns the account the user chose, as the desktop app has it,
// or what keeps it from being used.
func (a *app) account() (bw.Account, *problem) {
	if a.store == nil {
		return bw.Account{}, a.dataProblem(a.storeErr)
	}
	acc, ok := a.store.FindAccount(a.cfg.Email)
	if !ok {
		return bw.Account{}, a.notSignedIn(a.cfg.Email, a.cfg.Server)
	}
	if !acc.Server.Same(a.cfg.Server) {
		return acc, a.wrongServer(acc, a.cfg.Server, func() {
			a.cfg.Server = acc.Server
			a.changed()
			a.lockProb = nil
		})
	}
	return acc, nil
}

func (a *app) openBitwarden() {
	if err := a.desk.open(); err != nil {
		a.say("Couldn't open Bitwarden: %v", err)
	}
}

func (a *app) dataProblem(err error) *problem {
	switch {
	case err == nil:
		return &problem{title: "Reading Bitwarden's data…"}
	case errors.Is(err, bw.ErrNoData) && a.desk.installed():
		return &problem{
			title:  "Sign in to Bitwarden first",
			detail: "Bitwarden Mini reads the vault the Bitwarden app keeps on this Mac, and the app hasn't saved one yet. Open Bitwarden and sign in, then come back.",
			fix:    "Open Bitwarden", do: a.openBitwarden,
		}
	case errors.Is(err, bw.ErrNoData):
		return &problem{
			title:  "Bitwarden isn't installed",
			detail: "Bitwarden Mini is a companion to the Bitwarden desktop app. Install Bitwarden, sign in, then come back.",
			fix:    "Get Bitwarden", do: func() { a.sys.openURL("https://bitwarden.com/download/") },
		}
	case errors.Is(err, fs.ErrPermission):
		return &problem{
			title:  "Bitwarden Mini can't read Bitwarden's data",
			detail: "macOS keeps it from reading the data of the Bitwarden app. Allow it in System Settings → Privacy & Security → App Management, then try again.",
			fix:    "Try Again", do: a.reload,
		}
	}
	return &problem{
		title:  "Bitwarden's data can't be read",
		detail: err.Error(),
		fix:    "Try Again", do: a.reload,
	}
}

func (a *app) notSignedIn(email string, server bw.Server) *problem {
	detail := fmt.Sprintf("Sign in to the Bitwarden app with %s on %s first: Bitwarden Mini uses the vault the app keeps on this Mac.", email, server.Host())
	if a.store != nil {
		var others []string
		for _, acc := range a.store.Accounts() {
			others = append(others, fmt.Sprintf("%s (%s)", acc.Email, acc.Server.Host()))
		}
		if len(others) > 0 {
			detail += " Signed in now: " + strings.Join(others, ", ") + "."
		}
	}
	return &problem{
		title:  email + " isn't signed in to Bitwarden",
		detail: detail,
		fix:    "Open Bitwarden", do: a.openBitwarden,
	}
}

func (a *app) wrongServer(acc bw.Account, chosen bw.Server, use func()) *problem {
	return &problem{
		title: "Check the server region",
		detail: fmt.Sprintf("%s is an account on %s, not %s. Accounts on bitwarden.com, bitwarden.eu and self-hosted servers are separate: choose the one you signed up on.",
			acc.Email, acc.Server.Host(), chosen.Host()),
		fix: "Use " + acc.Server.Host(), do: use,
	}
}

// --- Setting up ---

var regionServers = []bw.Server{{Region: bw.RegionUS}, {Region: bw.RegionEU}, {Region: bw.RegionSelfHosted}}

// startSetup fills the setup's fields from the config, or from the account
// the desktop app shows.
func (a *app) startSetup() {
	a.page = pageSetup
	a.setupProb = nil
	a.setupEmail, a.setupURL = a.cfg.Email, a.cfg.Server.URL
	server := a.cfg.Server
	if a.setupEmail == "" && a.store != nil {
		if accs := a.store.Accounts(); len(accs) > 0 {
			a.setupEmail, server, a.setupURL = accs[0].Email, accs[0].Server, accs[0].Server.URL
		}
	}
	a.setupRegion = regionIndex(server)
}

func regionIndex(s bw.Server) int {
	switch s.Region {
	case bw.RegionEU:
		return 1
	case bw.RegionSelfHosted:
		return 2
	}
	return 0
}

func (a *app) setupServer() bw.Server {
	s := regionServers[min(max(a.setupRegion, 0), 2)]
	if s.Region == bw.RegionSelfHosted {
		u := strings.TrimRight(strings.TrimSpace(a.setupURL), "/")
		if u != "" && !strings.Contains(u, "://") {
			u = "https://" + u
		}
		s.URL = u
	}
	return s
}

// connect checks the account the user gave against the desktop app's.
func (a *app) connect() {
	email := strings.TrimSpace(a.setupEmail)
	server := a.setupServer()
	switch {
	case a.connecting:
		return
	case email == "" || !strings.Contains(email, "@"):
		a.setupProb = &problem{title: "Enter the email address of your Bitwarden account"}
		return
	case server.Region == bw.RegionSelfHosted && server.URL == "":
		a.setupProb = &problem{title: "Enter the address of your server", detail: "As https://vault.example.com, the address you sign in to Bitwarden with."}
		return
	}
	a.connecting = true
	a.spawn(func() {
		st, err := a.desk.load()
		a.post(func() {
			a.connecting = false
			a.setStore(st, err)
			if a.store == nil {
				a.setupProb = a.dataProblem(a.storeErr)
				return
			}
			acc, ok := a.store.FindAccount(email)
			if !ok {
				a.setupProb = a.notSignedIn(email, server)
				return
			}
			if !acc.Server.Same(server) {
				a.setupProb = a.wrongServer(acc, server, func() {
					a.setupRegion, a.setupURL = regionIndex(acc.Server), acc.Server.URL
					a.setupProb = nil
				})
				return
			}
			changed := !strings.EqualFold(a.cfg.Email, acc.Email)
			a.cfg.Email, a.cfg.Server = acc.Email, acc.Server
			if changed {
				a.cfg.Vault, a.cfg.Recent = "", nil
				a.lock(true)
			}
			a.changed()
			a.setupProb, a.page, a.tried = nil, pageItems, false
		})
	})
}

// --- Locking and unlocking ---

// lock locks the vault: it forgets it with hard, else keeps it for Touch
// ID when that is on.
func (a *app) lock(hard bool) {
	if a.vault == nil {
		a.locked = true
		return
	}
	if hard || !a.cfg.TouchID || !a.sys.biometrics() {
		a.vault.Lock()
		a.vault, a.index = nil, nil
	}
	a.locked = true
	a.query, a.rows, a.sel = "", nil, -1
	a.reveal, a.reprompted = map[string]bool{}, map[string]bool{}
	a.prob, a.confirm, a.flash = nil, nil, ""
	a.tried = false
	if a.page == pageEdit {
		a.page = pageItems
	}
	a.onLock()
}

// opened shows the vault, unlocked: it starts with a new search.
func (a *app) opened(v *bw.Vault) {
	if a.vault != nil && a.vault != v {
		a.vault.Lock()
	}
	a.vault, a.index, a.locked = v, search.NewIndex(v), false
	a.lockProb, a.password, a.busy = nil, "", ""
	a.query, a.sel = "", -1
	a.rebuild(false)
	a.search.Focus()
	a.onUnlock()
}

// touchIDOffered reports whether the locked page offers Touch ID: of this
// Mac, after the app locked itself, or the desktop app's.
func (a *app) touchIDOffered() bool {
	if a.vault != nil && a.cfg.TouchID && a.sys.biometrics() {
		return true
	}
	if a.store == nil {
		return a.sys.biometrics()
	}
	acc, ok := a.store.FindAccount(a.cfg.Email)
	return ok && acc.BiometricUnlock
}

// shown is called as the window shows: locked, it asks for Touch ID once.
func (a *app) shown() {
	if a.configured() && !a.unlocked() && !a.tried && a.busy == "" && a.lockProb == nil && a.page == pageItems {
		a.tried = true
		if a.touchIDOffered() {
			a.unlockTouchID()
		}
	}
}

// unlockTouchID unlocks with Touch ID: this Mac's, when the app locked
// itself and kept the vault, else through the desktop app.
func (a *app) unlockTouchID() {
	if a.busy != "" {
		return
	}
	a.lockProb = nil
	if a.vault != nil && a.cfg.TouchID && a.sys.biometrics() {
		a.busy = "touch"
		a.holdOpen++
		a.spawn(func() {
			err := a.sys.authenticate("unlock Bitwarden Mini")
			a.post(func() {
				a.busy = ""
				a.holdOpen--
				switch {
				case err == nil && a.vault != nil:
					a.opened(a.vault)
				case errors.Is(err, macos.ErrFallback):
					a.pwField.Focus()
				case errors.Is(err, macos.ErrCanceled), err == nil:
				default:
					a.lockProb = &problem{title: err.Error(), detail: "Enter your master password instead."}
				}
			})
		})
		return
	}
	acc, prob := a.account()
	if prob != nil {
		a.lockProb = prob
		return
	}
	a.busy = "touch"
	a.holdOpen++
	st := a.store
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		key, err := a.desk.unlock(ctx, acc.ID)
		var v *bw.Vault
		if err == nil {
			v, err = st.Decrypt(acc.ID, key)
			key.Wipe()
		}
		a.post(func() {
			a.busy = ""
			a.holdOpen--
			if err != nil {
				a.lockProb = a.unlockProblem(err, acc)
				return
			}
			a.opened(v)
		})
	})
}

// statusError is the desktop app's answer that it can't unlock with Touch
// ID now.
type statusError struct{ status ipc.Status }

func (e statusError) Error() string { return fmt.Sprintf("biometrics status %d", e.status) }

func (a *app) unlockProblem(err error, acc bw.Account) *problem {
	var se statusError
	switch {
	case errors.Is(err, ipc.ErrNoSocket) && !a.desk.running():
		return &problem{
			title:  "Bitwarden isn't open",
			detail: "Touch ID unlocks through the Bitwarden app. Open it, then try again, or enter your master password.",
			fix:    "Open Bitwarden", do: a.openBitwarden,
		}
	case errors.Is(err, ipc.ErrNoSocket):
		return &problem{
			title:  "Turn on browser integration in Bitwarden",
			detail: "Bitwarden Mini asks Bitwarden to unlock with Touch ID as its browser extension does. In Bitwarden's Settings, turn on “Allow browser integration”, or enter your master password.",
			fix:    "Open Bitwarden", do: a.openBitwarden,
		}
	case errors.Is(err, ipc.ErrWrongUser):
		return a.notSignedIn(acc.Email, acc.Server)
	case errors.As(err, &se):
		switch se.status {
		case ipc.StatusUnlockNeeded:
			return &problem{
				title:  "Unlock Bitwarden with your master password once",
				detail: "After it starts, Bitwarden needs your master password before Touch ID works. Enter it here, or in Bitwarden.",
			}
		case ipc.StatusHardwareUnavailable, ipc.StatusPlatformUnsupported:
			return &problem{title: "Touch ID isn't available", detail: "This Mac has no Touch ID, or no fingerprint set up. Enter your master password."}
		}
		return &problem{
			title:  "Turn on Touch ID in Bitwarden",
			detail: fmt.Sprintf("In Bitwarden's Settings, under Security, turn on “Unlock with Touch ID” for %s. Until then, enter your master password.", acc.Email),
			fix:    "Open Bitwarden", do: a.openBitwarden,
		}
	case errors.Is(err, ipc.ErrDenied):
		return &problem{title: "Bitwarden didn't unlock", detail: "Touch ID was canceled or didn't match. Try again, or enter your master password."}
	case errors.Is(err, ipc.ErrNoAnswer), errors.Is(err, context.DeadlineExceeded):
		return &problem{title: "Bitwarden didn't answer", detail: "Bring Bitwarden to the front and try again, or enter your master password.", fix: "Open Bitwarden", do: a.openBitwarden}
	case errors.Is(err, ipc.ErrDropped):
		return &problem{title: "Bitwarden refused the connection", detail: "Update Bitwarden to its latest version, then try again. Until then, enter your master password."}
	case errors.Is(err, bw.ErrUnsupported):
		return &problem{title: "This vault's encryption isn't supported yet", detail: err.Error()}
	case errors.Is(err, bw.ErrNoAccount):
		return a.notSignedIn(acc.Email, acc.Server)
	}
	return &problem{
		title:  "Bitwarden's key doesn't open the vault on this Mac",
		detail: "Sync Bitwarden (File → Sync Vault), then try again. " + err.Error(),
	}
}

// unlockPassword unlocks with the master password, with no server: it
// derives the key that the desktop app's data keeps the vault under.
func (a *app) unlockPassword() {
	if a.busy != "" || a.password == "" {
		return
	}
	acc, prob := a.account()
	if prob != nil {
		a.lockProb = prob
		return
	}
	a.busy = "password"
	a.lockProb = nil
	pw, st := a.password, a.store
	a.spawn(func() {
		key, err := st.UnlockWithPassword(acc.ID, pw)
		var v *bw.Vault
		if err == nil {
			v, err = st.Decrypt(acc.ID, key)
			key.Wipe()
		}
		a.post(func() {
			a.busy = ""
			if err == nil {
				a.opened(v)
				return
			}
			switch {
			case errors.Is(err, bw.ErrWrongPassword):
				a.lockProb = &problem{
					title: "Incorrect master password",
					detail: fmt.Sprintf("Enter the master password of %s on %s. Check that Caps Lock is off.",
						acc.Email, acc.Server.Host()),
				}
			case errors.Is(err, bw.ErrNoMasterPassword):
				a.lockProb = &problem{
					title:  "This account has no master password",
					detail: "It signs in with single sign-on. Unlock it with Touch ID: turn on “Unlock with Touch ID” in Bitwarden's Settings.",
				}
			default:
				a.lockProb = a.unlockProblem(err, acc)
			}
			a.password = ""
			a.pwField.Focus()
		})
	})
}

// --- The items ---

// vaultFilter reports whether the item is in the vault chosen.
func (a *app) inVault(it *bw.Item) bool {
	switch a.cfg.Vault {
	case "":
		return true
	case "personal":
		return it.OrganizationID == ""
	}
	return it.OrganizationID == a.cfg.Vault
}

// vaultName names the vault chosen.
func (a *app) vaultName() string {
	switch a.cfg.Vault {
	case "":
		return "All Vaults"
	case "personal":
		return "My Vault"
	}
	if a.vault != nil {
		if n := a.vault.OrganizationName(a.cfg.Vault); n != "" {
			return n
		}
	}
	return "All Vaults"
}

func (a *app) chooseVault(id string) {
	a.cfg.Vault = id
	a.changed()
	a.rebuild(false)
}

// suggestions are the items for the app the user was in, or its page.
func (a *app) suggestions() []*bw.Item {
	switch {
	case a.index == nil:
		return nil
	case a.front.url != "":
		return a.index.ForPage(a.front.url)
	case a.front.app.BundleID != "" && !macos.IsBrowser(a.front.app.BundleID):
		return a.index.ForApp(a.front.app.BundleID, a.front.app.Name)
	}
	return nil
}

// rebuild lists the rows for the search: the filters that finish what is
// typed, then the items found, or, without a search, the suggestions,
// favorites, recent items and all the others. keep keeps the item chosen,
// else the first row is.
func (a *app) rebuild(keep bool) {
	var keepID string
	if it := a.selected(); keep && it != nil {
		keepID = it.ID
	}
	a.rows = nil
	if a.index == nil {
		a.sel = -1
		return
	}
	if comps := a.index.Complete(a.query); len(comps) > 0 {
		a.rows = append(a.rows, row{header: "Filters"})
		for i := range comps {
			a.rows = append(a.rows, row{comp: &comps[i]})
		}
	}
	q := search.Parse(a.query)
	seen := map[string]bool{}
	section := func(title string, items []*bw.Item, limit int) {
		n := 0
		for _, it := range items {
			if seen[it.ID] || !a.inVault(it) || it.Archived {
				continue
			}
			if n == 0 && title != "" {
				a.rows = append(a.rows, row{header: title})
			}
			seen[it.ID] = true
			a.rows = append(a.rows, row{item: it})
			if n++; limit > 0 && n == limit {
				return
			}
		}
	}
	if q.Empty() {
		section("Suggestions", a.suggestions(), 6)
		var favs, recent []*bw.Item
		for _, it := range a.vault.Items {
			if it.Favorite {
				favs = append(favs, it)
			}
		}
		for _, id := range a.cfg.Recent {
			if it := a.vault.Item(id); it != nil {
				recent = append(recent, it)
			}
		}
		section("Favorites", favs, 0)
		section("Recently Used", recent, 6)
		section("All Items", a.vault.Items, 0)
	} else {
		suggested := map[string]bool{}
		for _, it := range a.suggestions() {
			suggested[it.ID] = true
		}
		title := ""
		if len(a.rows) > 0 {
			title = "Items"
		}
		section(title, a.index.Run(q, func(it *bw.Item) int {
			b := 0
			if suggested[it.ID] {
				b += 20
			}
			if i := slices.Index(a.cfg.Recent, it.ID); i >= 0 {
				b += 10 - min(i, 9)
			}
			return b
		}), 0)
	}
	a.sel = -1
	for i, r := range a.rows {
		if keepID != "" && r.item != nil && r.item.ID == keepID {
			a.sel = i
			break
		}
	}
	if a.sel < 0 {
		a.sel = a.next(-1, 1)
	}
	if a.sel >= 0 {
		a.list.ScrollIntoView(a.sel)
	}
}

// next returns the row after i that can be chosen, going by step, or i.
func (a *app) next(i, step int) int {
	for j := i + step; j >= 0 && j < len(a.rows); j += step {
		if a.rows[j].header == "" {
			return j
		}
	}
	return i
}

// move moves the choice by n rows.
func (a *app) move(n int) {
	i := a.sel
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	for range n {
		i = a.next(i, step)
	}
	a.choose(i)
}

func (a *app) choose(i int) {
	if i < 0 || i >= len(a.rows) || a.rows[i].header != "" {
		return
	}
	if i != a.sel {
		a.sel = i
		a.prob = nil
	}
	a.list.ScrollIntoView(i)
}

// selected returns the item chosen, or nil.
func (a *app) selected() *bw.Item {
	if a.sel >= 0 && a.sel < len(a.rows) {
		return a.rows[a.sel].item
	}
	return nil
}

func (a *app) queryChanged() {
	a.prob = nil
	a.rebuild(false)
}

// escape clears what is typed, else closes what is open, else hides the
// window.
func (a *app) escape() {
	switch {
	case a.confirm != nil:
		a.closeConfirm()
	case a.prob != nil:
		a.prob = nil
	case a.page == pageSettings && a.recording:
		a.recording = false
	case a.page == pageSettings && a.configured():
		a.page = pageItems
	case a.page == pageEdit:
		a.page = pageItems
	case a.page == pageSetup && a.configured():
		a.page = pageSettings
	case a.query != "" && a.unlocked():
		a.query = ""
		a.rebuild(false)
	default:
		a.sys.hide()
	}
}

// activate does what Return does: finishes a filter, fills a login, or
// copies what an item holds first.
func (a *app) activate() {
	if a.sel < 0 || a.sel >= len(a.rows) {
		return
	}
	r := a.rows[a.sel]
	switch {
	case r.comp != nil:
		a.query, a.caretEnd = r.comp.Query, true
		a.rebuild(false)
		a.search.Focus()
	case r.item != nil && r.item.Login != nil && r.item.Login.Password != "" && a.front.app.PID != 0:
		a.fill(r.item, false)
	case r.item != nil:
		a.copyPrimary()
	}
}

// field is a value of an item, as the details show it and copy it.
type field struct {
	label  string
	value  string
	secret bool
	kind   string // "totp", "uri", "note", "" for text
	uri    string
}

// primary is what Cmd-C copies: a login's username, a card's number, an
// identity's email, a note, a key's public half.
func primary(it *bw.Item) (field, bool) {
	switch {
	case it.Login != nil && it.Login.Username != "":
		return field{label: "username", value: it.Login.Username}, true
	case it.Card != nil && it.Card.Number != "":
		return field{label: "card number", value: it.Card.Number, secret: true}, true
	case it.Identity != nil && it.Identity.Email != "":
		return field{label: "email", value: it.Identity.Email}, true
	case it.Identity != nil && it.Identity.FullName() != "":
		return field{label: "name", value: it.Identity.FullName()}, true
	case it.SSHKey != nil && it.SSHKey.PublicKey != "":
		return field{label: "public key", value: it.SSHKey.PublicKey}, true
	case it.Type == bw.TypeSecureNote && it.Notes != "":
		return field{label: "note", value: it.Notes, secret: true}, true
	}
	return field{}, false
}

// secretOf is what Shift-Cmd-C copies: a login's password, a card's
// security code, a key's private half.
func secretOf(it *bw.Item) (field, bool) {
	switch {
	case it.Login != nil && it.Login.Password != "":
		return field{label: "password", value: it.Login.Password, secret: true}, true
	case it.Card != nil && it.Card.Code != "":
		return field{label: "security code", value: it.Card.Code, secret: true}, true
	case it.SSHKey != nil && it.SSHKey.PrivateKey != "":
		return field{label: "private key", value: it.SSHKey.PrivateKey, secret: true}, true
	}
	return field{}, false
}

// code returns the one-time password of a login now, and how long it
// lasts.
func (a *app) code(it *bw.Item) (string, time.Duration, bool) {
	if it == nil || it.Login == nil || it.Login.TOTP == "" {
		return "", 0, false
	}
	k, err := totp.Parse(it.Login.TOTP)
	if err != nil {
		return "", 0, false
	}
	now := a.now()
	return k.Code(now), k.Remaining(now), true
}

// guarded runs fn at once, or after the master password when the item
// asks for it, as Bitwarden does before showing its secrets.
func (a *app) guarded(it *bw.Item, fn func()) {
	if !it.Reprompt || a.reprompted[it.ID] {
		fn()
		return
	}
	a.confirm, a.confirmItem, a.confirmPw, a.confirmErr = fn, it.ID, "", ""
	a.confirmH.Focus()
}

// confirmPassword checks the master password for an item that asks for
// it.
func (a *app) confirmPassword() {
	if a.confirm == nil || a.confirmPw == "" || a.busy != "" {
		return
	}
	acc, prob := a.account()
	if prob != nil {
		a.confirmErr = prob.title
		return
	}
	pw, st, fn, id := a.confirmPw, a.store, a.confirm, a.confirmItem
	a.busy = "confirm"
	a.spawn(func() {
		key, err := st.UnlockWithPassword(acc.ID, pw)
		key.Wipe()
		a.post(func() {
			a.busy = ""
			if a.confirm == nil {
				return
			}
			if err != nil {
				a.confirmErr = "Incorrect master password"
				a.confirmPw = ""
				return
			}
			a.reprompted[id] = true
			a.closeConfirm()
			fn()
		})
	})
}

// closeConfirm closes the master password's prompt, and gives the
// keyboard back to the search.
func (a *app) closeConfirm() {
	a.confirm, a.confirmPw, a.confirmErr = nil, "", ""
	a.search.Focus()
}

// copyField copies a value of an item.
func (a *app) copyField(it *bw.Item, f field) {
	do := func() {
		a.sys.copy(f.value, f.secret)
		a.cfg.used(it.ID)
		a.changed()
		if f.secret && a.cfg.ClearClipboard > 0 {
			a.say("Copied %s of %s · clears in %s", f.label, it.Name, seconds(a.cfg.ClearClipboard))
		} else {
			a.say("Copied %s of %s", f.label, it.Name)
		}
	}
	if f.secret {
		a.guarded(it, do)
		return
	}
	do()
}

func (a *app) copyPrimary() {
	it := a.selected()
	if it == nil {
		return
	}
	if f, ok := primary(it); ok {
		a.copyField(it, f)
		return
	}
	a.say("%s has nothing to copy", it.Name)
}

func (a *app) copyPassword() {
	it := a.selected()
	if it == nil {
		return
	}
	if f, ok := secretOf(it); ok {
		a.copyField(it, f)
		return
	}
	a.say("%s has no password", it.Name)
}

func (a *app) copyCode() {
	it := a.selected()
	if it == nil {
		return
	}
	code, _, ok := a.code(it)
	if !ok {
		a.say("%s has no one-time password", it.Name)
		return
	}
	a.copyField(it, field{label: "one-time password", value: code, secret: true})
}

// launchURL is the first website of a login, as a URL to open.
func launchURL(it *bw.Item) string {
	if it == nil || it.Login == nil {
		return ""
	}
	for _, u := range it.Login.URIs {
		s := strings.TrimSpace(u.URI)
		switch {
		case s == "":
		case strings.HasPrefix(s, "http://"), strings.HasPrefix(s, "https://"):
			return s
		case !strings.Contains(s, "://") && strings.Contains(s, "."):
			return "https://" + s
		}
	}
	return ""
}

// fill types the login into the app the user was in, or, with open, into
// its website, which it opens first.
func (a *app) fill(it *bw.Item, open bool) {
	if it == nil || it.Login == nil {
		return
	}
	var page string
	if open {
		if page = launchURL(it); page == "" {
			a.say("%s has no website to open", it.Name)
			return
		}
	}
	a.guarded(it, func() {
		if !a.sys.trusted(false) {
			a.prob = &problem{
				title:  "Allow Bitwarden Mini to fill in other apps",
				detail: "To type your username and password, Bitwarden Mini needs Accessibility access: System Settings → Privacy & Security → Accessibility.",
				fix:    "Open System Settings",
				do: func() {
					a.sys.trusted(true)
					a.sys.openURL(macos.AccessibilitySettingsURL)
				},
			}
			return
		}
		target := a.front.app
		l := *it.Login
		name := it.Name
		a.cfg.used(it.ID)
		a.changed()
		a.sys.hide()
		code, _, hasCode := a.code(it)
		a.spawn(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err := a.sys.fill(ctx, target, fillRequest{username: l.Username, password: l.Password, page: page})
			a.post(func() {
				if err != nil {
					a.sys.copy(l.Password, true)
					where := target.Name
					if open || where == "" {
						where = "the page"
					}
					a.prob = &problem{
						title:  "Couldn't fill " + where,
						detail: fmt.Sprintf("No username or password field had the keyboard focus. The password of %s is on the clipboard: click the field and paste it.", name),
					}
					a.sys.show()
					return
				}
				if hasCode {
					// For the page that asks for it next, as Bitwarden
					// does after it fills.
					a.sys.copy(code, true)
				}
			})
		})
	})
}

func (a *app) openDetail() {
	if it := a.selected(); it != nil {
		a.sys.openDetail(it.ID)
	}
}

func (a *app) openInBitwarden() {
	it := a.selected()
	a.openBitwarden()
	a.sys.hide()
	if it != nil {
		a.say("Opened Bitwarden: find %s there", it.Name)
	}
}

// setFront records the app the user was in, and asks a browser for its
// page.
func (a *app) setFront(app macos.App) {
	a.front = front{app: app}
	if a.unlocked() && a.query == "" {
		a.rebuild(true)
	}
	if !macos.IsBrowser(app.BundleID) {
		return
	}
	a.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		u := a.sys.pageURL(ctx, app)
		a.post(func() {
			if a.front.app.PID != app.PID || u == "" {
				return
			}
			a.front.url = u
			if a.unlocked() && a.query == "" {
				a.rebuild(false)
			}
		})
	})
}

func seconds(n int) string {
	if n%60 == 0 {
		return fmt.Sprintf("%d min", n/60)
	}
	return fmt.Sprintf("%d s", n)
}
