// Package httpapi 提供 JSON HTTP API 层（路由前缀 /api）。
//
// 端点覆盖：批次 CRUD 与状态推进、窗口管理、样本导入（幂等）、
// 偏置校正、重叠查询、诊断运行、边裁决、快照发布/替代、自检。
package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"task262-freeoverlap/internal/model"
	"task262-freeoverlap/internal/service"
)

// Server 持有 service 依赖与 HTTP 路由。
type Server struct {
	svc *service.Service
	mux *http.ServeMux
}

// New 构造 HTTP Server 并注册全部路由。
func New(svc *service.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler 返回 http.Handler（供 http.Server 使用）。
func (s *Server) Handler() http.Handler { return s.mux }

// routes 注册 /api 前缀下的全部端点。
func (s *Server) routes() {
	// 批次
	s.mux.HandleFunc("POST /api/batches", s.handleCreateBatch)
	s.mux.HandleFunc("GET /api/batches", s.handleListBatches)
	s.mux.HandleFunc("GET /api/batches/{id}", s.handleGetBatch)
	s.mux.HandleFunc("POST /api/batches/{id}/advance", s.handleAdvanceBatch)
	s.mux.HandleFunc("POST /api/batches/{id}/diagnose", s.handleRunDiagnosis)
	s.mux.HandleFunc("GET /api/batches/{id}/diagnosis", s.handleRunDiagnosis)
	// 窗口
	s.mux.HandleFunc("POST /api/windows", s.handleCreateWindow)
	s.mux.HandleFunc("GET /api/windows", s.handleListWindows)
	s.mux.HandleFunc("POST /api/windows/{id}/samples", s.handleImportSamples)
	s.mux.HandleFunc("POST /api/windows/{id}/correct", s.handleCorrectWindow)
	s.mux.HandleFunc("PATCH /api/windows/{id}/status", s.handleSetWindowStatus)
	// 重叠与边
	s.mux.HandleFunc("GET /api/overlap", s.handleGetOverlap)
	s.mux.HandleFunc("GET /api/edges", s.handleListEdges)
	s.mux.HandleFunc("PATCH /api/edges/{id}/adjudicate", s.handleAdjudicateEdge)
	// 快照
	s.mux.HandleFunc("POST /api/snapshots", s.handleCreateSnapshot)
	s.mux.HandleFunc("GET /api/snapshots", s.handleListSnapshots)
	s.mux.HandleFunc("POST /api/snapshots/{id}/publish", s.handlePublishSnapshot)
	s.mux.HandleFunc("POST /api/snapshots/{id}/supersede", s.handleSupersedeSnapshot)
	// 健康与自检
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("POST /api/smoke", s.handleSmoke)
}

// --- 工具函数 ---

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return model.E(model.ErrInvalid, "bad request body: %v", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	msg := err.Error()
	switch {
	case model.IsKind(err, model.ErrNotFound):
		code = http.StatusNotFound
	case model.IsKind(err, model.ErrInvalid):
		code = http.StatusBadRequest
	case model.IsKind(err, model.ErrConflict):
		code = http.StatusConflict
	case model.IsKind(err, model.ErrImmutable):
		code = http.StatusConflict
	case model.IsKind(err, model.ErrStateMismatch):
		code = http.StatusConflict
	}
	writeJSON(w, code, map[string]any{"error": msg})
}

// TrimSlash 去掉字符串首尾斜杠（工具函数，供路由拼接使用）。
func TrimSlash(s string) string { return strings.Trim(s, "/") }
