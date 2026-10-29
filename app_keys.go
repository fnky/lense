package main

import (
	"strconv"
	"strings"

	"lense/internal/bw"
	"lense/internal/macos"

	"github.com/egoist/mygo/ui"
)

// searchKey takes the keys of Quick Access in the search, which has the
// keyboard: the search edits with those it leaves. Command-C copies the
// search's text while some of it is selected.
func (a *app) searchKey(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	if a.searchSel && ev.Mods == ui.Super && ev.Key == ui.KeyC {
		return false
	}
	return a.press(ev.Mods, ev.Key)
}

// press does what a key does in the mode Quick Access is in, reporting
// whether it did something.
func (a *app) press(mods ui.Modifiers, key ui.Key) bool {
	if a.large != nil {
		a.large = nil // any key puts Large Type away
		return true
	}
	if a.confirm != nil {
		return false
	}
	id := a.cfg.actionFor(a.mode, mods, key)
	if id == "" {
		// Typing goes back to the search, from the fields or the vaults.
		if a.mode != modeResults && mods&(ui.Super|ui.Ctrl) == 0 && types(key) {
			a.setMode(modeResults)
		}
		return false
	}
	return a.run(id)
}

// types reports whether a key types text into the search.
func types(key ui.Key) bool {
	name, ok := keyNames[key]
	return ok && (len(name) == 1 || key == ui.KeySpace || key == ui.KeyBackspace)
}

// run does an action, reporting whether it did: some leave their key to
// the search where it edits, as Right before the end of what is typed.
func (a *app) run(id string) bool {
	switch id {
	case "previous":
		a.move(-1)
	case "next":
		a.move(1)
	case "details":
		if a.selected() == nil || !a.caretAtEnd() {
			return false
		}
		a.enterDetails()
	case "default":
		a.activate()
	case "complete":
		if a.sel < 0 || a.sel >= len(a.rows) || a.rows[a.sel].comp == nil {
			return false
		}
		a.activate()
	case "field-previous":
		a.moveField(-1)
	case "field-next":
		a.moveField(1)
	case "field-reveal":
		a.revealField()
	case "field-back":
		a.setMode(modeResults)
	case "field-copy":
		// Copied by its key, the field is what the window showed for: it
		// goes, as it does once it fills. Clicked, the field stays.
		if it, f, ok := a.focusedField(); ok {
			a.copyFieldThen(it, f, a.close)
		}
	case "vault-previous":
		a.moveVault(-1)
	case "vault-next":
		a.moveVault(1)
	case "vault-expand":
		a.expandVault()
	case "vault-collapse":
		a.collapseVault()
	case "vault-choose":
		a.chooseVault(a.vaultCursor)
		a.setMode(modeResults)
	case "menu-previous":
		a.moveMenu(-1)
	case "menu-next":
		a.moveMenu(1)
	case "menu-choose":
		a.chooseMenu(a.menuSel)
	case "copy-username":
		a.copyPrimary()
	case "copy-password":
		a.copyPassword()
	case "copy-code":
		a.copyCode()
	case "open-fill":
		a.openFillMenu(a.selected(), false)
	case "open-window":
		a.openDetail()
	case "open-bitwarden":
		a.openInBitwarden()
	case "choose-vault":
		a.openVaults()
	case "search":
		a.setMode(modeResults)
		a.search.Focus()
	case "lock":
		a.lock(false)
	case "close":
		a.escape()
	case "hide":
		a.close()
	case "settings":
		a.openSettings()
	default:
		if n, ok := strings.CutPrefix(id, "vault-"); ok && len(n) == 1 && n[0] >= '1' && n[0] <= '9' {
			a.jumpVault(int(n[0] - '0'))
			return true
		}
		return false
	}
	return true
}

// caretAtEnd reports whether the search's caret is after all it holds,
// where Right goes to the fields rather than moving it.
func (a *app) caretAtEnd() bool {
	return !a.searchSel && a.searchCaret >= len([]rune(a.query))
}

// close puts Quick Access away, and forgets what it showed for the moment:
// secrets revealed, master passwords asked for. The vault stays unlocked.
func (a *app) close() {
	a.hidden()
	a.mode, a.fieldSel = modeResults, 0
	a.sys.hide()
}

// hidden forgets what the window revealed, as it goes, however it goes:
// the passwords shown, Large Type, and the master password given for the
// items that ask for it. The rest stays as it was for the next time.
func (a *app) hidden() {
	a.asked = false
	a.secureField("panel", false)
	a.reveal, a.reprompted = map[string]bool{}, map[string]bool{}
	a.large = nil
	a.codes, a.shownValues = nil, nil
	if a.mode == modeFillMenu {
		a.closeFillMenu()
	}
}

