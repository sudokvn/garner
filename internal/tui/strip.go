package tui

import (
	"strconv"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// The pipeline strip: six cells for the six steps of the Episode screen,
// from SABnzbd to Jellyfin, so a table row tells where a download stands
// or where it broke without opening the episode.

type cellState int

const (
	cellPending cellState = iota // not reached
	cellDone
	cellCurrent // running, or waiting for the user
	cellFailed
)

// stepCells reads the six steps pipeline lays out as cell states. A step's
// glyph says how far it got: ✓ done, ○ not reached, ✗ failed or
// cancelled; any other, such as ◐, ● or ▲, is where the download is now.
func stepCells(d api.Download, now time.Time) []cellState {
	steps := pipeline(d, now)
	cells := make([]cellState, len(steps))
	for i, st := range steps {
		switch ansi.Strip(st.glyph) {
		case "✓":
			cells[i] = cellDone
		case "○":
			cells[i] = cellPending
		case "✗":
			cells[i] = cellFailed
		default:
			cells[i] = cellCurrent
		}
	}
	return cells
}

// stripCells draws the six cells. In colour they are all squares: mint
// done, amber current, red failed, the border colour not reached. Without
// colour their shapes tell them apart: ■ done, ▸ current, ✗ failed, □ not
// reached.
func stripCells(d api.Download, now time.Time) []part {
	parts := make([]part, 0, 6)
	for _, c := range stepCells(d, now) {
		var p part
		switch c {
		case cellDone:
			p = part{"■", sMint}
		case cellCurrent:
			p = part{"■", sAmber}
			if colourless {
				p.text = "▸"
			}
		case cellFailed:
			p = part{"■", sRed}
			if colourless {
				p.text = "✗"
			}
		default:
			p = part{"■", sBorder}
			if colourless {
				p.text = "□"
			}
		}
		parts = append(parts, p)
	}
	return parts
}

// stripped reports whether an episode's state shows the strip: its latest
// download explains the state, as one that runs, waits for Jellyfin, or
// failed at a step.
func stripped(state api.EpisodeState, d *api.Download) bool {
	if d == nil || d.EpisodeState() != state {
		return false
	}
	switch state {
	case api.EpisodeDownloading, api.EpisodeProcessing, api.EpisodeImporting, api.EpisodeAttention, api.EpisodeFailed:
		return true
	}
	return false
}

// stripParts is an episode's state text as statusParts writes it, after
// the strip when stripped says so. The strip stands in for the progress
// bar. label, such as "S01E11", goes before the text of a download that
// runs; attempts names a running download that is not the episode's first.
func stripParts(label string, state api.EpisodeState, d *api.Download, summary string, nextRetry *time.Time, attempts int, now time.Time) []part {
	if !stripped(state, d) {
		return statusParts(state, d, summary, nextRetry, attempts, now)
	}
	parts := append(stripCells(*d, now), part{"  ", sMuted})
	running := state != api.EpisodeFailed && state != api.EpisodeAttention
	if running && label != "" {
		parts = append(parts, part{label + " ", sNormal})
	}
	switch state {
	case api.EpisodeDownloading:
		if d.Replaces != "" {
			parts = append(parts, part{"replaces " + clean(d.Replaces), sMuted}, part{" · ", sMuted})
		}
		return append(parts, downloadParts(d, attempts, false)...)
	case api.EpisodeProcessing, api.EpisodeImporting:
		parts = append(parts, statusParts(state, d, summary, nextRetry, 0, now)...)
		if attempts > 1 {
			parts = append(parts, part{" · attempt " + strconv.Itoa(attempts), sMuted})
		}
		return parts
	}
	return append(parts, statusParts(state, d, summary, nextRetry, attempts, now)...)
}
