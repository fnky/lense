package main

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bitwarden-mini/internal/bw"
	"bitwarden-mini/internal/bw/bwtest"
	"bitwarden-mini/internal/ipc"
	"bitwarden-mini/internal/macos"

	"github.com/egoist/mygo/ui"
)

// fakeDesktop is the Bitwarden app with the fixture's account, which
// unlocks with Touch ID unless told otherwise.
type fakeDesktop struct {
	t         *testing.T
	f         *bwtest.Fixture
	path      string
	unlockErr error
	unlocks   int
	notRun    bool
	opened    int
}

func (d *fakeDesktop) load() (*bw.Store, error) {
	if d.path == "" {
		return nil, bw.ErrNoData
	}
	return bw.Open(d.path)
}

func (d *fakeDesktop) unlock(ctx context.Context, userID string) (bw.SymmetricKey, error) {
	d.unlocks++
	if d.unlockErr != nil {
		return bw.SymmetricKey{}, d.unlockErr
	}
	if userID != d.f.UserID {
		return bw.SymmetricKey{}, ipc.ErrWrongUser
	}
	return bw.NewSymmetricKey(d.f.UserKey.Bytes())
}

func (d *fakeDesktop) running() bool   { return !d.notRun }
func (d *fakeDesktop) installed() bool { return true }
func (d *fakeDesktop) open() error     { d.opened++; return nil }

// fakeSystem is macOS: it records what was copied, filled and opened.
type fakeSystem struct {
	mu       sync.Mutex
	copied   []string
	secret   []bool
	filled   []fillRequest
	fillErr  error
	opened   []string
	details  []string
	hidden   int
	shown    int
	bio      bool
	authErr  error
	auths    int
	trust    bool
	field    bool // a text field has the focus in the app the user was in
	pageURLs map[string]string
}

func (s *fakeSystem) copy(text string, secret bool) {
	s.copied = append(s.copied, text)
	s.secret = append(s.secret, secret)
}
func (s *fakeSystem) openURL(u string)     { s.opened = append(s.opened, u) }
func (s *fakeSystem) biometrics() bool     { return s.bio }
func (s *fakeSystem) trusted(bool) bool    { return s.trust }
func (s *fakeSystem) show()                { s.shown++ }
func (s *fakeSystem) hide()                { s.hidden++ }
func (s *fakeSystem) openDetail(id string) { s.details = append(s.details, id) }
func (s *fakeSystem) authenticate(string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auths++
	return s.authErr
}
func (s *fakeSystem) fill(_ context.Context, _ macos.App, req fillRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filled = append(s.filled, req)
	return s.fillErr
}
func (s *fakeSystem) pageURL(_ context.Context, b macos.App) string { return s.pageURLs[b.BundleID] }
func (s *fakeSystem) fieldFocused(macos.App) bool                   { return s.field }

func (s *fakeSystem) last() string {
	if len(s.copied) == 0 {
		return ""
	}
	return s.copied[len(s.copied)-1]
}

// runner stands in for the main thread: work runs in goroutines, and what
// it posts runs as settle drains it.
type runner struct {
	mu    sync.Mutex
	wg    sync.WaitGroup
	queue []func()
}

func (r *runner) spawn(fn func()) {
	r.wg.Add(1)
	go func() { defer r.wg.Done(); fn() }()
}

func (r *runner) post(fn func()) {
	r.mu.Lock()
	r.queue = append(r.queue, fn)
	r.mu.Unlock()
}

type harness struct {
	t    *testing.T
	a    *app
	desk *fakeDesktop
	sys  *fakeSystem
	r    *runner
	tt   *ui.Tester
	f    *bwtest.Fixture
}

// newHarness is the app, its account set up, on the sample vault, at 15:20
// on Oct 19, 2026.
func newHarness(t *testing.T, f *bwtest.Fixture, configured bool) *harness {
	t.Helper()
	if f == nil {
		f = bwtest.Sample()
	}
	cfg := defaultConfig()
	if configured {
		cfg.Email, cfg.Server = f.Email, f.Server
	}
	h := &harness{t: t, f: f, r: &runner{}}
	h.desk = &fakeDesktop{t: t, f: f, path: f.Write(t)}
	h.sys = &fakeSystem{bio: true, trust: true}
	h.a = newApp(cfg, h.desk, h.sys)
	h.a.version = "test"
	h.a.now = func() time.Time { return time.Date(2026, 10, 19, 15, 20, 0, 0, time.UTC) }
	h.a.spawn, h.a.post = h.r.spawn, h.r.post
	h.a.reload()
	h.settle()
	h.tt = ui.NewTester(h.a.view, windowWidth, windowHeight)
	h.tt.SetScale(2)
	return h
}

