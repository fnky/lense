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
	choiceWait, typeAhead = 5*time.Millisecond, 0
	countries := map[string]string{"US": "United States", "DE": "Germany", "GB": "United Kingdom"}
	countryCode = func(c string) string {
		if len(c) <= 2 {
			return strings.ToUpper(c)
		}
		for code, name := range countries {
			if strings.EqualFold(name, c) {
				return code
			}
		}
		return ""
	}
	countryName = func(code string) string { return countries[code] }
	os.Exit(m.Run())
}

// page is an app showing a form, as accessibility would show it: its
// elements in the page's order, which is that of Tab, fields and others,
// as links.
type page struct {
	app macos.App
	// url is the address of the page; "" for one the browser doesn't tell.
	url string
	// away is whether another app is in front; awayOnTab brings one to
	// the front as Tab is pressed.
	away, awayOnTab bool
	elements        []*element
	focus           *element
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
	// noSet is an app that takes no value set through accessibility.
	noSet bool
	// building is how many looks at the page's fields leave out its last,
	// as a browser that tells of them as it builds what it tells.
	building int
	// done is what the fill did, in turn: "set user=ada", "type ada",
	// "tab", "open …".
	done []string
	// onPut runs as a field takes a value, set or typed, before it holds it.
	onPut func(p *page, e *element)
}

// element is an element of the page.
type element struct {
	p     *page
	name  string
	field bool
	// typ is its type, as an input's: text, password, email….
	typ                           string
	id, label, placeholder, class string
	form                          uint64
	formHint                      string
	value                         string
	hidden, disabled, readonly    bool
	offPage, frame, gone          bool
	// drop is how many characters of what it takes it drops, the first
	// time; silent is a password field that tells nothing of what it holds.
	drop   int
	silent bool
	// options are a menu's, as a page's select, which typing chooses of.
	options []string
}

func newPage(browser bool, els ...*element) *page {
	p := &page{app: macos.App{PID: 7, Name: "Notes", BundleID: "com.apple.Notes"}, url: loginPage}
	if browser {
		p.app = macos.App{PID: 7, Name: "Safari", BundleID: "com.apple.Safari"}
	}
	for _, e := range els {
		e.p = p
		p.elements = append(p.elements, e)
	}
	return p
}

// fieldOf is a field of the type, named by its identifier.
func fieldOf(typ, id string) *element { return &element{name: id, id: id, field: true, typ: typ} }

func text(id string) *element     { return fieldOf("text", id) }
func password(id string) *element { return fieldOf("password", id) }
func link(name string) *element   { return &element{name: name} }

// in puts elements in a form.
func in(form uint64, els ...*element) []*element {
	for _, e := range els {
		e.form = form
	}
	return els
}

func (p *page) find(name string) *element {
	for _, e := range p.elements {
		if e.name == name && !e.gone {
			return e
		}
	}
	return nil
}

func (p *page) did(what string) bool { return slices.Contains(p.done, what) }

func (p *page) Frontmost() macos.App {
	if p.away {
		return macos.App{PID: 99, Name: "Mail", BundleID: "com.apple.mail"}
	}
	return p.app
}
func (p *page) Activate(int) bool { return true }
func (p *page) OpenURL(u string)  { p.done = append(p.done, "open "+u) }
func (p *page) BrowserURL(context.Context, string) (string, error) {
	return p.url, nil
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
	p.done = append(p.done, "type "+s)
	if p.held > 0 {
		p.done = append(p.done, "while held")
	}
	e := p.focus
	if e == nil || !e.field || e.gone {
		return
	}
	if e.typ == "select" {
		// A menu chooses the first option that starts so.
		for _, o := range e.options {
			if strings.HasPrefix(strings.ToLower(o), strings.ToLower(s)) {
				e.value = o
				break
			}
		}
		return
	}
	if p.onPut != nil {
		p.onPut(p, e)
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
		s, e.drop = s[:len(s)-min(e.drop, len(s))], 0
	}
	e.value += s
}

