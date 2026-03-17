package service

import (
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ToolCallUsage captures all tool call counts from a single request.
type ToolCallUsage struct {
	WebSearchCalls         int
	WebSearchModelName     string
	ClaudeWebSearchCalls   int
	FileSearchCalls        int
	ImageGenerationCall    bool
	ImageGenerationQuality string
	ImageGenerationSize    string
}

// ToolCallItem represents a single billed tool usage line.
type ToolCallItem struct {
	Name       string  `json:"name"`
	CallCount  int     `json:"call_count"`
	PricePer1K float64 `json:"price_per_1k"`
	TotalPrice float64 `json:"total_price"`
	Quota      int     `json:"quota"`
}

// ToolCallResult holds the aggregated tool call billing for a request.
type ToolCallResult struct {
	TotalQuota int            `json:"total_quota"`
	Items      []ToolCallItem `json:"items,omitempty"`
}

func getWebSearchPriceKey(modelName string) string {
	isNormalPrice :=
		strings.HasPrefix(modelName, "o3") ||
			strings.HasPrefix(modelName, "o4") ||
			strings.HasPrefix(modelName, "gpt-5")
	if isNormalPrice {
		return "web_search"
	}
	return "web_search_high"
}

// ComputeToolCallQuota calculates the total quota for all tool calls in a
// request. All tool prices are $/1K calls (configurable via ToolCallPrices
// option). groupRatio is applied. Per-call billing (UsePrice) callers should
// NOT add this result — per-call price already includes everything.
func ComputeToolCallQuota(usage ToolCallUsage, groupRatio float64) ToolCallResult {
	var items []ToolCallItem
	totalQuota := 0

	addItem := func(name string, count int, pricePer1K float64) {
		if count <= 0 || pricePer1K <= 0 {
			return
		}
		totalPrice := pricePer1K * float64(count) / 1000
		quota := int(math.Round(totalPrice * common.QuotaPerUnit * groupRatio))
		items = append(items, ToolCallItem{
			Name:       name,
			CallCount:  count,
			PricePer1K: pricePer1K,
			TotalPrice: totalPrice,
			Quota:      quota,
		})
		totalQuota += quota
	}

	if usage.WebSearchCalls > 0 {
		priceKey := getWebSearchPriceKey(usage.WebSearchModelName)
		addItem("web_search", usage.WebSearchCalls, operation_setting.GetToolPrice(priceKey))
	}

	if usage.ClaudeWebSearchCalls > 0 {
		addItem("claude_web_search", usage.ClaudeWebSearchCalls, operation_setting.GetToolPrice("claude_web_search"))
	}

	if usage.FileSearchCalls > 0 {
		addItem("file_search", usage.FileSearchCalls, operation_setting.GetToolPrice("file_search"))
	}

	if usage.ImageGenerationCall {
		price := operation_setting.GetGPTImage1PriceOnceCall(usage.ImageGenerationQuality, usage.ImageGenerationSize)
		quota := int(math.Round(price * common.QuotaPerUnit * groupRatio))
		items = append(items, ToolCallItem{
			Name:       "image_generation",
			CallCount:  1,
			PricePer1K: price * 1000,
			TotalPrice: price,
			Quota:      quota,
		})
		totalQuota += quota
	}

	return ToolCallResult{
		TotalQuota: totalQuota,
		Items:      items,
	}
}
