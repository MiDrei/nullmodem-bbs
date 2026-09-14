package ansi

import (
	"strings"
	"testing"
)

func TestNewGridIsBlank(t *testing.T) {
	g := NewGrid(4, 2)
	if g.Width != 4 || g.Height != 2 || len(g.Cells) != 8 {
		t.Fatalf("NewGrid(4,2) = %+v, want 4x2 with 8 cells", g)
	}
	for _, c := range g.Cells {
		if c != blankCell() {
			t.Fatalf("cell = %+v, want blank", c)
		}
	}
}

func TestParseGridPlacesCharsAndColors(t *testing.T) {
	g := ParseGrid("\x1b[1;36mAB\x1b[0mC", 3)
	if g.Width != 3 || g.Height != 1 {
		t.Fatalf("grid = %+v, want 3x1", g)
	}
	want := []Cell{
		{Char: 'A', FG: 14, BG: 0},
		{Char: 'B', FG: 14, BG: 0},
		{Char: 'C', FG: 7, BG: 0},
	}
	for i, w := range want {
		if g.Cells[i] != w {
			t.Fatalf("cell %d = %+v, want %+v", i, g.Cells[i], w)
		}
	}
}

func TestParseGridWrapsAtWidthAndGrowsRows(t *testing.T) {
	g := ParseGrid("ABCDE", 2)
	if g.Height != 3 {
		t.Fatalf("Height = %d, want 3 (5 chars wrapped at width 2)", g.Height)
	}
	got := string([]byte{g.Cells[0].Char, g.Cells[1].Char, g.Cells[2].Char, g.Cells[3].Char, g.Cells[4].Char})
	if got != "ABCDE" {
		t.Fatalf("wrapped chars = %q, want %q", got, "ABCDE")
	}
}

func TestParseGridHandlesCRLFAsNewRow(t *testing.T) {
	g := ParseGrid("AB\r\nCD", 4)
	if g.Height != 2 {
		t.Fatalf("Height = %d, want 2", g.Height)
	}
	if g.Cells[g.index(0, 0)].Char != 'A' || g.Cells[g.index(1, 0)].Char != 'C' {
		t.Fatalf("row placement wrong: %+v", g.Cells)
	}
}

func TestParseGridHonorsCursorPositioning(t *testing.T) {
	// Move to row 3, col 5 (1-based) and place an X there without
	// having written anything on the rows in between.
	g := ParseGrid("\x1b[3;5HX", 10)
	if g.Height < 3 {
		t.Fatalf("Height = %d, want at least 3", g.Height)
	}
	if g.Cells[g.index(2, 4)].Char != 'X' {
		t.Fatalf("expected X at row 2, col 4 (0-based); got %+v", g.Cells[g.index(2, 4)])
	}
}

func TestParseGridReverseVideoResolvesActualColors(t *testing.T) {
	g := ParseGrid("\x1b[33;44;7mX", 1)
	cell := g.Cells[0]
	if cell.FG != 4 || cell.BG != 3 {
		t.Fatalf("reverse-video cell = %+v, want fg=4 (blue) bg=3 (yellow) after swap", cell)
	}
}

func TestGridEncodeRoundTripsThroughParseGrid(t *testing.T) {
	g := NewGrid(3, 2)
	g.Cells[g.index(0, 0)] = Cell{Char: 'H', FG: 14, BG: 1}
	g.Cells[g.index(0, 1)] = Cell{Char: 'i', FG: 14, BG: 1}
	g.Cells[g.index(1, 2)] = Cell{Char: '!', FG: 7, BG: 0}

	encoded := g.Encode()
	got := ParseGrid(encoded, 3)

	if got.Width != g.Width || got.Height != g.Height {
		t.Fatalf("round-tripped size = %dx%d, want %dx%d", got.Width, got.Height, g.Width, g.Height)
	}
	for i := range g.Cells {
		if got.Cells[i] != g.Cells[i] {
			t.Fatalf("cell %d = %+v, want %+v (encoded: %q)", i, got.Cells[i], g.Cells[i], encoded)
		}
	}
}

func TestGridEncodeTrimsTrailingBlankCells(t *testing.T) {
	// A screen relying on {FILL:x} tokens (see Layout) measures a
	// line's own visible width to compute padding at render time --
	// literal trailing spaces baked in by a naive encoder would eat
	// into that budget and corrupt the layout. Encode must trim them.
	g := NewGrid(10, 1)
	g.Cells[g.index(0, 0)] = Cell{Char: 'X', FG: 7, BG: 0}
	// Columns 1-9 stay the default blank cell.

	encoded := g.Encode()
	if strings.Contains(encoded, "X ") {
		t.Fatalf("encoded output still has trailing space(s) after content: %q", encoded)
	}
	if !strings.HasSuffix(encoded, "X"+Reset) {
		t.Fatalf("encoded output = %q, want it to end right after the last non-blank cell", encoded)
	}
}

func TestGridEncodeLeavesFullyBlankRowEmpty(t *testing.T) {
	g := NewGrid(5, 2)
	g.Cells[g.index(1, 0)] = Cell{Char: 'Y', FG: 7, BG: 0}

	encoded := g.Encode()
	got := ParseGrid(encoded, 5)
	if got.Cells[got.index(0, 0)] != blankCell() {
		t.Fatalf("row 0 should round-trip as entirely blank, got %+v", got.Cells[0])
	}
	if got.Cells[got.index(1, 0)].Char != 'Y' {
		t.Fatalf("row 1 should still have its content, got %+v", got.Cells[got.index(1, 0)])
	}
}

func TestGridEncodeFoldsBrightBackgroundToBaseEight(t *testing.T) {
	g := NewGrid(1, 1)
	g.Cells[0] = Cell{Char: 'X', FG: 7, BG: 12} // bright blue bg, out of classic SGR's bg range
	encoded := g.Encode()
	got := ParseGrid(encoded, 1)
	if got.Cells[0].BG != 4 {
		t.Fatalf("BG after encode round trip = %d, want 4 (12%%8)", got.Cells[0].BG)
	}
}

func TestGridEncodeRowsOmitsLeadingClearAndSlicesRowRange(t *testing.T) {
	g := NewGrid(3, 3)
	g.Cells[g.index(0, 0)] = Cell{Char: 'A', FG: 7, BG: 0}
	g.Cells[g.index(1, 0)] = Cell{Char: 'B', FG: 7, BG: 0}
	g.Cells[g.index(2, 0)] = Cell{Char: 'C', FG: 7, BG: 0}

	got := g.EncodeRows(1, 2)
	if strings.Contains(got, "\x1b[2J") {
		t.Fatalf("EncodeRows() = %q, want no leading clear+home (unlike Encode)", got)
	}
	if !strings.Contains(got, "B") {
		t.Fatalf("EncodeRows(1, 2) = %q, want it to contain row 1's content", got)
	}
	if strings.Contains(got, "A") || strings.Contains(got, "C") {
		t.Fatalf("EncodeRows(1, 2) = %q, want only row 1, not rows 0 or 2", got)
	}
}

func TestGridEncodeRowsClampsOutOfRangeWindow(t *testing.T) {
	g := NewGrid(2, 2)
	g.Cells[g.index(0, 0)] = Cell{Char: 'X', FG: 7, BG: 0}
	// Requesting well past the grid's actual height must not panic --
	// just yield whatever rows actually exist.
	got := g.EncodeRows(0, 100)
	if !strings.Contains(got, "X") {
		t.Fatalf("EncodeRows(0, 100) = %q, want it to still contain row 0's content", got)
	}
}
