package store

import (
	"math"
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
)

func setupSampleStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.CreateBatch(&model.CalcBatch{
		ID: "batch-x", Name: "x", Method: "umbrella", Temperature: 300, KT: 2.5,
		Status: model.BatchReceiving, CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if err := st.CreateWindow(&model.SamplingWindow{
		ID: "win-x", BatchID: "batch-x", Label: "w", Center: 1, SpringConst: 10,
		BiasVersion: 1, Status: model.WindowRaw, SampleCount: 0, CreatedAt: 1, UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("create window: %v", err)
	}
	return st
}

// The store must refuse non-finite energy/bias at the boundary, before any
// transaction is opened, so direct callers cannot leak a corrupt sample in.
func TestInsertSamples_RejectsNonFiniteEnergyOrBias(t *testing.T) {
	cases := []struct {
		name string
		sm   model.EnergySample
	}{
		{"NaN energy", model.EnergySample{ID: "s", WindowID: "win-x", Seq: 1, Energy: math.NaN(), Bias: 0.1, ContentHash: "h"}},
		{"Inf energy", model.EnergySample{ID: "s", WindowID: "win-x", Seq: 1, Energy: math.Inf(1), Bias: 0.1, ContentHash: "h"}},
		{"NaN bias", model.EnergySample{ID: "s", WindowID: "win-x", Seq: 1, Energy: 1.0, Bias: math.NaN(), ContentHash: "h"}},
		{"Inf bias", model.EnergySample{ID: "s", WindowID: "win-x", Seq: 1, Energy: 1.0, Bias: math.Inf(-1), ContentHash: "h"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := setupSampleStore(t)
			good := []model.EnergySample{
				{ID: "g1", WindowID: "win-x", Seq: 1, Energy: 1.0, Bias: 0.1, Weight: 1.0, ContentHash: "hg1", CreatedAt: 1},
				{ID: "g2", WindowID: "win-x", Seq: 2, Energy: 2.0, Bias: 0.2, Weight: 1.0, ContentHash: "hg2", CreatedAt: 1},
			}
			batch := append(good, tc.sm)
			ptrs := make([]*model.EnergySample, len(batch))
			for i := range batch {
				ptrs[i] = &batch[i]
			}
			n, err := st.InsertSamples("win-x", ptrs)
			if err == nil {
				t.Fatalf("expected error, got n=%d", n)
			}
			if !model.IsKind(err, model.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
			// Nothing should have been persisted: count stays 0.
			if got, _ := st.CountSamples("win-x"); got != 0 {
				t.Fatalf("count after rejected insert = %d, want 0", got)
			}
			w, err := st.GetWindow("win-x")
			if err != nil {
				t.Fatalf("get window: %v", err)
			}
			if w.SampleCount != 0 {
				t.Fatalf("persisted sample_count = %d, want 0", w.SampleCount)
			}
		})
	}
}

// A clean batch still inserts and updates the window counter.
func TestInsertSamples_CleanBatch(t *testing.T) {
	st := setupSampleStore(t)
	ptrs := []*model.EnergySample{
		{ID: "g1", WindowID: "win-x", Seq: 1, Energy: 1.0, Bias: 0.1, Weight: 1.0, ContentHash: "hg1", CreatedAt: 1},
		{ID: "g2", WindowID: "win-x", Seq: 2, Energy: 2.0, Bias: 0.2, Weight: 1.0, ContentHash: "hg2", CreatedAt: 1},
	}
	n, err := st.InsertSamples("win-x", ptrs)
	if err != nil || n != 2 {
		t.Fatalf("insert = %d (err %v), want 2", n, err)
	}
	if got, _ := st.CountSamples("win-x"); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}
}
