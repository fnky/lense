package fill

import (
	"strings"
	"unicode"

	"lense/internal/macos"
)

// The words Bitwarden's browser extension knows the fields of a login form
// by, in their identifiers, labels and placeholders.
var (
	emailWords    = []string{"email", "email address", "e-mail", "e-mail address", "email adresse", "e-mail adresse"}
	usernameWords = append([]string{"username", "user name", "userid", "user id", "customer id", "login id",
		"benutzername", "benutzer name", "benutzerid", "benutzer id"}, emailWords...)
	// loginWords name a username's field less surely.
	loginWords = []string{"login"}
	totpWords  = []string{"2facode", "approvals_code", "mfacode", "onetimecode", "onetimepassword", "otc-code",
		"otp-code", "otpcode", "second-factor", "security_code", "security code", "totp", "totpcode", "twofa",
		"twofactor", "twofactorcode", "verificationcode", "verification code", "otc-confirmation"}
	// shortTOTPWords name a one-time password's field as a whole word;
	// vagueTOTPWords less surely, anywhere.
	shortTOTPWords = []string{"otc", "otp", "2fa", "mfa"}
	vagueTOTPWords = append([]string{"code", "pin"}, shortTOTPWords...)
	recoveryWords  = []string{"backup", "recovery"}
	searchWords    = []string{"search", "query", "find", "go"}
	// ignoredWords name fields no login fills.
	ignoredWords     = []string{"captcha", "findanything", "forgot"}
	passwordWords    = []string{"password", "passwd"}
	notPasswordWords = append(append([]string{"hint"}, ignoredWords...), totpWords...)
	signUpWords      = []string{"register", "signup", "sign-up", "join", "create"}
	newsletterWords  = []string{"newsletter"}
	subscribeWords   = []string{"mailing list", "subscribe", "subscription", "unsubscribe"}
)

// field is a field of a page as a fill plans by it: what it told of
// itself, and the words it is named by.
type field struct {
	Field
	info  macos.FieldInfo
	words []string
}

// wordsOf returns the words a field is named by, as Bitwarden gathers
// them: its identifier, classes, type, title, placeholder and labels, in
// lower case, each as a whole, in pieces between the characters that are
// no letter or digit, hyphens taken out, and so with spaces taken out
// too, so that "User name" gives "username".
func wordsOf(in macos.FieldInfo) []string {
	var out []string
	for _, v := range []string{in.ID, in.Classes, in.Type, in.Help, in.Placeholder, in.AriaLabel, in.Label} {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		out = append(out, v)
		out = append(out, pieces(strings.ReplaceAll(v, "-", ""))...)
		out = append(out, pieces(strings.NewReplacer("-", "", " ", "").Replace(v))...)
	}
	return out
}

