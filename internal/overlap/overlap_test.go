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