func (p *page) PressKey(_ int, code uint16) {
	p.done = append(p.done, "tab")
	p.away = p.away || p.awayOnTab
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

func (p *page) Fields(int, Field) []Field {
	var out []Field
	for _, e := range p.elements {
		if e.field && !e.gone && !e.offPage {
			out = append(out, e)
		}
	}
	if p.building > 0 {
		p.building--
		out = out[:len(out)-1]
	}
	return out
}

func (e *element) Info() macos.FieldInfo {
	return macos.FieldInfo{
		Type: e.typ, ID: e.id, Label: e.label, Placeholder: e.placeholder, Classes: e.class,
		Value: e.Value(), Enabled: !e.disabled, Settable: !e.disabled && !e.readonly, Viewable: !e.hidden,
		Form: e.form, FormHint: e.formHint, OnPage: !e.offPage, InFrame: e.frame,
	}
}

func (e *element) Value() string {
	if e.typ == "password" {
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

func (e *element) PageURL() string {
	if e.offPage {
		return ""
	}
	return e.p.url
}

func (e *element) SetValue(v string) bool {
	p := e.p
	if e.gone || p.noSet || e.typ == "select" {
		return false
	}
	p.done = append(p.done, "set "+e.name+"="+v)
	if p.onPut != nil {
		p.onPut(p, e)
		if e.gone {
			return true // the page took it, and dropped it
		}
	}
	if e.drop > 0 {
		v, e.drop = v[:len(v)-min(e.drop, len(v))], 0
	}
	e.value = v
	return true
}

func (e *element) Same(o Field) bool { return e == o }

func (e *element) Focus() bool {
	if e.gone {
		return false
	}
	e.p.focus = e
	return true
}

const loginPage = "https://example.com/login"

var login = Request{Username: "ada", Password: "s3cret!", Matches: func(u string) bool { return u == loginPage }}

// withTOTP is the login with its one-time password.
var withTOTP = func() Request { r := login; r.TOTP = "123456"; return r }()

func fill(t *testing.T, p *page, req Request) error {
	t.Helper()
	return Fill(context.Background(), p, p.app, req)
}

func (p *page) holds(t *testing.T, want ...string) {
	t.Helper()
	for i := 0; i < len(want); i += 2 {
		e := p.find(want[i])
		if e == nil || e.value != want[i+1] {
			t.Errorf("%s holds %+v, want %q; did %q", want[i], e, want[i+1], p.done)
		}
	}
}

func (p *page) nothingFilled(t *testing.T) {
	t.Helper()
	for _, e := range p.elements {
		if e.value != "" {
			t.Errorf("%s holds %q; did %q", e.name, e.value, p.done)
		}
	}
}

// --- A browser's page, as Bitwarden fills it ---

func TestFillsAtOnce(t *testing.T) {
	p := newPage(true, in(1, text("login_field"), password("password"))...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "login_field", "ada", "password", "s3cret!")
	// Each set at once, nothing typed, no Tab; the focus left on the
	// password field.
	if !slices.Equal(p.done, []string{"set login_field=ada", "set password=s3cret!"}) || p.focus != p.find("password") {
		t.Errorf("did %q, focus on %v", p.done, p.focus)
	}
}

func TestWaitsForThePageToTellAll(t *testing.T) {
	// As the browser builds what it tells of the page, it leaves out its
	// confirmation field at first.
	p := newPage(true, in(1, fieldOf("email", "email"), password("password"), password("confirm"))...)
	p.building = 1
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "email", "ada", "password", "s3cret!", "confirm", "s3cret!")
}

func TestTypesWhereSettingDoesNot(t *testing.T) {
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.noSet = true
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada", "password", "s3cret!")
	if !p.did("type ada") || !p.did("type s3cret!") {
		t.Errorf("did %q", p.done)
	}
}

func TestUsernameNamedSoBeforeTheNearest(t *testing.T) {
	// The first of the form named a username's goes before the nearest.
	p := newPage(true, in(1, fieldOf("email", "email"), text("nickname"), password("password"))...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "email", "ada", "nickname", "")
	// Without a name, the nearest of the form.
	p = newPage(true, in(1, text("a"), text("b"), password("password"))...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "a", "", "b", "ada")
}

func TestUsernameOfAnotherForm(t *testing.T) {
	// A field of another form fills only named a username's.
	p := newPage(true, append(in(1, text("username")), in(2, password("password"))...)...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada", "password", "s3cret!")
	p = newPage(true, append(in(1, text("q2")), in(2, password("password"))...)...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "q2", "", "password", "s3cret!")
	// A page without forms is as one form.
	p = newPage(true, text("q2"), password("password"))
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "q2", "ada", "password", "s3cret!")
}

func TestFieldsNoLoginFills(t *testing.T) {
	hidden, disabled, readonly := fieldOf("email", "hidden-email"), text("disabled-user"), text("readonly-user")
	hidden.hidden, disabled.disabled, readonly.readonly = true, true, true
	search, captcha := text("searchQuery"), text("captcha_answer")
	p := newPage(true, text("username"), hidden, disabled, readonly, search, captcha, password("password"))
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada", "hidden-email", "", "disabled-user", "", "readonly-user", "", "searchQuery", "", "captcha_answer", "", "password", "s3cret!")
	// A search field, though the nearest before the password field.
	p = newPage(true, in(1, text("siteSearch"), password("password"))...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "siteSearch", "", "password", "s3cret!")
	// An email field of the form fills, though behind its label.
	hidden = fieldOf("email", "hidden-email")
	hidden.hidden = true
	p = newPage(true, in(1, hidden, password("password"))...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "hidden-email", "ada")
}

func TestNotANewslettersField(t *testing.T) {
	news := fieldOf("email", "email")
	news.formHint = "newsletter-signup"
	p := newPage(true, append(in(1, news), in(2, password("password"))...)...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "email", "", "password", "s3cret!")
}

func TestEveryPasswordField(t *testing.T) {
	// A form that signs up, with its confirmation.
	confirm := password("confirm")
	confirm.placeholder = "Confirm password"
	p := newPage(true, in(1, fieldOf("email", "email"), password("password"), confirm)...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "email", "ada", "password", "s3cret!", "confirm", "s3cret!")
	if p.focus != confirm {
		t.Errorf("focus on %v", p.focus)
	}
}

func TestSigningInBeforeSigningUp(t *testing.T) {
	up := in(1, fieldOf("email", "new-email"), password("new-password"))
	for _, e := range up {
		e.formHint = "signup-form"
	}
	p := newPage(true, append(up, in(2, text("username"), password("password"))...)...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "new-email", "", "new-password", "", "username", "ada", "password", "s3cret!")
}

func TestPasswordShownAsText(t *testing.T) {
	shown, hint := text("password"), text("password-hint")
	p := newPage(true, in(1, text("username"), shown, hint)...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada", "password", "s3cret!", "password-hint", "")
}

func TestOneTimePassword(t *testing.T) {
	otp := text("otp")
	otp.placeholder = "Authentication code"
	p := newPage(true, in(1, text("username"), password("password"), otp)...)
	if err := fill(t, p, withTOTP); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada", "password", "s3cret!", "otp", "123456")
	// Without a code, the field stays as it is.
	p = newPage(true, in(1, text("username"), password("password"), text("otp"))...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "otp", "")
	// One that asks for a recovery code isn't one for it.
	p = newPage(true, in(1, text("username"), password("password"), text("recovery-code"))...)
	if err := fill(t, p, withTOTP); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "recovery-code", "")
}

func TestPageForTheCodeAlone(t *testing.T) {
	p := newPage(true, in(1, text("app_totp"))...)
	if err := fill(t, p, withTOTP); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "app_totp", "123456")
	// A box for each digit.
	var boxes []*element
	for _, d := range "123456" {
		boxes = append(boxes, text("otp-"+string(d)))
	}
	p = newPage(true, in(1, boxes...)...)
	if err := fill(t, p, withTOTP); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "otp-1", "1", "otp-2", "2", "otp-6", "6")
	// A login without a code fills none.
	p = newPage(true, in(1, text("app_totp"))...)
	if err := fill(t, p, login); !errors.Is(err, ErrNoField) {
		t.Errorf("err %v", err)
	}
}

