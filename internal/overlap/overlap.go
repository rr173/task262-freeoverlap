// Package overlap 实现相邻采样窗口能量分布的重叠计算（分布重叠积分）。
//
// 物理背景：伞形采样重加权（WHAM/MBAR）的可靠性要求相邻窗口的能量分布有
// 足够重叠。对两个窗口的（重加权后的）能量样本分别构造一维直方图，重叠积分
// 定义为两个归一化分布的重叠面积：
//
//	O = ∫ min(p_A(E), p_B(E)) dE
//
// O 接近 1 表示分布几乎重合，O 接近 0 表示分布无交集（断层）。经验判据：
// O >= 0.3 视为重叠充分（sufficient），O < 0.05 视为断层（gap）。
// 权重样本使用能量加权直方图：每个样本按无偏权重累加到对应能量箱。
package overlap

import (
	"math"
	"sort"

	"task262-freeoverlap/internal/model"
)

// Thresholds 是重叠判定的经验阈值。
var Thresholds = struct {
	Sufficient float64 // 重叠充分下界
	Gap        float64 // 断层上界
}{
	Sufficient: 0.3,
	Gap:        0.05,
}

// Histogram 是能量轴上的一维加权直方图。
type Histogram struct {
	Min   float64
	Max   float64
	Bins  int
	Count []float64
}

// NewHistogram 构造覆盖 [min, max] 的 bins 个箱的空直方图。
func NewHistogram(min, max float64, bins int) *Histogram {
	if bins < 8 {
		bins = 8
	}
	if max <= min {
		max = min + 1
	}
	return &Histogram{Min: min, Max: max, Bins: bins, Count: make([]float64, bins)}
}

// Add 以权重 w 累加一个能量值到对应箱。
func (h *Histogram) Add(energy, w float64) {
	idx := h.Index(energy)
	if idx >= 0 {
		h.Count[idx] += w
	}
}

// Index 计算能量值所在箱号（越界返回 -1）。
func (h *Histogram) Index(energy float64) int {
	if energy < h.Min || energy > h.Max {
		return -1
	}
	span := (h.Max - h.Min) / float64(h.Bins)
	i := int((energy - h.Min) / span)
	if i >= h.Bins {
		i = h.Bins - 1
	}
	return i
}

// Normalize 把直方图归一化为概率密度（总权重 1）。
func (h *Histogram) Normalize() {
	total := 0.0
	for _, c := range h.Count {
		total += c
	}
	if total <= 0 {
		return
	}
	for i := range h.Count {
		h.Count[i] /= total
	}
}

// OverlapIntegral 计算两个已归一化直方图的重叠面积 ∫ min(pA, pB)。
func OverlapIntegral(a, b *Histogram) float64 {
	n := a.Bins
	if b.Bins < n {
		n = b.Bins
	}
	o := 0.0
	for i := 0; i < n; i++ {
		o += math.Min(a.Count[i], b.Count[i])
	}
	if o < 0 {
		o = 0
	}
	if o > 1 {
		o = 1
	}
	return o
}

// BuildHistogram 从加权能量样本构造归一化直方图。
// 能量范围取两窗口合并后的 [min, max] 以便对齐。
func BuildHistogram(samples []*model.EnergySample, min, max float64, bins int) *Histogram {
	h := NewHistogram(min, max, bins)
	for _, sm := range samples {
		if !model.IsUsableWeight(sm.Weight) {
			continue
		}
		h.Add(sm.Energy, sm.Weight)
	}
	h.Normalize()
	return h
}

// RangeOf 计算样本集合的能量最小/最大值。
//
// 与 BuildHistogram 一致，只纳入偏置校正后可重加权（IsUsableWeight）的观测。
// 校正失败（权重为 0 或非有限）的异常观测对分布无贡献，不能参与能量范围
// 判定，否则会撑大直方图覆盖区间、稀释分箱分辨率，从而把真正分离的两个
// 有效分布误判为重叠充分，掩盖实际断层。若没有任何可用观测，回退默认范围。
func RangeOf(samples []*model.EnergySample) (float64, float64) {
	if len(samples) == 0 {
		return 0, 1
	}
	lo, hi := math.NaN(), math.NaN()
	for _, sm := range samples {
		if !model.IsUsableWeight(sm.Weight) {
			continue
		}
		if math.IsNaN(lo) {
			lo, hi = sm.Energy, sm.Energy
			continue
		}
		if sm.Energy < lo {
			lo = sm.Energy
		}
		if sm.Energy > hi {
			hi = sm.Energy
		}
	}
	if math.IsNaN(lo) {
		return 0, 1
	}
	return lo, hi
}

// MergeRange 返回覆盖两个区间的合并范围（带 5% 边距）。
func MergeRange(aLo, aHi, bLo, bHi float64) (float64, float64) {
	lo := math.Min(aLo, bLo)
	hi := math.Max(aHi, bHi)
	pad := (hi - lo) * 0.05
	if pad <= 0 {
		pad = 1
	}
	return lo - pad, hi + pad
}

// PairOverlap 计算一对窗口样本的分布重叠积分与判定。
// 返回 overlap 值、是否充分、是否断层。
func PairOverlap(lower, upper []*model.EnergySample) (float64, bool, bool) {
	if len(lower) == 0 || len(upper) == 0 {
		return 0, false, true
	}
	loA, hiA := RangeOf(lower)
	loB, hiB := RangeOf(upper)
	min, max := MergeRange(loA, hiA, loB, hiB)
	ha := BuildHistogram(lower, min, max, 40)
	hb := BuildHistogram(upper, min, max, 40)
	o := OverlapIntegral(ha, hb)
	return o, o >= Thresholds.Sufficient, o < Thresholds.Gap
}

// Classify 把重叠值映射为判定文本。
func Classify(overlap float64) string {
	switch {
	case overlap >= Thresholds.Sufficient:
		return string(model.EdgeSufficient)
	case overlap < Thresholds.Gap:
		return string(model.EdgeGap)
	default:
		return string(model.EdgeCandidate)
	}
}

// Summary 统计一组重叠值的均值与最小值。
func Summary(overlaps []float64) (mean, min float64) {
	if len(overlaps) == 0 {
		return 0, 0
	}
	sorted := make([]float64, len(overlaps))
	copy(sorted, overlaps)
	sort.Float64s(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	return sum / float64(len(sorted)), sorted[0]
}
