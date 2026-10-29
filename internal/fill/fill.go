// Package fill fills a login into another app. A browser's page it fills
// as Bitwarden's browser extension does: the login form's username and
// password fields, and the field that asks for the one-time password,
// each set at once. An app's own fields, which tell little, it types
// into as the user would. It sees the app through accessibility, and
// waits for each step to take before the next.
package fill

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"lense/internal/macos"
	"lense/internal/search"
)

// Request is a login to fill into another app.
type Request struct {
	Username, Password string
	// TOTP is the login's one-time password as the fill begins, or "".
	TOTP string
	Page string // a page to open first, and fill once it shows
	// Matches reports whether a page is one of the login's websites, as
	// a browser's must be to be filled; nil matches none.
	Matches func(url string) bool
	// Origin is the origin a browser's page must show all through the
	// fill, as "https://github.com" of the page the user was on; without
	// it, the page's as the fill first types.
	Origin string
	// Card and Identity fill a page's fields of a card, or an identity,
	// in place of a login's; Matches the pages they may fill.
	Card     *Card
	Identity *Identity
}

var (
	// ErrNoField means no login form showed to fill: a username field,
	// a password field, or one for the one-time password.
	ErrNoField = errors.New("no field to fill")
	// ErrNotTaken means a field didn't take what was put into it.
	ErrNotTaken = errors.New("a field didn't take what was typed")
	// ErrInFrame means the login form is in a frame of the page, as
	// another site's: a fill types into no frame.
	ErrInFrame = errors.New("the login form is in a frame")
	// ErrNotInFront means the app to fill wasn't in front as a key was to
	// be typed: a fill types only where the user is looking.
	ErrNotInFront = errors.New("the app to fill isn't in front")
)

// PageError means the login form is on a page that isn't one of the
// login's websites, at URL, or on one the browser doesn't tell, "", or
// that the page went elsewhere, Changed, from the origin the fill was
// on: nothing is typed there.
type PageError struct {
	URL     string
	Changed bool
}

func (e *PageError) Error() string {
	switch {
	case e.URL == "":
		return "the browser doesn't tell which page it shows"
	case e.Changed:
		return "the page went elsewhere as it was filled: " + e.URL
	}
	return "the page isn't one of the login's websites: " + e.URL
}

// Field is a field of another app.
type Field interface {
	// Info is what the field told of itself as it was found.
	Info() macos.FieldInfo
	// Value is what the field holds now; a password field keeps its own.
	Value() string
	// Length is how many characters the field holds, where the app tells.
	Length() (int, bool)
	// Selected is how many of them are selected, where the app tells.
	Selected() (int, bool)
	// PageURL is the address of the page the field is on, or "".
	PageURL() string
	Focus() bool
	// SetValue puts text into the field at once, as accessibility sets a
	// value: a page gets its input and change events, as from Bitwarden.
	SetValue(text string) bool
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
	// Fields are the fields of the page near is on, or of the page the
	// app shows, in the page's order, those of its frames too.
	Fields(pid int, near Field) []Field
	TypeText(pid int, text string, pause time.Duration)
	PressKey(pid int, code uint16)
	SelectAll(pid int)
}

// How long a fill waits, which tests shorten.
var (
	poll = 50 * time.Millisecond
	// settle is how long a field has to take what was put into it, or
	// the focus to move.
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

// Fill fills the login into the app target, which comes back to the
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
			if !waitFor(ctx, appWait, front) {
				return ErrNotInFront
			}
		}
	}
	if target.PID == 0 {
		return ErrNoField
	}
	f := &filler{ctx: ctx, kb: kb, req: req, pid: target.PID, bundle: target.BundleID, browser: macos.IsBrowser(target.BundleID), origin: req.Origin}
	fill := f.fillApp
	switch {
	case req.Card != nil || req.Identity != nil:
		if !f.browser {
			return ErrNoField // an app's fields tell too little to know a card's
		}
		kb.Prepare(f.pid)
		fill = f.fillForm
	case f.browser:
		kb.Prepare(f.pid)
		fill = f.fillPage
	}
	// Keys still held, as the Shift of Shift-Return, would change those
	// typed: Shift-Tab goes back.
	waitFor(ctx, releaseWait, func() bool { return !kb.ModifiersHeld() })
	err := fill(req, wait)
	if errors.Is(err, ErrNotTaken) {
		// A page that drew its form anew lost what was put into the old
		// one: once more, into the form it shows now.
		err = fill(req, settle)
	}
	return err
}

// filler fills the app of pid; origin is the origin of the page it
// fills, once it is known.
type filler struct {
	ctx     context.Context
	kb      Keyboard
	req     Request
	pid     int
	bundle  string
	browser bool
	origin  string
}

// onLogins checks that a field a browser shows is on a page of one of the
// login's websites, not in a frame of it, and returns the page's address.
func (f *filler) onLogins(field Field) (string, error) {
	if field.Info().InFrame {
		return "", ErrInFrame
	}
	u := field.PageURL()
	if u == "" {
		u, _ = f.kb.BrowserURL(f.ctx, f.bundle)
	}
	switch {
	case u == "":
		return "", &PageError{}
	case f.req.Matches == nil || !f.req.Matches(u):
		return u, &PageError{URL: u}
	}
	return u, nil
}

// commit checks, as keys are about to go to a field, what each key typed
// needs, at that moment: the app in front, where the user is looking;
// and in a browser, a page of the login's websites, not a frame of it, on
// the origin the fill began on. A page may have gone elsewhere since the
// fill began, as after the username, to another host of the domain.
func (f *filler) commit(field Field) error {
	if f.kb.Frontmost().PID != f.pid {
		return ErrNotInFront
	}
	if !f.browser {
		return nil
	}
	u, err := f.onLogins(field)
	if err != nil {
		return err
	}
	switch o := Origin(u); {
	case f.origin == "":
		f.origin = o
	case o != f.origin:
		return &PageError{URL: u, Changed: true}
	}
	return nil
}

// Origin returns the origin of a page's address: its scheme, host and
// port, as "https://github.com".
func Origin(page string) string {
	u, err := url.Parse(page)
	if err != nil {
		return page
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

// focus gives the field the keyboard focus, and waits for it to have it.
func (f *filler) focus(field Field) bool {
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

// kind returns the kind of a field, as the keys know it: one to type a
// password into, one to type text into, or none.
func kind(f Field) macos.FieldKind {
	if f == nil {
		return macos.FieldNone
	}
	switch f.Info().Type {
	case "password":
		return macos.FieldPassword
	case "text", "email", "tel":
		return macos.FieldText
	}
	return macos.FieldNone
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
