package server

import (
	"net/http"
)

func (s *Server) handleSubdomain(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ddns.Snapshot())
}
func (s *Server) handleSubdomainUpdate(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	status, err := s.ddns.Configure(r.Context(), input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.audit(r, "subdomain.update", input.Name, "")
	writeJSON(w, http.StatusAccepted, status)
}
