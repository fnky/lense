package fill

import (
	"time"

	"lense/internal/macos"
)

// fillPage fills a browser's page as Bitwarden's browser extension does:
// the username's fields, then the password's, then the one-time
// password's, each set at once, and the focus left on the last password
// field filled, else the last field.
func (f *filler) fillPage(req Request, wait time.Duration) error {
	plan, err := f.findLogin(req, wait)
	if err != nil {
		return err
	}
	var last, lastPassword Field
	put := func(field Field, value string, secret bool) error {
		if value == "" || field == nil {
			return nil
		}
		if err := f.put(field, value, secret); err != nil {
			return err
		}
		last = field
		if secret {
			lastPassword = field
		}
		return nil
	}
	for _, u := range plan.usernames {
		if err := put(u, req.Username, false); err != nil {
			return err
		}
	}
	for _, p := range plan.passwords {
		if err := put(p, req.Password, true); err != nil {
			return err
		}
	}
	if code := []rune(req.TOTP); len(plan.totps) > 1 && len(plan.totps) == len(code) {
		// A box for each of its characters.
		for i, c := range code {
			if err := put(plan.totps[i], string(c), false); err != nil {
				return err
			}
		}
	} else if err := put(plan.totp, req.TOTP, false); err != nil {
		return err
	}
	switch {
	case lastPassword != nil:
		lastPassword.Focus()
	case last != nil:
		last.Focus()
	default:
		return ErrNoField // a page that asks for a code, of a login without one
	}
	return nil
}

// findLogin finds the fields to fill, as a page loads and shows them on
// one of the login's websites, and plans their fill.
func (f *filler) findLogin(req Request, wait time.Duration) (loginPlan, error) {
	start, deadline := time.Now(), time.Now().Add(wait)
	err := ErrNoField
	seen := -1
	for {
		if loaded, known := f.kb.PageLoaded(f.pid); !known || loaded {
			at := f.kb.Focused(f.pid)
			if at != nil {
				if in := at.Info(); !in.OnPage || in.InFrame {
					at = nil // the address field, say: the page's form is the one to fill
				}
			}
			fields := f.kb.Fields(f.pid, at)
			// A browser tells of a page's fields as it builds what it tells
			// of them, a while after it is asked to, as a page draws its
			// form: its fields are those of two looks in a row, or, of one
			// that keeps changing them, of the look halfway through.
			settled := len(fields) == seen || time.Since(start) > wait/2
			seen = len(fields)
			if settled {
				if plan := planLogin(fields, at); plan.fills(req) {
					// A page on its way to the login's, as through a
					// redirect, may get there yet.
					if _, err = f.onLogins(plan.first()); err == nil {
						return plan, nil
					}
				} else if framedLogin(fields).fills(req) {
					err = ErrInFrame // to tell why the page is left as it is
				}
			}
		}
		if !time.Now().Before(deadline) || f.ctx.Err() != nil {
			return loginPlan{}, err
		}
		sleep(f.ctx, 3*poll)
	}
}

// framedLogin plans a fill of the fields of a page's frames, as if they
// were the page's own, which no fill fills, to tell why it fills none.
func framedLogin(fields []Field) loginPlan {
	var framed []Field
	for _, fl := range fields {
		if fl.Info().InFrame {
			framed = append(framed, unframed{fl})
		}
	}
	return planLogin(framed, nil)
}

// unframed is a field of a frame, as if it were the page's.
type unframed struct{ Field }

func (u unframed) Info() (in macos.FieldInfo) {
	in = u.Field.Info()
	in.InFrame = false
	return in
}

// put puts a value into a field, as Bitwarden's extension does: the field
// focused, then its value set at once, which the page takes as its input
// and change events tell it. A field that holds it already is left as it
// is; one that doesn't take it so gets it typed.
func (f *filler) put(field Field, value string, secret bool) error {
	if !secret && field.Value() == value {
		return nil
	}
	if !f.focus(field) {
		return ErrNotTaken
	}
	if err := f.commit(field); err != nil {
		return err
	}
	if field.SetValue(value) && waitFor(f.ctx, settle, func() bool { return holds(field, value, secret) }) {
		return nil
	}
	return f.typeInto(field, value, secret)
}
