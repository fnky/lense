// Package fill types a login into another app, as the user would: its
// username into the login form's username field, its password into its
// password field. It sees the app through accessibility, and waits for
// each step to take before the next: the field to hold what was typed,
// the focus to move, the page to load.
package fill

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"lense/internal/macos"
	"lense/internal/search"
)

// Request is a login to type into another app.
type Request struct {
	Username, Password string
	Page               string // a page to open first, and fill once it shows
	// Matches reports whether a page is one of the login's websites, as
	// a browser's must be to be filled; nil matches none.
	Matches func(url string) bool
}

var (
	// ErrNoField means no login form showed to fill: a username field,
	// before a password field or named so, or a password field.
	ErrNoField = errors.New("no field to fill")
	// ErrNotTaken means a field didn't take what was typed into it.
	ErrNotTaken = errors.New("a field didn't take what was typed")
	// ErrInFrame means the login form is in a frame of the page, as
	// another site's: a fill types into no frame.
	ErrInFrame = errors.New("the login form is in a frame")
)

// PageError means the login form is on a page that isn't one of the
// login's websites, at URL, or on one the browser doesn't tell, "":
// nothing is typed there.
type PageError struct{ URL string }

func (e *PageError) Error() string {
	if e.URL == "" {
		return "the browser doesn't tell which page it shows"
	}
	return "the page isn't one of the login's websites: " + e.URL
}

// Field is a field of another app.
type Field interface {
	Kind() macos.FieldKind
	// Value is what the field holds; a password field keeps its own.
	Value() string
	// Length is how many characters the field holds, where the app tells.
	Length() (int, bool)
	// Selected is how many of them are selected, where the app tells.
	Selected() (int, bool)
	// Hint is what names the field, in lower case, as "login_field".
	Hint() string
	// OnPage reports whether the field is on a web page, rather than in
	// a browser's own controls, as its address field.
	OnPage() bool
	// InFrame reports whether the field is in a frame of the page.
	InFrame() bool
	// PageURL is the address of the page the field is on, or "": of the
	// frame, for one in a frame.
	PageURL() string
	Focus() bool
	Same(Field) bool
}

// Keyboard is what a fill asks of macOS: the apps, the fields of the one
// it fills, and the keys it types there. Mac is macOS's.
type Keyboard interface {
	Frontmost() macos.App
	Activate(pid int) bool
	OpenURL(url string)
	// BrowserURL is the address of the page a browser shows.
	BrowserURL(ctx context.Context, bundleID string) (string, error)
	// Prepare asks a browser to tell accessibility about its pages.
	Prepare(pid int)
	PageLoaded(pid int) (loaded, known bool)
	ModifiersHeld() bool
	// Focused is the field with the keyboard focus in the app, or nil.
	Focused(pid int) Field
	// TextFields are the text and password fields of the page near is
	// on, or of the page the app shows, in the page's order.
	TextFields(pid int, near Field) []Field
	TypeText(pid int, text string, pause time.Duration)
	PressKey(pid int, code uint16)
	SelectAll(pid int)
}

// How long a fill waits, which tests shorten.
var (
	poll = 50 * time.Millisecond
	// settle is how long a field has to take what was typed, or the
	// focus to move.
	settle = time.Second
	// appWait is how long the app the user was in has to come back to
	// the front, with its field.
	appWait = 1500 * time.Millisecond
	// pageWait is how long a page opened has to load, and show its form.
	pageWait = 10 * time.Second
	// releaseWait is how long the user has to let go of the keys held.
	releaseWait = time.Second
	// The pauses between the characters typed, and those typed again.
	keyPause, slowPause = 4 * time.Millisecond, 30 * time.Millisecond
)

// Fill types the login into the app target, which comes back to the
// front as the window that asked for it hides, or into the page the
// request opens first.
func Fill(ctx context.Context, kb Keyboard, target macos.App, req Request) error {
	wait := appWait
	if req.Page != "" {
		kb.OpenURL(req.Page)
		browser, err := waitForPage(ctx, kb, search.BaseDomain(search.Host(req.Page)))
		if err != nil {
			return err
		}
		target, wait = browser, pageWait
	} else if target.PID != 0 {
		front := func() bool { return kb.Frontmost().PID == target.PID }
		if !waitFor(ctx, appWait, front) {
			kb.Activate(target.PID)
			waitFor(ctx, appWait, front)
		}
	}
	if target.PID == 0 {
		return ErrNoField
	}
	f := filler{ctx: ctx, kb: kb, req: req, pid: target.PID, bundle: target.BundleID, browser: macos.IsBrowser(target.BundleID)}
	if f.browser {
		kb.Prepare(f.pid)
	}
	// Keys still held, as the Shift of Shift-Return, would change those
	// typed: Shift-Tab goes back.
	waitFor(ctx, releaseWait, func() bool { return !kb.ModifiersHeld() })
	err := f.fill(req, wait)
	if errors.Is(err, ErrNotTaken) {
		// A page that drew its form anew lost what was typed into the
		// old one: once more, into the form it shows now.
		err = f.fill(req, settle)
	}
	return err
}

