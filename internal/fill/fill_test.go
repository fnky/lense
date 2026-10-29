package fill

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"lense/internal/macos"
)

func TestMain(m *testing.M) {
	poll, settle, appWait, pageWait, releaseWait = time.Millisecond, 30*time.Millisecond, 30*time.Millisecond, 100*time.Millisecond, 30*time.Millisecond
	keyPause, slowPause = 0, 0
	os.Exit(m.Run())
}

// page is an app showing a form, as accessibility would show it: its
// elements in the order of Tab, fields and others, as links.
type page struct {
	app      macos.App
	elements []*element
	focus    *element
	// lag is how many looks the focus takes to move after Tab.
	lag     int
	pending *element
	// held is how many looks the user keeps keys held for.
	held int
	// selected is whether Command-A selected what the focused field holds;
	// selecting is how many looks it takes to after selectLag, keys typed
	// meanwhile going before what it selects, as on a busy page.
	selected             bool
	selecting, selectLag int
	// typed is what the keys did, in turn.
	typed []string
	// onType runs as a field takes keys, before it holds them.
	onType func(p *page, e *element)
}

// element is an element of the page.
type element struct {
	p     *page
	name  string
	kind  macos.FieldKind
	field bool // a text or password field, not a link or button
	value string
	hint  string
	// offPage is a browser's own field, as its address field.
	offPage bool
	gone    bool
	// drop is how many of the keys typed the field drops, the first time.
	drop int
	// silent is a password field that tells nothing of what it holds.
	silent bool
}

func newPage(browser bool, els ...*element) *page {
	p := &page{app: macos.App{PID: 7, Name: "Notes", BundleID: "com.apple.Notes"}}
	if browser {
		p.app = macos.App{PID: 7, Name: "Safari", BundleID: "com.apple.Safari"}
	}
	for _, e := range els {
		e.p = p
		p.elements = append(p.elements, e)
	}
	return p
}

func text(name string) *element { return &element{name: name, kind: macos.FieldText, field: true} }
func password(name string) *element {
	return &element{name: name, kind: macos.FieldPassword, field: true}
}
func link(name string) *element { return &element{name: name} }

func (p *page) find(name string) *element {
	for _, e := range p.elements {
		if e.name == name && !e.gone {
			return e
		}
	}
	return nil
}

func (p *page) Frontmost() macos.App { return p.app }
func (p *page) Activate(int) bool    { return true }
func (p *page) OpenURL(u string)     { p.typed = append(p.typed, "open "+u) }
func (p *page) BrowserURL(context.Context, string) (string, error) {
	return "https://example.com/login", nil
}
func (p *page) Prepare(int)                 {}
func (p *page) PageLoaded(int) (bool, bool) { return true, true }
func (p *page) ModifiersHeld() bool         { p.held--; return p.held >= 0 }
func (p *page) SelectAll(int) {
	if p.selectLag > 0 {
		p.selecting = p.selectLag
		return
	}
	p.selected = true
}
func (p *page) TypeText(_ int, s string, _ time.Duration) {
	p.typed = append(p.typed, "type "+s)
	if p.held > 0 {
		p.typed = append(p.typed, "while held")
	}
	e := p.focus
	if e == nil || !e.field || e.gone {
		return
	}
	if p.onType != nil {
		p.onType(p, e)
		if e.gone {
			return
		}
	}
	if p.selecting > 0 {
		// The selection takes after the first key: it selects that one too.
		e.value += s[:1]
		s, p.selecting, p.selected = s[1:], 0, true
	}
	if p.selected {
		e.value = ""
	}
	p.selected = false
	if e.drop > 0 {
		s, e.drop = s[:len(s)-e.drop], 0
	}
	e.value += s
}

func (p *page) PressKey(_ int, code uint16) {
	p.typed = append(p.typed, "tab")
	if code != macos.KeyTab {
		return
	}
	i := slices.Index(p.elements, p.focus)
	for _, e := range p.elements[i+1:] {
		if !e.gone && !e.offPage {
			p.pending = e
			return
		}
	}
}

func (p *page) Focused(int) Field {
	if p.pending != nil {
		if p.lag > 0 {
			p.lag--
		} else {
			p.focus, p.pending = p.pending, nil
		}
	}
	if p.focus == nil || p.focus.gone {
		return nil
	}
	return p.focus
}

func (p *page) TextFields(int, Field) []Field {
	var out []Field
	for _, e := range p.elements {
		if e.field && !e.gone && !e.offPage {
			out = append(out, e)
		}
	}
	return out
}

func (e *element) Kind() macos.FieldKind {
	if !e.field {
		return macos.FieldNone
	}
	return e.kind
}

func (e *element) Value() string {
	if e.kind == macos.FieldPassword {
		return ""
	}
	return e.value
}

func (e *element) Length() (int, bool) {
	if e.silent {
		return 0, false
	}
	return len(e.value), true
}

func (e *element) Selected() (int, bool) {
	p := e.p
	if p.focus != e {
		return 0, true
	}
	if p.selecting > 0 {
		if p.selecting--; p.selecting == 0 {
			p.selected = true
		}
	}
	if p.selected {
		return len(e.value), true
	}
	return 0, true
}

func (e *element) Hint() string      { return e.hint }
func (e *element) OnPage() bool      { return !e.offPage }
func (e *element) Same(o Field) bool { return e == o }

