package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sudokvn/garner/internal/api"
)

// The Library's Upcoming tab: the episodes of every show that aired in
// the last two days and are missing, or air soon. Each day from two days
// ago to a week ahead has a row, empty or not; the rest comes under
// Later. garner knows air dates only, no time of day.

// upRow is one line of the tab: a day with one of its episodes, a day
// without any, the Later heading or a blank line.
type upRow struct {
	day   string // the day's label; empty on its second and later episodes
	style lipgloss.Style
	ep    *api.UpcomingEpisode
}

// upcomingToday is the day the tab counts from: the server's, or this
// machine's when the server's cannot be read.
func upcomingToday(u *api.Upcoming) time.Time {
	today, err := time.Parse(time.DateOnly, u.Today)
	if err != nil {
		now := time.Now()
		today = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	}
	return today
}

// upcomingRows lays the episodes out by day, from the server's today.
func upcomingRows(u *api.Upcoming) []upRow {
	today := upcomingToday(u)
	first, last := today.AddDate(0, 0, -2), today.AddDate(0, 0, 6)
	byDay := map[string][]*api.UpcomingEpisode{}
	var later []*api.UpcomingEpisode
	for i := range u.Episodes {
		e := &u.Episodes[i]
		t, err := time.Parse(time.DateOnly, e.Aired)
		switch {
		case err != nil || t.After(last):
			later = append(later, e)
			continue
		case t.Before(first):
			first = t // older than the server lists; the week starts earlier
		}
		byDay[e.Aired] = append(byDay[e.Aired], e)
	}

	var rows []upRow
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		label, style := d.Format("Mon 2"), sNormal
		switch {
		case d.Equal(today):
			label, style = label+" · today", sBold
		case d.Before(today):
			style = sMuted
		}
		eps := byDay[d.Format(time.DateOnly)]
		if len(eps) == 0 {
			if !d.Equal(today) {
				style = sSubtle
			}
			rows = append(rows, upRow{day: label, style: style})
		}
		for i, e := range eps {
			r := upRow{style: style, ep: e}
			if i == 0 {
				r.day = label
			}
			rows = append(rows, r)
		}
	}
	if len(later) == 0 {
		return rows
	}
	rows = append(rows, upRow{}, upRow{day: "Later", style: sSubtle})
	prev := ""
	for _, e := range later {
		r := upRow{style: sNormal, ep: e}
		if e.Aired != prev {
			r.day = clean(e.Aired)
			if t, err := time.Parse(time.DateOnly, e.Aired); err == nil {
				r.day = t.Format("Mon 2 Jan")
			}
		}
		prev = e.Aired
		rows = append(rows, r)
	}
	return rows
}

func upcomingShow(e api.UpcomingEpisode) string { return clean(cmp.Or(e.SeriesNameEng, e.SeriesName)) }

func upcomingLabel(e api.UpcomingEpisode) string {
	return fmt.Sprintf("S%02dE%02d", e.Season, e.Number)
}

// upcomingPlan is what garner will do with the episode, from its state,
// whether the show's monitor mode gets it and when the monitor searches
// it.
func upcomingPlan(e api.UpcomingEpisode, monitorOff string, now time.Time) []part {
	switch e.State {
	case api.EpisodeImported:
		return []part{{"in the library", sSubtle}}
	case api.EpisodeDownloading:
		return []part{{"downloading", sMuted}}
	case api.EpisodeProcessing:
		return []part{{"processing", sMuted}}
	case api.EpisodeImporting:
		return []part{{"importing", sMuted}}
	case api.EpisodeAttention:
		return []part{{"needs you", sAmber}}
	}
	var parts []part
	switch e.State {
	case api.EpisodeFailed:
		parts = []part{{"failed", sRed}, {" · ", sMuted}}
	case api.EpisodeUpcoming:
	default:
		parts = []part{{"missing · ", sMuted}}
	}
	plan := ""
	switch {
	case !e.Monitored:
		plan = "not monitored"
	case monitorOff != "":
		plan = "the monitor is " + clean(monitorOff)
	case e.NextSearch == nil:
		plan = "no search planned"
	case e.State == api.EpisodeUpcoming && e.NextSearch.Local().Format(time.DateOnly) == e.Aired:
		plan = "searches the day it airs"
	default:
		plan = nextSearch(*e.NextSearch, now)
	}
	return append(parts, part{plan, sMuted})
}

