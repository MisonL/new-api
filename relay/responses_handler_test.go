package relay

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type retryEncryptedContextAdaptor struct {
	responses     []*http.Response
	requestBodies [][]byte
}

type streamingCompatibilityRetryAdaptor struct {
	retryEncryptedContextAdaptor
}

func (m *streamingCompatibilityRetryAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	return openairelay.OaiResponsesStreamHandler(c, info, resp)
}

func (m *retryEncryptedContextAdaptor) Init(info *relaycommon.RelayInfo) {}

func (m *retryEncryptedContextAdaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return "https://mock.local/v1/responses", nil
}

func (m *retryEncryptedContextAdaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	return nil
}

func (m *retryEncryptedContextAdaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, nil
}

func (m *retryEncryptedContextAdaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (m *retryEncryptedContextAdaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, nil
}

func (m *retryEncryptedContextAdaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, nil
}

func (m *retryEncryptedContextAdaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, nil
}

func (m *retryEncryptedContextAdaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return request, nil
}

func (m *retryEncryptedContextAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	m.requestBodies = append(m.requestBodies, body)
	if len(m.responses) == 0 {
		return encryptedContextRetryResponse(http.StatusInternalServerError), nil
	}
	resp := m.responses[0]
	m.responses = m.responses[1:]
	return resp, nil
}

func (m *retryEncryptedContextAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	return &dto.Usage{PromptTokens: 11, CompletionTokens: 3, TotalTokens: 14}, nil
}

func (m *retryEncryptedContextAdaptor) GetModelList() []string {
	return []string{"gpt-5.5"}
}

func (m *retryEncryptedContextAdaptor) GetChannelName() string {
	return "retry-encrypted-context"
}

func (m *retryEncryptedContextAdaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, nil
}

func (m *retryEncryptedContextAdaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, nil
}

func encryptedContextRetryResponse(status int) *http.Response {
	body := `{"error":{"message":"The encrypted content gAAA...MoQb could not be verified. Reason: Encrypted content could not be decrypted or parsed.","type":"invalid_request_error","code":"thinking_signature_invalid"}}`
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func missingEncryptedContentRetryResponse(status int) *http.Response {
	return missingEncryptedContentRetryResponseAt(status, 0)
}

func missingEncryptedContentRetryResponseAt(status int, inputIndex int) *http.Response {
	body := fmt.Sprintf(`{"error":{"message":"[ObjectParam] [input[%d].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[%d].encrypted_content'.","type":"invalid_request_error","code":"missing_required_parameter"}}`, inputIndex, inputIndex)
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func missingEncryptedContentErrorAfterStreamPrelude() *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.in_progress","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.error","error":{"message":"[ObjectParam] [input[0].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[0].encrypted_content'.","type":"invalid_request_error","code":"missing_required_parameter"}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func nestedMissingEncryptedContentRetryResponse(status int) *http.Response {
	body := `{"error":{"message":"[ObjectParam] [input[0].content[1].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[0].content[1].encrypted_content'.","type":"invalid_request_error","code":"missing_required_parameter"}}`
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func namespaceHistoryRetryResponse() *http.Response {
	return namespaceHistoryRetryResponseAt(1)
}

func namespaceHistoryRetryResponseAt(index int) *http.Response {
	param := fmt.Sprintf("input[%d].namespace", index)
	body := fmt.Sprintf(`{"error":{"message":"Unknown parameter: '%s'.","type":"invalid_request_error","param":"%s","code":"unknown_parameter"}}`, param, param)
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func namespaceHistoryRetryResponseWithMessageOnly() *http.Response {
	body := `{"error":{"message":"Unknown parameter: 'input[1].namespace'.","type":"upstream_error","param":"","code":null}}`
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func argumentsHistoryRetryResponse() *http.Response {
	body := `{"error":{"message":"Invalid type for 'input[1].arguments': expected an object, but got a string instead.","type":"invalid_request_error","param":"input[1].arguments","code":"invalid_type"}}`
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func argumentsHistoryStringRetryResponse() *http.Response {
	body := `{"error":{"message":"Invalid type for 'input[1].arguments': expected a string, but got an object instead.","type":"invalid_request_error","param":"input[1].arguments","code":"invalid_type"}}`
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func argumentsHistoryRetryResponseWithMessageOnly() *http.Response {
	body := `{"error":{"message":"Invalid type for 'input[1].arguments': expected an object, but got a string instead.","type":"upstream_error","param":"","code":null}}`
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func successResponsesHTTPResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":"resp_ok","object":"response"}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func namespaceHistoryErrorAfterStreamPrelude() *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.in_progress","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.error","error":{"message":"Unknown parameter: 'input[0].namespace'.","type":"invalid_request_error","param":"input[0].namespace","code":"unknown_parameter"}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func streamOptionsIncludeUsageErrorAfterStreamPrelude() *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.in_progress","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.error","error":{"message":"[ObjectParam] [stream_options.include_usage] [unknown_parameter] Unknown parameter: 'stream_options.include_usage'.","type":"invalid_request_error","param":"stream_options.include_usage","code":"unknown_parameter"}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func namespaceHistoryErrorAfterStreamOutput() *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.output_text.delta","delta":"partial output"}`,
		`data: {"type":"response.error","error":{"message":"Unknown parameter: 'input[0].namespace'.","type":"invalid_request_error","param":"input[0].namespace","code":"unknown_parameter"}}`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func successStreamingResponsesHTTPResponse() *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_ok","object":"response"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_ok","object":"response","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func TestMarkResponsesChatCompatIgnoredEncryptedInclude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	markResponsesChatCompatIgnoredEncryptedInclude(c, common.RawMessage(`["message.output_text.logprobs"]`))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesChatCompatIgnoredEncryptedInclude))

	markResponsesChatCompatIgnoredEncryptedInclude(c, common.RawMessage(`"reasoning.encrypted_content"`))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesChatCompatIgnoredEncryptedInclude))

	markResponsesChatCompatIgnoredEncryptedInclude(c, common.RawMessage(`["reasoning.encrypted_content"]`))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesChatCompatIgnoredEncryptedInclude))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryOpaqueEncryptedReasoningFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.5","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			encryptedContextRetryResponse(http.StatusBadRequest),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}, false)
	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, string(adaptor.requestBodies[0]), `"encrypted_content":"opaque"`)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryWrappedOpaqueEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			missingEncryptedContentRetryResponse(http.StatusBadGateway),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, string(adaptor.requestBodies[0]), `"encrypted_content":"opaque"`)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestRetriesUnpopulatedReasoningForMissingEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			missingEncryptedContentRetryResponse(http.StatusBadGateway),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":""},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[0]), `"type":"reasoning"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"type":"reasoning"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"type":"message"`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryMessageContentMissingEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"previous answer"},{"type":"encrypted_content","encrypted_content":"opaque"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			nestedMissingEncryptedContentRetryResponse(http.StatusBadGateway),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"previous answer"},{"type":"encrypted_content","encrypted_content":"opaque"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryOpaqueAgentMessageEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":[{"type":"agent_message","role":"assistant","content":[{"type":"output_text","text":"previous answer"},{"type":"encrypted_content","encrypted_content":"opaque"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			nestedMissingEncryptedContentRetryResponse(http.StatusBadGateway),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"agent_message","role":"assistant","content":[{"type":"output_text","text":"previous answer"},{"type":"encrypted_content","encrypted_content":"opaque"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, string(adaptor.requestBodies[0]), "previous answer")
	require.Contains(t, string(adaptor.requestBodies[0]), `"encrypted_content":"opaque"`)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotDropCompactionForMissingEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":[{"type":"compaction","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			missingEncryptedContentRetryResponse(http.StatusBadGateway),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, string(adaptor.requestBodies[0]), `"type":"compaction"`)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestRetriesValidToolOutputForMissingEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-terra",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   207,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			missingEncryptedContentRetryResponseAt(http.StatusBadGateway, 1),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-terra",
		Input: common.RawMessage(`[
			{"type":"function_call","id":"fc_1","call_id":"call_1","name":"wait","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":[
				{"type":"input_text","text":"first"},
				{"type":"input_text","text":"second"}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.JSONEq(t, string(adaptor.requestBodies[0]), string(adaptor.requestBodies[1]))
	require.Contains(t, string(adaptor.requestBodies[1]), `"type":"function_call_output"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"call_id":"call_1"`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryOpaqueEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"first"},{"type":"reasoning","encrypted_content":"second"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			missingEncryptedContentRetryResponse(http.StatusBadGateway),
			missingEncryptedContentRetryResponse(http.StatusBadGateway),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"first"},
			{"type":"reasoning","encrypted_content":"second"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, string(adaptor.requestBodies[0]), `"encrypted_content":"first"`)
	require.Contains(t, string(adaptor.requestBodies[0]), `"encrypted_content":"second"`)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestShouldRetryResponsesWithoutEncryptedReasoningRequiresEncryptedContentPath(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	err := types.WithOpenAIError(types.OpenAIError{
		Message: "missing_required_parameter encrypted_content",
		Type:    "invalid_request_error",
		Code:    "missing_required_parameter",
	}, http.StatusBadRequest)

	require.False(t, shouldRetryResponsesWithoutEncryptedReasoning(info, err))
}

func TestShouldRetryResponsesWithoutStreamOptionsIncludeUsageRequiresExactParameter(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	testCases := []struct {
		name string
		err  *types.NewAPIError
		want bool
	}{
		{
			name: "exact parameter",
			err: types.WithOpenAIError(types.OpenAIError{
				Param: "stream_options.include_usage",
				Code:  "unknown_parameter",
			}, http.StatusBadRequest),
			want: true,
		},
		{
			name: "gateway wrapped exact parameter",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "[ObjectParam] [stream_options.include_usage] [unknown_parameter] Unknown parameter: 'stream_options.include_usage'.",
				Code:    "invalid_request_error",
			}, http.StatusBadRequest),
			want: true,
		},
		{
			name: "similarly named parameter is not retried",
			err: types.WithOpenAIError(types.OpenAIError{
				Param: "stream_options.include_usage_extra",
				Code:  "unknown_parameter",
			}, http.StatusBadRequest),
			want: false,
		},
		{
			name: "similarly named gateway message is not retried",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Unknown parameter: 'stream_options.include_usage_extra'.",
				Code:    "unknown_parameter",
			}, http.StatusBadRequest),
			want: false,
		},
		{
			name: "unquoted exact message",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Unknown parameter: stream_options.include_usage.",
				Code:    "unknown_parameter",
			}, http.StatusBadRequest),
			want: true,
		},
		{
			name: "unquoted similarly named gateway message is not retried",
			err: types.WithOpenAIError(types.OpenAIError{
				Message: "Unknown parameter: stream_options.include_usage_extra.",
				Code:    "unknown_parameter",
			}, http.StatusBadRequest),
			want: false,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldRetryResponsesWithoutStreamOptionsIncludeUsage(info, tt.err))
		})
	}
}

func TestExecuteOpenAIResponsesRequestRetriesRejectedNamespaceHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6","input":[{"type":"custom_tool_call","call_id":"call_keep","name":"read_thread","namespace":"keep_namespace","input":"{}"},{"type":"custom_tool_call","call_id":"call_rejected","name":"read_thread","namespace":"rejected_namespace","input":"{}"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			namespaceHistoryRetryResponse(),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_keep","name":"read_thread","namespace":"keep_namespace","input":"{}"},
			{"type":"custom_tool_call","call_id":"call_rejected","name":"read_thread","namespace":"rejected_namespace","input":"{}"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[0]), `"namespace":"keep_namespace"`)
	require.Contains(t, string(adaptor.requestBodies[0]), `"namespace":"rejected_namespace"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"namespace":"keep_namespace"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"namespace":"rejected_namespace"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"call_id":"call_rejected"`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesNamespaceHistoryRetry))
}

