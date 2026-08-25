package httpapi

import (
	"net/http"

	"task262-freeoverlap/internal/model"
)

// --- 批次处理 ---

type createBatchRequest struct {
	Name        string  `json:"name"`
	Method      string  `json:"method"`
	Temperature float64 `json:"temperature"`
}

func (s *Server) handleCreateBatch(w http.ResponseWriter, r *http.Request) {
	var req createBatchRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.svc.CreateBatch(req.Name, req.Method, req.Temperature)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (s *Server) handleListBatches(w http.ResponseWriter, r *http.Request) {
	batches, err := s.svc.ListBatches()
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": batches})
}

func (s *Server) handleGetBatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, err := s.svc.GetBatch(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

type advanceRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleAdvanceBatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req advanceRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	status, ok := model.ValidateBatchStatus(req.Status)
	if !ok {
		writeErr(w, model.E(model.ErrInvalid, "invalid batch status %q", req.Status))
		return
	}
	b, err := s.svc.AdvanceBatch(id, status)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) handleRunDiagnosis(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	report, err := s.svc.RunDiagnosis(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
