// Package service 编排 store、weight、overlap、diag、snapshot 各层，
// 向 httpapi 暴露领域操作（创建批次、导入样本、诊断、裁决、发布快照）。
//
// 本文件只承载 Service 类型、构造器与包注释；具体操作按领域拆分在
// svc_batch.go / svc_window.go / svc_diag.go / svc_snapshot.go 中。
package service

import (
	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/store"
)

// Service 是应用服务的门面。
type Service struct {
	store *store.Store
}

// New 构造服务。
func New(st *store.Store) *Service { return &Service{store: st} }

func (s *Service) mutableBatch(batchID string) (*model.CalcBatch, error) {
	b, err := s.store.GetBatch(batchID)
	if err != nil {
		return nil, err
	}
	if b.Status.IsTerminal() {
		return nil, model.E(model.ErrImmutable, "batch %s is sealed", batchID)
	}
	return b, nil
}

func (s *Service) mutableWindow(windowID string) (*model.SamplingWindow, *model.CalcBatch, error) {
	w, err := s.store.GetWindow(windowID)
	if err != nil {
		return nil, nil, err
	}
	b, err := s.mutableBatch(w.BatchID)
	if err != nil {
		return nil, nil, err
	}
	return w, b, nil
}
