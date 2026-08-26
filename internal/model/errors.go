package model

import (
	"errors"
	"fmt"
	"math"
)

// 领域错误分类（供 httpapi 映射 HTTP 状态码）。
var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrInvalid       = errors.New("invalid input")
	ErrImmutable     = errors.New("immutable")
	ErrStateMismatch = errors.New("state mismatch")
)

// DomainError 携带领域错误分类与上下文消息。
type DomainError struct {
	Kind error
	Msg  string
}

func (e *DomainError) Error() string { return e.Msg }

// Unwrap 让 errors.Is 可以匹配 Kind。
func (e *DomainError) Unwrap() error { return e.Kind }

// E 快速构造带上下文的领域错误。
func E(kind error, format string, args ...any) error {
	return &DomainError{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// IsKind 判断错误是否属于某个领域错误类别。
func IsKind(err error, kind error) bool {
	return errors.Is(err, kind)
}

// IsUsableWeight reports whether a persisted sample weight can participate in
// a normalized distribution. Zero is reserved for samples rejected during
// bias correction; it must not be treated as an uncorrected unit weight.
func IsUsableWeight(weight float64) bool {
	return weight > 0 && !math.IsNaN(weight) && !math.IsInf(weight, 0)
}

// BatchStateOrder 定义批次状态的合法推进顺序。
var BatchStateOrder = []BatchStatus{
	BatchReceiving,
	BatchPending,
	BatchInsufficient,
	BatchPublishable,
	BatchSealed,
}

// CanBatchTransition 判断批次状态 s -> t 是否合法（按顺序只进不退，封存终态）。
func CanBatchTransition(s, t BatchStatus) bool {
	if s == t {
		return true
	}
	if s == BatchSealed {
		return false
	}
	si, ti := -1, -1
	for i, st := range BatchStateOrder {
		if st == s {
			si = i
		}
		if st == t {
			ti = i
		}
	}
	return si >= 0 && ti >= 0 && ti > si
}

// CanApplyDiagnosisStatus allows a fresh diagnosis to replace a previous
// diagnosis conclusion. Unlike operator-driven lifecycle transitions,
// publishable and insufficient are classifications of current data and may
// move in either direction until the batch is sealed.
func CanApplyDiagnosisStatus(current, result BatchStatus) bool {
	if current.IsTerminal() {
		return false
	}
	return result == BatchInsufficient || result == BatchPublishable
}

// ValidateBatchStatus 校验文本是否为合法批次状态。
func ValidateBatchStatus(s string) (BatchStatus, bool) {
	switch BatchStatus(s) {
	case BatchReceiving, BatchPending, BatchInsufficient, BatchPublishable, BatchSealed:
		return BatchStatus(s), true
	}
	return "", false
}

// ValidateWindowStatus 校验文本是否为合法窗口状态。
func ValidateWindowStatus(s string) (WindowStatus, bool) {
	switch WindowStatus(s) {
	case WindowRaw, WindowCorrected, WindowNonconverged, WindowExcluded:
		return WindowStatus(s), true
	}
	return "", false
}

// ValidateEdgeStatus 校验文本是否为合法边状态。
func ValidateEdgeStatus(s string) (EdgeStatus, bool) {
	switch EdgeStatus(s) {
	case EdgeCandidate, EdgeSufficient, EdgeGap, EdgeResample:
		return EdgeStatus(s), true
	}
	return "", false
}

// ValidateSnapshotStatus 校验文本是否为合法快照状态。
func ValidateSnapshotStatus(s string) (SnapshotStatus, bool) {
	switch SnapshotStatus(s) {
	case SnapshotDraft, SnapshotPublished, SnapshotSuperseded:
		return SnapshotStatus(s), true
	}
	return "", false
}

// IsTerminal 判断批次状态是否为终态（封存后不可再迁移）。
func (s BatchStatus) IsTerminal() bool { return s == BatchSealed }

// IsImmutable 判断快照状态是否已冻结（发布/替代后不可修改）。
func (s SnapshotStatus) IsImmutable() bool {
	return s == SnapshotPublished || s == SnapshotSuperseded
}
