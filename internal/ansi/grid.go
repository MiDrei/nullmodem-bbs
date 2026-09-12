package ansi

import (
	"strconv"
	"strings"
)

// Cell is one character position in a Grid: a raw CP437 byte plus the
// resolved (reverse-video-applied) foreground/background pair ToHTML
// would render it with. FG ranges 0-15 (the bright variants are
// 8-15); BG is normally 0-7, but can briefly hold a 0-15 value after
// parsing a reverse-video run with a bright foreground -- Encode
// folds that back down to the base 8 on write (see Encode's doc
// comment).
type Cell struct {
	Char byte `json:"char"`
	FG   int  `json:"fg"`
	BG   int  `json:"bg"`
}

func blankCell() Cell { return Cell{Char: ' ', FG: 7, BG: 0} }

// Grid is a 2D screen buffer for the web ANSI designer: Width*Height
// cells, row-major. It's the designer's save/load wire format (JSON)
// as well as the in-memory form ParseGrid/Encode convert to and from
// a raw .ans file's CP437/ANSI byte stream.
type Grid struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Cells  []Cell `json:"cells"`
}

func (g *Grid) index(row, col int) int { return row*g.Width + col }

// NewGrid returns a blank Width x Height grid (space, default colors).
func NewGrid(width, height int) Grid {
	g := Grid{Width: width, Height: height, Cells: make([]Cell, width*height)}
	for i := range g.Cells {
		g.Cells[i] = blankCell()
	}
	return g
}

// growTo ensures the grid has at least row+1 rows, blank-filling any
// newly added ones. Used while parsing, since a screen file's real
// height isn't known up front.
func (g *Grid) growTo(row int) {
	for g.Height <= row {
		g.Cells = append(g.Cells, make([]Cell, g.Width)...)
		for c := 0; c < g.Width; c++ {
			g.Cells[g.Height*g.Width+c] = blankCell()
		}
		g.Height++
	}
}

// ParseGrid parses a raw CP437/ANSI screen (as returned by LoadScreen)
// into a Grid of the given width, growing rows as content demands it.
// It's a small terminal emulator covering what classic BBS art and
// this package's own Encode actually use: SGR (colors/attributes),
// absolute/relative cursor motion (CUP/CUU/CUD/CUF/CUB), cursor
// save/restore, and ED2 (clear screen). Anything else -- erase-line,
// scrolling regions, 256-color/truecolor SGR, ... -- is a silent
// no-op rather than an error, so an unsupported sequence degrades
// gracefully instead of aborting the import.
func ParseGrid(raw string, width int) Grid {
	if width <= 0 {
		width = 80
	}
	g := Grid{Width: width}
	g.growTo(0)

	state := defaultHTMLState()
	row, col := 0, 0
	savedRow, savedCol := 0, 0
	// pendingWrap defers advancing to the next row until a character
	// actually needs it, matching a real terminal's autowrap: writing
	// the last column of a row shouldn't itself conjure a trailing
	// blank row when nothing more follows.
	pendingWrap := false

	put := func(ch byte) {
		if pendingWrap {
			col, row = 0, row+1
			g.growTo(row)
			pendingWrap = false
		}
		g.growTo(row)
		fg, bg := state.colors()
		g.Cells[g.index(row, col)] = Cell{Char: ch, FG: fg, BG: bg}
		if col == width-1 {
			pendingWrap = true
		} else {
			col++
		}
	}
	newline := func() {
		col, row = 0, row+1
		g.growTo(row)
		pendingWrap = false
	}
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}

	i, n := 0, len(raw)
	for i < n {
		c := raw[i]
		switch {
		case c == 0x1b && i+1 < n && raw[i+1] == '[':
			j := i + 2
			for j < n && !isCSIFinal(raw[j]) {
				j++
			}
			if j >= n {
				i = n
				continue
			}
			params := raw[i+2 : j]
			switch raw[j] {
			case 'm':
				applySGR(&state, params)
			case 'H', 'f':
				r, c := parseCursorParams(params)
				row, col = r-1, c-1
				if row < 0 {
					row = 0
				}
				col = clamp(col, 0, width-1)
				g.growTo(row)
				pendingWrap = false
			case 'A':
				row = clamp(row-parseSingleParam(params, 1), 0, row)
				pendingWrap = false
			case 'B':
				row += parseSingleParam(params, 1)
				g.growTo(row)
				pendingWrap = false
			case 'C':
				col = clamp(col+parseSingleParam(params, 1), 0, width-1)
				pendingWrap = false
			case 'D':
				col = clamp(col-parseSingleParam(params, 1), 0, width-1)
				pendingWrap = false
			case 'J':
				if params == "" || params == "2" {
					for idx := range g.Cells {
						g.Cells[idx] = blankCell()
					}
					row, col = 0, 0
					pendingWrap = false
				}
			case 's':
				savedRow, savedCol = row, col
			case 'u':
				row, col = savedRow, savedCol
				pendingWrap = false
			}
			i = j + 1
		case c == '\r' && i+1 < n && raw[i+1] == '\n':
			newline()
			i += 2
		case c == '\n':
			newline()
			i++
		case c == '\r':
			col = 0
			pendingWrap = false
			i++
		default:
			put(c)
			i++
		}
	}
	return g
}

func parseCursorParams(params string) (row, col int) {
	row, col = 1, 1
	if params == "" {
		return row, col
	}
	parts := strings.SplitN(params, ";", 2)
	if v, err := strconv.Atoi(parts[0]); err == nil && v > 0 {
		row = v
	}
	if len(parts) > 1 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			col = v
		}
	}
	return row, col
}

func parseSingleParam(params string, def int) int {
	if params == "" {
		return def
	}
	if v, err := strconv.Atoi(params); err == nil && v > 0 {
		return v
	}
	return def
}

// Encode serializes g back to a raw CP437/ANSI byte stream: a leading
// clear+home, one SGR run per color change (bg folded to its base 0-7
// slot -- classic SGR has no portable bright-background code, so a
// cell whose BG came from a reverse-video-with-bright-foreground
// import loses that extra bit here, a minor, known fidelity trade-off
// for a designer that always paints BG from an 8-color palette
// itself), and CRLF between rows.
func (g Grid) Encode() string {
	var b strings.Builder
	b.WriteString("\x1b[2J\x1b[H")
	prevFG, prevBG := -1, -1
	for row := 0; row < g.Height; row++ {
		for col := 0; col < g.Width; col++ {
			cell := g.Cells[g.index(row, col)]
			if cell.FG != prevFG || cell.BG != prevBG {
				b.WriteString(sgrFor(cell.FG, cell.BG))
				prevFG, prevBG = cell.FG, cell.BG
			}
			b.WriteByte(cell.Char)
		}
		if row < g.Height-1 {
			b.WriteString(CRLF)
		}
	}
	b.WriteString(Reset)
	return b.String()
}

func sgrFor(fg, bg int) string {
	base := fg % 8
	intensity := "0"
	if fg >= 8 {
		intensity = "1"
	}
	return esc + intensity + ";" + strconv.Itoa(30+base) + ";" + strconv.Itoa(40+bg%8) + "m"
}
