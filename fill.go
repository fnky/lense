package main

import (
	"context"
	"time"

	"bitwarden-mini/internal/macos"
	"bitwarden-mini/internal/search"
)

// fillInto types a login into the app the user was in: its username into
// the text field with the keyboard focus, then, past Tab, its password,
// or only its password into a password field. With a page, it opens the
// page first and waits for a browser to show it. It types nowhere but in
// a text field: errNoField when none has the focus.
func fillInto(ctx context.Context, target macos.App, req fillRequest, openURL func(string)) error {
	wait := 800 * time.Millisecond
	if req.page != "" {
		openURL(req.page)
		browser, err := waitForPage(ctx, search.BaseDomain(search.Host(req.page)))
		if err != nil {
			return err
		}
		target = browser
		wait = 6 * time.Second // as the page loads, and focuses its first field
	} else if target.PID != 0 {
		// The window hid: the app the user was in comes back.
		deadline := time.Now().Add(1500 * time.Millisecond)
		for macos.FrontmostApp().PID != target.PID && time.Now().Before(deadline) {
			sleep(ctx, 50*time.Millisecond)
		}
		if macos.FrontmostApp().PID != target.PID {
			macos.Activate(target.PID)
			sleep(ctx, 250*time.Millisecond)
		}
	}
	if macos.IsBrowser(target.BundleID) {
		macos.EnableWebAccessibility(target.PID)
	}
	kind := macos.FieldNone
	deadline := time.Now().Add(wait)
	for {
		if kind = macos.FocusedField(); kind != macos.FieldNone || !time.Now().Before(deadline) || ctx.Err() != nil {
			break
		}
		sleep(ctx, 150*time.Millisecond)
	}
	switch kind {
	case macos.FieldPassword:
		macos.SelectAll()
		macos.TypeText(req.password)
	case macos.FieldText:
		if req.username == "" {
			return errNoField
		}
		macos.SelectAll()
		macos.TypeText(req.username)
		macos.PressKey(macos.KeyTab)
		sleep(ctx, 150*time.Millisecond)
		// Pages that ask for the password next show no password field
		// yet: the username is enough there.
		if macos.FocusedField() == macos.FieldPassword {
			macos.SelectAll()
			macos.TypeText(req.password)
		}
	default:
		return errNoField
	}
	return nil
}

// waitForPage waits for a browser to come to the front showing a page of
// domain. A browser that does not tell its page gets a moment to load it.
func waitForPage(ctx context.Context, domain string) (macos.App, error) {
	for {
		if err := ctx.Err(); err != nil {
			return macos.App{}, errNoField
		}
		fa := macos.FrontmostApp()
		if macos.IsBrowser(fa.BundleID) {
			u, err := macos.BrowserURL(ctx, fa.BundleID)
			if err != nil || u == "" {
				sleep(ctx, 2*time.Second)
				return fa, nil
			}
			if search.BaseDomain(search.Host(u)) == domain {
				return fa, nil
			}
		}
		sleep(ctx, 250*time.Millisecond)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
