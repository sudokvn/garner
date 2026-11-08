package api

import "time"

// Status is what the server reports about itself and the token in use.
type Status struct {
	Version string `json:"version"`
	// SourceURL identifies this server build's corresponding-source archive.
	// Older servers and builds without a source offer leave it empty.
	SourceURL string `json:"source_url,omitempty"`
	Token     string `json:"token"`
	// TokenCreated is unset when the token's record was not found.
	TokenCreated time.Time `json:"token_created,omitzero"`
	Started      time.Time `json:"started"`
}

// ErrorBody is the body of every error answer.
type ErrorBody struct {
	Error string `json:"error"`
	// Problems lists what is wrong with a profile, one problem each.
	Problems []string `json:"problems,omitempty"`
}

// Services is how the services the server uses are doing, and what the
// library holds.
type Services struct {
	Services []ServiceCheck `json:"services"`
	Library  LibraryTotals  `json:"library"`
}

type ServiceCheck struct {
	Name string `json:"name"`
	// OK is false when the service is not set up or did not answer.
	OK bool `json:"ok"`
	// Detail is one line: what the service reports, or what went wrong.
	Detail string `json:"detail"`
	// At dates the detail, such as TVDB's last catalogue sync.
	At *time.Time `json:"at,omitempty"`
}

// LibraryTotals sums up the library: shows by type, aired episodes in
// it, their size, and the library folders as the server sees them.
type LibraryTotals struct {
	Shows    int            `json:"shows"`
	Types    map[string]int `json:"types"`
	Imported int            `json:"imported"`
	Aired    int            `json:"aired"`
	Bytes    int64          `json:"bytes"`
	Folders  []string       `json:"folders"`
	// Monitored counts the shows the monitor gets episodes of; MonitorOff
	// says why it does not run, empty when it does.
	Monitored  int    `json:"monitored"`
	MonitorOff string `json:"monitor_off,omitempty"`
}