// filler fills the app of pid.
type filler struct {
	ctx     context.Context
	kb      Keyboard
	req     Request
	pid     int
	bundle  string
	browser bool
}

func (f filler) fill(req Request, wait time.Duration) error {
	user, pass, err := f.find(wait)
	if err != nil {
		return err
	}
	if req.Username == "" {
		user = nil
	}
	if user == nil && pass == nil {
		return ErrNoField
	}
	if user != nil {
		if err := f.typeInto(user, req.Username, false); err != nil {
			return err
		}
		pass = f.next(user, pass)
	}
	if pass == nil || req.Password == "" {
		// A page that asks for the username alone, as the first of two.
		return nil
	}
	return f.typeInto(pass, req.Password, true)
}

// find finds the fields to fill, waiting for a page to load and show
// them on one of the login's websites.
func (f filler) find(wait time.Duration) (user, pass Field, err error) {
	deadline := time.Now().Add(wait)
	err = ErrNoField
	for {
		if loaded, known := f.kb.PageLoaded(f.pid); !f.browser || !known || loaded {
			if user, pass = f.fields(); user != nil || pass != nil {
				field := user
				if field == nil {
					field = pass
				}
				// A page on its way to the login's, as through a redirect,
				// may get there yet.
				if err = f.onLoginPage(field); err == nil {
					return user, pass, nil
				}
			}
		}
		if !time.Now().Before(deadline) || f.ctx.Err() != nil {
			return nil, nil, err
		}
		sleep(f.ctx, 3*poll)
	}
}

// fields finds the fields to fill: in an app, the field with the focus;
// on a page, the login form's, around the field with the focus.
func (f filler) fields() (user, pass Field) {
	at := f.kb.Focused(f.pid)
	if !f.browser {
		switch kind(at) {
		case macos.FieldPassword:
			return nil, at
		case macos.FieldText:
			return at, nil
		}
		return nil, nil
	}
	if at != nil && (!at.OnPage() || at.InFrame()) {
		at = nil // the address field, say: the page's form is the one to fill
	}
	switch kind(at) {
	case macos.FieldPassword:
		return nil, at
	case macos.FieldText:
	default:
		at = nil
	}
	// The fields of frames are no login's to fill, but tell why a page
	// with its form in one is left as it is.
	var page, framed []Field
	for _, fl := range f.kb.TextFields(f.pid, at) {
		if fl.InFrame() {
			framed = append(framed, fl)
		} else {
			page = append(page, fl)
		}
	}
	if user, pass = pick(page, at); user == nil && pass == nil {
		return pick(framed, nil)
	}
	return user, pass
}

// onLoginPage checks that a field a browser shows is on one of the
// login's websites: nowhere else does a fill type.
func (f filler) onLoginPage(field Field) error {
	if !f.browser {
		return nil
	}
	if field.InFrame() {
		return ErrInFrame
	}
	u := field.PageURL()
	if u == "" {
		u, _ = f.kb.BrowserURL(f.ctx, f.bundle)
	}
	switch {
	case u == "":
		return &PageError{}
	case f.req.Matches == nil || !f.req.Matches(u):
		return &PageError{URL: u}
	}
	return nil
}

// next goes from the username's field to the password's, with Tab, as
// the user would; where Tab goes elsewhere, as to a link, to the
// password field of the form, which may show only once the username is
// typed. It returns nil for a form that asks for the username alone.
func (f filler) next(user, pass Field) Field {
	f.kb.PressKey(f.pid, macos.KeyTab)
	waitFor(f.ctx, settle, func() bool {
		at := f.kb.Focused(f.pid)
		return at != nil && !at.Same(user)
	})
	if at := f.kb.Focused(f.pid); kind(at) == macos.FieldPassword && (!f.browser || at.OnPage() && !at.InFrame()) {
		return at
	}
	if pass != nil || !f.browser {
		return pass
	}
	waitFor(f.ctx, settle/2, func() bool {
		_, pass = pick(f.kb.TextFields(f.pid, user), user)
		return pass != nil
	})
	return pass
}

// typeInto types text over what the field holds, and waits for it to
// hold it: a field that takes less, as one that drops keys, gets it once
// more, typed slower.
func (f filler) typeInto(field Field, text string, secret bool) error {
	// The page may have gone elsewhere since: to another site, say, after
	// the username.
	if err := f.onLoginPage(field); err != nil {
		return err
	}
	for _, pause := range []time.Duration{keyPause, slowPause} {
		if !f.focus(field) {
			return ErrNotTaken
		}
		f.selectAll(field, secret)
		f.kb.TypeText(f.pid, text, pause)
		if waitFor(f.ctx, settle, func() bool { return holds(field, text, secret) }) {
			return nil
		}
	}
	if v := strings.TrimSpace(field.Value()); !secret && utf8.RuneCountInString(v) >= utf8.RuneCountInString(text) {
		return nil // a page that formats what it takes, as a phone number
	}
	return ErrNotTaken
}

