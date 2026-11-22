package api

import "time"

// SearchOutcome is what a search came to.
type SearchOutcome string

// Search outcomes.
const (
	SearchFound    SearchOutcome = "found"    // a release was accepted
	SearchFallback SearchOutcome = "fallback" // only last resorts are left
	SearchNothing  SearchOutcome = "nothing"  // no release is usable
	SearchFailed   SearchOutcome = "failed"   // every indexer failed without returning anything
)

// Verdict is what garner does with a release.
type Verdict string

// Verdicts.
const (
	Accept Verdict = "accept"
	// Fallback releases are used only when nothing is accepted; the file
	// check confirms them after download.
	Fallback Verdict = "fallback"
	Reject   Verdict = "reject"
)

// Search is the latest search for an episode.
type Search struct {
	EpisodeID int64 `json:"episode_id"`
	// Season is set instead for a season search, which keeps only packs.
	Season   int           `json:"season,omitempty"`
	Searched time.Time     `json:"searched"`
	Outcome  SearchOutcome `json:"outcome"`
	Summary  string        `json:"summary"`
	// NextRetry is when the monitor searches the episode again, as the
	// server answers; unset when the monitor does not get the episode.
	NextRetry  *time.Time      `json:"next_retry,omitempty"`
	Indexers   []SearchIndexer `json:"indexers"`
	Candidates []Candidate     `json:"candidates"`
}

// SearchIndexer is what one indexer returned.
type SearchIndexer struct {
	Name    string `json:"name"`
	Results int    `json:"results"`
	Error   string `json:"error,omitempty"`
	// Partial reports whether the indexer answered only some of an
	// episode search's queries, which the search's summary notes; nil for
	// searches stored before garner kept it, and for season searches.
	// Never sent to clients.
	Partial *bool `json:"-"`
}

// Candidate is a release a search found, with the verdict on it.
type Candidate struct {
	ID   int64 `json:"id"`
	Rank int   `json:"rank"`
	// SearchSeason identifies the parent regular-season search when its
	// download link is loaded; zero means an episode search. Specials
	// have no season-pack searches. Never sent to clients.
	SearchSeason int    `json:"-"`
	Indexer      string `json:"indexer"`
	Title        string `json:"title"`
	// Link downloads the release through Prowlarr and holds its API key. It
	// is stored but never loaded for or sent to clients.
	Link string `json:"-"`
	// Posting tells NZB postings of the same release apart: a fingerprint
	// of the indexer's ID for this one. Empty for candidates stored before
	// garner kept it. Never sent to clients.
	Posting string `json:"-"`
	// Page is the release's page at the indexer, for people to open; empty
	// when unknown or when its address could hold credentials.
	Page string `json:"page,omitempty"`
	// Source is where the release comes from as its name says, such as
	// BluRay or WEB-DL; empty when the name does not say.
	Source    string     `json:"source,omitempty"`
	Size      int64      `json:"size"`
	Published *time.Time `json:"published,omitempty"`
	Grabs     int        `json:"grabs"`
	Verdict   Verdict    `json:"verdict"`
	Tier      string     `json:"tier,omitempty"`
	Audio     []string   `json:"audio"`
	Reasons   []string   `json:"reasons"`
	// Decisive counts the leading reasons that decided the verdict: why it
	// was rejected, or why it is only a last resort. The rest are facts.
	Decisive int     `json:"decisive"`
	Episodes []int64 `json:"episodes"`
	// Attempt is the latest download of this posting for the episode the
	// candidate is for; nil when garner never tried it.
	Attempt *Download `json:"attempt,omitempty"`
	// Reported is what the indexer reported about the release beyond its
	// name, kept to judge it again; nil for candidates stored before
	// garner kept it. Never sent to clients.
	Reported *IndexerReport `json:"-"`
}

