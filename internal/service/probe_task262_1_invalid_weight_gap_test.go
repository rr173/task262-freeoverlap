package service

import (
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestInvalidReweightSampleCannotBridgeGap(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-invalid-weight", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	left, err := s.AddWindow(batch.ID, "left", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	right, err := s.AddWindow(batch.ID, "right", 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportSamples(left.ID, []ImportSample{
		{Seq: 1, Energy: 0, Bias: 0},
		{Seq: 2, Energy: 100, Bias: 1e9},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportSamples(right.ID, []ImportSample{
		{Seq: 1, Energy: 100, Bias: 0},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CorrectWindow(left.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CorrectWindow(right.ID); err != nil {
		t.Fatal(err)
	}

	report, err := s.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.GapEdges != 1 || report.Converged {
		t.Fatalf("unusable sample bridged the physical gap: %+v", report)
	}
	if report.Edges[0].Status != string(model.EdgeGap) {
		t.Fatalf("edge status = %s, want gap", report.Edges[0].Status)
	}
}
