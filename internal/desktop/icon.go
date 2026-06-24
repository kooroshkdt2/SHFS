//go:build !headless
// +build !headless

package desktop

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"bytes"

	"fyne.io/fyne/v2"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// ResourceShfsIcon returns the app icon as a Fyne resource.
func ResourceShfsIcon() fyne.Resource {
	return resourceShfsIcon()
}

func resourceShfsIcon() fyne.Resource {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))

	// Blue rounded-rect background
	bgColor := color.RGBA{0x33, 0x66, 0xCC, 0xFF}
	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)

	// White "S" letter centered
	white := color.RGBA{0xFF, 0xFF, 0xFF, 0xFF}
	point := fixed.Point26_6{
		X: fixed.Int26_6(18 * 64),
		Y: fixed.Int26_6(46 * 64),
	}
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{white},
		Face: basicfont.Face7x13,
		Dot:  point,
	}
	d.DrawString("S")

	var buf bytes.Buffer
	png.Encode(&buf, img)

	return fyne.NewStaticResource("shfs-icon", buf.Bytes())
}
