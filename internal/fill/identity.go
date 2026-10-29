package fill

import (
	"strings"

	"lense/internal/macos"
)

// Identity is an identity to fill into a page.
type Identity struct {
	Title, First, Middle, Last       string
	Address1, Address2, Address3     string
	City, State, PostalCode, Country string
	Company, Email, Phone, Username  string
}

// identityParts are what an identity fills, in the order a field is tried
// for them, by the words Bitwarden's extension knows them by: within all
// of a field's names together, but for its full name and its address,
// matched name by name.
var identityParts = []struct {
	part  string
	words words
	// alone matches each of a field's names alone.
	alone bool
}{
	{part: "title", words: withinAll("honorific-prefix", "prefix", "title", "anrede")},
	{part: "name", words: words{all: []string{"name"}, within: []string{"full-name", "your-name"}}, alone: true},
	{part: "first", words: withinAll("f-name", "first-name", "given-name", "first-n", "vorname")},
	{part: "middle", words: withinAll("m-name", "middle-name", "additional-name", "middle-initial", "middle-n", "middle-i")},
	{part: "last", words: withinAll("l-name", "last-name", "s-name", "surname", "family-name", "family-n", "last-n", "nachname", "familienname")},
	{part: "email", words: withinAll("e-mail", "email-address")},
	{part: "address1", words: withinAll("address-1", "address-line-1", "addr-1", "street-1")},
	{part: "address2", words: withinAll("address-2", "address-line-2", "addr-2", "street-2", "address-ext")},
	{part: "address3", words: withinAll("address-3", "address-line-3", "addr-3", "street-3")},
	{part: "address", alone: true, words: words{
		all:    []string{"address", "street-address", "addr", "street", "mailing-addr", "billing-addr"},
		within: []string{"mail-addr", "bill-addr", "strasse", "adresse"},
	}},
	{part: "postal", words: withinAll("postal", "zip", "zip2", "zip-code", "postal-code", "post-code", "postcode", "address-zip",
		"address-postal", "address-code", "address-postal-code", "address-zip-code", "plz", "postleitzahl")},
	{part: "city", words: withinAll("city", "town", "address-level-2", "address-city", "address-town", "ort", "stadt", "wohnort")},
	{part: "state", words: withinAll("state", "province", "provence", "address-level-1", "address-state", "address-province", "bundesland")},
	{part: "country", words: withinAll("country", "country-code", "country-name", "address-country", "address-country-name",
		"address-country-code", "land")},
	{part: "phone", words: withinAll("phone", "mobile", "mobile-phone", "tel", "telephone", "phone-number", "telefon",
		"telefonnummer", "mobil", "handy")},
	{part: "username", words: withinAll("user-name", "user-id", "screen-name")},
	{part: "company", words: withinAll("company", "company-name", "organization", "organization-name", "firma")},
}

// identityField is a field of an identity's: what it takes, and the field.
type identityField struct {
	part  string
	field field
}

// identityNames returns what names a field for an identity, as Bitwarden's
// extension looks at them: its identifier, label, placeholder and type.
func identityNames(fl field) []string {
	return []string{fl.info.ID, fl.info.Label, fl.info.AriaLabel, fl.info.Placeholder, fl.info.Type}
}

// planIdentity plans an identity's fill of a page's fields, as Bitwarden's
// extension does: each field, but a password's, takes the first part of
// an identity its names match; several fields may take one part.
func planIdentity(found []Field) []identityField {
	var out []identityField
	for _, fl := range fieldsOf(found) {
		if !fillable(fl) || fl.typeIs("password") {
			continue
		}
		var all strings.Builder
		for _, n := range identityNames(fl) {
			all.WriteString(squash(n))
		}
	part:
		for _, p := range identityParts {
			if p.alone {
				for _, n := range identityNames(fl) {
					if n = squash(n); n != "" && p.words.match(n) {
						out = append(out, identityField{p.part, fl})
						break part
					}
				}
				continue
			}
			for _, w := range p.words.within {
				if strings.Contains(all.String(), squash(w)) {
					out = append(out, identityField{p.part, fl})
					break part
				}
			}
		}
	}
	return out
}

