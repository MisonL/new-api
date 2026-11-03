package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// prelude 提交判定必须把 thinking_content 与 reasoning_content/reasoning 一视同仁。
// 漏掉它时，只带 thinking_content 的推理帧被当成空 prelude，上游在其后中断
// 会被误判为「内容未下发」而换渠道重放，客户端看到重复推理。这条用例与
// Claude/OpenAI relay 的 prelude 语义一致：任何实质内容字段非空都要清标记。
func TestTrackStreamPreludeCommitTreatsThinkingContentAsContent(t *testing.T) {
	cases := []struct {
		name        string
		frame       string
		wantRetain  bool
		wantComment string
	}{
		{
			name:        "thinking_content 非空必须清标记",
			frame:       `{"choices":[{"delta":{"thinking_content":"推理片段"}}]}`,
			wantRetain:  false,
			wantComment: "thinking_content 是实质内容，下发后不能再重试",
		},
		{
			name:        "message.thinking_content 非空必须清标记",
			frame:       `{"choices":[{"message":{"thinking_content":"推理片段"}}]}`,
			wantRetain:  false,
			wantComment: "非流式 message 形状同样要识别",
		},
		{
			name:        "reasoning_content 非空必须清标记",
			frame:       `{"choices":[{"delta":{"reasoning_content":"推理片段"}}]}`,
			wantRetain:  false,
			wantComment: "既有字段保持行为",
		},
		{
			name:        "纯 role 帧保留标记",
			frame:       `{"choices":[{"delta":{"role":"assistant"}}]}`,
			wantRetain:  true,
			wantComment: "只有 role 仍属 prelude，可重试",
		},
		{
			name:        "空 thinking_content 保留标记",
			frame:       `{"choices":[{"delta":{"thinking_content":""}}]}`,
			wantRetain:  true,
			wantComment: "空字符串不是内容",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyResponsesStreamPreludeRetrySafe, true)

			trackStreamPreludeCommit(c, tc.frame)

			got := common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe)
			require.Equal(t, tc.wantRetain, got, tc.wantComment)
		})
	}
}
