package tui

// markLines is garner's mark from docs/brand, drawn with half blocks: a
// pixel is half a cell, so the mark's 18 by 18 pixels take 9 rows. It is
// the logo's construction laid on the pixel grid (outer radius 9, inner
// radius 5, ring and block 4 wide), so the cuts, the block and the gaps
// beside the block fall on whole pixels. The pixels are square where a cell
// is twice as tall as it is wide; with other fonts the mark comes out a
// little taller or wider. It uses only ▀ ▄ █, which terminals draw without
// seams.
var markLines = [...]string{
	"    ▄▄██████",
	"  ▄█████████",
	" ████▀▀",
	"████▀       ▄▄▄▄",
	"████        ████",
	"████▄       ▀▀▀▀",
	" ████▄▄    ▄▄▄▄▄▄",
	"  ▀████████████▀",
	"    ▀▀██████▀▀",
}

// markWidth is the mark's width in cells.
const markWidth = 17

// markRows renders the mark in mint, each row as wide as the mark.
func markRows() []string {
	rows := make([]string, len(markLines))
	for i, l := range markLines {
		rows[i] = sMint.Render(pad(l, markWidth))
	}
	return rows
}
