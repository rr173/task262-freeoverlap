package service

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func TestConcurrentSnapshotPublicationCommitsOnce(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := New(st)

	batch, err := s.CreateBatch("probe-concurrent-publish", "umbrella", 300)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		window, err := s.AddWindow(batch.ID, string(rune('a'+i)), float64(i), 10)
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
	if report, err := s.RunDiagnosis(batch.ID); err != nil || !report.Converged {
		t.Fatalf("diagnosis=%+v, err=%v", report, err)
	}
	snapshot, err := s.CreateSnapshot(batch.ID, "concurrent")
	if err != nil {
		t.Fatal(err)
	}

	const participants = 20
	start := make(chan struct{})
	results := make(chan error, participants)
	var ready sync.WaitGroup
	ready.Add(participants)
	for i := 0; i < participants; i++ {
		go func() {
			ready.Done()
			<-start
			_, err := s.PublishSnapshot(snapshot.ID)
			results <- err
		}()
	}
	ready.Wait()
	close(start)

	successes := 0
	for i := 0; i < participants; i++ {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, model.ErrConflict) && !errors.Is(err, model.ErrStateMismatch) {
			t.Fatalf("unexpected publish error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful publications = %d, want exactly 1", successes)
	}
	storedSnapshot, err := st.GetSnapshot(snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedBatch, err := st.GetBatch(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSnapshot.Status != model.SnapshotPublished || storedSnapshot.FrozenAt == 0 || storedBatch.Status != model.BatchSealed {
		t.Fatalf("publication not atomically committed: snapshot=%+v batch=%+v", storedSnapshot, storedBatch)
	}
}
