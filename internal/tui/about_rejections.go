package tui

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/sudokvn/garner/internal/api"
)

// problemNames name the kinds of problem the file check rejects a
// download for, in plain words.
var problemNames = map[api.ProblemKind]string{
	api.ProblemAudioLanguage: "Wrong audio language",
	api.ProblemLength:        "Wrong length",
	api.ProblemResolution:    "Wrong resolution",
	api.ProblemWrongEpisode:  "Wrong episode",
	api.ProblemNoVideoFile:   "No video file",
	api.ProblemNoVideoTrack:  "No video track",
	api.ProblemNoAudioTrack:  "No audio track",
	api.ProblemUnreadable:    "Unreadable file",
	api.ProblemUnclassified:  "Not classified",
}

// problemName names a kind, also one this garner does not know yet.
func problemName(k api.ProblemKind) string {
	if name, ok := problemNames[k]; ok {
		return name
	}
	return clean(strings.ReplaceAll(string(k), "_", " "))
}

// rejectionBar is the widest bar of the counts by kind, in cells: one
// cell a download, scaled down when a count is larger.
const rejectionBar = 20

// rejectionLines fill About's pane of the downloads the file check
// rejected: how many and since when, a count per kind with a bar, and
// what became of their episodes.
func (s *aboutScreen) rejectionLines() []string {
	switch {
	case s.rejectErr != nil:
		return []string{gFailed + " " + sRed.Render(clean(s.rejectErr.Error()))}
	case s.rejections == nil:
		return []string{gWorking + " " + sMuted.Render("Counting…")}
	case s.rejections.Total == 0:
		return []string{sMuted.Render("The file check has rejected no download.")}
	}
	r := s.rejections
	head := sBold.Render(plural(r.Total, "download")) + sMuted.Render(" rejected by the file check")
	if !r.Oldest.IsZero() {
		head += sMuted.Render(" since " + r.Oldest.Local().Format("2 Jan 2006"))
	}
	head += sMuted.Render(" · " + strconv.Itoa(r.LastWeek) + " in the last 7 days")
	lines := []string{head, ""}

	type count struct {
		name string
		n    int
	}
	var counts []count
	most, wide := 0, 0
	for k, n := range r.Kinds {
		if n > 0 {
			counts = append(counts, count{problemName(k), n})
			most, wide = max(most, n), max(wide, len(strconv.Itoa(n)))
		}
	}
	slices.SortFunc(counts, func(a, b count) int { return cmp.Or(cmp.Compare(b.n, a.n), cmp.Compare(a.name, b.name)) })
	for _, c := range counts {
		bar := c.n
		if most > rejectionBar {
			bar = max(1, (c.n*rejectionBar+most/2)/most)
		}
		lines = append(lines, sSubtle.Render(pad(c.name, 22))+sNormal.Render(fmt.Sprintf("%*d", wide, c.n))+"   "+sMuted.Render(strings.Repeat("■", bar)))
	}

	// What became of the episodes: a file from a later download, none
	// yet, or the file they had before.
	outcome := plural(r.Episodes, "episode") + " affected: " + strconv.Itoa(r.Imported) + " imported later · " + strconv.Itoa(r.Missing) + " still missing"
	if kept := r.Episodes - r.Imported - r.Missing; kept > 0 {
		outcome += " · " + strconv.Itoa(kept) + " kept an earlier file"
	}
	return append(lines, "", sNormal.Render(outcome))
}
