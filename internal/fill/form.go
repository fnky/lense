package fill

import "time"

// How long a menu takes what is typed into it, which tests shorten: a
// moment for it to show what it chose, and a while for it to forget what
// was typed, to take another start.
var choiceWait, typeAhead = 400 * time.Millisecond, 1100 * time.Millisecond

// formField is a field a card or an identity fills: the value it takes,
// and for a menu, the ways it may name it, in turn, and whether what it
// shows chosen names it.
type formField struct {
	field   Field
	value   string
	choices []string
	shows   func(shown string) bool
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
			choices, shows := menuChoices(*req.Card, cf, v)
			out = append(out, formField{cf.field.Field, v, choices, shows})
		}
	case req.Identity != nil:
		for _, idf := range planIdentity(found) {
			v := identityValue(*req.Identity, idf.part)
			if v == "" {
				continue
			}
			choices, shows := identityChoices(*req.Identity, idf.part, v)
			out = append(out, formField{idf.field.Field, v, choices, shows})
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
			ok, err := f.choose(ff.field, ff.choices, ff.shows)
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

// choose chooses the option of a menu that names a value, as the user
// would, typing the start of it: each of choices in turn, till the menu
// shows one it names. A menu that shows none goes back to the option it
// had, as Bitwarden's extension leaves a menu of no such option as it is.
func (f *filler) choose(field Field, choices []string, shows func(string) bool) (bool, error) {
	before := field.Value()
	if shows(before) {
		return true, nil
	}
	for i, c := range choices {
		if !f.focus(field) {
			return false, ErrNotTaken
		}
		if err := f.commit(field); err != nil {
			return false, err
		}
		if i > 0 {
			sleep(f.ctx, typeAhead)
		}
		f.kb.TypeText(f.pid, c, keyPause)
		if waitFor(f.ctx, choiceWait, func() bool { return shows(field.Value()) }) {
			return true, nil
		}
	}
	if before != "" && field.Value() != before {
		sleep(f.ctx, typeAhead)
		if err := f.commit(field); err != nil {
			return false, err
		}
		f.kb.TypeText(f.pid, before, keyPause)
	}
	return false, nil
}
