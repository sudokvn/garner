package catalog

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/match"
)

// FindShows finds shows on TVDB and marks those already in the catalogue.
// The text may also be a TVDB ID or a thetvdb.com link.
//
// TVDB ranks by the main name, which is Japanese for most anime, so a
// search for an anime's English name could put it 19th, behind shows whose
// main names only look alike. Results whose name in any language contains
// every search word therefore come first; otherwise TVDB's order stays.
func (c *Catalog) FindShows(ctx context.Context, q string) (api.FoundShows, error) {
	ids, err := c.store.SeriesIDs(ctx)
	if err != nil {
		return api.FoundShows{}, err
	}
	added := make(map[int64]bool, len(ids))
	for _, id := range ids {
		added[id] = true
	}
	page := api.FoundShows{Results: []api.FoundShow{}}

	id, slug, link := tvdbRef(q)
	if slug != "" {
		id, err = c.tvdb.seriesIDBySlug(ctx, slug)
		if errors.Is(err, errNotFound) {
			return page, nil
		}
		if err != nil {
			return page, err
		}
	}
	if id > 0 {
		p, err := c.describe(ctx, id)
		switch {
		case err == nil:
			p.Added = added[id]
			page.Results = append(page.Results, p.FoundShow)
		case errors.Is(err, ErrUnknownSeries):
		case link:
			return page, err
		default:
			// A number may also be part of a title, such as "1983".
			c.log.Warn("TVDB ID lookup failed; searching the number as text", "id", id, "err", err)
		}
	}
	if link {
		page.Total = len(page.Results)
		return page, nil
	}

	results, total, err := c.tvdb.search(ctx, q)
	if err != nil && len(page.Results) > 0 {
		// The number found its show; the title search is a bonus.
		c.log.Warn("TVDB text search failed; showing the ID match only", "err", err)
		page.Total = len(page.Results)
		return page, nil
	}
	if err != nil {
		return page, err
	}
	words := match.Words(q)
	var found []api.FoundShow
	var hits []bool
	byID := len(page.Results) // the show the number named, if any
	skipped := 0
	for _, r := range results {
		rid, err := strconv.ParseInt(r.TVDBID, 10, 64)
		if err != nil || byID > 0 && rid == id {
			skipped++
			continue
		}
		year, _ := strconv.Atoi(r.Year)
		found = append(found, api.FoundShow{
			ID: rid, Slug: r.Slug, Name: r.Name, NameEng: r.Translations["eng"], NameDeu: r.Translations["deu"], Year: year,
			Status: r.Status, Language: r.PrimaryLanguage, Network: r.Network, Added: added[rid],
		})
		names := []string{r.Name}
		for _, n := range r.Translations {
			names = append(names, n)
		}
		hits = append(hits, containsAll(words, names))
	}
	order := make([]int, len(found))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case hits[a] == hits[b]:
			return 0
		case hits[a]:
			return -1
		}
		return 1
	})
	for _, i := range order {
		page.Results = append(page.Results, found[i])
	}
	// TVDB's total counts what was skipped, but not the show the number
	// named.
	page.Total = max(total-skipped+byID, len(page.Results))
	return page, nil
}

// containsAll reports whether one of names, compared as release names are,
// contains every word.
func containsAll(words, names []string) bool {
	if len(words) == 0 {
		return false
	}
	for _, n := range names {
		// Without spaces, so "belnabel" finds "Bel Na Bel".
		compact := strings.Join(match.Words(n), "")
		if compact != "" && !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(compact, w) }) {
			return true
		}
	}
	return false
}

// tvdbRef reads a TVDB ID or a thetvdb.com link. link is true for links,
// which name exactly one show; a bare number may also be part of a title.
func tvdbRef(q string) (id int64, slug string, link bool) {
	q = strings.TrimSpace(q)
	if n, err := strconv.ParseInt(q, 10, 64); err == nil && n > 0 {
		return n, "", false
	}
	if !strings.Contains(strings.ToLower(q), "thetvdb.com") {
		return 0, "", false
	}
	if !strings.Contains(q, "://") {
		q = "https://" + q
	}
	u, err := url.Parse(q)
	if err != nil {
		return 0, "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "dereferrer" && parts[1] == "series":
		// thetvdb.com/dereferrer/series/401234
		n, _ := strconv.ParseInt(parts[2], 10, 64)
		return n, "", n > 0
	case len(parts) >= 2 && parts[0] == "series" && parts[1] != "":
		// thetvdb.com/series/toro-no-minato, possibly followed by /seasons/…
		return 0, parts[1], true
	case u.Query().Get("tab") == "series":
		// The old thetvdb.com/?tab=series&id=401234
		n, _ := strconv.ParseInt(u.Query().Get("id"), 10, 64)
		return n, "", n > 0
	}
	return 0, "", false
}
