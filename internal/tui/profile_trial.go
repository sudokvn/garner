package tui

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/client"
	"github.com/sudokvn/garner/internal/profile"
)

// The changes pane tries the unsaved profile on a show's last search: the
// server judges the stored releases again under it, and the pane shows
// what that changes. Nothing is searched, stored or downloaded. The show
// is one that uses the profile and has a stored search, preferring one
// with a missing episode; t picks the next.

// trialWait is how long the profile stays unchanged before the server is
// asked, so moving through options does not send a request a key.
const trialWait = 400 * time.Millisecond

// trialTries bounds the searches one choice of show asks to judge, as
// searches stored before garner kept what the indexers reported cannot
// be judged again.
const trialTries = 6

// trialShows bounds the shows one choice of show looks up, as a profile
// many shows use may have no stored search to judge.
const trialShows = 12

// trialListed is how many changed releases the pane lists.
const trialListed = 3

type (
	// trialTickMsg ends the wait after a change; seq tells whether another
	// change came since.
	trialTickMsg struct {
		from *editorScreen
		seq  int
	}
	// trialMsg is the server's judgement, or why there is none.
	trialMsg struct {
		from      *editorScreen
		key       string // the profile judged, as judgeKey writes it
		other     bool   // t asked for another show
		pick      *trialPick
		judgement *api.Judgement
		note      string // why nothing could be judged
		err       error
	}
)

// trialPick is the show and episode the profile is tried on.
type trialPick struct {
	show    api.SeriesDetail
	episode int64
	code    string // such as S01E11
}

// trial is the state of trying the profile on a search.
type trial struct {
	pick      *trialPick
	judgement *api.Judgement
	judged    string // judgeKey of the profile judged or being judged
	shown     string // judgeKey of the profile judgement is for
	note      string
	seq       int
	busy      bool
	again     bool // the profile changed while the server judged
	another   bool // t was pressed while the server judged
}

// judgeKey is the profile as far as judging goes: its name and
// description change nothing.
func judgeKey(p profile.Profile) string {
	p.Name, p.Description = "", ""
	return profile.Encode(p)
}

// trialAfterChange starts the wait after a key changed the profile.
func (s *editorScreen) trialAfterChange() tea.Cmd {
	s.trial.seq++
	seq := s.trial.seq
	return tea.Tick(trialWait, func(time.Time) tea.Msg { return trialTickMsg{s, seq} })
}

// startTrial asks the server to judge the profile as it is, on the show
// tried on so far or, with other, on the next one.
func (s *editorScreen) startTrial(other bool) tea.Cmd {
	t := &s.trial
	if !s.changed() {
		return nil
	}
	if s.id == 0 || s.shows == 0 {
		t.note, t.judgement, t.judged = "No show uses this profile, so there is no search to try it on.", nil, ""
		return nil
	}
	if problems, _ := s.check(); len(problems) > 0 {
		// Not judged: once the problems are gone, the server is asked again.
		t.note, t.judgement, t.judged = "The profile has problems, so it can't be tried on a search.", nil, ""
		return nil
	}
	key := judgeKey(s.p)
	if t.busy {
		t.again = t.again || key != t.judged
		t.another = t.another || other
		return nil
	}
	if !other && t.current(key) {
		return nil
	}
	t.busy, t.judged = true, key
	cl, id, text, shows := s.sess.client, s.id, profile.Encode(s.p), s.sess.shows
	fixed := t.pick
	var after int64
	if other && t.pick != nil {
		fixed, after = nil, t.pick.show.Series.ID
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg := trialMsg{from: s, key: key, other: other}
		if fixed != nil {
			j, err := cl.JudgeSearch(ctx, fixed.episode, text)
			msg.pick = fixed
			if err == nil {
				msg.judgement = &j
				return msg
			}
			if msg.note, msg.err = judgeNote(fixed, err); msg.err != nil || msg.note != "" {
				return msg
			}
		}
		pick, j, note, err := chooseTrial(ctx, cl, shows, id, after, text)
		msg.pick, msg.judgement, msg.note, msg.err = pick, j, note, err
		return msg
	}
}

