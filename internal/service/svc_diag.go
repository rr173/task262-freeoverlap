package service

import (
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
	// 重建边并合并上一轮已落库的人工裁决（resample + note），保证报告与边列表一致。
	active, err := s.activeWindows(batchID)
	if err != nil {
		return nil, err
	}
	previousEdges, err := s.store.ListEdges(batchID)
	if err != nil {
		return nil, err
	}
	currentEdges := diag.BuildEdgesWithAdjudications(report, active, previousEdges)
	if err := s.store.ReplaceEdges(batchID, currentEdges); err != nil {
		return nil, err
	}
	// 推进批次状态。
	next := model.BatchPublishable
	if !report.Converged {
		next = model.BatchInsufficient
	}
	if !model.CanApplyDiagnosisStatus(batch.Status, next) {
		return nil, model.E(model.ErrStateMismatch,
			"cannot apply diagnosis status %s to batch %s in %s", next, batchID, batch.Status)
	}
	if err := s.store.UpdateBatchStatus(batchID, next); err != nil {
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