// upCols sizes the tab's table: glyph, day, show, episode, title, then
// what garner will do takes the rest.
type upCols struct {
	width, day, show, episode, title int
}

func upcomingColumns(rows []upRow, width int) upCols {
	c := upCols{width: width, day: len("Day"), show: len("Show"), episode: len("Episode"), title: len("Title")}
	for _, r := range rows {
		c.day = max(c.day, ansi.StringWidth(r.day))
		if r.ep == nil {
			continue
		}
		c.show = max(c.show, ansi.StringWidth(upcomingShow(*r.ep)))
		c.episode = max(c.episode, len(upcomingLabel(*r.ep)))
		c.title = max(c.title, ansi.StringWidth(clean(cmp.Or(r.ep.NameEng, r.ep.Name, "no title yet"))))
	}
	c.title = min(c.title, 36)
	// Margins and gaps: 3 + glyph 1 + 3 + day + 3 + show + 3 + episode + 3
	// + title + 3 + plan + 3. Narrow: the titles give way, then go with
	// their gap, then the names give way.
	room := width - (3 + 1 + 3 + c.day + 3 + 3 + c.episode + 3 + 3 + 3) - 24
	if c.show+c.title > room {
		c.title = room - c.show
	}
	if c.title < 10 {
		c.title, room = 0, room+3
		c.show = max(12, min(c.show, room))
	}
	return c
}

func (c upCols) head() string {
	title := ""
	if c.title > 0 {
		title = sSubtle.Render(fit("Title", c.title, false)) + "   "
	}
	return "   " + " " + "   " + sSubtle.Render(fit("Day", c.day, false)) + "   " + sSubtle.Render(fit("Show", c.show, false)) +
		"   " + sSubtle.Render(fit("Episode", c.episode, false)) + "   " + title + sSubtle.Render("garner")
}

func (c upCols) row(r upRow, monitorOff string, now time.Time, selected bool) string {
	cur := cursor(selected)
	bg := cur.bg
	gap := bg(lipgloss.NewStyle()).Render("   ")
	glyph, glyphStyle := " ", sSubtle
	if r.ep != nil {
		glyph, glyphStyle = episodeGlyph(r.ep.State)
	}
	line := cur.margin() + bg(glyphStyle).Render(glyph) + gap + bg(r.style).Render(fit(r.day, c.day, false))
	if r.ep == nil {
		return cur.fill(line, c.width)
	}
	e := *r.ep
	title, titleStyle := clean(cmp.Or(e.NameEng, e.Name)), sNormal
	if title == "" {
		title, titleStyle = "no title yet", sSubtle
	}
	line += gap + bg(sNormal).Render(fit(upcomingShow(e), c.show, false)) +
		gap + bg(sNormal).Render(fit(upcomingLabel(e), c.episode, false)) + gap
	if c.title > 0 {
		line += bg(titleStyle).Render(fit(title, c.title, false)) + gap
	}
	line += renderParts(upcomingPlan(e, monitorOff, now), max(0, c.width-3-lipgloss.Width(line)), bg)
	return cur.fill(line, c.width)
}

// upcomingView is the tab below the tabs: column names, a rule and the
// rows.
func (s *libraryScreen) upcomingView(width int, now time.Time) string {
	var b strings.Builder
	switch {
	case s.upcoming == nil && s.upcomingErr != nil:
		b.WriteString("   " + gFailed + " " + sRed.Render(clean(s.upcomingErr.Error())))
		return b.String()
	case s.upcoming == nil:
		b.WriteString("   " + sMuted.Render("Loading the upcoming episodes…"))
		return b.String()
	}
	rows := upcomingRows(s.upcoming)
	cols := upcomingColumns(rows, width)
	b.WriteString(cols.head() + "\n")
	b.WriteString("   " + sBorder.Render(strings.Repeat("─", max(0, width-6))) + "\n")
	cur := s.upCursor(rows)
	if cur >= 0 {
		s.upOffset = min(max(s.upOffset, cur-s.page+1), cur)
	}
	s.upOffset = max(0, min(s.upOffset, len(rows)-s.page))
	for i := s.upOffset; i < min(len(rows), s.upOffset+s.page); i++ {
		b.WriteString(cols.row(rows[i], s.upcoming.MonitorOff, now, i == cur) + "\n")
	}
	return b.String()
}