// judgeNote turns the server's refusal to judge into the pane's line;
// an empty note with no error means another search may do.
func judgeNote(p *trialPick, err error) (string, error) {
	var se *client.ServerError
	switch {
	case errors.Is(err, client.ErrUnauthorized):
		return "", err
	case errors.As(err, &se) && len(se.Problems) > 0:
		return "The server found problems in the profile, so it can't be tried on a search.", nil
	case errors.As(err, &se) && (se.Status == 404 || se.Status == 409):
		return "", nil
	}
	return "Could not try the changes on " + clean(p.show.Series.Name) + " " + p.code + ": " + clean(err.Error()), nil
}

// chooseTrial finds a show that uses profile id and a stored search of
// it the server can judge, starting after the show after when it is set.
// It looks up at most trialShows shows and asks to judge at most
// trialTries searches.
func chooseTrial(ctx context.Context, cl *client.Client, shows []api.Show, id, after int64, text string) (*trialPick, *api.Judgement, string, error) {
	if len(shows) == 0 {
		var err error
		if shows, err = cl.Library(ctx); err != nil {
			if errors.Is(err, client.ErrUnauthorized) {
				return nil, nil, "", err
			}
			return nil, nil, "Could not load the library: " + clean(err.Error()), nil
		}
	}
	var using []api.Show
	for _, sh := range shows {
		if sh.ProfileID == id {
			using = append(using, sh)
		}
	}
	// Shows with a missing episode first: their search is the one that
	// matters most.
	missing := func(sh api.Show) bool { return sh.Counts[api.EpisodeWaiting]+sh.Counts[api.EpisodeFailed] > 0 }
	slices.SortStableFunc(using, func(a, b api.Show) int {
		if ma, mb := missing(a), missing(b); ma != mb {
			if ma {
				return -1
			}
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	if after != 0 {
		if i := slices.IndexFunc(using, func(sh api.Show) bool { return sh.ID == after }); i >= 0 {
			// The others in turn; the one tried on so far is not asked again.
			using = append(using[i+1:], using[:i]...)
		}
	}
	tries, note := 0, ""
	for i, sh := range using {
		if i == trialShows {
			return nil, nil, cmp.Or(note, "No search of the shows that use this profile could be judged again."), nil
		}
		d, err := cl.Series(ctx, sh.ID)
		if errors.Is(err, client.ErrUnauthorized) {
			return nil, nil, "", err
		}
		if err != nil {
			continue
		}
		for _, ep := range searchedEpisodes(d) {
			if tries == trialTries {
				return nil, nil, cmp.Or(note, "No search of the shows that use this profile could be judged again."), nil
			}
			tries++
			p := &trialPick{show: d, episode: ep.id, code: ep.code}
			j, judgeErr := cl.JudgeSearch(ctx, ep.id, text)
			if judgeErr == nil {
				return p, &j, "", nil
			}
			if n, err := judgeNote(p, judgeErr); err != nil || n != "" {
				return nil, nil, n, err
			}
			var se *client.ServerError
			if errors.As(judgeErr, &se) && se.Status == 409 && note == "" {
				// The server says why, such as that the search is too old.
				note = clean(d.Series.Name) + " " + ep.code + ": " + clean(se.Message)
			}
		}
	}
	if note != "" {
		return nil, nil, note, nil
	}
	if after != 0 {
		return nil, nil, "", nil
	}
	return nil, nil, "No show that uses this profile has a search to try it on.", nil
}

type searchedEpisode struct {
	id   int64
	code string
}

// searchedEpisodes lists a show's episodes with a stored search: missing
// ones first, then the latest aired.
func searchedEpisodes(d api.SeriesDetail) []searchedEpisode {
	episodes := map[int64]api.Episode{}
	for _, e := range d.Episodes {
		episodes[e.ID] = e
	}
	var states []api.EpisodeStatus
	for _, st := range d.States {
		if st.Summary != "" {
			states = append(states, st)
		}
	}
	missing := func(st api.EpisodeStatus) bool {
		return st.State == api.EpisodeWaiting || st.State == api.EpisodeFailed
	}
	slices.SortStableFunc(states, func(a, b api.EpisodeStatus) int {
		if ma, mb := missing(a), missing(b); ma != mb {
			if ma {
				return -1
			}
			return 1
		}
		return cmp.Compare(episodes[b.EpisodeID].Aired, episodes[a.EpisodeID].Aired)
	})
	out := make([]searchedEpisode, len(states))
	for i, st := range states {
		code := "episode"
		if pos, ok := episodes[st.EpisodeID].Numbers[d.Series.DefaultOrder]; ok {
			code = fmt.Sprintf("S%02dE%02d", pos.Season, pos.Number)
		}
		out[i] = searchedEpisode{st.EpisodeID, code}
	}
	return out
}

// trialAnswer takes the server's judgement.
func (s *editorScreen) trialAnswer(msg trialMsg) tea.Cmd {
	t := &s.trial
	t.busy = false
	if errors.Is(msg.err, client.ErrUnauthorized) {
		return func() tea.Msg { return disconnectedMsg{errRevoked} }
	}
	switch {
	case msg.other && msg.judgement == nil && t.judgement != nil:
		// The show tried on so far stays.
		s.flash.info(sMuted.Render("No other show that uses this profile has a search that can be judged again."))
	case msg.judgement != nil:
		t.pick, t.judgement, t.note, t.shown = msg.pick, msg.judgement, "", msg.key
	default:
		t.judgement, t.note = nil, msg.note
		if msg.pick != nil {
			t.pick = msg.pick
		}
	}
	if t.another {
		t.again, t.another = false, false
		return s.startTrial(true)
	}
	if t.again || !t.current(judgeKey(s.p)) {
		t.again = false
		return s.startTrial(false)
	}
	return nil
}

// current reports whether the pane shows the answer for the profile key:
// its judgement, or why it has none. A judgement t kept from before is
// not current for a profile changed since.
func (t *trial) current(key string) bool {
	if t.judgement != nil {
		return t.shown == key
	}
	return t.note != "" && t.judged == key
}

// trialLines are the pane's lines about the search the profile is tried
// on, inner cells wide.
func (s *editorScreen) trialLines(inner int) []string {
	t := &s.trial
	label := "Tried on"
	switch {
	case t.judgement == nil && t.note != "":
		return wrapLabelled(label, sMuted.Render(t.note), inner)
	case t.judgement == nil:
		return wrapLabelled(label, sMuted.Render("Trying the changes on a show's last search…"), inner)
	}
	j, p := t.judgement, t.pick
	head := sNormal.Render(clean(p.show.Series.Name)+" "+p.code) + sMuted.Render(" · its last search, "+when(j.Searched, time.Now()))
	if p.show.LanguageName != "" {
		head += sMuted.Render(" · original language " + clean(p.show.LanguageName))
	}
	if t.busy || t.shown != judgeKey(s.p) {
		head += sMuted.Render(" · updating…")
	}
	lines := wrapLabelled(label, head, inner)
	room := inner - settingWidth
	listed, more := 0, 0
	for _, c := range j.Candidates {
		if c.Before.Verdict == c.After.Verdict && c.Before.Tier == c.After.Tier {
			continue
		}
		if listed == trialListed {
			more++
			continue
		}
		listed++
		lines = append(lines, labelled("", sNormal.Render(cutMiddle(clean(c.Title), room))))
		lines = append(lines, changeRows("", verdictParts(c.Before, j, false), verdictParts(c.After, j, true), inner)...)
	}
	before, after := firstDownload(j, false), firstDownload(j, true)
	switch {
	case before == nil && after == nil:
		lines = append(lines, labelled("", sMuted.Render("Nothing would be downloaded, as before.")))
	case before != nil && after != nil && before.ID == after.ID:
		lines = append(lines, firstLine("Still first: ", after.Title, after.After.Tier, room))
	default:
		if after != nil {
			lines = append(lines, firstLine("First now: ", after.Title, after.After.Tier, room))
		} else {
			lines = append(lines, labelled("", sMuted.Render("Nothing would be downloaded now.")))
		}
		if before != nil {
			lines = append(lines, firstLine("First before: ", before.Title, before.Before.Tier, room))
		}
	}
	switch {
	case more > 0:
		lines = append(lines, labelled("", sMuted.Render(plural(more, "more release")+" change verdict or tier.")))
	case listed > 0:
		lines = append(lines, labelled("", sMuted.Render("No other verdict changes.")))
	default:
		lines = append(lines, labelled("", sMuted.Render("No verdict changes.")))
	}
	return lines
}

// firstLine names the release that would download first.
func firstLine(lead, title, tier string, room int) string {
	rest := ""
	if tier != "" {
		rest = " · " + clean(tier)
	}
	name := cutMiddle(clean(title), max(10, room-len(lead)-len(rest)))
	return labelled("", sMuted.Render(lead)+sNormal.Render(name)+sMuted.Render(rest))
}

// firstDownload is the release garner would download first under the
// profile before or after: the best accepted one, else the best last
// resort, skipping postings whose download failed or was cancelled.
func firstDownload(j *api.Judgement, after bool) *api.JudgedCandidate {
	judged := func(c api.JudgedCandidate) api.Judged {
		if after {
			return c.After
		}
		return c.Before
	}
	var best *api.JudgedCandidate
	better := func(c *api.JudgedCandidate) bool {
		if best == nil {
			return true
		}
		a, b := judged(*c), judged(*best)
		if a.Verdict != b.Verdict {
			return a.Verdict == api.Accept
		}
		return a.Rank < b.Rank
	}
	for i := range j.Candidates {
		c := &j.Candidates[i]
		v := judged(*c).Verdict
		if v == api.Reject || skipped(c.Attempt) {
			continue
		}
		if better(c) {
			best = c
		}
	}
	return best
}

// skipped reports whether garner on its own passes over a posting after
// this download of it.
func skipped(d *api.Download) bool {
	if d == nil {
		return false
	}
	return d.State == api.Failed || d.State == api.Cancelled || d.Import.State == api.ImportRejected || d.Import.State == api.ImportFailed
}

// verdictParts describe a release's verdict under one profile: the
// verdict, the tier and its place among the accepted releases, or the
// server's reasons that decided it.
func verdictParts(v api.Judged, j *api.Judgement, after bool) []part {
	sep := part{" · ", sMuted}
	var parts []part
	switch v.Verdict {
	case api.Accept:
		parts = []part{{"✓ ", sMint}, {"accepted", sNormal}}
	case api.Fallback:
		parts = []part{{"~ ", sAmber}, {"last resort", sNormal}}
	default:
		parts = []part{{"✗ ", sRed}, {"rejected", sNormal}}
	}
	if after {
		for i := range parts {
			if i > 0 {
				parts[i].style = sBold
			}
		}
	}
	if v.Tier != "" {
		parts = append(parts, sep, part{clean(v.Tier), sNormal})
	}
	if v.Verdict == api.Accept {
		place, of := 0, 0
		for _, c := range j.Candidates {
			o := c.Before
			if after {
				o = c.After
			}
			if o.Verdict == api.Accept {
				of++
				if o.Rank <= v.Rank {
					place++
				}
			}
		}
		parts = append(parts, sep, part{acceptedPlace(place, of), sMuted})
		return parts
	}
	if n := min(max(v.Decisive, 1), len(v.Reasons)); n > 0 {
		parts = append(parts, sep, part{clean(strings.Join(v.Reasons[:n], "; ")), sMuted})
	}
	return parts
}

// acceptedPlace says where an accepted release ranks: "1st of 4 accepted
// releases", "the only accepted release".
func acceptedPlace(place, of int) string {
	switch {
	case of == 1:
		return "the only accepted release"
	case place == of:
		return "last of the " + strconv.Itoa(of) + " accepted releases"
	}
	return ordinal(place) + " of " + strconv.Itoa(of) + " accepted releases"
}
