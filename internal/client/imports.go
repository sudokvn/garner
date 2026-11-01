package client

import (
	"context"
	"net/http"
	"strconv"

	"github.com/trucke/garner/internal/api"
)

// TakeOver looks for a show's files in the library and takes over those of
// episodes without a file.
func (c *Client) TakeOver(ctx context.Context, id int64) (api.TakeOver, error) {
	var t api.TakeOver
	return t, c.do(ctx, http.MethodPost, "/api/v1/series/"+strconv.FormatInt(id, 10)+"/files/take-over", nil, &t)
}

// Rename moves a show's taken-over files into garner's folder for it,
// named the way garner names files.
func (c *Client) Rename(ctx context.Context, id int64) (api.Renamed, error) {
	var r api.Renamed
	return r, c.do(ctx, http.MethodPost, "/api/v1/series/"+strconv.FormatInt(id, 10)+"/files/rename", nil, &r)
}

// RetryImport repeats a failed or unconfirmed import.
func (c *Client) RetryImport(ctx context.Context, downloadID int64) (api.Download, error) {
	var d api.Download
	return d, c.do(ctx, http.MethodPost, "/api/v1/downloads/"+strconv.FormatInt(downloadID, 10)+"/import", nil, &d)
}
