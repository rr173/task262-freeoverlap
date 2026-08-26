package service

import (
	"task262-freeoverlap/internal/diag"
	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/overlap"
	"task262-freeoverlap/internal/snapshot"
	"task262-freeoverlap/internal/store"
	"task262-freeoverlap/internal/weight"
)

// CreateSnapshot 创建草稿快照（内容来自最新诊断）。
func (s *Service) CreateSnapshot(batchID, label string) (*model.ReliabilitySnapshot, error) {
	batch, err := s.store.GetBatch(batchID)
	if err != nil {
		return nil, err
	}
	if batch.Status.IsTerminal() {
		return nil, model.E(model.ErrImmutable, "batch %s is sealed", batchID)
	}
	report, err := diag.Diagnose(&storeLoader{st: s.store}, func(id string) (*model.SamplingWindow, error) {
		return s.store.GetWindow(id)
	}, batch)
	if err != nil {
		return nil, err
	}
	content, err := diag.RenderSnapshot(report)
	if err != nil {
		return nil, err
	}
	sn := snapshot.Create(batchID, label, content)
	if err := s.store.CreateSnapshot(sn); err != nil {
		return nil, err
	}
	return sn, nil
}

// ListSnapshots 列出批次快照。
func (s *Service) ListSnapshots(batchID string) ([]*model.ReliabilitySnapshot, error) {
	return s.store.ListSnapshots(batchID)
}

// PublishSnapshot 发布快照（不可变冻结）。
func (s *Service) PublishSnapshot(snapshotID string) (*model.ReliabilitySnapshot, error) {
	sn, err := s.store.GetSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	batch, err := s.store.GetBatch(sn.BatchID)
	if err != nil {
		return nil, err
	}
	if err := snapshot.ValidatePublication(batch.Status); err != nil {
		return nil, err
	}
	if err := snapshot.Publish(sn); err != nil {
		return nil, err
	}
	if err := s.store.CommitSnapshotPublication(snapshotID, sn.BatchID, sn.FrozenAt); err != nil {
		return nil, err
	}
	return s.store.GetSnapshot(snapshotID)
}

// SupersedeSnapshot 用新快照替代已发布快照。
func (s *Service) SupersedeSnapshot(snapshotID string) (*model.ReliabilitySnapshot, error) {
	sn, err := s.store.GetSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	if err := snapshot.Supersede(sn); err != nil {
		return nil, err
	}
	if err := s.store.UpdateSnapshotStatus(snapshotID, sn.Status, sn.FrozenAt); err != nil {
		return nil, err
	}
	return s.store.GetSnapshot(snapshotID)
}

// GetOverlap 计算两个窗口的重叠值（即时查询用）。
//
// 校正与重叠语义与批次诊断 (diag.Diagnose) 完全一致：对每个窗口加载样本、
// 做偏置校正（重加权）、计算分布重叠积分。跨批次窗口一律拒绝（保留原有行为）。
//
// 与诊断语义对齐的关键：诊断会丢弃 WindowExcluded 窗口（不参与重叠判定），
// 即时查询同样必须拒绝涉及已排除窗口的查询——否则会在被判定不可用的窗口上
// 返回与批次结论相矛盾的重叠值（例如即时查询显示“重叠充分”而批次诊断判定断层）。
func (s *Service) GetOverlap(windowA, windowB string) (float64, error) {
	wa, err := s.store.GetWindow(windowA)
	if err != nil {
		return 0, err
	}
	wb, err := s.store.GetWindow(windowB)
	if err != nil {
		return 0, err
	}
	// 跨批次窗口拒绝：保留原有行为。
	if wa.BatchID != wb.BatchID {
		return 0, model.E(model.ErrInvalid, "windows belong to different batches")
	}
	// 与诊断语义对齐：已排除窗口不参与重叠判定。
	if wa.Status == model.WindowExcluded {
		return 0, model.E(model.ErrInvalid, "window %s is excluded", windowA)
	}
	if wb.Status == model.WindowExcluded {
		return 0, model.E(model.ErrInvalid, "window %s is excluded", windowB)
	}
	batch, err := s.store.GetBatch(wa.BatchID)
	if err != nil {
		return 0, err
	}
	sa, err := s.store.ListSamples(windowA)
	if err != nil {
		return 0, err
	}
	sb, err := s.store.ListSamples(windowB)
	if err != nil {
		return 0, err
	}
	// 偏置校正（重加权），与 diag.Diagnose 使用相同的 CorrectWindow。
	correctedA, _ := weight.CorrectWindow(sa, weight.BiasParams{
		Center: wa.Center, SpringConst: wa.SpringConst, KT: batch.KT,
	})
	correctedB, _ := weight.CorrectWindow(sb, weight.BiasParams{
		Center: wb.Center, SpringConst: wb.SpringConst, KT: batch.KT,
	})
	o, _, _ := overlap.PairOverlap(correctedA, correctedB)
	return o, nil
}

// --- 内部辅助 ---

// storeLoader 把 store 适配为 diag.Loader。
type storeLoader struct {
	st *store.Store
}

func (l *storeLoader) ListWindows(batchID string) ([]*model.SamplingWindow, error) {
	return l.st.ListWindows(batchID)
}

func (l *storeLoader) ListSamples(windowID string) ([]*model.EnergySample, error) {
	return l.st.ListSamples(windowID)
}

func (s *Service) listAllEdges() ([]*model.WindowEdge, error) {
	batches, err := s.store.ListBatches()
	if err != nil {
		return nil, err
	}
	var out []*model.WindowEdge
	for _, b := range batches {
		edges, err := s.store.ListEdges(b.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, edges...)
	}
	return out, nil
}

func (s *Service) getEdge(edgeID string) (*model.WindowEdge, error) {
	edges, err := s.listAllEdges()
	if err != nil {
		return nil, err
	}
	for _, e := range edges {
		if e.ID == edgeID {
			return e, nil
		}
	}
	return nil, model.E(model.ErrNotFound, "edge %s not found", edgeID)
}
