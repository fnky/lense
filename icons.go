package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"

	"github.com/egoist/mygo/ui"
)

// Icons, in the manner of Lucide: strokes in the color of the text.
func icon(paths string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + paths + `</svg>`))
}

var (
	iconSearch    = icon(`<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>`)
	iconLock      = icon(`<rect width="18" height="11" x="3" y="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>`)
	iconLogin     = icon(`<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/>`)
	iconCard      = icon(`<rect width="20" height="14" x="2" y="5" rx="2"/><path d="M2 10h20"/>`)
	iconNote      = icon(`<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>`)
	iconIdentity  = icon(`<path d="M16 10h2"/><path d="M16 14h2"/><path d="M6.17 15a3 3 0 0 1 5.66 0"/><circle cx="9" cy="11" r="2"/><rect x="2" y="5" width="20" height="14" rx="2"/>`)
	iconSSH       = icon(`<path d="m4 17 6-6-6-6"/><path d="M12 19h8"/>`)
	iconStar      = icon(`<path d="M11.525 2.295a.53.53 0 0 1 .95 0l2.31 4.679a2.123 2.123 0 0 0 1.595 1.16l5.166.756a.53.53 0 0 1 .294.904l-3.736 3.638a2.123 2.123 0 0 0-.611 1.878l.882 5.14a.53.53 0 0 1-.771.56l-4.618-2.428a2.122 2.122 0 0 0-1.973 0L6.396 21.01a.53.53 0 0 1-.77-.56l.881-5.139a2.122 2.122 0 0 0-.611-1.879L2.16 9.795a.53.53 0 0 1 .294-.906l5.165-.755a2.122 2.122 0 0 0 1.597-1.16z"/>`)
	iconCopy      = icon(`<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>`)
	iconExternal  = icon(`<path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>`)
	iconEye       = icon(`<path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"/><circle cx="12" cy="12" r="3"/>`)
	iconEyeOff    = icon(`<path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"/><path d="M14.084 14.158a3 3 0 0 1-4.242-4.242"/><path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"/><path d="m2 2 20 20"/>`)
	iconChevron   = icon(`<path d="m6 9 6 6 6-6"/>`)
	iconPlusSmall = icon(`<path d="M5 12h14"/><path d="M12 5v14"/>`)
	iconSettings  = icon(`<path d="M20 7h-9"/><path d="M14 17H5"/><circle cx="17" cy="17" r="3"/><circle cx="7" cy="7" r="3"/>`)
	iconFolder    = icon(`<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>`)
	iconVault     = icon(`<path d="M12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/><path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65"/><path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65"/>`)
	iconOrg       = icon(`<path d="M6 22V4a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v18Z"/><path d="M6 12H4a2 2 0 0 0-2 2v6a2 2 0 0 0 2 2h2"/><path d="M18 9h2a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-2"/><path d="M10 6h4"/><path d="M10 10h4"/><path d="M10 14h4"/><path d="M10 18h4"/>`)
	iconPasskey   = icon(`<path d="M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z"/><circle cx="16.5" cy="7.5" r=".5" fill="currentColor"/>`)
	iconGlobe     = icon(`<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>`)
	iconFinger    = icon(`<path d="M12 10a2 2 0 0 0-2 2c0 1.02-.1 2.51-.26 4"/><path d="M14 13.12c0 2.38 0 6.38-1 8.88"/><path d="M17.29 21.02c.12-.6.43-2.3.5-3.02"/><path d="M2 12a10 10 0 0 1 18-6"/><path d="M2 16h.01"/><path d="M21.8 16c.2-2 .131-5.354 0-6"/><path d="M5 19.5C5.5 18 6 15 6 12a6 6 0 0 1 .34-2"/><path d="M8.65 22c.21-.66.45-1.32.57-2"/><path d="M9 6.8a6 6 0 0 1 9 5.2v2"/>`)
	iconAlert     = icon(`<circle cx="12" cy="12" r="10"/><path d="M12 8v4"/><path d="M12 16h.01"/>`)
	iconCheck     = icon(`<path d="M20 6 9 17l-5-5"/>`)
	iconClose     = icon(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
	iconBack      = icon(`<path d="m15 18-6-6 6-6"/>`)
	iconShield    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><path fill="currentColor" fill-rule="evenodd" d="M5 2.5h14a1 1 0 0 1 1 1V12c0 4.6-4.3 7.8-8 9.6-3.7-1.8-8-5-8-9.6V3.5a1 1 0 0 1 1-1Zm7 2.3v14.4c3-1.6 5.8-3.9 5.8-7.2V4.8Z"/></svg>`))
)

// itemIcon is the icon of an item of a kind.
func itemIcon(kind int) *ui.SVG {
	switch kind {
	case 2:
		return iconNote
	case 3:
		return iconCard
	case 4:
		return iconIdentity
	case 5:
		return iconSSH
	}
	return iconLogin
}

// shield reports how much of the pixel at (px, py) Bitwarden's shield
// covers, in a box of size s: the outline, and the left half within.
func shield(px, py, s float64, outline bool) bool {
	x, y := px/s, py/s // 0..1
	const top, mid, bottom = 0.08, 0.52, 0.95
	const left, right = 0.14, 0.86
	inside := func(x, y, inset float64) bool {
		l, r := left+inset, right-inset
		t, m, b := top+inset, mid, bottom-inset*1.4
		switch {
		case y < t || y > b:
			return false
		case y <= m:
			return x >= l && x <= r
		}
		// The bottom curves in to a point, as an ellipse's quarter.
		f := (y - m) / (b - m)
		hw := (r - l) / 2 * math.Sqrt(math.Max(0, 1-f*f))
		cx := (l + r) / 2
		return x >= cx-hw && x <= cx+hw
	}
	if !outline {
		return inside(x, y, 0)
	}
	if !inside(x, y, 0) {
		return false
	}
	// A ring, and the left half of what it holds.
	if !inside(x, y, 0.1) {
		return true
	}
	return x < 0.5 && inside(x, y, 0.16)
}

// render draws a mark into a size×size image at 4×4 samples per pixel,
// in c.
func render(size int, c color.NRGBA, hit func(px, py float64) bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			cover := 0
			for sy := range 4 {
				for sx := range 4 {
					if hit(float64(x)+(float64(sx)+0.5)/4, float64(y)+(float64(sy)+0.5)/4) {
						cover++
					}
				}
			}
			if cover > 0 {
				p := c
				p.A = uint8(int(c.A) * cover / 16)
				img.SetNRGBA(x, y, p)
			}
		}
	}
	return img
}

// trayIcon is the shield, 18 points high at twice the scale, black on
// transparent for macOS to tint.
func trayIcon(locked bool) []byte {
	const size = 36
	img := render(size, color.NRGBA{A: 255}, func(px, py float64) bool {
		return shield(px, py, size, true)
	})
	if locked {
		// A dot at the bottom right says the vault is locked.
		dot := render(size, color.NRGBA{A: 255}, func(px, py float64) bool {
			return math.Hypot(px-29, py-29) <= 5.5
		})
		hole := render(size, color.NRGBA{A: 255}, func(px, py float64) bool {
			return math.Hypot(px-29, py-29) <= 8
		})
		for i := 3; i < len(img.Pix); i += 4 {
			if hole.Pix[i] > 0 {
				img.Pix[i] = uint8(int(img.Pix[i]) * (255 - int(hole.Pix[i])) / 255)
			}
			if dot.Pix[i] > img.Pix[i] {
				img.Pix[i] = dot.Pix[i]
			}
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
