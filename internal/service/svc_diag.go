package service

import (
	"sort"

	"task262-freeoverlap/internal/diag"
	"task262-freeoverlap/internal/model"
)

// RunDiagnosis 对批次运行完整诊断并落库边判定。
// 返回诊断报告。批次状态根据结果推进：有断层 -> insufficient；否则 -> publishable。
func (s *Service) RunDiagnosis(batchID string) (*model.DiagnosisReport, error) {
	batch, err := s.store.GetBatch(batchID)
	if err != nil {
		return nil, err
	}
	if batch.Status.IsTerminal() {
		return nil, model.E(model.ErrImmutable, "batch %s is sealed", batchID)
	}
	return s.diagnoseAndApply(batch)
}

// diagnoseAndApply runs a fresh diagnosis over the batch's current data and
// commits the resulting projections to the store: per-window status, the edge
// set (preserving prior adjudications), and—critically—the batch's own
// publishability classification.
//
// Because publishability is a classification of *current* data, every path that
// re-evaluates the diagnosis (RunDiagnosis and CreateSnapshot) must route
// through here. Otherwise a batch that once passed diagnosis can keep a stale
// publishable status after later data (e.g. a newly added window that opens a
// gap) makes it unqualified. Sealed batches are rejected upstream by the caller.
func (s *Service) diagnoseAndApply(batch *model.CalcBatch) (*model.DiagnosisReport, error) {
	loader := &storeLoader{st: s.store}
	resolve := func(windowID string) (*model.SamplingWindow, error) {
		return s.store.GetWindow(windowID)
	}
	report, err := diag.Diagnose(loader, resolve, batch)
	if err != nil {
		return nil, err
	}
	for _, ws := range report.Windows {
		if err := s.store.UpdateWindowStatus(ws.ID, ws.Status, ws.SampleCount); err != nil {
			return nil, err
		}
	}
	// 落库边。
	windows, _ := s.store.ListWindows(batch.ID)
	sort.Slice(windows, func(i, j int) bool { return windows[i].Center < windows[j].Center })
	var active []*model.SamplingWindow
	for _, w := range windows {
		if w.Status != model.WindowExcluded {
			active = append(active, w)
		}
	}
	var currentEdges []*model.WindowEdge
	if len(active) >= 2 {
		lower := make([]string, 0, len(active)-1)
		upper := make([]string, 0, len(active)-1)
		for i := 0; i+1 < len(active); i++ {
			lower = append(lower, active[i].ID)
			upper = append(upper, active[i+1].ID)
		}
		currentEdges = diag.EdgesFromReport(report, lower, upper)
	}
	previousEdges, err := s.store.ListEdges(batch.ID)
	if err != nil {
		return nil, err
	}
	diag.PreserveAdjudications(report, currentEdges, previousEdges)
	if err := s.store.ReplaceEdges(batch.ID, currentEdges); err != nil {
		return nil, err
	}
	// 推进批次状态：发布资格是对当前数据的判定，随最新诊断同步。
	next := model.BatchPublishable
	if !report.Converged {
		next = model.BatchInsufficient
	}
	if !model.CanApplyDiagnosisStatus(batch.Status, next) {
		return nil, model.E(model.ErrStateMismatch,
			"cannot apply diagnosis status %s to batch %s in %s", next, batch.ID, batch.Status)
	}
	if err := s.store.UpdateBatchStatus(batch.ID, next); err != nil {
		return nil, err
	}
	return report, nil
}

// ListEdges 列出批次边。
func (s *Service) ListEdges(batchID string) ([]*model.WindowEdge, error) {
	return s.store.ListEdges(batchID)
}

// AdjudicateEdge 裁决一条边（标记需重采样等）。
func (s *Service) AdjudicateEdge(edgeID string, status model.EdgeStatus, note string) (*model.WindowEdge, error) {
	edges, err := s.listAllEdges()
	if err != nil {
		return nil, err
	}
	var target *model.WindowEdge
	for _, e := range edges {
		if e.ID == edgeID {
			target = e
			break
		}
	}
	if target == nil {
		return nil, model.E(model.ErrNotFound, "edge %s not found", edgeID)
	}
	if err := s.store.UpdateEdgeStatus(edgeID, status, note); err != nil {
		return nil, err
	}
	return s.getEdge(edgeID)
}
