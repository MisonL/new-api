package aws

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Nova 在 AKSK 模式下只有 InvokeModel（非流式）实现，DoResponse 却在进入
// IsNova 分支后直接调用 handleNovaRequest，不看 info.IsStream：stream:true 的
// 请求也会走非流式 handler，向 SSE 客户端写出一个 application/json 体。客户端
// 要么解析失败，要么把整个响应当空流，而渠道侧记为成功。静默降级不可接受，
// 必须显式失败，让上层按错误处理并可被观测。

func newNovaTestContext(stream bool, model string) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		IsStream:     stream,
		RelayFormat:  types.RelayFormatOpenAI,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: model},
	}
	return c, recorder, info
}

func novaInvokeInput() *bedrockruntime.InvokeModelInput {
	return &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String("amazon.nova-pro-v1:0"),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	}
}

func TestNovaStreamRequestFailsExplicitly(t *testing.T) {
	c, recorder, info := newNovaTestContext(true, "amazon.nova-pro-v1:0")
	adaptor := &Adaptor{
		ClientMode: ClientModeAKSK,
		IsNova:     true,
		AwsReq:     novaInvokeInput(),
	}

	usage, apiErr := adaptor.DoResponse(c, nil, info)

	require.NotNil(t, apiErr, "Nova 流式请求必须显式失败，不能返回非流式 JSON 体")
	require.Nil(t, usage, "被拒绝的流式请求不得产出用量")
	require.Equal(t, http.StatusNotImplemented, apiErr.StatusCode,
		"不支持流式必须用可识别的状态码，实际 %d", apiErr.StatusCode)
	require.Empty(t, recorder.Body.String(),
		"不得向流式请求写出任何响应体：%s", recorder.Body.String())
}

// 反向对照：非流式的 Nova 请求不得被新校验挡住。用本地 HTTP 端点冒充
// Bedrock runtime，真实走一遍 InvokeModel 与响应写出。
func TestNovaNonStreamRequestStillInvokesModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":{"message":{"content":[{"text":"nova-reply"}]}},"usage":{"inputTokens":3,"outputTokens":2,"totalTokens":5}}`))
	}))
	t.Cleanup(server.Close)

	c, recorder, info := newNovaTestContext(false, "amazon.nova-pro-v1:0")
	adaptor := &Adaptor{
		ClientMode: ClientModeAKSK,
		IsNova:     true,
		AwsReq:     novaInvokeInput(),
		AwsClient: bedrockruntime.New(bedrockruntime.Options{
			Region:       "us-east-1",
			Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider("ak", "sk", "")),
			BaseEndpoint: aws.String(server.URL),
			HTTPClient:   server.Client(),
		}),
	}

	usage, apiErr := adaptor.DoResponse(c, nil, info)

	require.Nil(t, apiErr, "非流式 Nova 请求不得被流式检查拒绝：%v", apiErr)
	require.Contains(t, recorder.Body.String(), "nova-reply", "非流式响应必须照常写出")
	require.NotNil(t, usage)
}
