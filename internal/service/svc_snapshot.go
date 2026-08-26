package service

import (
	"sort"

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
	// 合并上一轮已落库的人工裁决（resample + note），使快照报告与持久化边列表
	// 逐条一致；只读渲染，不在此处落库边（边由最近一次 RunDiagnosis 维护）。
	active, err := s.activeWindows(batchID)
	if err != nil {
		return nil, err
	}
	previousEdges, err := s.store.ListEdges(batchID)
	if err != nil {
		return nil, err
	}
	diag.BuildEdgesWithAdjudications(report, active, previousEdges)

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

// GetOverlap 计算指定两条边或两个窗口的重叠值（查询用）。
func (s *Service) GetOverlap(windowA, windowB string) (float64, error) {
	wa, err := s.store.GetWindow(windowA)
	if err != nil {
		return 0, err
	}
	wb, err := s.store.GetWindow(windowB)
	if err != nil {
		return 0, err
	}
	if wa.BatchID != wb.BatchID {
		return 0, model.E(model.ErrInvalid, "windows belong to different batches")
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

// activeWindows 返回批次下「按 center 升序、已剔 excluded」的有效窗口，
// 与 diag.Diagnose 生成报告时使用的窗口顺序保持一致。诊断重建边与合并裁决
// 都依赖该顺序，故 RunDiagnosis 与 CreateSnapshot 必须共用同一来源。
func (s *Service) activeWindows(batchID string) ([]*model.SamplingWindow, error) {
	windows, err := s.store.ListWindows(batchID)
	if err != nil {
		return nil, err
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Center < windows[j].Center })
	var active []*model.SamplingWindow
	for _, w := range windows {
		if w.Status != model.WindowExcluded {
			active = append(active, w)
		}
	}
	return active, nil
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
