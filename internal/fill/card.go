package fill

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Card is a card to fill into a page.
type Card struct {
	Name, Number, Brand string
	// ExpMonth is "1" to "12"; ExpYear as it was saved, "2027" or "27".
	ExpMonth, ExpYear string
	Code              string
}

// words is a list of words a field is named by, as Bitwarden's extension
// keeps them: a word matches a field's name as all of it, or, where it is
// one of within, within it. A list without within matches within it all.
type words struct {
	all, within []string
}

func (w words) match(name string) bool {
	for _, k := range w.all {
		k = strings.ReplaceAll(strings.ToLower(k), "-", "")
		if name == k {
			return true
		}
	}
	for _, k := range w.within {
		k = strings.ReplaceAll(strings.ToLower(k), "-", "")
		if strings.Contains(name, k) {
			return true
		}
	}
	return false
}

// withinAll is a list whose words match within a name, each of them.
func withinAll(ws ...string) words { return words{within: ws} }

// The words Bitwarden's extension knows a card's fields by. Most pages
// tell it the card's name's field by its autocomplete, cc-name, which
// accessibility doesn't tell: its label, "Name on card", stands in.
var (
	cardNameWords = words{
		all:    []string{"name", "nom"},
		within: []string{"accountholdername", "cc-name", "card-name", "cardholder-name", "cardholder", "name-on-card"},
	}
	cardNumberWords = words{
		all:    []string{"number", "cc", "credit-card", "carte", "carte-credit", "card-pan"},
		within: []string{"cc-number", "cc-num", "card-number", "card-num", "cc-no", "card-no", "numero-carte", "num-carte", "cb-num"},
	}
	cardExpWords = words{
		all: []string{"cc-exp", "card-exp", "cc-expiration", "card-expiration", "cc-ex", "card-ex", "card-expire",
			"card-expiry", "validite", "expiration", "expiry", "payment-cc-date"},
		within: []string{"mm-yy", "mm-yyyy", "yy-mm", "yyyy-mm", "expiration-date", "payment-card-expiration"},
	}
	cardMonthWords = withinAll("exp-month", "cc-exp-month", "cc-month", "card-month", "cc-mo", "card-mo", "exp-mo",
		"card-exp-mo", "cc-exp-mo", "card-expiration-month", "expiration-month", "cc-mm", "cc-m", "card-mm", "card-m",
		"card-exp-mm", "cc-exp-mm", "exp-mm", "exp-m", "expire-month", "expire-mo", "expiry-month", "expiry-mo",
		"card-expire-month", "card-expire-mo", "card-expiry-month", "card-expiry-mo", "mois-validite",
		"mois-expiration", "m-validite", "m-expiration", "expiry-date-field-month", "expiration-date-month",
		"expiration-date-mm", "exp-mon", "validity-mo", "exp-date-mo", "cb-date-mois", "date-m")
	cardYearWords = withinAll("exp-year", "cc-exp-year", "cc-year", "card-year", "cc-yr", "card-yr", "exp-yr",
		"card-exp-yr", "cc-exp-yr", "card-expiration-year", "expiration-year", "cc-yy", "cc-y", "card-yy", "card-y",
		"card-exp-yy", "cc-exp-yy", "exp-yy", "exp-y", "cc-yyyy", "card-yyyy", "card-exp-yyyy", "cc-exp-yyyy",
		"expire-year", "expire-yr", "expiry-year", "expiry-yr", "card-expire-year", "card-expire-yr",
		"card-expiry-year", "card-expiry-yr", "an-validite", "an-expiration", "annee-validite",
		"annee-expiration", "expiry-date-field-year", "expiration-date-year", "cb-date-ann", "expiration-date-yy",
		"expiration-date-yyyy", "validity-year", "exp-date-year", "date-y")
	cardCodeWords = withinAll("cvv", "cvc", "cvv2", "cc-csc", "cc-cvv", "card-csc", "card-cvv", "cvd", "cid", "cvc2",
		"cnv", "cvn2", "cc-code", "card-code", "code-securite", "security-code", "crypto", "card-verif",
		"verification-code", "csc", "ccv")
	cardBrandWords = withinAll("cc-type", "card-type", "card-brand", "cc-brand", "cb-type")
)

