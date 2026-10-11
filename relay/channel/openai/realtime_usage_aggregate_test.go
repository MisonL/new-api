package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

// The tiered billing expression can price cache creation, image, and audio
// tokens separately (service/tiered_settle.go BuildTieredTokenParams reads
// CachedTokens, CachedCreationTokens, ImageTokens and AudioTokens). Every
// detail field a realtime turn reports must therefore survive the per-turn
// aggregation into the session total; a field the aggregation drops reads as
// zero in the expression and its tokens are billed at the text rate instead.
func TestAddRealtimeUsageSumsEveryDetailField(t *testing.T) {
	dst := &dto.RealtimeUsage{}
	src := &dto.RealtimeUsage{
		TotalTokens:  30,
		InputTokens:  20,
		OutputTokens: 10,
		InputTokenDetails: dto.InputTokenDetails{
			CachedTokens:         1,
			CachedCreationTokens: 2,
			TextTokens:           3,
			AudioTokens:          4,
			ImageTokens:          5,
		},
		OutputTokenDetails: dto.OutputTokenDetails{
			TextTokens:  6,
			AudioTokens: 7,
			ImageTokens: 8,
		},
	}

	addRealtimeUsage(dst, src)

	if dst.TotalTokens != 30 || dst.InputTokens != 20 || dst.OutputTokens != 10 {
		t.Fatalf("totals not summed: %+v", dst)
	}
	if dst.InputTokenDetails.CachedTokens != 1 {
		t.Errorf("CachedTokens = %d, want 1", dst.InputTokenDetails.CachedTokens)
	}
	if dst.InputTokenDetails.CachedCreationTokens != 2 {
		t.Errorf("CachedCreationTokens = %d, want 2 (dropped from aggregation)", dst.InputTokenDetails.CachedCreationTokens)
	}
	if dst.InputTokenDetails.TextTokens != 3 {
		t.Errorf("input TextTokens = %d, want 3", dst.InputTokenDetails.TextTokens)
	}
	if dst.InputTokenDetails.AudioTokens != 4 {
		t.Errorf("input AudioTokens = %d, want 4", dst.InputTokenDetails.AudioTokens)
	}
	if dst.InputTokenDetails.ImageTokens != 5 {
		t.Errorf("input ImageTokens = %d, want 5 (dropped from aggregation)", dst.InputTokenDetails.ImageTokens)
	}
	if dst.OutputTokenDetails.TextTokens != 6 {
		t.Errorf("output TextTokens = %d, want 6", dst.OutputTokenDetails.TextTokens)
	}
	if dst.OutputTokenDetails.AudioTokens != 7 {
		t.Errorf("output AudioTokens = %d, want 7", dst.OutputTokenDetails.AudioTokens)
	}
	if dst.OutputTokenDetails.ImageTokens != 8 {
		t.Errorf("output ImageTokens = %d, want 8 (dropped from aggregation)", dst.OutputTokenDetails.ImageTokens)
	}
}

func TestAddRealtimeUsageAccumulatesAcrossTurns(t *testing.T) {
	dst := &dto.RealtimeUsage{}
	turn := &dto.RealtimeUsage{
		InputTokens:        5,
		InputTokenDetails:  dto.InputTokenDetails{ImageTokens: 2},
		OutputTokenDetails: dto.OutputTokenDetails{ImageTokens: 3},
	}
	addRealtimeUsage(dst, turn)
	addRealtimeUsage(dst, turn)

	if dst.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", dst.InputTokens)
	}
	if dst.InputTokenDetails.ImageTokens != 4 {
		t.Errorf("input ImageTokens = %d, want 4", dst.InputTokenDetails.ImageTokens)
	}
	if dst.OutputTokenDetails.ImageTokens != 6 {
		t.Errorf("output ImageTokens = %d, want 6", dst.OutputTokenDetails.ImageTokens)
	}
}
