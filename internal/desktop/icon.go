//go:build !headless
// +build !headless

package desktop

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"fyne.io/fyne/v2"
)

//go:embed shfs.png
var iconPNG []byte

// ResourceShfsIcon returns the static app icon.
func ResourceShfsIcon() fyne.Resource {
	return fyne.NewStaticResource("shfs-icon", iconPNG)
}

// BandwidthTrayIcon generates a 32x32 tray icon showing live bandwidth.
// Pink bar = outgoing (left), yellow bar = incoming (right), on black background.
func BandwidthTrayIcon(outBytes, inBytes int64, maxBytes int64) fyne.Resource {
	sz := 32
	img := image.NewRGBA(image.Rect(0, 0, sz, sz))

	// Black background
	bg := color.RGBA{0, 0, 0, 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{bg}, image.Point{}, draw.Src)

	if maxBytes == 0 {
		maxBytes = 1024
	}

	// Pink bar (outgoing) — left half
	outH := int(float64(outBytes) / float64(maxBytes) * float64(sz))
	if outH > sz { outH = sz }
	if outH > 0 {
		pink := color.RGBA{0xE0, 0x90, 0xA0, 0xFF}
		for y := sz - outH; y < sz; y++ {
			for x := 2; x < 14; x++ { img.Set(x, y, pink) }
		}
	}

	// Yellow bar (incoming) — right half
	inH := int(float64(inBytes) / float64(maxBytes) * float64(sz))
	if inH > sz { inH = sz }
	if inH > 0 {
		yellow := color.RGBA{0xE0, 0xD0, 0x90, 0xFF}
		for y := sz - inH; y < sz; y++ {
			for x := 18; x < 30; x++ { img.Set(x, y, yellow) }
		}
	}

	// "S" and "H" labels at top
	label := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	for y := 2; y < 7; y++ { img.Set(6, y, label); img.Set(15, y, label); img.Set(24, y, label) }
	for x := 7; x < 15; x++ { img.Set(x, 3, label) } // S top bar
	for x := 7; x < 14; x++ { img.Set(x, 4, label) } // H middle bar

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return fyne.NewStaticResource("tray-bw", buf.Bytes())
}