func TestExecuteOpenAIResponsesRequestRetriesEachRejectedNamespaceHistoryItem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			namespaceHistoryRetryResponse(),
			namespaceHistoryRetryResponseAt(0),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_keep","name":"read_thread","namespace":"first_namespace","input":"{}"},
			{"type":"custom_tool_call","call_id":"call_rejected","name":"read_thread","namespace":"second_namespace","input":"{}"}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 3)
	require.Contains(t, string(adaptor.requestBodies[1]), `"namespace":"first_namespace"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"namespace":"second_namespace"`)
	require.NotContains(t, string(adaptor.requestBodies[2]), `"namespace"`)
}

func TestExecuteOpenAIResponsesRequestLimitsCompatibilityRetries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	responses := make([]*http.Response, maxResponsesCompatibilityRetries+1)
	inputItems := make([]string, maxResponsesCompatibilityRetries+1)
	for index := range responses {
		responses[index] = namespaceHistoryRetryResponseAt(index)
		inputItems[index] = fmt.Sprintf(`{"type":"custom_tool_call","call_id":"call_%d","namespace":"namespace_%d","input":"{}"}`, index, index)
	}
	adaptor := &retryEncryptedContextAdaptor{responses: responses}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   178,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage("[" + strings.Join(inputItems, ",") + "]"),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, maxResponsesCompatibilityRetries+1)
	require.Contains(t, err.Error(), fmt.Sprintf("input[%d].namespace", maxResponsesCompatibilityRetries))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryAfterStreamingOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				namespaceHistoryErrorAfterStreamOutput(),
				successResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_rejected","name":"read_thread","namespace":"rejected_namespace","input":"{}"}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, recorder.Body.String(), "response.created")
	require.Contains(t, recorder.Body.String(), "partial output")
	require.NotContains(t, recorder.Body.String(), "Unknown parameter")
}

