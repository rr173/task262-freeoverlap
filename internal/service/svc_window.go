package service

import (
	"fmt"
	"math"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/weight"
)

// AddWindow 向批次添加采样窗口。
func (s *Service) AddWindow(batchID, label string, center, springConst float64) (*model.SamplingWindow, error) {
	if _, err := s.mutableBatch(batchID); err != nil {
		return nil, err
	}
	if label == "" {
		return nil, model.E(model.ErrInvalid, "window label required")
	}
	if springConst <= 0 {
		return nil, model.E(model.ErrInvalid, "spring constant must be positive")
	}
	w := &model.SamplingWindow{
		ID:          model.NewID("win", batchID+"|"+label),
		BatchID:     batchID,
		Label:       label,
		Center:      center,
		SpringConst: springConst,
		BiasVersion: 1,
		Status:      model.WindowRaw,
		SampleCount: 0,
		CreatedAt:   model.NowMillis(),
		UpdatedAt:   model.NowMillis(),
	}
	if err := s.store.CreateWindow(w); err != nil {
		return nil, err
	}
	return w, nil
}

// ListWindows 列出批次窗口。
func (s *Service) ListWindows(batchID string) ([]*model.SamplingWindow, error) {
	return s.store.ListWindows(batchID)
}

// SetWindowStatus 设置窗口状态（排除/恢复/标记未收敛）。
func (s *Service) SetWindowStatus(windowID string, status model.WindowStatus) (*model.SamplingWindow, error) {
	if _, _, err := s.mutableWindow(windowID); err != nil {
		return nil, err
	}
	n, err := s.store.CountSamples(windowID)
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateWindowStatus(windowID, status, n); err != nil {
		return nil, err
	}
	return s.store.GetWindow(windowID)
}

// ImportSamples 批量导入窗口样本（按内容哈希幂等去重）。
// 返回新导入数与重复数。
//
// 一次导入要么完整成功要么完全不写入：整批请求在打开写事务之前完成
// 校验，任一非法观测（energy 或 bias 为 NaN/Inf）都使请求整体失败，
// 排在它前面的合法观测不会落库；失败后窗口样本计数保持导入前状态，
// 后续合法导入仍按内容哈希正常幂等去重。
func (s *Service) ImportSamples(windowID string, samples []ImportSample) (*ImportResult, error) {
	if _, _, err := s.mutableWindow(windowID); err != nil {
		return nil, err
	}
	// Validate the complete request before preparing any sample or opening the
	// write transaction. A single bad observation must fail the whole batch so
	// no valid prefix leaks into the window.
	for i, in := range samples {
		if err := validateSample(in); err != nil {
			return nil, model.E(model.ErrInvalid, "window %s sample[%d]: %v", windowID, i, err)
		}
	}
	prepared := make([]*model.EnergySample, 0, len(samples))
	for _, in := range samples {
		sm := &model.EnergySample{
			ID:          model.NewID("smpl", fmt.Sprintf("%s|%d", windowID, in.Seq)),
			WindowID:    windowID,
			Seq:         in.Seq,
			Energy:      in.Energy,
			Bias:        in.Bias,
			Weight:      1.0,
			ContentHash: model.SampleHash(windowID, in.Seq, in.Energy, in.Bias),
			CreatedAt:   model.NowMillis(),
		}
		prepared = append(prepared, sm)
	}
	inserted, err := s.store.InsertSamples(windowID, prepared)
	if err != nil {
		return nil, err
	}
	return &ImportResult{Inserted: inserted, Duplicated: len(samples) - inserted}, nil
}

// validateSample 校验单条导入观测的数值合法性。energy 与 bias 都必须是
// 有限实数——NaN/Inf 会使重加权不可解，因此整批导入必须拒绝并回滚。
func validateSample(in ImportSample) error {
	if math.IsNaN(in.Energy) || math.IsInf(in.Energy, 0) {
		return fmt.Errorf("energy must be finite (got %v)", in.Energy)
	}
	if math.IsNaN(in.Bias) || math.IsInf(in.Bias, 0) {
		return fmt.Errorf("bias must be finite (got %v)", in.Bias)
	}
	return nil
}

// ImportSample 是导入样本的输入结构。
type ImportSample struct {
	Seq    int     `json:"seq"`
	Energy float64 `json:"energy"`
	Bias   float64 `json:"bias"`
}

// ImportResult 描述一次批量导入的结果。
type ImportResult struct {
	Inserted   int `json:"inserted"`
	Duplicated int `json:"duplicated"`
}

// CorrectWindow 对单个窗口执行偏置校正并回写权重。
func (s *Service) CorrectWindow(windowID string) (*weight.CorrectionSummary, error) {
	w, batch, err := s.mutableWindow(windowID)
	if err != nil {
		return nil, err
	}
	samples, err := s.store.ListSamples(windowID)
	if err != nil {
		return nil, err
	}
	p := weight.BiasParams{Center: w.Center, SpringConst: w.SpringConst, KT: batch.KT}
	corrected, summary := weight.CorrectWindow(samples, p)
	for _, sm := range corrected {
		if err := s.store.UpdateSampleWeight(sm.ID, sm.Weight); err != nil {
			return nil, err
		}
	}
	status := w.Status
	if summary.Converged && status == model.WindowRaw {
		status = model.WindowCorrected
	} else if !summary.Converged && len(corrected) > 0 {
		status = model.WindowNonconverged
	}
	if err := s.store.UpdateWindowStatus(windowID, status, len(corrected)); err != nil {
		return nil, err
	}
	return &summary, nil
}
