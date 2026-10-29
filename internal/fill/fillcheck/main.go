// Command fillcheck checks filling in a browser, Dia: it serves login
// forms that are hard to fill, fills each as Lense does, and reports what
// each form took. It types into the browser, which needs Accessibility:
// macOS gives it to an app of its own, which keeps it across builds when
// signed with a certificate.
//
//	app="build/Lense Fill Check.app" plist="build/Lense Fill Check.app/Contents/Info.plist"
//	go build -o "$app/Contents/MacOS/fillcheck" ./internal/fill/fillcheck
//	plutil -create xml1 "$plist"
//	for kv in CFBundleInfoDictionaryVersion=6.0 CFBundlePackageType=APPL CFBundleIdentifier=io.cbp.lense.fillcheck \
//		"CFBundleName=Lense Fill Check" CFBundleExecutable=fillcheck CFBundleShortVersionString=1.0 CFBundleVersion=1; do
//		plutil -insert "${kv%%=*}" -string "${kv#*=}" "$plist"
//	done
//	plutil -insert LSUIElement -bool true "$plist"
//	codesign -f -s "Developer ID Application" "$app"
//	open -W "$app" --args -out build/fillcheck.txt; cat build/fillcheck.txt
//
// System Settings lists it under Accessibility as it asks, and only as an
// app: the package type and versions, outside a temporary folder. It
// waits for the user to allow it, then opens a tab in Dia for each form:
// leave the keyboard alone while it types.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"lense/internal/fill"
	"lense/internal/macos"
)

const dia = "company.thebrowser.dia"

var login = fill.Request{Username: "ada@example.com", Password: "S3cret-pässwörd!", TOTP: "135790"}

// cases are the forms: the page reports what the fields marked data-user,
// data-pass, data-pass2, data-totp and data-trap hold.
var cases = []struct {
	name, body string
	// fills are the fields the login fills, as "user totp": "user pass"
	// unless it says, none where it's refused.
	fills string
	// open fills as Open and Fill does, opening the page first.
	open bool
	// elsewhere is a page of no website of the login's.
	elsewhere bool
	// refused is why nothing is to be typed: a fill of no field, a page of
	// another site or a frame.
	refused string
}{
	{name: "plain", body: `<form><input id=login_field name=login data-user autofocus><input id=password name=password type=password data-pass><button>Sign in</button></form>`},
	{name: "link", body: `<form><input name=user data-user autofocus><a href="#">Forgot your username?</a><input type=password data-pass></form>`},
	{name: "nofocus", body: `<header><input name=q placeholder=Search></header><form><input name=email placeholder="Email address" data-user><input type=password data-pass></form>`},
	{name: "redraw", body: `<form id=f><input name=user data-user autofocus><input type=password data-pass></form>
<script>
// As a page's framework takes over its form: the first key goes to the
// form it replaces.
let drawn = false
document.addEventListener('input', () => {
	if (drawn) return
	drawn = true
	setTimeout(() => { document.getElementById('f').innerHTML = '<input name=user data-user><input type=password data-pass>' }, 30)
}, true)
</script>`},
	{name: "slow", body: `<form><input name=user data-user autofocus><input type=password data-pass></form>
<script>
document.addEventListener('keydown', () => { const end = performance.now() + 20; while (performance.now() < end) {} }, true)
</script>`},
	// A password field that shows once the username is typed: as
	// Bitwarden's, the fill fills what shows, the next fill the rest.
	{name: "late", body: `<form><input name=user placeholder="Email or phone" data-user autofocus><button type=button>Next</button><input type=password data-pass hidden></form>
<script>
document.querySelector('[data-user]').addEventListener('input', () => setTimeout(() => { document.querySelector('[data-pass]').hidden = false }, 300))
</script>`, fills: "user"},
	{name: "twostep", body: `<form><input id=username name=username data-user autofocus><button>Next</button></form>`, fills: "user"},
	{name: "totp", body: `<form><input id=login_field data-user autofocus><input id=password type=password data-pass><input id=otp placeholder="Authentication code" data-totp></form>`, fills: "user pass totp"},
	{name: "codepage", body: `<form><input id=app_totp placeholder="6-digit code" data-totp autofocus></form>`, fills: "totp"},
	{name: "signup", body: `<form id=signup><input type=email placeholder=Email data-user><input type=password data-pass><input type=password placeholder="Confirm password" data-pass2></form>`, fills: "user pass pass2"},
	// A field to trap a robot, out of sight.
	{name: "honeypot", body: `<form><input id=username style="position: absolute; left: -9999px" data-trap><input type=email placeholder=Email data-user><input type=password data-pass></form>`},
	{name: "open", body: `<form><input id=login_field name=login data-user autofocus><input id=password name=password type=password data-pass></form>`, open: true},
	// A website's home page, without a login form: its search field takes
	// nothing.
	{name: "nologin", body: `<header><input name=q data-user autofocus></header><nav><a href="#">Sign in</a></nav>`, refused: "field"},
	{name: "wrongsite", body: `<form><input id=login_field name=login data-user autofocus><input id=password name=password type=password data-pass></form>`, elsewhere: true, refused: "page"},
	// A login form in a frame, as another site's on the page.
	{name: "iframe", body: `<p>Sign in below.</p><iframe src="/frame/iframe" style="width: 480px; height: 160px"></iframe>`, refused: "frame"},
}