// IndexerReport is what an indexer reported about a release beyond its
// name that judging it needs.
type IndexerReport struct {
	// Languages are the audio languages as the indexer writes them, such
	// as "German"; empty when it reports none.
	Languages []string
	// TVDBID is the show the indexer files the release under; 0 when it
	// names none.
	TVDBID int64
}

// SamePosting reports whether the download is of this candidate's NZB
// posting: by the indexer's ID for it when both have one, otherwise, for
// rows from before garner kept it, by name and size.
func (c Candidate) SamePosting(d Download) bool {
	if c.Posting != "" && d.Posting != "" {
		return c.Posting == d.Posting
	}
	return c.Title == d.Title && c.Size == d.Size
}

// Judgement is an episode's latest stored search judged again without
// searching: its releases under the show's own profile (Before) and under
// a profile sent with the request (After), both by the library and the
// catalogue as they are now. Before is the stored judgement, unless the
// library, the catalogue or garner's rules changed since the search, so
// what differs between Before and After comes from the profile alone.
// Judging stores and downloads nothing.
type Judgement struct {
	EpisodeID int64 `json:"episode_id"`
	// Searched is when the stored search ran.
	Searched time.Time    `json:"searched"`
	Before   JudgedSearch `json:"before"`
	After    JudgedSearch `json:"after"`
	// Candidates are the search's releases, in the order After ranks them.
	Candidates []JudgedCandidate `json:"candidates"`
}

// JudgedSearch is a search's outcome under one profile.
type JudgedSearch struct {
	// Profile names the profile; Rules describes its preferences for the
	// show, as ProfileInfo does.
	Profile string        `json:"profile"`
	Rules   string        `json:"rules"`
	Outcome SearchOutcome `json:"outcome"`
	Summary string        `json:"summary"`
}

// JudgedCandidate is a stored release judged under both profiles.
type JudgedCandidate struct {
	// ID is the stored candidate's. Downloading it goes by the stored
	// judgement, not by these.
	ID      int64  `json:"id"`
	Indexer string `json:"indexer"`
	Title   string `json:"title"`
	Size    int64  `json:"size"`
	Before  Judged `json:"before"`
	After   Judged `json:"after"`
	// Attempt is the latest download of this posting for the episode; nil
	// when garner never tried it. garner on its own skips a posting whose
	// download failed or was cancelled.
	Attempt *Download `json:"attempt,omitempty"`
}

// Judged is the verdict on a release under one profile.
type Judged struct {
	Verdict Verdict `json:"verdict"`
	// Tier is the profile's preference the release meets, such as "German
	// 1080p"; empty when it meets none.
	Tier string `json:"tier,omitempty"`
	// Rank is the release's place among the search's releases, 1 for the
	// best.
	Rank  int      `json:"rank"`
	Audio []string `json:"audio"`
	// Reasons explain the verdict; the first Decisive of them decided it,
	// as in Candidate.
	Reasons  []string `json:"reasons"`
	Decisive int      `json:"decisive"`
}

// SearchProgress is a running search: every indexer, and what it answered
// so far. A season search sets SeriesID and Season instead of EpisodeID.
type SearchProgress struct {
	EpisodeID int64             `json:"episode_id"`
	SeriesID  int64             `json:"series_id,omitempty"`
	Season    int               `json:"season,omitempty"`
	Indexers  []IndexerProgress `json:"indexers"`
}

type IndexerProgress struct {
	Name    string `json:"name"`
	Done    bool   `json:"done"`
	Results int    `json:"results"`
	Error   string `json:"error,omitempty"`
}

// ShowProgress is a search of every missing episode of a show.
type ShowProgress struct {
	SeriesID int64 `json:"series_id"`
	// Waiting is set until the search starts, after the searches before it.
	Waiting bool `json:"waiting,omitempty"`
	// Total counts the episodes to search, Done those searched so far, and
	// Downloads the downloads they started.
	Total     int  `json:"total"`
	Done      int  `json:"done"`
	Downloads int  `json:"downloads"`
	Finished  bool `json:"finished,omitempty"`
}
