// Package search finds items of a vault for what the user types: words,
// and filters that start with "=", as 1Password's Quick Access takes:
//
//	=untagged          items in no folder and no collection
//	=favorite          favorites
//	=tag:<name>        items in a folder or collection of that name
//	=category:<kind>   logins, cards, notes, identities or SSH keys
//	=vault:<name>      items of an organization, or of "My Vault"
//	=passkey           logins with a passkey
//	=location:<host>   logins for a website
//
// Values with spaces are quoted: =tag:"Side projects".
package search

import (
	"net"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"lense/internal/bw"
)

// Kind is the kind of a filter.
type Kind int

const (
	Untagged Kind = iota + 1
	Favorite
	Tag
	Category
	Vault
	Passkey
	Location
)

// Filter is a filter of a query.
type Filter struct {
	Kind  Kind
	Value string // in lower case, for those that take one
}

// Query is what the user typed.
type Query struct {
	Terms   []string // words, in lower case
	Filters []Filter
	// Bad are the filters not understood, as typed.
	Bad []string
}

// Empty reports whether q finds every item.
func (q Query) Empty() bool { return len(q.Terms) == 0 && len(q.Filters) == 0 }

// keywords are the names of filters, and what else they are typed as.
var keywords = []struct {
	kind  Kind
	names []string
	value bool
}{
	{Untagged, []string{"untagged"}, false},
	{Favorite, []string{"favorite", "favorites", "favourite", "favourites", "fav"}, false},
	{Tag, []string{"tag", "folder", "collection"}, true},
	{Category, []string{"category", "type"}, true},
	{Vault, []string{"vault"}, true},
	{Passkey, []string{"passkey", "passkeys"}, false},
	{Location, []string{"location", "url", "website"}, true},
}

// Parse reads a query.
func Parse(s string) Query {
	var q Query
	for _, tok := range tokens(s) {
		if !strings.HasPrefix(tok, "=") || tok == "=" {
			if t := strings.ToLower(strings.TrimPrefix(tok, "=")); t != "" {
				q.Terms = append(q.Terms, t)
			}
			continue
		}
		name, value, hasValue := strings.Cut(tok[1:], ":")
		name = strings.ToLower(name)
		ok := false
		for _, k := range keywords {
			for _, n := range k.names {
				if n != name {
					continue
				}
				if k.value && !hasValue {
					break
				}
				if k.value && strings.TrimSpace(value) == "" {
					ok = true // being typed: finds all until it has a value
					break
				}
				f := Filter{Kind: k.kind}
				if k.value {
					f.Value = strings.ToLower(strings.TrimSpace(value))
				}
				q.Filters = append(q.Filters, f)
				ok = true
			}
		}
		if !ok {
			q.Bad = append(q.Bad, tok)
		}
	}
	return q
}

// tokens splits s at spaces outside quotes, removing the quotes.
func tokens(s string) []string {
	var out []string
	var b strings.Builder
	quoted, any := false, false
	flush := func() {
		if any {
			out = append(out, b.String())
		}
		b.Reset()
		any = false
	}
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
			any = true
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			b.WriteRune(r)
			any = true
		}
	}
	flush()
	return out
}

// Index is what a search looks items up with.
type Index struct {
	v       *bw.Vault
	tags    map[string][]string // item ID: its folder's and collections' names, in lower case
	allTags []string
	orgs    map[string]string // organization ID: name in lower case
}

// NewIndex prepares the vault for searches.
func NewIndex(v *bw.Vault) *Index {
	ix := &Index{v: v, tags: map[string][]string{}, orgs: map[string]string{}}
	folders := map[string]string{}
	for _, f := range v.Folders {
		folders[f.ID] = strings.ToLower(f.Name)
		ix.allTags = append(ix.allTags, strings.ToLower(f.Name))
	}
	cols := map[string]string{}
	for _, c := range v.Collections {
		cols[c.ID] = strings.ToLower(c.Name)
		ix.allTags = append(ix.allTags, strings.ToLower(c.Name))
	}
	for _, o := range v.Organizations {
		ix.orgs[o.ID] = strings.ToLower(o.Name)
	}
	for _, it := range v.Items {
		var t []string
		if n, ok := folders[it.FolderID]; ok {
			t = append(t, n)
		}
		for _, c := range it.CollectionIDs {
			if n, ok := cols[c]; ok {
				t = append(t, n)
			}
		}
		ix.tags[it.ID] = t
	}
	return ix
}

// Vault returns the vault of the index.
func (ix *Index) Vault() *bw.Vault { return ix.v }

