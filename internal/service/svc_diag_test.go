package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func mustOpenStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestRunDiagnosisPersistsNonconverged reproduces the reported bug: after a
// diagnosis run that concludes a window is nonconverged, the persisted window
// detail must reflect nonconverged, not remain raw.
func TestRunDiagnosisPersistsNonconverged(t *testing.T) {
	st := mustOpenStore(t)
	svc := New(st)

	batch, err := svc.CreateBatch("nb", "umbrella", 300.0)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}

	// Two adjacent windows. w1 gets enough uniform samples to converge.
	// w2 gets only a single sample -> ESS tiny -> nonconverged.
	w1, err := svc.AddWindow(batch.ID, "w1", 0, 10.0)
	if err != nil {
		t.Fatalf("add w1: %v", err)
	}
	w2, err := svc.AddWindow(batch.ID, "w2", 5, 10.0)
	if err != nil {
		t.Fatalf("add w2: %v", err)
	}

	// w1: 200 uniform-ish samples around center 0 -> converges.
	var s1 []ImportSample
	for j := 0; j < 200; j++ {
		s1 = append(s1, ImportSample{Seq: j + 1, Energy: float64(j%40-20) * 0.15, Bias: 0.5})
	}
	if _, err := svc.ImportSamples(w1.ID, s1); err != nil {
		t.Fatalf("import w1: %v", err)
	}
	// w2: samples whose bias all overflow the reweight cap (bias/kT > 700).
	// Every ReweightFactor returns ok=false, all weights are 0, normalization
	// fails (norm == nil) -> ESS 0 -> CorrectWindow reports Converged=false.
	// Diagnosis must therefore conclude w2 is nonconverged.
	var s2 []ImportSample
	for j := 0; j < 50; j++ {
		s2 = append(s2, ImportSample{Seq: j + 1, Energy: 5, Bias: 1e9})
	}
	if _, err := svc.ImportSamples(w2.ID, s2); err != nil {
		t.Fatalf("import w2: %v", err)
	}

	report, err := svc.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatalf("diagnose: %v", err)
	}

	// The report must flag w2 as nonconverged.
	var w2sum *model.WindowSummary
	for i := range report.Windows {
		if report.Windows[i].ID == w2.ID {
			w2sum = &report.Windows[i]
		}
	}
	if w2sum == nil {
		t.Fatalf("w2 missing from report windows: %+v", report.Windows)
	}
	if w2sum.Status != model.WindowNonconverged {
		t.Fatalf("report w2 status = %s, want nonconverged", w2sum.Status)
	}

	// The persisted window detail (window list / detail) must agree with the
	// diagnosis conclusion instead of staying raw.
	got, err := svc.store.GetWindow(w2.ID)
	if err != nil {
		t.Fatalf("get w2: %v", err)
	}
	if got.Status != model.WindowNonconverged {
		t.Fatalf("persisted w2 status = %s, want nonconverged", got.Status)
	}

	// The researcher's view of the window list must show the same status.
	listed, err := svc.ListWindows(batch.ID)
	if err != nil {
		t.Fatalf("list windows: %v", err)
	}
	for _, lw := range listed {
		if lw.ID == w2.ID && lw.Status != model.WindowNonconverged {
			t.Fatalf("listed w2 status = %s, want nonconverged", lw.Status)
		}
	}

	// w1 should be corrected, not raw.
	got1, err := svc.store.GetWindow(w1.ID)
	if err != nil {
		t.Fatalf("get w1: %v", err)
	}
	if got1.Status != model.WindowCorrected {
		t.Fatalf("persisted w1 status = %s, want corrected", got1.Status)
	}
}

// TestRunDiagnosisPreservesExcludedWindow ensures excluded windows keep their
// status and are not touched by a diagnosis run.
func TestRunDiagnosisPreservesExcludedWindow(t *testing.T) {
	st := mustOpenStore(t)
	svc := New(st)

	batch, err := svc.CreateBatch("ex", "umbrella", 300.0)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	w1, err := svc.AddWindow(batch.ID, "w1", 0, 10.0)
	if err != nil {
		t.Fatalf("add w1: %v", err)
	}
	w2, err := svc.AddWindow(batch.ID, "w2", 5, 10.0)
	if err != nil {
		t.Fatalf("add w2: %v", err)
	}

	var s1 []ImportSample
	for j := 0; j < 200; j++ {
		s1 = append(s1, ImportSample{Seq: j + 1, Energy: float64(j%40-20) * 0.15, Bias: 0.5})
	}
	if _, err := svc.ImportSamples(w1.ID, s1); err != nil {
		t.Fatalf("import w1: %v", err)
	}
	if _, err := svc.ImportSamples(w2.ID, s1); err != nil {
		t.Fatalf("import w2: %v", err)
	}

	// Mark w2 excluded via the operator status endpoint.
	if _, err := svc.SetWindowStatus(w2.ID, model.WindowExcluded); err != nil {
		t.Fatalf("exclude w2: %v", err)
	}

	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatalf("diagnose: %v", err)
	}

	got, err := svc.store.GetWindow(w2.ID)
	if err != nil {
		t.Fatalf("get w2: %v", err)
	}
	if got.Status != model.WindowExcluded {
		t.Fatalf("excluded w2 status = %s, want excluded (must be preserved)", got.Status)
	}
}

