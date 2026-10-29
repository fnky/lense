package fill

import (
	"context"
	"time"

	"lense/internal/macos"
)

// Mac is macOS's keyboard, which opens pages with Open.
type Mac struct {
	Open func(url string)
}

func (Mac) Frontmost() macos.App          { return macos.FrontmostApp() }
func (Mac) Activate(pid int) bool         { return macos.Activate(pid) }
func (m Mac) OpenURL(url string)          { m.Open(url) }
func (Mac) Prepare(pid int)               { macos.EnableWebAccessibility(pid) }
func (Mac) ModifiersHeld() bool           { return macos.ModifiersHeld() }
func (Mac) SelectAll(pid int)             { macos.SelectAll(pid) }
func (Mac) PressKey(pid int, code uint16) { macos.PressKey(pid, code) }

func (Mac) BrowserURL(ctx context.Context, bundleID string) (string, error) {
	return macos.BrowserURL(ctx, bundleID)
}

func (Mac) PageLoaded(pid int) (loaded, known bool) { return macos.PageLoaded(pid) }

func (Mac) TypeText(pid int, text string, pause time.Duration) {
	macos.TypeText(pid, text, pause)
}

func (Mac) Focused(pid int) Field {
	if e := macos.FocusedElement(pid); e != nil {
		return macField{e}
	}
	return nil
}

func (Mac) Fields(pid int, near Field) []Field {
	var e *macos.Element
	if mf, ok := near.(macField); ok {
		e = mf.e
	}
	var out []Field
	for _, f := range macos.Fields(pid, e) {
		out = append(out, macField{f})
	}
	return out
}

func (Mac) Menu(pid int, of Field) Menu {
	if mf, ok := of.(macField); ok {
		if m := macos.OpenMenu(pid, mf.e); m != nil {
			return m
		}
	}
	return nil
}

// macField is a field of another app, as accessibility shows it.
type macField struct{ e *macos.Element }

func (f macField) Info() macos.FieldInfo     { return f.e.Info() }
func (f macField) Value() string             { return f.e.Value() }
func (f macField) Length() (int, bool)       { return f.e.Length() }
func (f macField) Selected() (int, bool)     { return f.e.Selected() }
func (f macField) PageURL() string           { return f.e.PageURL() }
func (f macField) Focus() bool               { return f.e.Focus() }
func (f macField) SetValue(text string) bool { return f.e.SetValue(text) }

func (f macField) Same(o Field) bool {
	of, ok := o.(macField)
	return ok && f.e.Same(of.e)
}