// settle waits for the app's work, and what it starts, then draws.
func (h *harness) settle() {
	for {
		h.r.wg.Wait()
		h.r.mu.Lock()
		q := h.r.queue
		h.r.queue = nil
		h.r.mu.Unlock()
		if len(q) == 0 {
			break
		}
		for _, fn := range q {
			fn()
		}
	}
	if h.tt != nil {
		// Focus that moves as a frame ends asks for another, which the
		// window draws at once.
		h.tt.Frame()
		h.tt.Frame()
	}
}

func (h *harness) has(texts ...string) {
	h.t.Helper()
	for _, s := range texts {
		if !h.tt.HasText(s) {
			h.t.Errorf("no %q in %q", s, h.tt.Texts())
		}
	}
}

func (h *harness) click(label string) {
	h.t.Helper()
	if err := h.tt.Click(label); err != nil {
		h.t.Fatalf("click %q: %v in %q", label, err, h.tt.Texts())
	}
	h.settle()
}

func (h *harness) key(mods ui.Modifiers, k ui.Key) {
	h.tt.Key(mods, k)
	h.settle()
}

// unlock unlocks with the master password.
func (h *harness) unlock() {
	h.t.Helper()
	h.a.password = h.f.Password
	h.a.unlockPassword()
	h.settle()
	if !h.a.unlocked() {
		h.t.Fatalf("locked: %+v", h.a.lockProb)
	}
}

// snapshot writes the frame to $BWMINI_SNAPSHOTS, to look at.
func snapshot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("BWMINI_SNAPSHOTS")
	if dir == "" {
		return
	}
	tt.Frame()
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestSetup(t *testing.T) {
	h := newHarness(t, nil, false)
	h.a.startSetup()
	h.settle()
	h.has("Connect to Bitwarden", "Signed in to Bitwarden", "ada@example.com")
	snapshot(t, h.tt, "setup")

	// An account the desktop app doesn't have.
	h.a.setupEmail = "grace@example.com"
	h.click("Continue")
	if h.a.setupProb == nil || !strings.Contains(h.a.setupProb.title, "isn't signed in") {
		t.Fatalf("problem %+v", h.a.setupProb)
	}
	h.has("grace@example.com isn't signed in to Bitwarden", "Open Bitwarden")
	h.click("Open Bitwarden")
	if h.desk.opened != 1 {
		t.Error("Bitwarden didn't open")
	}

	// The right account on the wrong server.
	h.a.setupEmail, h.a.setupRegion = "ada@example.com", 1
	h.click("Continue")
	h.has("Check the server region", "Use bitwarden.com")
	snapshot(t, h.tt, "setup-region")
	h.click("Use bitwarden.com")
	if h.a.setupRegion != 0 {
		t.Fatalf("region %d", h.a.setupRegion)
	}
	h.click("Continue")
	if !h.a.configured() || h.a.page != pageItems || h.a.cfg.Server.Region != bw.RegionUS {
		t.Fatalf("not set up: %+v %+v", h.a.cfg, h.a.setupProb)
	}
	h.has("Bitwarden Mini is locked", "ada@example.com · bitwarden.com")
}

func TestServerChangedSince(t *testing.T) {
	h := newHarness(t, nil, true)
	h.a.cfg.Server = bw.Server{Region: bw.RegionEU}
	h.a.password = h.f.Password
	h.a.unlockPassword()
	h.settle()
	h.has("Check the server region", "Use bitwarden.com")
	h.click("Use bitwarden.com")
	h.a.password = h.f.Password
	h.a.unlockPassword()
	h.settle()
	if !h.a.unlocked() {
		t.Fatalf("locked: %+v", h.a.lockProb)
	}
}

