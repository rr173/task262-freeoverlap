package httpapi

import (
	"net/http"

	"task262-freeoverlap/internal/model"
)

// --- 重叠与边 ---

func (s *Server) handleGetOverlap(w http.ResponseWriter, r *http.Request) {
	a := r.URL.Query().Get("a")
	b := r.URL.Query().Get("b")
	if a == "" || b == "" {
		writeErr(w, model.E(model.ErrInvalid, "a and b query params required"))
		return
	}
	o, err := s.svc.GetOverlap(a, b)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window_a": a,
		"window_b": b,
		"overlap":  o,
		"class":    classifyText(o),
	})
}

func classifyText(o float64) string {
	switch {
	case o >= 0.3:
		return "sufficient"
	case o < 0.05:
		return "gap"
	default:
		return "marginal"
	}
}

func (s *Server) handleListEdges(w http.ResponseWriter, r *http.Request) {
	batchID := r.URL.Query().Get("batch_id")
	if batchID == "" {
		writeErr(w, model.E(model.ErrInvalid, "batch_id query required"))
		return
	}
	edges, err := s.svc.ListEdges(batchID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"edges": edges})
}

type adjudicateRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func (s *Server) handleAdjudicateEdge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req adjudicateRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	status, ok := model.ValidateEdgeStatus(req.Status)
	if !ok {
		writeErr(w, model.E(model.ErrInvalid, "invalid edge status %q", req.Status))
		return
	}
	e, err := s.svc.AdjudicateEdge(id, status, req.Note)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}
