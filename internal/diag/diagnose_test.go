package diag

import (
	"testing"

	"task262-freeoverlap/internal/model"
)

// fakeLoader 提供内存中的窗口与样本。
type fakeLoader struct {
	windows map[string]*model.SamplingWindow
	samples map[string][]*model.EnergySample
}

func (f *fakeLoader) ListWindows(batchID string) ([]*model.SamplingWindow, error) {
	var out []*model.SamplingWindow
	for _, w := range f.windows {
		if w.BatchID == batchID {
			out = append(out, w)
		}
	}
	return out, nil
}

func (f *fakeLoader) ListSamples(windowID string) ([]*model.EnergySample, error) {
	return f.samples[windowID], nil
}

func mkEnergy(start, step float64, n int) []*model.EnergySample {
	out := make([]*model.EnergySample, n)
	for i := range out {
		out[i] = &model.EnergySample{ID: "s", Energy: start + step*float64(i), Bias: 0.5}
	}
	return out
}

func TestDiagnoseConverged(t *testing.T) {
	loader := &fakeLoader{
		windows: map[string]*model.SamplingWindow{
			"w1": {ID: "w1", BatchID: "b1", Label: "w1", Center: 0, SpringConst: 1, Status: model.WindowRaw},
			"w2": {ID: "w2", BatchID: "b1", Label: "w2", Center: 5, SpringConst: 1, Status: model.WindowRaw},
		},
		samples: map[string][]*model.EnergySample{
			"w1": mkEnergy(0, 0.1, 100),
			"w2": mkEnergy(3, 0.1, 100), // 与 w1 部分重叠
		},
	}
	batch := &model.CalcBatch{ID: "b1", KT: 2.5}
	report, err := Diagnose(loader, nil, batch)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalWindows != 2 || len(report.Edges) != 1 {
		t.Fatalf("report wrong: %+v", report)
	}
	if report.MinOverlap <= 0 {
		t.Fatalf("overlapping windows should have positive overlap: %+v", report)
	}
}

func TestDiagnoseGap(t *testing.T) {
	loader := &fakeLoader{
		windows: map[string]*model.SamplingWindow{
			"w1": {ID: "w1", BatchID: "b1", Label: "w1", Center: 0, SpringConst: 1, Status: model.WindowRaw},
			"w2": {ID: "w2", BatchID: "b1", Label: "w2", Center: 100, SpringConst: 1, Status: model.WindowRaw},
		},
		samples: map[string][]*model.EnergySample{
			"w1": mkEnergy(0, 0.1, 50),
			"w2": mkEnergy(100, 0.1, 50), // 完全分离 -> 断层
		},
	}
	batch := &model.CalcBatch{ID: "b1", KT: 2.5}
	report, err := Diagnose(loader, nil, batch)
	if err != nil {
		t.Fatal(err)
	}
	if report.GapEdges != 1 || report.Converged {
		t.Fatalf("expect 1 gap edge and unconverged: %+v", report)
	}
	if report.Edges[0].Status != string(model.EdgeGap) {
		t.Fatalf("edge should be gap: %+v", report.Edges[0])
	}
}

func TestDiagnoseExcludedWindow(t *testing.T) {
	loader := &fakeLoader{
		windows: map[string]*model.SamplingWindow{
			"w1": {ID: "w1", BatchID: "b1", Label: "w1", Center: 0, SpringConst: 1, Status: model.WindowRaw},
			"w2": {ID: "w2", BatchID: "b1", Label: "w2", Center: 5, SpringConst: 1, Status: model.WindowExcluded},
		},
		samples: map[string][]*model.EnergySample{
			"w1": mkEnergy(0, 0.1, 50),
			"w2": mkEnergy(3, 0.1, 50),
		},
	}
	batch := &model.CalcBatch{ID: "b1", KT: 2.5}
	report, err := Diagnose(loader, nil, batch)
	if err != nil {
		t.Fatal(err)
	}
	if report.Excluded != 1 || len(report.Edges) != 0 {
		t.Fatalf("excluded window should not create edges: %+v", report)
	}
}

func TestEdgesFromReport(t *testing.T) {
	report := &model.DiagnosisReport{
		BatchID: "b1",
		Edges: []model.EdgeSummary{
			{LowerLabel: "a", UpperLabel: "b", Overlap: 0.9, Status: string(model.EdgeSufficient)},
		},
	}
	edges := EdgesFromReport(report, []string{"w1"}, []string{"w2"})
	if len(edges) != 1 || edges[0].LowerWindowID != "w1" || edges[0].Status != model.EdgeSufficient {
		t.Fatalf("edges wrong: %+v", edges)
	}
}

func TestRenderSnapshot(t *testing.T) {
	report := &model.DiagnosisReport{BatchID: "b1", Converged: true}
	s, err := RenderSnapshot(report)
	if err != nil || len(s) == 0 {
		t.Fatalf("render failed: %v", err)
	}
}
