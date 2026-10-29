package main

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"lense/internal/bw"
	"lense/internal/bw/bwtest"
	"lense/internal/ipc"
	"lense/internal/macos"

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
	// launched counts the launches, which start the app unless it
	// staysClosed; starting is how many times it doesn't answer yet then,
	// with startErr (ipc.ErrNoSocket by default), and probes how many
	// times it was asked whether it listens.
	launched    int
	staysClosed bool
	starting    int
	startErr    error
	probes      int
}

// answer is what the app answers as it is asked anything.
func (d *fakeDesktop) answer() error {
	switch {
	case d.notRun:
		return ipc.ErrNoSocket
	case d.starting > 0:
		d.starting--
		if d.startErr != nil {
			return d.startErr
		}
		return ipc.ErrNoSocket
	}
	return nil
}

func (d *fakeDesktop) listens(ctx context.Context, userID string) error {
	d.probes++
	return d.answer()
}

func (d *fakeDesktop) load() (*bw.Store, error) {
	if d.path == "" {
		return nil, bw.ErrNoData
	}
	return bw.Open(d.path)
}

func (d *fakeDesktop) unlock(ctx context.Context, userID string) (bw.SymmetricKey, error) {
	d.unlocks++
	if err := d.answer(); err != nil {
		return bw.SymmetricKey{}, err
	}
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
func (d *fakeDesktop) launch() error {
	d.launched++
	if !d.staysClosed {
		d.notRun = false
	}
	return nil
}

// fakeSystem is macOS: it records what was copied, filled and opened.
type fakeSystem struct {
	mu      sync.Mutex
	copied  []string
	secret  []bool
	filled  []fillRequest
	fillErr error
	opened  []string
	details []string
	hidden  int
	shown   int
	bio     bool
	authErr error // what Touch ID answers as a sealed key opens
	// seals counts the secrets sealed, which sealedKeys are, unless
	// sealErr.
	seals      int
	sealErr    error
	sealedKeys []*fakeSealed
	auths      int
	trust      bool
	field      bool   // a text field has the focus in the app the user was in
	browser    string // the browser that websites open in
	drags      []dragPhase
	pageURLs   map[string]string
	// phase and momentum are those of the scrolling, none by default.
	phase, momentum macos.Phase
	// closed is told the window closed, as hide closes it.
	closed func()
	// secure is how Secure Event Input was turned, in turn.
	secure []bool
}

func (s *fakeSystem) copy(text string, secret bool) {
	s.copied = append(s.copied, text)
	s.secret = append(s.secret, secret)
}
func (s *fakeSystem) openURL(u string)  { s.opened = append(s.opened, u) }
func (s *fakeSystem) biometrics() bool  { return s.bio }
func (s *fakeSystem) trusted(bool) bool { return s.trust }
func (s *fakeSystem) show()             { s.shown++ }
func (s *fakeSystem) hide() {
	s.hidden++
	if s.closed != nil {
		s.closed() // as the shell tells the app the window closed
	}
}
func (s *fakeSystem) openDetail(id string) { s.details = append(s.details, id) }
func (s *fakeSystem) seal(secret []byte) (sealedSecret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seals++
	if s.sealErr != nil {
		return nil, s.sealErr
	}
	f := &fakeSealed{sys: s, secret: slices.Clone(secret)}
	s.sealedKeys = append(s.sealedKeys, f)
	return f, nil
}

// fakeSealed is a secret sealed by the Secure Enclave, which opens it as
// Touch ID does with authErr.
type fakeSealed struct {
	sys      *fakeSystem
	secret   []byte
	released bool
}

func (f *fakeSealed) open(string) ([]byte, error) {
	f.sys.mu.Lock()
	defer f.sys.mu.Unlock()
	f.sys.auths++
	switch {
	case f.released:
		return nil, macos.ErrSealedGone
	case f.sys.authErr != nil:
		return nil, f.sys.authErr
	}
	return slices.Clone(f.secret), nil
}

func (f *fakeSealed) release() { clear(f.secret); f.released = true }
func (s *fakeSystem) fill(_ context.Context, _ macos.App, req fillRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filled = append(s.filled, req)
	return s.fillErr
}
func (s *fakeSystem) pageURL(_ context.Context, b macos.App) string { return s.pageURLs[b.BundleID] }
func (s *fakeSystem) fieldFocused(macos.App) bool                   { return s.field }
func (s *fakeSystem) defaultBrowser() string                        { return s.browser }
func (s *fakeSystem) secureInput(on bool)                           { s.secure = append(s.secure, on) }
func (s *fakeSystem) drag(p dragPhase)                              { s.drags = append(s.drags, p) }
func (s *fakeSystem) scrollPhase() (macos.Phase, macos.Phase)       { return s.phase, s.momentum }

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
	upd  *fakeUpdater
	r    *runner
	tt   *ui.Tester
	f    *bwtest.Fixture
	// relaunched counts the restarts the app asked for.
	relaunched int
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
	h.sys.closed = h.a.hidden
	h.a.version = "test"
	h.a.now = func() time.Time { return time.Date(2026, 10, 19, 15, 20, 0, 0, time.UTC) }
	h.a.sleep = func(time.Duration) {}
	h.a.spawn, h.a.post = h.r.spawn, h.r.post
	h.upd = &fakeUpdater{}
	h.a.upd, h.a.updEnabled = h.upd, true
	h.a.relaunch = func() { h.relaunched++ }
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

// rest rests the pointer on the element showing or labeled s, and returns
// the texts that show then, its tooltip's, and where it is.
func (h *harness) rest(s string) ([]string, ui.Rect) {
	h.t.Helper()
	r, ok := h.tt.Find(s)
	if !ok {
		h.t.Fatalf("no %q", s)
	}
	h.tt.Move(10, 470)
	h.tt.Advance(time.Second)
	before := h.tt.Texts()
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	h.tt.Advance(time.Second)
	// The texts that show once more than before: labels are texts too.
	seen := map[string]int{}
	for _, s := range before {
		seen[s]++
	}
	var tips []string
	for _, s := range h.tt.Texts() {
		if seen[s]--; seen[s] < 0 {
			tips = append(tips, s)
		}
	}
	return tips, r
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

// snapshot writes the frame to $LENSE_SNAPSHOTS, to look at.
func snapshot(t *testing.T, tt *ui.Tester, name string) {
	dir := os.Getenv("LENSE_SNAPSHOTS")
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
	h.has("Lense is locked", "ada@example.com · bitwarden.com")
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
	h.has("Lense is locked", "Unlock with Touch ID")
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
	h.a.shown(true) // the window the user asked for shows, locked: it asks the desktop app
	h.settle()
	if h.desk.unlocks != 1 || !h.a.unlocked() {
		t.Fatalf("unlocks %d, problem %+v", h.desk.unlocks, h.a.lockProb)
	}

	// Locked by the app itself, the vault goes, its user key sealed by
	// the Secure Enclave, which Touch ID of this Mac opens: the vault
	// decrypts again, without the desktop app.
	h.a.query = "git"
	h.a.lock(false)
	h.settle()
	if h.a.vault != nil || h.a.index != nil || h.sys.seals != 1 || h.a.sealed == nil {
		t.Fatalf("vault %v, seals %d", h.a.vault != nil, h.sys.seals)
	}
	sealed := h.sys.sealedKeys[0]
	if !bytes.Equal(sealed.secret, h.f.UserKey.Bytes()) {
		t.Fatal("sealed something else than the user key")
	}
	h.click("Unlock with Touch ID")
	if h.sys.auths != 1 || h.desk.unlocks != 1 || !h.a.unlocked() {
		t.Fatalf("auths %d, unlocks %d, problem %+v", h.sys.auths, h.desk.unlocks, h.a.lockProb)
	}
	if h.a.query != "" {
		t.Error("the search survived the lock")
	}
	if h.a.sealed != nil || !sealed.released {
		t.Error("the sealed key outlived the unlock")
	}

	// Canceled, nothing happens; Touch ID changed, the key goes.
	h.a.lock(false)
	h.settle()
	h.sys.authErr = macos.ErrCanceled
	h.click("Unlock with Touch ID")
	if h.a.unlocked() || h.a.lockProb != nil || h.a.sealed == nil {
		t.Fatalf("canceled: unlocked %v, problem %+v", h.a.unlocked(), h.a.lockProb)
	}
	// Touch ID failing otherwise says why, and keeps the key to try again.
	h.sys.authErr = &macos.AuthError{Code: -1009, Reason: "Operation is not allowed."}
	h.click("Unlock with Touch ID")
	if h.a.unlocked() || h.a.sealed == nil {
		t.Fatalf("failed: unlocked %v, kept %v", h.a.unlocked(), h.a.sealed != nil)
	}
	h.has("Touch ID didn't unlock", "Touch ID: Operation is not allowed. (-1009). Try again, or enter your master password.")
	h.sys.authErr = macos.ErrSealedGone
	h.click("Unlock with Touch ID")
	if h.a.unlocked() || h.a.sealed != nil {
		t.Fatal("the key that no longer opens stayed")
	}
	h.has("Touch ID changed")
	h.sys.authErr = nil
	h.click("Unlock with Touch ID") // through the desktop app
	if h.desk.unlocks != 2 || !h.a.unlocked() {
		t.Fatalf("unlocks %d", h.desk.unlocks)
	}

	// Locked for good, the key goes too: it asks the desktop app again.
	h.a.lock(false)
	h.a.lock(true)
	h.settle()
	if h.a.vault != nil || h.a.sealed != nil || !h.sys.sealedKeys[len(h.sys.sealedKeys)-1].released {
		t.Fatal("the vault, or its key, was kept")
	}
	h.click("Unlock with Touch ID")
	if h.desk.unlocks != 3 || !h.a.unlocked() {
		t.Fatalf("unlocks %d", h.desk.unlocks)
	}
}

func TestShowsWhileLoading(t *testing.T) {
	h := newHarness(t, nil, true)
	// The app opens: its window shows before it read the desktop app's
	// data, which the locked window waits for, its buttons off.
	h.a.store, h.a.loaded = nil, false
	h.a.shown(false)
	h.settle()
	h.has("Lense is locked", "Loading…", "Unlock with Touch ID")
	snapshot(t, h.tt, "locked-loading")
	_ = h.tt.Click("Unlock with Touch ID")
	h.a.password = "not it"
	h.a.unlockPassword()
	h.settle()
	if h.desk.unlocks != 0 || h.a.busy != "" || h.a.lockProb != nil {
		t.Fatalf("unlocks %d, busy %q, problem %+v while loading", h.desk.unlocks, h.a.busy, h.a.lockProb)
	}
	// Read, it doesn't ask for Touch ID: the user didn't ask for the
	// window.
	h.a.password = ""
	h.a.reload()
	h.settle()
	if h.tt.HasText("Loading…") || h.desk.unlocks != 0 || h.a.unlocked() {
		t.Fatalf("loading %v, unlocks %d", h.tt.HasText("Loading…"), h.desk.unlocks)
	}
	// Asked for, while it loads, the window asks once it read.
	h.a.hidden()
	h.a.store, h.a.loaded = nil, false
	h.a.shown(true)
	h.settle()
	if h.desk.unlocks != 0 {
		t.Fatal("asked for Touch ID while loading")
	}
	h.a.reload()
	h.settle()
	if h.desk.unlocks != 1 || !h.a.unlocked() {
		t.Fatalf("unlocks %d, problem %+v", h.desk.unlocks, h.a.lockProb)
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
		h.desk.unlockErr, h.desk.notRun, h.desk.staysClosed = tc.err, tc.notRun, tc.notRun
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

	// Return fills the page the user was on, of the login's website.
	h.a.front = front{app: macos.App{PID: 42, Name: "Safari", BundleID: "com.apple.Safari"}, url: "https://github.com/login"}
	h.a.noteSuggestions()
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 1 || h.sys.filled[0].username != "ada" || h.sys.filled[0].password != "gh-s3cret!" || h.sys.filled[0].page != "" {
		t.Fatalf("filled %+v", h.sys.filled)
	}
	// Shift-Return offers its websites to open, and fills the one chosen.
	h.key(ui.Shift, ui.KeyEnter)
	h.key(0, ui.KeyEnter)
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
	f.Add(bwtest.Item{Type: bw.TypeLogin, Name: "Bank", Reprompt: true, Login: &bwtest.Login{Username: "ada", Password: "b4nk"}}, false)
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
	// Return on a field closes the window once it copied, after the
	// master password: closed, the window forgets it was given.
	h.key(0, ui.KeyRight)
	h.key(0, ui.KeyDown)
	h.key(0, ui.KeyEnter)
	if len(h.sys.copied) != 3 || h.sys.hidden != 1 {
		t.Fatalf("copied %q, hidden %d", h.sys.copied, h.sys.hidden)
	}
	h.key(0, ui.KeyRight)
	h.key(0, ui.KeyDown)
	h.key(0, ui.KeyEnter)
	if h.a.confirm == nil || len(h.sys.copied) != 3 || h.sys.hidden != 1 {
		t.Fatalf("copied %q, hidden %d without the master password", h.sys.copied, h.sys.hidden)
	}
	h.a.confirmPw = f.Password
	h.a.confirmPassword()
	h.settle()
	if h.sys.last() != "b4nk" || len(h.sys.copied) != 4 || h.sys.hidden != 2 {
		t.Fatalf("copied %q, hidden %d", h.sys.copied, h.sys.hidden)
	}
}

func TestFillNeedsAccessibility(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.sys.trust, h.sys.field = false, true
	h.a.front = front{app: macos.App{PID: 42, Name: "Slack"}}
	h.tt.Type("git")
	h.settle()
	// Not where GitHub is for, Return offers its website, then Slack.
	h.key(0, ui.KeyEnter)
	h.key(0, ui.KeyDown)
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 0 {
		t.Fatal("filled without access")
	}
	h.has("Allow Lense to fill in other apps", "Open System Settings")
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
	h.key(0, ui.KeyDown)
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
	h.f.Add(bwtest.Item{Type: bw.TypeLogin, Name: "GitLab", Login: &bwtest.Login{Username: "ada"}}, false)
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

func TestAcknowledgements(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.SetSize(windowWidth, 3000) // all of the settings, unscrolled
	h.key(ui.Cmd, ui.KeyComma)
	h.click("Acknowledgements")
	if h.a.page != pageAcknowledgements {
		t.Fatalf("page %v", h.a.page)
	}
	h.has("Bitwarden", "Lense is not affiliated with or endorsed by Bitwarden, Inc.", "MyGo", "MIT License")
	h.tt.SetSize(windowWidth, windowHeight)
	snapshot(t, h.tt, "acknowledgements")
	h.tt.SetSize(windowWidth, 3000)
	h.click("Open bitwarden.com")
	h.click("Open github.com/egoist/mygo")
	if !slices.Equal(h.sys.opened, []string{"https://bitwarden.com", "https://github.com/egoist/mygo"}) {
		t.Fatalf("opened %q", h.sys.opened)
	}
	// Esc goes back to the settings, then out of them.
	h.key(0, ui.KeyEscape)
	if h.a.page != pageSettings {
		t.Fatalf("page %v", h.a.page)
	}
	h.click("Acknowledgements")
	h.click("Settings")
	if h.a.page != pageSettings {
		t.Fatalf("back: page %v", h.a.page)
	}
}

// The license the app carries is the one of the MyGo it is built with.
func TestMyGoLicenseIsCurrent(t *testing.T) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/egoist/mygo").Output()
	if err != nil {
		t.Skip("no go command: ", err)
	}
	b, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != mygoLicense {
		t.Fatal("MyGo's license changed: copy its LICENSE to licenses/mygo.txt")
	}
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
	h.click("Show Lense key")
	if h.a.recordID != "show" {
		t.Fatalf("recording %q", h.a.recordID)
	}
	h.key(ui.Cmd|ui.Alt, ui.KeyK)
	if h.a.cfg.Shortcut != "Option+Cmd+K" || h.a.recordID != "" {
		t.Fatalf("shortcut %q", h.a.cfg.Shortcut)
	}
	h.has("⌥⌘K")
	h.a.setShortcut = func(string) error { return errors.New("taken") }
	h.click("Show Lense key")
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
	// A login without a website to open.
	f := bwtest.Sample()
	f.Add(bwtest.Item{Type: bw.TypeLogin, Name: "Mainframe", Login: &bwtest.Login{Username: "ada", Password: "m41nfr4me"}}, false)
	h := newHarness(t, f, true)
	h.unlock()
	h.a.front = front{app: macos.App{PID: 42, Name: "Notes", BundleID: "com.apple.Notes"}}
	h.tt.Type("mainframe")
	h.settle()
	h.has("Copy password")
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 0 || h.sys.last() != "m41nfr4me" {
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

func TestOpenAndFillMenu(t *testing.T) {
	f := bwtest.Sample()
	f.Add(bwtest.Item{Type: bw.TypeLogin, Name: "Tracker", Login: &bwtest.Login{Username: "ada", Password: "tr4ck",
		URIs: []bw.URI{{URI: "https://tracker.example.com/login"}, {URI: "status.example.com"}}}}, false)
	h := newHarness(t, f, true)
	h.unlock()
	h.sys.browser, h.sys.trust = "Safari", true
	h.a.setFront(macos.App{PID: 42, Name: "Notes", BundleID: "com.apple.Notes"})
	h.tt.Type("tracker")
	h.settle()
	// The footer leaves out the arrows to the fields and back.
	if h.tt.HasText("Fields") {
		t.Error("the footer shows the key of the fields")
	}
	h.key(0, ui.KeyRight)
	if h.a.mode != modeDetails || h.tt.HasText("Back") {
		t.Errorf("mode %v, the footer shows the key back", h.a.mode)
	}
	h.key(0, ui.KeyLeft)

	// Away from its websites, Return offers them to open, and fill.
	h.has("Open and Fill in Safari")
	h.key(0, ui.KeyEnter)
	if h.a.mode != modeFillMenu || len(h.sys.filled) != 0 {
		t.Fatalf("mode %v, filled %+v", h.a.mode, h.sys.filled)
	}
	h.has("tracker.example.com/login", "status.example.com")
	snapshot(t, h.tt, "fill-menu")
	// Escape puts the menu away, and leaves the window.
	h.key(0, ui.KeyEscape)
	if h.a.mode != modeResults || h.sys.hidden != 0 || h.a.query != "tracker" {
		t.Fatalf("mode %v, hidden %d, query %q", h.a.mode, h.sys.hidden, h.a.query)
	}
	// The arrows choose a website, which Return opens and fills.
	h.key(0, ui.KeyEnter)
	h.key(0, ui.KeyDown)
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 1 || h.sys.filled[0].page != "https://status.example.com" {
		t.Fatalf("filled %+v", h.sys.filled)
	}
	// Typing puts it away, into the search.
	h.key(0, ui.KeyEnter)
	h.tt.Type("x")
	h.settle()
	if h.a.mode != modeResults || h.a.query != "trackerx" {
		t.Fatalf("mode %v, query %q", h.a.mode, h.a.query)
	}
	h.key(0, ui.KeyBackspace)

	// On one of its websites, Return fills the page.
	h.sys.pageURLs = map[string]string{"com.apple.Safari": "https://tracker.example.com/board"}
	h.a.setFront(macos.App{PID: 7, Name: "Safari", BundleID: "com.apple.Safari"})
	h.settle()
	h.has("Fill in Safari")
	h.key(0, ui.KeyEnter)
	if len(h.sys.filled) != 2 || h.sys.filled[1].page != "" {
		t.Fatalf("filled %+v", h.sys.filled)
	}
	// Held, Shift shows what Shift-Return does: the menu, there too.
	h.tt.HoldModifiers(ui.Shift)
	h.settle()
	h.has("⇧↩", "Open and Fill in Safari")
	h.tt.HoldModifiers(0)
	h.settle()
	h.has("Fill in Safari")
	h.key(ui.Shift, ui.KeyEnter)
	if cs := h.a.menuChoices(h.a.selected()); h.a.mode != modeFillMenu || len(cs) != 2 {
		t.Fatalf("mode %v, choices %+v", h.a.mode, cs)
	}
	h.click("tracker.example.com/login")
	if len(h.sys.filled) != 3 || h.sys.filled[2].page != "https://tracker.example.com/login" {
		t.Fatalf("filled %+v", h.sys.filled)
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
	// Return copies the field, and closes the window.
	h.key(0, ui.KeyEnter)
	if h.sys.last() != "ada" || h.sys.hidden != 1 || h.a.mode != modeResults {
		t.Fatalf("Return copied %q, hidden %d, mode %v", h.sys.copied, h.sys.hidden, h.a.mode)
	}
	h.key(0, ui.KeyRight) // shown again, back to the fields
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
	if code, _, _ := h.a.code(h.a.selected()); h.sys.last() != code || h.sys.hidden != 2 {
		t.Errorf("copied %q, not the code; hidden %d", h.sys.last(), h.sys.hidden)
	}
	// Clicked, a field copies, and the window stays.
	h.key(0, ui.KeyRight)
	h.click("username")
	if h.sys.last() != "ada" || h.sys.hidden != 2 || h.a.mode != modeDetails {
		t.Fatalf("clicked: copied %q, hidden %d, mode %v", h.sys.copied, h.sys.hidden, h.a.mode)
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

func TestClicksLeaveTheKeyboardToTheSearch(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("g")
	h.settle()
	h.click("GitHub") // a result
	h.tt.Type("i")
	h.settle()
	if h.a.query != "gi" {
		t.Fatalf("typed %q after a result was clicked", h.a.query)
	}
	h.key(0, ui.KeyRight)
	h.click("username") // a field, which it copies
	h.tt.Type("t")
	h.settle()
	if h.sys.last() != "ada" || h.a.query != "git" {
		t.Fatalf("copied %q, typed %q after a field was clicked", h.sys.copied, h.a.query)
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
	f.Add(bwtest.Item{Type: bw.TypeLogin, Name: "Bank", Login: &bwtest.Login{Username: "ada", Password: "b4nk-pass"},
		Fields: []bwtest.Field{{Type: bw.FieldHidden, Name: "PIN", Value: "8642"}}}, false)
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

func TestDraggingTheWindow(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	// The header's margin, where nothing else takes the pointer.
	h.tt.Press(6, 28)
	h.tt.Move(40, 60)
	h.tt.Move(80, 90)
	h.tt.Release(80, 90)
	h.settle()
	want := []dragPhase{dragStart, dragMove, dragMove, dragEnd}
	if !slices.Equal(h.sys.drags, want) {
		t.Fatalf("drags %v", h.sys.drags)
	}
	// The search takes its own presses, and moves nothing.
	h.sys.drags = nil
	h.tt.Press(200, 28)
	h.tt.Move(260, 60)
	h.tt.Release(260, 60)
	h.settle()
	if len(h.sys.drags) != 0 {
		t.Errorf("the search dragged: %v", h.sys.drags)
	}
	// So do the buttons of the header and the footer.
	h.click("Open in Bitwarden")
	if h.desk.opened != 1 {
		t.Errorf("Open in Bitwarden opened it %d times", h.desk.opened)
	}
	h.click("Vault")
	if h.a.mode != modeVaults {
		t.Errorf("the vault picker: mode %v", h.a.mode)
	}
	h.key(0, ui.KeyEscape)
	h.click("Settings (⌘,)")
	if h.a.page != pageSettings {
		t.Errorf("the settings button: page %v", h.a.page)
	}
	if len(h.sys.drags) != 0 {
		t.Errorf("a button dragged: %v", h.sys.drags)
	}
	// The settings' bar drags, but for its Done button.
	h.tt.Press(300, 20)
	h.tt.Move(320, 40)
	h.tt.Release(320, 40)
	h.settle()
	if len(h.sys.drags) != 3 {
		t.Errorf("the settings' bar: %v", h.sys.drags)
	}
	h.click("Done")
	if h.a.page != pageItems {
		t.Errorf("Done: page %v", h.a.page)
	}
}

func TestGuidesSnapshot(t *testing.T) {
	work := rect{0, 25, 1512, 920}
	x, y, g := snap(380, 25+(920-windowHeight)/5+6, windowWidth, windowHeight, work)
	if x != 376 || !g.snapY {
		t.Fatalf("snapped to %d,%d %+v", x, y, g)
	}
	// Over dark and light backgrounds alike, as the gray shows over both.
	dark, light := ui.RGB(40, 44, 52), ui.RGB(236, 238, 242)
	for _, tc := range []struct {
		name string
		ago  time.Duration
		bg   ui.Color
	}{{"guides-snapped", 0, dark}, {"guides-settled", time.Second, dark}, {"guides-settled-light", time.Second, light}} {
		g.carry(guides{}, time.Now().Add(-tc.ago))
		tt := ui.NewTester(func(c *ui.Context) {
			c.Root().Background(tc.bg)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				drawGuides(p, r, g, ui.Hex("#3D7FF0"))
				p.Fill(ui.Rect{X: float32(x - work.X), Y: float32(y - work.Y), W: windowWidth, H: windowHeight}, ui.RGBA(128, 128, 128, 0.25), radius)
			})
		}, work.W, work.H)
		snapshot(t, tt, tc.name)
	}
}

func TestWindowMaterial(t *testing.T) {
	h := newHarness(t, nil, true)
	if !h.a.cfg.translucent() || h.a.cfg.Material != "popover" {
		t.Fatalf("material by default %q", h.a.cfg.Material)
	}
	applied := 0
	h.a.applyMaterial = func() { applied++ }
	h.unlock()
	h.tt.SetSize(windowWidth, 2400)
	h.key(ui.Cmd, ui.KeyComma)
	h.has("Appearance", "Window material", "Popover")
	h.click("Popover")
	h.click("Solid")
	if h.a.cfg.Material != "solid" || h.a.cfg.translucent() || applied != 1 {
		t.Fatalf("material %q, applied %d", h.a.cfg.Material, applied)
	}
}

func TestRowsFadeUnderThePinnedHeader(t *testing.T) {
	f := bwtest.Sample()
	for i := range 20 {
		f.Add(bwtest.Item{Type: bw.TypeSecureNote, Name: "Note " + strconv.Itoa(i+10), Notes: "note"}, false)
	}
	h := newHarness(t, f, true)
	h.tt.SetVibrancy(true)
	h.unlock()
	// Scroll the list well into All Items, rows under its header.
	for range 6 {
		h.tt.Scroll(150, 300, 0, 60)
		h.settle()
	}
	h.tt.Frame()
	snapshot(t, h.tt, "pinned-header")
	label, ok := h.tt.Find("ALL ITEMS")
	if !ok {
		t.Fatal("no header")
	}
	// Right of the label, down to the header's bottom, the material shows
	// alone.
	clear := func() {
		t.Helper()
		img, s := h.tt.Image(), float32(2)
		for y := int(label.Y * s); y < int((label.Y+label.H+4)*s); y++ {
			for x := int((label.X + label.W + 8) * s); x < int((label.X+label.W+180)*s); x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0x800 {
					t.Fatalf("a row shows under the header at %d, %d: alpha %d", x, y, a>>8)
				}
			}
		}
	}
	clear()
	// Stretched past the end, the rows go up under the header, which
	// stays. The trackpad goes just to the end first.
	at := h.a.listBand.at
	h.tt.Scroll(150, 300, 0, at.MaxY-at.Y)
	h.settle()
	for range 4 {
		if !h.a.stretch(&h.a.listBand, pull(250, h.tt.Now())) {
			t.Fatal("the list scrolled past its end")
		}
		h.tt.Advance(8 * time.Millisecond)
	}
	h.tt.Frame()
	if h.a.listOff > -40 {
		t.Fatalf("stretched by %v", h.a.listOff)
	}
	snapshot(t, h.tt, "pinned-header-stretched")
	if r, _ := h.tt.Find("ALL ITEMS"); r != label {
		t.Fatalf("the header moved from %v to %v", label, r)
	}
	clear()
}

func TestFooterOverAppKitsMaterial(t *testing.T) {
	h := newHarness(t, nil, true)
	var shown []bool
	redraws := 0
	h.a.nativeBar = true
	h.a.showBar = func(show bool) { shown = append(shown, show) }
	h.a.redrawBar = func() { redraws++ }
	h.settle()
	if len(shown) != 0 {
		t.Fatalf("the footer shows over the lock: %v", shown)
	}
	h.unlock()
	if !slices.Equal(shown, []bool{true}) || redraws == 0 {
		t.Fatalf("unlocked: shown %v, drawn %d times", shown, redraws)
	}
	// The window draws no footer: the footer's own window does.
	if _, ok := h.tt.Find("Open in Bitwarden"); ok {
		t.Fatal("the window draws the footer under the material")
	}
	ft := ui.NewTester(h.a.footerView, windowWidth, footerHeight)
	if _, ok := ft.Find("Open in Bitwarden"); !ok {
		t.Fatal("the footer's window shows no footer")
	}
	if _, ok := ft.Find("Copy username"); !ok {
		t.Fatal("the footer's window shows no keys")
	}
	h.a.page = pageSettings
	h.settle()
	h.a.page = pageItems
	h.settle()
	if !slices.Equal(shown, []bool{true, false, true}) {
		t.Fatalf("over the settings and back: %v", shown)
	}
}

func TestTooltipsNameIconsInTheWindowsAppearance(t *testing.T) {
	h := newHarness(t, nil, true)
	h.tt.SetDark(true)
	h.unlock()
	// What says what it does itself has no tip.
	for _, s := range []string{"Open in Bitwarden", "All Vaults", "username"} {
		if tips, _ := h.rest(s); len(tips) > 0 {
			t.Errorf("%s shows a tip: %q", s, tips)
		}
	}
	// An icon has, in the window's appearance: dark in dark mode.
	tips, r := h.rest("Settings (⌘,)")
	if !slices.Equal(tips, []string{"Settings (⌘,)"}) {
		t.Fatalf("the settings' tip: %q", tips)
	}
	snapshot(t, h.tt, "tooltip-dark")
	below := h.tt.Image().At(int((r.X+r.W/2)*2), int((r.Y+r.H+9)*2))
	if c, _, _, _ := below.RGBA(); c>>8 > 80 {
		t.Fatalf("the tip is light in dark mode: %v", below)
	}
}

func TestWebsiteFieldOpensAndFills(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	// GitHub, chosen: its website's button opens the page and fills it.
	h.click("Open and Fill")
	if len(h.sys.filled) != 1 || h.sys.filled[0].page != "https://github.com/login" || h.sys.filled[0].username != "ada" {
		t.Fatalf("filled %+v", h.sys.filled)
	}
	if len(h.sys.opened) != 0 {
		t.Fatalf("opened %v, not filled", h.sys.opened)
	}
	// Every field's Copy is in the same place, before the field's button:
	// the username's, which has none, as the password's, before Reveal.
	cp, _ := h.tt.Find("Copy")
	reveal, _ := h.tt.Find("Reveal (hold ⌥)")
	if cp.Y >= reveal.Y || cp.X+38 != reveal.X {
		t.Fatalf("the username's copy at %v, the password's reveal at %v", cp, reveal)
	}
	h.tt.Move(cp.X-100, cp.Y+cp.H/2)
	h.tt.Advance(200 * time.Millisecond)
	snapshot(t, h.tt, "field-hover")
}

func TestCopyTakesTheSecondsOfACodesPlace(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	secs, ok := h.tt.Find("30s")
	if !ok {
		t.Fatalf("no countdown in %q", h.tt.Texts())
	}
	// Copy's place on every row, and the button's after it.
	cp, _ := h.tt.Find("Copy")
	// look reports whether something blue, and anything, shows on the
	// code's row from x0 to x1.
	look := func(x0, x1 float32) (blue, ink bool) {
		img := h.tt.Image()
		for y := int(secs.Y * 2); y < int((secs.Y+secs.H)*2); y++ {
			for x := int(x0 * 2); x < int(x1*2); x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				r, g, b = r>>8, g>>8, b>>8
				if b > r+60 && b > g+30 {
					blue = true
				}
				if r < 200 {
					ink = true
				}
			}
		}
		return blue, ink
	}
	ring := func() bool { blue, _ := look(cp.X+38, cp.X+66); return blue }
	_, seconds := look(cp.X+29, cp.X+37) // between the places, as far as the seconds go
	if !ring() || !seconds {
		t.Fatal("no countdown before the pointer comes")
	}
	if _, ink := look(cp.X+4, cp.X+12); ink {
		t.Fatal("copy shows before the pointer comes")
	}
	h.tt.Move(secs.X-200, secs.Y+secs.H/2)
	h.tt.Advance(200 * time.Millisecond)
	snapshot(t, h.tt, "code-hover")
	_, seconds = look(cp.X+29, cp.X+37)
	_, copied := look(cp.X+4, cp.X+24)
	if !ring() || seconds || !copied {
		t.Fatalf("hovered: ring %v, seconds %v, copy %v", ring(), seconds, copied)
	}
}

func TestTooltipsTurnOff(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	if !h.a.cfg.Tooltips {
		t.Fatal("tooltips off by default")
	}
	var saved []config
	h.a.saveCfg = func(c config) { saved = append(saved, c) }
	h.tt.SetSize(windowWidth, 2400) // all of the settings, unscrolled
	h.key(ui.Cmd, ui.KeyComma)
	// The switch at the end of the setting's row, beside its two lines.
	label, ok := h.tt.Find("Show tooltips")
	if !ok {
		t.Fatalf("no setting in %q", h.tt.Texts())
	}
	h.tt.ClickAt(windowWidth-28-18, label.Y+17)
	h.settle()
	if h.a.cfg.Tooltips || len(saved) != 1 || saved[0].Tooltips {
		t.Fatalf("tooltips %v, saved %+v", h.a.cfg.Tooltips, saved)
	}
	h.key(0, ui.KeyEscape)
	h.tt.SetSize(windowWidth, windowHeight)
	h.settle()
	if tips, _ := h.rest("Settings (⌘,)"); len(tips) > 0 {
		t.Fatalf("tooltips off, the settings' shows %q", tips)
	}
}

func TestHidingForgetsWhatShowed(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("git")
	h.settle()
	it := h.a.selected()
	h.a.reveal[fieldKey(it.ID, 1)] = true
	h.a.reprompted[it.ID] = true
	// Filling hides the window, as going elsewhere does: what it revealed
	// goes with it, what it showed stays for the next time.
	h.key(ui.Shift, ui.KeyEnter)
	h.key(0, ui.KeyEnter)
	if h.sys.hidden != 1 || len(h.a.reveal) != 0 || len(h.a.reprompted) != 0 {
		t.Fatalf("hidden %d, revealed %v, reprompted %v", h.sys.hidden, h.a.reveal, h.a.reprompted)
	}
	if h.a.query != "git" || h.a.selected() != it {
		t.Fatalf("query %q, chosen %v", h.a.query, h.a.selected())
	}
}

func TestPickersChoose(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.SetDark(true)
	h.tt.SetSize(windowWidth, 2400) // all of the settings, unscrolled
	h.key(ui.Cmd, ui.KeyComma)
	snapshot(t, h.tt, "settings-dark")
	h.click("Popover")
	h.has("Solid", "Menu", "HUD", "Sidebar", "Under Window")
	snapshot(t, h.tt, "picker-open")
	h.click("Solid")
	if h.a.cfg.Material != "solid" {
		t.Fatalf("material %q", h.a.cfg.Material)
	}
	if h.tt.HasText("Under Window") {
		t.Fatal("the picker stays open")
	}
}

func TestTouchIDOpensBitwarden(t *testing.T) {
	// As the window shows, Touch ID is tried, and Bitwarden isn't opened
	// for it: the window says it isn't open.
	h := newHarness(t, nil, true)
	h.desk.notRun, h.desk.starting = true, 3
	h.a.shown(true)
	h.settle()
	if h.desk.launched != 0 || h.a.unlocked() {
		t.Fatalf("launched %d, unlocked %v", h.desk.launched, h.a.unlocked())
	}
	h.has("Bitwarden isn't open")
	// Asked for, Touch ID opens Bitwarden, and waits for it to listen.
	h.click("Unlock with Touch ID")
	if h.desk.launched != 1 || !h.a.unlocked() || h.desk.opened != 0 {
		t.Fatalf("launched %d, opened %d, unlocked %v: %+v", h.desk.launched, h.desk.opened, h.a.unlocked(), h.a.lockProb)
	}
	// As the window showed, as the button asked, as Bitwarden answered.
	if h.desk.unlocks != 1+1+1 || h.desk.probes != 3+1 {
		t.Errorf("asked Bitwarden to unlock %d times, whether it listens %d", h.desk.unlocks, h.desk.probes)
	}
	if h.a.opening || h.a.busy != "" || h.a.holdOpen != 0 {
		t.Errorf("opening %v, busy %q, held open %d", h.a.opening, h.a.busy, h.a.holdOpen)
	}
}

func TestTouchIDWaitsForBitwardenToAnswer(t *testing.T) {
	// Launched, Bitwarden listens before it answers: the first asks get no
	// answer, and Touch ID waits for one rather than giving up.
	h := newHarness(t, nil, true)
	h.desk.notRun, h.desk.starting, h.desk.startErr = true, 4, ipc.ErrNoAnswer
	h.click("Unlock with Touch ID")
	if !h.a.unlocked() || h.desk.probes != 5 || h.desk.unlocks != 2 {
		t.Fatalf("unlocked %v, probed %d, asked %d: %+v", h.a.unlocked(), h.desk.probes, h.desk.unlocks, h.a.lockProb)
	}
}

func TestVaultChevronsExpand(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.key(ui.Cmd, ui.KeyK)
	// The chevron shows what the vault holds, rather than choosing it.
	h.click("Expand Acme Inc.")
	if h.a.mode != modeVaults || h.a.cfg.Vault != "" || !h.a.vaultOpen[h.f.OrgID] {
		t.Fatalf("mode %v, vault %q, open %v", h.a.mode, h.a.cfg.Vault, h.a.vaultOpen)
	}
	h.has("Engineering")
	snapshot(t, h.tt, "vaults-expanded")
	h.click("Collapse Acme Inc.")
	if h.a.vaultOpen[h.f.OrgID] || h.tt.HasText("Engineering") {
		t.Fatal("still open")
	}
	// The rest of the row chooses it.
	h.click("Acme Inc.")
	if h.a.cfg.Vault != h.f.OrgID || h.a.mode != modeResults {
		t.Fatalf("vault %q, mode %v", h.a.cfg.Vault, h.a.mode)
	}
}

func TestSecretsDecryptOnlyAsTheyShow(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	h.tt.Type("git")
	h.settle()
	it := h.a.selected()
	// Shown concealed, the password isn't decrypted: dots stand for it.
	h.has("••••••••••")
	if len(h.a.shownValues) != 0 {
		t.Fatalf("decrypted to show concealed: %v", h.a.shownValues)
	}
	// Revealed, it decrypts once, and goes as the keys leave the fields.
	h.key(0, ui.KeyRight)
	h.key(0, ui.KeyDown)
	h.key(0, ui.KeyRight)
	h.has("gh-s3cret!")
	if len(h.a.shownValues) != 1 {
		t.Fatalf("shown %v", h.a.shownValues)
	}
	h.key(0, ui.KeyLeft)
	h.key(0, ui.KeyLeft)
	if h.a.shownValues != nil || h.tt.HasText("gh-s3cret!") {
		t.Fatalf("still shown: %v", h.a.shownValues)
	}
	// Copied, it decrypts for the clipboard alone.
	h.key(ui.Cmd|ui.Shift, ui.KeyC)
	if h.sys.last() != "gh-s3cret!" || h.a.shownValues != nil {
		t.Fatalf("copied %q, shown %v", h.sys.last(), h.a.shownValues)
	}
	// Locked, what was decrypted goes, and the secrets of the items open
	// no more.
	h.a.lock(true)
	h.settle()
	if h.a.codes != nil || h.a.shownValues != nil {
		t.Fatal("decrypted values outlived the lock")
	}
	if _, err := it.Login.Password.Reveal(); !errors.Is(err, bw.ErrLocked) {
		t.Fatalf("revealed, locked: %v", err)
	}
}

func TestSecureInputWhileTheMasterPasswordIsTyped(t *testing.T) {
	h := newHarness(t, nil, true)
	h.settle()
	if len(h.sys.secure) != 0 {
		t.Fatalf("secure input %v before the field has the keyboard", h.sys.secure)
	}
	h.click("Master password")
	if !slices.Equal(h.sys.secure, []bool{true}) {
		t.Fatalf("secure input %v as the field has the keyboard", h.sys.secure)
	}
	h.settle()
	if len(h.sys.secure) != 1 {
		t.Fatalf("secure input turned %v times", h.sys.secure)
	}
	h.unlock()
	if !slices.Equal(h.sys.secure, []bool{true, false}) {
		t.Fatalf("secure input %v once unlocked", h.sys.secure)
	}
}

func TestScreenCaptureIsAllowedInTheSettings(t *testing.T) {
	h := newHarness(t, nil, true)
	h.unlock()
	if h.a.cfg.AllowScreenCapture {
		t.Fatal("screen capture allowed by default")
	}
	applied := 0
	h.a.applyCapture = func() { applied++ }
	h.tt.SetSize(windowWidth, 2400) // all of the settings, unscrolled
	h.key(ui.Cmd, ui.KeyComma)
	label, ok := h.tt.Find("Allow screen capture")
	if !ok {
		t.Fatalf("no setting in %q", h.tt.Texts())
	}
	h.tt.ClickAt(windowWidth-28-18, label.Y+17)
	h.settle()
	if !h.a.cfg.AllowScreenCapture || applied != 1 {
		t.Fatalf("allowed %v, applied %d times", h.a.cfg.AllowScreenCapture, applied)
	}
}
