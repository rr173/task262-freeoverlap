package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

// diagTestBatch builds an in-memory batch with the given window centers, importing
// 200 deterministic overlapping samples per window and correcting each one.
func diagTestBatch(t *testing.T, name string, centers []float64) (*Service, *model.CalcBatch, []*model.SamplingWindow) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st)
	batch, err := svc.CreateBatch(name, "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	var ws []*model.SamplingWindow
	for i, c := range centers {
		w, err := svc.AddWindow(batch.ID, fmt.Sprintf("w%d", i), c, 10.0)
		if err != nil {
			t.Fatal(err)
		}
		ws = append(ws, w)
	}
	for _, w := range ws {
		var samples []ImportSample
		for j := 0; j < 200; j++ {
			e := w.Center + float64(j%40-20)*0.15
			samples = append(samples, ImportSample{Seq: j + 1, Energy: e, Bias: 0.5})
		}
		if _, err := svc.ImportSamples(w.ID, samples); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CorrectWindow(w.ID); err != nil {
			t.Fatal(err)
		}
	}
	return svc, batch, ws
}

// reportEdgeIDs derives the (lowerID, upperID) pairs the report describes, mapping the
// report's labels back to window IDs via the currently-active (non-excluded) windows.
func reportEdgeIDs(t *testing.T, svc *Service, batchID string, report *model.DiagnosisReport) map[string]bool {
	t.Helper()
	ws, err := svc.ListWindows(batchID)
	if err != nil {
		t.Fatal(err)
	}
	labelToID := map[string]string{}
	for _, w := range ws {
		labelToID[w.Label] = w.ID
	}
	out := map[string]bool{}
	for _, e := range report.Edges {
		out[labelToID[e.LowerLabel]+"|"+labelToID[e.UpperLabel]] = true
	}
	return out
}

// TestSnapshotRefreshesEdgeListOnExcludedWindow reproduces the reported regression:
// after excluding a window and re-running the diagnosis (here via CreateSnapshot, which
// recomputes the report), the persisted edge list must drop edges that involve the
// excluded window and exactly match the current report's adjacent pairs.
func TestSnapshotRefreshesEdgeListOnExcludedWindow(t *testing.T) {
	svc, batch, ws := diagTestBatch(t, "exclude-snap", []float64{0, 5, 10})

	// First diagnosis: two edges w0-w1 and w1-w2.
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	if edges, _ := svc.ListEdges(batch.ID); len(edges) != 2 {
		t.Fatalf("initial edges = %d, want 2", len(edges))
	}

	// Exclude the middle window w1, then recompute via a snapshot.
	mid := ws[1]
	if _, err := svc.SetWindowStatus(mid.ID, model.WindowExcluded); err != nil {
		t.Fatal(err)
	}
	sn, err := svc.CreateSnapshot(batch.ID, "after-exclude")
	if err != nil {
		t.Fatal(err)
	}
	var report model.DiagnosisReport
	if err := json.Unmarshal([]byte(sn.Snapshot), &report); err != nil {
		t.Fatal(err)
	}
	wantPairs := reportEdgeIDs(t, svc, batch.ID, &report)
	if len(wantPairs) != 1 {
		t.Fatalf("snapshot report edges = %d, want 1 (only the surviving pair)", len(wantPairs))
	}

	edges, err := svc.ListEdges(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != len(wantPairs) {
		t.Fatalf("edge list = %d, want %d to match current report", len(edges), len(wantPairs))
	}
	gotPairs := map[string]bool{}
	for _, e := range edges {
		key := e.LowerWindowID + "|" + e.UpperWindowID
		gotPairs[key] = true
		if e.LowerWindowID == mid.ID || e.UpperWindowID == mid.ID {
			t.Errorf("stale edge involving excluded window persisted: %s", key)
		}
	}
	for key := range wantPairs {
		if !gotPairs[key] {
			t.Errorf("current report edge %s missing from edge list", key)
		}
	}
}

// TestRerunDiagnosisDropsExcludedWindowEdges guards the diagnose-rerun path: excluding a
// window and re-running the diagnosis must remove its edges from the persisted list.
func TestRerunDiagnosisDropsExcludedWindowEdges(t *testing.T) {
	svc, batch, ws := diagTestBatch(t, "exclude-rerun", []float64{0, 5, 10})

	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	mid := ws[1]
	if _, err := svc.SetWindowStatus(mid.ID, model.WindowExcluded); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	edges, _ := svc.ListEdges(batch.ID)
	if len(edges) != 1 {
		t.Fatalf("after exclude+rerun: edges = %d, want 1", len(edges))
	}
	for _, e := range edges {
		if e.LowerWindowID == mid.ID || e.UpperWindowID == mid.ID {
			t.Errorf("stale edge involving excluded window persisted: %+v", e)
		}
	}
}

// TestSnapshotPreservesResampleAdjudication ensures that when a surviving edge carried a
// prior resample adjudication, recomputing the diagnosis (via snapshot) preserves the
// researcher's disposition and note while recomputing the overlap.
func TestSnapshotPreservesResampleAdjudication(t *testing.T) {
	svc, batch, ws := diagTestBatch(t, "preserve-adjud", []float64{0, 5, 10, 15})

	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	// Adjudicate the w1-w2 edge to resample.
	edges, _ := svc.ListEdges(batch.ID)
	var targetID string
	for _, e := range edges {
		if e.LowerWindowID == ws[1].ID && e.UpperWindowID == ws[2].ID {
			targetID = e.ID
		}
	}
	if targetID == "" {
		t.Fatal("could not find w1-w2 edge to adjudicate")
	}
	if _, err := svc.AdjudicateEdge(targetID, model.EdgeResample, "needs more sampling"); err != nil {
		t.Fatal(err)
	}
	// Exclude an unrelated window (w0); w1-w2 survives and must keep its adjudication.
	if _, err := svc.SetWindowStatus(ws[0].ID, model.WindowExcluded); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSnapshot(batch.ID, "after-exclude-unrelated"); err != nil {
		t.Fatal(err)
	}
	edges2, _ := svc.ListEdges(batch.ID)
	var survivor *model.WindowEdge
	for _, e := range edges2 {
		if e.LowerWindowID == ws[1].ID && e.UpperWindowID == ws[2].ID {
			survivor = e
		}
	}
	if survivor == nil {
		t.Fatal("surviving w1-w2 edge disappeared")
	}
	if survivor.Status != model.EdgeResample {
		t.Errorf("surviving edge lost adjudication: status=%s, want resample", survivor.Status)
	}
	if survivor.Note != "needs more sampling" {
		t.Errorf("surviving edge lost note: %q", survivor.Note)
	}
}
