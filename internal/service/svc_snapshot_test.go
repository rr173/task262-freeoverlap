package service

import (
	"encoding/json"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

// newServiceAt 构造一个内存库上的 service 用于发布流程测试。
func newServiceAt(t *testing.T) (*Service, func()) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return New(st), func() { st.Close() }
}

// addConvergedWindow 在批次内添加一个分布围绕 center、与相邻窗口重叠良好的窗口，
// 并导入与校正样本。
func addConvergedWindow(t *testing.T, svc *Service, batchID, label string, center float64) {
	t.Helper()
	w, err := svc.AddWindow(batchID, label, center, 10.0)
	if err != nil {
		t.Fatalf("add window %s: %v", label, err)
	}
	var samples []ImportSample
	for j := 0; j < 60; j++ {
		samples = append(samples, ImportSample{Seq: j + 1, Energy: center + float64(j%30-15)*0.2, Bias: 0.5})
	}
	if _, err := svc.ImportSamples(w.ID, samples); err != nil {
		t.Fatalf("import samples %s: %v", label, err)
	}
	if _, err := svc.CorrectWindow(w.ID); err != nil {
		t.Fatalf("correct window %s: %v", label, err)
	}
}

// TestPublishRejectsGapSnapshot 复现并守护封存不变式：批次先被判为 publishable，
// 随后新增远端窗口产生断层，CreateSnapshot 会把带断层的报告固化进快照，
// PublishSnapshot 必须拒绝，批次保持原（非 sealed）状态、快照保持 draft。
func TestPublishRejectsGapSnapshot(t *testing.T) {
	svc, cleanup := newServiceAt(t)
	defer cleanup()

	batch, err := svc.CreateBatch("gap-batch", "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	// 两个相邻且重叠良好的窗口 -> 诊断 publishable。
	addConvergedWindow(t, svc, batch.ID, "w0", 0)
	addConvergedWindow(t, svc, batch.ID, "w1", 5)
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchPublishable {
		t.Fatalf("precondition: want publishable, got %s", b.Status)
	}

	// 新增一个远端窗口，w1 与它分布无交集 -> 断层；不重新诊断，批次状态仍 publishable。
	addConvergedWindow(t, svc, batch.ID, "wfar", 1000)

	sn, err := svc.CreateSnapshot(batch.ID, "gap-snapshot")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	// 快照内容应反映断层。
	var report model.DiagnosisReport
	if err := json.Unmarshal([]byte(sn.Snapshot), &report); err != nil {
		t.Fatalf("snapshot content invalid: %v", err)
	}
	if report.Converged || report.GapEdges == 0 {
		t.Fatalf("snapshot should carry a gap, got %+v", report)
	}

	// 发布必须被拒绝。
	if _, err := svc.PublishSnapshot(sn.ID); err == nil {
		t.Fatal("BUG: publishing a gapped snapshot should fail")
	}

	// 批次不得被封存，快照仍为 draft。
	b, _ := svc.GetBatch(batch.ID)
	if b.Status == model.BatchSealed {
		t.Fatalf("BUG: batch sealed despite gap snapshot: %s", b.Status)
	}
	got, _ := svc.store.GetSnapshot(sn.ID)
	if got.Status != model.SnapshotDraft {
		t.Fatalf("snapshot should remain draft, got %s", got.Status)
	}
}

// TestPublishAcceptsConvergedSnapshot 守护正常路径：重叠充分的批次可正常发布并封存。
func TestPublishAcceptsConvergedSnapshot(t *testing.T) {
	svc, cleanup := newServiceAt(t)
	defer cleanup()

	batch, err := svc.CreateBatch("ok-batch", "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	addConvergedWindow(t, svc, batch.ID, "w0", 0)
	addConvergedWindow(t, svc, batch.ID, "w1", 5)
	addConvergedWindow(t, svc, batch.ID, "w2", 10)
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchPublishable {
		t.Fatalf("precondition: want publishable, got %s", b.Status)
	}

	sn, err := svc.CreateSnapshot(batch.ID, "ok-snapshot")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	published, err := svc.PublishSnapshot(sn.ID)
	if err != nil {
		t.Fatalf("publish should succeed for converged batch: %v", err)
	}
	if published.Status != model.SnapshotPublished || published.FrozenAt == 0 {
		t.Fatalf("snapshot not frozen: %+v", published)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchSealed {
		t.Fatalf("batch should be sealed after publish, got %s", b.Status)
	}
}

// TestPublishRejectsInsufficientBatch 覆盖简单路径：诊断结果为 insufficient 时，
// 创建并发布快照必须失败，批次保持 insufficient。
func TestPublishRejectsInsufficientBatch(t *testing.T) {
	svc, cleanup := newServiceAt(t)
	defer cleanup()

	batch, err := svc.CreateBatch("insuf-batch", "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	// 两个远端窗口 -> 分布无交集 -> 断层。
	addConvergedWindow(t, svc, batch.ID, "w0", 0)
	addConvergedWindow(t, svc, batch.ID, "wfar", 1000)
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchInsufficient {
		t.Fatalf("precondition: want insufficient, got %s", b.Status)
	}

	sn, err := svc.CreateSnapshot(batch.ID, "insuf-snapshot")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if _, err := svc.PublishSnapshot(sn.ID); err == nil {
		t.Fatal("BUG: publishing an insufficient batch should fail")
	}
	b, _ := svc.GetBatch(batch.ID)
	if b.Status != model.BatchInsufficient {
		t.Fatalf("batch status changed to %s, want to stay insufficient", b.Status)
	}
}