// TestCreateSnapshotSyncsWindowStatus reproduces the reported bug: when a
// snapshot is created (which recomputes the diagnosis report and embeds the
// window conclusions in the snapshot content), the persisted window detail
// must agree with that conclusion instead of staying raw. The researcher reads
// the diagnosis from the snapshot but browses the window list to pick windows
// needing resampling.
func TestCreateSnapshotSyncsWindowStatus(t *testing.T) {
	st := mustOpenStore(t)
	svc := New(st)

	batch, err := svc.CreateBatch("snap-sync", "umbrella", 300.0)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	w1, err := svc.AddWindow(batch.ID, "w1", 0, 10.0)
	if err != nil {
		t.Fatalf("add w1: %v", err)
	}
	w2, err := svc.AddWindow(batch.ID, "w2", 5, 10.0)
	if err != nil {
		t.Fatalf("add w2: %v", err)
	}

	// w1 converges.
	var s1 []ImportSample
	for j := 0; j < 200; j++ {
		s1 = append(s1, ImportSample{Seq: j + 1, Energy: float64(j%40-20) * 0.15, Bias: 0.5})
	}
	if _, err := svc.ImportSamples(w1.ID, s1); err != nil {
		t.Fatalf("import w1: %v", err)
	}
	// w2 nonconverged: bias/kT overflows the reweight cap -> all-zero weights ->
	// normalization fails -> Converged=false.
	var s2 []ImportSample
	for j := 0; j < 50; j++ {
		s2 = append(s2, ImportSample{Seq: j + 1, Energy: 5, Bias: 1e9})
	}
	if _, err := svc.ImportSamples(w2.ID, s2); err != nil {
		t.Fatalf("import w2: %v", err)
	}

	// Create a snapshot WITHOUT calling RunDiagnosis first: the snapshot
	// recomputes the report and embeds the window conclusion.
	sn, err := svc.CreateSnapshot(batch.ID, "snap")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if got, _ := svc.store.GetWindow(w2.ID); got.Status != model.WindowNonconverged {
		t.Fatalf("after snapshot, persisted w2 status = %s, want nonconverged (must match the snapshot report)", got.Status)
	}
	if got, _ := svc.store.GetWindow(w1.ID); got.Status != model.WindowCorrected {
		t.Fatalf("after snapshot, persisted w1 status = %s, want corrected", got.Status)
	}
	// The window list the researcher browses must agree.
	listed, err := svc.ListWindows(batch.ID)
	if err != nil {
		t.Fatalf("list windows: %v", err)
	}
	for _, lw := range listed {
		switch lw.ID {
		case w2.ID:
			if lw.Status != model.WindowNonconverged {
				t.Fatalf("listed w2 status = %s, want nonconverged", lw.Status)
			}
		case w1.ID:
			if lw.Status != model.WindowCorrected {
				t.Fatalf("listed w1 status = %s, want corrected", lw.Status)
			}
		}
	}
	// Sanity: the snapshot content itself reports w2 nonconverged.
	var parsed model.DiagnosisReport
	if err := json.Unmarshal([]byte(sn.Snapshot), &parsed); err != nil {
		t.Fatalf("snapshot content not valid JSON: %v", err)
	}
	var w2sum *model.WindowSummary
	for i := range parsed.Windows {
		if parsed.Windows[i].ID == w2.ID {
			w2sum = &parsed.Windows[i]
		}
	}
	if w2sum == nil || w2sum.Status != model.WindowNonconverged {
		t.Fatalf("snapshot report w2 status = %+v, want nonconverged", w2sum)
	}
}

// TestCreateSnapshotPreservesExcludedWindow ensures excluded windows keep
// their status when a snapshot is created.
func TestCreateSnapshotPreservesExcludedWindow(t *testing.T) {
	st := mustOpenStore(t)
	svc := New(st)

	batch, err := svc.CreateBatch("snap-ex", "umbrella", 300.0)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	w1, err := svc.AddWindow(batch.ID, "w1", 0, 10.0)
	if err != nil {
		t.Fatalf("add w1: %v", err)
	}
	w2, err := svc.AddWindow(batch.ID, "w2", 5, 10.0)
	if err != nil {
		t.Fatalf("add w2: %v", err)
	}

	var s1 []ImportSample
	for j := 0; j < 200; j++ {
		s1 = append(s1, ImportSample{Seq: j + 1, Energy: float64(j%40-20) * 0.15, Bias: 0.5})
	}
	if _, err := svc.ImportSamples(w1.ID, s1); err != nil {
		t.Fatalf("import w1: %v", err)
	}
	if _, err := svc.ImportSamples(w2.ID, s1); err != nil {
		t.Fatalf("import w2: %v", err)
	}
	if _, err := svc.SetWindowStatus(w2.ID, model.WindowExcluded); err != nil {
		t.Fatalf("exclude w2: %v", err)
	}

	if _, err := svc.CreateSnapshot(batch.ID, "snap"); err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if got, _ := svc.store.GetWindow(w2.ID); got.Status != model.WindowExcluded {
		t.Fatalf("after snapshot, excluded w2 status = %s, want excluded (must be preserved)", got.Status)
	}
}