// upCursor is the row of the episode under the cursor: the one chosen
// before, or the first that airs today or later. -1 without episodes.
func (s *libraryScreen) upCursor(rows []upRow) int {
	if i := slices.IndexFunc(rows, func(r upRow) bool { return r.ep != nil && r.ep.EpisodeID == s.upSelected }); i >= 0 {
		return i
	}
	today := ""
	if s.upcoming != nil {
		today = upcomingToday(s.upcoming).Format(time.DateOnly)
	}
	i := slices.IndexFunc(rows, func(r upRow) bool { return r.ep != nil && r.ep.Aired >= today })
	if i < 0 {
		i = slices.IndexFunc(rows, func(r upRow) bool { return r.ep != nil })
	}
	if i >= 0 {
		s.upSelected = rows[i].ep.EpisodeID
	}
	return i
}

// upMove moves the cursor by n episodes; days without any are passed.
func (s *libraryScreen) upMove(n int) {
	if s.upcoming == nil {
		return
	}
	rows := upcomingRows(s.upcoming)
	var eps []int
	for i, r := range rows {
		if r.ep != nil {
			eps = append(eps, i)
		}
	}
	if len(eps) == 0 {
		return
	}
	at := max(0, slices.Index(eps, s.upCursor(rows)))
	s.upSelected = rows[eps[min(len(eps)-1, max(0, at+n))]].ep.EpisodeID
}

func (s *libraryScreen) cursorUpcoming() (api.UpcomingEpisode, bool) {
	if s.upcoming == nil {
		return api.UpcomingEpisode{}, false
	}
	rows := upcomingRows(s.upcoming)
	if i := s.upCursor(rows); i >= 0 {
		return *rows[i].ep, true
	}
	return api.UpcomingEpisode{}, false
}

func (s *libraryScreen) hasUpcoming() bool {
	_, ok := s.cursorUpcoming()
	return ok
}

// upcomingHelp is the Library's help() on this tab: s searches here, and
// the keys for the shows of the other tabs do nothing.
var upcomingHelp = []string{
	"enter", "open the show", "s", "search the episode now, once it aired",
	"a", "add a show", "tab", "next category",
	"1 2 3 4", "jump to a category", "q", "quit",
}

// upAired is the episode under the cursor when it has aired by the
// server's day, so it can be searched.
func (s *libraryScreen) upAired() (api.UpcomingEpisode, bool) {
	e, ok := s.cursorUpcoming()
	if !ok || e.Aired == "" || e.Aired > upcomingToday(s.upcoming).Format(time.DateOnly) {
		return api.UpcomingEpisode{}, false
	}
	return e, true
}

// upSearch is s on the tab: it searches the aired episode under the cursor
// now and opens its releases, as s on an episode row of the Show screen
// does. esc there comes back to the tab, the cursor where it was.
func (s *libraryScreen) upSearch() tea.Cmd {
	e, ok := s.upAired()
	if !ok {
		return nil
	}
	row := episodeRow{
		episode: api.Episode{ID: e.EpisodeID, Aired: e.Aired, Name: e.Name, NameEng: e.NameEng},
		pos:     api.Position{Season: e.Season, Number: e.Number},
		status:  api.EpisodeStatus{EpisodeID: e.EpisodeID, State: e.State},
	}
	next := newReleases(s.sess, newShow(s.sess, s, e.SeriesID, upcomingShow(e), ""), row, true)
	next.leave = s
	return func() tea.Msg { return switchMsg{next} }
}

// upSearchSlot is s in the tab's key bar, greyed out on an episode that
// has not aired.
func (s *libraryScreen) upSearchSlot() string {
	_, aired := s.upAired()
	return keySlot("s", "search now", aired)
}
