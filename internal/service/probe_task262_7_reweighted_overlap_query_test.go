package service

import (
	"math"
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestOverlapQueryUsesDiagnosisReweighting(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-query-reweight", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	left, err := s.AddWindow(batch.ID, "left", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	right, err := s.AddWindow(batch.ID, "right", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	leftSamples := make([]ImportSample, 0, 40)
	rightSamples := make([]ImportSample, 0, 40)
	for i := 0; i < 40; i++ {
		energy := 0.0
		leftBias, rightBias := 30.0, 1.0
		if i >= 20 {
			energy = 10
			leftBias, rightBias = 1, 30
		}
		leftSamples = append(leftSamples, ImportSample{Seq: i + 1, Energy: energy, Bias: leftBias})
		rightSamples = append(rightSamples, ImportSample{Seq: i + 1, Energy: energy, Bias: rightBias})
	}
	if _, err := s.ImportSamples(left.ID, leftSamples); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportSamples(right.ID, rightSamples); err != nil {
		t.Fatal(err)
	}

	queried, err := s.GetOverlap(left.ID, right.ID)
	if err != nil {
		t.Fatal(err)
	}
	report, err := s.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Edges) != 1 || report.Edges[0].Status != string(model.EdgeGap) {
		t.Fatalf("diagnosis did not expose the weighted gap: %+v", report)
	}
	if math.Abs(queried-report.Edges[0].Overlap) > 1e-12 || queried >= 0.05 {
		t.Fatalf("query=%v diagnosis=%v, want the same weighted gap", queried, report.Edges[0].Overlap)
	}
}
