package search

import (
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"

	"lense/internal/bw"
)

// Completion is a way to finish the filter being typed.
type Completion struct {
	Label  string // what the list shows, as =tag:Work
	Detail string // what it finds
	Query  string // the query with the filter finished
}

// Complete returns the ways to finish the filter at the end of s, as the
// user types it: the names of filters after "=", then their values.
func (ix *Index) Complete(s string) []Completion {
	if s == "" || strings.HasSuffix(s, " ") && strings.Count(s, `"`)%2 == 0 {
		return nil
	}
	start := lastTokenStart(s)
	tok := s[start:]
	if !strings.HasPrefix(tok, "=") {
		return nil
	}
	head := s[:start]
	name, value, hasValue := strings.Cut(tok[1:], ":")
	name = strings.ToLower(name)
	var out []Completion
	add := func(label, detail, insert string) {
		out = append(out, Completion{Label: label, Detail: detail, Query: head + insert})
	}
	if !hasValue {
		for _, k := range []struct{ name, detail string }{
			{"favorite", "Favorites"},
			{"tag:", "Items in a folder or collection"},
			{"folder:", "Items in a folder"},
			{"collection:", "Items in a collection"},
			{"category:", "Logins, cards, notes, identities or SSH keys"},
			{"vault:", "Items of a vault"},
			{"location:", "Logins for a website"},
			{"passkey", "Logins with a passkey"},
			{"untagged", "Items in no folder or collection"},
		} {
			if strings.HasPrefix(k.name, name) && "="+k.name != tok {
				insert := "=" + k.name
				if !strings.HasSuffix(insert, ":") {
					insert += " "
				}
				add("="+k.name, k.detail, insert)
			}
		}
		return out
	}
	value = strings.Trim(value, `"`)
	lv := strings.ToLower(value)
	values := func(kind, detail string, names []string) {
		seen := map[string]bool{}
		for _, n := range names {
			l := strings.ToLower(n)
			if seen[l] || !strings.HasPrefix(l, lv) || l == lv {
				continue
			}
			seen[l] = true
			v := n
			if strings.ContainsAny(v, " \t") {
				v = `"` + v + `"`
			}
			add("="+kind+":"+n, detail, "="+kind+":"+v+" ")
		}
	}
	switch name {
	case "tag", "folder", "collection":
		var folders, cols []string
		for _, f := range ix.v.Folders {
			folders = append(folders, f.Name)
		}
		for _, c := range ix.v.Collections {
			cols = append(cols, c.Name)
		}
		switch name {
		case "folder":
			values(name, "Folder", folders)
		case "collection":
			values(name, "Collection", cols)
		default:
			values(name, "Folder or collection", append(folders, cols...))
		}
	case "category", "type":
		values(name, "Category", []string{"Login", "Card", "Note", "Identity", "SSH"})
	case "vault":
		names := []string{"My Vault"}
		for _, o := range ix.v.Organizations {
			names = append(names, o.Name)
		}
		values(name, "Vault", names)
	case "location", "url", "website":
		count := map[string]int{}
		for _, it := range ix.v.Items {
			if it.Login == nil {
				continue
			}
			for _, u := range it.Login.URIs {
				if h := Host(u.URI); h != "" {
					count[h]++
				}
			}
		}
		hosts := make([]string, 0, len(count))
		for h := range count {
			hosts = append(hosts, h)
		}
		sort.Slice(hosts, func(i, j int) bool {
			if count[hosts[i]] != count[hosts[j]] {
				return count[hosts[i]] > count[hosts[j]]
			}
			return hosts[i] < hosts[j]
		})
		values(name, "Website", hosts)
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

// lastTokenStart returns where the last token of s starts, a quoted value
// included.
func lastTokenStart(s string) int {
	quoted := false
	start := 0
	for i, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case r == ' ' && !quoted:
			start = i + 1
		}
	}
	return start
}

// URI match types of Bitwarden.
const (
	MatchDomain     = 0
	MatchHost       = 1
	MatchStartsWith = 2
	MatchExact      = 3
	MatchRegex      = 4
	MatchNever      = 5
)

