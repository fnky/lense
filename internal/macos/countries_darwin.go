//go:build darwin

package macos

import (
	"strings"
	"sync"

	"github.com/ebitengine/purego/objc"
)

// countries are the codes of two letters of the countries ISO names, by
// their English names in lower case, and those names by code, as macOS
// knows them.
var countries = sync.OnceValues(func() (map[string]string, map[string]string) {
	byName, byCode := map[string]string{}, map[string]string{}
	if load() != nil {
		return byName, byCode
	}
	withPool(func() {
		en := class("NSLocale").Send(sel("localeWithLocaleIdentifier:"), nsString("en_US"))
		codes := class("NSLocale").Send(sel("ISOCountryCodes"))
		for i := uint(0); i < objc.Send[uint](codes, sel("count")); i++ {
			code := codes.Send(sel("objectAtIndex:"), i)
			name := goString(en.Send(sel("localizedStringForCountryCode:"), code))
			if name != "" {
				byName[strings.ToLower(name)] = goString(code)
				byCode[goString(code)] = name
			}
		}
	})
	return byName, byCode
})

// CountryCode returns the ISO code of two letters of the country named so
// in English, as "US" of United States, or "".
func CountryCode(name string) string {
	byName, _ := countries()
	return byName[strings.ToLower(strings.TrimSpace(name))]
}

// CountryName returns the English name of the country of an ISO code of
// two letters, as "United States" of US, or "".
func CountryName(code string) string {
	_, byCode := countries()
	return byCode[strings.ToUpper(strings.TrimSpace(code))]
}