// probeForm has fields of every kind a fill may meet, for -probe to list
// what Dia tells of each.
const probeForm = `<form>
<input id=u1 name=username autocomplete=username placeholder=Username data-user>
<input name=email type=email autocomplete=email>
<label for=pw>Password</label><input id=pw name=pw type=password autocomplete=current-password>
<input name=newpw type=password autocomplete=new-password aria-label="New password">
<input name=otp autocomplete=one-time-code inputmode=numeric maxlength=6 placeholder="6-digit code">
<input name=phone type=tel autocomplete=tel>
<input name=zip type=number>
<input name=q type=search>
<label for=cc>Card number</label><input id=cc name=cardnumber autocomplete=cc-number class="field card-number" data-testid=cc>
<select name=exp-month autocomplete=cc-exp-month aria-label="Expiration month"><option>MM</option><option>01</option><option>02</option></select>
<input name=cvc autocomplete=cc-csc aria-label="Security code" class="field cvv" maxlength=4>
<input name=dis disabled value=x><input name=ro readonly value=y>
<textarea name=address autocomplete=street-address></textarea>
</form>
<div><input id=outside placeholder="Outside any form"></div>
<div style="position: relative"><input id=covered placeholder=Covered><div style="position: absolute; inset: 0; background: white"></div></div>
<input id=clear placeholder=Clear style="opacity: 0">
<input id=away placeholder=Away style="position: absolute; left: -9999px">
<div style="position: relative"><label for=floating style="position: absolute; left: 8px; top: 4px">Email</label><input id=floating style="height: 32px"></div>
<input id=tiny placeholder=Tiny style="width: 4px; height: 4px">
<form id=react-form><input id=react-user placeholder="React username"></form>
<p id=react-state>react state: ""</p>
<script>
// Which events the fields get, in turn, to the check.
let n = 0
for (const t of ['focus', 'click', 'keydown', 'keyup', 'input', 'change', 'blur'])
	document.addEventListener(t, e => fetch('/event', {method: 'POST', body: (++n) + ':' + e.target.id + ':' + t}), true)
// As React keeps the value it last saw, by the instance's own setter, and
// takes a change only where the field's value differs from it.
const ru = document.getElementById('react-user')
let seen = ''
const proto = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')
Object.defineProperty(ru, 'value', {get() { return proto.get.call(this) }, set(v) { seen = v; proto.set.call(this, v) }})
ru.addEventListener('input', () => {
	if (ru.value !== seen) { seen = ru.value; document.getElementById('react-state').textContent = 'react state: ' + JSON.stringify(seen); fetch('/event', {method: 'POST', body: 'react took ' + seen}) }
})
</script>`

// frame is the login form of the frame of the iframe case.
const frame = `<form><input id=login_field name=login data-user autofocus><input id=password name=password type=password data-pass></form>`