func TestExecuteOpenAIResponsesRequestRetriesAfterOnlyStreamingPrelude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   150,
			ChannelType: constant.ChannelTypeOpenAI,
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				namespaceHistoryErrorAfterStreamPrelude(),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_rejected","name":"read_thread","namespace":"rejected_namespace","input":"{}"}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.NotContains(t, recorder.Body.String(), "resp_partial")
	require.Contains(t, recorder.Body.String(), "resp_ok")
	require.NotContains(t, recorder.Body.String(), "Unknown parameter")
}

func TestExecuteOpenAIResponsesRequestRetriesUnsupportedStreamOptionsIncludeUsageAfterStreamingPrelude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   72,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AllowIncludeObfuscation: true,
			},
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				streamOptionsIncludeUsageErrorAfterStreamPrelude(),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
		StreamOptions: &dto.StreamOptions{
			IncludeUsage:       true,
			IncludeObfuscation: true,
		},
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[0]), `"include_usage":true`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"include_usage"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"include_obfuscation":true`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOptionsIncludeUsageRetry))
	require.Zero(t, info.StreamStatus.TotalErrorCount())
	require.NotContains(t, recorder.Body.String(), "resp_partial")
	require.Contains(t, recorder.Body.String(), "resp_ok")
}

func TestExecuteOpenAIResponsesRequestRetriesUnpopulatedReasoningAfterStreamingPrelude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				missingEncryptedContentErrorAfterStreamPrelude(),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":""},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"type":"reasoning"`)
	require.Contains(t, recorder.Body.String(), "resp_ok")
	require.NotContains(t, recorder.Body.String(), "resp_partial")
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestRetriesAfterPingOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, writeErr := c.Writer.Write([]byte(": ping\n\n"))
	require.NoError(t, writeErr)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   150,
			ChannelType: constant.ChannelTypeOpenAI,
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				namespaceHistoryErrorAfterStreamPrelude(),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_rejected","name":"read_thread","namespace":"rejected_namespace","input":"{}"}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, recorder.Body.String(), ": ping")
	require.NotContains(t, recorder.Body.String(), "resp_partial")
	require.Contains(t, recorder.Body.String(), "resp_ok")
}

func TestExecuteOpenAIResponsesRequestRetriesRejectedNamespaceHistoryFromMessageOnlyError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6","input":[{"type":"custom_tool_call","call_id":"call_keep","name":"read_thread","namespace":"keep_namespace","input":"{}"},{"type":"custom_tool_call","call_id":"call_rejected","name":"read_thread","namespace":"rejected_namespace","input":"{}"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			namespaceHistoryRetryResponseWithMessageOnly(),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call","call_id":"call_keep","name":"read_thread","namespace":"keep_namespace","input":"{}"},
			{"type":"custom_tool_call","call_id":"call_rejected","name":"read_thread","namespace":"rejected_namespace","input":"{}"}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[1]), `"namespace":"keep_namespace"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"namespace":"rejected_namespace"`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesNamespaceHistoryRetry))
}

func TestExecuteOpenAIResponsesRequestRetriesRejectedStringArgumentsHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6","input":[{"type":"function_call","call_id":"call_keep","name":"read_file","arguments":"{\"path\":\"keep.txt\"}"},{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":"{\"path\":\"target.txt\",\"ok\":true}"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			argumentsHistoryRetryResponse(),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"function_call","call_id":"call_keep","name":"read_file","arguments":"{\"path\":\"keep.txt\"}"},
			{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":"{\"path\":\"target.txt\",\"ok\":true}"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[0]), `"arguments":"{\"path\":\"keep.txt\"}"`)
	require.Contains(t, string(adaptor.requestBodies[0]), `"arguments":"{\"path\":\"target.txt\",\"ok\":true}"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"arguments":{"path":"keep.txt"}`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"arguments":{"ok":true,"path":"target.txt"}`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesArgumentsHistoryRetry))
}

func TestExecuteOpenAIResponsesRequestRetriesArgumentsInBothDirections(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   178,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			argumentsHistoryRetryResponse(),
			argumentsHistoryStringRetryResponse(),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"function_call","call_id":"call_keep","name":"read_file","arguments":"{\"path\":\"keep.txt\"}"},
			{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":"{\"path\":\"target.txt\",\"ok\":true}"}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 3)
	require.Contains(t, string(adaptor.requestBodies[0]), `"arguments":"{\"path\":\"keep.txt\"}"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"arguments":{"path":"keep.txt"}`)
	require.Contains(t, string(adaptor.requestBodies[2]), `"arguments":"{\"path\":\"keep.txt\"}"`)
}

func TestExecuteOpenAIResponsesRequestRetriesArgumentsInReverseDirection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   178,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			argumentsHistoryStringRetryResponse(),
			argumentsHistoryRetryResponse(),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"function_call","call_id":"call_keep","name":"read_file","arguments":{"path":"keep.txt"}},
			{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":{"path":"target.txt","ok":true}}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 3)
	require.Contains(t, string(adaptor.requestBodies[0]), `"arguments":{"path":"keep.txt"}`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"arguments":"{\"path\":\"keep.txt\"}"`)
	require.Contains(t, string(adaptor.requestBodies[2]), `"arguments":{"path":"keep.txt"}`)
}

