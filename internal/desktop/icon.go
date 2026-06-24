//go:build !headless
// +build !headless

package desktop

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed shfs.png
var iconPNG []byte

// ResourceShfsIcon returns the static app icon.
func ResourceShfsIcon() fyne.Resource {
	return fyne.NewStaticResource("shfs-icon", iconPNG)
}