// MatchesPage reports whether a login's URI is for the page at pageURL,
// as Bitwarden matches them: by the URI's match detection, else by def,
// as MatchDomain, on the same base domain.
func MatchesPage(u bw.URI, pageURL string, def int) bool {
	if u.URI == "" || pageURL == "" {
		return false
	}
	match := def
	if u.Match != nil {
		match = *u.Match
	}
	switch match {
	case MatchHost:
		h := hostPort(u.URI)
		return h != "" && h == hostPort(pageURL)
	case MatchStartsWith:
		return strings.HasPrefix(pageURL, u.URI)
	case MatchExact:
		return pageURL == u.URI
	case MatchRegex:
		re, err := regexp.Compile("(?i)" + u.URI)
		return err == nil && re.MatchString(pageURL)
	case MatchNever:
		return false
	}
	h, p := hostPort(u.URI), hostPort(pageURL)
	if h == "" || p == "" {
		return false
	}
	domain := BaseDomain(hostname(h))
	if domain != BaseDomain(hostname(p)) {
		return false
	}
	// Hosts of a domain that serve other people's pages match no login
	// of the domain.
	return !slices.Contains(foreignHosts[domain], p)
}

// foreignHosts are hosts of a domain that serve pages anyone makes, by the
// domain, as Bitwarden's DomainMatchBlacklist has them: Apps Script's.
var foreignHosts = map[string][]string{
	"google.com": {"script.google.com"},
}

// hostPort returns the host of a URI as a URL's host has it, as Bitwarden
// compares hosts: in lower case and punycode, "www." kept, with its port
// unless the scheme's own; "" for none.
func hostPort(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	if !strings.Contains(uri, "://") {
		uri = "https://" + uri
	}
	u, err := url.Parse(uri)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	h := asciiHost(u.Hostname())
	if strings.Contains(h, ":") {
		h = "[" + h + "]" // an IPv6 address
	}
	switch p := u.Port(); {
	case p == "", p == "443" && u.Scheme == "https", p == "80" && u.Scheme == "http":
		return h
	default:
		return h + ":" + p
	}
}

// hostname returns the host of a hostPort, without its port.
func hostname(hostPort string) string {
	if strings.HasPrefix(hostPort, "[") {
		h, _, _ := strings.Cut(hostPort[1:], "]")
		return h
	}
	h, _, _ := strings.Cut(hostPort, ":")
	return h
}

// ForPage returns the logins for the page at pageURL, matching their
// URIs by def where they don't say how.
func (ix *Index) ForPage(pageURL string, def int) []*bw.Item {
	var out []*bw.Item
	for _, it := range ix.v.Items {
		if it.Login == nil || it.Archived {
			continue
		}
		for _, u := range it.Login.URIs {
			if MatchesPage(u, pageURL, def) {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

// ForApp returns the items for an app, by its bundle identifier or name:
// those with a URI naming the app, as macapp://com.tinyspeck.slackmacgap,
// then those named as the app, or for its website, as slack.com for Slack.
func (ix *Index) ForApp(bundleID, name string) []*bw.Item {
	bundleID, name = strings.ToLower(bundleID), strings.ToLower(strings.TrimSpace(name))
	if bundleID == "" && name == "" {
		return nil
	}
	var strong, weak []*bw.Item
	for _, it := range ix.v.Items {
		if it.Archived || it.Type != bw.TypeLogin && it.Type != bw.TypeSecureNote {
			continue
		}
		hit, near := false, false
		if it.Login != nil {
			for _, u := range it.Login.URIs {
				l := strings.ToLower(u.URI)
				if bundleID != "" && strings.Contains(l, bundleID) {
					hit = true
				}
				if h := Host(u.URI); name != "" && h != "" {
					label, _, _ := strings.Cut(BaseDomain(h), ".")
					if label == strings.ReplaceAll(name, " ", "") {
						near = true
					}
				}
			}
		}
		if name != "" && strings.ToLower(it.Name) == name {
			near = true
		}
		switch {
		case hit:
			strong = append(strong, it)
		case near:
			weak = append(weak, it)
		}
	}
	return append(strong, weak...)
}
