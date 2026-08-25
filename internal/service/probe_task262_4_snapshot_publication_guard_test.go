package service

import (
	"errors"
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestInsufficientBatchCannotPublishSnapshot(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-snapshot-guard", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	for i, center := range []float64{0, 100} {
		window, err := s.AddWindow(batch.ID, string(rune('a'+i)), center, 10)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.ImportSamples(window.ID, []ImportSample{{Seq: 1, Energy: center, Bias: 0}}); err != nil {
			t.Fatal(err)
		}
	}
	report, err := s.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Converged {
		t.Fatalf("expected an insufficient diagnosis: %+v", report)
	}
	snapshot, err := s.CreateSnapshot(batch.ID, "bad")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishSnapshot(snapshot.ID); !errors.Is(err, model.ErrStateMismatch) {
		t.Fatalf("publish error = %v, want state mismatch", err)
	}
	storedSnapshot, err := st.GetSnapshot(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedBatch, err := s.GetBatch(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSnapshot.Status != model.SnapshotDraft || storedBatch.Status != model.BatchInsufficient {
		t.Fatalf("publication changed state: snapshot=%+v batch=%+v", storedSnapshot, storedBatch)
	}
}
