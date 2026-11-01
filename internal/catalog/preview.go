package catalog

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/text/language"

	"github.com/trucke/garner/internal/api"
	"github.com/trucke/garner/internal/release"
)

// Preview describes a show on TVDB without adding it.
func (c *Catalog) Preview(ctx context.Context, id int64) (api.Preview, error) {
	p, err := c.describe(ctx, id)
	if err != nil {
		return api.Preview{}, err
	}
	p.Added, err = c.store.HasSeries(ctx, id)
	if err != nil {
		return api.Preview{}, err
	}
	list, _, err := c.tvdb.episodes(ctx, id, p.order, "")
	if err != nil {
		return api.Preview{}, err
	}
	seasons := map[int]bool{}
	for _, e := range list {
		if e.SeasonNumber > 0 {
			p.Episodes++
			seasons[e.SeasonNumber] = true
		}
	}
	p.Seasons = len(seasons)
	return p.Preview, nil
}

type described struct {
	api.Preview
	order string // the default episode order
}

// describe loads a show's TVDB record and its English and German names.
func (c *Catalog) describe(ctx context.Context, id int64) (described, error) {
	s, err := c.tvdb.series(ctx, id)
	if errors.Is(err, errNotFound) {
		return described{}, fmt.Errorf("series %d: %w", id, ErrUnknownSeries)
	}
	if err != nil {
		return described{}, err
	}
	year, _ := strconv.Atoi(s.Year)
	d := described{order: "official", Preview: api.Preview{
		FoundShow: api.FoundShow{ID: s.ID, Slug: s.Slug, Name: s.Name, Year: year, Status: s.Status.Name, Language: s.OriginalLanguage},
		Type:      typeFor(s), LanguageName: LanguageName(s.OriginalLanguage),
		FirstAired: s.FirstAired, LastAired: s.LastAired, Runtime: s.AverageRuntime, Orders: []string{},
		URL: SeriesURL(s.Slug, s.ID),
	}}
	if s.OriginalNetwork != nil {
		d.Network = s.OriginalNetwork.Name
	}
	for _, st := range s.SeasonTypes {
		name := st.Name
		if st.AlternateName != nil && *st.AlternateName != "" {
			name += " (" + *st.AlternateName + ")"
		}
		if st.ID == s.DefaultSeason {
			d.order = st.Type
			d.Orders = append([]string{name}, d.Orders...)
		} else {
			d.Orders = append(d.Orders, name)
		}
	}
	overview, overviewLang := "", ""
	for _, lang := range languages {
		tr, ok, err := c.tvdb.translation(ctx, id, lang)
		if err != nil {
			return d, err
		}
		if !ok {
			continue
		}
		if lang == "eng" {
			d.NameEng = tr.Name
		} else {
			d.NameDeu = tr.Name
		}
		if overview == "" && tr.Overview != "" {
			overview, overviewLang = tr.Overview, lang
		}
	}
	d.Overview = cmp.Or(overview, s.Overview)
	if d.Overview != "" {
		d.OverviewLanguage = cmp.Or(overviewLang, s.OriginalLanguage)
	}
	return d, nil
}

// LanguageName names a TVDB language code in English, such as Japanese
// for jpn. Languages garner has no name for keep a two-letter code.
func LanguageName(code string) string {
	if l, ok := release.LanguageNamed(code); ok {
		return l.Name()
	}
	if b, err := language.ParseBase(code); err == nil {
		return strings.ToUpper(b.String())
	}
	return code
}

// typeFor is the type a new show gets: anime for TVDB's Anime genre.
func typeFor(s tvdbSeries) string {
	for _, g := range s.Genres {
		if g.Name == "Anime" {
			return api.TypeAnime
		}
	}
	return api.TypeTV
}

// SeriesURL is a show's page on thetvdb.com, by ID when TVDB has no slug.
func SeriesURL(slug string, id int64) string {
	if slug == "" {
		return "https://thetvdb.com/dereferrer/series/" + strconv.FormatInt(id, 10)
	}
	return "https://thetvdb.com/series/" + url.PathEscape(slug)
}
