package fill

import (
	"strings"
	"time"
	"unicode/utf8"

	"lense/internal/macos"
)

// fillApp fills an app's own fields, which tell little of themselves, as
// the user would: the field with the focus, the username's or the
// password's, and from the username's, with Tab, the password's, each
// typed into.
func (f *filler) fillApp(req Request, wait time.Duration) error {
	var user, pass Field
	deadline := time.Now().Add(wait)
	for {
		switch at := f.kb.Focused(f.pid); kind(at) {
		case macos.FieldPassword:
			pass = at
		case macos.FieldText:
			user = at
		}
		if user != nil || pass != nil {
			break
		}
		if !time.Now().Before(deadline) || f.ctx.Err() != nil {
			return ErrNoField
		}
		sleep(f.ctx, 3*poll)
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
		if f.kb.Frontmost().PID != f.pid {
			return ErrNotInFront // as Tab is to go
		}
		f.kb.PressKey(f.pid, macos.KeyTab)
		waitFor(f.ctx, settle, func() bool {
			at := f.kb.Focused(f.pid)
			return at != nil && !at.Same(user)
		})
		if at := f.kb.Focused(f.pid); kind(at) == macos.FieldPassword {
			pass = at
		}
	}
	if pass == nil || req.Password == "" {
		return nil // a form that asks for the username alone
	}
	return f.typeInto(pass, req.Password, true)
}

// typeInto types text over what the field holds, and waits for it to
// hold it: a field that takes less, as one that drops keys, gets it once
// more, typed slower.
func (f *filler) typeInto(field Field, text string, secret bool) error {
	for _, pause := range []time.Duration{keyPause, slowPause} {
		if !f.focus(field) {
			return ErrNotTaken
		}
		if err := f.commit(field); err != nil {
			return err
		}
		f.selectAll(field, secret)
		f.kb.TypeText(f.pid, text, pause)
		if waitFor(f.ctx, settle, func() bool { return holds(field, text, secret) }) {
			return nil
		}
	}
	if v := strings.TrimSpace(field.Value()); !secret && utf8.RuneCountInString(v) >= utf8.RuneCountInString(text) {
		return nil // a field that formats what it takes, as a phone number
	}
	return ErrNotTaken
}

// selectAll selects what the field holds, to type over it, and waits for
// the selection to take: Command-A goes its own way to a page, as an
// edit of the browser's, and a page busy with the keys before it would
// take keys typed after it before it, and lose them to the selection.
// A field that holds nothing needs none.
func (f *filler) selectAll(field Field, secret bool) {
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
