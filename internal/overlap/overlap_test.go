package overlap

import (
	"math"
	"testing"

	"task262-freeoverlap/internal/model"
)

func mkSamples(start, step float64, n int) []*model.EnergySample {
	out := make([]*model.EnergySample, n)
	for i := range out {
		out[i] = &model.EnergySample{
			ID:     "s",
			Energy: start + step*float64(i),
			Weight: 1.0,
		}
	}
	return out
}

func TestPairOverlapIdentical(t *testing.T) {
	a := mkSamples(0, 0.1, 50)
	b := mkSamples(0, 0.1, 50)
	o, sufficient, gap := PairOverlap(a, b)
	if !sufficient || gap {
		t.Fatalf("identical distributions: o=%v suff=%v gap=%v", o, sufficient, gap)
	}
	if o < 0.8 {
		t.Fatalf("identical distributions should have high overlap, got %v", o)
	}
}

func TestPairOverlapDisjoint(t *testing.T) {
	a := mkSamples(0, 0.1, 50)   // [0, 4.9]
	b := mkSamples(100, 0.1, 50) // [100, 104.9] 完全分离
	o, sufficient, gap := PairOverlap(a, b)
	if gap == false || sufficient {
		t.Fatalf("disjoint distributions: o=%v suff=%v gap=%v", o, sufficient, gap)
	}
	if o > 0.02 {
		t.Fatalf("disjoint overlap should be ~0, got %v", o)
	}
}

func TestPairOverlapPartial(t *testing.T) {
	a := mkSamples(0, 0.1, 100) // [0, 9.9]
	b := mkSamples(5, 0.1, 100) // [5, 14.9] 部分重叠
	o, _, _ := PairOverlap(a, b)
	if o < 0.1 || o > 0.95 {
		t.Fatalf("partial overlap out of range: %v", o)
	}
}

// TestPairOverlapExcludedSamplesDontMaskGap 确保无法用于重加权的异常观测
// 不参与能量范围与重叠判断。两个窗口的可用样本本就分离（真实断层），但各自
// 含一条权重为 0（校正失败）的远端离群观测；若 RangeOf 纳入这些离群点，合并
// 范围被极度撑大、40 箱分箱稀释，会使两个分离的可用分布坍缩进同一宽箱，从而
// 把断层误判为重叠充分——这正是本修复要消除的“掩盖”现象。
func TestPairOverlapExcludedSamplesDontMaskGap(t *testing.T) {
	// 可用样本：A 在 [0,5]，B 在 [6,11]，中间无交集 -> 真实断层。
	aUsable := mkSamples(0, 0.25, 21)   // 0,0.25,...,5.0
	bUsable := mkSamples(6, 0.25, 21)   // 6,6.25,...,11.0
	// 远端离群、权重为 0（校正失败），不应参与范围判定。
	a := append(aUsable, &model.EnergySample{ID: "x", Energy: 1e6, Weight: 0})
	b := append(bUsable, &model.EnergySample{ID: "x", Energy: 1e6, Weight: 0})
	o, sufficient, gap := PairOverlap(a, b)
	if !gap || sufficient {
		t.Fatalf("disjoint usable distributions must be a gap: o=%v suff=%v gap=%v", o, sufficient, gap)
	}
}

// TestRangeOfSkipsUnusableWeight 确保 RangeOf 仅纳入可用权重观测，
// 与 BuildHistogram 的 IsUsableWeight 口径一致。
func TestRangeOfSkipsUnusableWeight(t *testing.T) {
	usable := mkSamples(0, 1.0, 3) // 能量 0,1,2
	samples := append([]*model.EnergySample{
		{ID: "lo", Energy: -100, Weight: 0},     // 校正失败，不参与范围
		{ID: "hi", Energy: 1000, Weight: 0},    // 校正失败，不参与范围
		{ID: "nan", Energy: 999, Weight: 1.0},  // 被下面覆盖前用于保证顺序无关
	}, usable...)
	samples[2].Weight = 0 // 把 nan 标记为不可用
	lo, hi := RangeOf(samples)
	if lo != 0 || hi != 2 {
		t.Fatalf("RangeOf should ignore unusable weights: got [%v,%v], want [0,2]", lo, hi)
	}
}

func TestHistogramIndex(t *testing.T) {
	h := NewHistogram(0, 10, 10)
	if h.Index(-1) != -1 || h.Index(11) != -1 {
		t.Fatalf("out-of-range index should be -1")
	}
	if h.Index(0) != 0 || h.Index(9.99) != 9 {
		t.Fatalf("index mapping wrong: %v %v", h.Index(0), h.Index(9.99))
	}
}

func TestSummary(t *testing.T) {
	mean, min := Summary([]float64{3, 1, 2})
	if math.Abs(mean-2) > 1e-12 || min != 1 {
		t.Fatalf("summary mean=%v min=%v", mean, min)
	}
}

func TestClassify(t *testing.T) {
	if Classify(0.5) != string(model.EdgeSufficient) {
		t.Fatal("0.5 should be sufficient")
	}
	if Classify(0.01) != string(model.EdgeGap) {
		t.Fatal("0.01 should be gap")
	}
}
