package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/trucke/garner/internal/api"
)

// Library lists the shows with a summary of each.
func (c *Client) Library(ctx context.Context) ([]api.Show, error) {
	var shows []api.Show
	return shows, c.do(ctx, http.MethodGet, "/api/v1/series", nil, &shows)
}

// FindShows finds shows on TVDB. q may also be a TVDB ID or a thetvdb.com
// link.
func (c *Client) FindShows(ctx context.Context, q string) (api.FoundShows, error) {
	var found api.FoundShows
	return found, c.do(ctx, http.MethodGet, "/api/v1/search?"+url.Values{"q": {q}}.Encode(), nil, &found)
}

// Preview describes a show on TVDB before it is added.
func (c *Client) Preview(ctx context.Context, id int64) (api.Preview, error) {
	var p api.Preview
	return p, c.do(ctx, http.MethodGet, "/api/v1/search/"+strconv.FormatInt(id, 10), nil, &p)
}

// Add loads a show into the library and takes over its files there;
// monitor says which of its episodes the monitor gets. It takes as long as
// loading the show's episodes from TVDB and TheXEM does, usually a few
// seconds, plus checking the files found, which can take minutes.
func (c *Client) Add(ctx context.Context, id int64, monitor string) (api.SeriesDetail, error) {
	var d api.SeriesDetail
	return d, c.do(ctx, http.MethodPost, "/api/v1/series", map[string]any{"tvdb_id": id, "monitor": monitor}, &d)
}

// Series returns a show with what garner has of each episode.
func (c *Client) Series(ctx context.Context, id int64) (api.SeriesDetail, error) {
	var d api.SeriesDetail
	return d, c.do(ctx, http.MethodGet, "/api/v1/series/"+strconv.FormatInt(id, 10), nil, &d)
}

// SetProfile gives a show another profile. Its earlier search results are
// cleared.
func (c *Client) SetProfile(ctx context.Context, id, profileID int64) (api.SeriesDetail, error) {
	var d api.SeriesDetail
	return d, c.do(ctx, http.MethodPatch, "/api/v1/series/"+strconv.FormatInt(id, 10), map[string]int64{"profile_id": profileID}, &d)
}

// SetType makes a show a TV show or an anime, api.TypeTV or api.TypeAnime:
// how garner reads episode numbers and which library folder takes new
// files. Files already in the library stay where they are.
func (c *Client) SetType(ctx context.Context, id int64, typ string) (api.SeriesDetail, error) {
	var d api.SeriesDetail
	return d, c.do(ctx, http.MethodPatch, "/api/v1/series/"+strconv.FormatInt(id, 10), map[string]string{"type": typ}, &d)
}

// SetMonitor changes which of a show's episodes the monitor gets.
func (c *Client) SetMonitor(ctx context.Context, id int64, mode string) (api.SeriesDetail, error) {
	var d api.SeriesDetail
	return d, c.do(ctx, http.MethodPatch, "/api/v1/series/"+strconv.FormatInt(id, 10), map[string]string{"monitor": mode}, &d)
}

// Refresh reloads a show from TVDB and TheXEM.
func (c *Client) Refresh(ctx context.Context, id int64) (api.SeriesDetail, error) {
	var d api.SeriesDetail
	return d, c.do(ctx, http.MethodPost, "/api/v1/series/"+strconv.FormatInt(id, 10)+"/refresh", nil, &d)
}

// DeleteSeries removes a show and cancels its downloads. Its files are
// deleted unless keepFiles is set.
func (c *Client) DeleteSeries(ctx context.Context, id int64, keepFiles bool) (api.Deleted, error) {
	files := "delete"
	if keepFiles {
		files = "keep"
	}
	var d api.Deleted
	return d, c.do(ctx, http.MethodDelete, "/api/v1/series/"+strconv.FormatInt(id, 10)+"?files="+files, nil, &d)
}
