// Package ansiimg draws an ANSI screen (a parsed ansi.Grid) as a
// picture, in the VGA palette and the same CP437 8x16 font the web
// viewer uses -- for link previews (OpenGraph) of the board.
package ansiimg

import (
	"image"
	"image/color"
	"image/draw"

	xdraw "golang.org/x/image/draw"

	"git.maik.ch/nullmodem/kit/ansi"
)

// VGA is the 16-colour text-mode palette.
var VGA = [16]color.RGBA{
	{0, 0, 0, 255}, {170, 0, 0, 255}, {0, 170, 0, 255}, {170, 85, 0, 255},
	{0, 0, 170, 255}, {170, 0, 170, 255}, {0, 170, 170, 255}, {170, 170, 170, 255},
	{85, 85, 85, 255}, {255, 85, 85, 255}, {85, 255, 85, 255}, {255, 255, 85, 255},
	{85, 85, 255, 255}, {255, 85, 255, 255}, {85, 255, 255, 255}, {255, 255, 255, 255},
}

// Render draws g at its natural size (8x16 pixels a cell), without the
// blank rows at its end.
func Render(g ansi.Grid) *image.RGBA {
	rows := g.Height
	for rows > 0 && blankRow(g, rows-1) {
		rows--
	}
	img := image.NewRGBA(image.Rect(0, 0, g.Width*8, max(rows, 1)*16))
	draw.Draw(img, img.Bounds(), &image.Uniform{VGA[0]}, image.Point{}, draw.Src)
	for row := 0; row < rows; row++ {
		for col := 0; col < g.Width; col++ {
			c := g.Cells[row*g.Width+col]
			fg, bg := VGA[c.FG&15], VGA[c.BG&15]
			bits := font8x16[c.Char]
			for y := 0; y < 16; y++ {
				for x := 0; x < 8; x++ {
					px := bg
					if bits[y]>>(7-x)&1 == 1 {
						px = fg
					}
					img.SetRGBA(col*8+x, row*16+y, px)
				}
			}
		}
	}
	return img
}

func blankRow(g ansi.Grid, row int) bool {
	for col := 0; col < g.Width; col++ {
		c := g.Cells[row*g.Width+col]
		if (c.Char != ' ' && c.Char != 0) || c.BG&7 != 0 {
			return false
		}
	}
	return true
}

// Card fits the screen into a w x h picture on black (1200x630 for
// OpenGraph): drawn at twice its size first, then scaled to fit, so
// the pixels stay crisp.
func Card(g ansi.Grid, w, h int) *image.RGBA {
	src := Render(g)
	big := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx()*2, src.Bounds().Dy()*2))
	xdraw.NearestNeighbor.Scale(big, big.Bounds(), src, src.Bounds(), draw.Src, nil)

	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), &image.Uniform{VGA[0]}, image.Point{}, draw.Src)
	margin := h / 20
	sw, sh := big.Bounds().Dx(), big.Bounds().Dy()
	scale := min(float64(w-2*margin)/float64(sw), float64(h-2*margin)/float64(sh))
	dw, dh := int(float64(sw)*scale), int(float64(sh)*scale)
	dst := image.Rect((w-dw)/2, (h-dh)/2, (w-dw)/2+dw, (h-dh)/2+dh)
	xdraw.CatmullRom.Scale(out, dst, big, big.Bounds(), draw.Src, nil)
	return out
}
