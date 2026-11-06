package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/sudokvn/garner/internal/api"
	"github.com/sudokvn/garner/internal/profile"
	"github.com/sudokvn/garner/internal/release"
	"github.com/sudokvn/garner/internal/selection"
	"github.com/sudokvn/garner/internal/store"
)

// profileCache holds the stored profiles parsed, so applying one needs no
// database query: the store asks for a show's upgrade rule while it reads
// rows. Every change to a profile goes through the server, which reloads
// the cache after it.
type profileCache struct {
	mu       sync.RWMutex
	byID     map[int64]profile.Profile
	order    []int64          // by name, as the store lists them
	defaults map[string]int64 // type → profile ID
}

// loadProfiles reads the stored profiles into the cache. garner saves only
// profiles that pass the check, so one that does not is a bug.
func (s *Server) loadProfiles(ctx context.Context) error {
	stored, err := s.store.Profiles(ctx)
	if err != nil {
		return err
	}
	byID, defaults := map[int64]profile.Profile{}, map[string]int64{}
	order := make([]int64, 0, len(stored))
	for _, sp := range stored {
		p, _, err := profile.Parse(sp.Text)
		if err != nil {
			return fmt.Errorf("profile %q in the database: %w", sp.Name, err)
		}
		byID[sp.ID] = p
		order = append(order, sp.ID)
		if sp.DefaultTV {
			defaults[api.TypeTV] = sp.ID
		}
		if sp.DefaultAnime {
			defaults[api.TypeAnime] = sp.ID
		}
	}
	for _, typ := range []string{api.TypeTV, api.TypeAnime} {
		if _, ok := defaults[typ]; !ok {
			return fmt.Errorf("the database has no default profile for new %s shows", typ)
		}
	}
	s.profiles.mu.Lock()
	defer s.profiles.mu.Unlock()
	s.profiles.byID, s.profiles.order, s.profiles.defaults = byID, order, defaults
	return nil
}

// storedProfile returns a show's profile. A show loaded before it had one,
// and not refreshed since, gets the default for its type.
func (s *Server) storedProfile(sr store.Series) profile.Profile {
	s.profiles.mu.RLock()
	defer s.profiles.mu.RUnlock()
	if p, ok := s.profiles.byID[sr.ProfileID]; ok {
		return p
	}
	return s.profiles.byID[s.profiles.defaults[cmp.Or(sr.Type, api.TypeTV)]]
}

// profileFor applies a show's profile to the show.
func (s *Server) profileFor(sr store.Series) selection.Profile {
	return selection.FromProfile(s.storedProfile(sr), selection.OriginalLanguage(sr.OriginalLanguage))
}

// defaultProfileFor applies the profile new shows of a type get to a show
// whose original language is original.
func (s *Server) defaultProfileFor(typ string, original release.Language) selection.Profile {
	return selection.FromProfile(s.storedProfile(store.Series{Type: typ}), original)
}

// profileInfos lists every profile as applied to a show, for its profile
// choice.
func (s *Server) profileInfos(sr store.Series) []api.ProfileInfo {
	s.profiles.mu.RLock()
	defer s.profiles.mu.RUnlock()
	original := selection.OriginalLanguage(sr.OriginalLanguage)
	out := make([]api.ProfileInfo, 0, len(s.profiles.order))
	for _, id := range s.profiles.order {
		p := s.profiles.byID[id]
		applied := selection.FromProfile(p, original)
		out = append(out, api.ProfileInfo{ID: id, Name: p.Name, Rules: applied.Describe(), Preferences: applied.Preferences()})
	}
	return out
}

func (s *Server) profileRoutes(mux router) {
	mux.Handle("GET /api/v1/profiles", http.HandlerFunc(s.handleListProfiles))
	mux.Handle("GET /api/v1/profiles/{id}", http.HandlerFunc(s.handleGetProfile))
	mux.Handle("POST /api/v1/profiles", http.HandlerFunc(s.handleCreateProfile))
	mux.Handle("PUT /api/v1/profiles/{id}", http.HandlerFunc(s.handleReplaceProfile))
	mux.Handle("DELETE /api/v1/profiles/{id}", http.HandlerFunc(s.handleDeleteProfile))
	mux.Handle("PUT /api/v1/profiles/{id}/default", http.HandlerFunc(s.handleDefaultProfile))
}

// apiProfile is a stored profile as clients see it, with the shows that
// use it.
func (s *Server) apiProfile(ctx context.Context, p store.StoredProfile) (api.Profile, error) {
	shows, err := s.store.ProfileShowNames(ctx, p.ID)
	if err != nil {
		return api.Profile{}, err
	}
	return api.Profile{ID: p.ID, Name: p.Name, Text: p.Text, DefaultTV: p.DefaultTV, DefaultAnime: p.DefaultAnime, Shows: shows}, nil
}

