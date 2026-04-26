package pricingrepair

import "math"

type LogSnapshot struct {
	PromptTokens     int64
	CompletionTokens int64
	CacheTokens      int64
	GroupRatio       float64
}

type ModelPricing struct {
	ModelRatio      float64
	CompletionRatio float64
	CacheRatio      float64
}

func CalculateQuota(snapshot LogSnapshot, pricing ModelPricing) int64 {
	if snapshot.GroupRatio == 0 {
		snapshot.GroupRatio = 1
	}

	baseUsage := float64(snapshot.PromptTokens)
	if snapshot.CacheTokens > 0 {
		baseUsage -= float64(snapshot.CacheTokens)
		baseUsage += float64(snapshot.CacheTokens) * pricing.CacheRatio
	}
	baseUsage += float64(snapshot.CompletionTokens) * pricing.CompletionRatio
	if baseUsage <= 0 {
		return 0
	}

	quota := int64(math.Round(baseUsage * snapshot.GroupRatio * pricing.ModelRatio))
	if pricing.ModelRatio != 0 && quota <= 0 {
		return 1
	}
	return quota
}

func HourBucket(createdAt int64) int64 {
	return createdAt - (createdAt % 3600)
}
