package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// NewID 生成确定性 ID：前缀 + 内容摘要，保证幂等场景可复现。
func NewID(prefix, payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return prefix + "-" + hex.EncodeToString(sum[:6])
}

// SampleHash 计算能量样本的内容指纹（幂等去重键）。
func SampleHash(windowID string, seq int, energy, bias float64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%.6f|%.6f", windowID, seq, energy, bias)))
	return hex.EncodeToString(sum[:])
}

// EdgeID 计算相邻窗口边的确定性 ID。
func EdgeID(batchID, lowerID, upperID string) string {
	return NewID("edge", batchID+"|"+lowerID+"|"+upperID)
}