func (s *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	stored, err := s.store.Profiles(r.Context())
	if err != nil {
		s.internalError(w, "list profiles", err)
		return
	}
	out := make([]api.Profile, 0, len(stored))
	for _, p := range stored {
		ap, err := s.apiProfile(r.Context(), p)
		if err != nil {
			s.internalError(w, "list profiles", err)
			return
		}
		out = append(out, ap)
	}
	writeJSON(w, http.StatusOK, out)
}

func profileID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid profile ID")
		return 0, false
	}
	return id, true
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	s.writeProfile(w, r, id, http.StatusOK)
}

func (s *Server) writeProfile(w http.ResponseWriter, r *http.Request, id int64, status int) {
	p, err := s.store.Profile(r.Context(), id)
	if errors.Is(err, store.ErrNoProfile) {
		writeError(w, http.StatusNotFound, "no such profile")
		return
	}
	if err != nil {
		s.internalError(w, "load profile", err)
		return
	}
	ap, err := s.apiProfile(r.Context(), p)
	if err != nil {
		s.internalError(w, "load profile", err)
		return
	}
	writeJSON(w, status, ap)
}

// profileText reads a request that carries a profile's text and checks
// it. On a problem it answers and returns false.
func profileText(w http.ResponseWriter, r *http.Request) (profile.Profile, bool) {
	var req struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, `expected {"text": a profile}`)
		return profile.Profile{}, false
	}
	p, _, err := profile.Parse(req.Text)
	var pe *profile.Error
	if errors.As(err, &pe) {
		writeJSON(w, http.StatusBadRequest, api.ErrorBody{Error: "the profile has problems", Problems: pe.Problems})
		return profile.Profile{}, false
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return profile.Profile{}, false
	}
	return p, true
}

func (s *Server) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	p, ok := profileText(w, r)
	if !ok {
		return
	}
	id, err := s.saveProfile(r.Context(), 0, p)
	if !s.profileSaved(w, err) {
		return
	}
	s.writeProfile(w, r, id, http.StatusCreated)
}

func (s *Server) handleReplaceProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	p, ok := profileText(w, r)
	if !ok {
		return
	}
	_, err := s.saveProfile(r.Context(), id, p)
	if !s.profileSaved(w, err) {
		return
	}
	s.writeProfile(w, r, id, http.StatusOK)
}

// saveProfile stores a checked profile in its canonical form: as a new
// one for id 0, or in place of profile id. garner then judges by it at
// once, and the clients learn of it.
func (s *Server) saveProfile(ctx context.Context, id int64, p profile.Profile) (int64, error) {
	text := profile.Encode(p)
	var err error
	if id == 0 {
		id, err = s.store.CreateProfile(ctx, p.Name, text)
	} else {
		err = s.store.UpdateProfile(ctx, id, p.Name, text)
	}
	if err != nil {
		return 0, err
	}
	s.profilesChanged(ctx)
	return id, nil
}

// profilesChanged reloads the profiles garner judges by and tells the
// clients. A changed profile drops the search results of its shows, so the
// monitor may have work. The change is saved, so the cache follows it even
// when the client stopped waiting.
func (s *Server) profilesChanged(ctx context.Context) {
	if err := s.loadProfiles(context.WithoutCancel(ctx)); err != nil {
		s.log.Error("reload profiles", "err", err)
	}
	s.events.publish(Event{Type: api.EventProfilesUpdated})
	s.wakeMonitor()
}

// profileSaved answers a failed save and reports whether it succeeded.
func (s *Server) profileSaved(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNameTaken):
		writeError(w, http.StatusConflict, "another profile has that name")
	case errors.Is(err, store.ErrNoProfile):
		writeError(w, http.StatusNotFound, "no such profile")
	default:
		s.internalError(w, "save profile", err)
	}
	return false
}

func (s *Server) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	err := s.store.DeleteProfile(r.Context(), id)
	var inUse *store.ProfileInUseError
	switch {
	case errors.As(err, &inUse):
		writeError(w, http.StatusConflict, inUse.Error())
		return
	case errors.Is(err, store.ErrNoProfile):
		writeError(w, http.StatusNotFound, "no such profile")
		return
	case err != nil:
		s.internalError(w, "delete profile", err)
		return
	}
	s.profilesChanged(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// handleDefaultProfile makes a profile the one new shows of the types get.
func (s *Server) handleDefaultProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileID(w, r)
	if !ok {
		return
	}
	var req struct {
		Types []string `json:"types"`
	}
	err := readJSON(r, &req)
	if err != nil || len(req.Types) == 0 || slices.ContainsFunc(req.Types, func(t string) bool { return t != store.TypeTV && t != store.TypeAnime }) {
		writeError(w, http.StatusBadRequest, `expected {"types": ["tv"], ["anime"] or ["tv", "anime"]}`)
		return
	}
	// The types change together, so a failure leaves the defaults and the
	// cache as they were.
	if !s.profileSaved(w, s.store.SetDefaultProfile(r.Context(), id, req.Types...)) {
		return
	}
	s.profilesChanged(r.Context())
	s.writeProfile(w, r, id, http.StatusOK)
}