// Run returns the items of the vault q finds, the best first; boost
// raises an item, as one used recently.
func (ix *Index) Run(q Query, boost func(*bw.Item) int) []*bw.Item {
	type hit struct {
		it    *bw.Item
		score int
	}
	exact := ix.exactNames(q)
	var hits []hit
	for _, it := range ix.v.Items {
		if it.Archived {
			continue
		}
		score, ok := ix.match(it, q, exact)
		if !ok {
			continue
		}
		if it.Favorite {
			score += 3
		}
		if boost != nil {
			score += boost(it)
		}
		hits = append(hits, hit{it, score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return strings.ToLower(hits[i].it.Name) < strings.ToLower(hits[j].it.Name)
	})
	out := make([]*bw.Item, len(hits))
	for i, h := range hits {
		out[i] = h.it
	}
	return out
}

// exactNames reports, for the tag and vault filters of q, whether a folder,
// collection or organization has exactly that name: then the filter takes
// that one alone, else those whose names start with it, as one being
// typed.
func (ix *Index) exactNames(q Query) map[Filter]bool {
	out := map[Filter]bool{}
	for _, f := range q.Filters {
		switch f.Kind {
		case Tag:
			for _, t := range ix.allTags {
				if t == f.Value {
					out[f] = true
				}
			}
		case Vault:
			for _, n := range ix.orgs {
				if n == f.Value {
					out[f] = true
				}
			}
		}
	}
	return out
}

// Categories, and the words that name them.
var categories = []struct {
	t     bw.ItemType
	names []string
}{
	{bw.TypeLogin, []string{"login", "logins", "password", "passwords"}},
	{bw.TypeCard, []string{"card", "cards", "credit card", "creditcard", "payment"}},
	{bw.TypeSecureNote, []string{"note", "notes", "secure note", "securenote"}},
	{bw.TypeIdentity, []string{"identity", "identities"}},
	{bw.TypeSSHKey, []string{"ssh", "ssh key", "sshkey", "key", "keys"}},
}

// personal is how the personal vault is named in =vault:.
var personal = []string{"my vault", "personal", "private", "me"}

func (ix *Index) match(it *bw.Item, q Query, exact map[Filter]bool) (int, bool) {
	for _, f := range q.Filters {
		if !ix.filter(it, f, exact[f]) {
			return 0, false
		}
	}
	score := 1
	for _, term := range q.Terms {
		s := ix.term(it, term)
		if s == 0 {
			return 0, false
		}
		score += s
	}
	return score, true
}

func (ix *Index) filter(it *bw.Item, f Filter, exact bool) bool {
	named := func(name string) bool {
		if exact {
			return name == f.Value || strings.HasPrefix(name, f.Value+"/")
		}
		return strings.HasPrefix(name, f.Value)
	}
	switch f.Kind {
	case Untagged:
		return len(ix.tags[it.ID]) == 0
	case Favorite:
		return it.Favorite
	case Passkey:
		return it.Login != nil && len(it.Login.Passkeys) > 0
	case Tag:
		for _, t := range ix.tags[it.ID] {
			if named(t) {
				return true
			}
		}
		return false
	case Category:
		for _, c := range categories {
			for _, n := range c.names {
				if strings.HasPrefix(n, f.Value) && it.Type == c.t {
					return true
				}
			}
		}
		return false
	case Vault:
		if it.OrganizationID == "" {
			for _, p := range personal {
				if strings.HasPrefix(p, f.Value) {
					return true
				}
			}
			return false
		}
		return named(ix.orgs[it.OrganizationID])
	case Location:
		if it.Login == nil {
			return false
		}
		for _, u := range it.Login.URIs {
			if h := Host(u.URI); h != "" && (strings.Contains(h, f.Value) || strings.Contains(strings.ToLower(u.URI), f.Value)) {
				return true
			}
		}
		return false
	}
	return false
}

// term scores how well an item matches a word: by its name, best at its
// start, then by its username, websites and the rest.
func (ix *Index) term(it *bw.Item, t string) int {
	best := 0
	try := func(s string, start, word, within int) {
		s = strings.ToLower(s)
		switch {
		case s == "":
		case strings.HasPrefix(s, t):
			best = max(best, start)
		case wordPrefix(s, t):
			best = max(best, word)
		case strings.Contains(s, t):
			best = max(best, within)
		}
	}
	try(it.Name, 100, 70, 40)
	if l := it.Login; l != nil {
		try(l.Username, 50, 35, 25)
		for _, u := range l.URIs {
			try(Host(u.URI), 45, 35, 20)
		}
		for _, p := range l.Passkeys {
			try(p.RpID, 30, 20, 10)
		}
	}
	if c := it.Card; c != nil {
		try(c.Brand, 40, 30, 15)
		try(c.CardholderName, 30, 20, 10)
		if c.Last4 != "" && t == c.Last4 {
			best = max(best, 40)
		}
	}
	if id := it.Identity; id != nil {
		try(id.FullName(), 40, 30, 15)
		try(id.Email, 30, 20, 10)
		try(id.Company, 20, 15, 10)
	}
	for _, tag := range ix.tags[it.ID] {
		try(tag, 15, 10, 5)
	}
	return best
}

// wordPrefix reports whether a word of s starts with t.
func wordPrefix(s, t string) bool {
	for i, r := range s {
		if i > 0 && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if strings.HasPrefix(s[i+len(string(r)):], t) {
				return true
			}
		}
	}
	return false
}

// Host returns the host of a login's URI, without "www.": "github.com" for
// https://www.github.com/login, and "192.168.1.1" for 192.168.1.1. Apps'
// URIs, as androidapp://com.example, give their identifier.
func Host(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if !strings.Contains(uri, "://") {
		uri = "https://" + uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	return strings.TrimPrefix(h, "www.")
}

// BaseDomain returns the registrable domain of a host, roughly: its last
// two labels, or three under the country codes that sell domains below a
// second level (co.uk, com.au).
func BaseDomain(host string) string {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return host
	}
	labels := strings.Split(host, ".")
	n := 2
	if len(labels) >= 3 {
		second := labels[len(labels)-2]
		if len(labels[len(labels)-1]) == 2 && (len(second) <= 3 || second == "gov" || second == "ac") {
			switch second {
			case "co", "com", "net", "org", "gov", "ac", "edu", "ne", "or", "go":
				n = 3
			}
		}
	}
	if len(labels) < n {
		return host
	}
	return strings.Join(labels[len(labels)-n:], ".")
}
