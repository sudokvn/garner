package api

import (
	"slices"
	"time"
)

// DownloadState is where a download is with SABnzbd.
type DownloadState string

// Download states.
const (
	Submitting  DownloadState = "submitting" // recorded, SABnzbd not yet confirmed
	Queued      DownloadState = "queued"
	Downloading DownloadState = "downloading"
	Processing  DownloadState = "processing" // SABnzbd repairs, unpacks or moves the files
	Completed   DownloadState = "completed"
	Failed      DownloadState = "failed"
	Cancelled   DownloadState = "cancelled" // stopped on request; not a failure of the release
)

// Download is a release handed to SABnzbd.
type Download struct {
	ID        int64 `json:"id"`
	EpisodeID int64 `json:"episode_id"`
	// SearchSeason is the source regular season (>0), or zero for an
	// episode search: specials have no season-pack searches. It stays
	// with replacements and is internal, like Link.
	SearchSeason int    `json:"-"`
	Title        string `json:"title"`
	Indexer      string `json:"indexer"`
	// Link holds Prowlarr's API key; it is never sent to clients.
	Link string `json:"-"`
	// Posting is the candidate's Posting; internal, like Link.
	Posting string  `json:"-"`
	Size    int64   `json:"size"`
	Verdict Verdict `json:"verdict"`
	Tier    string  `json:"tier,omitempty"`
	// Reasons and Decisive are the release's search result: why it was
	// chosen, the first Decisive of them deciding.
	Reasons  []string `json:"reasons"`
	Decisive int      `json:"decisive"`
	Manual   bool     `json:"manual"`
	// Replaces is the tier of the file in the library the download
	// replaces, such as "German 720p"; empty when the episode had no file
	// when it started, or the file's tier is unknown.
	Replaces string        `json:"replaces,omitempty"`
	State    DownloadState `json:"state"`
	// Detail is SABnzbd's own status or running step, such as "Repairing".
	Detail   string  `json:"detail,omitempty"`
	JobID    string  `json:"job_id,omitempty"`
	Progress float64 `json:"progress"`
	TimeLeft string  `json:"time_left,omitempty"`
	// Storage is where SABnzbd put the files, as SABnzbd sees its disk.
	Storage string `json:"storage,omitempty"`
	Failure string `json:"failure,omitempty"`
	// FailureSummary gives the concrete cause when the processing log says
	// more than Failure; empty for downloads from before garner kept it.
	// Failure keeps SABnzbd's original report.
	FailureSummary string `json:"failure_summary,omitempty"`
	// Retryable means another release may succeed where this one failed.
	Retryable bool `json:"retryable,omitempty"`
	// Stopped says why garner tried no other release after this one
	// failed; empty when it tried one or did not have to.
	Stopped string    `json:"stopped,omitempty"`
	Stages  []Stage   `json:"stages"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	// Acknowledged is when the user cleared this attempt's warning. The
	// failure, its original timestamps and its retry eligibility stay put.
	Acknowledged time.Time `json:"acknowledged,omitzero"`
	Import       Import    `json:"import"`
}

// Stage is one of SABnzbd's post-processing steps and what happened in it.
type Stage struct {
	Name    string   `json:"name"`
	Actions []string `json:"actions"`
}

func (d Download) Active() bool {
	switch d.State {
	case Submitting, Queued, Downloading, Processing:
		return true
	}
	return false
}

// EpisodeState is the state the download alone puts its episode in, as the
// library names it: downloading, processing or importing while it runs,
// attention when Jellyfin did not confirm its files, failed when it or its
// import failed and imported when its import is done. It is empty for a
// cancelled download. The library also weighs the episode's file and
// searches: a file in the library outranks a failure, and a newer search
// replaces it.
func (d Download) EpisodeState() EpisodeState {
	switch {
	case d.State == Processing:
		return EpisodeProcessing
	case d.Active():
		return EpisodeDownloading
	case d.State == Completed && (d.Import.State == "" || d.Import.State == ImportPending ||
		d.Import.State == ImportMoving || d.Import.State == ImportScanning):
		return EpisodeImporting
	case d.State == Completed && d.Import.State == ImportUnverified:
		return EpisodeAttention
	case d.State == Failed || d.Import.State == ImportFailed || d.Import.State == ImportRejected:
		return EpisodeFailed
	case d.Import.State == ImportDone:
		return EpisodeImported
	}
	return ""
}

// CanAcknowledge reports whether this is an uncleared download failure,
// not an import problem that still needs fixing. The server also checks
// that this attempt is still the episode's latest.
func (d Download) CanAcknowledge() bool {
	return d.State == Failed && d.Acknowledged.IsZero() && (d.Import.State == "" || d.Import.State == ImportRejected)
}

// ActivityItem is an episode's latest download with its show and its
// place in the show's default order; Season and Number are 0 when the order
// does not place it.
type ActivityItem struct {
	Download Download `json:"download"`
	// Rejected lists, newest first, the episode's earlier attempts in the
	// same run as Download that the file check rejected. A run is a chain
	// of attempts, each started by garner to replace the failed one before
	// it; an attempt garner did not replace ends it.
	Rejected      []Download `json:"rejected,omitempty"`
	SeriesID      int64      `json:"series_id"`
	SeriesName    string     `json:"series_name"`
	SeriesNameEng string     `json:"series_name_eng,omitempty"`
	Season        int        `json:"season"`
	Number        int        `json:"number"`
}

// Rejections counts the downloads the file check rejected. The counts
// cover current shows only: a show's downloads are deleted with it.
type Rejections struct {
	// Total counts the rejected downloads and LastWeek those rejected in
	// the last 7 days. Oldest is when the oldest of them was rejected;
	// zero when there is none.
	Total    int       `json:"total"`
	LastWeek int       `json:"last_week"`
	Oldest   time.Time `json:"oldest,omitzero"`
	// Kinds counts the rejected downloads by kind of problem, as
	// Download.RejectedFor names them: one rejected for problems of two
	// kinds counts under both. ProblemUnclassified counts those whose
	// problems were stored before garner kept kinds.
	Kinds map[ProblemKind]int `json:"kinds"`
	// Episodes counts the episodes with a rejected download. Of those,
	// Imported have a file garner imported from a download started after
	// their last rejected one, and Missing have no file. An episode that
	// kept an earlier file counts as neither.
	Episodes int `json:"episodes"`
	Imported int `json:"imported"`
	Missing  int `json:"missing"`
}

// ImportState is where the import of a finished download is; empty
// before it starts.
type ImportState string

// Import states.
const (
	ImportPending    ImportState = "pending"    // waiting for the file check
	ImportMoving     ImportState = "moving"     // checked; files are being moved
	ImportScanning   ImportState = "scanning"   // moved; waiting for Jellyfin to list them
	ImportDone       ImportState = "imported"   // Jellyfin identified every file as intended
	ImportUnverified ImportState = "unverified" // moved, but Jellyfin did not confirm the episodes
	ImportRejected   ImportState = "rejected"   // the file check found the release bad
	ImportFailed     ImportState = "failed"     // a path or permission problem; see the recovery step
)

// Import is the move of a finished download into the library.
type Import struct {
	State  ImportState `json:"state,omitempty"`
	Detail string      `json:"detail,omitempty"`
	// Recovery says what to do when the import failed.
	Recovery string       `json:"recovery,omitempty"`
	Files    []ImportFile `json:"files"`
	// Notes are worth showing, such as subtitle files moved or files left
	// in SABnzbd's folder.
	Notes   []string  `json:"notes"`
	Updated time.Time `json:"updated,omitzero"`
}

// Planned reports whether the file check gave every file its place in the
// library; a failure after that happened while moving.
func (im Import) Planned() bool {
	return len(im.Files) > 0 && !slices.ContainsFunc(im.Files, func(f ImportFile) bool { return f.Target == "" })
}

// ImportFile is one video file of a download.
type ImportFile struct {
	// Source and Target are as garner reaches them.
	Source   string  `json:"source"`
	Target   string  `json:"target,omitempty"`
	Episodes []int64 `json:"episodes"`
	Size     int64   `json:"size"`
	// Check is the file check's outcome; nil before the check.
	Check *FileCheck `json:"check,omitempty"`
	// Jellyfin is how Jellyfin identified the file.
	Jellyfin string `json:"jellyfin,omitempty"`
	Verified bool   `json:"verified,omitempty"`
}

// FileCheck is the outcome of comparing a file with its release.
type FileCheck struct {
	// Problems mean the release is not what it claimed; it is bad.
	Problems []string `json:"problems,omitempty"`
	// Kinds holds the kind of each problem, in the order of Problems. It
	// is empty for checks stored before garner kept kinds; Kind reads it.
	Kinds []ProblemKind `json:"kinds,omitempty"`
	// Notes are evidence worth showing.
	Notes      []string `json:"notes,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	Audio      []string `json:"audio"`
	Subtitles  []string `json:"subtitles"`
	Duration   string   `json:"duration,omitempty"`
}

