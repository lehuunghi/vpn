package server

import (
	"net/http"

	"github.com/lehuunghi/vpn/internal/vpsrelay"
)

func (s *Server) handleRelay(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.eng.Relay().Snapshot())
}

func (s *Server) handleRelayUpdate(w http.ResponseWriter, r *http.Request) {
	var input vpsrelay.Input
	if !readJSON(w, r, &input) {
		return
	}
	status, err := s.eng.Relay().Configure(r.Context(), input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	action := "relay.disabled"
	if status.Enabled {
		action = "relay.enabled"
	}
	s.audit(r, action, status.Domain, "")
	s.eng.Hub().Publish("settings", s.eng.Settings())
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, status)
}