func TestExecuteOpenAIResponsesRequestRetriesRejectedStringArgumentsHistoryFromMessageOnlyError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6","input":[{"type":"function_call","call_id":"call_keep","name":"read_file","arguments":"{\"path\":\"keep.txt\"}"},{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":"{\"path\":\"target.txt\",\"ok\":true}"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			argumentsHistoryRetryResponseWithMessageOnly(),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"function_call","call_id":"call_keep","name":"read_file","arguments":"{\"path\":\"keep.txt\"}"},
			{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":"{\"path\":\"target.txt\",\"ok\":true}"}
		]`),
	}, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[1]), `"arguments":{"path":"keep.txt"}`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"arguments":{"ok":true,"path":"target.txt"}`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesArgumentsHistoryRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryUnnormalizableArgumentsHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6","input":[{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":"not json"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			argumentsHistoryRetryResponseWithMessageOnly(),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6",
		Input: common.RawMessage(`[
			{"type":"function_call","call_id":"call_rejected","name":"edit_file","arguments":"not json"}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesArgumentsHistoryRetry))
}

func TestExecuteOpenAIResponsesRequestMapsOpaqueEncryptedReasoningFailureWithoutRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.5","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(constant.ContextKeyChannelStatusCodeMapping), `{"400":418}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			encryptedContextRetryResponse(http.StatusBadRequest),
			successResponsesHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, http.StatusTeapot, err.StatusCode)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, string(adaptor.requestBodies[0]), `"encrypted_content":"opaque"`)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestMapsStatusWhenEncryptedRetryHasNoRemovableItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(constant.ContextKeyChannelStatusCodeMapping), `{"400":418}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			encryptedContextRetryResponse(http.StatusBadRequest),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, http.StatusTeapot, err.StatusCode)
	require.Len(t, adaptor.requestBodies, 1)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotRewriteOpaqueBodyWhenPassThroughEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.5","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = storage.Close()
	})
	c.Set(common.KeyBodyStorage, storage)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			encryptedContextRetryResponse(http.StatusBadRequest),
			successResponsesHTTPResponse(),
		},
	}

	usage, retryErr := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}, false)
	require.Nil(t, usage)
	require.Error(t, retryErr)
	require.Equal(t, http.StatusBadRequest, retryErr.StatusCode)
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, string(rawBody), string(adaptor.requestBodies[0]))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestBuildOpenAIResponsesRequestBodyAppliesReasoningContextPolicyWhenPassThroughEnabled(t *testing.T) {
	unsupported := false
	tests := []struct {
		name        string
		profile     dto.ResponsesUpstreamProfile
		registry    *dto.ResponsesChannelCapabilityRegistry
		wantContext bool
		wantAction  string
	}{
		{
			name:       "explicit unsupported strips context",
			profile:    dto.ResponsesUpstreamProfileOfficialOpenAI,
			registry:   &dto.ResponsesChannelCapabilityRegistry{SupportsReasoningContext: &unsupported},
			wantAction: "stripped_unsupported_capability",
		},
		{
			name:        "official profile forwards context",
			profile:     dto.ResponsesUpstreamProfileOfficialOpenAI,
			wantContext: true,
			wantAction:  "forwarded_supported_capability",
		},
		{
			name:        "generic profile forwards unverified context",
			profile:     dto.ResponsesUpstreamProfileGenericOpenAI,
			wantContext: true,
			wantAction:  "forwarded_unverified_capability",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawBody := []byte(`{"model":"gpt-5.6-sol","input":"continue","reasoning":{"effort":"high","summary":"auto","mode":"adaptive","context":"all_turns","future_reasoning_option":{"enabled":true}}}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
			c.Request.Header.Set("Content-Type", "application/json")
			storage, err := common.CreateBodyStorage(rawBody)
			require.NoError(t, err)
			t.Cleanup(func() { _ = storage.Close() })
			c.Set(common.KeyBodyStorage, storage)
			request := &dto.OpenAIResponsesRequest{}
			require.NoError(t, common.Unmarshal(rawBody, request))
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				OriginModelName: request.Model,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:   206,
					ChannelType: constant.ChannelTypeOpenAI,
					ChannelSetting: dto.ChannelSettings{
						PassThroughBodyEnabled: true,
					},
					ChannelOtherSettings: dto.ChannelOtherSettings{
						ResponsesUpstreamProfile:    tt.profile,
						ResponsesCapabilityRegistry: tt.registry,
					},
				},
			}

			body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

			require.Nil(t, apiErr)
			bodyBytes, err := io.ReadAll(body)
			require.NoError(t, err)
			require.Equal(t, tt.wantContext, strings.Contains(string(bodyBytes), `"context":"all_turns"`))
			require.Contains(t, string(bodyBytes), `"future_reasoning_option":{"enabled":true}`)
			require.NotNil(t, converted.Reasoning)
			require.Equal(t, tt.wantContext, len(converted.Reasoning.Context) > 0)
			require.Equal(t, tt.wantAction, common.GetContextKeyString(c, constant.ContextKeyResponsesReasoningContextAction))
		})
	}
}