// AddProblem records a problem with its kind. Problems of a check stored
// before garner kept kinds stay unclassified.
func (c *FileCheck) AddProblem(kind ProblemKind, problem string) {
	for len(c.Kinds) < len(c.Problems) {
		c.Kinds = append(c.Kinds, ProblemUnclassified)
	}
	c.Problems = append(c.Problems, problem)
	c.Kinds = append(c.Kinds, kind)
}

// Kind returns the kind of the i-th problem: ProblemUnclassified for a
// check stored before garner kept kinds.
func (c FileCheck) Kind(i int) ProblemKind {
	if i < 0 || i >= len(c.Kinds) || c.Kinds[i] == "" {
		return ProblemUnclassified
	}
	return c.Kinds[i]
}

// ProblemKind is what kind of problem made the file check reject a
// download. Kinds stay the same when the sentences change, so clients can
// group and count problems without reading them.
type ProblemKind string

// Kinds of problem.
const (
	ProblemNoVideoFile   ProblemKind = "no_video_file"  // the download holds no video file
	ProblemWrongEpisode  ProblemKind = "wrong_episode"  // no file of the download holds the episode it is for
	ProblemUnreadable    ProblemKind = "unreadable"     // FFprobe could not read the file
	ProblemNoVideoTrack  ProblemKind = "no_video_track" // the file has no video track
	ProblemResolution    ProblemKind = "resolution"     // below 720p, below what the release claimed, or 2160p unasked
	ProblemNoAudioTrack  ProblemKind = "no_audio_track" // the file has no audio track
	ProblemAudioLanguage ProblemKind = "audio_language" // no audio track in a language the release promised
	ProblemLength        ProblemKind = "length"         // far longer or shorter than the episodes run
	// ProblemUnclassified stands for problems stored before garner kept
	// kinds.
	ProblemUnclassified ProblemKind = "unclassified"
)

