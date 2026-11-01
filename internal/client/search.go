package client

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/trucke/garner/internal/api"
)

// SearchShow asks the server to search every missing episode of a show
// and download the best release of each that meets its rules. It returns
// how many episodes are missing; series.search events follow the search.
func (c *Client) SearchShow(ctx context.Context, id int64) (int, error) {
	var out api.ShowSearch
	err := c.do(ctx, http.MethodPost, "/api/v1/series/"+strconv.FormatInt(id, 10)+"/search", nil, &out)
	return out.Episodes, err
}

func (c *Client) SearchEpisode(ctx context.Context, episodeID int64) (api.Search, error) {
	var s api.Search
	return s, c.do(ctx, http.MethodPost, "/api/v1/episodes/"+strconv.FormatInt(episodeID, 10)+"/search", nil, &s)
}

// EpisodeSearch returns an episode's latest stored search.
func (c *Client) EpisodeSearch(ctx context.Context, episodeID int64) (api.Search, error) {
	var s api.Search
	return s, c.do(ctx, http.MethodGet, "/api/v1/episodes/"+strconv.FormatInt(episodeID, 10)+"/search", nil, &s)
}

// SearchSeason searches a whole season for packs.
func (c *Client) SearchSeason(ctx context.Context, seriesID int64, season int) (api.Search, error) {
	var s api.Search
	return s, c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/series/%d/seasons/%d/search", seriesID, season), nil, &s)
}
