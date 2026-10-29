package fill

import (
	"strings"
	"time"

	"lense/internal/macos"
)

// formField is a field a card or an identity fills: the value it takes,
// and for a menu, which of its items, by their titles, names it, or -1.
type formField struct {
	field Field
	value string
	pick  func(items []string) int
}

// planForm plans the fill of a page's fields by the card or the identity
// of a request, as Bitwarden's extension plans it; fields left out are
// those of parts the card or the identity has nothing for.
func planForm(found []Field, req Request) []formField {
	var out []formField
	switch {
	case req.Card != nil:
		for _, cf := range planCard(found) {
			v := cardValue(*req.Card, cf)
			if v == "" {
				continue
			}
			out = append(out, formField{cf.field.Field, v, menuPick(*req.Card, cf, v)})
		}
	case req.Identity != nil:
		for _, idf := range planIdentity(found) {
			v := identityValue(*req.Identity, idf.part)
			if v == "" {
				continue
			}
			out = append(out, formField{idf.field.Field, v, identityPick(*req.Identity, idf.part, v)})
		}
	}
	return out
}

// Forms reports whether the page the browser of pid shows has a card's
// fields, its number's at least, and an identity's, of two parts beyond
// an email, a username or a phone number, as Bitwarden's extension offers
// to fill a card or an identity only where a page asks for one.
func Forms(kb Keyboard, pid int) (card, identity bool) {
	// The browser tells of the page as it builds what it tells, a while
	// after it's asked to: its fields are those of two looks in a row.
	fields := kb.Fields(pid, nil)
	for i := 0; i < 10; i++ {
		time.Sleep(3 * poll)
		again := kb.Fields(pid, nil)
		if len(again) == len(fields) {
			break
		}
		fields = again
	}
	for _, cf := range planCard(fields) {
		card = card || cf.part == "number"
	}
	parts := map[string]bool{}
	for _, idf := range planIdentity(fields) {
		switch idf.part {
		case "email", "username", "phone":
		default:
			parts[idf.part] = true
		}
	}
	return card, len(parts) >= 2
}

// fillForm fills a page's fields of a card or an identity, as Bitwarden's
// extension does: each that a part of it matches, in the page's order,
// set at once, or chosen of its menu.
func (f *filler) fillForm(req Request, wait time.Duration) error {
	plan, err := f.findForm(req, wait)
	if err != nil {
		return err
	}
	filled := false
	for _, ff := range plan {
		if ff.field.Info().Type == "select" {
			ok, err := f.choose(ff.field, ff.pick)
			if err != nil {
				return err
			}
			filled = filled || ok
			continue
		}
		if err := f.put(ff.field, ff.value, ff.field.Info().Type == "password"); err != nil {
			return err
		}
		filled = true
	}
	if !filled {
		return ErrNoField
	}
	return nil
}

// findForm finds the fields of a card or an identity to fill, as a page
// shows them all, and plans their fill.
func (f *filler) findForm(req Request, wait time.Duration) ([]formField, error) {
	start, deadline := time.Now(), time.Now().Add(wait)
	err := ErrNoField
	seen := -1
	for {
		if loaded, known := f.kb.PageLoaded(f.pid); !known || loaded {
			fields := f.kb.Fields(f.pid, nil)
			settled := len(fields) == seen || time.Since(start) > wait/2
			seen = len(fields)
			if settled {
				if plan := planForm(fields, req); len(plan) > 0 {
					if _, err = f.onLogins(plan[0].field); err == nil {
						return plan, nil
					}
				} else if len(planForm(unframedAll(fields), req)) > 0 {
					err = ErrInFrame // to tell why the page is left as it is
				}
			}
		}
		if !time.Now().Before(deadline) || f.ctx.Err() != nil {
			return nil, err
		}
		sleep(f.ctx, 3*poll)
	}
}

// unframedAll returns the fields of a page's frames, as if they were the
// page's own, which no fill fills, to tell why it fills none.
func unframedAll(fields []Field) []Field {
	var out []Field
	for _, fl := range fields {
		if fl.Info().InFrame {
			out = append(out, unframed{fl})
		}
	}
	return out
}

// choose chooses the option of a menu, a page's select, that pick picks
// of its items, as the user would: the menu opened with Space, and the
// item pressed. A menu of no such option it closes, as Bitwarden's
// extension leaves a select of no such option as it is, as one that
// doesn't show, where accessibility doesn't tell the browser's menus.
func (f *filler) choose(field Field, pick func(items []string) int) (bool, error) {
	if pick([]string{strings.TrimSpace(field.Value())}) == 0 {
		return true, nil // it shows the option already
	}
	if !f.focus(field) {
		return false, ErrNotTaken
	}
	if err := f.commit(field); err != nil {
		return false, err
	}
	f.kb.PressKey(f.pid, macos.KeySpace)
	var menu Menu
	if !waitFor(f.ctx, settle, func() bool { menu = f.kb.Menu(f.pid, field); return menu != nil }) {
		return false, nil
	}
	items := menu.Items()
	i := pick(items)
	if i < 0 || i >= len(items) || items[i] == "" {
		menu.Close()
		return false, nil
	}
	// The browser in front, on the page, as the item is pressed too.
	if err := f.commit(field); err != nil {
		menu.Close()
		return false, err
	}
	if !menu.Choose(i) {
		menu.Close()
		return false, ErrNotTaken
	}
	if !waitFor(f.ctx, settle, func() bool { return strings.EqualFold(strings.TrimSpace(field.Value()), items[i]) }) {
		return false, ErrNotTaken
	}
	return true, nil
}

// firstOf picks the first item of a menu whose title names says names
// the value.
func firstOf(names func(title string) bool) func(items []string) int {
	return func(items []string) int {
		for i, t := range items {
			if t != "" && names(t) {
				return i
			}
		}
		return -1
	}
}
