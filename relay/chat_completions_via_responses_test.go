package relay

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type mockChatViaResponsesAdaptor struct {
	convertedReq dto.OpenAIResponsesRequest
	requestBody  []byte
	response     *http.Response
}

func (m *mockChatViaResponsesAdaptor) Init(info *relaycommon.RelayInfo) {}

func (m *mockChatViaResponsesAdaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return "https://mock.local/v1/responses", nil
}

func (m *mockChatViaResponsesAdaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	return nil
}

func (m *mockChatViaResponsesAdaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, nil
}

func (m *mockChatViaResponsesAdaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (m *mockChatViaResponsesAdaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, nil
}

func (m *mockChatViaResponsesAdaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, nil
}

func (m *mockChatViaResponsesAdaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, nil
}

func (m *mockChatViaResponsesAdaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	m.convertedReq = request
	return request, nil
}

func (m *mockChatViaResponsesAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	m.requestBody = body
	return m.response, nil
}

func (m *mockChatViaResponsesAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	return nil, nil
}

func (m *mockChatViaResponsesAdaptor) GetModelList() []string {
	return []string{"gpt-5"}
}

func (m *mockChatViaResponsesAdaptor) GetChannelName() string {
	return "mock-chat-via-responses"
}

func (m *mockChatViaResponsesAdaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, nil
}

func (m *mockChatViaResponsesAdaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, nil
}

var _ channel.Adaptor = (*mockChatViaResponsesAdaptor)(nil)

func newChatViaResponsesInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeChatCompletions,
		RequestURLPath:  "/v1/chat/completions",
		OriginModelName: "gpt-5",
		RelayFormat:     types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:           constant.APITypeOpenAI,
			ChannelId:         123,
			ChannelType:       1,
			UpstreamModelName: "gpt-5",
		},
	}
}

func marshalResponsesStreamChunk(t *testing.T, chunk dto.ResponsesStreamResponse) string {
	t.Helper()
	raw, err := common.Marshal(chunk)
	require.NoError(t, err)
	return "data: " + string(raw) + "\n"
}

func TestChatCompletionsViaResponsesNonStream(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	completedStatus, err := common.Marshal("completed")
	require.NoError(t, err)
	responsesResp := dto.OpenAIResponsesResponse{
		ID:        "resp_non_stream",
		Object:    "response",
		CreatedAt: 1710000000,
		Status:    completedStatus,
		Model:     "gpt-5",
		Output: []dto.ResponsesOutput{
			{
				Type:   "message",
				ID:     "msg_0",
				Status: "completed",
				Role:   "assistant",
				Content: []dto.ResponsesOutputContent{
					{
						Type: "output_text",
						Text: "compat ok",
					},
				},
			},
		},
		Usage: &dto.Usage{
			InputTokens:  8,
			OutputTokens: 4,
			TotalTokens:  12,
		},
	}
	respBytes, err := common.Marshal(responsesResp)
	require.NoError(t, err)

	adaptor := &mockChatViaResponsesAdaptor{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(respBytes)),
		},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte("{}")))

	chatReq := &dto.GeneralOpenAIRequest{
		Model: "gpt-5",
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}
	info := newChatViaResponsesInfo()

	usage, newAPIErr := chatCompletionsViaResponses(c, info, adaptor, chatReq)
	require.Nil(t, newAPIErr)
	require.NotNil(t, usage)
	require.Equal(t, 12, usage.TotalTokens)

	require.Equal(t, "gpt-5", adaptor.convertedReq.Model)
	require.True(t, len(adaptor.requestBody) > 0)
	require.Contains(t, string(adaptor.requestBody), "\"model\":\"gpt-5\"")

	var chatResp dto.OpenAITextResponse
	err = common.Unmarshal(recorder.Body.Bytes(), &chatResp)
	require.NoError(t, err)
	require.Equal(t, "chat.completion", chatResp.Object)
	require.Equal(t, "gpt-5", chatResp.Model)
	require.Len(t, chatResp.Choices, 1)
	require.Equal(t, "compat ok", chatResp.Choices[0].Message.StringContent())
	require.Equal(t, 12, chatResp.Usage.TotalTokens)

	require.Equal(t, relayconstant.RelayModeChatCompletions, info.RelayMode)
	require.Equal(t, "/v1/chat/completions", info.RequestURLPath)
}

func TestChatCompletionsViaResponsesStream(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	completedStatus, err := common.Marshal("completed")
	require.NoError(t, err)
	streamBody := strings.Builder{}
	streamBody.WriteString(marshalResponsesStreamChunk(t, dto.ResponsesStreamResponse{
		Type: "response.created",
		Response: &dto.OpenAIResponsesResponse{
			ID:        "resp_stream",
			Object:    "response",
			CreatedAt: 1710000001,
			Model:     "gpt-5",
		},
	}))
	streamBody.WriteString(marshalResponsesStreamChunk(t, dto.ResponsesStreamResponse{
		Type:  "response.output_text.delta",
		Delta: "hi",
	}))
	streamBody.WriteString(marshalResponsesStreamChunk(t, dto.ResponsesStreamResponse{
		Type: "response.completed",
		Response: &dto.OpenAIResponsesResponse{
			ID:        "resp_stream",
			Object:    "response",
			CreatedAt: 1710000001,
			Status:    completedStatus,
			Model:     "gpt-5",
			Usage: &dto.Usage{
				InputTokens:  2,
				OutputTokens: 1,
				TotalTokens:  3,
			},
		},
	}))
	streamBody.WriteString("data: [DONE]\n")

	adaptor := &mockChatViaResponsesAdaptor{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(streamBody.String())),
		},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte("{}")))

	chatReq := &dto.GeneralOpenAIRequest{
		Model:  "gpt-5",
		Stream: common.GetPointer(true),
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: "hello",
			},
		},
	}
	info := newChatViaResponsesInfo()
	info.ShouldIncludeUsage = true

	usage, newAPIErr := chatCompletionsViaResponses(c, info, adaptor, chatReq)
	require.Nil(t, newAPIErr)
	require.NotNil(t, usage)
	require.Equal(t, 3, usage.TotalTokens)
	require.True(t, info.IsStream)

	output := recorder.Body.String()
	require.Contains(t, output, "\"object\":\"chat.completion.chunk\"")
	require.Contains(t, output, "\"content\":\"hi\"")
	require.Contains(t, output, "\"finish_reason\":\"stop\"")
	require.Contains(t, output, "\"total_tokens\":3")
}