func TestUnlockWithPassword(t *testing.T) {
	h := newHarness(t, nil, true)
	h.has("Bitwarden Mini is locked", "Unlock with Touch ID")
	snapshot(t, h.tt, "locked")
	h.a.password = "not it"
	h.a.unlockPassword()
	h.settle()
	h.has("Incorrect master password")
	snapshot(t, h.tt, "locked-wrong-password")
	if h.a.password != "" {
		t.Error("the wrong password stays")
	}
	h.unlock()
	h.has("GitHub", "Visa", "FAVORITES", "ALL ITEMS")
	snapshot(t, h.tt, "items")
	h.tt.SetDark(true)
	snapshot(t, h.tt, "items-dark")
}

func TestUnlockWithTouchID(t *testing.T) {
	h := newHarness(t, nil, true)
	h.a.shown() // the window shows, locked: it asks the desktop app
	h.settle()
	if h.desk.unlocks != 1 || !h.a.unlocked() {
		t.Fatalf("unlocks %d, problem %+v", h.desk.unlocks, h.a.lockProb)
	}

	// Locked by the app itself, Touch ID of this Mac shows it again,
	// without the desktop app.
	h.a.query = "git"
	h.a.lock(false)
	h.settle()
	if h.a.vault == nil {
		t.Fatal("the vault was forgotten")
	}
	h.click("Unlock with Touch ID")
	if h.sys.auths != 1 || h.desk.unlocks != 1 || !h.a.unlocked() {
		t.Fatalf("auths %d, unlocks %d", h.sys.auths, h.desk.unlocks)
	}
	if h.a.query != "" {
		t.Error("the search survived the lock")
	}

	// Locked for good, it asks the desktop app again.
	h.a.lock(true)
	h.settle()
	if h.a.vault != nil {
		t.Fatal("the vault was kept")
	}
	h.click("Unlock with Touch ID")
	if h.desk.unlocks != 2 || !h.a.unlocked() {
		t.Fatalf("unlocks %d", h.desk.unlocks)
	}
}

func TestTouchIDProblems(t *testing.T) {
	for _, tc := range []struct {
		err    error
		notRun bool
		want   string
	}{
		{ipc.ErrNoSocket, true, "Bitwarden isn't open"},
		{ipc.ErrNoSocket, false, "Turn on browser integration in Bitwarden"},
		{statusError{ipc.StatusNotEnabledLocally}, false, "Turn on Touch ID in Bitwarden"},
		{statusError{ipc.StatusUnlockNeeded}, false, "Unlock Bitwarden with your master password once"},
		{ipc.ErrDenied, false, "Bitwarden didn't unlock"},
		{ipc.ErrWrongUser, false, "ada@example.com isn't signed in to Bitwarden"},
		{ipc.ErrNoAnswer, false, "Bitwarden didn't answer"},
	} {
		h := newHarness(t, nil, true)
		h.desk.unlockErr, h.desk.notRun = tc.err, tc.notRun
		h.click("Unlock with Touch ID")
		if h.a.unlocked() {
			t.Fatal("unlocked")
		}
		h.has(tc.want)
		if tc.notRun {
			snapshot(t, h.tt, "locked-not-running")
		}
	}
}

