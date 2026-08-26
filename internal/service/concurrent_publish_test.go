package service

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func setupSealableBatch(t *testing.T, svc *Service) *model.CalcBatch {
	t.Helper()
	batch, err := svc.CreateBatch("repro-"+t.Name(), "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range []float64{0, 5, 10} {
		if _, err := svc.AddWindow(batch.ID, fmt.Sprintf("w%d", i), c, 10.0); err != nil {
			t.Fatal(err)
		}
	}
	windows, _ := svc.ListWindows(batch.ID)
	for _, w := range windows {
		var samples []ImportSample
		for j := 0; j < 60; j++ {
			e := w.Center + float64(j%30-15)*0.2
			samples = append(samples, ImportSample{Seq: j + 1, Energy: e, Bias: 0.5})
		}
		if _, err := svc.ImportSamples(w.ID, samples); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CorrectWindow(w.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.RunDiagnosis(batch.ID); err != nil {
		t.Fatal(err)
	}
	return batch
}

// TestConcurrentPublishSameDraft: 同一份草稿快照被并发发布，
// 恰好一次成功，其余明确冲突。
func TestConcurrentPublishSameDraft(t *testing.T) {
	for i := 0; i < 20; i++ { // 多轮复跑压竞态
		st, err := store.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		svc := New(st)
		batch := setupSealableBatch(t, svc)
		sn, err := svc.CreateSnapshot(batch.ID, "snap")
		if err != nil {
			t.Fatal(err)
		}
		const N = 30
		var ok, conflict, other int32
		var okIDs []string
		var mu sync.Mutex
		var wg sync.WaitGroup
		start := make(chan struct{})
		for g := 0; g < N; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				published, err := svc.PublishSnapshot(sn.ID)
				switch {
				case err == nil:
					atomic.AddInt32(&ok, 1)
					mu.Lock()
					okIDs = append(okIDs, published.ID)
					mu.Unlock()
				case model.IsKind(err, model.ErrConflict):
					atomic.AddInt32(&conflict, 1)
				default:
					atomic.AddInt32(&other, 1)
					t.Logf("round %d other err: %v", i, err)
				}
			}()
		}
		close(start)
		wg.Wait()

		if ok != 1 {
			t.Fatalf("round %d: expected exactly 1 successful publish, got ok=%d conflict=%d other=%d", i, ok, conflict, other)
		}
		if other != 0 {
			t.Fatalf("round %d: all losers must conflict, got other=%d", i, other)
		}
		if ok+conflict != int32(N) {
			t.Fatalf("round %d: missing responses: ok=%d conflict=%d", i, ok, conflict)
		}
		// 批次必须进入封存，快照必须为已发布且冻结。
		b, _ := svc.GetBatch(batch.ID)
		if b.Status != model.BatchSealed {
			t.Fatalf("round %d: batch not sealed: %s", i, b.Status)
		}
		s, _ := svc.ListSnapshots(batch.ID)
		publishedCount := 0
		for _, x := range s {
			if x.Status == model.SnapshotPublished {
				publishedCount++
			}
		}
		if publishedCount != 1 {
			t.Fatalf("round %d: expected exactly 1 published snapshot, got %d", i, publishedCount)
		}
		st.Close()
	}
}

// TestConcurrentPublishMultipleDrafts: 同一批次的多份不同草稿快照被并发发布，
// 仍只有一份成功冻结并封存批次，其余明确冲突。
func TestConcurrentPublishMultipleDrafts(t *testing.T) {
	for i := 0; i < 20; i++ {
		st, err := store.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		svc := New(st)
		batch := setupSealableBatch(t, svc)
		var snapIDs []string
		for k := 0; k < 6; k++ {
			sn, err := svc.CreateSnapshot(batch.ID, fmt.Sprintf("snap-%d", k))
			if err != nil {
				t.Fatal(err)
			}
			snapIDs = append(snapIDs, sn.ID)
		}

		var ok, conflict, other int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, id := range snapIDs {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				<-start
				_, err := svc.PublishSnapshot(id)
				switch {
				case err == nil:
					atomic.AddInt32(&ok, 1)
				case model.IsKind(err, model.ErrConflict):
					atomic.AddInt32(&conflict, 1)
				default:
					atomic.AddInt32(&other, 1)
					t.Logf("round %d other err for %s: %v", i, id, err)
				}
			}(id)
		}
		close(start)
		wg.Wait()

		if ok != 1 {
			t.Fatalf("round %d: expected exactly 1 successful publish across drafts, got ok=%d conflict=%d other=%d", i, ok, conflict, other)
		}
		if other != 0 {
			t.Fatalf("round %d: losers must conflict, got other=%d", i, other)
		}
		// 所有失败者必须是 conflict（明确冲突），且其快照仍是草稿。
		s, _ := svc.ListSnapshots(batch.ID)
		publishedCount := 0
		for _, x := range s {
			if x.Status == model.SnapshotPublished {
				publishedCount++
			}
		}
		if publishedCount != 1 {
			t.Fatalf("round %d: expected exactly 1 published snapshot, got %d", i, publishedCount)
		}
		st.Close()
	}
}
