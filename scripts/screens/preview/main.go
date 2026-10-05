// Command preview draws a screen the way the board shows it on an
// 80-column terminal -- its texts in a language, the sysop's lines for
// a security level, placeholders filled in -- as a PNG, for looking at
// a screen before it goes live.
//
//	go run ./scripts/screens/preview [-lang de-du] [-sl 255] [-scale 2] screen.ans out.png
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"

	"golang.org/x/image/draw"

	"git.maik.ch/nullmodem/bbs/internal/ansiimg"
	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/menu"
	"git.maik.ch/nullmodem/kit/ansi"
)

// width is what the board lays screens out to on an 80-column
// terminal (internal/bbs's Terminal.Width).
const width = 79

func main() {
	lang := flag.String("lang", "de-du", "language of the {T:key} texts")
	sl := flag.Int("sl", 255, "security level of the caller")
	scale := flag.Int("scale", 2, "pixels per font pixel")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: preview [-lang de-du] [-sl 255] [-scale 2] screen.ans out.png")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), flag.Arg(1), *lang, *sl, *scale); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(in, out, lang string, sl, scale int) error {
	raw, err := ansi.LoadScreen(in)
	if err != nil {
		return err
	}
	vars := ansi.Vars{"BBSNAME": "Maiks Place BBS", "SYSOP": "Mike Dreier", "USERNAME": "SwissMaik",
		"NODE": "1", "SL": fmt.Sprint(sl), "VERSION": "NullModem BBS",
		"SYSOP_ITEM": menu.SysopItem(sl, string(ansi.EncodeCP437(i18n.T(lang, "menu.sysop_item"))))}
	text := ansi.Layout(ansi.Render(menu.SysopLines(i18n.FillScreen(lang, raw), sl), vars), width)
	src := ansiimg.Render(ansi.ParseGrid(text, width+1))
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*scale, b.Dy()*scale))
	draw.NearestNeighbor.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := png.Encode(f, dst); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