func TestSearchAndKeys(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("git")
	h.settle()
	if it := h.a.selected(); it == nil || it.Name != "GitHub" {
		t.Fatalf("selected %v, rows %d", it, len(h.a.rows))
	}
	h.has("username", "ada", "password", "one-time password", "github.com/login")
	snapshot(t, h.tt, "search")

	h.key(ui.Cmd, ui.KeyC)
	if h.sys.last() != "ada" || h.sys.secret[len(h.sys.secret)-1] {
		t.Errorf("⌘C copied %q", h.sys.copied)
	}
	h.key(ui.Cmd|ui.Shift, ui.KeyC)
	if h.sys.last() != "gh-s3cret!" || !h.sys.secret[len(h.sys.secret)-1] {
		t.Errorf("⇧⌘C copied %q", h.sys.copied)
	}
	h.key(ui.Cmd|ui.Alt, ui.KeyC)
	if code, _, _ := h.a.code(h.a.selected()); h.sys.last() != code || len(code) != 6 {
		t.Errorf("⌥⌘C copied %q, code %q", h.sys.last(), code)
	}
	h.has("Copied one-time password of GitHub · clears in 90 s")
	if h.a.cfg.Recent[0] != h.a.selected().ID {
		t.Errorf("recent %v", h.a.cfg.Recent)
	}

	// Return fills the app the user was in.
	h.a.front = front{app: macos.App{PID: 42, Name: "Safari", BundleID: "com.apple.Safari"}}
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 1 || h.sys.filled[0].username != "ada" || h.sys.filled[0].password != "gh-s3cret!" || h.sys.filled[0].page != "" {
		t.Fatalf("filled %+v", h.sys.filled)
	}
	// Shift-Return opens its website, and fills it.
	h.key(ui.Shift, ui.KeyEnter)
	if len(h.sys.filled) != 2 || h.sys.filled[1].page != "https://github.com/login" {
		t.Fatalf("filled %+v", h.sys.filled)
	}
	h.key(ui.Cmd, ui.KeyO)
	if len(h.sys.details) != 1 {
		t.Error("⌘O opened no window")
	}
	h.key(ui.Cmd|ui.Shift, ui.KeyO)
	if h.desk.opened != 1 {
		t.Error("⇧⌘O didn't open Bitwarden")
	}

	// Escape clears the search, then hides the window.
	hidden := h.sys.hidden
	h.key(0, ui.KeyEscape)
	if h.a.query != "" || h.sys.hidden != hidden {
		t.Fatalf("query %q, hidden %d", h.a.query, h.sys.hidden-hidden)
	}
	h.key(0, ui.KeyEscape)
	if h.sys.hidden != hidden+1 {
		t.Error("escape didn't hide")
	}
}

func TestArrowsMoveTheChoice(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	first := h.a.selected()
	h.key(0, ui.KeyDown)
	second := h.a.selected()
	if first == nil || second == nil || first == second {
		t.Fatalf("%v then %v", first, second)
	}
	h.key(0, ui.KeyUp)
	if h.a.selected() != first {
		t.Error("up didn't come back")
	}
	h.key(0, ui.KeyUp)
	if h.a.selected() != first {
		t.Error("up went past the first row")
	}
}

func TestFilters(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("=ca")
	h.settle()
	h.has("=category:", "FILTERS")
	snapshot(t, h.tt, "filters")
	h.key(0, ui.KeyEnter) // finishes the filter
	if h.a.query != "=category:" {
		t.Fatalf("query %q", h.a.query)
	}
	h.tt.Type("c")
	h.settle()
	h.key(0, ui.KeyEnter)
	if h.a.query != "=category:Card " {
		t.Fatalf("query %q", h.a.query)
	}
	if it := h.a.selected(); it == nil || it.Name != "Visa" {
		t.Fatalf("selected %v", it)
	}
	h.has("4111 1111 1111 1111"[:0]+"cardholder name", "Ada Lovelace")
}

func TestVaults(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.a.chooseVault(h.f.OrgID)
	h.settle()
	for _, r := range h.a.rows {
		if r.item != nil && r.item.OrganizationID != h.f.OrgID {
			t.Errorf("%s of another vault", r.item.Name)
		}
	}
	h.has("Acme Inc.", "AWS Console", "Deploy key")
	h.a.chooseVault("personal")
	h.settle()
	if h.tt.HasText("AWS Console") {
		t.Error("an organization's item in My Vault")
	}
}

func TestSuggestions(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.sys.pageURLs = map[string]string{"com.apple.Safari": "https://github.com/pulls"}
	h.a.setFront(macos.App{PID: 7, Name: "Safari", BundleID: "com.apple.Safari"})
	h.settle()
	if len(h.a.rows) < 2 || h.a.rows[0].header != "Suggestions" || h.a.rows[1].item.Name != "GitHub" {
		t.Fatalf("rows %+v", h.a.rows[:2])
	}
	h.has("SUGGESTIONS", "Fill in Safari")
}

