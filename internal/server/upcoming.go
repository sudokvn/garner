package server

import (
	"net/http"
	"time"

	"github.com/sudokvn/garner/internal/api"
)

// handleUpcoming lists the episodes of every show that aired lately and
// are missing, or air soon, with when the monitor searches each.
func (s *Server) handleUpcoming(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	episodes, err := s.store.Upcoming(r.Context(), now, s.schedule())
	if err != nil {
		s.internalError(w, "list upcoming episodes", err)
		return
	}
	writeJSON(w, http.StatusOK, api.Upcoming{Today: now.Format(time.DateOnly), Episodes: episodes, MonitorOff: s.monitorOff()})
}