func TestUsernameAlone(t *testing.T) {
	p := newPage(true, in(1, text("username"), link("Next"))...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada")
	// An email field, by its type alone.
	email := fieldOf("email", "")
	email.name = "email"
	p = newPage(true, in(1, email, link("Next"))...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "email", "ada")
	// One that subscribes to news isn't one to sign in with.
	p = newPage(true, in(1, fieldOf("email", "email"), text("subscribe"))...)
	if err := fill(t, p, login); !errors.Is(err, ErrNoField) {
		t.Errorf("a newsletter's: %v", err)
	}
	p.nothingFilled(t)
}

func TestNoLoginForm(t *testing.T) {
	// A page of the website without its login form, as its home page,
	// with the focus on a field named nothing a login knows.
	p := newPage(true, text("q"), link("Sign in"))
	p.focus = p.find("q")
	if err := fill(t, p, login); !errors.Is(err, ErrNoField) {
		t.Errorf("err %v", err)
	}
	p.nothingFilled(t)
	p = newPage(true, link("Home"), text("newsletter"))
	if err := fill(t, p, login); !errors.Is(err, ErrNoField) {
		t.Errorf("a page's only text field: %v", err)
	}
	p.nothingFilled(t)
}

func TestAroundTheFocus(t *testing.T) {
	// Two login forms: the one with the focus fills.
	p := newPage(true, append(in(1, text("username"), password("password")), in(2, text("user2"), password("pass2"))...)...)
	p.focus = p.find("user2")
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "", "password", "", "user2", "ada", "pass2", "s3cret!")
	// The focus on a username's field, which then fills.
	p = newPage(true, in(1, fieldOf("email", "email"), text("member"), password("password"))...)
	p.focus = p.find("member")
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "email", "", "member", "ada")
}

