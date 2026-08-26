package service

import (
	"encoding/json"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

// newTestStore opens an in-memory store for service-level tests.
func newTestStore(t *testing.T) (*store.Store, *Service) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, New(st)
}

// addCorrectedWindow creates a window around center, imports a deterministic
// spread of samples (±3 around the center) and runs bias correction.
func addCorrectedWindow(t *testing.T, svc *Service, batchID, label string, center float64) {
	t.Helper()
	w, err := svc.AddWindow(batchID, label, center, 10.0)
	if err != nil {
		t.Fatalf("add window %s: %v", label, err)
	}
	var in []ImportSample
	for j := 0; j < 60; j++ {
		e := w.Center + float64(j%30-15)*0.2 // spread ~[-3, 2.8]
		in = append(in, ImportSample{Seq: j + 1, Energy: e, Bias: 0.5})
	}
	if _, err := svc.ImportSamples(w.ID, in); err != nil {
		t.Fatalf("import samples %s: %v", label, err)
	}
	if _, err := svc.CorrectWindow(w.ID); err != nil {
		t.Fatalf("correct window %s: %v", label, err)
	}
}

// TestDiagnosisPublishabilityTracksLatestData guards the regression where a
// batch that once passed diagnosis kept a stale publishable status after later
// data (a newly added gap window) made it unqualified. CreateSnapshot re-
// evaluates the current data, so it must also refresh the batch's
// publishability classification — and publishing must then be blocked.
func TestDiagnosisPublishabilityTracksLatestData(t *testing.T) {
	_, svc := newTestStore(t)
	batch, err := svc.CreateBatch("regress", "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	addCorrectedWindow(t, svc, batch.ID, "w0", 0)
	addCorrectedWindow(t, svc, batch.ID, "w1", 5) // overlaps w0 -> converged
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchPublishable {
		t.Fatalf("after first diagnosis: status=%s, want publishable", b.Status)
	}

	// A far-away window opens a gap in the current data.
	addCorrectedWindow(t, svc, batch.ID, "w2", 100)

	// CreateSnapshot re-evaluates current data; the batch must become insufficient.
	sn, err := svc.CreateSnapshot(batch.ID, "snap")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchInsufficient {
		t.Fatalf("after gap data + snapshot: status=%s, want insufficient", b.Status)
	}
	var parsed model.DiagnosisReport
	if err := json.Unmarshal([]byte(sn.Snapshot), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Converged || parsed.GapEdges != 1 {
		t.Fatalf("snapshot should report an unconverged gap, got %+v", parsed)
	}
	// Publishing is now blocked by the refreshed (insufficient) batch status.
	if _, err := svc.PublishSnapshot(sn.ID); !model.IsKind(err, model.ErrStateMismatch) {
		t.Fatalf("publish should be blocked with state-mismatch, got %v", err)
	}
}

// TestDiagnosisRecoveryPublishable ensures the data-recovery path still works:
// after a diagnosis reports a gap (insufficient), removing the offending data
// and re-diagnosing must restore publishable. Publishability must move in either
// direction until the batch is sealed.
func TestDiagnosisRecoveryPublishable(t *testing.T) {
	_, svc := newTestStore(t)
	batch, err := svc.CreateBatch("recover", "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	addCorrectedWindow(t, svc, batch.ID, "w0", 0)
	addCorrectedWindow(t, svc, batch.ID, "w1", 5)     // overlaps w0
	addCorrectedWindow(t, svc, batch.ID, "wbad", 100) // gap vs w1
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchInsufficient {
		t.Fatalf("after gap diagnosis: status=%s, want insufficient", b.Status)
	}
	// Recover by excluding the offending window.
	bad, err := svc.ListWindows(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	var badID string
	for _, w := range bad {
		if w.Label == "wbad" {
			badID = w.ID
		}
	}
	if badID == "" {
		t.Fatal("wbad window not found")
	}
	if _, err := svc.SetWindowStatus(badID, model.WindowExcluded); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchPublishable {
		t.Fatalf("after recovery diagnosis: status=%s, want publishable", b.Status)
	}
}

// TestDiagnosisSealedImmutable ensures the sealed terminal state is preserved:
// once a snapshot is published and the batch sealed, neither diagnosis nor
// snapshot creation may mutate it.
func TestDiagnosisSealedImmutable(t *testing.T) {
	_, svc := newTestStore(t)
	batch, err := svc.CreateBatch("sealed", "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	addCorrectedWindow(t, svc, batch.ID, "w0", 0)
	addCorrectedWindow(t, svc, batch.ID, "w1", 5)
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	sn, err := svc.CreateSnapshot(batch.ID, "snap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishSnapshot(sn.ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := svc.GetBatch(batch.ID); b.Status != model.BatchSealed {
		t.Fatalf("expected sealed after publish, got %s", b.Status)
	}
	if _, err := svc.RunDiagnosis(batch.ID); !model.IsKind(err, model.ErrImmutable) {
		t.Fatalf("diagnose sealed batch should be immutable, got %v", err)
	}
	if _, err := svc.CreateSnapshot(batch.ID, "snap2"); !model.IsKind(err, model.ErrImmutable) {
		t.Fatalf("create snapshot on sealed batch should be immutable, got %v", err)
	}
}
