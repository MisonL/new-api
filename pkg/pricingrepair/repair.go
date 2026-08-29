package pricingrepair

import (
	"math"
	"strings"
)

const ClaudeCacheCreation1hMultiplier = 6.0 / 3.75

type LogSnapshot struct {
	PromptTokens          int64
	CompletionTokens      int64
	CacheTokens           int64
	CacheCreationTokens   int64
	CacheCreationTokens5m int64
	CacheCreationTokens1h int64
	GroupRatio            float64
	GroupRatioSet         bool
	UsageSemantic         string
	LegacyClaudeDerived   bool
}

type ModelPricing struct {
	ModelRatio           float64
	CompletionRatio      float64
	CacheRatio           float64
	CacheCreationRatio   float64
	CacheCreationRatio5m float64
	CacheCreationRatio1h float64
}

func CalculateQuota(snapshot LogSnapshot, pricing ModelPricing) int64 {
	groupRatio := snapshot.GroupRatio
	if !snapshot.GroupRatioSet && groupRatio == 0 {
		groupRatio = 1
	}

	claudeUsage := strings.EqualFold(strings.TrimSpace(snapshot.UsageSemantic), "anthropic") || snapshot.LegacyClaudeDerived
	baseUsage := float64(snapshot.PromptTokens)
	if snapshot.CacheTokens > 0 {
		if !claudeUsage {
			baseUsage -= float64(snapshot.CacheTokens)
		}
		baseUsage += float64(snapshot.CacheTokens) * pricing.CacheRatio
	}

	cacheCreationSplit := snapshot.CacheCreationTokens5m > 0 || snapshot.CacheCreationTokens1h > 0
	if snapshot.CacheCreationTokens > 0 || cacheCreationSplit {
		if !claudeUsage {
			baseUsage -= float64(snapshot.CacheCreationTokens)
			baseUsage += float64(snapshot.CacheCreationTokens) * pricing.CacheCreationRatio
		} else {
			remaining := snapshot.CacheCreationTokens - snapshot.CacheCreationTokens5m - snapshot.CacheCreationTokens1h
			if remaining < 0 {
				remaining = 0
			}
			baseUsage += float64(remaining) * pricing.CacheCreationRatio
			baseUsage += float64(snapshot.CacheCreationTokens5m) * pricing.CacheCreationRatio5m
			baseUsage += float64(snapshot.CacheCreationTokens1h) * pricing.CacheCreationRatio1h
		}
	}
	if baseUsage < 0 {
		baseUsage = 0
	}
	baseUsage += float64(snapshot.CompletionTokens) * pricing.CompletionRatio
	if baseUsage == 0 && snapshot.PromptTokens == 0 && snapshot.CompletionTokens == 0 && snapshot.CacheTokens == 0 && snapshot.CacheCreationTokens == 0 && snapshot.CacheCreationTokens5m == 0 && snapshot.CacheCreationTokens1h == 0 {
		return 0
	}

	quota := int64(math.Round(baseUsage * groupRatio * pricing.ModelRatio))
	if pricing.ModelRatio != 0 && groupRatio != 0 && quota <= 0 {
		return 1
	}
	return quota
}

func HourBucket(createdAt int64) int64 {
	return createdAt - (createdAt % 3600)
}