func TestBuildOpenAIResponsesRequestBodyAppliesStreamOptionsIncludeUsageCapabilityWhenPassThroughEnabled(t *testing.T) {
	unsupported := false
	supported := true
	tests := []struct {
		name         string
		registry     *dto.ResponsesChannelCapabilityRegistry
		wantUsage    bool
		wantStripped bool
	}{
		{
			name:      "unknown capability preserves raw request",
			wantUsage: true,
		},
		{
			name: "explicit unsupported strips only include usage",
			registry: &dto.ResponsesChannelCapabilityRegistry{
				SupportsStreamOptionsIncludeUsage: &unsupported,
			},
			wantStripped: true,
		},
		{
			name: "explicit support preserves raw request",
			registry: &dto.ResponsesChannelCapabilityRegistry{
				SupportsStreamOptionsIncludeUsage: &supported,
			},
			wantUsage: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawBody := []byte(`{"model":"gpt-5.6-sol","input":"continue","stream_options":{"include_usage":true,"include_obfuscation":true,"future_option":{"enabled":true}}}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
			c.Request.Header.Set("Content-Type", "application/json")
			storage, err := common.CreateBodyStorage(rawBody)
			require.NoError(t, err)
			t.Cleanup(func() { _ = storage.Close() })
			c.Set(common.KeyBodyStorage, storage)

			request := &dto.OpenAIResponsesRequest{}
			require.NoError(t, common.Unmarshal(rawBody, request))
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				OriginModelName: request.Model,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:   72,
					ChannelType: constant.ChannelTypeOpenAI,
					ChannelSetting: dto.ChannelSettings{
						PassThroughBodyEnabled: true,
					},
					ChannelOtherSettings: dto.ChannelOtherSettings{
						AllowIncludeObfuscation:     true,
						ResponsesCapabilityRegistry: tt.registry,
					},
				},
			}

			body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

			require.Nil(t, apiErr)
			bodyBytes, err := io.ReadAll(body)
			require.NoError(t, err)
			require.Equal(t, tt.wantUsage, strings.Contains(string(bodyBytes), `"include_usage":true`))
			require.Contains(t, string(bodyBytes), `"include_obfuscation":true`)
			require.Contains(t, string(bodyBytes), `"future_option":{"enabled":true}`)
			require.Equal(t, tt.wantStripped, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOptionsCapabilityStripped))
			if tt.wantStripped {
				require.NotNil(t, converted.StreamOptions)
				require.False(t, converted.StreamOptions.IncludeUsage)
			} else {
				require.Equal(t, string(rawBody), string(bodyBytes))
			}
		})
	}
}

func TestBuildOpenAIResponsesRequestBodyKeepsStreamOptionsCapabilityAfterParamOverride(t *testing.T) {
	unsupported := false
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":"continue","stream_options":{"include_obfuscation":true}}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   72,
			ChannelType: constant.ChannelTypeOpenAI,
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"path":  "stream_options.include_usage",
						"mode":  "set",
						"value": true,
					},
				},
			},
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AllowIncludeObfuscation: true,
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					SupportsStreamOptionsIncludeUsage: &unsupported,
				},
			},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false, buildResponsesRequestBodyOptions{
		ForceConvertedBody: true,
	})

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotContains(t, string(bodyBytes), `"include_usage"`)
	require.Contains(t, string(bodyBytes), `"include_obfuscation":true`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOptionsCapabilityStripped))
}

func TestBuildOpenAIResponsesRequestBodyStripsExplicitFalseStreamOptionsIncludeUsage(t *testing.T) {
	unsupported := false
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":"continue","stream_options":{"include_usage":false,"include_obfuscation":true,"future_option":{"enabled":true}}}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   72,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
			ChannelOtherSettings: dto.ChannelOtherSettings{
				AllowIncludeObfuscation: true,
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					SupportsStreamOptionsIncludeUsage: &unsupported,
				},
			},
		},
	}

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotContains(t, string(bodyBytes), `"include_usage"`)
	require.Contains(t, string(bodyBytes), `"include_obfuscation":true`)
	require.Contains(t, string(bodyBytes), `"future_option":{"enabled":true}`)
	require.NotNil(t, converted.StreamOptions)
	require.False(t, converted.StreamOptions.HasIncludeUsageField())
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOptionsCapabilityStripped))
}

func TestBuildOpenAIResponsesRequestBodyForcesRemoteCompactionFallbackConversion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Preserve the active task."}]},{"type":"compaction_trigger"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = storage.Close()
	})
	c.Set(common.KeyBodyStorage, storage)
	common.SetContextKey(c, constant.ContextKeyResponsesRemoteCompactSyntheticFallback, true)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Preserve the active task."}]},
			{"type":"compaction_trigger"}
		]`),
		Stream: common.GetPointer(true),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		Request:         request,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   169,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
	}

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	require.Nil(t, converted.Stream)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NotEqual(t, string(rawBody), string(bodyBytes))
	require.Contains(t, string(bodyBytes), "Preserve the active task.")
	require.Contains(t, string(bodyBytes), "self-contained handoff summary")
	require.NotContains(t, string(bodyBytes), "compaction_trigger")
}

func TestExecuteOpenAIResponsesRequestRejectsCompactionWithoutEncryptedContentBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"compaction"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, true)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.True(t, types.IsSkipRetryError(err))
	require.ErrorIs(t, err, service.ErrResponsesCompactionContentRequired)
	require.Empty(t, adaptor.requestBodies)
}

func TestExecuteOpenAIResponsesRequestNormalizesEmptyContextCompactionBeforeValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"context_compaction"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]
	}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = storage.Close()
	})
	c.Set(common.KeyBodyStorage, storage)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"context_compaction"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   208,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{successResponsesHTTPResponse()},
	}

	usage, apiErr := executeOpenAIResponsesRequest(c, info, adaptor, request, true)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 1)
	require.NotContains(t, string(adaptor.requestBodies[0]), `"type":"context_compaction"`)
	require.Contains(t, string(adaptor.requestBodies[0]), `"type":"message"`)
}

