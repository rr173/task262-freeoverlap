// Package model 定义分子动力学自由能窗口重叠诊断服务的核心领域实体与枚举。
//
// 领域背景：计算化学研究者用伞形采样（Umbrella Sampling）/ 自由能微扰等
// 方法沿反应坐标划分多个采样窗口，每个窗口施加偏置势以覆盖高能垒区域。
// 相邻窗口的能量分布必须足够重叠，重加权（WHAM/MBAR）才能得到可靠的自由能曲线；
// 若某对窗口之间出现“断层”（分布无交集），自由能曲线在该处产生假势垒。
//
// 本包只承载数据结构与枚举常量，不包含持久化或业务逻辑。
package model

import "time"

// BatchStatus 描述一次自由能计算批次的诊断生命周期。
type BatchStatus string

const (
	// BatchReceiving 接收中：批次已创建，窗口与样本仍在导入。
	BatchReceiving BatchStatus = "receiving"
	// BatchPending 待诊断：窗口与样本齐备，等待运行重叠诊断。
	BatchPending BatchStatus = "pending"
	// BatchInsufficient 重叠不足：诊断发现断层或未收敛窗口，等待处理。
	BatchInsufficient BatchStatus = "insufficient"
	// BatchPublishable 可发布：所有相邻窗口重叠充分，可发布可靠性快照。
	BatchPublishable BatchStatus = "publishable"
	// BatchSealed 封存：批次结论定稿，仅作归档只读。
	BatchSealed BatchStatus = "sealed"
)

// WindowStatus 描述单个采样窗口的校正与收敛状态。
type WindowStatus string

const (
	// WindowRaw 原始：样本已导入但尚未做偏置校正。
	WindowRaw WindowStatus = "raw"
	// WindowCorrected 已校正：已完成偏置权重校正。
	WindowCorrected WindowStatus = "corrected"
	// WindowNonconverged 未收敛：样本量或有效样本量不足。
	WindowNonconverged WindowStatus = "nonconverged"
	// WindowExcluded 排除：研究者确认该窗口数据不可用，从诊断中排除。
	WindowExcluded WindowStatus = "excluded"
)

// EdgeStatus 描述相邻窗口之间重叠关系的一次判定。
type EdgeStatus string

const (
	// EdgeCandidate 候选：重叠尚未计算或等待复核。
	EdgeCandidate EdgeStatus = "candidate"
	// EdgeSufficient 重叠充分：相邻窗口分布交集足够，支撑可靠重加权。
	EdgeSufficient EdgeStatus = "sufficient"
	// EdgeGap 断层：相邻窗口分布无交集，重加权在此处不可靠。
	EdgeGap EdgeStatus = "gap"
	// EdgeResample 需重采样：研究者裁决该断层窗口需要追加采样。
	EdgeResample EdgeStatus = "resample"
)

// SnapshotStatus 描述可靠性快照的生命周期。
type SnapshotStatus string

const (
	// SnapshotDraft 草稿：可继续编辑的快照。
	SnapshotDraft SnapshotStatus = "draft"
	// SnapshotPublished 发布：已对外发布的不可变快照。
	SnapshotPublished SnapshotStatus = "published"
	// SnapshotSuperseded 替代：被后续快照取代。
	SnapshotSuperseded SnapshotStatus = "superseded"
)

// CalcBatch 是一次自由能计算诊断的聚合容器。
type CalcBatch struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Method      string      `json:"method"`
	Temperature float64     `json:"temperature"` // 开尔文
	KT          float64     `json:"kt"`          // 热动能 kT（kJ/mol）
	Status      BatchStatus `json:"status"`
	CreatedAt   int64       `json:"created_at"`
	UpdatedAt   int64       `json:"updated_at"`
}

// SamplingWindow 是反应坐标上的一个采样窗口（伞形采样偏置中心）。
type SamplingWindow struct {
	ID          string       `json:"id"`
	BatchID     string       `json:"batch_id"`
	Label       string       `json:"label"`
	Center      float64      `json:"center"`       // 偏置中心坐标
	SpringConst float64      `json:"spring_const"` // 谐振子偏置力常数
	BiasVersion int          `json:"bias_version"`
	Status      WindowStatus `json:"status"`
	SampleCount int          `json:"sample_count"`
	CreatedAt   int64        `json:"created_at"`
	UpdatedAt   int64        `json:"updated_at"`
}

// EnergySample 是某个窗口内的一次能量观测（带偏置与重加权权重）。
type EnergySample struct {
	ID          string  `json:"id"`
	WindowID    string  `json:"window_id"`
	Seq         int     `json:"seq"`
	Energy      float64 `json:"energy"`       // 无偏势能（kJ/mol）
	Bias        float64 `json:"bias"`         // 施加的偏置势值（kJ/mol）
	Weight      float64 `json:"weight"`       // 校正后的无偏权重
	ContentHash string  `json:"content_hash"` // 幂等去重指纹
	CreatedAt   int64   `json:"created_at"`
}

// WindowEdge 描述一对相邻窗口之间的重叠判定。
type WindowEdge struct {
	ID            string     `json:"id"`
	BatchID       string     `json:"batch_id"`
	LowerWindowID string     `json:"lower_window_id"`
	UpperWindowID string     `json:"upper_window_id"`
	Overlap       float64    `json:"overlap"` // 重叠积分（0~1）
	Status        EdgeStatus `json:"status"`
	Note          string     `json:"note"`
	CreatedAt     int64      `json:"created_at"`
}

// ReliabilitySnapshot 是不可变的诊断结论快照。
type ReliabilitySnapshot struct {
	ID        string         `json:"id"`
	BatchID   string         `json:"batch_id"`
	Label     string         `json:"label"`
	Status    SnapshotStatus `json:"status"`
	Snapshot  string         `json:"snapshot"`
	CreatedAt int64          `json:"created_at"`
	FrozenAt  int64          `json:"frozen_at"`
}

// DiagnosisReport 是单次诊断运行的汇总结果（用于快照内容与 API 返回）。
type DiagnosisReport struct {
	BatchID       string          `json:"batch_id"`
	TotalWindows  int             `json:"total_windows"`
	Excluded      int             `json:"excluded"`
	GapEdges      int             `json:"gap_edges"`
	ResampleEdges int             `json:"resample_edges"`
	Nonconverged  int             `json:"nonconverged"`
	MinOverlap    float64         `json:"min_overlap"`
	MeanOverlap   float64         `json:"mean_overlap"`
	Converged     bool            `json:"converged"`
	Windows       []WindowSummary `json:"windows"`
	Edges         []EdgeSummary   `json:"edges"`
	GeneratedAt   int64           `json:"generated_at"`
}

// WindowSummary records the correction result that diagnosis computed for an
// active window. The service layer persists this projection so API reads and
// the aggregate state agree after a diagnosis run.
type WindowSummary struct {
	ID          string       `json:"id"`
	Label       string       `json:"label"`
	Status      WindowStatus `json:"status"`
	SampleCount int          `json:"sample_count"`
	ESS         float64      `json:"ess"`
	Converged   bool         `json:"converged"`
}

// EdgeSummary 是诊断报告中单条边的摘要。
type EdgeSummary struct {
	LowerLabel string  `json:"lower_label"`
	UpperLabel string  `json:"upper_label"`
	Overlap    float64 `json:"overlap"`
	Status     string  `json:"status"`
	Note       string  `json:"note"`
}

// NowMillis 返回当前 Unix 毫秒时间戳。
func NowMillis() int64 {
	return time.Now().UnixMilli()
}
