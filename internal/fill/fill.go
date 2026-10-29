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
}

var (
	// ErrNoField means no username or password field showed to fill.
	ErrNoField = errors.New("no field to fill")
	// ErrNotTaken means a field didn't take what was typed into it.
	ErrNotTaken = errors.New("a field didn't take what was typed")
)

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
	f := filler{ctx: ctx, kb: kb, pid: target.PID, browser: macos.IsBrowser(target.BundleID)}
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
	pid     int
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
// them: the field with the focus, and on a page, the password field
// after it; on a page without one, its login form's fields.
func (f filler) find(wait time.Duration) (user, pass Field, err error) {
	deadline := time.Now().Add(wait)
	for {
		if loaded, known := f.kb.PageLoaded(f.pid); !f.browser || !known || loaded {
			at := f.kb.Focused(f.pid)
			if at != nil && f.browser && !at.OnPage() {
				at = nil // the address field, say: the page's form is the one to fill
			}
			switch kind(at) {
			case macos.FieldPassword:
				return nil, at, nil
			case macos.FieldText:
				if f.browser {
					_, pass = pick(f.kb.TextFields(f.pid, at), at)
				}
				return at, pass, nil
			}
			if f.browser {
				if user, pass = pick(f.kb.TextFields(f.pid, at), nil); user != nil || pass != nil {
					return user, pass, nil
				}
			}
		}
		if !time.Now().Before(deadline) || f.ctx.Err() != nil {
			return nil, nil, ErrNoField
		}
		sleep(f.ctx, 3*poll)
	}
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
	if at := f.kb.Focused(f.pid); kind(at) == macos.FieldPassword && (!f.browser || at.OnPage()) {
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
// in the page's order. With the focus on one, at, it takes that one, and
// the first password field after it. Without, it takes the first
// password field, and the text field nearest before it for the
// username; of a page that asks for the username alone, the text field
// that names itself so, or its only one.
func pick(fields []Field, at Field) (user, pass Field) {
	if at != nil {
		for i, fl := range fields {
			if !fl.Same(at) {
				continue
			}
			for _, p := range fields[i+1:] {
				if p.Kind() == macos.FieldPassword {
					return at, p
				}
			}
			return at, nil
		}
		return nil, nil
	}
	kinds := make([]macos.FieldKind, len(fields))
	for i, fl := range fields {
		kinds[i] = fl.Kind()
	}
	for i, k := range kinds {
		if k != macos.FieldPassword {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			if kinds[j] == macos.FieldText {
				return fields[j], fields[i]
			}
		}
		return nil, fields[i]
	}
	var texts []Field
	for i, fl := range fields {
		if kinds[i] == macos.FieldText {
			texts = append(texts, fl)
		}
	}
	for _, fl := range texts {
		if loginHint.MatchString(fl.Hint()) {
			return fl, nil
		}
	}
	if len(texts) == 1 {
		return texts[0], nil
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
