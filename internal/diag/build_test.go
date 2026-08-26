package diag

import (
	"testing"

	"task262-freeoverlap/internal/model"
)

// mkActive 构造按 center 升序的两个有效窗口。
func mkActive() []*model.SamplingWindow {
	return []*model.SamplingWindow{
		{ID: "w1", Label: "w1", Center: 0},
		{ID: "w2", Label: "w2", Center: 5},
	}
}

// TestBuildEdgesWithAdjudicationsPreserves 断言：上一轮已落库的 resample 裁决，
// 经 BuildEdgesWithAdjudications 合并后，报告与返回的边都保留裁决与说明，
// 且 GapEdges 不再把该边计入、ResampleEdges 准确。
func TestBuildEdgesWithAdjudicationsPreserves(t *testing.T) {
	active := mkActive()
	pairID := model.EdgeID("b1", "w1", "w2")
	previous := []*model.WindowEdge{
		{ID: pairID, Status: model.EdgeResample, Note: "需补采样"},
	}
	report := &model.DiagnosisReport{
		BatchID:  "b1",
		GapEdges: 1,
		Edges: []model.EdgeSummary{
			{LowerLabel: "w1", UpperLabel: "w2", Overlap: 0.01,
				Status: string(model.EdgeGap), Note: "分布无交集，重加权在此处不可靠"},
		},
	}

	current := BuildEdgesWithAdjudications(report, active, previous)

	if len(current) != 1 {
		t.Fatalf("current edges = %d, want 1", len(current))
	}
	if current[0].Status != model.EdgeResample {
		t.Fatalf("current edge status = %s, want resample", current[0].Status)
	}
	if current[0].Note != "需补采样" {
		t.Fatalf("current edge note = %q, want 需补采样", current[0].Note)
	}
	if report.Edges[0].Status != string(model.EdgeResample) {
		t.Fatalf("report edge status = %s, want resample", report.Edges[0].Status)
	}
	if report.Edges[0].Note != "需补采样" {
		t.Fatalf("report edge note = %q, want 需补采样", report.Edges[0].Note)
	}
	if report.ResampleEdges != 1 {
		t.Fatalf("ResampleEdges = %d, want 1", report.ResampleEdges)
	}
	if report.GapEdges != 0 {
		t.Fatalf("GapEdges = %d, want 0", report.GapEdges)
	}
	if report.Converged {
		t.Fatalf("report should be unconverged")
	}
}

// TestBuildEdgesWithAdjudicationsNoPrevious 无上一轮裁决时，边保持本轮计算结果。
func TestBuildEdgesWithAdjudicationsNoPrevious(t *testing.T) {
	active := mkActive()
	report := &model.DiagnosisReport{
		BatchID:  "b1",
		GapEdges: 1,
		Edges: []model.EdgeSummary{
			{LowerLabel: "w1", UpperLabel: "w2", Overlap: 0.01,
				Status: string(model.EdgeGap), Note: "分布无交集，重加权在此处不可靠"},
		},
	}
	current := BuildEdgesWithAdjudications(report, active, nil)
	if current[0].Status != model.EdgeGap {
		t.Fatalf("status = %s, want gap", current[0].Status)
	}
	if report.ResampleEdges != 0 || report.GapEdges != 1 {
		t.Fatalf("counts: Resample=%d Gap=%d", report.ResampleEdges, report.GapEdges)
	}
}
