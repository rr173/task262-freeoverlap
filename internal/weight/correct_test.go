package weight

import (
	"math"
	"testing"

	"task262-freeoverlap/internal/model"
)

func TestUmbrellaBias(t *testing.T) {
	p := BiasParams{Center: 1.0, SpringConst: 10.0, KT: 2.5}
	// z=1.0 -> dz=0 -> bias=0
	if b := UmbrellaBias(1.0, p); math.Abs(b) > 1e-12 {
		t.Fatalf("center bias = %v, want 0", b)
	}
	// z=2.0 -> dz=1 -> bias=0.5*10*1=5
	if b := UmbrellaBias(2.0, p); math.Abs(b-5.0) > 1e-9 {
		t.Fatalf("off-center bias = %v, want 5", b)
	}
}

func TestReweightFactorOverflow(t *testing.T) {
	// 极大偏置应判定不可用而不返回 Inf。
	w, ok := ReweightFactor(1e9, 2.5)
	if ok || w != 0 {
		t.Fatalf("overflow bias: w=%v ok=%v, want 0/false", w, ok)
	}
	// 正常偏置。
	w, ok = ReweightFactor(2.5, 2.5)
	if !ok || math.Abs(w-math.E) > 1e-9 {
		t.Fatalf("normal bias: w=%v ok=%v", w, ok)
	}
}

func TestNormalizeWeights(t *testing.T) {
	weights := []float64{1, 1, 2}
	norm, ess := NormalizeWeights(weights)
	if len(norm) != 3 || math.Abs(norm[0]-0.25) > 1e-9 || math.Abs(norm[2]-0.5) > 1e-9 {
		t.Fatalf("norm = %v", norm)
	}
	// ESS = 1 / (0.25^2 + 0.25^2 + 0.5^2) = 1 / 0.375 = 8/3
	if math.Abs(ess-8.0/3.0) > 1e-9 {
		t.Fatalf("ess = %v, want %v", ess, 8.0/3.0)
	}
}

func TestCorrectWindowConverged(t *testing.T) {
	samples := make([]*model.EnergySample, 100)
	for i := range samples {
		samples[i] = &model.EnergySample{
			ID:     "s",
			Energy: 0.01 * float64(i),
			Bias:   0.5, // 统一偏置 -> 均匀权重 -> ESS 高
		}
	}
	corrected, summary := CorrectWindow(samples, BiasParams{Center: 0.5, SpringConst: 0.1, KT: 2.5})
	if !summary.Converged {
		t.Fatalf("uniform weights should converge: %+v", summary)
	}
	total := 0.0
	for _, sm := range corrected {
		total += sm.Weight
	}
	if math.Abs(total-1.0) > 1e-9 {
		t.Fatalf("weights should sum to 1, got %v", total)
	}
}
