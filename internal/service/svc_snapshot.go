package service

import (
	"task262-freeoverlap/internal/diag"
	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/overlap"
	"task262-freeoverlap/internal/snapshot"
	"task262-freeoverlap/internal/store"
	"task262-freeoverlap/internal/weight"
)

// CreateSnapshot creates a draft snapshot whose content is the latest
// diagnosis. Because building the snapshot re-evaluates the current data, it
// also routes through diagnoseAndApply so the batch's publishability reflects
// that latest data — a batch that once passed diagnosis must not stay
// publishable after later data (e.g. a newly added gap window) makes it
// unqualified, nor may a snapshot be frozen when the batch is not publishable.
func (s *Service) CreateSnapshot(batchID, label string) (*model.ReliabilitySnapshot, error) {
	batch, err := s.store.GetBatch(batchID)
	if err != nil {
		return nil, err
	}
	if batch.Status.IsTerminal() {
		return nil, model.E(model.ErrImmutable, "batch %s is sealed", batchID)
	}
	report, err := s.diagnoseAndApply(batch)
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
