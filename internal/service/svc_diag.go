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
	loader := &storeLoader{st: s.store}
	resolve := func(windowID string) (*model.SamplingWindow, error) {
		return s.store.GetWindow(windowID)
	}
	report, err := diag.Diagnose(loader, resolve, batch)
	if err != nil {
		return nil, err
	}
	if err := s.saveDiagnosis(report, batchID); err != nil {
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

// saveDiagnosis 持久化一次诊断报告的投影：窗口校正状态与相邻窗口边列表。
// 边按当前仍生效（非 excluded）的相邻窗口重建，排除窗口参与的边不再留存；
// 对同一相邻对保留研究者此前的 resample 裁决与备注，正常窗口的重叠数值与
// gap/sufficient 判定保持为本次重计算结果。调用 ReplaceEdges 先清后插，保证
// 边列表始终与本次诊断报告一致。
func (s *Service) saveDiagnosis(report *model.DiagnosisReport, batchID string) error {
	for _, ws := range report.Windows {
		if err := s.store.UpdateWindowStatus(ws.ID, ws.Status, ws.SampleCount); err != nil {
			return err
		}
	}
	windows, err := s.store.ListWindows(batchID)
	if err != nil {
		return err
	}
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
	previousEdges, err := s.store.ListEdges(batchID)
	if err != nil {
		return err
	}
	diag.PreserveAdjudications(report, currentEdges, previousEdges)
	return s.store.ReplaceEdges(batchID, currentEdges)
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
