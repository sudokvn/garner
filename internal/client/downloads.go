package client

import (
	"context"
	"net/http"
	"strconv"

	"github.com/sudokvn/garner/internal/api"
)

// DownloadCandidate downloads a release of a search, whatever its verdict.
func (c *Client) DownloadCandidate(ctx context.Context, candidateID int64) (api.Download, error) {
	var d api.Download
	return d, c.do(ctx, http.MethodPost, "/api/v1/candidates/"+strconv.FormatInt(candidateID, 10)+"/download", nil, &d)
}

// Episode returns an episode's file and downloads.
func (c *Client) Episode(ctx context.Context, episodeID int64) (api.EpisodeDetail, error) {
	var e api.EpisodeDetail
	return e, c.do(ctx, http.MethodGet, "/api/v1/episodes/"+strconv.FormatInt(episodeID, 10), nil, &e)
}

// CancelDownload stops a queued or downloading release.
func (c *Client) CancelDownload(ctx context.Context, downloadID int64) (api.Download, error) {
	var d api.Download
	return d, c.do(ctx, http.MethodPost, "/api/v1/downloads/"+strconv.FormatInt(downloadID, 10)+"/cancel", nil, &d)
}

// AcknowledgeDownload clears a failed attempt's warning without retrying it.
func (c *Client) AcknowledgeDownload(ctx context.Context, downloadID int64) (api.Download, error) {
	var d api.Download
	return d, c.do(ctx, http.MethodPost, "/api/v1/downloads/"+strconv.FormatInt(downloadID, 10)+"/acknowledge", nil, &d)
}

// DownloadSeasonCandidate downloads a pack of a season search.
func (c *Client) DownloadSeasonCandidate(ctx context.Context, candidateID int64) (api.Download, error) {
	var d api.Download
	return d, c.do(ctx, http.MethodPost, "/api/v1/season-candidates/"+strconv.FormatInt(candidateID, 10)+"/download", nil, &d)
}

// Activity lists what runs, what waits for the user, and what ended within
// the last day.
func (c *Client) Activity(ctx context.Context) ([]api.ActivityItem, error) {
	var items []api.ActivityItem
	return items, c.do(ctx, http.MethodGet, "/api/v1/activity", nil, &items)
}