// page wraps a form in a page that reports to the check as it loads, and
// as its fields change.
const page = `<!doctype html><meta charset=utf-8><title>%[1]s</title>
<body style="font: 16px system-ui; padding: 40px">
<h1>%[1]s</h1>
%[2]s
<script>
// Numbered, as reports may arrive out of turn.
let seq = 0
const report = () => {
	const v = s => (document.querySelector(s) || {}).value || ''
	fetch('/report/%[1]s', {method: 'POST', body: JSON.stringify({seq: ++seq, user: v('[data-user]'), pass: v('[data-pass]'), pass2: v('[data-pass2]'), totp: v('[data-totp]'), trap: v('[data-trap]')})})
}
document.addEventListener('input', report, true)
for (const t of ['focus', 'input', 'change', 'blur'])
	document.addEventListener(t, e => fetch('/event', {method: 'POST', body: t + ':' + (e.target.placeholder || e.target.type || e.target.id)}), true)
addEventListener('load', () => setTimeout(() => fetch('/ready/%[1]s', {method: 'POST'}), 100))
</script>`

type state struct {
	User, Pass, Pass2, TOTP, Trap string
}

// report is what a page reports its fields hold, in turn.
type report struct {
	Seq int
	state
}

func main() {
	out := flag.String("out", "", "where to write the results")
	only := flag.String("only", "", "the form to fill alone, as slow")
	probe := flag.Bool("probe", false, "list what Dia tells of the fields of a form of every kind, and fill nothing")
	probePage := flag.String("page", "probe", "the form -probe lists, as signup")
	flag.Parse()
	w := io.Writer(os.Stdout)
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		w = io.MultiWriter(os.Stdout, f)
	}
	if !macos.Trusted(true) {
		// macOS lists the app as it asks, and only while it runs: wait for
		// the user to allow it, then for them to leave System Settings.
		deadline := time.Now().Add(5 * time.Minute)
		for !macos.Trusted(false) {
			if time.Now().After(deadline) {
				fmt.Fprintln(w, "needs Accessibility: allow Lense Fill Check in System Settings, and run it again")
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
		time.Sleep(5 * time.Second)
	}

	var mu sync.Mutex
	reports, ready := map[string]report{}, map[string]bool{}
	var events []string
	mux := http.NewServeMux()
	for _, c := range cases {
		mux.HandleFunc("GET /"+c.name, func(rw http.ResponseWriter, r *http.Request) {
			rw.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(rw, page, c.name, c.body)
		})
	}
	mux.HandleFunc("GET /probe", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(rw, page, "probe", probeForm)
	})
	mux.HandleFunc("GET /frame/{case}", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(rw, page, r.PathValue("case"), frame)
	})
	mux.HandleFunc("POST /report/{case}", func(rw http.ResponseWriter, r *http.Request) {
		var rep report
		json.NewDecoder(r.Body).Decode(&rep)
		mu.Lock()
		if c := r.PathValue("case"); rep.Seq > reports[c].Seq {
			reports[c] = rep
		}
		mu.Unlock()
	})
	mux.HandleFunc("POST /event", func(rw http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		events = append(events, string(b))
		mu.Unlock()
	})
	mux.HandleFunc("POST /ready/{case}", func(rw http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ready[r.PathValue("case")] = true
		mu.Unlock()
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	go http.Serve(ln, mux)
	base := "http://" + ln.Addr().String() + "/"
	login.Matches = func(u string) bool { return strings.HasPrefix(u, base) }

	openInDia := func(u string) { exec.Command("/usr/bin/open", "-b", dia, u).Run() }
	kb := fill.Mac{Open: openInDia}
	if *probe {
		openInDia(base + *probePage)
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			mu.Lock()
			r := ready[*probePage]
			mu.Unlock()
			if r && macos.FrontmostApp().BundleID == dia {
				break
			}
		}
		pid := macos.FrontmostApp().PID
		macos.EnableWebAccessibility(pid)
		time.Sleep(time.Second)
		controls := macos.Fields(pid, nil)
		for i, c := range controls {
			in := c.Info()
			fmt.Fprintf(w, "%2d %-8s id=%q label=%q placeholder=%q aria=%q help=%q classes=%q value=%q enabled=%v settable=%v viewable=%v form=%x %q\n",
				i, in.Type, in.ID, in.Label, in.Placeholder, in.AriaLabel, in.Help, in.Classes, in.Value, in.Enabled, in.Settable, in.Viewable, in.Form, in.FormHint)
		}
		// Set the first field's value as accessibility does, and the
		// React-like one's, and see what the page made of it.
		for i, c := range controls {
			if id := c.Info().ID; id == "u1" || id == "react-user" || *probePage != "probe" {
				c.Focus()
				time.Sleep(200 * time.Millisecond)
				v := fmt.Sprintf("value-%d", i)
				ok := c.SetValue(v)
				time.Sleep(300 * time.Millisecond)
				n, _ := c.Length()
				fmt.Fprintf(w, "set %d %q to %q: %v, holds %q, %d characters\n", i, id, v, ok, c.Value(), n)
			}
		}
		time.Sleep(700 * time.Millisecond)
		mu.Lock()
		fmt.Fprintf(w, "events: %s\n", strings.Join(events, " "))
		mu.Unlock()
		return
	}
	failed := 0
	for _, c := range cases {
		if *only != "" && c.name != *only {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		start := time.Now()
		mu.Lock()
		events = nil
		mu.Unlock()
		req := login
		if c.elsewhere {
			req.Matches = func(string) bool { return false }
		}
		var err error
		if c.open {
			err = fill.Fill(ctx, kb, macos.App{}, withPage(req, base+c.name))
		} else {
			openInDia(base + c.name)
			for ctx.Err() == nil {
				mu.Lock()
				r := ready[c.name]
				mu.Unlock()
				if r && macos.FrontmostApp().BundleID == dia {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			err = fill.Fill(ctx, kb, macos.FrontmostApp(), req)
		}
		took := time.Since(start)
		cancel()
		time.Sleep(700 * time.Millisecond) // for the page's last report
		mu.Lock()
		got := reports[c.name].state
		mu.Unlock()
		var want state
		fills := c.fills
		if fills == "" && c.refused == "" {
			fills = "user pass"
		}
		for _, f := range strings.Fields(fills) {
			switch f {
			case "user":
				want.User = login.Username
			case "pass":
				want.Pass = login.Password
			case "pass2":
				want.Pass2 = login.Password
			case "totp":
				want.TOTP = login.TOTP
			}
		}
		verdict := "ok"
		if got != want || !refusedAs(err, c.refused) {
			verdict = "FAIL"
			failed++
		}
		fmt.Fprintf(w, "%-9s %-4s %5.2fs  user %q, password %d of %d characters", c.name, verdict, took.Seconds(), got.User, len([]rune(got.Pass)), len([]rune(want.Pass)))
		if want.Pass2 != "" || got.Pass2 != "" {
			fmt.Fprintf(w, ", confirmed %d", len([]rune(got.Pass2)))
		}
		if want.TOTP != "" || got.TOTP != "" {
			fmt.Fprintf(w, ", code %q", got.TOTP)
		}
		if got.Trap != "" {
			fmt.Fprintf(w, ", TRAP %q", got.Trap)
		}
		if verdict == "FAIL" {
			mu.Lock()
			fmt.Fprintf(w, "\n          events: %s", strings.Join(events, " "))
			mu.Unlock()
		}
		if err != nil {
			fmt.Fprintf(w, ", %v", err)
		}
		if got.Pass != want.Pass {
			fmt.Fprintf(w, ", password %q", got.Pass) // the check's own, not a secret
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%d of %d failed\n", failed, len(cases))
}

// refusedAs reports whether a fill ended as the case asks: with no error,
// or refusing as it says.
func refusedAs(err error, why string) bool {
	var pe *fill.PageError
	switch why {
	case "field":
		return errors.Is(err, fill.ErrNoField)
	case "page":
		return errors.As(err, &pe) && pe.URL != ""
	case "frame":
		return errors.Is(err, fill.ErrInFrame)
	}
	return err == nil
}

func withPage(r fill.Request, page string) fill.Request {
	r.Page = page
	return r
}
