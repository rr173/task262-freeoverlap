package service

import (
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

// buildAndPublishSealed drives a batch through the real publish flow so its
// terminal status is BatchSealed exactly as a researcher would observe it.
func buildAndPublishSealed(t *testing.T) (*Service, *store.Store, *model.CalcBatch, []*model.SamplingWindow) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st)

	batch, err := svc.CreateBatch("seal-batch", "umbrella", 300.0)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	labels := []string{"w0", "w1"}
	centers := []float64{0, 5}
	var wins []*model.SamplingWindow
	for i, label := range labels {
		w, err := svc.AddWindow(batch.ID, label, centers[i], 10.0)
		if err != nil {
			t.Fatalf("add window %s: %v", label, err)
		}
		wins = append(wins, w)
	}
	for _, w := range wins {
		var samples []ImportSample
		for j := 0; j < 60; j++ {
			samples = append(samples, ImportSample{
				Seq: j + 1, Energy: w.Center + float64(j%30-15)*0.2, Bias: 0.5,
			})
		}
		if _, err := svc.ImportSamples(w.ID, samples); err != nil {
			t.Fatalf("import samples %s: %v", w.ID, err)
		}
		if _, err := svc.CorrectWindow(w.ID); err != nil {
			t.Fatalf("correct window %s: %v", w.ID, err)
		}
	}
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatalf("diagnose: %v", err)
	}
	sn, err := svc.CreateSnapshot(batch.ID, "v1")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if _, err := svc.PublishSnapshot(sn.ID); err != nil {
		t.Fatalf("publish snapshot: %v", err)
	}
	b2, _ := svc.GetBatch(batch.ID)
	if b2.Status != model.BatchSealed {
		t.Fatalf("batch not sealed after publish: %s", b2.Status)
	}
	return svc, st, batch, wins
}

// After a snapshot is published the batch is sealed; no window/sample/edge
// mutation may land, or the persisted data would drift from the frozen
// conclusion. This regression covers every write entrypoint.
func TestSealedBatchRejectsAllServiceWrites(t *testing.T) {
	svc, st, batch, wins := buildAndPublishSealed(t)

	samplesBefore, _ := st.CountSamples(wins[0].ID)
	publishedBefore, _ := svc.ListEdges(batch.ID)
	mustNoMutation(t, svc, st, batch, wins, samplesBefore, publishedBefore)
}

func mustNoMutation(t *testing.T, svc *Service, st *store.Store, batch *model.CalcBatch, wins []*model.SamplingWindow, samplesBefore int, edgesBefore []*model.WindowEdge) {
	checkImmutable := func(name string, err error) {
		t.Helper()
		if !model.IsKind(err, model.ErrImmutable) {
			t.Fatalf("%s on sealed batch: err=%v, want ErrImmutable", name, err)
		}
	}

	// Sample/window writes.
	_, err := svc.ImportSamples(wins[0].ID, []ImportSample{{Seq: 999, Energy: 1.0, Bias: 0.5}})
	checkImmutable("ImportSamples", err)
	if _, err := svc.AddWindow(batch.ID, "wX", 10, 10.0); !model.IsKind(err, model.ErrImmutable) {
		t.Fatalf("AddWindow on sealed batch: err=%v, want ErrImmutable", err)
	}
	if _, err := svc.CorrectWindow(wins[0].ID); !model.IsKind(err, model.ErrImmutable) {
		t.Fatalf("CorrectWindow on sealed batch: err=%v, want ErrImmutable", err)
	}
	if _, err := svc.SetWindowStatus(wins[0].ID, model.WindowExcluded); !model.IsKind(err, model.ErrImmutable) {
		t.Fatalf("SetWindowStatus on sealed batch: err=%v, want ErrImmutable", err)
	}

	// Diagnosis and snapshot creation are read-producers, but they would mutate
	// the persisted edge view / create new evidence, so they must be blocked.
	if _, err := svc.RunDiagnosis(batch.ID); !model.IsKind(err, model.ErrImmutable) {
		t.Fatalf("RunDiagnosis on sealed batch: err=%v, want ErrImmutable", err)
	}
	if _, err := svc.CreateSnapshot(batch.ID, "v2"); !model.IsKind(err, model.ErrImmutable) {
		t.Fatalf("CreateSnapshot on sealed batch: err=%v, want ErrImmutable", err)
	}

	// Edge adjudication previously slipped through; it must now be blocked.
	if len(edgesBefore) > 0 {
		_, err := svc.AdjudicateEdge(edgesBefore[0].ID, model.EdgeResample, "post-seal")
		checkImmutable("AdjudicateEdge", err)
		edgesAfter, _ := svc.ListEdges(batch.ID)
		if edgesAfter[0].Status != edgesBefore[0].Status {
			t.Fatalf("edge status changed on sealed batch: %s -> %s",
				edgesBefore[0].Status, edgesAfter[0].Status)
		}
	}

	// Nothing may have been written underneath.
	samplesAfter, _ := st.CountSamples(wins[0].ID)
	if samplesAfter != samplesBefore {
		t.Fatalf("sample count changed on sealed batch: %d -> %d", samplesBefore, samplesAfter)
	}
}

// Unsealed batches keep their normal editing semantics; the guard does not
// tighten during the receiving/publishable lifecycle.
func TestUnsealedBatchEditsNormally(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	svc := New(st)

	batch, err := svc.CreateBatch("live-batch", "umbrella", 300.0)
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	w0, err := svc.AddWindow(batch.ID, "w0", 0, 10.0)
	if err != nil {
		t.Fatalf("add window: %v", err)
	}
	w1, err := svc.AddWindow(batch.ID, "w1", 5, 10.0)
	if err != nil {
		t.Fatalf("add window: %v", err)
	}
	for _, w := range []*model.SamplingWindow{w0, w1} {
		var samples []ImportSample
		for j := 0; j < 60; j++ {
			samples = append(samples, ImportSample{
				Seq: j + 1, Energy: w.Center + float64(j%30-15)*0.2, Bias: 0.5,
			})
		}
		if _, err := svc.ImportSamples(w.ID, samples); err != nil {
			t.Fatalf("import on unsealed batch: %v", err)
		}
		if _, err := svc.CorrectWindow(w.ID); err != nil {
			t.Fatalf("correct on unsealed batch: %v", err)
		}
	}
	if _, err := svc.SetWindowStatus(w0.ID, model.WindowRaw); err != nil {
		t.Fatalf("set window status on unsealed batch: %v", err)
	}
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatalf("diagnose on unsealed batch: %v", err)
	}
	edges, _ := svc.ListEdges(batch.ID)
	if len(edges) == 0 {
		t.Fatalf("expected edges after diagnosis")
	}
	if _, err := svc.AdjudicateEdge(edges[0].ID, model.EdgeResample, "note"); err != nil {
		t.Fatalf("adjudicate edge on unsealed batch: %v", err)
	}
}
