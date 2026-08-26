package service

import (
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestExcludedWindowReconcilesPersistedEdges(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-edge-reconcile", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	var windows []*model.SamplingWindow
	for i, center := range []float64{0, 5, 10} {
		w, err := s.AddWindow(batch.ID, string(rune('a'+i)), center, 10)
		if err != nil {
			t.Fatal(err)
		}
		windows = append(windows, w)
		if _, err := s.ImportSamples(w.ID, []ImportSample{{Seq: 1, Energy: center, Bias: 0}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	edges, err := s.ListEdges(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("initial edge count = %d, want 2", len(edges))
	}
	if _, err := s.SetWindowStatus(windows[1].ID, model.WindowExcluded); err != nil {
		t.Fatal(err)
	}
	report, err := s.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	edges, err = s.ListEdges(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Edges) != 1 || len(edges) != 1 {
		t.Fatalf("report edges=%d persisted edges=%d, want 1/1", len(report.Edges), len(edges))
	}
	if edges[0].LowerWindowID != windows[0].ID || edges[0].UpperWindowID != windows[2].ID {
		t.Fatalf("stale edge set: %+v", edges)
	}
}