func TestLeavesWhatFieldsHold(t *testing.T) {
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.find("username").value = "ada"
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	if p.did("set username=ada") {
		t.Errorf("set the username again: %q", p.done)
	}
}

func TestDroppedCharactersAreTypedAgain(t *testing.T) {
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.find("username").drop = 2
	p.find("password").drop = 3
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada", "password", "s3cret!")
}

func TestFormDrawnAnew(t *testing.T) {
	// As a page's script takes over its form, the first value goes to the
	// form it replaces.
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.onPut = func(p *page, e *element) {
		p.onPut = nil
		for _, old := range p.elements {
			old.gone = true
		}
		for _, n := range in(1, text("username"), password("password")) {
			n.p = p
			p.elements = append(p.elements, n)
		}
		p.focus = nil
	}
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada", "password", "s3cret!")
}

func TestNotTaken(t *testing.T) {
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.onPut = func(p *page, e *element) { e.drop = len("ada") } // drops all of it, set or typed
	if err := fill(t, p, login); !errors.Is(err, ErrNotTaken) {
		t.Errorf("err %v, did %q", err, p.done)
	}
}

func TestOpensThePage(t *testing.T) {
	p := newPage(true, in(1, text("username"), password("password"))...)
	req := login
	req.Page = loginPage
	if err := fill(t, p, req); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.Join(p.done, ","), "open https://example.com/login,") {
		t.Errorf("did %q", p.done)
	}
	p.holds(t, "username", "ada", "password", "s3cret!")
}

// --- Only the login's websites, in front, in no frame ---

func TestOnlyTheLoginsWebsites(t *testing.T) {
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.url = "https://example.net/login"
	var pe *PageError
	if err := fill(t, p, login); !errors.As(err, &pe) || pe.URL != "https://example.net/login" {
		t.Errorf("another site: %v", err)
	}
	p.nothingFilled(t)
	// A request that matches no page fills none.
	p = newPage(true, in(1, text("username"), password("password"))...)
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); !errors.As(err, &pe) {
		t.Errorf("no websites: %v", err)
	}
	p.nothingFilled(t)
}

func TestPageThatDoesNotTell(t *testing.T) {
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.url = ""
	var pe *PageError
	if err := fill(t, p, login); !errors.As(err, &pe) || pe.URL != "" {
		t.Errorf("err %v", err)
	}
	p.nothingFilled(t)
}

func TestPageGoesElsewhere(t *testing.T) {
	// After the username, the page goes to another site: no password there.
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.onPut = func(p *page, e *element) { p.url = "https://example.net/" }
	var pe *PageError
	if err := fill(t, p, login); !errors.As(err, &pe) {
		t.Errorf("err %v", err)
	}
	p.holds(t, "username", "ada", "password", "")
}

// anyOfExample matches every page of example.com's hosts.
var anyOfExample = Request{Username: "ada", Password: "s3cret!", Matches: func(u string) bool {
	return strings.HasPrefix(u, "https://example.com/") || strings.Contains(u, ".example.com/")
}}

func TestStaysOnItsOrigin(t *testing.T) {
	// After the username, the page goes to another host of its domain,
	// which the login's websites match too.
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.url = "https://login.example.com/"
	p.onPut = func(p *page, e *element) { p.url = "https://evil.example.com/" }
	var pe *PageError
	if err := fill(t, p, anyOfExample); !errors.As(err, &pe) || !pe.Changed {
		t.Errorf("another origin: %v", err)
	}
	p.holds(t, "username", "ada", "password", "")
}

func TestFillsThePageTheUserWasOn(t *testing.T) {
	req := anyOfExample
	req.Origin = "https://example.com"
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.url = "https://login.example.com/"
	var pe *PageError
	if err := fill(t, p, req); !errors.As(err, &pe) || !pe.Changed {
		t.Errorf("another page than the user's: %v", err)
	}
	p.nothingFilled(t)
	p = newPage(true, in(1, text("username"), password("password"))...)
	if err := fill(t, p, req); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "username", "ada", "password", "s3cret!")
}

