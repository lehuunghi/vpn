package server

import "net/http"

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.updater.Status())
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	status, err := s.updater.Check(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "Không kiểm tra được phiên bản mới: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleUpdateStart(w http.ResponseWriter, r *http.Request) {
	job, err := s.updater.Start()
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.audit(r, "system.update", "", job.ID)
	writeJSON(w, http.StatusAccepted, job)
}
