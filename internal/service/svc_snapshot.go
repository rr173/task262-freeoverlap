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
//
// 并发发布同一草稿或同一批次的不同草稿时，store 层 CommitSnapshotPublication
// 通过条件更新保证恰好一次成功：第一个发布者冻结快照并封存批次，其余发布者
// 的封存更新匹配零行 -> 事务回滚（撤销其快照冻结）-> 明确冲突。
//
// 服务层在此对三类失败统一返回 ErrConflict，使并发发布者都能区分“冲突”而非
// 误以为不可变或状态错配：
//   - 批次已被另一发布者封存（sealed）；
//   - 快照已被另一发布者冻结（published/superseded）；
//   - store 层事务条件更新未命中。
//
// 注意：GetBatch/GetSnapshot 的预读发生在事务之外，因此预读后到提交前可能被
// 另一发布者抢先。下面的预检查只为快路径提供清晰错误；真正的并发裁决仍由
// CommitSnapshotPublication 的事务条件更新完成。
func (s *Service) PublishSnapshot(snapshotID string) (*model.ReliabilitySnapshot, error) {
	sn, err := s.store.GetSnapshot(snapshotID)
	if err != nil {
		return nil, err
	}
	batch, err := s.store.GetBatch(sn.BatchID)
	if err != nil {
		return nil, err
	}
	// 预检查：批次已封存说明另一发布者已完成最终冻结 -> 明确冲突。
	if batch.Status == model.BatchSealed {
		return nil, model.E(model.ErrConflict, "batch %s already sealed; snapshot publication already finalized", sn.BatchID)
	}
	if err := snapshot.ValidatePublication(batch.Status); err != nil {
		return nil, err
	}
	// 预检查：快照已冻结/替代说明本快照已不是草稿 -> 明确冲突。
	if sn.Status.IsImmutable() {
		return nil, model.E(model.ErrConflict, "snapshot %s already %s; cannot republish", snapshotID, sn.Status)
	}
	if err := snapshot.Publish(sn); err != nil {
		// snapshot.Publish 仅在状态非法时报错，等价于已被冻结 -> 冲突。
		if model.IsKind(err, model.ErrConflict) || model.IsKind(err, model.ErrImmutable) {
			return nil, model.E(model.ErrConflict, "snapshot %s cannot be published: %v", snapshotID, err)
		}
		return nil, err
	}
	if err := s.store.CommitSnapshotPublication(snapshotID, sn.BatchID, sn.FrozenAt); err != nil {
		// 并发抢断：条件更新零命中 -> 已被另一发布者冻结/封存 -> 明确冲突。
		if model.IsKind(err, model.ErrConflict) {
			return nil, err
		}
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