func TestReprompt(t *testing.T) {
	f := bwtest.Sample()
	f.Add(bw.Item{Type: bw.TypeLogin, Name: "Bank", Reprompt: true, Login: &bw.Login{Username: "ada", Password: "b4nk"}}, false)
	h := newHarness(t, f, true)
	h.unlock()
	h.tt.Type("bank")
	h.settle()
	h.key(ui.Cmd|ui.Shift, ui.KeyC)
	if h.a.confirm == nil || len(h.sys.copied) != 0 {
		t.Fatalf("copied %q without the master password", h.sys.copied)
	}
	h.has("Enter your master password")
	snapshot(t, h.tt, "reprompt")
	h.a.confirmPw = "wrong"
	h.a.confirmPassword()
	h.settle()
	h.has("Incorrect master password")
	h.a.confirmPw = f.Password
	h.a.confirmPassword()
	h.settle()
	if h.sys.last() != "b4nk" {
		t.Fatalf("copied %q", h.sys.copied)
	}
	// Once is enough until it locks.
	h.key(ui.Cmd|ui.Shift, ui.KeyC)
	if h.a.confirm != nil || len(h.sys.copied) != 2 {
		t.Error("asked again")
	}
}

func TestFillNeedsAccessibility(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.sys.trust, h.sys.field = false, true
	h.a.front = front{app: macos.App{PID: 42, Name: "Slack"}}
	h.tt.Type("git")
	h.settle()
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 0 {
		t.Fatal("filled without access")
	}
	h.has("Allow Bitwarden Mini to fill in other apps", "Open System Settings")
	snapshot(t, h.tt, "fill-access")
}

func TestFillFails(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.sys.fillErr, h.sys.field = errNoField, true
	h.a.front = front{app: macos.App{PID: 42, Name: "Slack"}}
	h.tt.Type("git")
	h.settle()
	h.key(0, ui.KeyEnter)
	if h.sys.shown != 1 || h.sys.last() != "gh-s3cret!" {
		t.Fatalf("shown %d, copied %q", h.sys.shown, h.sys.copied)
	}
	h.has("Couldn't fill Slack")
}

