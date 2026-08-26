package service

import (
	"errors"
	"math"
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestInvalidSampleBatchImportIsAtomic(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-atomic-import", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	window, err := s.AddWindow(batch.ID, "window", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ImportSamples(window.ID, []ImportSample{
		{Seq: 1, Energy: 0.5, Bias: 1},
		{Seq: 2, Energy: math.NaN(), Bias: 1},
	})
	if !errors.Is(err, model.ErrInvalid) {
		t.Fatalf("import error = %v, want invalid", err)
	}
	samples, err := st.ListSamples(window.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := st.GetWindow(window.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 0 || stored.SampleCount != 0 {
		t.Fatalf("invalid request partially persisted: samples=%d window=%+v", len(samples), stored)
	}
	result, err := s.ImportSamples(window.ID, []ImportSample{{Seq: 3, Energy: 1.5, Bias: 1}})
	if err != nil || result.Inserted != 1 {
		t.Fatalf("valid import after rollback = %+v, err=%v", result, err)
	}
}