func TestPrepareResponsesRequestForChatCompatibilityDropsEmptyContextCompaction(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"context_compaction"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   117,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	prepared, result, err := prepareResponsesRequestForChatCompatibility(context.Background(), info, request)

	require.NoError(t, err)
	require.NotSame(t, request, prepared)
	require.Equal(t, 1, result.DroppedCount)
	require.NotContains(t, string(prepared.Input), `"type":"context_compaction"`)
	require.Contains(t, string(prepared.Input), `"type":"message"`)
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryPlainBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   179,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			&http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"message":"bad request","type":"invalid_request_error","code":"bad_request"}}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}, false)
	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func prepareNativeOpaqueResponsesRequestBodyTest(t *testing.T, passThrough bool) (
	*gin.Context,
	*relaycommon.RelayInfo,
	*dto.OpenAIResponsesRequest,
	string,
) {
	t.Helper()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	db, err := gorm.Open(sqlite.Open("file:relay_native_opaque_restore?mode=memory&cache=private"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.SyntheticCompactStateRecord{}))
	model.DB = db

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	scope := service.SyntheticCompactStateScope{
		UserID:      7,
		TokenID:     8,
		Group:       "default",
		Model:       "gpt-5.5",
		ChannelID:   179,
		ChannelType: constant.ChannelTypeOpenAI,
	}
	state, err := service.StoreNativeOpaqueCompactState(context.Background(), scope, "gpt-5.5", "resp_upstream_native", "opaque-token", 1710000000)
	require.NoError(t, err)
	rawBody := []byte(`{"model":"gpt-5.5","previous_response_id":"` + state.ID + `","input":"continue"}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = storage.Close()
	})
	c.Set(common.KeyBodyStorage, storage)
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		UserId:          scope.UserID,
		TokenId:         scope.TokenID,
		TokenGroup:      scope.Group,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   scope.ChannelID,
			ChannelType: scope.ChannelType,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: passThrough,
			},
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	return c, info, request, state.ID
}

func TestBuildOpenAIResponsesRequestBodyRestoresNativeOpaquePreviousResponseIDForPassThrough(t *testing.T) {
	c, info, request, stateID := prepareNativeOpaqueResponsesRequestBodyTest(t, true)

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &retryEncryptedContextAdaptor{}, request, false)

	require.Nil(t, apiErr)
	require.Equal(t, "resp_upstream_native", converted.PreviousResponseID)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Contains(t, string(bodyBytes), `"previous_response_id":"resp_upstream_native"`)
	require.NotContains(t, string(bodyBytes), stateID)
	require.Equal(t, "native_opaque_previous_response_id_restored", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
}

func TestBuildOpenAIResponsesRequestBodyRestoresNativeOpaquePreviousResponseIDForConvertedBody(t *testing.T) {
	c, info, request, stateID := prepareNativeOpaqueResponsesRequestBodyTest(t, false)

	body, converted, apiErr := buildOpenAIResponsesRequestBody(
		c,
		info,
		&retryEncryptedContextAdaptor{},
		request,
		false,
		buildResponsesRequestBodyOptions{ForceConvertedBody: true},
	)

	require.Nil(t, apiErr)
	require.Equal(t, "resp_upstream_native", converted.PreviousResponseID)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Contains(t, string(bodyBytes), `"previous_response_id":"resp_upstream_native"`)
	require.NotContains(t, string(bodyBytes), stateID)
	require.Equal(t, "native_opaque_previous_response_id_restored", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
}

func TestBuildOpenAIResponsesRequestBodyPreservesUnauthenticatedContextCompactionAsOpaque(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"context_compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
			{"type":"context_compaction"},
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_22222222222222222222222222222222"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]
	}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = storage.Close()
	})
	c.Set(common.KeyBodyStorage, storage)
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"context_compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_11111111111111111111111111111111"},
			{"type":"context_compaction"},
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_22222222222222222222222222222222"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   208,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &retryEncryptedContextAdaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, rawBody, bodyBytes)
	require.Equal(t, request.Input, converted.Input)
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNativeUnverified, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestBuildOpenAIResponsesRequestBodyPreservesOpaqueRequestWhenStreamOptionsCapabilityIsDisabled(t *testing.T) {
	unsupported := false
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}],"stream_options":{"include_usage":true,"include_obfuscation":true}}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   72,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
					SupportsStreamOptionsIncludeUsage: &unsupported,
				},
			},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, rawBody, bodyBytes)
	require.Contains(t, string(bodyBytes), `"include_usage":true`)
	require.Contains(t, string(bodyBytes), `"encrypted_content":"opaque"`)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOptionsCapabilityStripped))
}

func TestShouldRouteResponsesViaChatSkipsResponsesCompact(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	responsesInfo := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: 1,
		},
	}
	require.True(t, shouldRouteResponsesViaChat(responsesInfo, false))

	compactInfo := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: 1,
		},
	}
	require.False(t, shouldRouteResponsesViaChat(compactInfo, false))
}

func TestNormalizeResponsesCompactUsageFillsPromptEstimate(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(23)
	usage := &dto.Usage{
		CompletionTokens: 5,
		TotalTokens:      5,
		OutputTokens:     5,
	}

	normalizeResponsesCompactUsage(info, usage)

	require.Equal(t, 23, usage.PromptTokens)
	require.Equal(t, 23, usage.InputTokens)
	require.Equal(t, 28, usage.TotalTokens)
	require.Equal(t, 5, usage.CompletionTokens)
	require.Equal(t, 5, usage.OutputTokens)
}

func TestNormalizeResponsesCompactUsageMirrorsCanonicalTokensWithoutEstimate(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	usage := &dto.Usage{
		PromptTokens:     11,
		CompletionTokens: 5,
		TotalTokens:      16,
	}

	normalizeResponsesCompactUsage(info, usage)

	require.Equal(t, 11, usage.PromptTokens)
	require.Equal(t, 11, usage.InputTokens)
	require.Equal(t, 5, usage.CompletionTokens)
	require.Equal(t, 5, usage.OutputTokens)
	require.Equal(t, 16, usage.TotalTokens)
}

func TestNormalizeResponsesCompactUsageKeepsUpstreamPromptTokens(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(23)
	usage := &dto.Usage{
		PromptTokens:     11,
		CompletionTokens: 5,
		TotalTokens:      16,
		InputTokens:      11,
		OutputTokens:     5,
	}

	normalizeResponsesCompactUsage(info, usage)

	require.Equal(t, 11, usage.PromptTokens)
	require.Equal(t, 11, usage.InputTokens)
	require.Equal(t, 16, usage.TotalTokens)
}

func TestNormalizeResponsesCompactUsageKeepsUpstreamTotalTokens(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(23)
	usage := &dto.Usage{
		PromptTokens:     11,
		CompletionTokens: 5,
		TotalTokens:      20,
		InputTokens:      11,
		OutputTokens:     5,
	}

	normalizeResponsesCompactUsage(info, usage)

	require.Equal(t, 11, usage.PromptTokens)
	require.Equal(t, 5, usage.CompletionTokens)
	require.Equal(t, 20, usage.TotalTokens)
}

func TestFindResponsesViaChatRuleCarriesCustomToolBridgeOption(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat-codex",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
				Options: &model_setting.ProtocolConversionOptions{
					EnableCustomToolBridge: true,
				},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: 1,
		},
	}

	rule, err := findResponsesViaChatRule(context.Background(), info, false, nil)
	require.NoError(t, err)
	require.NotNil(t, rule)
	require.True(t, responsesViaChatOptionsFromRule(rule).EnableCustomToolBridge)
	require.False(t, responsesViaChatOptionsFromRule(nil).EnableCustomToolBridge)
}

func TestFindResponsesViaChatRuleSkipsCodexRemoteCompactionV2Trigger(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: 1,
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"summarize"}]},
			{"type":"compaction_trigger"}
		]`),
	}

	require.True(t, request.HasCompactionTrigger())
	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleSkipsUnauthenticatedSyntheticCompactionMarker(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: 1,
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_33333333333333333333333333333333"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	require.False(t, request.HasCompactionTrigger())
	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleSkipsRemoteCompactionInput(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   168,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"opaque-native-compact"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleSkipsOrdinaryPreviousResponseID(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_ordinary_previous",
	}

	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleAllowsLocalSyntheticPreviousResponseID(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_44444444444444444444444444444444",
	}

	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.NotNil(t, rule)
}

func TestFindResponsesViaChatRuleSkipsOrdinaryPreviousResponseIDEvenWithLocalMarker(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_ordinary_previous",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_44444444444444444444444444444444"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleSkipsNativeOpaquePreviousResponseID(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   157,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: "resp_newapi_nativecmp_n0123456789abcdef0123456789abcdef_0123456789abcdef01234567",
	}

	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.Nil(t, rule)
}

func TestFindResponsesViaChatRuleMatchesExplicitChatOnlyChannel(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})

	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat-channel-168",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    false,
				ChannelIDs:     []int{168},
				ModelPatterns:  []string{`^gpt-5(\..+)?$`},
				Options: &model_setting.ProtocolConversionOptions{
					EnableCustomToolBridge: true,
				},
			},
		},
	}

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   168,
			ChannelType: constant.ChannelTypeDeepSeek,
		},
	}
	request := &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`"hi"`),
	}

	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.NotNil(t, rule)
	require.Equal(t, "responses-to-chat-channel-168", rule.Name)
	require.True(t, responsesViaChatOptionsFromRule(rule).EnableCustomToolBridge)
}