func TestStoreChangesWhileUnlocked(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("git")
	h.settle()
	h.f.Add(bw.Item{Type: bw.TypeLogin, Name: "GitLab", Login: &bw.Login{Username: "ada"}}, false)
	if err := os.WriteFile(h.desk.path, h.f.JSON(), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	os.Chtimes(h.desk.path, later, later)
	h.a.reload()
	h.settle()
	h.has("GitLab")
	if it := h.a.selected(); it == nil || it.Name != "GitHub" {
		t.Errorf("the choice moved to %v", it)
	}
}

func TestNoData(t *testing.T) {
	h := newHarness(t, nil, true)
	h.desk.path = ""
	h.a.reload()
	h.settle()
	h.click("Unlock with Touch ID")
	h.has("Sign in to Bitwarden first")
}

func TestSettings(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.SetSize(windowWidth, 2400) // all of it, unscrolled
	h.key(ui.Cmd, ui.KeyComma)
	h.has("Settings", "Lock when idle", "Unlock with Touch ID", "Clear copied secrets", "⇧⌘Space", "Accessibility access",
		"Keyboard shortcuts", "Copy the username", "⌘C", "⌘B", "Search My Vault")
	snapshot(t, h.tt, "settings")

	// The global shortcut, recorded.
	h.click("Show Bitwarden Mini key")
	if h.a.recordID != "show" {
		t.Fatalf("recording %q", h.a.recordID)
	}
	h.key(ui.Cmd|ui.Alt, ui.KeyK)
	if h.a.cfg.Shortcut != "Option+Cmd+K" || h.a.recordID != "" {
		t.Fatalf("shortcut %q", h.a.cfg.Shortcut)
	}
	h.has("⌥⌘K")
	h.a.setShortcut = func(string) error { return errors.New("taken") }
	h.click("Show Bitwarden Mini key")
	h.key(ui.Cmd, ui.KeyJ)
	if h.a.cfg.Shortcut != "Option+Cmd+K" {
		t.Errorf("a taken shortcut was saved")
	}
	h.has("⌘J is taken by another app. Try another.")
	h.key(0, ui.KeyEscape) // stops recording
	h.key(0, ui.KeyEscape) // leaves the settings
	if h.a.page != pageItems {
		t.Errorf("page %v", h.a.page)
	}
}

func TestCustomKeys(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.SetSize(windowWidth, 2400)
	h.key(ui.Cmd, ui.KeyComma)

	// Another key for copying the password, which Shift-Cmd-C still does.
	h.a.recordID, h.a.recordIndex = "copy-password", 1
	h.settle()
	h.key(ui.Cmd, ui.KeyP)
	if got := h.a.cfg.keysOf("copy-password"); len(got) != 2 || got[1] == "" {
		t.Fatalf("keys %q", got)
	}
	// A key that another action does is refused.
	h.click("Copy the password key")
	h.key(ui.Cmd, ui.KeyO)
	h.has("⌘O already does “Open the item in a window”.")
	h.key(0, ui.KeyEscape)
	// Delete takes a key out.
	h.a.recordID, h.a.recordIndex = "copy-username", 1
	h.settle()
	h.key(0, ui.KeyBackspace)
	if got := h.a.cfg.keysOf("copy-username"); len(got) != 1 || got[0] != "Cmd+C" {
		t.Fatalf("keys %q", got)
	}
	// Letters need a modifier: they type into the search.
	h.a.recordID, h.a.recordIndex = "lock", 0
	h.settle()
	h.key(0, ui.KeyQ)
	h.has("Hold ⌘, ⌃ or ⌥ with that key.")
	h.key(0, ui.KeyEscape)

	h.key(0, ui.KeyEscape)
	h.tt.Type("git")
	h.settle()
	h.key(ui.Cmd, ui.KeyP)
	if h.sys.last() != "gh-s3cret!" {
		t.Fatalf("⌘P copied %q", h.sys.copied)
	}
	h.key(ui.Cmd, ui.KeyB) // taken out
	if h.sys.last() != "gh-s3cret!" {
		t.Errorf("⌘B copied %q", h.sys.last())
	}
	h.a.resetKeys()
	if h.a.cfg.Keys != nil {
		t.Errorf("keys %v", h.a.cfg.Keys)
	}
}

func TestShortcuts(t *testing.T) {
	for _, tc := range []struct {
		mods ui.Modifiers
		key  ui.Key
		want string
		ok   bool
	}{
		{ui.Super | ui.Shift, ui.KeySpace, "Shift+Cmd+Space", true},
		{ui.Ctrl | ui.Alt, ui.KeyP, "Ctrl+Option+P", true},
		{0, ui.KeyF5, "F5", true},
		{0, ui.KeyF11, "F11", true},
		{ui.Shift, ui.KeyA, "", false},
		{0, ui.KeyA, "", false},
	} {
		got, ok := accelerator(tc.mods, tc.key)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%v %v: %q %v", tc.mods, tc.key, got, ok)
		}
	}
	if s := keySymbols("Cmd+Shift+Space"); s != "⇧⌘Space" {
		t.Errorf("symbols %q", s)
	}
	if s := keySymbols("Ctrl+Option+Return"); s != "⌃⌥↩" {
		t.Errorf("symbols %q", s)
	}
}

func TestPlacement(t *testing.T) {
	var p placements
	laptop := rect{0, 25, 1512, 920}
	wide := rect{1512, 0, 2560, 1415}
	x, y := p.place(1, laptop, windowWidth, windowHeight)
	if x != (1512-windowWidth)/2 || y != 25+(920-windowHeight)/5 {
		t.Fatalf("first %d,%d", x, y)
	}
	// Moved on the laptop's display, it comes back there.
	p.remember(1, laptop, rect{100, 200, windowWidth, windowHeight})
	if x, y := p.place(1, laptop, windowWidth, windowHeight); x != 100 || y != 200 {
		t.Fatalf("laptop %d,%d", x, y)
	}
	// On another display, it takes the same place relative to the middle.
	x, y = p.place(2, wide, windowWidth, windowHeight)
	if x != 1512+2560/2+(100+windowWidth/2-1512/2)-windowWidth/2 || y != 175 {
		t.Fatalf("wide %d,%d", x, y)
	}
	// It stays on displays too small for that.
	small := rect{0, 0, 800, 500}
	x, y = p.place(3, small, windowWidth, windowHeight)
	if x < 0 || x+windowWidth > 800 || y < 0 || y+windowHeight > 500 {
		t.Fatalf("small %d,%d", x, y)
	}
}