// identityValue returns what an identity puts into a field of a part: its
// full name of its names, its address of its lines, its state's and its
// country's codes; "" for none.
func identityValue(id Identity, part string) string {
	switch part {
	case "title":
		return id.Title
	case "name":
		return join(" ", id.First, id.Middle, id.Last)
	case "first":
		return id.First
	case "middle":
		return id.Middle
	case "last":
		return id.Last
	case "email":
		return id.Email
	case "address1":
		return id.Address1
	case "address2":
		return id.Address2
	case "address3":
		return id.Address3
	case "address":
		if id.Address1 == "" {
			return ""
		}
		return join(", ", id.Address1, id.Address2, id.Address3)
	case "postal":
		return id.PostalCode
	case "city":
		return id.City
	case "state":
		return stateCode(id.State)
	case "country":
		return countryCode(id.Country)
	case "phone":
		return id.Phone
	case "username":
		return id.Username
	case "company":
		return id.Company
	}
	return ""
}

func join(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// stateCode returns a state's code of two letters, as a field takes it: its
// own of one so short, else that of a state of the US or a province of
// Canada so named; "" for none.
func stateCode(state string) string {
	state = strings.TrimSpace(state)
	if len(state) <= 2 {
		return state
	}
	return states[strings.ToLower(state)]
}

// countryCode returns a country's code of two letters, ISO's: its own of
// one so short, else that of the country so named; "" for none.
var countryCode = func(country string) string {
	country = strings.TrimSpace(country)
	if len(country) <= 2 {
		return strings.ToUpper(country)
	}
	return macos.CountryCode(country)
}

// countryName returns the English name of a country of an ISO code.
var countryName = macos.CountryName

// identityPick returns which item of a menu, by their titles, names an
// identity's value: a state's titled as its name, as "Massachusetts", or
// its code, a country's as its name or its code.
func identityPick(id Identity, part, value string) func(items []string) int {
	var choices []string
	switch part {
	case "state":
		choices = []string{strings.TrimSpace(id.State), stateNames[strings.ToUpper(value)], value}
	case "country":
		choices = []string{strings.TrimSpace(id.Country), countryName(value), value}
	default:
		choices = []string{value}
	}
	var out []string
	for _, c := range choices {
		if c != "" && !containsFold(out, c) {
			out = append(out, c)
		}
	}
	return firstOf(func(t string) bool { return containsFold(out, t) })
}

func containsFold(list []string, s string) bool {
	for _, l := range list {
		if strings.EqualFold(l, s) {
			return true
		}
	}
	return false
}

// states are the codes of the states of the US, its territories, and the
// provinces of Canada, by their names; stateNames their names by code.
var states, stateNames = func() (map[string]string, map[string]string) {
	byName, byCode := map[string]string{}, map[string]string{}
	for _, s := range strings.Split(`AL Alabama|AK Alaska|AS American Samoa|AZ Arizona|AR Arkansas|CA California|CO Colorado|CT Connecticut|DE Delaware|DC District Of Columbia|FM Federated States Of Micronesia|FL Florida|GA Georgia|GU Guam|HI Hawaii|ID Idaho|IL Illinois|IN Indiana|IA Iowa|KS Kansas|KY Kentucky|LA Louisiana|ME Maine|MH Marshall Islands|MD Maryland|MA Massachusetts|MI Michigan|MN Minnesota|MS Mississippi|MO Missouri|MT Montana|NE Nebraska|NV Nevada|NH New Hampshire|NJ New Jersey|NM New Mexico|NY New York|NC North Carolina|ND North Dakota|MP Northern Mariana Islands|OH Ohio|OK Oklahoma|OR Oregon|PW Palau|PA Pennsylvania|PR Puerto Rico|RI Rhode Island|SC South Carolina|SD South Dakota|TN Tennessee|TX Texas|UT Utah|VT Vermont|VI Virgin Islands|VA Virginia|WA Washington|WV West Virginia|WI Wisconsin|WY Wyoming|AB Alberta|BC British Columbia|MB Manitoba|NB New Brunswick|NL Newfoundland And Labrador|NS Nova Scotia|NT Northwest Territories|NU Nunavut|ON Ontario|PE Prince Edward Island|QC Quebec|SK Saskatchewan|YT Yukon`, "|") {
		code, name, _ := strings.Cut(s, " ")
		byName[strings.ToLower(name)], byCode[code] = code, name
	}
	return byName, byCode
}()