// selectAll selects what the field holds, to type over it, and waits for
// the selection to take: Command-A goes its own way to a page, as an
// edit of the browser's, and a page busy with the keys before it would
// take keys typed after it before it, and lose them to the selection.
// A field that holds nothing needs none.
func (f filler) selectAll(field Field, secret bool) {
	n, known := field.Length()
	if !known {
		v := field.Value()
		n, known = utf8.RuneCountInString(v), v != "" || !secret
	}
	if known && n == 0 {
		return
	}
	f.kb.SelectAll(f.pid)
	if known {
		waitFor(f.ctx, settle, func() bool {
			s, ok := field.Selected()
			return !ok || s == n
		})
	}
}

// focus gives the field the keyboard focus, and waits for it to have it.
func (f filler) focus(field Field) bool {
	has := func() bool {
		at := f.kb.Focused(f.pid)
		return at != nil && at.Same(field)
	}
	if has() {
		return true
	}
	// One the page took away, as it drew its form anew, takes no focus.
	return field.Focus() && waitFor(f.ctx, settle, has)
}

// holds reports whether the field holds text. A password field keeps
// what it holds from accessibility, but may tell how much.
func holds(field Field, text string, secret bool) bool {
	if !secret {
		return strings.EqualFold(strings.TrimSpace(field.Value()), strings.TrimSpace(text))
	}
	n, ok := field.Length()
	if !ok {
		v := field.Value()
		if v == "" {
			return true // it tells nothing
		}
		n = utf8.RuneCountInString(v)
	}
	return n == utf8.RuneCountInString(text) || n == len(utf16.Encode([]rune(text)))
}

// pick picks the fields of a login form among the text fields of a page,
// in the page's order: the first password field, and the text field
// nearest before it for the username; of a page that asks for the
// username alone, the text field that names itself so. Any other text
// field is no username's, as a search field: a page without a login
// form gets nothing. The field with the focus, at, is the username's
// when it is the one before a password field, or named so.
func pick(fields []Field, at Field) (user, pass Field) {
	kinds := make([]macos.FieldKind, len(fields))
	for i, fl := range fields {
		kinds[i] = fl.Kind()
	}
	// before is the text field nearest before field i, or -1.
	before := func(i int) int {
		for j := i - 1; j >= 0; j-- {
			if kinds[j] == macos.FieldText {
				return j
			}
		}
		return -1
	}
	if at != nil {
		for i, fl := range fields {
			if !fl.Same(at) {
				continue
			}
			for j := i + 1; j < len(fields); j++ {
				if kinds[j] == macos.FieldPassword {
					if before(j) == i {
						return at, fields[j]
					}
					break
				}
			}
			if loginHint.MatchString(at.Hint()) {
				return at, nil
			}
			break
		}
	}
	for i, k := range kinds {
		if k != macos.FieldPassword {
			continue
		}
		if j := before(i); j >= 0 {
			return fields[j], fields[i]
		}
		return nil, fields[i]
	}
	for i, fl := range fields {
		if kinds[i] == macos.FieldText && loginHint.MatchString(fl.Hint()) {
			return fl, nil
		}
	}
	return nil, nil
}

// loginHint matches the names of username fields: their identifiers,
// placeholders and labels.
var loginHint = regexp.MustCompile(`user|e-?mail|login|account|identifier|phone`)

func kind(f Field) macos.FieldKind {
	if f == nil {
		return macos.FieldNone
	}
	return f.Kind()
}

// waitForPage waits for a browser to come to the front showing a page of
// domain. A browser that does not tell its page gets a moment to load it.
func waitForPage(ctx context.Context, kb Keyboard, domain string) (macos.App, error) {
	for {
		if err := ctx.Err(); err != nil {
			return macos.App{}, ErrNoField
		}
		fa := kb.Frontmost()
		if macos.IsBrowser(fa.BundleID) {
			u, err := kb.BrowserURL(ctx, fa.BundleID)
			if err != nil || u == "" {
				sleep(ctx, 2*time.Second)
				return fa, nil
			}
			if search.BaseDomain(search.Host(u)) == domain {
				return fa, nil
			}
		}
		sleep(ctx, 5*poll)
	}
}

// waitFor polls cond until it holds, for up to d, and reports whether it
// did.
func waitFor(ctx context.Context, d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) || ctx.Err() != nil {
			return false
		}
		sleep(ctx, poll)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
