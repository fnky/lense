package fill

import (
	"errors"
	"testing"
)

var visa = Card{Name: "Ada Lovelace", Number: "4111111111111111", Brand: "Visa", ExpMonth: "8", ExpYear: "2029", Code: "123"}

// anyPage fills the card or identity of a request on every page.
func anyPage(r Request) Request {
	r.Matches = func(string) bool { return true }
	return r
}

func menu(id string, options ...string) *element {
	e := fieldOf("select", id)
	e.options, e.value = options, options[0]
	return e
}

func TestFillsACard(t *testing.T) {
	code := password("cvc")
	p := newPage(true, in(1, text("cardholder-name"), text("card-number"), text("exp-month"), text("exp-year"), code, text("card-type"))...)
	if err := fill(t, p, anyPage(Request{Card: &visa})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "cardholder-name", "Ada Lovelace", "card-number", "4111111111111111", "exp-month", "8", "exp-year", "2029", "cvc", "123", "card-type", "Visa")
}

func TestCardByLabelsAndPlaceholders(t *testing.T) {
	name, number, month, year, cvv := text("a"), text("b"), text("c"), text("d"), text("e")
	name.label, number.label, cvv.label = "Name on card", "Card number", "Security code"
	month.placeholder, year.placeholder = "Exp month (MM)", "Exp year (YY)"
	month.id, year.id = "exp-month", "exp-year"
	p := newPage(true, in(1, name, number, month, year, cvv)...)
	if err := fill(t, p, anyPage(Request{Card: &visa})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "a", "Ada Lovelace", "b", "4111111111111111", "c", "08", "d", "29", "e", "123")
}

func TestCardExpirationAsItShows(t *testing.T) {
	for placeholder, want := range map[string]string{
		"":        "08/29",
		"MM/YY":   "08/29",
		"MM/YYYY": "08/2029",
		"YYMM":    "2908",
		"M.YY":    "8.29",
		"MM-JJJJ": "08-2029",
		"MM / YY": "08/29", // a separator of one character, or none
		"mm yyyy": "08 2029",
		"ММ/ГГ":   "08/29",
		"MM / AA": "08/29",
		"YYYY-MM": "2029-08",
	} {
		exp := text("cc-exp")
		exp.placeholder = placeholder
		p := newPage(true, in(1, text("card-number"), exp)...)
		if err := fill(t, p, anyPage(Request{Card: &visa})); err != nil {
			t.Fatal(err)
		}
		if exp.value != want {
			t.Errorf("%q: %q, want %q", placeholder, exp.value, want)
		}
	}
}

func TestCardMenus(t *testing.T) {
	months := []string{"MM"}
	for _, m := range monthNames {
		months = append(months, m)
	}
	p := newPage(true, in(1, text("card-number"),
		menu("exp-month", "Month", "01 - January", "02 - February", "08 - August", "12 - December"),
		menu("exp-year", "Year", "2026", "2029", "2030"))...)
	if err := fill(t, p, anyPage(Request{Card: &visa})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "exp-month", "08 - August", "exp-year", "2029")
	// Months by name, years of two digits.
	p = newPage(true, in(1, text("card-number"), menu("exp-month", months...), menu("exp-year", "YY", "26", "29"))...)
	if err := fill(t, p, anyPage(Request{Card: &visa})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "exp-month", "August", "exp-year", "29")
	// A menu of no such option stays as it was.
	p = newPage(true, in(1, text("card-number"), menu("card-type", "Choose", "Mastercard"))...)
	if err := fill(t, p, anyPage(Request{Card: &visa})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "card-type", "Choose")
}

func TestCardFillsEachPartOnce(t *testing.T) {
	p := newPage(true, in(1, text("card-number"), text("cc-number"))...)
	if err := fill(t, p, anyPage(Request{Card: &visa})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "card-number", "4111111111111111", "cc-number", "")
}

