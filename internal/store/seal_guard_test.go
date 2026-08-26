package store

import (
	"testing"

	"task262-freeoverlap/internal/model"
)

// sealBatch sets a batch to the sealed terminal state directly, simulating the
// result of a snapshot publication without exercising the publish path.
func sealBatch(t *testing.T, st *Store, batchID string) {
	t.Helper()
	if _, err := st.db.Exec(
		`UPDATE calc_batches SET status = ?, updated_at = ? WHERE id = ?`,
		string(model.BatchSealed), model.NowMillis(), batchID); err != nil {
		t.Fatalf("seal batch: %v", err)
	}
}

func setupSealedBatchFixtures(t *testing.T) (*Store, *model.CalcBatch, *model.SamplingWindow, *model.WindowEdge) {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	batch := &model.CalcBatch{
		ID: "batch-fix", Name: "fix", Method: "umbrella",
		Temperature: 300, KT: 2.494, Status: model.BatchPublishable,
		CreatedAt: model.NowMillis(), UpdatedAt: model.NowMillis(),
	}
	if err := st.CreateBatch(batch); err != nil {
		t.Fatalf("create batch: %v", err)
	}
	win := &model.SamplingWindow{
		ID: "win-fix", BatchID: batch.ID, Label: "w0",
		Center: 0, SpringConst: 10, BiasVersion: 1, Status: model.WindowRaw,
		CreatedAt: model.NowMillis(), UpdatedAt: model.NowMillis(),
	}
	if err := st.CreateWindow(win); err != nil {
		t.Fatalf("create window: %v", err)
	}
	sample := &model.EnergySample{
		ID: "smpl-fix", WindowID: win.ID, Seq: 1, Energy: 1.0, Bias: 0.5,
		Weight: 1.0, ContentHash: model.SampleHash(win.ID, 1, 1.0, 0.5),
		CreatedAt: model.NowMillis(),
	}
	if _, err := st.InsertSamples(win.ID, []*model.EnergySample{sample}); err != nil {
		t.Fatalf("insert sample: %v", err)
	}
	edge := &model.WindowEdge{
		ID: model.EdgeID(batch.ID, win.ID, win.ID), BatchID: batch.ID,
		LowerWindowID: win.ID, UpperWindowID: win.ID, Overlap: 0.5,
		Status: model.EdgeCandidate, CreatedAt: model.NowMillis(),
	}
	if err := st.ReplaceEdges(batch.ID, []*model.WindowEdge{edge}); err != nil {
		t.Fatalf("replace edges: %v", err)
	}
	sealBatch(t, st, batch.ID)
	return st, batch, win, edge
}

