package service

import (
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestDiagnosisPersistsNonconvergedWindowStatus(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-window-status", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	window, err := s.AddWindow(batch.ID, "bad", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportSamples(window.ID, []ImportSample{
		{Seq: 1, Energy: 0, Bias: 1e9},
		{Seq: 2, Energy: 1, Bias: 1e9},
	}); err != nil {
		t.Fatal(err)
	}
	report, err := s.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	windows, err := s.ListWindows(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Nonconverged != 1 || len(windows) != 1 || windows[0].Status != model.WindowNonconverged {
		t.Fatalf("diagnosis=%+v windows=%+v", report, windows)
	}
}