func TestReturnWithoutAFieldCopies(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.a.front = front{app: macos.App{PID: 42, Name: "Notes", BundleID: "com.apple.Notes"}}
	h.tt.Type("git")
	h.settle()
	h.has("Copy password")
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 0 || h.sys.last() != "gh-s3cret!" {
		t.Fatalf("filled %v, copied %q", h.sys.filled, h.sys.copied)
	}
	// With a field to fill, Return fills it.
	h.sys.field, h.a.flash = true, ""
	h.settle()
	h.has("Fill in Notes")
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 1 {
		t.Fatalf("filled %v", h.sys.filled)
	}
}

func TestDetailsKeys(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("git")
	h.settle()
	h.key(0, ui.KeyRight)
	if h.a.mode != modeDetails || h.a.fieldSel != 0 {
		t.Fatalf("mode %v, field %d", h.a.mode, h.a.fieldSel)
	}
	snapshot(t, h.tt, "details")
	h.key(0, ui.KeyEnter)
	if h.sys.last() != "ada" {
		t.Fatalf("Return copied %q", h.sys.copied)
	}
	h.key(0, ui.KeyDown)
	if _, f, _ := h.a.focusedField(); f.label != "password" {
		t.Fatalf("field %q", f.label)
	}
	h.key(0, ui.KeyRight) // reveals it
	h.has("gh-s3cret!")
	h.key(0, ui.KeyRight) // then shows it in Large Type
	if h.a.large == nil || h.a.large.value != "gh-s3cret!" {
		t.Fatalf("large %+v", h.a.large)
	}
	h.has("Press any key to close", "10")
	snapshot(t, h.tt, "large-type")
	h.key(0, ui.KeyDown) // any key puts it away, and does nothing else
	if h.a.large != nil {
		t.Fatal("Large Type stays")
	}
	if _, f, _ := h.a.focusedField(); f.label != "password" {
		t.Errorf("the key moved to %q", f.label)
	}
	h.key(0, ui.KeyDown)
	h.key(0, ui.KeyEnter)
	if code, _, _ := h.a.code(h.a.selected()); h.sys.last() != code {
		t.Errorf("copied %q, not the code", h.sys.last())
	}
	h.key(0, ui.KeyLeft)
	if h.a.mode != modeResults {
		t.Fatalf("mode %v", h.a.mode)
	}
	// Typing goes back to the search.
	h.key(0, ui.KeyRight)
	h.tt.Type("h")
	h.settle()
	if h.a.mode != modeResults || h.a.query != "gith" {
		t.Errorf("mode %v, query %q", h.a.mode, h.a.query)
	}
	// Right moves the caret while it's before the end.
	h.key(0, ui.KeyLeft)
	h.key(0, ui.KeyRight)
	if h.a.mode != modeResults {
		t.Errorf("Right before the end went to the fields")
	}
}

func TestEscapeForgetsWhatShowed(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("git")
	h.settle()
	h.a.reveal[fieldKey(h.a.selected().ID, 1)] = true
	h.key(0, ui.KeyEscape) // clears the search
	if h.a.query != "" || h.sys.hidden != 0 {
		t.Fatalf("query %q, hidden %d", h.a.query, h.sys.hidden)
	}
	h.key(0, ui.KeyEscape) // closes
	if h.sys.hidden != 1 || len(h.a.reveal) != 0 || !h.a.unlocked() {
		t.Fatalf("hidden %d, reveal %v, unlocked %v", h.sys.hidden, h.a.reveal, h.a.unlocked())
	}
}

