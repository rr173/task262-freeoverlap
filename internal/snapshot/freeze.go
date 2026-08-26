// Package snapshot 管理可靠性快照的创建与冻结语义。
//
// 快照是不可变的诊断结论载体：发布（freeze）后内容不得再修改，
// 只能被更新的快照“替代”。本包只处理状态语义，持久化由 store 负责。
package snapshot

import (
	"task262-freeoverlap/internal/model"
)

// Create 构造一个新的草稿快照。
func Create(batchID, label, content string) *model.ReliabilitySnapshot {
	return &model.ReliabilitySnapshot{
		ID:        model.NewID("snap", batchID+"|"+label),
		BatchID:   batchID,
		Label:     label,
		Status:    model.SnapshotDraft,
		Snapshot:  content,
		CreatedAt: model.NowMillis(),
	}
}

// ValidatePublication enforces the aggregate lifecycle rule: only a snapshot
// whose baked-in diagnosis is actually publishable may be frozen as reliable
// evidence. Verifying the report (rather than the batch's stored status) is
// what stops a batch that was classified publishable and later grew a gap from
// being sealed together with an unreliable snapshot: the snapshot content is
// recomputed from current data on creation, so it reflects the truth even when
// the persisted batch status is stale.
func ValidatePublication(report *model.DiagnosisReport) error {
	if report == nil || !report.Converged {
		return model.E(model.ErrStateMismatch, "snapshot diagnosis is not publishable")
	}
	return nil
}

// Publish 把草稿/共享快照转为已发布（不可变）。已发布或已替代的快照不可再发布。
func Publish(sn *model.ReliabilitySnapshot) error {
	switch sn.Status {
	case model.SnapshotDraft:
		sn.Status = model.SnapshotPublished
		sn.FrozenAt = model.NowMillis()
		return nil
	case model.SnapshotPublished:
		return model.E(model.ErrConflict, "snapshot %s already published", sn.ID)
	case model.SnapshotSuperseded:
		return model.E(model.ErrImmutable, "snapshot %s superseded", sn.ID)
	default:
		return model.E(model.ErrInvalid, "snapshot %s invalid status %s", sn.ID, sn.Status)
	}
}

// Supersede 把已发布快照标记为已替代（新的快照成为当前版本）。
func Supersede(sn *model.ReliabilitySnapshot) error {
	if sn.Status != model.SnapshotPublished {
		return model.E(model.ErrStateMismatch, "snapshot %s must be published to supersede, got %s", sn.ID, sn.Status)
	}
	sn.Status = model.SnapshotSuperseded
	return nil
}