// --- The fields of the item chosen ---

// setMode moves the keys to another mode. Leaving the fields of an item
// conceals the passwords revealed there, and puts Large Type away.
func (a *app) setMode(m mode) {
	if a.mode == modeDetails && m != modeDetails {
		a.reveal = map[string]bool{}
		a.large = nil
		a.shownValues = nil
	}
	if m == modeVaults && a.mode != modeVaults {
		a.vaultBand = band{} // the selector opens at its top
	}
	a.mode = m
}

func (a *app) enterDetails() {
	it := a.selected()
	if it == nil || len(a.fieldsOf(it)) == 0 {
		return
	}
	a.mode, a.fieldSel = modeDetails, 0
}

func (a *app) moveField(n int) {
	it := a.selected()
	if it == nil {
		return
	}
	a.fieldSel = min(max(a.fieldSel+n, 0), len(a.fieldsOf(it))-1)
}

// focusedField returns the field the keys are on in the details, its
// value read now: a one-time password's changes.
func (a *app) focusedField() (*bw.Item, field, bool) {
	it := a.selected()
	if it == nil || a.mode != modeDetails {
		return nil, field{}, false
	}
	fs := a.fieldsOf(it)
	if a.fieldSel < 0 || a.fieldSel >= len(fs) {
		return nil, field{}, false
	}
	f := fs[a.fieldSel]
	if f.kind == "totp" {
		f.value, _, _ = a.code(it)
	}
	return it, f, true
}

// fieldKey names a field of an item, by its place among the item's
// fields, to reveal it alone.
func fieldKey(itemID string, i int) string { return itemID + "/" + strconv.Itoa(i) }

// revealField shows the secret field the keys are on, and no other, then
// shows it in Large Type; other fields go to Large Type at once.
func (a *app) revealField() {
	it, f, ok := a.focusedField()
	if !ok {
		return
	}
	if key := fieldKey(it.ID, a.fieldSel); f.secret && f.kind != "totp" && !a.reveal[key] {
		a.guarded(it, func() { a.reveal[key] = true })
		return
	}
	a.guarded(it, func() {
		v, err := f.open()
		if err != nil {
			a.say("Couldn't decrypt %s of %s", f.label, it.Name)
			return
		}
		a.large = &largeType{label: f.label, value: v, item: it.Name}
	})
}

// largeType is a value shown big enough to read across a room, or to type
// into another device.
type largeType struct {
	label, value, item string
}

// --- Choosing the vault ---

// vaultChoice is a vault to search, or a folder or collection in one.
type vaultChoice struct {
	id     string // "" all, "personal", an organization's ID, "folder:<id>", "collection:<id>"
	name   string
	parent string
	depth  int
	kids   bool
	num    int // the n of Cmd-n, or 0
}

// vaultTree lists the vaults, with the folders of the personal vault and
// the collections of organizations under them. Command and a digit
// choose the vaults, then what they hold, in order.
func (a *app) vaultTree() []vaultChoice {
	out := []vaultChoice{{id: "", name: "All Vaults"}}
	if a.vault == nil {
		out[0].num = 1
		return out
	}
	out = append(out, vaultChoice{id: "personal", name: "My Vault", kids: len(a.vault.Folders) > 0})
	for _, f := range a.vault.Folders {
		out = append(out, vaultChoice{id: "folder:" + f.ID, name: f.Name, parent: "personal", depth: 1})
	}
	for _, o := range a.vault.Organizations {
		i := len(out)
		out = append(out, vaultChoice{id: o.ID, name: o.Name})
		for _, c := range a.vault.Collections {
			if c.OrganizationID == o.ID {
				out = append(out, vaultChoice{id: "collection:" + c.ID, name: c.Name, parent: o.ID, depth: 1})
				out[i].kids = true
			}
		}
	}
	n := 0
	for _, top := range []bool{true, false} {
		for i := range out {
			if (out[i].depth == 0) == top && n < 9 {
				n++
				out[i].num = n
			}
		}
	}
	return out
}

// visibleVaults are the choices the selector shows: those of vaults
// opened.
func (a *app) visibleVaults() []vaultChoice {
	var out []vaultChoice
	for _, v := range a.vaultTree() {
		if v.depth == 0 || a.vaultOpen[v.parent] {
			out = append(out, v)
		}
	}
	return out
}

func (a *app) openVaults() {
	if a.mode == modeVaults {
		a.setMode(modeResults)
		return
	}
	a.setMode(modeVaults)
	a.vaultCursor = a.cfg.Vault
	if a.vaultOpen == nil {
		a.vaultOpen = map[string]bool{}
	}
	for _, v := range a.vaultTree() {
		if v.id == a.cfg.Vault && v.parent != "" {
			a.vaultOpen[v.parent] = true
		}
	}
}

