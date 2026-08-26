// Package weight 实现伞形采样（Umbrella Sampling）的偏置校正与重加权。
//
// 物理背景：伞形采样在每个窗口 w 上施加谐振子偏置势
//
//	U_bias(z) = 0.5 * k * (z - z0)^2
//
// 其中 z 是反应坐标（此处以能量样本的无偏势能近似投影位置），z0 是偏置中心，
// k 是力常数。样本在偏置势下被采样的概率与无偏分布不同，重加权时需把每个样本
// 的统计权重乘上 Boltzmann 因子 exp(+U_bias / kT)，再对窗口内样本做归一化，
// 得到无偏权重。相邻窗口分布是否重叠，直接决定 WHAM/MBAR 重加权方程是否可解。
package weight

import (
	"math"

	"task262-freeoverlap/internal/model"
)

// BiasParams 描述单个窗口的偏置参数。
type BiasParams struct {
	Center      float64 // 偏置中心（反应坐标位置）
	SpringConst float64 // 谐振子力常数 k（kJ/mol/nm^2 或同单位平方）
	KT          float64 // 热动能（kJ/mol）
}

// UmbrellaBias 计算谐振子偏置势值：0.5 * k * (z - z0)^2。
func UmbrellaBias(z float64, p BiasParams) float64 {
	dz := z - p.Center
	return 0.5 * p.SpringConst * dz * dz
}

// ReweightFactor 计算偏置校正的 Boltzmann 因子 exp(+U_bias / kT)。
//
// 数值安全：当 U_bias 很大时直接 exp 会溢出为 +Inf，这里对指数做截断，
// 并返回是否可用的标记。重加权时若权重溢出或为 NaN，该样本应被标记为
// 不可靠（不参与重叠统计）。
func ReweightFactor(bias, kt float64) (float64, bool) {
	if kt <= 0 || math.IsNaN(bias) || math.IsInf(bias, 0) {
		return 0, false
	}
	x := bias / kt
	// 截断：超过 700 时 exp 溢出，但相对权重已经极大，按不可用处理。
	if x > 700 {
		return 0, false
	}
	return math.Exp(x), true
}

// NormalizeWeights 把一组权重归一化为和为 1。
// 返回归一化权重切片与有效样本量（ESS = 1 / sum(w^2)）。
func NormalizeWeights(weights []float64) ([]float64, float64) {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	if total <= 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return nil, 0
	}
	out := make([]float64, len(weights))
	sumSq := 0.0
	for i, w := range weights {
		nw := w / total
		out[i] = nw
		sumSq += nw * nw
	}
	ess := 0.0
	if sumSq > 0 {
		ess = 1.0 / sumSq
	}
	return out, ess
}

// CorrectWindow 对一个窗口的全部样本做偏置校正：
//  1. 对每个样本计算校正权重 exp(+U_bias/kT)；
//  2. 归一化得到无偏权重；
//  3. 计算有效样本量 ESS 与收敛状态。
//
// 返回校正后的样本（含权重）与统计汇总。样本的 energy 字段作为反应坐标 z。
func CorrectWindow(samples []*model.EnergySample, p BiasParams) ([]*model.EnergySample, CorrectionSummary) {
	weights := make([]float64, len(samples))
	for i, sm := range samples {
		// 样本已记录施加的偏置值 bias，直接使用；若缺失（bias==0 且无偏置参数），
		// 则用 UmbrellaBias 计算。
		b := sm.Bias
		if math.Abs(b) < 1e-12 {
			b = UmbrellaBias(sm.Energy, p)
		}
		w, ok := ReweightFactor(b, p.KT)
		if !ok {
			w = 0
		}
		weights[i] = w
	}
	norm, ess := NormalizeWeights(weights)
	summary := CorrectionSummary{
		Total:      len(samples),
		ValidCount: 0,
		ESS:        ess,
		Converged:  ess >= float64(len(samples))*0.1,
	}
	if norm == nil {
		for i, sm := range samples {
			cp := *sm
			cp.Weight = 0
			samples[i] = &cp
		}
		summary.Converged = false
		return samples, summary
	}
	for i, sm := range samples {
		cp := *sm
		cp.Weight = norm[i]
		samples[i] = &cp
		if norm[i] > 1e-12 {
			summary.ValidCount++
		}
	}
	return samples, summary
}

// CorrectionSummary 描述一次窗口校正的结果。
type CorrectionSummary struct {
	Total      int     `json:"total"`
	ValidCount int     `json:"valid_count"`
	ESS        float64 `json:"ess"`
	Converged  bool    `json:"converged"`
}
