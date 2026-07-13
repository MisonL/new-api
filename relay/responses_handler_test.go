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

func TestExecuteOpenAIResponsesRequestRetriesEncryptedReasoningFailure(t *testing.T) {
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
	if err != nil {
		t.Logf("retry error: status=%d code=%s msg=%q", err.StatusCode, err.GetErrorCode(), err.Error())
	}
	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[0]), `"encrypted_content":"opaque"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"encrypted_content":"opaque"`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
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

func TestExecuteOpenAIResponsesRequestRetriesEncryptedReasoningBeforeStatusMapping(t *testing.T) {
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

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[0]), `"encrypted_content":"opaque"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"encrypted_content":"opaque"`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
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

func TestExecuteOpenAIResponsesRequestRetryForcesConvertedBodyWhenPassThroughEnabled(t *testing.T) {
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
	require.Nil(t, retryErr)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Equal(t, string(rawBody), string(adaptor.requestBodies[0]))
	require.NotContains(t, string(adaptor.requestBodies[1]), `"encrypted_content":"opaque"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"text":"hello"`)
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

func TestFindResponsesViaChatRuleAllowsLocalSyntheticCompactionForCleanup(t *testing.T) {
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
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_missing"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	require.False(t, request.HasCompactionTrigger())
	rule, err := findResponsesViaChatRule(context.Background(), info, false, request)
	require.NoError(t, err)
	require.NotNil(t, rule)
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
		PreviousResponseID: "resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_missing",
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
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_missing"},
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
		PreviousResponseID: "resp_newapi_nativecmp_opaque",
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

func TestShouldConvertResponsesRequestForSub2APIHTTPLimitedNativeCompact(t *testing.T) {
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
	require.True(t, relaycommon.ShouldConvertResponsesRequest(info))
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
	} {
		err := newResponsesConvertRequestError(err)

		require.Equal(t, http.StatusBadRequest, err.StatusCode)
		require.Equal(t, types.ErrorCodeConvertRequestFailed, err.GetErrorCode())
	}
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

	applyResponsesCompactSummaryModelOverride(c, info, request)

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

	applyResponsesCompactSummaryModelOverride(c, codexInfo, codexRequest)

	require.Equal(t, "gpt-5.4", codexRequest.Model)
	require.Equal(t, "gpt-5.4", codexInfo.UpstreamModelName)
	require.Equal(t, "gpt-5.5-openai-compact", codexInfo.OriginModelName)
}