func (a *app) vaultIndex(vs []vaultChoice) int {
	for i, v := range vs {
		if v.id == a.vaultCursor {
			return i
		}
	}
	return 0
}

func (a *app) moveVault(n int) {
	vs := a.visibleVaults()
	i := min(max(a.vaultIndex(vs)+n, 0), len(vs)-1)
	a.vaultCursor = vs[i].id
}

// expandVault shows what the vault holds, or goes to the first of it.
func (a *app) expandVault() {
	vs := a.visibleVaults()
	i := a.vaultIndex(vs)
	switch v := vs[i]; {
	case v.kids && !a.vaultOpen[v.id]:
		a.vaultOpen[v.id] = true
	case v.kids && i+1 < len(vs):
		a.vaultCursor = vs[i+1].id
	}
}

// collapseVault hides what the vault holds, goes to the vault of a folder
// or collection, or back to the results.
func (a *app) collapseVault() {
	vs := a.visibleVaults()
	switch v := vs[a.vaultIndex(vs)]; {
	case v.kids && a.vaultOpen[v.id]:
		a.vaultOpen[v.id] = false
	case v.parent != "":
		a.vaultCursor = v.parent
	default:
		a.setMode(modeResults)
	}
}

// jumpVault chooses the vault of Command-n.
func (a *app) jumpVault(n int) {
	for _, v := range a.vaultTree() {
		if v.num == n {
			a.chooseVault(v.id)
			a.setMode(modeResults)
			return
		}
	}
	a.say("No vault has ⌘%d", n)
}

// --- Return ---

// defaultAction is Return's: it fills a login where the user was when
// that is where the login is for, a page of one of its websites or its
// app, and else opens its website and fills it, or, of several, offers
// them in the Open and Fill menu. A login without a website fills the app the user was in when
// that app has a field to fill, as a browser's page, and copies its
// password otherwise; the other items copy what they hold first.
func (a *app) defaultAction(it *bw.Item) {
	if l := it.Login; l != nil && !l.Password.Empty() {
		switch {
		case a.onSite(it):
			a.fillInto(it, "")
		case launchURL(it) != "":
			a.openFillMenu(it, true)
		case a.fillTarget(it) != "":
			a.fillInto(it, "")
		default:
			a.copyField(it, field{label: "password", sec: l.Password, secret: true})
		}
		return
	}
	if a.fillsForm(it) {
		a.fillInto(it, "")
		return
	}
	if f, ok := primary(it); ok {
		a.copyField(it, f)
		return
	}
	a.say("%s has nothing to copy", it.Name)
}

// fillsForm reports whether Return fills a card's or an identity's fields
// of the page the user was on, as the page has them.
func (a *app) fillsForm(it *bw.Item) bool {
	return macos.IsBrowser(a.front.app.BundleID) && (it.Card != nil && a.front.cards || it.Identity != nil && a.front.identities)
}

// returnLabel says what Return does to it, for the footer, or "".
func (a *app) returnLabel(it *bw.Item) string {
	if a.fillsForm(it) {
		return "Fill in " + a.front.app.Name
	}
	if it.Login == nil || it.Login.Password.Empty() {
		if f, ok := primary(it); ok && (it.Card != nil || it.Identity != nil) {
			return "Copy " + f.label
		}
		return ""
	}
	if name := a.fillTarget(it); name != "" && (a.onSite(it) || launchURL(it) == "") {
		return "Fill in " + name
	}
	if launchURL(it) != "" {
		return a.openFillLabel()
	}
	return "Copy password"
}

// openFillLabel says what the Open and Fill menu does, in the browser
// that websites open in.
func (a *app) openFillLabel() string {
	if a.browser == "" {
		return "Open and Fill"
	}
	return "Open and Fill in " + a.browser
}

// onSite reports whether the user was where the login is for, with a
// field to fill it in: on a page of one of its websites, or in its app,
// as its suggestions say.
func (a *app) onSite(it *bw.Item) bool {
	return a.suggested[it.ID] && a.fillTarget(it) != ""
}

// fillTarget names where the user was, which Return can fill, or "": a
// browser on a page of one of the login's websites, or an app with a
// field focused.
func (a *app) fillTarget(it *bw.Item) string {
	t := a.front.app
	if it == nil || it.Login == nil || it.Login.Password.Empty() || t.PID == 0 {
		return ""
	}
	if macos.IsBrowser(t.BundleID) && a.suggested[it.ID] || !macos.IsBrowser(t.BundleID) && a.sys.fieldFocused(t) {
		if t.Name == "" {
			return "the app"
		}
		return t.Name
	}
	return ""
}