func TestVaultSelector(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.key(ui.Cmd, ui.KeyK)
	if h.a.mode != modeVaults {
		t.Fatalf("mode %v", h.a.mode)
	}
	h.has("All Vaults", "My Vault", "Acme Inc.", "⌘1", "⌘2", "⌘3")
	snapshot(t, h.tt, "vaults")
	h.key(0, ui.KeyDown) // My Vault
	h.key(0, ui.KeyDown) // Acme Inc.
	h.key(0, ui.KeyRight)
	h.has("Engineering")
	h.key(0, ui.KeyRight) // into it
	if h.a.vaultCursor != "collection:"+h.a.vault.Collections[0].ID {
		t.Fatalf("cursor %q", h.a.vaultCursor)
	}
	h.key(0, ui.KeyEnter)
	if h.a.mode != modeResults || h.a.vaultName() != "Engineering" {
		t.Fatalf("mode %v, vault %q", h.a.mode, h.a.vaultName())
	}
	for _, r := range h.a.rows {
		if r.item != nil && r.item.Name != "AWS Console" && r.item.Name != "Deploy key" {
			t.Errorf("%s isn't in Engineering", r.item.Name)
		}
	}
	// Left goes back to the vault, then away.
	h.key(ui.Cmd, ui.KeyK)
	h.key(0, ui.KeyLeft)
	if h.a.vaultCursor != h.f.OrgID {
		t.Fatalf("cursor %q", h.a.vaultCursor)
	}
	h.key(0, ui.KeyLeft) // collapses
	h.key(0, ui.KeyLeft) // goes back
	if h.a.mode != modeResults {
		t.Fatalf("mode %v", h.a.mode)
	}
	// Command and a digit choose at once.
	h.key(ui.Cmd, ui.Key2)
	if h.a.cfg.Vault != "personal" {
		t.Fatalf("⌘2: %q", h.a.cfg.Vault)
	}
	h.key(ui.Cmd, ui.Key1)
	if h.a.cfg.Vault != "" {
		t.Fatalf("⌘1: %q", h.a.cfg.Vault)
	}
	// Folders and collections follow the vaults.
	tree := h.a.vaultTree()
	nums := map[string]int{}
	for _, v := range tree {
		nums[v.name] = v.num
	}
	if nums["All Vaults"] != 1 || nums["My Vault"] != 2 || nums["Acme Inc."] != 3 || nums["Personal"] == 0 {
		t.Errorf("numbers %v", nums)
	}
}

func TestLeavingTheFieldsConcealsPasswords(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("git")
	h.settle()
	reveal := func() {
		t.Helper()
		h.key(0, ui.KeyRight)
		h.key(0, ui.KeyDown)
		h.key(0, ui.KeyRight)
		if !h.a.reveal[fieldKey(h.a.selected().ID, 1)] {
			t.Fatal("not revealed")
		}
		h.has("gh-s3cret!")
	}
	for _, leave := range []struct {
		how string
		do  func()
	}{
		{"Left", func() { h.key(0, ui.KeyLeft) }},
		{"typing", func() { h.tt.Type("h"); h.settle() }},
		{"Cmd-F", func() { h.key(ui.Cmd, ui.KeyF) }},
		{"the vault selector", func() { h.key(ui.Cmd, ui.KeyK); h.key(0, ui.KeyEscape) }},
	} {
		h.a.query, h.a.caretEnd = "git", true
		h.a.queryChanged()
		h.settle()
		reveal()
		leave.do()
		if h.a.mode == modeDetails || len(h.a.reveal) != 0 || h.tt.HasText("gh-s3cret!") {
			t.Errorf("after %s: mode %v, reveal %v", leave.how, h.a.mode, h.a.reveal)
		}
	}
}

func TestRevealOnlyTheFieldChosen(t *testing.T) {
	f := bwtest.Sample()
	f.Add(bw.Item{Type: bw.TypeLogin, Name: "Bank", Login: &bw.Login{Username: "ada", Password: "b4nk-pass"},
		Fields: []bw.Field{{Type: bw.FieldHidden, Name: "PIN", Value: "8642"}}}, false)
	h := newHarness(t, f, true)
	h.unlock()
	h.tt.Type("bank")
	h.settle()
	h.key(0, ui.KeyRight) // username
	h.key(0, ui.KeyDown)  // password
	h.key(0, ui.KeyRight) // reveals it alone
	h.has("b4nk-pass")
	if h.tt.HasText("8642") {
		t.Fatal("the PIN showed with the password")
	}
	h.key(0, ui.KeyDown)  // PIN
	h.key(0, ui.KeyRight) // reveals it too
	h.has("b4nk-pass", "8642")
	snapshot(t, h.tt, "reveal-one")
	// The eye of a field reveals that field alone.
	h.key(0, ui.KeyLeft)
	if err := h.tt.Click("Reveal (hold ⌥)"); err != nil {
		t.Fatal(err)
	}
	h.settle()
	if !h.tt.HasText("b4nk-pass") || h.tt.HasText("8642") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}
