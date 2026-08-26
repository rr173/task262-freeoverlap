package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"task262-freeoverlap/internal/service"
	"task262-freeoverlap/internal/store"
)

func publishBody(id string) (*bytes.Reader, string) {
	b := []byte(`{}`)
	_ = id
	return bytes.NewReader(b), "application/json"
}

func TestHTTPConcurrentPublishConflict(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st)
	srv := New(svc)

	// build a publishable batch via direct service calls (mirrors smoke setup)
	batch, err := svc.CreateBatch("httpconf", "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range []float64{0, 5, 10} {
		if _, err := svc.AddWindow(batch.ID, "w"+itoa(i), c, 10.0); err != nil {
			t.Fatal(err)
		}
	}
	ws, _ := svc.ListWindows(batch.ID)
	for _, w := range ws {
		var samples []service.ImportSample
		for j := 0; j < 60; j++ {
			e := w.Center + float64(j%30-15)*0.2
			samples = append(samples, service.ImportSample{Seq: j + 1, Energy: e, Bias: 0.5})
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
	sn, err := svc.CreateSnapshot(batch.ID, "http-snap")
	if err != nil {
		t.Fatal(err)
	}

	const N = 25
	var ok, conflict int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body, ct := publishBody(sn.ID)
			req := httptest.NewRequest("POST", "/api/snapshots/"+sn.ID+"/publish", body)
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			switch rec.Code {
			case 200:
				atomic.AddInt32(&ok, 1)
			case 409:
				atomic.AddInt32(&conflict, 1)
			default:
				b, _ := io.ReadAll(rec.Body)
				t.Logf("unexpected status %d: %s", rec.Code, b)
			}
		}()
	}
	close(start)
	wg.Wait()

	if ok != 1 {
		t.Fatalf("expected exactly one 200, got ok=%d conflict=%d", ok, conflict)
	}
	if ok+conflict != int32(N) {
		t.Fatalf("expected all 200 or 409, got ok=%d conflict=%d", ok, conflict)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	return string(rune('0' + i))
}

func init() { _ = json.Marshal }
