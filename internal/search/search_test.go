package search_test

import (
	"slices"
	"testing"

	"bitwarden-mini/internal/bw"
	"bitwarden-mini/internal/bw/bwtest"
	"bitwarden-mini/internal/search"
)

func sample(t *testing.T) *search.Index {
	t.Helper()
	f := bwtest.Sample()
	s, err := bw.Open(f.Write(t))
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Decrypt(f.UserID, f.UserKey)
	if err != nil {
		t.Fatal(err)
	}
	return search.NewIndex(v)
}

func names(items []*bw.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func TestParse(t *testing.T) {
	q := search.Parse(`git =favorite =tag:"Side projects" =Category:login =bogus =vault:`)
	if !slices.Equal(q.Terms, []string{"git"}) {
		t.Errorf("terms %q", q.Terms)
	}
	want := []search.Filter{{Kind: search.Favorite}, {Kind: search.Tag, Value: "side projects"}, {Kind: search.Category, Value: "login"}}
	if !slices.Equal(q.Filters, want) {
		t.Errorf("filters %+v", q.Filters)
	}
	if !slices.Equal(q.Bad, []string{"=bogus"}) {
		t.Errorf("bad %q", q.Bad)
	}
}

func TestRun(t *testing.T) {
	ix := sample(t)
	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"git", []string{"GitHub"}},
		{"=favorite", []string{"GitHub", "Visa"}},
		{"=untagged =category:login", []string{"Google", "Router"}},
		{"=tag:work", []string{"GitHub"}},
		{"=tag:eng", []string{"AWS Console", "Deploy key"}},
		{"=category:card", []string{"Visa"}},
		{"=category:ssh", []string{"Deploy key"}},
		{"=category:note", []string{"Wi-Fi"}},
		{"=vault:acme", []string{"AWS Console", "Deploy key"}},
		{`=vault:"Acme Inc."`, []string{"AWS Console", "Deploy key"}},
		{"=vault:my =category:login", []string{"GitHub", "Google", "Router"}},
		{"=passkey", []string{"Google"}},
		{"=location:github", []string{"GitHub"}},
		{"=location:192.168", []string{"Router"}},
		{"ada", []string{"Ada Lovelace", "GitHub", "AWS Console", "Google", "Visa"}},
		{"1111", []string{"Visa"}},
		{"console", []string{"AWS Console"}},
		{"nothing-like-this", nil},
	} {
		got := names(ix.Run(search.Parse(tc.q), nil))
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.q, got, tc.want)
		}
	}
}

func TestRunEmptyFindsAll(t *testing.T) {
	ix := sample(t)
	if n := len(ix.Run(search.Parse(""), nil)); n != len(ix.Vault().Items) {
		t.Fatalf("%d of %d", n, len(ix.Vault().Items))
	}
}

func TestComplete(t *testing.T) {
	ix := sample(t)
	labels := func(s string) []string {
		var out []string
		for _, c := range ix.Complete(s) {
			out = append(out, c.Label)
		}
		return out
	}
	if got := labels("git =ta"); !slices.Equal(got, []string{"=tag:"}) {
		t.Errorf("=ta: %q", got)
	}
	if got := labels("=tag:"); !slices.Equal(got, []string{"=tag:Personal", "=tag:Work", "=tag:Engineering"}) {
		t.Errorf("=tag: %q", got)
	}
	if got := labels("=vault:a"); !slices.Equal(got, []string{"=vault:Acme Inc."}) {
		t.Errorf("=vault:a %q", got)
	}
	if c := ix.Complete("x =vault:a"); len(c) != 1 || c[0].Query != `x =vault:"Acme Inc." ` {
		t.Errorf("query %+v", c)
	}
	if got := labels("git "); got != nil {
		t.Errorf("after a space: %q", got)
	}
}

func TestMatchesPage(t *testing.T) {
	m := func(n int) *int { return &n }
	for _, tc := range []struct {
		uri  bw.URI
		page string
		want bool
	}{
		{bw.URI{URI: "https://github.com/login"}, "https://gist.github.com/x", true},
		{bw.URI{URI: "https://github.com"}, "https://github.community", false},
		{bw.URI{URI: "https://bbc.co.uk"}, "https://www.bbc.co.uk/news", true},
		{bw.URI{URI: "https://bbc.co.uk"}, "https://itv.co.uk", false},
		{bw.URI{URI: "https://github.com", Match: m(search.MatchHost)}, "https://gist.github.com", false},
		{bw.URI{URI: "https://a.com/x", Match: m(search.MatchStartsWith)}, "https://a.com/xyz", true},
		{bw.URI{URI: "https://a.com/x", Match: m(search.MatchExact)}, "https://a.com/xyz", false},
		{bw.URI{URI: `^https://.*\.corp\.com`, Match: m(search.MatchRegex)}, "https://wiki.corp.com", true},
		{bw.URI{URI: "https://a.com", Match: m(search.MatchNever)}, "https://a.com", false},
		{bw.URI{URI: "192.168.1.1"}, "http://192.168.1.1/admin", true},
	} {
		if got := search.MatchesPage(tc.uri, tc.page); got != tc.want {
			t.Errorf("%v on %s: %v", tc.uri.URI, tc.page, got)
		}
	}
}

func TestForApp(t *testing.T) {
	ix := sample(t)
	if got := names(ix.ForApp("com.github.GitHubClient", "GitHub")); !slices.Equal(got, []string{"GitHub"}) {
		t.Errorf("GitHub app: %q", got)
	}
	if got := names(ix.ForPage("https://github.com/settings")); !slices.Equal(got, []string{"GitHub"}) {
		t.Errorf("github page: %q", got)
	}
}
