package httpapi

import (
	"net/http"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/service"
)

// --- 窗口处理 ---

type createWindowRequest struct {
	BatchID     string  `json:"batch_id"`
	Label       string  `json:"label"`
	Center      float64 `json:"center"`
	SpringConst float64 `json:"spring_const"`
}

func (s *Server) handleCreateWindow(w http.ResponseWriter, r *http.Request) {
	var req createWindowRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	wnd, err := s.svc.AddWindow(req.BatchID, req.Label, req.Center, req.SpringConst)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, wnd)
}

func (s *Server) handleListWindows(w http.ResponseWriter, r *http.Request) {
	batchID := r.URL.Query().Get("batch_id")
	if batchID == "" {
		writeErr(w, model.E(model.ErrInvalid, "batch_id query required"))
		return
	}
	ws, err := s.svc.ListWindows(batchID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"windows": ws})
}

type importSamplesRequest struct {
	Samples []service.ImportSample `json:"samples"`
}

func (s *Server) handleImportSamples(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req importSamplesRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	if len(req.Samples) == 0 {
		writeErr(w, model.E(model.ErrInvalid, "samples required"))
		return
	}
	res, err := s.svc.ImportSamples(id, req.Samples)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleCorrectWindow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	summary, err := s.svc.CorrectWindow(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

type setWindowStatusRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleSetWindowStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req setWindowStatusRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	status, ok := model.ValidateWindowStatus(req.Status)
	if !ok {
		writeErr(w, model.E(model.ErrInvalid, "invalid window status %q", req.Status))
		return
	}
	wnd, err := s.svc.SetWindowStatus(id, status)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wnd)
}
