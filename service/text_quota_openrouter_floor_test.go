package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// OpenRouter Claude 计费会从 prompt 里减掉 cache 与 cache creation。上游
// usage 自相矛盾（cached > prompt）时结果为负，整单配额被压到 1 而不是
// 按非负的 prompt 计算。必须给 prompt 加 0 下限。
func TestCalculateTextQuotaSummaryFloorsOpenRouterClaudePromptAtZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	relayInfo := &relaycommon.RelayInfo{
		FinalRequestRelayFormat: types.RelayFormatClaude,
		OriginModelName:         "anthropic/claude-3.7-sonnet",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenRouter,
		},
		PriceData: types.PriceData{
			ModelRatio:         1,
			CompletionRatio:    1,
			CacheRatio:         0.1,
			CacheCreationRatio: 1.25,
			GroupRatioInfo:     types.GroupRatioInfo{GroupRatio: 1},
		},
		StartTime: time.Now(),
	}
	usage := &dto.Usage{
		PromptTokens:     100,
		CompletionTokens: 10,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 150,
		},
	}

	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	require.Equal(t, 0, summary.PromptTokens, "prompt 不能为负")
	// 0 + 150*0.1 + 10 = 25
	require.Equal(t, 25, summary.Quota)
}
