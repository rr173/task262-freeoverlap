package httpapi

import (
	"net/http"

	"task262-freeoverlap/internal/model"
)

// --- 快照 ---

type createSnapshotRequest struct {
	BatchID string `json:"batch_id"`
	Label   string `json:"label"`
}

func (s *Server) handleCreateSnapshot(w http.ResponseWriter, r *http.Request) {
	var req createSnapshotRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	sn, err := s.svc.CreateSnapshot(req.BatchID, req.Label)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sn)
}

func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	batchID := r.URL.Query().Get("batch_id")
	if batchID == "" {
		writeErr(w, model.E(model.ErrInvalid, "batch_id query required"))
		return
	}
	sns, err := s.svc.ListSnapshots(batchID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": sns})
}

func (s *Server) handlePublishSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sn, err := s.svc.PublishSnapshot(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sn)
}

func (s *Server) handleSupersedeSnapshot(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sn, err := s.svc.SupersedeSnapshot(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sn)
}

// --- 健康与自检 ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "task262-freeoverlap"})
}

func (s *Server) handleSmoke(w http.ResponseWriter, r *http.Request) {
	if err := smoke(s.svc); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "smoke": "passed"})
}
