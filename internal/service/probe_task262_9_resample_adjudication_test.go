package service

import (
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestDiagnosisPreservesResampleAdjudication(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-adjudication", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	for i, center := range []float64{0, 100} {
		window, err := s.AddWindow(batch.ID, string(rune('a'+i)), center, 10)
		if err != nil {
			t.Fatal(err)
		}
		var samples []ImportSample
		for seq := 1; seq <= 20; seq++ {
			samples = append(samples, ImportSample{Seq: seq, Energy: center + float64(seq%2), Bias: 1})
		}
		if _, err := s.ImportSamples(window.ID, samples); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	edges, err := s.ListEdges(batch.ID)
	if err != nil || len(edges) != 1 {
		t.Fatalf("initial edges=%+v, err=%v", edges, err)
	}
	const note = "extend both neighboring trajectories"
	if _, err := s.AdjudicateEdge(edges[0].ID, model.EdgeResample, note); err != nil {
		t.Fatal(err)
	}
	report, err := s.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.ListEdges(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.ResampleEdges != 1 || report.GapEdges != 0 || report.Converged ||
		len(report.Edges) != 1 || report.Edges[0].Status != string(model.EdgeResample) || report.Edges[0].Note != note ||
		len(stored) != 1 || stored[0].Status != model.EdgeResample || stored[0].Note != note {
		t.Fatalf("adjudication lost: report=%+v stored=%+v", report, stored)
	}
}
