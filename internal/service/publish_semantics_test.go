package service

import (
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

// setupInsufficientBatch builds a batch with a gap (insufficient, not publishable).
func setupInsufficientBatch(t *testing.T, svc *Service) *model.CalcBatch {
	t.Helper()
	batch, err := svc.CreateBatch("insuf-"+t.Name(), "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddWindow(batch.ID, "wf0", 0, 10.0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddWindow(batch.ID, "wf1", 100, 10.0); err != nil {
		t.Fatal(err)
	}
	ws, _ := svc.ListWindows(batch.ID)
	for _, w := range ws {
		var samples []ImportSample
		for j := 0; j < 60; j++ {
			e := w.Center + float64(j%30-15)*0.2
			samples = append(samples, ImportSample{Seq: j + 1, Energy: e, Bias: 0.5})
		}
		if _, err := svc.ImportSamples(w.ID, samples); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CorrectWindow(w.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	return batch
}

// TestPublishInsufficientBatchIsStateMismatch verifies that the concurrent-
// publish fix does not relabel a genuine state-mismatch (batch insufficient,
// not because another publisher raced) as a conflict.
func TestPublishInsufficientBatchIsStateMismatch(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(st)

	batch := setupInsufficientBatch(t, svc)
	b, _ := svc.GetBatch(batch.ID)
	if b.Status != model.BatchInsufficient {
		t.Fatalf("want insufficient, got %s", b.Status)
	}
	sn, err := svc.CreateSnapshot(batch.ID, "snap")
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.PublishSnapshot(sn.ID)
	if err == nil {
		t.Fatal("expected error publishing insufficient batch")
	}
	if !model.IsKind(err, model.ErrStateMismatch) {
		t.Fatalf("publishing an insufficient (non-publishable, non-sealed) batch must be a state mismatch, got: %v", err)
	}
	// Batch must remain insufficient (publication must not mutate state).
	b2, _ := svc.GetBatch(batch.ID)
	if b2.Status != model.BatchInsufficient {
		t.Fatalf("batch status mutated on failed publish: %s", b2.Status)
	}
}

// TestPublishRepublishFrozenIsConflict verifies a sequential republish of an
// already-frozen snapshot returns a conflict (not immutable/state-mismatch).
func TestPublishRepublishFrozenIsConflict(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(st)

	batch := setupSealableBatch(t, svc)
	sn, err := svc.CreateSnapshot(batch.ID, "snap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishSnapshot(sn.ID); err != nil {
		t.Fatal(err)
	}
	// Batch is now sealed; snapshot is published. A second publish must conflict.
	if _, err := svc.PublishSnapshot(sn.ID); !model.IsKind(err, model.ErrConflict) {
		t.Fatalf("republish of frozen snapshot should conflict, got: %v", err)
	}
}

// TestPublishSecondDraftAfterSealIsConflict verifies that, after a batch is
// sealed by one publication, the batch becomes immutable and a new draft can
// no longer be created (so there is no path to publish a second draft of a
// sealed batch through the normal flow). The real cross-snapshot race — two
// drafts created *before* sealing then published concurrently — is covered by
// TestConcurrentPublishMultipleDrafts.
func TestPublishSecondDraftAfterSealIsImmutable(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(st)

	batch := setupSealableBatch(t, svc)
	sn1, err := svc.CreateSnapshot(batch.ID, "snap-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishSnapshot(sn1.ID); err != nil {
		t.Fatal(err)
	}
	// Batch sealed: creating a fresh draft is rejected as immutable.
	if _, err := svc.CreateSnapshot(batch.ID, "snap-2"); !model.IsKind(err, model.ErrImmutable) {
		t.Fatalf("creating a draft after seal should be immutable, got: %v", err)
	}
}