func TestNoCardFields(t *testing.T) {
	p := newPage(true, in(1, text("username"), password("password"))...)
	if err := fill(t, p, anyPage(Request{Card: &visa})); !errors.Is(err, ErrNoField) {
		t.Errorf("err %v", err)
	}
	p.nothingFilled(t)
	// Nor in a frame, as a payment provider's.
	p = newPage(true, in(1, framed(text("card-number")), framed(text("cvc")))...)
	if err := fill(t, p, anyPage(Request{Card: &visa})); !errors.Is(err, ErrInFrame) {
		t.Errorf("err %v", err)
	}
	p.nothingFilled(t)
	// Nor an app's fields, which tell too little.
	p = newPage(false, text("card-number"))
	p.focus = p.find("card-number")
	if err := fill(t, p, anyPage(Request{Card: &visa})); !errors.Is(err, ErrNoField) {
		t.Errorf("err %v", err)
	}
}

var ada = Identity{Title: "Ms", First: "Ada", Middle: "Augusta", Last: "Lovelace", Address1: "12 Analytical Row", Address2: "Flat 2",
	City: "London", State: "Massachusetts", PostalCode: "02139", Country: "United States", Company: "Engines Ltd",
	Email: "ada@example.com", Phone: "+44 20 7946 0000", Username: "ada"}

func TestFillsAnIdentity(t *testing.T) {
	email, phone := fieldOf("email", "contact"), fieldOf("tel", "mobile")
	p := newPage(true, in(1, text("first-name"), text("middle-name"), text("last-name"), email, text("address-line-1"),
		text("address-line-2"), text("city"), text("state"), text("postal-code"), text("country"), phone, text("company"))...)
	if err := fill(t, p, anyPage(Request{Identity: &ada})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "first-name", "Ada", "middle-name", "Augusta", "last-name", "Lovelace", "contact", "ada@example.com",
		"address-line-1", "12 Analytical Row", "address-line-2", "Flat 2", "city", "London", "state", "MA",
		"postal-code", "02139", "country", "US", "mobile", "+44 20 7946 0000", "company", "Engines Ltd")
}

func TestIdentityWholes(t *testing.T) {
	p := newPage(true, in(1, text("name"), text("address"), text("shipping-city"), text("billing-city"))...)
	if err := fill(t, p, anyPage(Request{Identity: &ada})); err != nil {
		t.Fatal(err)
	}
	// The name of its names, the address of its lines, and every city.
	p.holds(t, "name", "Ada Augusta Lovelace", "address", "12 Analytical Row, Flat 2", "shipping-city", "London", "billing-city", "London")
}

func TestIdentityMenus(t *testing.T) {
	me := ada
	me.State, me.Country = "MA", "US"
	p := newPage(true, in(1, text("city"),
		menu("state", "Choose a state", "Maine", "Maryland", "Massachusetts"),
		menu("country", "Choose a country", "Germany", "United Kingdom", "United States"))...)
	if err := fill(t, p, anyPage(Request{Identity: &me})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "state", "Massachusetts", "country", "United States")
}

func TestIdentityOfAnUnknownState(t *testing.T) {
	me := ada
	me.State = "Bavaria"
	p := newPage(true, in(1, text("city"), text("state"))...)
	if err := fill(t, p, anyPage(Request{Identity: &me})); err != nil {
		t.Fatal(err)
	}
	p.holds(t, "city", "London", "state", "")
}

func TestForms(t *testing.T) {
	card := newPage(true, in(1, text("card-number"), text("cvc"))...)
	identity := newPage(true, in(1, text("first-name"), text("city"), fieldOf("email", "email"))...)
	login := newPage(true, in(1, fieldOf("email", "email"), password("password"))...)
	for name, tc := range map[string]struct {
		p              *page
		card, identity bool
	}{"card": {card, true, false}, "identity": {identity, false, true}, "login": {login, false, false}} {
		if c, i := Forms(tc.p, 7); c != tc.card || i != tc.identity {
			t.Errorf("%s: card %v, identity %v", name, c, i)
		}
	}
}
