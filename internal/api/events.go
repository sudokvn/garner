package api

// Events the server streams from /api/v1/events, as server-sent events of
// these names. Each says which type its JSON data holds.
const (
	EventHello           = "hello"            // Hello, first on every stream
	EventSearchProgress  = "search.progress"  // SearchProgress
	EventSearchFinished  = "search.finished"  // SearchFinished
	EventSeriesUpdated   = "series.updated"   // SeriesChanged
	EventSeriesDeleted   = "series.deleted"   // SeriesChanged
	EventSeriesSearch    = "series.search"    // ShowProgress
	EventDownloadUpdated = "download.updated" // Download
	EventProfilesUpdated = "profiles.updated" // no data: reload the profiles
)

// Hello opens an event stream.
type Hello struct {
	Version string `json:"version"`
}

// SearchFinished is a finished search for an episode; season searches
// send none.
type SearchFinished struct {
	EpisodeID int64         `json:"episode_id"`
	Outcome   SearchOutcome `json:"outcome"`
}

// SeriesChanged names a show that changed or was deleted.
type SeriesChanged struct {
	ID int64 `json:"id"`
}
