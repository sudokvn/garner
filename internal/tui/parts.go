package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/trucke/garner/internal/api"
)

// Text that many screens share: styled parts and fitting them to a width,
// what an episode is doing, and how sizes, times and counts are written.

// fit cuts plain text to w cells with an ellipsis, then pads it.
func fit(s string, w int, right bool) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	padding := strings.Repeat(" ", w-ansi.StringWidth(s))
	if right {
		return padding + s
	}
	return s + padding
}

type part struct {
	text  string
	style lipgloss.Style
}

// renderParts draws styled parts within w cells, cutting the last that
// does not fit.
func renderParts(parts []part, w int, bg func(lipgloss.Style) lipgloss.Style) string {
	var b strings.Builder
	for _, p := range parts {
		if w <= 0 {
			break
		}
		t := ansi.Truncate(p.text, w, "…")
		w -= ansi.StringWidth(t)
		b.WriteString(bg(p.style).Render(t))
	}
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// fact is a labelled fact, as in "Aired  8 Jul 2003".
func fact(label, value string) string { return sSubtle.Render(label) + "  " + sNormal.Render(value) }

// importSlowAfter is when an import that waits for Jellyfin shows since
// when it waits.
const importSlowAfter = 3 * time.Minute

// redrawMsg only makes the screen draw again.
type redrawMsg struct{}

// redrawWhenSlow draws the screen again when a waiting import passes
// importSlowAfter, so its start time appears without a key press.
func redrawWhenSlow(downloads ...*api.Download) tea.Cmd {
	var next time.Time
	for _, d := range downloads {
		if d == nil || d.Import.State != api.ImportScanning {
			continue
		}
		if at := d.Updated.Add(importSlowAfter); at.After(time.Now()) && (next.IsZero() || at.Before(next)) {
			next = at
		}
	}
	if next.IsZero() {
		return nil
	}
	return tea.Tick(time.Until(next), func(time.Time) tea.Msg { return redrawMsg{} })
}

// statusParts describes what an episode is doing, from its state, latest
// download and latest search. The sentences come from the server verbatim.
// A download that replaces a file in the library names the file's tier
// first, so a narrow State column keeps it; its percentage stands in for
// the progress bar.
func statusParts(state api.EpisodeState, d *api.Download, summary string, nextRetry *time.Time, attempts int, now time.Time) []part {
	if d == nil || d.Replaces == "" {
		return pipelineParts(state, d, summary, nextRetry, attempts, now)
	}
	replaces := []part{{"replaces " + clean(d.Replaces), sMuted}, {" · ", sMuted}}
	switch state {
	case api.EpisodeDownloading:
		return append(replaces, downloadParts(d, attempts, false)...)
	case api.EpisodeProcessing, api.EpisodeImporting:
		return append(replaces, pipelineParts(state, d, summary, nextRetry, attempts, now)...)
	case api.EpisodeFailed:
		// The library keeps its file; said before the failure, which is long.
		parts := pipelineParts(state, d, summary, nextRetry, attempts, now)
		return slices.Insert(parts, 1, part{" · keeps " + clean(d.Replaces), sMuted})
	}
	return pipelineParts(state, d, summary, nextRetry, attempts, now)
}

// pipelineParts is statusParts without the file a download replaces, as
// the Episode screen's steps show it.
func pipelineParts(state api.EpisodeState, d *api.Download, summary string, nextRetry *time.Time, attempts int, now time.Time) []part {
	sep := part{" · ", sMuted}
	switch state {
	case api.EpisodeFailed:
		if d == nil {
			return []part{{"failed", sRed}}
		}
		var parts []part
		switch d.Import.State {
		case api.ImportFailed:
			parts = []part{{"import failed", sRed}, sep, {clean(d.Import.Detail), sMuted}}
		case api.ImportRejected:
			parts = []part{{"rejected", sRed}, sep, {clean(d.Import.Detail), sMuted}}
		default:
			parts = []part{{"download failed", sRed}, sep, {clean(cmp.Or(d.FailureSummary, d.Failure)), sMuted}}
		}
		if nextRetry != nil {
			parts = append(parts, sep, part{nextSearch(*nextRetry, now), sMuted})
		}
		return parts
	case api.EpisodeAttention:
		detail := ""
		if d != nil {
			detail = clean(d.Import.Detail)
		}
		return []part{{"needs you", sAmber}, sep, {detail, sMuted}}
	case api.EpisodeDownloading:
		return downloadParts(d, attempts, true)
	case api.EpisodeProcessing:
		if d != nil && d.Detail != "" {
			return []part{{clean(d.Detail), sMuted}}
		}
		return []part{{"processing", sMuted}}
	case api.EpisodeImporting:
		if d == nil {
			return []part{{"importing", sMuted}}
		}
		switch d.Import.State {
		case api.ImportMoving:
			return []part{{"moving into the library", sMuted}}
		case api.ImportScanning:
			// Jellyfin usually confirms within 1 to 2 minutes; only a longer
			// wait gets its start time.
			if started := d.Updated; now.Sub(started) >= importSlowAfter {
				return []part{{"importing into the media library… · since " + when(started, now), sMuted}}
			}
			return []part{{"importing into the media library…", sMuted}}
		}
		return []part{{"checking the file", sMuted}}
	case api.EpisodeWaiting, api.EpisodeNotSearched:
		if d != nil && !d.Acknowledged.IsZero() {
			parts := []part{{"missing · last download failed", sMuted}}
			if nextRetry != nil {
				parts = append(parts, sep, part{nextSearch(*nextRetry, now), sMuted})
			}
			return parts
		}
		if d != nil && d.State == api.Cancelled {
			// The monitor leaves it to you until you download it.
			return []part{{"missing · download cancelled", sMuted}}
		}
		if state == api.EpisodeNotSearched {
			return []part{{"missing", sMuted}}
		}
		text := "missing"
		if summary != "" {
			text += " · " + lowerFirst(clean(summary))
		}
		if nextRetry != nil {
			text += " · " + nextSearch(*nextRetry, now)
		}
		return []part{{text, sMuted}}
	}
	return nil
}

// downloadParts shows a download's progress, with a bar or without.
func downloadParts(d *api.Download, attempts int, bar bool) []part {
	var parts []part
	switch {
	case d == nil || d.Progress <= 0:
		parts = []part{{"queued", sMuted}}
	case bar:
		const width = 16
		done := min(width, int(d.Progress/100*width+0.5))
		parts = []part{
			{strings.Repeat("█", done), sAmber},
			{strings.Repeat("█", width-done), sBorder},
			{fmt.Sprintf("  %3.0f%%", d.Progress), sNormal},
		}
	default:
		parts = []part{{fmt.Sprintf("%.0f%%", d.Progress), sNormal}}
	}
	if d != nil && d.Progress > 0 {
		if left := timeLeft(d.TimeLeft); left != "" {
			parts = append(parts, part{"   " + left + " left", sMuted})
		}
	}
	if attempts > 1 {
		parts = append(parts, part{" · attempt " + strconv.Itoa(attempts), sMuted})
	}
	return parts
}

// nextSearch says when the monitor searches again; "soon" once the time
// has come, while it works through the episodes before.
func nextSearch(t, now time.Time) string {
	if !t.After(now) {
		return "next search soon"
	}
	return "next search " + when(t, now)
}

// lowerFirst lets a server sentence continue a line: "Found 21 matching
// releases" becomes "found 21 matching releases".
func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToLower(r)) + s[n:]
}

