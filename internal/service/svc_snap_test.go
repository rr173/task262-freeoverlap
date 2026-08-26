package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

func newSvcT5(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func importNearT5(t *testing.T, svc *Service, w *model.SamplingWindow, base float64) {
	t.Helper()
	var samples []ImportSample
	for j := 0; j < 60; j++ {
		samples = append(samples, ImportSample{Seq: j + 1, Energy: base + float64(j%20-10)*0.15, Bias: 0.5})
	}
	if _, err := svc.ImportSamples(w.ID, samples); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := svc.CorrectWindow(w.ID); err != nil {
		t.Fatalf("correct: %v", err)
	}
}

// TestSnapshotPreservesAdjudication: 裁决后创建快照，快照内容应与边列表一致
// （包含 resample 裁决与说明），而非恢复成普通断层。
func TestSnapshotPreservesAdjudication(t *testing.T) {
	svc := newSvcT5(t)
	batch, err := svc.CreateBatch("b", "umbrella", 300.0)
	if err != nil {
		t.Fatal(err)
	}
	w1, _ := svc.AddWindow(batch.ID, "w1", 0, 10.0)
	w2, _ := svc.AddWindow(batch.ID, "w2", 100, 10.0)
	importNearT5(t, svc, w1, 0)
	importNearT5(t, svc, w2, 100)

	rep1, err := svc.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatalf("diag1: %v", err)
	}
	if rep1.GapEdges != 1 {
		t.Fatalf("expect 1 gap, got %+v", rep1)
	}
	edges1, _ := svc.ListEdges(batch.ID)
	if _, err := svc.AdjudicateEdge(edges1[0].ID, model.EdgeResample, "需补采样"); err != nil {
		t.Fatalf("adjudicate: %v", err)
	}

	// 快照内容应反映裁决（与边列表一致）。
	sn, err := svc.CreateSnapshot(batch.ID, "s1")
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	var parsed model.DiagnosisReport
	if err := json.Unmarshal([]byte(sn.Snapshot), &parsed); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	t.Logf("snapshot edges: %+v", parsed.Edges)
	t.Logf("snapshot GapEdges=%d ResampleEdges=%d Converged=%v", parsed.GapEdges, parsed.ResampleEdges, parsed.Converged)

	// 持久化边应为 resample + note。
	edges2, _ := svc.ListEdges(batch.ID)
	if len(edges2) != 1 || edges2[0].Status != model.EdgeResample || edges2[0].Note != "需补采样" {
		t.Fatalf("stored edge inconsistent with adjudication: %+v", edges2)
	}
	// 快照报告应与边列表一致。
	if len(parsed.Edges) != 1 || parsed.Edges[0].Status != string(model.EdgeResample) {
		t.Fatalf("snapshot report lost adjudication: %+v", parsed.Edges)
	}
	if parsed.Edges[0].Note != "需补采样" {
		t.Fatalf("snapshot report note lost: %+v", parsed.Edges[0])
	}
	if parsed.ResampleEdges != 1 {
		t.Fatalf("snapshot ResampleEdges = %d, want 1", parsed.ResampleEdges)
	}
	if parsed.GapEdges != 0 {
		t.Fatalf("snapshot GapEdges = %d, want 0", parsed.GapEdges)
	}

	// 再跑一次 RunDiagnosis：报告与边列表按相邻关系（lower/upper label）逐条一致，
	// 且重采样裁决与说明在两处都保留。
	rep2, err := svc.RunDiagnosis(batch.ID)
	if err != nil {
		t.Fatalf("diag2: %v", err)
	}
	edges3, _ := svc.ListEdges(batch.ID)
	if len(rep2.Edges) != len(edges3) {
		t.Fatalf("rep2 edges %d != stored %d", len(rep2.Edges), len(edges3))
	}
	// 持久化边按 lower/upper window label 建索引（边列表按哈希排序，不能按下标比对）。
	winByID := map[string]*model.SamplingWindow{}
	ws, _ := svc.ListWindows(batch.ID)
	for _, w := range ws {
		winByID[w.ID] = w
	}
	storedByLabel := map[string]*model.WindowEdge{}
	for _, e := range edges3 {
		lo := winByID[e.LowerWindowID].Label
		hi := winByID[e.UpperWindowID].Label
		storedByLabel[lo+"-"+hi] = e
	}
	for _, re := range rep2.Edges {
		se := storedByLabel[re.LowerLabel+"-"+re.UpperLabel]
		if se == nil {
			t.Fatalf("report edge %s-%s missing in stored", re.LowerLabel, re.UpperLabel)
		}
		if string(se.Status) != re.Status {
			t.Errorf("edge %s-%s: report status=%s stored=%s", re.LowerLabel, re.UpperLabel, re.Status, se.Status)
		}
		if se.Note != re.Note {
			t.Errorf("edge %s-%s: report note=%q stored=%q", re.LowerLabel, re.UpperLabel, re.Note, se.Note)
		}
	}
	if rep2.Edges[0].Status != string(model.EdgeResample) || rep2.Edges[0].Note != "需补采样" {
		t.Fatalf("rep2 lost adjudication: %+v", rep2.Edges[0])
	}
}