func TestShouldConvertResponsesRequestForCodexEncryptedContextStrip(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				StripCodexEncryptedContext: true,
			},
		},
	}
	require.True(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelOtherSettings.StripCodexEncryptedContext = false
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.RelayMode = relayconstant.RelayModeResponsesCompact
	info.ChannelOtherSettings.StripCodexEncryptedContext = true
	info.ChannelType = constant.ChannelTypeCodex
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelType = constant.ChannelTypeOpenAI
	require.True(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelType = constant.ChannelTypeAzure
	require.True(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelOtherSettings.StripCodexEncryptedContext = false
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelType = constant.ChannelTypeOpenAI
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeNative
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))

	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	require.True(t, relaycommon.ShouldConvertResponsesRequest(info))
}

func TestShouldConvertResponsesRequestForResponsesProxyProfileCompact(t *testing.T) {
	for _, profile := range []dto.ResponsesUpstreamProfile{
		dto.ResponsesUpstreamProfileGenericProxy,
		dto.ResponsesUpstreamProfileChatOnlyProxy,
	} {
		info := &relaycommon.RelayInfo{
			RelayMode: relayconstant.RelayModeResponsesCompact,
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType: constant.ChannelTypeOpenAI,
				ChannelOtherSettings: dto.ChannelOtherSettings{
					ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
					ResponsesUpstreamProfile: profile,
				},
			},
		}

		require.True(t, relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info))
		require.True(t, relaycommon.ShouldConvertResponsesRequest(info))
	}
}

func TestShouldNotForceConvertResponsesRequestForSub2APIHTTPNativeCompact(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
			},
		},
	}

	require.True(t, relaycommon.IsNativeOpenAICompatibleResponsesCompact(info))
	require.False(t, relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info))
	require.False(t, relaycommon.ShouldConvertResponsesRequest(info))
}

func TestShouldConvertResponsesRequestForStatefulProfiles(t *testing.T) {
	for _, profile := range []dto.ResponsesUpstreamProfile{
		dto.ResponsesUpstreamProfileOfficialOpenAI,
		dto.ResponsesUpstreamProfileOfficialNewAPI,
		dto.ResponsesUpstreamProfileSameClusterNewAPI,
		dto.ResponsesUpstreamProfileTrustedNewAPI,
		dto.ResponsesUpstreamProfileSub2APIWSV2,
	} {
		info := &relaycommon.RelayInfo{
			RelayMode: relayconstant.RelayModeResponsesCompact,
			ChannelMeta: &relaycommon.ChannelMeta{
				ChannelType: constant.ChannelTypeOpenAI,
				ChannelOtherSettings: dto.ChannelOtherSettings{
					ResponsesCompactMode:     dto.ResponsesCompactModeAuto,
					ResponsesUpstreamProfile: profile,
				},
			},
		}

		require.False(t, relaycommon.IsSyntheticOpenAICompatibleResponsesCompact(info))
		require.False(t, relaycommon.ShouldConvertResponsesRequest(info))
	}
}

func TestShouldHandleSyntheticResponsesForSyntheticCompactMode(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
	}

	require.True(t, relaycommon.ShouldHandleSyntheticOpenAICompatibleResponses(info))

	info.ChannelOtherSettings.ResponsesCompactMode = dto.ResponsesCompactModeNative
	require.False(t, relaycommon.ShouldHandleSyntheticOpenAICompatibleResponses(info))
}

func TestNewResponsesConvertRequestErrorMapsSyntheticClientErrorsToBadRequest(t *testing.T) {
	for _, err := range []error{
		service.ErrSyntheticCompactStateNotFound,
		service.ErrSyntheticCompactRequiresVisibleInput,
		service.ErrSyntheticCompactStateScopeMismatch,
		service.ErrResponsesRESTPreviousIDUnsupported,
		service.ErrResponsesChatCompatibilityPreviousIDUnsupported,
		service.ErrResponsesChatCompatibilityToolBridgeRequestInvalid,
	} {
		err := newResponsesConvertRequestError(err)

		require.Equal(t, http.StatusBadRequest, err.StatusCode)
		require.Equal(t, types.ErrorCodeConvertRequestFailed, err.GetErrorCode())
	}
}

func TestResponsesHelperRejectsLocalChatCompatibilityPreviousIDForCompactRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 205)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.5")
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponsesCompact,
		Request: &dto.OpenAIResponsesCompactionRequest{
			Model:              "gpt-5.5",
			PreviousResponseID: "resp_newapi_chatcmp_test",
			Input:              common.RawMessage(`[{"type":"compaction_trigger"}]`),
		},
	}

	err := ResponsesHelper(c, info)

	require.NotNil(t, err)
	require.Equal(t, http.StatusBadRequest, err.StatusCode)
	require.Equal(t, types.ErrorCodeConvertRequestFailed, err.GetErrorCode())
	require.ErrorIs(t, err, service.ErrResponsesChatCompatibilityPreviousIDUnsupported)
}

func TestApplyResponsesCompactSummaryModelOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "gpt-5.4")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "gpt-5.5",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesCompactMode: dto.ResponsesCompactModeSynthetic,
			},
		},
	}
	request := &dto.OpenAIResponsesRequest{Model: "gpt-5.5"}

	applyResponsesCompactSummaryModelOverride(c, info, request, false)

	require.Equal(t, "gpt-5.4", request.Model)
	require.Equal(t, "gpt-5.4", info.UpstreamModelName)
	require.Equal(t, "gpt-5.5-openai-compact", info.OriginModelName)

	codexInfo := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.5-openai-compact",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "gpt-5.5",
		},
	}
	codexRequest := &dto.OpenAIResponsesRequest{Model: "gpt-5.5"}

	applyResponsesCompactSummaryModelOverride(c, codexInfo, codexRequest, false)

	require.Equal(t, "gpt-5.4", codexRequest.Model)
	require.Equal(t, "gpt-5.4", codexInfo.UpstreamModelName)
	require.Equal(t, "gpt-5.5-openai-compact", codexInfo.OriginModelName)
}

func TestBuildOpenAIResponsesRequestBodyPreservesOpaqueEncryptedReplayExceptModelMapping(t *testing.T) {
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"context_compaction","id":"client_generated_id","encrypted_content":"opaque-reasoning-state","future_state":{"version":2}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		],
		"instructions":"keep-original-instructions",
		"service_tier":"priority",
		"stream_options":{"include_usage":true,"future_option":{"enabled":true}},
		"future_top_level":{"enabled":true}
	}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	request.Model = "mapped-gpt-5.6-sol"
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, request.Input, converted.Input)
	require.Contains(t, string(bodyBytes), `"model":"mapped-gpt-5.6-sol"`)
	restoredBody := bytes.Replace(bodyBytes, []byte(`"mapped-gpt-5.6-sol"`), []byte(`"gpt-5.6-sol"`), 1)
	require.Equal(t, rawBody, restoredBody)
}

func TestBuildOpenAIResponsesCompactRequestBodyPreservesOpaqueReplayExceptModelMapping(t *testing.T) {
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"type":"compaction","encrypted_content":{"opaque":true},"future_state":[1,2,3]}],
		"instructions":"keep-original-instructions",
		"future_top_level":{"enabled":true}
	}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	request.Model = "mapped-gpt-5.6-sol"
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, request.Input, converted.Input)
	require.Contains(t, string(bodyBytes), `"model":"mapped-gpt-5.6-sol"`)
	restoredBody := bytes.Replace(bodyBytes, []byte(`"mapped-gpt-5.6-sol"`), []byte(`"gpt-5.6-sol"`), 1)
	require.Equal(t, rawBody, restoredBody)
}

func TestExecuteOpenAIResponsesRequestDoesNotMutateOpaqueReplayForCompatibilityRetry(t *testing.T) {
	tests := []struct {
		name     string
		response func() *http.Response
	}{
		{name: "encrypted content verification error", response: func() *http.Response { return encryptedContextRetryResponse(http.StatusBadRequest) }},
		{name: "namespace compatibility error", response: func() *http.Response { return namespaceHistoryRetryResponseAt(1) }},
		{name: "arguments compatibility error", response: argumentsHistoryRetryResponse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"function_call","call_id":"call_1","name":"read_file","namespace":"codex","arguments":"{\"path\":\"README.md\"}"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
			storage, err := common.CreateBodyStorage(rawBody)
			require.NoError(t, err)
			t.Cleanup(func() { _ = storage.Close() })
			c.Set(common.KeyBodyStorage, storage)

			request := &dto.OpenAIResponsesRequest{}
			require.NoError(t, common.Unmarshal(rawBody, request))
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				OriginModelName: request.Model,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:   206,
					ChannelType: constant.ChannelTypeOpenAI,
					ChannelOtherSettings: dto.ChannelOtherSettings{
						ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
					},
				},
			}
			adaptor := &retryEncryptedContextAdaptor{
				responses: []*http.Response{tt.response(), successResponsesHTTPResponse()},
			}

			usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

			require.Nil(t, usage)
			require.Error(t, requestErr)
			require.Len(t, adaptor.requestBodies, 1)
			require.Equal(t, rawBody, adaptor.requestBodies[0])
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesNamespaceHistoryRetry))
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesArgumentsHistoryRetry))
		})
	}
}

func TestDoOpenAIResponsesRequestMarksOpaqueReplayForwardedWhenUpstreamReturnsNonOK(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	relaycommon.MarkResponsesEncryptedReasoningReplayDecision(c, dto.ResponsesEncryptedReasoningReplayCapabilitySupported)
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{encryptedContextRetryResponse(http.StatusBadRequest)},
	}

	usage, requestErr := doOpenAIResponsesRequest(
		c,
		&relaycommon.RelayInfo{},
		adaptor,
		bytes.NewReader([]byte(`{"model":"gpt-5.6-sol"}`)),
	)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(
		t,
		relaycommon.ResponsesEncryptedReasoningReplayActionForwardedNative,
		common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction),
	)
}

func TestResponsesHelperRejectsUnsupportedOpaqueEncryptedReplayBeforeRelay(t *testing.T) {
	unsupported := false
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 168)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.6-sol")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
		ResponsesCapabilityRegistry: &dto.ResponsesChannelCapabilityRegistry{
			SupportsEncryptedReasoningReplay: &unsupported,
		},
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},
				{"type":"message","role":"user","content":"continue"}
			]`),
		},
	}

	err := ResponsesHelper(c, info)

	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.ErrorIs(t, err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
	require.Equal(t, "unsupported", common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionRejectedUnsupported, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestResponsesHelperRejectsOpaqueCompactWithoutNativeCompactBeforeRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 208)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.6-sol")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesCompactMode:     dto.ResponsesCompactModeSynthetic,
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
	})
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol",
		Request: &dto.OpenAIResponsesCompactionRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},
				{"type":"message","role":"user","content":"continue"}
			]`),
		},
	}

	err := ResponsesHelper(c, info)

	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.ErrorIs(t, err, relaycommon.ErrResponsesEncryptedReasoningReplayUnsupported)
	require.Equal(t, "supported", common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionRejectedUnsupported, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestApplyResponsesCompactSummaryModelOverrideSkipsOpaqueEncryptedReplay(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "gpt-5.4")
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: "gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeCodex,
			UpstreamModelName: "mapped-gpt-5.6-sol",
		},
	}
	request := &dto.OpenAIResponsesRequest{Model: "mapped-gpt-5.6-sol"}

	applyResponsesCompactSummaryModelOverride(c, info, request, true)

	require.Equal(t, "mapped-gpt-5.6-sol", request.Model)
	require.Equal(t, "mapped-gpt-5.6-sol", info.UpstreamModelName)
}
