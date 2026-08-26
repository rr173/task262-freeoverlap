package service

import (
	"math"
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

// setupStore constructs a Service backed by a fresh SQLite DB with one batch
// and one mutable window, returning the window ID for sample import.
func setupStore(t *testing.T) (*Service, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st)
	b, err := svc.CreateBatch("b1", "umbrella", 300)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	w, err := svc.AddWindow(b.ID, "w1", 1.0, 10.0)
	if err != nil {
		t.Fatalf("add window: %v", err)
	}
	return svc, st, w.ID
}

func countSamples(t *testing.T, st *store.Store, windowID string) int {
	t.Helper()
	n, err := st.CountSamples(windowID)
	if err != nil {
		t.Fatalf("count samples: %v", err)
	}
	return n
}

func sampleCount(t *testing.T, st *store.Store, windowID string) int {
	t.Helper()
	w, err := st.GetWindow(windowID)
	if err != nil {
		t.Fatalf("get window: %v", err)
	}
	return w.SampleCount
}

// A batch containing an invalid observation must be rejected in full: no
// valid prefix is persisted, and the window's persisted sample_count stays
// at its pre-import value so a subsequent legal import remains consistent.
func TestImportSamples_RejectsBatchWithInvalidObservationAllOrNothing(t *testing.T) {
	svc, st, wid := setupStore(t)

	cases := []struct {
		name   string
		idx    int
		sample ImportSample
	}{
		{"NaN energy mid-batch", 1, ImportSample{Seq: 2, Energy: math.NaN(), Bias: 0.2}},
		{"Inf energy mid-batch", 1, ImportSample{Seq: 2, Energy: math.Inf(1), Bias: 0.2}},
		{"NaN bias mid-batch", 1, ImportSample{Seq: 2, Energy: 2.0, Bias: math.NaN()}},
		{"Inf bias mid-batch", 1, ImportSample{Seq: 2, Energy: 2.0, Bias: math.Inf(-1)}},
		{"NaN energy first", 0, ImportSample{Seq: 1, Energy: math.NaN(), Bias: 0.1}},
		{"NaN bias last", 2, ImportSample{Seq: 3, Energy: 3.0, Bias: math.NaN()}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			good := []ImportSample{
				{Seq: 1, Energy: 1.0, Bias: 0.1},
				{Seq: 2, Energy: 2.0, Bias: 0.2},
				{Seq: 3, Energy: 3.0, Bias: 0.3},
			}
			good[tc.idx] = tc.sample

			before := countSamples(t, st, wid)
			res, err := svc.ImportSamples(wid, good)
			if err == nil {
				t.Fatalf("expected error for %s, got result %+v", tc.name, res)
			}
			if !model.IsKind(err, model.ErrInvalid) {
				t.Fatalf("expected ErrInvalid, got %v", err)
			}
			// All-or-nothing: nothing may be persisted for the failed request.
			if got := countSamples(t, st, wid); got != before {
				t.Fatalf("sample count changed after failed import: before=%d after=%d", before, got)
			}
			if got := sampleCount(t, st, wid); got != before {
				t.Fatalf("persisted sample_count changed after failed import: before=%d after=%d", before, got)
			}
		})
	}
}

// After a rejected import, a subsequent legal import of the same observations
// must succeed and report the correct inserted/duplicated counts — proving the
// window counter stayed consistent (no leaked prefix to deduplicate against).
func TestImportSamples_SubsequentLegalImportAfterFailure(t *testing.T) {
	svc, st, wid := setupStore(t)

	// First import: a NaN-bias observation sits in the middle of valid ones.
	_, err := svc.ImportSamples(wid, []ImportSample{
		{Seq: 1, Energy: 1.0, Bias: 0.1},
		{Seq: 2, Energy: 2.0, Bias: math.NaN()},
		{Seq: 3, Energy: 3.0, Bias: 0.3},
	})
	if err == nil {
		t.Fatalf("expected first import with NaN bias to fail")
	}
	if got := countSamples(t, st, wid); got != 0 {
		t.Fatalf("count after failed import = %d, want 0", got)
	}

	// Second import: all legal, same content the failed batch intended.
	res, err := svc.ImportSamples(wid, []ImportSample{
		{Seq: 1, Energy: 1.0, Bias: 0.1},
		{Seq: 2, Energy: 2.0, Bias: 0.2},
		{Seq: 3, Energy: 3.0, Bias: 0.3},
	})
	if err != nil {
		t.Fatalf("legal reimport failed: %v", err)
	}
	if res.Inserted != 3 || res.Duplicated != 0 {
		t.Fatalf("legal reimport result = %+v, want inserted=3 duplicated=0", res)
	}
	if got := countSamples(t, st, wid); got != 3 {
		t.Fatalf("count after legal reimport = %d, want 3", got)
	}

	// A third identical import must be fully idempotent (all duplicated).
	res2, err := svc.ImportSamples(wid, []ImportSample{
		{Seq: 1, Energy: 1.0, Bias: 0.1},
		{Seq: 2, Energy: 2.0, Bias: 0.2},
		{Seq: 3, Energy: 3.0, Bias: 0.3},
	})
	if err != nil || res2.Inserted != 0 || res2.Duplicated != 3 {
		t.Fatalf("idempotent reimport = %+v (err %v), want inserted=0 duplicated=3", res2, err)
	}
	if got := countSamples(t, st, wid); got != 3 {
		t.Fatalf("count after idempotent reimport = %d, want 3", got)
	}
}

// A fully valid batch imports normally and updates the window counter.
func TestImportSamples_ValidBatchImportsAndUpdatesCount(t *testing.T) {
	svc, st, wid := setupStore(t)
	res, err := svc.ImportSamples(wid, []ImportSample{
		{Seq: 1, Energy: 1.0, Bias: 0.1},
		{Seq: 2, Energy: 2.0, Bias: 0.2},
	})
	if err != nil {
		t.Fatalf("valid import failed: %v", err)
	}
	if res.Inserted != 2 || res.Duplicated != 0 {
		t.Fatalf("result = %+v, want inserted=2 duplicated=0", res)
	}
	if got := sampleCount(t, st, wid); got != 2 {
		t.Fatalf("persisted sample_count = %d, want 2", got)
	}
}

