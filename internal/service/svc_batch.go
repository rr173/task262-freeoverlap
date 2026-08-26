package service

import (
	"task262-freeoverlap/internal/model"
)

// CreateBatch 创建批次并进入接收中状态。
func (s *Service) CreateBatch(name, method string, temperature float64) (*model.CalcBatch, error) {
	if name == "" {
		return nil, model.E(model.ErrInvalid, "batch name required")
	}
	if method == "" {
		method = "umbrella"
	}
	if temperature <= 0 {
		return nil, model.E(model.ErrInvalid, "temperature must be positive")
	}
	kt := 0.0083144621 * temperature // R kJ/(mol·K)
	b := &model.CalcBatch{
		ID:          model.NewID("batch", name+"|"+method),
		Name:        name,
		Method:      method,
		Temperature: temperature,
		KT:          kt,
		Status:      model.BatchReceiving,
		CreatedAt:   model.NowMillis(),
		UpdatedAt:   model.NowMillis(),
	}
	if err := s.store.CreateBatch(b); err != nil {
		return nil, err
	}
	return b, nil
}

// GetBatch 读取批次。
func (s *Service) GetBatch(id string) (*model.CalcBatch, error) { return s.store.GetBatch(id) }

// ListBatches 列出全部批次。
func (s *Service) ListBatches() ([]*model.CalcBatch, error) { return s.store.ListBatches() }

// AdvanceBatch 把批次状态推进到目标状态（合法迁移校验）。
func (s *Service) AdvanceBatch(id string, to model.BatchStatus) (*model.CalcBatch, error) {
	b, err := s.store.GetBatch(id)
	if err != nil {
		return nil, err
	}
	if !model.CanBatchTransition(b.Status, to) {
		return nil, model.E(model.ErrStateMismatch,
			"cannot transition batch %s from %s to %s", id, b.Status, to)
	}
	if err := s.store.UpdateBatchStatus(id, to); err != nil {
		return nil, err
	}
	return s.store.GetBatch(id)
}
