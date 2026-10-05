package claude

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 上游可以在 200 响应里塞一个 error 帧。Anthropic 的错误体有多种形态：
// 规范的带 "type"，也有只带 "code" + "message" 的。
//
// GetClaudeError 对 map 形态只在存在 "type" 键时填充 Type，所以只带
// code+message 的错误帧会得到 Type == ""。守卫若只查 Type != ""，
// 就会把这种错误帧当正常数据放过去，调用方随后照常计费。
//
// 对照：OpenAI chat 路径查 Type/Message/Param/Code 任一非空，
// Responses 路径要求 message 非空。Claude 路径应与之对齐。

func newClaudeErrorFrameInfo() (*gin.Context, *relaycommon.RelayInfo, *ClaudeResponseInfo) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-sonnet-4",
		IsStream:        true,
	}
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-4"}
	return c, info, &ClaudeResponseInfo{Usage: &dto.Usage{}}
}

// TestHandleStreamResponseDataRejectsErrorFrameWithoutType 是回归：
// 只带 code + message 的错误帧必须被识别为错误，不得作为正常帧放行。
func TestHandleStreamResponseDataRejectsErrorFrameWithoutType(t *testing.T) {
	c, info, claudeInfo := newClaudeErrorFrameInfo()

	data := `{"error":{"code":"rate_limit_exceeded","message":"Number of requests exceeded your rate limit"}}`
	apiErr := HandleStreamResponseData(c, info, claudeInfo, data)

	require.NotNil(t, apiErr,
		"无 type 的 Claude 错误帧必须被识别为错误，否则用户为限流错误被计费")
	require.Contains(t, apiErr.Error(), "rate limit",
		"错误信息必须保留上游 message")
}

// TestHandleStreamResponseDataRejectsErrorFrameWithType 是对照组：
// 带 type 的规范错误帧仍然必须被识别（修复不得破坏既有行为）。
func TestHandleStreamResponseDataRejectsErrorFrameWithType(t *testing.T) {
	c, info, claudeInfo := newClaudeErrorFrameInfo()

	data := `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
	apiErr := HandleStreamResponseData(c, info, claudeInfo, data)

	require.NotNil(t, apiErr, "带 type 的错误帧必须被识别")
}

// TestHandleStreamResponseDataAcceptsNormalFrame 是反向验证：
// 正常数据帧不得被误判为错误。修复若写成「只要有 error 键就报错」会在这里失败。
func TestHandleStreamResponseDataAcceptsNormalFrame(t *testing.T) {
	c, info, claudeInfo := newClaudeErrorFrameInfo()

	data := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`
	apiErr := HandleStreamResponseData(c, info, claudeInfo, data)

	require.Nil(t, apiErr, "正常内容帧不得被当作错误")
}

// TestClaudeErrorGuardIgnoresEmptyErrorObject 覆盖空错误体：
// `{"error":{}}` 解出的是非 nil 但全空的 ClaudeError，
// 守卫不得因为「有 error 键」就报错。
func TestClaudeErrorGuardIgnoresEmptyErrorObject(t *testing.T) {
	c, info, claudeInfo := newClaudeErrorFrameInfo()

	data := `{"error":{},"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`
	apiErr := HandleStreamResponseData(c, info, claudeInfo, data)

	require.Nil(t, apiErr, "空 error 对象不得被当作错误帧")
}
