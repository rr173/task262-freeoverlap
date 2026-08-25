package service

import (
	"errors"
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestSealedBatchRejectsMutation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-sealed", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	var windows []*model.SamplingWindow
	for i := 0; i < 2; i++ {
		window, err := s.AddWindow(batch.ID, string(rune('a'+i)), 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		windows = append(windows, window)
		if _, err := s.ImportSamples(window.ID, []ImportSample{{Seq: 1, Energy: 0, Bias: 0}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.CreateSnapshot(batch.ID, "sealed")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishSnapshot(snapshot.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ImportSamples(windows[0].ID, []ImportSample{{Seq: 2, Energy: 0, Bias: 0}}); !errors.Is(err, model.ErrImmutable) {
		t.Fatalf("sealed import error = %v, want immutable", err)
	}
	if _, err := s.SetWindowStatus(windows[0].ID, model.WindowExcluded); !errors.Is(err, model.ErrImmutable) {
		t.Fatalf("sealed status update error = %v, want immutable", err)
	}
	current, err := s.ListWindows(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current[0].SampleCount != 1 || current[0].Status != model.WindowCorrected {
		t.Fatalf("sealed window mutated: %+v", current[0])
	}
}