// pieces splits s between the characters that are no letter or digit.
func pieces(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// named reports whether any of words appears within the words a field is
// named by, hyphens taken out of it.
func (fl field) named(words []string) bool {
	all := strings.Join(fl.words, ",")
	for _, w := range words {
		if strings.Contains(all, strings.ReplaceAll(strings.ToLower(w), "-", "")) {
			return true
		}
	}
	return false
}

// namedExactly reports whether any of words is one of the words a field is
// named by.
func (fl field) namedExactly(words []string) bool {
	for _, w := range words {
		for _, v := range fl.words {
			if v == w {
				return true
			}
		}
	}
	return false
}

func (fl field) typeIs(types ...string) bool {
	for _, t := range types {
		if fl.info.Type == t {
			return true
		}
	}
	return false
}

// usable reports whether a fill may put a value into the field: one on
// the page, not in a frame of it, neither disabled nor readonly.
func (fl field) usable() bool {
	return fl.info.OnPage && !fl.info.InFrame && fl.info.Enabled && fl.info.Settable
}

// searching reports whether the field is a search field: of type search,
// or whose identifier, placeholder or type has a word of searchWords, as
// its camel case and other characters part them.
func (fl field) searching() bool {
	if fl.info.Type == "search" {
		return true
	}
	for _, v := range []string{fl.info.ID, fl.info.Placeholder, fl.info.Type} {
		for _, w := range camelPieces(v) {
			for _, s := range searchWords {
				if w == s {
					return true
				}
			}
		}
	}
	return false
}

// camelPieces splits s into its words, lower case, between its characters
// that are no letter and before the capitals of its camel case.
func camelPieces(s string) []string {
	var b strings.Builder
	prev := rune(0)
	for _, r := range s {
		if unicode.IsUpper(r) && unicode.IsLower(prev) {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	return strings.FieldsFunc(strings.ToLower(b.String()), func(r rune) bool { return !unicode.IsLetter(r) })
}

// ignored reports whether the field is one no login fills, as a captcha's,
// by its identifier or placeholder.
func (fl field) ignored() bool {
	squash := strings.NewReplacer(" ", "", "_", "", "-", "")
	for _, v := range []string{fl.info.ID, fl.info.Placeholder} {
		v = squash.Replace(strings.ToLower(v))
		for _, w := range ignoredWords {
			if strings.Contains(v, w) {
				return true
			}
		}
	}
	return false
}

// password reports whether the field takes a login's password: a password
// field, or a text field named a password's, one that shows it as it is
// typed, but no one-time password's.
func (fl field) password() bool {
	if fl.ignored() || fl.named(totpWords) {
		return false
	}
	switch fl.info.Type {
	case "password":
		return true
	case "text":
		squash := strings.NewReplacer(" ", "", "_", "", "-", "")
		for _, v := range []string{fl.info.ID, fl.info.Placeholder, fl.info.AriaLabel, fl.info.Label} {
			v = squash.Replace(strings.ToLower(v))
			for _, w := range passwordWords {
				if strings.Contains(v, w) && !fl.named(notPasswordWords) {
					return true
				}
			}
		}
	}
	return false
}

// signUp reports whether the field is of a form that signs up, rather than
// in, by its identifier or its form's.
func (fl field) signUp() bool {
	v := strings.ToLower(fl.info.ID + " " + fl.info.FormHint)
	for _, w := range signUpWords {
		if strings.Contains(v, w) {
			return true
		}
	}
	return false
}

// loginPlan is the fields a login fills: its username into usernames, its
// password into passwords, its one-time password into totp, or one
// character of it into each of totps, as many as it has.
type loginPlan struct {
	usernames, passwords, totps []Field
	totp                        Field
}

// first returns the field the plan fills first.
func (p loginPlan) first() Field {
	switch {
	case len(p.usernames) > 0:
		return p.usernames[0]
	case len(p.passwords) > 0:
		return p.passwords[0]
	}
	return p.totp
}

// fills reports whether the plan fills anything of the request.
func (p loginPlan) fills(req Request) bool {
	return len(p.usernames) > 0 && req.Username != "" || len(p.passwords) > 0 && req.Password != "" || p.totp != nil && req.TOTP != ""
}

// planLogin plans a login's fill of a page's fields, found in the page's
// order, as Bitwarden's browser extension does. at is the field with the
// focus, around which, as around the field the extension's menu is open
// on, the fill keeps to its form. Fields in frames, on no page, disabled,
// readonly or out of sight are filled by none.
func planLogin(found []Field, at Field) loginPlan {
	var fields []field
	forms := false
	for _, f := range found {
		in := f.Info()
		if !in.OnPage || in.InFrame {
			continue
		}
		fields = append(fields, field{Field: f, info: in, words: wordsOf(in)})
		forms = forms || in.Form != 0
	}
	// Forms that sign up for a newsletter fill no username.
	newsletter := map[uint64]bool{}
	for _, fl := range fields {
		if fl.info.Form != 0 && (strings.Contains(strings.ToLower(fl.info.FormHint), "newsletter") || fl.named(newsletterWords)) {
			newsletter[fl.info.Form] = true
		}
	}
	focus := -1
	for i, fl := range fields {
		if at != nil && fl.Same(at) {
			focus = i
		}
	}

	// The passwords' fields: those of the form with the focus, where it
	// has any; those of a form that signs in before those of one that
	// signs up.
	var in, up []int
	for i, fl := range fields {
		if fl.usable() && fl.info.Viewable && fl.password() {
			if fl.signUp() {
				up = append(up, i)
			} else {
				in = append(in, i)
			}
		}
	}
	passwords := in
	if len(passwords) == 0 {
		passwords = up
	}
	if focus >= 0 {
		var mine []int
		for _, i := range passwords {
			if fields[i].info.Form == fields[focus].info.Form {
				mine = append(mine, i)
			}
		}
		if len(mine) > 0 {
			passwords = mine
		}
	}
	if len(passwords) == 0 {
		return planWithoutPassword(fields, focus, newsletter)
	}

	var plan loginPlan
	for _, i := range passwords {
		plan.passwords = append(plan.passwords, fields[i].Field)
	}
	form := fields[passwords[0]].info.Form
	// The username's: the field with the focus, where it is one for a
	// username in the passwords' form; else, before each password field,
	// the first of its form named a username's, the nearest of its form,
	// or the nearest of another named so.
	if focus >= 0 {
		fl := fields[focus]
		if fl.usable() && fl.typeIs("text", "email", "tel") && !fl.searching() && !fl.ignored() && !fl.named(totpWords) && fl.info.Form == form {
			plan.usernames = []Field{fl.Field}
		}
	}
	if len(plan.usernames) == 0 {
		seen := map[int]bool{}
		for _, p := range passwords {
			if u := usernameBefore(fields, p, forms, newsletter); u >= 0 && !seen[u] {
				seen[u] = true
				plan.usernames = append(plan.usernames, fields[u].Field)
			}
		}
	}
	// The one-time password's, in the passwords' form: the last named so,
	// unless one is named exactly so; or one for each character of it.
	exact := false
	for _, fl := range fields {
		if !fl.usable() || !fl.info.Viewable || !fl.typeIs("text", "number", "tel") || fl.ignored() || fl.password() {
			continue
		}
		if forms && fl.info.Form != form || !fl.named(totpWords) && !fl.named(vagueTOTPWords) || fl.named(recoveryWords) {
			continue
		}
		plan.totps = append(plan.totps, fl.Field)
		if !exact {
			plan.totp, exact = fl.Field, fl.namedExactly(totpWords)
		}
	}
	return plan
}

// usernameBefore returns the username's field for the password field p,
// among those before it: the first in its form named a username's, else
// the nearest in its form, else the nearest elsewhere named so; -1 for
// none. A page without forms is as one form; an email or phone field of
// the form fills unseen, as one behind its label.
func usernameBefore(fields []field, p int, forms bool, newsletter map[uint64]bool) int {
	same, other := -1, -1
	for i, fl := range fields[:p] {
		if !fl.usable() || !fl.typeIs("text", "email", "tel") || fl.searching() || fl.ignored() || fl.named(totpWords) || newsletter[fl.info.Form] {
			continue
		}
		mine := fl.info.Form == fields[p].info.Form
		if !fl.info.Viewable && !(mine && fl.typeIs("email", "tel")) {
			continue
		}
		named := fl.named(usernameWords)
		switch {
		case mine && named:
			return i
		case mine:
			same = i
		case !forms || named:
			other = i
		}
	}
	if same >= 0 {
		return same
	}
	return other
}

// planWithoutPassword plans the fill of a page without a password field,
// as the first of two that ask for the username, then the password, or
// one that asks for the one-time password alone. Its fields rank as
// Bitwarden ranks them: one named a one-time password's; one named a
// username's; one named otp, 2fa or the like; one named login; one named
// code or pin. Those of the first rank there is fill, or the field with
// the focus alone, where it is one of them.
func planWithoutPassword(fields []field, focus int, newsletter map[uint64]bool) loginPlan {
	// subscribes reports whether a field asks for an address to send news
	// to: one of a form that says so.
	subscribes := func(fl field) bool {
		if newsletter[fl.info.Form] {
			return true
		}
		for _, o := range fields {
			if o.info.Form == fl.info.Form && o.named(subscribeWords) {
				return true
			}
		}
		return false
	}
	rank := func(fl field) int {
		if !fl.usable() || !fl.info.Viewable || fl.searching() || fl.ignored() || fl.named(recoveryWords) {
			return 0
		}
		code, text := fl.typeIs("text", "number", "tel"), fl.typeIs("text", "email", "tel")
		switch {
		case code && fl.named(totpWords):
			return 1
		case text && fl.named(usernameWords) && !subscribes(fl):
			return 2
		case code && fl.namedExactly(shortTOTPWords):
			return 3
		case text && fl.named(loginWords) && !subscribes(fl):
			return 4
		case code && fl.named(vagueTOTPWords):
			return 5
		}
		return 0
	}
	best := 0
	var chosen []field
	for _, fl := range fields {
		switch r := rank(fl); {
		case r == 0:
		case best == 0 || r < best:
			best, chosen = r, []field{fl}
		case r == best:
			chosen = append(chosen, fl)
		}
	}
	if focus >= 0 && rank(fields[focus]) != 0 {
		best, chosen = rank(fields[focus]), []field{fields[focus]}
	}
	var plan loginPlan
	for _, fl := range chosen {
		if best == 2 || best == 4 {
			plan.usernames = append(plan.usernames, fl.Field)
		} else {
			plan.totps = append(plan.totps, fl.Field)
			plan.totp = fl.Field
		}
	}
	return plan
}