// A sealed batch must reject every window/sample/edge mutation, even when the
// caller bypasses the service layer and writes through the store directly.
func TestSealedBatchRejectsDirectMutations(t *testing.T) {
	st, batch, win, edge := setupSealedBatchFixtures(t)

	before, err := st.CountSamples(win.ID)
	if err != nil {
		t.Fatalf("count before: %v", err)
	}

	t.Run("InsertSamples", func(t *testing.T) {
		extra := &model.EnergySample{
			ID: "smpl-extra", WindowID: win.ID, Seq: 2, Energy: 2.0, Bias: 0.5,
			Weight: 1.0, ContentHash: model.SampleHash(win.ID, 2, 2.0, 0.5),
			CreatedAt: model.NowMillis(),
		}
		n, err := st.InsertSamples(win.ID, []*model.EnergySample{extra})
		if !model.IsKind(err, model.ErrImmutable) {
			t.Fatalf("InsertSamples on sealed batch: err=%v, want ErrImmutable", err)
		}
		if n != 0 {
			t.Fatalf("InsertSamples returned n=%d on sealed batch", n)
		}
		after, _ := st.CountSamples(win.ID)
		if after != before {
			t.Fatalf("sample count changed: %d -> %d", before, after)
		}
	})
	t.Run("CreateWindow", func(t *testing.T) {
		extra := &model.SamplingWindow{
			ID: "win-extra", BatchID: batch.ID, Label: "wX",
			Center: 5, SpringConst: 10, BiasVersion: 1, Status: model.WindowRaw,
			CreatedAt: model.NowMillis(), UpdatedAt: model.NowMillis(),
		}
		if err := st.CreateWindow(extra); !model.IsKind(err, model.ErrImmutable) {
			t.Fatalf("CreateWindow on sealed batch: err=%v, want ErrImmutable", err)
		}
	})
	t.Run("UpdateWindowStatus", func(t *testing.T) {
		if err := st.UpdateWindowStatus(win.ID, model.WindowExcluded, 1); !model.IsKind(err, model.ErrImmutable) {
			t.Fatalf("UpdateWindowStatus on sealed batch: err=%v, want ErrImmutable", err)
		}
		got, _ := st.GetWindow(win.ID)
		if got.Status == model.WindowExcluded {
			t.Fatalf("window status changed on sealed batch: %+v", got)
		}
	})
	t.Run("UpdateSampleWeight", func(t *testing.T) {
		if err := st.UpdateSampleWeight("smpl-fix", 0.0); !model.IsKind(err, model.ErrImmutable) {
			t.Fatalf("UpdateSampleWeight on sealed batch: err=%v, want ErrImmutable", err)
		}
	})
	t.Run("UpdateEdgeStatus", func(t *testing.T) {
		if err := st.UpdateEdgeStatus(edge.ID, model.EdgeResample, "post-seal"); !model.IsKind(err, model.ErrImmutable) {
			t.Fatalf("UpdateEdgeStatus on sealed batch: err=%v, want ErrImmutable", err)
		}
	})
	t.Run("ReplaceEdges", func(t *testing.T) {
		if err := st.ReplaceEdges(batch.ID, nil); !model.IsKind(err, model.ErrImmutable) {
			t.Fatalf("ReplaceEdges on sealed batch: err=%v, want ErrImmutable", err)
		}
	})
}

// Unsealed batches must remain fully editable; the guard only rejects the
// sealed terminal state so normal editing is unaffected.
func TestUnsealedBatchAllowsMutations(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	batch := &model.CalcBatch{
		ID: "batch-live", Name: "live", Method: "umbrella",
		Temperature: 300, KT: 2.494, Status: model.BatchReceiving,
		CreatedAt: model.NowMillis(), UpdatedAt: model.NowMillis(),
	}
	if err := st.CreateBatch(batch); err != nil {
		t.Fatalf("create batch: %v", err)
	}
	win := &model.SamplingWindow{
		ID: "win-live", BatchID: batch.ID, Label: "w0",
		Center: 0, SpringConst: 10, BiasVersion: 1, Status: model.WindowRaw,
		CreatedAt: model.NowMillis(), UpdatedAt: model.NowMillis(),
	}
	if err := st.CreateWindow(win); err != nil {
		t.Fatalf("create window on receiving batch: %v", err)
	}
	sample := &model.EnergySample{
		ID: "smpl-live", WindowID: win.ID, Seq: 1, Energy: 1.0, Bias: 0.5,
		Weight: 1.0, ContentHash: model.SampleHash(win.ID, 1, 1.0, 0.5),
		CreatedAt: model.NowMillis(),
	}
	if n, err := st.InsertSamples(win.ID, []*model.EnergySample{sample}); err != nil || n != 1 {
		t.Fatalf("insert sample on receiving batch: n=%d err=%v", n, err)
	}
	if err := st.UpdateWindowStatus(win.ID, model.WindowCorrected, 1); err != nil {
		t.Fatalf("update window status on receiving batch: %v", err)
	}
	if err := st.UpdateSampleWeight("smpl-live", 0.5); err != nil {
		t.Fatalf("update sample weight on receiving batch: %v", err)
	}
	edge := &model.WindowEdge{
		ID: model.EdgeID(batch.ID, win.ID, win.ID), BatchID: batch.ID,
		LowerWindowID: win.ID, UpperWindowID: win.ID, Overlap: 0.5,
		Status: model.EdgeCandidate, CreatedAt: model.NowMillis(),
	}
	if err := st.ReplaceEdges(batch.ID, []*model.WindowEdge{edge}); err != nil {
		t.Fatalf("replace edges on receiving batch: %v", err)
	}
	if err := st.UpdateEdgeStatus(edge.ID, model.EdgeResample, "note"); err != nil {
		t.Fatalf("update edge status on receiving batch: %v", err)
	}
}