func (e *element) Focus() bool {
	if e.gone {
		return false
	}
	e.p.focus = e
	return true
}

var login = Request{Username: "ada", Password: "s3cret!"}

func fill(t *testing.T, p *page, req Request) error {
	t.Helper()
	return Fill(context.Background(), p, p.app, req)
}

func (p *page) holds(t *testing.T, want ...string) {
	t.Helper()
	for i := 0; i < len(want); i += 2 {
		e := p.find(want[i])
		if e == nil || e.value != want[i+1] {
			t.Errorf("%s holds %+v, want %q; typed %q", want[i], e, want[i+1], p.typed)
		}
	}
}

func TestFillsTheUsernameThenThePassword(t *testing.T) {
	p := newPage(true, text("user"), password("pass"))
	p.focus = p.find("user")
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestWaitsForTheFocusToMove(t *testing.T) {
	// A native app's form, which Lense knows only through the focus.
	p := newPage(false, text("user"), password("pass"))
	p.focus, p.lag = p.find("user"), 10
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestTabGoesToALink(t *testing.T) {
	p := newPage(true, text("user"), link("Forgot your username?"), password("pass"))
	p.focus = p.find("user")
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestPasswordFieldShowsLate(t *testing.T) {
	pass := password("pass")
	pass.gone = true
	p := newPage(true, text("user"), link("Next"), pass)
	p.focus = p.find("user")
	p.onType = func(p *page, e *element) {
		if e.name == "user" {
			pass.gone = false
		}
	}
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestUsernameAlone(t *testing.T) {
	p := newPage(true, text("user"), link("Next"))
	p.focus = p.find("user")
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada")
}

func TestNothingFocused(t *testing.T) {
	search := text("search")
	p := newPage(true, search, link("Home"), text("user"), password("pass"))
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "search", "", "user", "ada", "pass", "s3cret!")
}

func TestUsernameAloneByItsName(t *testing.T) {
	user := text("email")
	user.hint = "login_field"
	p := newPage(true, text("search"), user, link("Next"))
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "search", "", "email", "ada")
}

func TestNotIntoTheAddressField(t *testing.T) {
	address := text("address")
	address.offPage = true
	p := newPage(true, address, text("user"), password("pass"))
	p.focus = address
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "address", "", "user", "ada", "pass", "s3cret!")
}

func TestPasswordFocused(t *testing.T) {
	p := newPage(true, text("user"), password("pass"))
	p.focus = p.find("pass")
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "", "pass", "s3cret!")
}

func TestDroppedKeysAreTypedAgain(t *testing.T) {
	p := newPage(true, text("user"), password("pass"))
	p.find("user").drop = 2
	p.find("pass").drop = 3
	p.focus = p.find("user")
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestTypesOnceTheSelectionTakes(t *testing.T) {
	// A busy page selects what a field holds only a while after Command-A.
	p := newPage(true, text("user"), password("pass"))
	p.find("user").value, p.find("pass").value = "someone", "old"
	p.focus, p.selectLag = p.find("user"), 5
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
	// An empty field needs no selection, which would come too late.
	p = newPage(true, text("user"), password("pass"))
	p.focus, p.selectLag = p.find("user"), 1000
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestFormDrawnAnew(t *testing.T) {
	// As a page's script takes over its form, the first keys go to the
	// form it replaces.
	p := newPage(true, text("user"), password("pass"))
	p.focus = p.find("user")
	p.onType = func(p *page, e *element) {
		p.onType = nil
		for _, old := range p.elements {
			old.gone = true
		}
		for _, n := range []*element{text("user"), password("pass")} {
			n.p = p
			p.elements = append(p.elements, n)
		}
		p.focus = nil
	}
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestWaitsForTheKeysHeld(t *testing.T) {
	p := newPage(true, text("user"), password("pass"))
	p.focus, p.held = p.find("user"), 5
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(p.typed, "while held") {
		t.Errorf("typed while keys were held: %q", p.typed)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestASilentPasswordField(t *testing.T) {
	p := newPage(true, text("user"), password("pass"))
	p.find("pass").silent = true
	p.focus = p.find("user")
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestNoField(t *testing.T) {
	if err := fill(t, newPage(true, link("Home")), login); !errors.Is(err, ErrNoField) {
		t.Errorf("a page without fields: %v", err)
	}
	// In a native app, only the field with the focus.
	p := newPage(false, text("user"), password("pass"))
	if err := fill(t, p, login); !errors.Is(err, ErrNoField) {
		t.Errorf("an app without the focus on a field: %v", err)
	}
}

func TestNotTaken(t *testing.T) {
	p := newPage(true, text("user"), password("pass"))
	p.focus = p.find("user")
	p.onType = func(p *page, e *element) { e.drop = len("ada") } // drops every key
	if err := fill(t, p, login); !errors.Is(err, ErrNotTaken) {
		t.Errorf("err %v, typed %q", err, p.typed)
	}
}

func TestOpensThePage(t *testing.T) {
	p := newPage(true, text("user"), password("pass"))
	p.focus = p.find("user")
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!", Page: "https://example.com/login"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.Join(p.typed, ","), "open https://example.com/login,") {
		t.Errorf("typed %q", p.typed)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}
