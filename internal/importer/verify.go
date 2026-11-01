package importer

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/trucke/garner/internal/store"
)

// verify checks that Jellyfin lists each file as the intended episode.
func (s *Service) verify(ctx context.Context, d *store.Download) {
	if s.jellyfin == nil {
		// Jellyfin was configured when the files were moved, but no longer is.
		s.save(ctx, d, store.Import{State: store.ImportDone, Detail: "moved into the library; Jellyfin is not configured, so nothing confirmed it", Files: d.Import.Files})
		return
	}
	c, err := s.context(ctx, d.EpisodeID)
	if err != nil {
		s.log.Warn("verify import", "download", d.ID, "err", err)
		return
	}
	files := slices.Clone(d.Import.Files)
	progress, done, listed := false, true, false
	for i := range files {
		f := &files[i]
		if f.Verified {
			continue
		}
		jfFile, _ := s.opts.JellyfinPaths.Remote(f.Target)
		jfShow, _ := s.opts.JellyfinPaths.Remote(filepath.Dir(filepath.Dir(f.Target)))
		ep, found, err := s.jellyfin.EpisodeAt(ctx, jfShow, jfFile)
		if err != nil {
			s.log.Warn("Jellyfin lookup failed", "download", d.ID, "err", err)
			done = false
			continue
		}
		if !found {
			done = false
			continue
		}
		listed = true
		if ep.TVDB == "" {
			// Jellyfin lists a file before its metadata provider has
			// identified it; wait for the TVDB ID.
			done = false
			continue
		}
		progress = true
		want := c.episodes[f.Episodes[0]]
		f.Jellyfin = fmt.Sprintf("S%02dE%02d %q, TVDB episode %s", ep.Season, ep.Number, ep.Name, cmp.Or(ep.TVDB, "none"))
		if ep.TVDB != strconv.FormatInt(want.ID, 10) || ep.SeriesTVDB != strconv.FormatInt(c.data.Series.ID, 10) {
			s.save(ctx, d, store.Import{State: store.ImportUnverified, Files: files,
				Detail: fmt.Sprintf("Jellyfin identified %s as %s (show TVDB %s), not %s (TVDB episode %d)",
					filepath.Base(f.Target), f.Jellyfin, cmp.Or(ep.SeriesTVDB, "none"), c.show.Label(want.ID), want.ID),
				Recovery: "Correct the match in Jellyfin (Identify on the show or episode), then retry the import so garner checks again."})
			return
		}
		f.Verified = true
	}
	switch {
	case done:
		labels := make([]string, len(files))
		for i, f := range files {
			labels[i] = f.Jellyfin
		}
		s.save(ctx, d, store.Import{State: store.ImportDone, Detail: "Jellyfin lists it as " + strings.Join(labels, "; "), Files: files})
	case progress:
		s.save(ctx, d, store.Import{State: store.ImportScanning, Detail: d.Import.Detail, Files: files})
	case !listed && d.Import.Detail == detailWaiting && s.Now().Sub(d.Import.Updated) > fullScanAfter:
		if err := s.jellyfin.RefreshLibrary(ctx); err != nil {
			s.log.Warn("could not ask Jellyfin to scan all libraries", "err", err)
			return
		}
		s.save(ctx, d, store.Import{State: store.ImportScanning, Detail: detailFullScan, Files: files})
	case s.Now().Sub(d.Import.Updated) > scanTimeout && listed:
		s.save(ctx, d, store.Import{State: store.ImportUnverified, Files: files,
			Detail:   fmt.Sprintf("Jellyfin lists the files but has not identified them through TVDB after %s", scanTimeout),
			Recovery: "Check that the TheTVDB plugin comes first for this library in Jellyfin, refresh the show's metadata, then retry the import."})
	case s.Now().Sub(d.Import.Updated) > scanTimeout:
		s.save(ctx, d, store.Import{State: store.ImportUnverified, Files: files,
			Detail:   fmt.Sprintf("the files are in the library, but Jellyfin has not listed them after %s", scanTimeout),
			Recovery: "Scan the library in Jellyfin, then retry the import so garner checks again."})
	}
}
