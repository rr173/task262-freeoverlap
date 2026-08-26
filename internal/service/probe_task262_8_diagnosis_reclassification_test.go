package service

import (
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestDiagnosisRevokesStalePublishableStatus(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-reclassification", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	for i, center := range []float64{0, 1} {
		window, err := s.AddWindow(batch.ID, string(rune('a'+i)), center, 10)
		if err != nil {
			t.Fatal(err)
		}
		var samples []ImportSample
		for seq := 1; seq <= 20; seq++ {
			samples = append(samples, ImportSample{Seq: seq, Energy: float64(seq % 2), Bias: 1})
		}
		if _, err := s.ImportSamples(window.ID, samples); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.RunDiagnosis(batch.ID)
	if err != nil || !first.Converged {
		t.Fatalf("initial diagnosis = %+v, err=%v", first, err)
	}
	before, err := s.GetBatch(batch.ID)
	if err != nil || before.Status != model.BatchPublishable {
		t.Fatalf("initial batch = %+v, err=%v", before, err)
	}

	far, err := s.AddWindow(batch.ID, "far", 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	var farSamples []ImportSample
	for seq := 1; seq <= 20; seq++ {
		farSamples = append(farSamples, ImportSample{Seq: seq, Energy: 100 + float64(seq%2), Bias: 1})
	}
	if _, err := s.ImportSamples(far.ID, farSamples); err != nil {
		t.Fatal(err)
	}
	second, err := s.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.GetBatch(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.Converged || second.GapEdges == 0 || after.Status != model.BatchInsufficient {
		t.Fatalf("stale publication eligibility survived: report=%+v batch=%+v", second, after)
	}
}