func TestOnlyInFront(t *testing.T) {
	// The app the user was in doesn't come back to the front.
	p := newPage(true, in(1, text("username"), password("password"))...)
	p.away = true
	if err := fill(t, p, login); !errors.Is(err, ErrNotInFront) {
		t.Errorf("an app behind another: %v", err)
	}
	p.nothingFilled(t)
	// Another comes to the front after the username: no password.
	p = newPage(true, in(1, text("username"), password("password"))...)
	p.onPut = func(p *page, e *element) { p.away = true }
	if err := fill(t, p, login); !errors.Is(err, ErrNotInFront) {
		t.Errorf("an app gone behind another: %v", err)
	}
	p.holds(t, "username", "ada", "password", "")
}

func framed(e *element) *element { e.frame = true; return e }

func TestNotInFrames(t *testing.T) {
	// A login form in a frame, as another site's on the page.
	p := newPage(true, in(1, framed(text("username")), framed(password("password")))...)
	if err := fill(t, p, login); !errors.Is(err, ErrInFrame) {
		t.Errorf("a form in a frame: %v", err)
	}
	p.nothingFilled(t)
	// The page's own form, beside one in a frame, is the one filled.
	p = newPage(true, append(in(1, framed(text("other")), framed(password("other-pass"))), in(2, text("username"), password("password"))...)...)
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "other", "", "other-pass", "", "username", "ada", "password", "s3cret!")
}

func TestNotIntoTheAddressField(t *testing.T) {
	address := text("address")
	address.offPage = true
	p := newPage(true, address, text("username"), password("password"))
	p.focus = address
	if err := fill(t, p, login); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "address", "", "username", "ada", "password", "s3cret!")
}

// --- An app's own fields, typed into ---

func TestAppTypes(t *testing.T) {
	p := newPage(false, text("user"), password("pass"))
	p.focus = p.find("user")
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
	if !slices.Equal(p.done, []string{"type ada", "tab", "type s3cret!"}) {
		t.Errorf("did %q", p.done)
	}
}

func TestAppWaitsForTheFocusToMove(t *testing.T) {
	p := newPage(false, text("user"), password("pass"))
	p.focus, p.lag = p.find("user"), 10
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestAppPasswordFocused(t *testing.T) {
	p := newPage(false, text("user"), password("pass"))
	p.focus = p.find("pass")
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "", "pass", "s3cret!")
}

func TestAppTypesOnceTheSelectionTakes(t *testing.T) {
	// A busy app selects what a field holds only a while after Command-A.
	p := newPage(false, text("user"), password("pass"))
	p.find("user").value, p.find("pass").value = "someone", "old"
	p.focus, p.selectLag = p.find("user"), 5
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
	// An empty field needs no selection, which would come too late.
	p = newPage(false, text("user"), password("pass"))
	p.focus, p.selectLag = p.find("user"), 1000
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestAppWaitsForTheKeysHeld(t *testing.T) {
	p := newPage(false, text("user"), password("pass"))
	p.focus, p.held = p.find("user"), 5
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); err != nil {
		t.Fatal(err)
	}
	if p.did("while held") {
		t.Errorf("typed while keys were held: %q", p.done)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestAppSilentPasswordField(t *testing.T) {
	p := newPage(false, text("user"), password("pass"))
	p.find("pass").silent = true
	p.focus = p.find("user")
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "user", "ada", "pass", "s3cret!")
}

func TestAppNeedsTheFocus(t *testing.T) {
	p := newPage(false, text("user"), password("pass"))
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); !errors.Is(err, ErrNoField) {
		t.Errorf("an app without the focus on a field: %v", err)
	}
}

func TestAppOnlyInFront(t *testing.T) {
	// Another comes to the front as Tab goes to the password field.
	p := newPage(false, text("user"), password("pass"))
	p.focus, p.awayOnTab = p.find("user"), true
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); !errors.Is(err, ErrNotInFront) {
		t.Errorf("an app gone behind another after Tab: %v", err)
	}
	p.holds(t, "user", "ada", "pass", "")
	// Another comes to the front after the username: no Tab.
	p = newPage(false, text("user"), password("pass"))
	p.focus = p.find("user")
	p.onPut = func(p *page, e *element) { p.away = true }
	if err := fill(t, p, Request{Username: "ada", Password: "s3cret!"}); !errors.Is(err, ErrNotInFront) {
		t.Errorf("an app gone behind another: %v", err)
	}
	if p.did("tab") {
		t.Errorf("pressed Tab in an app behind another: %q", p.done)
	}
}
