// Package diag 编排自由能窗口重叠诊断：加载窗口与样本、做偏置校正、
// 计算相邻窗口重叠、定位断层与未收敛窗口、汇总诊断报告。
package diag

import (
	"encoding/json"
	"sort"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/overlap"
	"task262-freeoverlap/internal/weight"
)

// Loader 抽象数据访问，便于诊断层与 store 解耦。
type Loader interface {
	ListWindows(batchID string) ([]*model.SamplingWindow, error)
	ListSamples(windowID string) ([]*model.EnergySample, error)
}

// WindowResolver 供诊断层解析窗口 ID -> 窗口实体（用于标签）。
type WindowResolver func(windowID string) (*model.SamplingWindow, error)

// Diagnose 对批次执行完整诊断：
//  1. 加载全部窗口（按中心坐标排序），排除 excluded 窗口；
//  2. 对每个有效窗口加载样本，做偏置校正（重加权）；
//  3. 对相邻有效窗口计算分布重叠；
//  4. 汇总报告：断层边、需重采样边、未收敛窗口、最小/平均重叠、收敛性。
//
// 返回报告与相邻窗口边判定列表。不修改持久化状态，由调用方决定如何落库。
func Diagnose(loader Loader, resolve WindowResolver, batch *model.CalcBatch) (*model.DiagnosisReport, error) {
	windows, err := loader.ListWindows(batch.ID)
	if err != nil {
		return nil, err
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Center < windows[j].Center })

	var active []*model.SamplingWindow
	for _, w := range windows {
		if w.Status != model.WindowExcluded {
			active = append(active, w)
		}
	}

	report := &model.DiagnosisReport{
		BatchID:      batch.ID,
		TotalWindows: len(windows),
		Excluded:     len(windows) - len(active),
		GeneratedAt:  model.NowMillis(),
	}

	// 校正每个窗口并收集能量样本。
	windowSamples := make(map[string][]*model.EnergySample, len(active))
	nonconverged := 0
	for _, w := range active {
		samples, err := loader.ListSamples(w.ID)
		if err != nil {
			return nil, err
		}
		p := weight.BiasParams{Center: w.Center, SpringConst: w.SpringConst, KT: batch.KT}
		corrected, summary := weight.CorrectWindow(samples, p)
		windowSamples[w.ID] = corrected
		status := model.WindowCorrected
		if len(corrected) == 0 || !summary.Converged {
			nonconverged++
			status = model.WindowNonconverged
		}
		report.Windows = append(report.Windows, model.WindowSummary{
			ID:          w.ID,
			Label:       w.Label,
			Status:      status,
			SampleCount: len(corrected),
			ESS:         summary.ESS,
			Converged:   summary.Converged,
		})
	}
	report.Nonconverged = nonconverged

	// 相邻窗口重叠。
	var overlaps []float64
	var edgeSummaries []model.EdgeSummary
	for i := 0; i+1 < len(active); i++ {
		lo, hi := active[i], active[i+1]
		o, sufficient, gap := overlap.PairOverlap(windowSamples[lo.ID], windowSamples[hi.ID])
		overlaps = append(overlaps, o)
		status := model.EdgeCandidate
		switch {
		case gap:
			status = model.EdgeGap
			report.GapEdges++
		case sufficient:
			status = model.EdgeSufficient
		default:
			status = model.EdgeCandidate
		}
		note := ""
		if gap {
			note = "分布无交集，重加权在此处不可靠"
		}
		edgeSummaries = append(edgeSummaries, model.EdgeSummary{
			LowerLabel: lo.Label,
			UpperLabel: hi.Label,
			Overlap:    o,
			Status:     string(status),
			Note:       note,
		})
	}
	if len(overlaps) > 0 {
		report.MeanOverlap, report.MinOverlap = overlap.Summary(overlaps)
	}
	report.Edges = edgeSummaries
	report.Converged = report.GapEdges == 0 && report.Nonconverged == 0 && len(active) >= 2
	return report, nil
}

// EdgesFromReport 把诊断报告中的边摘要转换为可落库的 WindowEdge 列表。
// 需要提供相邻窗口对的 ID 映射。
func EdgesFromReport(report *model.DiagnosisReport, lowerIDs, upperIDs []string) []*model.WindowEdge {
	if len(report.Edges) != len(lowerIDs) || len(report.Edges) != len(upperIDs) {
		return nil
	}
	out := make([]*model.WindowEdge, 0, len(report.Edges))
	for i, es := range report.Edges {
		out = append(out, &model.WindowEdge{
			ID:            model.EdgeID(report.BatchID, lowerIDs[i], upperIDs[i]),
			BatchID:       report.BatchID,
			LowerWindowID: lowerIDs[i],
			UpperWindowID: upperIDs[i],
			Overlap:       es.Overlap,
			Status:        model.EdgeStatus(es.Status),
			Note:          es.Note,
			CreatedAt:     model.NowMillis(),
		})
	}
	return out
}

// PreserveAdjudications carries explicit resampling decisions into a refreshed
// diagnosis. The numerical overlap is recomputed, while the researcher's
// disposition and note remain authoritative for the same adjacent pair.
func PreserveAdjudications(report *model.DiagnosisReport, current, previous []*model.WindowEdge) {
	priorByID := make(map[string]*model.WindowEdge, len(previous))
	for _, edge := range previous {
		priorByID[edge.ID] = edge
	}
	for i, edge := range current {
		prior := priorByID[edge.ID]
		if prior == nil || prior.Status != model.EdgeResample {
			continue
		}
		if edge.Status == model.EdgeGap && report.GapEdges > 0 {
			report.GapEdges--
		}
		edge.Status = model.EdgeResample
		edge.Note = prior.Note
		report.Edges[i].Status = string(model.EdgeResample)
		report.Edges[i].Note = prior.Note
		report.ResampleEdges++
	}
	if report.ResampleEdges > 0 {
		report.Converged = false
	}
}

// StatusFromReport derives the batch classification a diagnosis implies: a
// converged report (no gaps, no unconverged windows, enough active windows)
// is publishable; anything else is insufficient. This is the single source of
// truth used both when persisting a fresh diagnosis and when re-deriving
// publishability from the report baked into a snapshot.
func StatusFromReport(report *model.DiagnosisReport) model.BatchStatus {
	if report.Converged {
		return model.BatchPublishable
	}
	return model.BatchInsufficient
}

// RenderSnapshot 把诊断报告序列化为快照内容。
func RenderSnapshot(report *model.DiagnosisReport) (string, error) {
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