func status(s string) string {
	switch s {
	case "Continuing":
		return "Airing"
	case "":
		return ""
	}
	return clean(s)
}

// size uses decimal units.
func size(n int64) string {
	switch {
	case n <= 0:
		return ""
	case n >= 100e9:
		return fmt.Sprintf("%.0f GB", float64(n)/1e9)
	case n >= 1e9:
		return fmt.Sprintf("%.1f GB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.0f MB", float64(n)/1e6)
	}
	return fmt.Sprintf("%.0f kB", float64(n)/1e3)
}

// timeLeft turns SABnzbd's "0:01:20" into "1m20s".
func timeLeft(s string) string {
	parts := strings.Split(s, ":")
	total := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return ""
		}
		total = total*60 + n
	}
	if len(parts) == 4 { // days:hours:minutes:seconds
		return ""
	}
	if total <= 0 {
		return ""
	}
	return elapsed(time.Duration(total) * time.Second)
}

// elapsed is a short duration: 40s, 1m20s, 2h5m.
func elapsed(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// day writes a date as "15 Oct", with the year when it is not this year.
func day(date string, now time.Time) string {
	t, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return clean(date)
	}
	if t.Year() != now.Year() {
		return t.Format("2 Jan 2006")
	}
	return t.Format("2 Jan")
}

// when writes a time as "13:30" today, otherwise with the day.
func when(t, now time.Time) string {
	t = t.Local()
	if t.Format(time.DateOnly) == now.Local().Format(time.DateOnly) {
		return t.Format("15:04")
	}
	return day(t.Format(time.DateOnly), now) + " " + t.Format("15:04")
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < 2*time.Minute:
		return "a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	}
	return "on " + t.Local().Format("2 Jan 2006")
}

// wrapAfter breaks s after each sep where the line is full, such as a file
// name after its dots.
func wrapAfter(s, sep string, width int) []string {
	width = max(10, width)
	var lines []string
	line := ""
	for _, part := range strings.SplitAfter(s, sep) {
		if line != "" && ansi.StringWidth(line+part) > width {
			lines = append(lines, line)
			line = ""
		}
		line += part
		for ansi.StringWidth(line) > width {
			lines = append(lines, ansi.Truncate(line, width, ""))
			line = ansi.TruncateLeft(line, width, "")
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