// squash puts a name as Bitwarden's extension compares it: in lower case,
// of its letters and digits alone.
func squash(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// names returns what names a field, in the order Bitwarden's extension
// looks at them for a card: its identifier, title, label and placeholder.
func names(in field) []string {
	return []string{in.info.ID, in.info.Help, in.info.Label, in.info.AriaLabel, in.info.Placeholder}
}

// cardField is a field of a card's: what it takes, and the field.
type cardField struct {
	part  string // name, number, exp, month, year, code or brand
	field field
}

// cardParts are what a card fills, in the order a field is tried for them.
var cardParts = []struct {
	part  string
	words words
}{
	{"name", cardNameWords}, {"number", cardNumberWords}, {"exp", cardExpWords}, {"month", cardMonthWords},
	{"year", cardYearWords}, {"code", cardCodeWords}, {"brand", cardBrandWords},
}

// planCard plans a card's fill of a page's fields, as Bitwarden's extension
// does: each field, in the page's order, by its names in turn, takes the
// first part of a card whose words match one, of those no field took yet.
func planCard(found []Field) []cardField {
	var out []cardField
	taken := map[string]bool{}
	for _, fl := range fieldsOf(found) {
		if !fillable(fl) {
			continue
		}
	field:
		for _, name := range names(fl) {
			if name = squash(name); name == "" {
				continue
			}
			for _, p := range cardParts {
				if !taken[p.part] && p.words.match(name) {
					taken[p.part] = true
					out = append(out, cardField{p.part, fl})
					break field
				}
			}
		}
	}
	return out
}

// fieldsOf reads what a page's fields tell, those on the page, not in a
// frame of it.
func fieldsOf(found []Field) []field {
	var out []field
	for _, f := range found {
		if in := f.Info(); in.OnPage && !in.InFrame {
			out = append(out, field{Field: f, info: in, words: wordsOf(in)})
		}
	}
	return out
}

// fillable reports whether a card or an identity fills the field: one a
// fill may put a value into, in sight, of a type that takes one, and no
// search field.
func fillable(fl field) bool {
	return fl.usable() && fl.info.Viewable && !fl.searching() &&
		fl.typeIs("text", "email", "tel", "number", "password", "select", "textarea", "url")
}

// cardValue returns what a card puts into a field of it, as the field's
// names ask: a month or a year of two or four digits, an expiration as
// "MM/YY" or as its placeholder shows.
func cardValue(c Card, cf cardField) string {
	hints := strings.ToLower(strings.Join(names(cf.field), " "))
	month, _ := strconv.Atoi(c.ExpMonth)
	switch cf.part {
	case "name":
		return c.Name
	case "number":
		return c.Number
	case "code":
		return c.Code
	case "brand":
		return c.Brand
	case "month":
		if month == 0 {
			return ""
		}
		if strings.Contains(strings.ReplaceAll(hints, " ", ""), "mm") {
			return fmt.Sprintf("%02d", month)
		}
		return strconv.Itoa(month)
	case "year":
		switch {
		case strings.Contains(hints, "yyyy"):
			return fullYear(c.ExpYear)
		case strings.Contains(hints, "yy"):
			return shortYear(c.ExpYear)
		}
		return c.ExpYear
	case "exp":
		if month == 0 || c.ExpYear == "" {
			return ""
		}
		return expiration(month, c.ExpYear, hints)
	}
	return ""
}

// fullYear returns a year of four digits: 2027 of 27.
func fullYear(y string) string {
	if len(y) == 2 {
		return strconv.Itoa(time.Now().Year()/100) + y
	}
	return y
}

// shortYear returns a year of two digits: 27 of 2027.
func shortYear(y string) string {
	if len(y) == 4 {
		return y[2:]
	}
	return y
}

// expHint finds how a field shows an expiration, as its placeholder
// "MM/YY": a month and a year, either first, with one separator or none
// between them, in the letters of several languages.
var expHint = regexp.MustCompile(`(мм|mm|м|m)([/\-. ]?)(yyyy|åååå|jjjj|aaaa|aa|гггг|rrrr|yy|åå|jj|гг|rr)|(yyyy|åååå|jjjj|aaaa|aa|гггг|rrrr|yy|åå|jj|гг|rr)([/\-. ]?)(мм|mm|м|m)`)

// longYears are the letters of a year of four digits.
var longYears = map[string]bool{"yyyy": true, "åååå": true, "jjjj": true, "aaaa": true, "aa": true, "гггг": true, "rrrr": true}

// expiration formats an expiration as hints shows it, else "MM/YY".
func expiration(month int, year, hints string) string {
	m := expHint.FindStringSubmatch(hints)
	if m == nil {
		return fmt.Sprintf("%02d/%s", month, shortYear(year))
	}
	if m[1] != "" { // the month first
		y := shortYear(year)
		if longYears[m[3]] {
			y = fullYear(year)
		}
		mo := strconv.Itoa(month)
		if len([]rune(m[1])) == 2 {
			mo = fmt.Sprintf("%02d", month)
		}
		return mo + m[2] + y
	}
	y := shortYear(year)
	if longYears[m[4]] {
		y = fullYear(year)
	}
	mo := strconv.Itoa(month)
	if len([]rune(m[6])) == 2 {
		mo = fmt.Sprintf("%02d", month)
	}
	return y + m[5] + mo
}

// monthNames are the months, in English, as a menu may name them.
var monthNames = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}

// menuPick returns which item of a menu, by their titles, names a card's
// value: a month's, of twelve items, or thirteen, one by its place, as
// Bitwarden's extension picks it, else the one titled as "03", "3",
// "March" or "Mar"; a year's titled as "2027" or "27".
func menuPick(c Card, cf cardField, value string) func(items []string) int {
	switch month, _ := strconv.Atoi(c.ExpMonth); cf.part {
	case "month":
		if month < 1 || month > 12 {
			return firstOf(func(string) bool { return false })
		}
		name := strings.ToLower(monthNames[month-1][:3])
		titled := firstOf(func(t string) bool {
			if n, err := strconv.Atoi(leadingDigits(t)); err == nil {
				return n == month
			}
			return strings.HasPrefix(strings.ToLower(t), name)
		})
		return func(items []string) int {
			switch len(items) {
			case 12:
				return month - 1
			case 13:
				// The months first, of a last of none; else after one that
				// names none, as "Month".
				if items[0] != "" && items[12] == "" {
					return month - 1
				}
				return month
			}
			return titled(items)
		}
	case "year":
		full, short := fullYear(c.ExpYear), shortYear(c.ExpYear)
		return firstOf(func(t string) bool {
			d := leadingDigits(t)
			return d == full || d == short
		})
	}
	return firstOf(func(t string) bool { return strings.EqualFold(t, value) })
}

// leadingDigits returns the digits s begins with.
func leadingDigits(s string) string {
	i := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsDigit(r) })
	if i < 0 {
		return s
	}
	return s[:i]
}