// RejectedFor lists the kinds of problem the file check rejected the
// download for, each once, in the order found; nil unless its import was
// rejected. A download without video files, or whose files hold none of
// its episode, has that kind whenever it was rejected; problems from
// before garner kept kinds are ProblemUnclassified.
func (d Download) RejectedFor() []ProblemKind {
	if d.Import.State != ImportRejected {
		return nil
	}
	files := d.Import.Files
	if len(files) == 0 {
		return []ProblemKind{ProblemNoVideoFile}
	}
	if !slices.ContainsFunc(files, func(f ImportFile) bool { return slices.Contains(f.Episodes, d.EpisodeID) }) {
		return []ProblemKind{ProblemWrongEpisode}
	}
	var kinds []ProblemKind
	for _, f := range files {
		if f.Check == nil {
			continue
		}
		for i := range f.Check.Problems {
			if k := f.Check.Kind(i); !slices.Contains(kinds, k) {
				kinds = append(kinds, k)
			}
		}
	}
	if len(kinds) == 0 {
		// Before garner kept kinds, an unreadable file had no check.
		return []ProblemKind{ProblemUnclassified}
	}
	return kinds
}

// ResolutionClass names a video's resolution as releases do, such as 1080
// for 1920x800: wide films are cropped in height.
func ResolutionClass(w, h int) int {
	switch {
	case w >= 3200 || h >= 1800:
		return 2160
	case w >= 1800 || h >= 1000:
		return 1080
	case w >= 1200 || h >= 700:
		return 720
	}
	return h
}

// EpisodeDetail is what garner has of one episode: TVDB's description, its
// file in the library, if any, and every download, newest first.
type EpisodeDetail struct {
	Overview         string       `json:"overview,omitempty"`
	OverviewLanguage string       `json:"overview_language,omitempty"`
	File             *EpisodeFile `json:"file"`
	Downloads        []Download   `json:"downloads"`
}

// EpisodeFile is an episode's file, as garner reaches it.
type EpisodeFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}
