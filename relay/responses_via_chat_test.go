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

type mockResponsesViaChatAdaptor struct {
	convertedReq *dto.GeneralOpenAIRequest
	requestBody  []byte
	response     *http.Response
}

func (m *mockResponsesViaChatAdaptor) Init(info *relaycommon.RelayInfo) {}

func (m *mockResponsesViaChatAdaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return "https://mock.local/v1/chat/completions", nil
}

func (m *mockResponsesViaChatAdaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	return nil
}

func (m *mockResponsesViaChatAdaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request != nil {
		reqCopy := *request
		reqCopy.Messages = append([]dto.Message(nil), request.Messages...)
		m.convertedReq = &reqCopy
	}
	return request, nil
}

func (m *mockResponsesViaChatAdaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (m *mockResponsesViaChatAdaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, nil
}

func (m *mockResponsesViaChatAdaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, nil
}

func (m *mockResponsesViaChatAdaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, nil
}

func (m *mockResponsesViaChatAdaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, nil
}

func (m *mockResponsesViaChatAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	m.requestBody = body
	return m.response, nil
}

func (m *mockResponsesViaChatAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	return nil, nil
}

func (m *mockResponsesViaChatAdaptor) GetModelList() []string {
	return []string{"gpt-5"}
}

func (m *mockResponsesViaChatAdaptor) GetChannelName() string {
	return "mock-responses-via-chat"
}

func (m *mockResponsesViaChatAdaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, nil
}

func (m *mockResponsesViaChatAdaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, nil
}

var _ channel.Adaptor = (*mockResponsesViaChatAdaptor)(nil)

func newResponsesViaChatInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		RequestURLPath:  "/v1/responses",
		OriginModelName: "gpt-5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType:           constant.APITypeOpenAI,
			ChannelId:         123,
			ChannelType:       1,
			UpstreamModelName: "gpt-5",
		},
	}
}

func marshalRawJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := common.Marshal(value)
	require.NoError(t, err)
	return raw
}

func marshalChatStreamChunk(t *testing.T, chunk dto.ChatCompletionsStreamResponse) string {
	t.Helper()
	raw, err := common.Marshal(chunk)
	require.NoError(t, err)
	return "data: " + string(raw) + "\n"
}

func TestResponsesViaChatNonStream(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	chatResp := dto.OpenAITextResponse{
		Id:      "chatcmpl_non_stream",
		Object:  "chat.completion",
		Created: int64(1710000000),
		Model:   "gpt-5",
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index: 0,
				Message: dto.Message{
					Role:    "assistant",
					Content: "compat ok",
				},
				FinishReason: "stop",
			},
		},
		Usage: dto.Usage{
			PromptTokens:     7,
			CompletionTokens: 3,
			TotalTokens:      10,
		},
	}
	chatRespBytes, err := common.Marshal(chatResp)
	require.NoError(t, err)

	adaptor := &mockResponsesViaChatAdaptor{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewReader(chatRespBytes)),
		},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte("{}")))

	req := &dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: marshalRawJSON(t, "hello"),
	}
	info := newResponsesViaChatInfo()

	usage, newAPIErr := responsesViaChat(c, info, adaptor, req)
	require.Nil(t, newAPIErr)
	require.NotNil(t, usage)
	require.Equal(t, 10, usage.TotalTokens)

	require.NotNil(t, adaptor.convertedReq)
	require.Equal(t, "gpt-5", adaptor.convertedReq.Model)
	require.Len(t, adaptor.convertedReq.Messages, 1)
	require.Equal(t, "user", adaptor.convertedReq.Messages[0].Role)
	require.Equal(t, "hello", adaptor.convertedReq.Messages[0].StringContent())

	var responsesResp dto.OpenAIResponsesResponse
	err = common.Unmarshal(recorder.Body.Bytes(), &responsesResp)
	require.NoError(t, err)
	require.Equal(t, "response", responsesResp.Object)
	require.Equal(t, "gpt-5", responsesResp.Model)
	require.Len(t, responsesResp.Output, 1)
	require.Equal(t, "message", responsesResp.Output[0].Type)
	require.Equal(t, "compat ok", responsesResp.Output[0].Content[0].Text)

	require.Equal(t, relayconstant.RelayModeResponses, info.RelayMode)
	require.Equal(t, "/v1/responses", info.RequestURLPath)
}

func TestResponsesViaChatStream(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	roleChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_stream",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role: "assistant",
				},
			},
		},
	}
	textChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_stream",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Content: common.GetPointer("hi"),
				},
			},
		},
	}
	finishReason := "stop"
	doneChunk := dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_stream",
		Object:  "chat.completion.chunk",
		Created: 1710000001,
		Model:   "gpt-5",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index:        0,
				FinishReason: &finishReason,
			},
		},
		Usage: &dto.Usage{
			PromptTokens:     2,
			CompletionTokens: 1,
			TotalTokens:      3,
		},
	}

	streamBody := strings.Builder{}
	streamBody.WriteString(marshalChatStreamChunk(t, roleChunk))
	streamBody.WriteString(marshalChatStreamChunk(t, textChunk))
	streamBody.WriteString(marshalChatStreamChunk(t, doneChunk))
	streamBody.WriteString("data: [DONE]\n")

	adaptor := &mockResponsesViaChatAdaptor{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(streamBody.String())),
		},
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte("{}")))

	req := &dto.OpenAIResponsesRequest{
		Model:  "gpt-5",
		Stream: common.GetPointer(true),
		Input:  marshalRawJSON(t, "hello stream"),
	}
	info := newResponsesViaChatInfo()

	usage, newAPIErr := responsesViaChat(c, info, adaptor, req)
	require.Nil(t, newAPIErr)
	require.NotNil(t, usage)
	require.Equal(t, 3, usage.TotalTokens)
	require.True(t, info.IsStream)

	output := recorder.Body.String()
	require.Contains(t, output, "event: response.created")
	require.Contains(t, output, "event: response.output_text.delta")
	require.Contains(t, output, "event: response.completed")
	require.Contains(t, output, "\"text\":\"hi\"")
	require.Contains(t, output, "\"total_tokens\":3")
}
