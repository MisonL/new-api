package relay

import (
	"bytes"
	"context"
	"errors"
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
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
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

type stageSizeInflatingResponsesAdaptor struct {
	streamingCompatibilityRetryAdaptor
	conversionCallCount    int
	richConversionCount    int
	boundedConversionCount int
	inflateBoundedRecovery bool
}

type inboundPreemptiveCompactionAdaptor struct {
	streamingCompatibilityRetryAdaptor
	conversionInputs []dto.OpenAIResponsesRequest
}

type preemptiveRecoveryBuildErrorAdaptor struct {
	streamingCompatibilityRetryAdaptor
	conversionCalls int
}

type responsesPassThroughProbeAdaptor struct {
	retryEncryptedContextAdaptor
	conversionCalls int
}

func (m *responsesPassThroughProbeAdaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	m.conversionCalls++
	return request, nil
}

func (m *inboundPreemptiveCompactionAdaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	m.conversionInputs = append(m.conversionInputs, request)
	return request, nil
}

func (m *preemptiveRecoveryBuildErrorAdaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	m.conversionCalls++
	if m.conversionCalls == 2 {
		return nil, errors.New("recovery conversion failed")
	}
	return request, nil
}

func (m *stageSizeInflatingResponsesAdaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	m.conversionCallCount++
	if m.conversionCallCount == 2 {
		m.richConversionCount++
		request.Instructions = common.RawMessage(`"` + strings.Repeat("x", 33<<20) + `"`)
	} else if m.conversionCallCount == 3 {
		m.boundedConversionCount++
		if m.inflateBoundedRecovery {
			request.Instructions = common.RawMessage(`"` + strings.Repeat("x", 33<<20) + `"`)
		}
	}
	return request, nil
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
	return missingEncryptedContentErrorAfterStreamPreludeAt(0)
}

func missingEncryptedContentErrorAfterStreamPreludeAt(inputIndex int) *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.in_progress","response":{"id":"resp_partial","object":"response"}}`,
		fmt.Sprintf(`data: {"type":"response.error","error":{"message":"[ObjectParam] [input[%d].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[%d].encrypted_content'.","type":"invalid_request_error","code":"missing_required_parameter"}}`, inputIndex, inputIndex),
		`data: [DONE]`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func missingEncryptedContentErrorAfterEmptyToolCallAddedAt(inputIndex int) *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.output_item.added","sequence_number":2,"output_index":0,"item":{"id":"ctc_pending","type":"custom_tool_call","status":"in_progress","call_id":"call_pending","name":"exec"}}`,
		fmt.Sprintf(`data: {"type":"response.error","error":{"message":"[ObjectParam] [input[%d].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[%d].encrypted_content'.","type":"invalid_request_error","code":"missing_required_parameter"}}`, inputIndex, inputIndex),
		`data: [DONE]`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func missingEncryptedContentErrorAfterExplicitlyEmptyToolCallAddedAt(inputIndex int) *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.output_item.added","sequence_number":2,"output_index":0,"item":{"id":"ctc_pending","type":"custom_tool_call","status":"in_progress","call_id":"call_pending","name":"exec","content":[],"summary":[],"arguments":"","input":"","encrypted_content":""}}`,
		fmt.Sprintf(`data: {"type":"response.error","error":{"message":"[ObjectParam] [input[%d].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[%d].encrypted_content'.","type":"invalid_request_error","code":"missing_required_parameter"}}`, inputIndex, inputIndex),
		`data: [DONE]`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func missingEncryptedContentErrorAfterToolCallInputAddedAt(inputIndex int) *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"ctc_pending","type":"custom_tool_call","status":"in_progress","call_id":"call_pending","name":"exec","input":"{\"command\":\"pwd\"}"}}`,
		fmt.Sprintf(`data: {"type":"response.error","error":{"message":"[ObjectParam] [input[%d].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[%d].encrypted_content'.","type":"invalid_request_error","code":"missing_required_parameter"}}`, inputIndex, inputIndex),
		`data: [DONE]`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
}

func missingEncryptedContentErrorAfterUnknownToolCallPayloadAddedAt(inputIndex int) *http.Response {
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_partial","object":"response"}}`,
		`data: {"type":"response.output_item.added","sequence_number":2,"output_index":0,"item":{"id":"ctc_pending","type":"custom_tool_call","status":"in_progress","call_id":"call_pending","name":"exec","future_payload":{"command":"pwd"}}}`,
		fmt.Sprintf(`data: {"type":"response.error","error":{"message":"[ObjectParam] [input[%d].encrypted_content] [missing_required_parameter] Missing required parameter: 'input[%d].encrypted_content'.","type":"invalid_request_error","code":"missing_required_parameter"}}`, inputIndex, inputIndex),
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

func upstreamRequestTooLargeHTTPResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusRequestEntityTooLarge,
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><head><title>413 Payload Too Large</title></head><body>
			<center><h1>413 Payload Too Large</h1></center><hr><center>cloudflare</center>
		</body></html>`)),
		Header: http.Header{"Content-Type": []string{"text/html"}},
	}
}

func TestExecuteOpenAIResponsesRequestRecoversCodexLocalCompactionAfterUpstream413(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{
		"request_kind":"compaction",
		"compaction":{"implementation":"responses","trigger":"automatic"}
	}`)

	stream := true
	largeImageData := strings.Repeat("A", 256*1024)
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.6-sol",
		Conversation:       common.RawMessage(`"conv_should_not_be_replayed"`),
		ContextManagement:  common.RawMessage(`{"compact_threshold":123}`),
		PreviousResponseID: "resp_should_not_be_replayed",
		Stream:             &stream,
		Tools:              common.RawMessage(`[{"type":"function","name":"large_tool","parameters":{"type":"object"}}]`),
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Summarize the active coding task and preserve the latest user request."}]},
			{"type":"custom_tool_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"data:image/png;base64,%s"}]},
			{"type":"additional_tools","tools":[{"type":"custom","name":"large_additional_tool"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_TASK_MUST_SURVIVE"}]}
		]`, largeImageData)),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
		StreamStatus:    relaycommon.NewStreamStatus(),
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				upstreamRequestTooLargeHTTPResponse(),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Contains(t, string(adaptor.requestBodies[0]), "data:image/png;base64,")
	require.Less(t, len(adaptor.requestBodies[1]), len(adaptor.requestBodies[0]))
	require.NotContains(t, string(adaptor.requestBodies[1]), "data:image/png;base64,")
	require.NotContains(t, string(adaptor.requestBodies[1]), `"tools"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"additional_tools"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"previous_response_id"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"conversation"`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"context_management"`)
	require.Contains(t, string(adaptor.requestBodies[1]), "LATEST_TASK_MUST_SURVIVE")
	var recoveredRequest dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(adaptor.requestBodies[1], &recoveredRequest))
	var inputItems []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(recoveredRequest.Input, &inputItems))
	require.Len(t, inputItems, 1)
	var inputType string
	require.NoError(t, common.Unmarshal(inputItems[0]["type"], &inputType))
	require.Equal(t, "message", inputType)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCodexLocalCompaction413Recovery))
	require.Equal(t, int64(len(adaptor.requestBodies[0])), common.GetContextKeyInt64(c, constant.ContextKeyResponsesCodexLocalCompactionOriginalBodyBytes))
	require.Equal(t, int64(len(adaptor.requestBodies[1])), common.GetContextKeyInt64(c, constant.ContextKeyResponsesCodexLocalCompactionReducedBodyBytes))
	require.Contains(t, recorder.Body.String(), `"type":"response.completed"`)
}

func TestExecuteOpenAIResponsesRequestPreemptivelyReducesOversizedCodexLocalCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{
		"request_kind":"compaction",
		"compaction":{"implementation":"responses","trigger":"automatic"}
	}`)

	stream := true
	largeImageData := strings.Repeat("A", 33<<20)
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5.6-sol",
		Conversation:       common.RawMessage(`"conv_should_not_be_replayed"`),
		ContextManagement:  common.RawMessage(`{"compact_threshold":123}`),
		PreviousResponseID: "resp_should_not_be_replayed",
		Stream:             &stream,
		Tools:              common.RawMessage(`[{"type":"function","name":"large_tool","parameters":{"type":"object"}}]`),
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Summarize the active coding task and preserve the latest user request."}]},
			{"type":"custom_tool_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"data:image/png;base64,%s"}]},
			{"type":"additional_tools","tools":[{"type":"custom","name":"large_additional_tool"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_TASK_MUST_SURVIVE"}]}
		]`, largeImageData)),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
		StreamStatus:    relaycommon.NewStreamStatus(),
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{successStreamingResponsesHTTPResponse()},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	if err != nil {
		t.Fatalf("unexpected preemptive reduction error: status=%d code=%s error=%q openai=%+v stream=%s", err.StatusCode, err.GetErrorCode(), err.Error(), err.ToOpenAIError(), info.StreamStatus.Summary())
	}
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 1)
	require.LessOrEqual(t, len(adaptor.requestBodies[0]), service.CodexLocalCompaction413RecoverySafeBodyMaxBytes)
	require.NotContains(t, string(adaptor.requestBodies[0]), "data:image/png;base64,")
	require.NotContains(t, string(adaptor.requestBodies[0]), `"tools"`)
	require.NotContains(t, string(adaptor.requestBodies[0]), `"additional_tools"`)
	require.NotContains(t, string(adaptor.requestBodies[0]), `"previous_response_id"`)
	require.NotContains(t, string(adaptor.requestBodies[0]), `"conversation"`)
	require.NotContains(t, string(adaptor.requestBodies[0]), `"context_management"`)
	require.Contains(t, string(adaptor.requestBodies[0]), "LATEST_TASK_MUST_SURVIVE")
	require.Equal(t, codexLocalCompactionBodyReductionSourcePreemptiveSize,
		common.GetContextKeyString(c, constant.ContextKeyResponsesCodexLocalCompactionBodyReductionSource))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCodexLocalCompaction413Recovery))
	require.Contains(t, recorder.Body.String(), `"type":"response.completed"`)
}

func TestExecuteOpenAIResponsesRequestPreemptivelyReducesOversizedCodexLocalCompactionBeforeOriginalOutboundBuild(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{
		"request_kind":"compaction",
		"compaction":{"implementation":"responses","trigger":"automatic"}
	}`)
	common.SetContextKey(c, constant.ContextKeyRequestBodySize, int64(33<<20))

	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_TASK_MUST_SURVIVE"}]},
			{"type":"custom_tool_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"data:image/png;base64,%s"}]}
		]`, strings.Repeat("A", 33<<20))),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
		StreamStatus:    relaycommon.NewStreamStatus(),
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &inboundPreemptiveCompactionAdaptor{
		streamingCompatibilityRetryAdaptor: streamingCompatibilityRetryAdaptor{
			retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
				responses: []*http.Response{successStreamingResponsesHTTPResponse()},
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	if err != nil {
		t.Fatalf("unexpected preemptive inbound reduction error: status=%d code=%s error=%q openai=%+v stream=%s", err.StatusCode, err.GetErrorCode(), err.Error(), err.ToOpenAIError(), info.StreamStatus.Summary())
	}
	require.NotNil(t, usage)
	require.Len(t, adaptor.conversionInputs, 1)
	require.NotContains(t, string(adaptor.conversionInputs[0].Input), "data:image/png;base64,")
	require.Len(t, adaptor.requestBodies, 1)
	require.LessOrEqual(t, len(adaptor.requestBodies[0]), service.CodexLocalCompaction413RecoverySafeBodyMaxBytes)
	require.NotContains(t, string(adaptor.requestBodies[0]), "data:image/png;base64,")
	require.Equal(t, codexLocalCompactionBodyReductionSourcePreemptiveSize,
		common.GetContextKeyString(c, constant.ContextKeyResponsesCodexLocalCompactionBodyReductionSource))
}

func TestExecuteOpenAIResponsesRequestRestoresBodyMetadataWhenPreemptiveRecoveryBuildFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`)

	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input:  common.RawMessage(fmt.Sprintf(`"%s"`, strings.Repeat("A", 33<<20))),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		IsStream:        true,
		StreamStatus:    relaycommon.NewStreamStatus(),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &preemptiveRecoveryBuildErrorAdaptor{}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Equal(t, types.ErrorCodeUpstreamRequestTooLarge, err.GetErrorCode())
	require.Equal(t, 2, adaptor.conversionCalls)
	require.Greater(t, info.UpstreamRequestBodySize, int64(service.CodexLocalCompaction413RecoverySafeBodyMaxBytes))
	require.NotNil(t, info.UpstreamRequestGetBody)
}

func TestExecuteOpenAIResponsesRequestKeepsCompatibilityRetryAfterPreemptiveBodyReduction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{
		"request_kind":"compaction",
		"compaction":{"implementation":"responses"}
	}`)

	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input: common.RawMessage(fmt.Sprintf(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_TASK_MUST_SURVIVE"}]},
			{"type":"custom_tool_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"data:image/png;base64,%s"}]}
		]`, strings.Repeat("A", 33<<20))),
		StreamOptions: &dto.StreamOptions{IncludeUsage: true},
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
		StreamStatus:    relaycommon.NewStreamStatus(),
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				streamOptionsIncludeUsageErrorAfterStreamPrelude(),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	if err != nil {
		t.Fatalf("unexpected preemptive compatibility retry error: status=%d code=%s error=%q openai=%+v stream=%s", err.StatusCode, err.GetErrorCode(), err.Error(), err.ToOpenAIError(), info.StreamStatus.Summary())
	}
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	for _, body := range adaptor.requestBodies {
		require.LessOrEqual(t, len(body), service.CodexLocalCompaction413RecoverySafeBodyMaxBytes)
		require.NotContains(t, string(body), "data:image/png;base64,")
	}
	require.Contains(t, string(adaptor.requestBodies[0]), `"include_usage":true`)
	require.NotContains(t, string(adaptor.requestBodies[1]), `"include_usage"`)
	require.Equal(t, codexLocalCompactionBodyReductionSourcePreemptiveSize,
		common.GetContextKeyString(c, constant.ContextKeyResponsesCodexLocalCompactionBodyReductionSource))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOptionsIncludeUsageRetry))
}

func TestExecuteOpenAIResponsesRequestStopsCrossChannelRetryWhenCodexLocalCompactionRecoveryFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{
		"request_kind":"compaction",
		"compaction":{"implementation":"responses"}
	}`)

	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input: common.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"Summarize the active task."}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_TASK_MUST_SURVIVE"}]}
		]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{
			upstreamRequestTooLargeHTTPResponse(),
			upstreamRequestTooLargeHTTPResponse(),
			upstreamRequestTooLargeHTTPResponse(),
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 3)
	require.True(t, types.IsSkipRetryError(err))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCodexLocalCompaction413RecoveryFailed))
}

func TestExecuteOpenAIResponsesRequestUsesBoundedRecoveryAfterRichRecovery413(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{
		"request_kind":"compaction",
		"compaction":{"implementation":"responses"}
	}`)

	stream := true
	richOnlyContext := "RICH_RECOVERY_CONTEXT_MUST_NOT_REACH_BOUNDED_STAGE"
	latestContext := "LATEST_CONTEXT_MUST_REACH_BOUNDED_STAGE"
	request := &dto.OpenAIResponsesRequest{
		Model:        "gpt-5.6-sol",
		Instructions: common.RawMessage(`"COMPACTION_INSTRUCTIONS_MUST_SURVIVE"`),
		Stream:       &stream,
		Input: common.RawMessage(fmt.Sprintf(`"%s%s%s%s"`,
			strings.Repeat("A", 96*1024),
			richOnlyContext,
			strings.Repeat("B", 96*1024*2),
			latestContext,
		)),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
		StreamStatus:    relaycommon.NewStreamStatus(),
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				upstreamRequestTooLargeHTTPResponse(),
				upstreamRequestTooLargeHTTPResponse(),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	if err != nil {
		t.Fatalf("unexpected recovery error: status=%d code=%s error=%q openai=%+v stream=%s", err.StatusCode, err.GetErrorCode(), err.Error(), err.ToOpenAIError(), info.StreamStatus.Summary())
	}
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 3)
	require.Contains(t, string(adaptor.requestBodies[1]), richOnlyContext)
	require.NotContains(t, string(adaptor.requestBodies[2]), richOnlyContext)
	require.Contains(t, string(adaptor.requestBodies[1]), latestContext)
	require.Contains(t, string(adaptor.requestBodies[2]), latestContext)
	require.Contains(t, string(adaptor.requestBodies[1]), "COMPACTION_INSTRUCTIONS_MUST_SURVIVE")
	require.Contains(t, string(adaptor.requestBodies[2]), "COMPACTION_INSTRUCTIONS_MUST_SURVIVE")
}

func TestExecuteOpenAIResponsesRequestBoundsRecoveryAfterConvertedRichBodyExceedsSafeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`)

	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input:  common.RawMessage(`"` + strings.Repeat("visible context ", 60000) + `"`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
		StreamStatus:    relaycommon.NewStreamStatus(),
		DisablePing:     true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &stageSizeInflatingResponsesAdaptor{
		streamingCompatibilityRetryAdaptor: streamingCompatibilityRetryAdaptor{
			retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
				responses: []*http.Response{
					upstreamRequestTooLargeHTTPResponse(),
					successStreamingResponsesHTTPResponse(),
				},
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	if err != nil {
		t.Fatalf("unexpected recovery error: status=%d code=%s error=%q openai=%+v stream=%s", err.StatusCode, err.GetErrorCode(), err.Error(), err.ToOpenAIError(), info.StreamStatus.Summary())
	}
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.Equal(t, 1, adaptor.richConversionCount)
	require.Equal(t, 1, adaptor.boundedConversionCount)
	require.LessOrEqual(t, len(adaptor.requestBodies[1]), service.CodexLocalCompaction413RecoverySafeBodyMaxBytes)
}

func TestExecuteOpenAIResponsesRequestStopsWhenBoundedRecoveryStillExceedsSafeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`)

	stream := true
	request := &dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input:  common.RawMessage(`"` + strings.Repeat("visible context ", 60000) + `"`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-sol",
		IsStream:        true,
		StreamStatus:    relaycommon.NewStreamStatus(),
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   205,
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}
	adaptor := &stageSizeInflatingResponsesAdaptor{
		inflateBoundedRecovery: true,
		streamingCompatibilityRetryAdaptor: streamingCompatibilityRetryAdaptor{
			retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
				responses: []*http.Response{upstreamRequestTooLargeHTTPResponse()},
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.True(t, types.IsSkipRetryError(err))
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, 1, adaptor.richConversionCount)
	require.Equal(t, 1, adaptor.boundedConversionCount)
}

func TestExecuteOpenAIResponsesRequestDoesNotUseCodexLocal413RecoveryForOtherRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stream := true

	testCases := []struct {
		name         string
		turnMetadata string
		input        common.RawMessage
	}{
		{
			name:  "ordinary responses",
			input: common.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"ordinary request"}]}]`),
		},
		{
			name: "remote v2 compaction",
			turnMetadata: `{
				"request_kind":"compaction",
				"compaction":{"implementation":"responses_compaction_v2"}
			}`,
			input: common.RawMessage(`[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]},
				{"type":"compaction_trigger"}
			]`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			if tc.turnMetadata != "" {
				c.Request.Header.Set("X-Codex-Turn-Metadata", tc.turnMetadata)
			}
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				OriginModelName: "gpt-5.6-sol",
				IsStream:        true,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:   205,
					ChannelType: constant.ChannelTypeOpenAI,
				},
			}
			adaptor := &retryEncryptedContextAdaptor{responses: []*http.Response{upstreamRequestTooLargeHTTPResponse()}}

			usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
				Model:  "gpt-5.6-sol",
				Input:  tc.input,
				Stream: &stream,
			}, false)

			require.Nil(t, usage)
			require.Error(t, err)
			require.Len(t, adaptor.requestBodies, 1)
			require.False(t, types.IsSkipRetryError(err))
		})
	}
}

func TestShouldNotPreemptivelyReduceOpaqueEncryptedReplay(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`)
	common.SetContextKey(c, constant.ContextKeyRequestBodySize, int64(service.CodexLocalCompaction413RecoverySafeBodyMaxBytes+1))
	stream := true
	request := dto.OpenAIResponsesRequest{
		Model:  "gpt-5.6-sol",
		Stream: &stream,
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},
			{"type":"message","role":"user","content":"continue"}
		]`),
	}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		IsStream:  true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
		},
	}

	require.False(t, shouldPreemptivelyReduceCodexLocalCompactionInboundRequest(c, info, request))
	require.False(t, shouldPreemptivelyReduceCodexLocalCompactionForBodySize(
		c,
		info,
		request,
		int64(service.CodexLocalCompaction413RecoverySafeBodyMaxBytes+1),
	))
}

func TestExecuteOpenAIResponsesRequestDoesNotRecoverOpaqueReplayAfter413(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"message","role":"user","content":"continue"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`)
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		IsStream:        true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
			},
		},
	}
	adaptor := &retryEncryptedContextAdaptor{
		responses: []*http.Response{upstreamRequestTooLargeHTTPResponse(), successResponsesHTTPResponse()},
	}

	usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Equal(t, http.StatusRequestEntityTooLarge, requestErr.StatusCode)
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCodexLocalCompaction413Recovery))
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
	rawBody := []byte(`{"model":"gpt-5.5","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
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

	usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Equal(t, http.StatusBadRequest, requestErr.StatusCode)
	require.Equal(t, types.ErrorCode("thinking_signature_invalid"), requestErr.GetErrorCode())
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Len(t, adaptor.responses, 1)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotMutateOpaqueReplayForCompatibilityRetry(t *testing.T) {
	tests := []struct {
		name     string
		response func() *http.Response
	}{
		{
			name: "encrypted content verification error",
			response: func() *http.Response {
				return encryptedContextRetryResponse(http.StatusBadRequest)
			},
		},
		{
			name:     "namespace compatibility error",
			response: func() *http.Response { return namespaceHistoryRetryResponseAt(1) },
		},
		{
			name:     "arguments compatibility error",
			response: argumentsHistoryRetryResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"function_call","call_id":"call_1","name":"read_file","namespace":"codex","arguments":"{\"path\":\"README.md\"}"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
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

func TestExecuteOpenAIResponsesRequestDoesNotRetryOpaqueWrappedMissingEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
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

	usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Equal(t, http.StatusBadRequest, requestErr.StatusCode)
	require.Equal(t, types.ErrorCode("missing_required_parameter"), requestErr.GetErrorCode())
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Len(t, adaptor.responses, 1)
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

func TestExecuteOpenAIResponsesRequestDoesNotRetryOpaqueAgentMessageMissingEncryptedContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"agent_message","role":"assistant","content":[{"type":"output_text","text":"previous answer"},{"type":"encrypted_content","encrypted_content":"opaque"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
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

	usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"agent_message","role":"assistant","content":[{"type":"output_text","text":"previous answer"},{"type":"encrypted_content","encrypted_content":"opaque"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Equal(t, http.StatusBadRequest, requestErr.StatusCode)
	require.Equal(t, types.ErrorCode("missing_required_parameter"), requestErr.GetErrorCode())
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Len(t, adaptor.responses, 1)
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

func TestExecuteOpenAIResponsesRequestRetriesValidCustomToolOutputAfterStreamingPrelude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	inputItems := make([]string, 0, 75)
	for index := 0; index < 73; index++ {
		inputItems = append(inputItems, fmt.Sprintf(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"history-%d"}]}`, index))
	}
	inputItems = append(inputItems, `{"type":"custom_tool_call_output","call_id":"call_73","output":[{"type":"input_text","text":"tool result"}]}`)
	inputItems = append(inputItems, `{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}`)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-terra",
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
				missingEncryptedContentErrorAfterStreamPreludeAt(73),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-terra",
		Input: common.RawMessage("[" + strings.Join(inputItems, ",") + "]"),
	}, false)

	if err != nil {
		t.Fatalf("expected retry to succeed, got status=%d code=%s error=%q openai=%+v stream=%s", err.StatusCode, err.GetErrorCode(), err.Error(), err.ToOpenAIError(), info.StreamStatus.Summary())
	}
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.JSONEq(t, string(adaptor.requestBodies[0]), string(adaptor.requestBodies[1]))
	require.Contains(t, string(adaptor.requestBodies[1]), `"type":"custom_tool_call_output"`)
	require.Contains(t, string(adaptor.requestBodies[1]), `"call_id":"call_73"`)
	require.Contains(t, recorder.Body.String(), "resp_ok")
	require.NotContains(t, recorder.Body.String(), "resp_partial")
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestRetriesValidCustomToolOutputAfterEmptyToolCallAddedPrelude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-terra",
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
				missingEncryptedContentErrorAfterEmptyToolCallAddedAt(0),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-terra",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call_output","call_id":"call_73","output":[{"type":"input_text","text":"tool result"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	if err != nil {
		t.Fatalf("expected retry after empty output item added to succeed, got status=%d code=%s error=%q openai=%+v stream=%s writer_written=%t prelude_retry_safe=%t", err.StatusCode, err.GetErrorCode(), err.Error(), err.ToOpenAIError(), info.StreamStatus.Summary(), c.Writer.Written(), common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe))
	}
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.NotContains(t, recorder.Body.String(), "resp_partial")
	require.Contains(t, recorder.Body.String(), "resp_ok")
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
	require.Empty(t, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedContextRetryBlockedAfterEvent))
}

func TestExecuteOpenAIResponsesRequestRetriesAfterExplicitlyEmptyToolCallAddedPrelude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-terra",
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
				missingEncryptedContentErrorAfterExplicitlyEmptyToolCallAddedAt(0),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-terra",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call_output","call_id":"call_73","output":[{"type":"input_text","text":"tool result"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	if err != nil {
		t.Fatalf("expected retry after explicitly empty output item added to succeed, got status=%d code=%s error=%q openai=%+v stream=%s writer_written=%t prelude_retry_safe=%t", err.StatusCode, err.GetErrorCode(), err.Error(), err.ToOpenAIError(), info.StreamStatus.Summary(), c.Writer.Written(), common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamPreludeRetrySafe))
	}
	require.NotNil(t, usage)
	require.Len(t, adaptor.requestBodies, 2)
	require.NotContains(t, recorder.Body.String(), "resp_partial")
	require.Contains(t, recorder.Body.String(), "resp_ok")
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryAfterToolCallInputAdded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-terra",
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
				missingEncryptedContentErrorAfterToolCallInputAddedAt(0),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-terra",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call_output","call_id":"call_73","output":[{"type":"input_text","text":"tool result"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, recorder.Body.String(), "ctc_pending")
	require.NotContains(t, recorder.Body.String(), "resp_ok")
	require.Equal(t, dto.ResponsesOutputTypeItemAdded, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedContextRetryBlockedAfterEvent))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryAfterUnknownToolCallPayloadAdded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.6-terra",
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
				missingEncryptedContentErrorAfterUnknownToolCallPayloadAddedAt(0),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, err := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-terra",
		Input: common.RawMessage(`[
			{"type":"custom_tool_call_output","call_id":"call_73","output":[{"type":"input_text","text":"tool result"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, err)
	require.Len(t, adaptor.requestBodies, 1)
	require.Contains(t, recorder.Body.String(), "future_payload")
	require.NotContains(t, recorder.Body.String(), "resp_ok")
	require.Equal(t, dto.ResponsesOutputTypeItemAdded, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedContextRetryBlockedAfterEvent))
}

func TestExecuteOpenAIResponsesRequestDoesNotRetryMultipleOpaqueEncryptedItems(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"first"},{"type":"reasoning","encrypted_content":"second"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
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

	usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"first"},
			{"type":"reasoning","encrypted_content":"second"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Equal(t, http.StatusBadRequest, requestErr.StatusCode)
	require.Equal(t, types.ErrorCode("missing_required_parameter"), requestErr.GetErrorCode())
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Len(t, adaptor.responses, 2)
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

func TestExecuteOpenAIResponsesRequestDoesNotRetryOpaqueReplayAfterStreamingPrelude(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	stream := true
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		IsStream:        true,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}
	request.Stream = &stream
	adaptor := &streamingCompatibilityRetryAdaptor{
		retryEncryptedContextAdaptor: retryEncryptedContextAdaptor{
			responses: []*http.Response{
				missingEncryptedContentErrorAfterStreamPrelude(),
				successStreamingResponsesHTTPResponse(),
			},
		},
	}

	usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, request, false)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Equal(t, types.ErrorCode("missing_required_parameter"), requestErr.GetErrorCode())
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Len(t, adaptor.responses, 1)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
	require.NotContains(t, recorder.Body.String(), "resp_ok")
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

func TestExecuteOpenAIResponsesRequestDoesNotRetryOpaqueEncryptedReasoningAndAppliesStatusMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{"model":"gpt-5.5","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
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

	usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Equal(t, http.StatusTeapot, requestErr.StatusCode)
	require.Equal(t, http.StatusBadRequest, requestErr.OriginalStatusCode)
	require.Equal(t, types.ErrorCode("thinking_signature_invalid"), requestErr.GetErrorCode())
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Len(t, adaptor.responses, 1)
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

func TestExecuteOpenAIResponsesRequestDoesNotRetryOpaqueBodyWhenPassThroughEnabled(t *testing.T) {
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

	usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.5",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}
		]`),
	}, false)

	require.Nil(t, usage)
	require.Error(t, requestErr)
	require.Equal(t, http.StatusBadRequest, requestErr.StatusCode)
	require.Equal(t, types.ErrorCode("thinking_signature_invalid"), requestErr.GetErrorCode())
	require.Len(t, adaptor.requestBodies, 1)
	require.Equal(t, rawBody, adaptor.requestBodies[0])
	require.Len(t, adaptor.responses, 1)
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

func TestBuildOpenAIResponsesRequestBodyKeepsNativeReasoningContextOnRawPassThrough(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":"continue","reasoning":{"effort":"high","context":"all_turns","future_reasoning_option":{"enabled":true}}}`)
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
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
			},
		},
	}
	adaptor := &responsesPassThroughProbeAdaptor{}

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, adaptor, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, rawBody, bodyBytes)
	require.Equal(t, 0, adaptor.conversionCalls)
	require.JSONEq(t, string(request.Reasoning.Context), string(converted.Reasoning.Context))
	require.Equal(t, relaycommon.ResponsesReasoningContextActionForwardedSupported, common.GetContextKeyString(c, constant.ContextKeyResponsesReasoningContextAction))
}

func TestBuildOpenAIResponsesRequestBodyAppliesCodexHeaderOverrideOnRawPassThrough(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":"continue","reasoning":{"effort":"high"}}`)
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
		RequestHeaders: map[string]string{
			"X-Codex-Turn-Metadata": "turn-metadata",
			"X-Client-Request-Id":   "request-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   114,
			ChannelType: constant.ChannelTypeCodex,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
			ChannelOtherSettings: dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled:            true,
					Mode:               dto.HeaderProfileModeFixed,
					SelectedProfileIDs: []string{"codex-cli@latest"},
				},
			},
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode":  "pass_headers",
						"value": []interface{}{"X-Codex-Turn-Metadata", "X-Client-Request-Id"},
					},
					map[string]interface{}{
						"mode": "copy_header",
						"from": "X-Client-Request-Id",
						"to":   "Session_id",
					},
				},
			},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &responsesPassThroughProbeAdaptor{}, request, true)
	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, rawBody, bodyBytes)
	require.Equal(t, "turn-metadata", info.RuntimeHeadersOverride["x-codex-turn-metadata"])
	require.Equal(t, "request-123", info.RuntimeHeadersOverride["session_id"])
	require.Equal(t, "request-123", info.RuntimePassedRequestHeaders["x-client-request-id"])
}

func TestBuildOpenAIResponsesRequestBodyAppliesCodexHeaderOverrideOnOpaqueReplay(t *testing.T) {
	rawBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"message","role":"user","content":"continue"}]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	for name, value := range map[string]string{
		"Originator":            "Codex Desktop",
		"X-Codex-Beta-Features": "remote_compaction_v2",
		"X-Codex-Turn-Metadata": `{"session_id":"session-123"}`,
		"X-Codex-Window-Id":     "window-123",
		"X-Client-Request-Id":   "request-123",
	} {
		c.Request.Header.Set(name, value)
	}
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		RequestHeaders:  map[string]string{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   114,
			ChannelType: constant.ChannelTypeCodex,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled:            true,
					Mode:               dto.HeaderProfileModeFixed,
					SelectedProfileIDs: []string{"codex-cli@latest"},
				},
			},
			ParamOverride: map[string]interface{}{
				"operations": []interface{}{
					map[string]interface{}{
						"mode": "pass_headers",
						"value": []interface{}{
							"X-Codex-Beta-Features",
							"X-Codex-Turn-Metadata",
							"X-Codex-Window-Id",
							"X-Client-Request-Id",
						},
					},
				},
			},
		},
	}
	for name, value := range c.Request.Header {
		if len(value) == 0 || strings.EqualFold(name, "Content-Type") {
			continue
		}
		info.RequestHeaders[name] = value[0]
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &responsesPassThroughProbeAdaptor{}, request, false)
	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, rawBody, bodyBytes)
	require.Equal(t, "remote_compaction_v2", info.RuntimePassedRequestHeaders["x-codex-beta-features"])
	require.Equal(t, `{"session_id":"session-123"}`, info.RuntimePassedRequestHeaders["x-codex-turn-metadata"])
	require.Equal(t, "window-123", info.RuntimePassedRequestHeaders["x-codex-window-id"])
	require.Equal(t, "request-123", info.RuntimePassedRequestHeaders["x-client-request-id"])

	headerOverride, err := relaychannel.ResolveHeaderOverride(info, c)
	require.NoError(t, err)
	require.Equal(t, "remote_compaction_v2", headerOverride["x-codex-beta-features"])
	require.Equal(t, "window-123", headerOverride["x-codex-window-id"])
}

func TestBuildOpenAIResponsesRequestBodyAppliesReasoningContextPolicyToOpaqueReplay(t *testing.T) {
	tests := []struct {
		name        string
		profile     dto.ResponsesUpstreamProfile
		wantContext bool
		wantAction  string
	}{
		{
			name:       "unsupported profile strips context only",
			profile:    dto.ResponsesUpstreamProfileSub2APIHTTP,
			wantAction: relaycommon.ResponsesReasoningContextActionStrippedUnsupported,
		},
		{
			name:        "supported profile preserves context",
			profile:     dto.ResponsesUpstreamProfileOfficialOpenAI,
			wantContext: true,
			wantAction:  relaycommon.ResponsesReasoningContextActionForwardedSupported,
		},
		{
			name:        "unknown profile preserves context",
			profile:     dto.ResponsesUpstreamProfileGenericOpenAI,
			wantContext: true,
			wantAction:  relaycommon.ResponsesReasoningContextActionForwardedUnverified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawBody := []byte(`{"model":"gpt-5.6-sol","reasoning":{"effort":"high","context":"all_turns","future_reasoning_option":{"enabled":true}},"input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"message","role":"user","content":"continue"}]}`)
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
					ChannelOtherSettings: dto.ChannelOtherSettings{
						ResponsesUpstreamProfile: tt.profile,
					},
				},
			}

			body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)
			require.Nil(t, apiErr)
			bodyBytes, err := io.ReadAll(body)
			require.NoError(t, err)

			var outbound dto.OpenAIResponsesRequest
			require.NoError(t, common.Unmarshal(bodyBytes, &outbound))
			require.Equal(t, tt.wantContext, outbound.Reasoning != nil && len(outbound.Reasoning.Context) > 0)
			require.Contains(t, string(bodyBytes), `"encrypted_content":"opaque-reasoning-state"`)
			require.Contains(t, string(bodyBytes), `"future_reasoning_option":{"enabled":true}`)
			require.Equal(t, tt.wantContext, converted.Reasoning != nil && len(converted.Reasoning.Context) > 0)
			require.Equal(t, tt.wantAction, common.GetContextKeyString(c, constant.ContextKeyResponsesReasoningContextAction))
		})
	}
}

func TestBuildOpenAIResponsesRequestBodyAppliesStreamOptionsCapabilityToOpaqueReplay(t *testing.T) {
	unsupported := false
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"reasoning","id":"rs_replay","encrypted_content":"opaque-reasoning-state"},
			{"type":"message","role":"user","content":"continue"}
		],
		"stream_options":{"include_usage":true,"include_obfuscation":true,"future_option":{"enabled":true}}
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
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelOtherSettings: dto.ChannelOtherSettings{
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
	require.Contains(t, string(bodyBytes), `"encrypted_content":"opaque-reasoning-state"`)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOptionsCapabilityStripped))
	require.NotNil(t, converted.StreamOptions)
	require.False(t, converted.StreamOptions.HasIncludeUsageField())
}

func TestBuildOpenAIResponsesRequestBodyNormalizesItemIDsOnRawPassThrough(t *testing.T) {
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"function_call","id":"clientfunctionid","call_id":"call_stable","name":"exec","arguments":"{}"},
			{"type":"function_call","id":"fc_valid","call_id":"call_valid","name":"exec","arguments":"{}"},
			{"type":"message","id":"msg_valid","role":"user","content":[{"type":"input_text","text":"continue"}]}
		],
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
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, common.Unmarshal(bodyBytes, &got))
	items, ok := got["input"].([]any)
	require.True(t, ok)
	require.Len(t, items, 3)
	firstItem := items[0].(map[string]any)
	_, hasInvalidID := firstItem["id"]
	require.False(t, hasInvalidID)
	require.Equal(t, "call_stable", firstItem["call_id"])
	require.Equal(t, "fc_valid", items[1].(map[string]any)["id"])
	require.Equal(t, "msg_valid", items[2].(map[string]any)["id"])
	require.Equal(t, map[string]any{"enabled": true}, got["future_top_level"])
	require.Equal(t, 1, common.GetContextKeyInt(c, constant.ContextKeyResponsesInputItemIDNormalizationRemovedCount))
	require.NotNil(t, converted.Input)
}

func TestBuildOpenAIResponsesRequestBodyPreservesOpaqueEncryptedReplayExceptModelMapping(t *testing.T) {
	unsupported := false
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"context_compaction","id":"clientgeneratedid","encrypted_content":"opaque-reasoning-state","future_state":{"version":2}},
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
			ChannelOtherSettings: dto.ChannelOtherSettings{
				ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileOfficialOpenAI,
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
	require.Contains(t, string(bodyBytes), `"model":"mapped-gpt-5.6-sol"`)
	var bodyJSON map[string]any
	require.NoError(t, common.Unmarshal(bodyBytes, &bodyJSON))
	items, ok := bodyJSON["input"].([]any)
	require.True(t, ok)
	require.Len(t, items, 2)
	firstItem := items[0].(map[string]any)
	require.Equal(t, "clientgeneratedid", firstItem["id"])
	require.Equal(t, "opaque-reasoning-state", firstItem["encrypted_content"])
	require.Equal(t, map[string]any{"version": float64(2)}, firstItem["future_state"])
	require.Equal(t, "keep-original-instructions", bodyJSON["instructions"])
	require.Equal(t, map[string]any{"enabled": true}, bodyJSON["future_top_level"])
	var convertedItems []map[string]any
	require.NoError(t, common.Unmarshal(converted.Input, &convertedItems))
	require.Len(t, convertedItems, 2)
	require.Equal(t, "clientgeneratedid", convertedItems[0]["id"])
	require.Zero(t, common.GetContextKeyInt(c, constant.ContextKeyResponsesInputItemIDNormalizationRemovedCount))
	require.NotContains(t, string(bodyBytes), `"include_usage"`)
	require.NotEqual(t, string(rawBody), string(bodyBytes))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesStreamOptionsCapabilityStripped))
}

func TestBuildOpenAIResponsesRequestBodyCapturesCodexFoldedPassThroughBody(t *testing.T) {
	rawBody := []byte(`{"model":"client-gpt-5.6-sol","input":[{"type":"message","role":"user","content":"continue"}],"stream":true,"future":true}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	request.Model = "upstream-gpt-5.6-sol"
	info := &relaycommon.RelayInfo{
		IsStream:        true,
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "client-gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeCodex,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
			ChannelOtherSettings: dto.ChannelOtherSettings{CodexFoldedSSEEnabled: true},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)
	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	wantBody := bytes.Replace(rawBody, []byte(`"client-gpt-5.6-sol"`), []byte(`"upstream-gpt-5.6-sol"`), 1)
	require.Equal(t, wantBody, bodyBytes)
	require.Equal(t, wantBody, info.CodexFoldedBaseRequestBody)
}

func TestBuildOpenAIResponsesRequestBodyCapturesCodexFoldedOpaqueReplayBody(t *testing.T) {
	rawBody := []byte(`{"model":"client-gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque"}],"stream":true,"future":true}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(rawBody, request))
	request.Model = "upstream-gpt-5.6-sol"
	info := &relaycommon.RelayInfo{
		IsStream:        true,
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "client-gpt-5.6-sol",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:          constant.ChannelTypeCodex,
			ChannelOtherSettings: dto.ChannelOtherSettings{CodexFoldedSSEEnabled: true},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)
	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	wantBody := bytes.Replace(rawBody, []byte(`"client-gpt-5.6-sol"`), []byte(`"upstream-gpt-5.6-sol"`), 1)
	require.Equal(t, wantBody, bodyBytes)
	require.Equal(t, wantBody, info.CodexFoldedBaseRequestBody)
}

func TestBuildOpenAIResponsesCompactRequestBodyPreservesOpaqueReplayExceptModelMapping(t *testing.T) {
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"reasoning","encrypted_content":"opaque-reasoning-state","future_state":{"version":3}},
			{"type":"message","role":"user","content":"continue"}
		],
		"reasoning":{"effort":"high","future_option":{"enabled":true}},
		"future_compact_option":{"mode":"native"}
	}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)
	common.SetContextKey(c, constant.ContextKeyResponsesCompactSummaryModel, "must-not-override")

	compactRequest := &dto.OpenAIResponsesCompactionRequest{}
	require.NoError(t, common.Unmarshal(rawBody, compactRequest))
	request := compactRequest.ToResponsesRequest()
	request.Model = "mapped-gpt-5.6-sol"
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponsesCompact,
		OriginModelName: compactRequest.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   206,
			ChannelType: constant.ChannelTypeCodex,
		},
	}

	applyResponsesCompactSummaryModelOverride(c, info, request, true)
	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, request.Input, converted.Input)
	require.Contains(t, string(bodyBytes), `"model":"mapped-gpt-5.6-sol"`)
	restoredBody := bytes.Replace(bodyBytes, []byte(`"mapped-gpt-5.6-sol"`), []byte(`"gpt-5.6-sol"`), 1)
	require.Equal(t, rawBody, restoredBody)
}

func TestExecuteOpenAIResponsesCompactRequestDoesNotRetryOpaqueReplay(t *testing.T) {
	tests := []struct {
		name     string
		rawBody  []byte
		isStream bool
	}{
		{
			name:    "non streaming",
			rawBody: []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"message","role":"user","content":"continue"}]}`),
		},
		{
			name:     "stream flag",
			rawBody:  []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"message","role":"user","content":"continue"}]}`),
			isStream: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(tt.rawBody))
			c.Request.Header.Set("Content-Type", "application/json")
			storage, err := common.CreateBodyStorage(tt.rawBody)
			require.NoError(t, err)
			t.Cleanup(func() { _ = storage.Close() })
			c.Set(common.KeyBodyStorage, storage)

			compactRequest := &dto.OpenAIResponsesCompactionRequest{}
			require.NoError(t, common.Unmarshal(tt.rawBody, compactRequest))
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponsesCompact,
				OriginModelName: compactRequest.Model,
				IsStream:        tt.isStream,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelId:   206,
					ChannelType: constant.ChannelTypeOpenAI,
				},
			}
			adaptor := &retryEncryptedContextAdaptor{
				responses: []*http.Response{
					missingEncryptedContentRetryResponse(http.StatusBadGateway),
					successResponsesHTTPResponse(),
				},
			}

			usage, requestErr := executeOpenAIResponsesRequest(c, info, adaptor, compactRequest.ToResponsesRequest(), false)

			require.Nil(t, usage)
			require.Error(t, requestErr)
			require.Equal(t, http.StatusBadRequest, requestErr.StatusCode)
			require.Equal(t, types.ErrorCode("missing_required_parameter"), requestErr.GetErrorCode())
			require.Len(t, adaptor.requestBodies, 1)
			require.Equal(t, tt.rawBody, adaptor.requestBodies[0])
			require.Len(t, adaptor.responses, 1)
			require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesEncryptedContextRetry))
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

	prepared, result, err := prepareResponsesRequestForChatCompatibility(info, request)

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

func TestBuildOpenAIResponsesRequestBodyPreservesOpaqueContextCompactionBeforePassThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"context_compaction","encrypted_content":"context-summary"},
			{"type":"context_compaction"},
			{"type":"compaction","encrypted_content":"native-summary"},
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
			{"type":"context_compaction","encrypted_content":"context-summary"},
			{"type":"context_compaction"},
			{"type":"compaction","encrypted_content":"native-summary"},
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

	body, converted, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)

	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, rawBody, bodyBytes)
	require.Equal(t, request.Input, converted.Input)
	require.Contains(t, string(bodyBytes), `"type":"context_compaction"`)
	require.Contains(t, string(converted.Input), `"type":"context_compaction"`)
	require.Contains(t, string(bodyBytes), `"encrypted_content":"context-summary"`)
	require.Contains(t, string(bodyBytes), `"encrypted_content":"native-summary"`)
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
				{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_22222222222222222222222222222222"},
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
		PreviousResponseID: "resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_22222222222222222222222222222222",
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
				{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:nffffffffffffffffffffffffffffffff:resp_newapi_synthcmp_nffffffffffffffffffffffffffffffff_22222222222222222222222222222222"},
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
		service.ErrResponsesChatCompatibilityPreviousIDUnsupported,
		service.ErrResponsesChatCompatibilityToolBridgeRequestInvalid,
		service.ErrProtocolConversionUnsupported,
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

func TestResponsesHelperRejectsUnsupportedOpaqueEncryptedReplayBeforeRelay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 168)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.6-sol")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{
		ResponsesUpstreamProfile: dto.ResponsesUpstreamProfileSub2APIHTTP,
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
	require.Equal(t, "unsupported", common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
	require.Equal(t, relaycommon.ResponsesEncryptedReasoningReplayActionRejectedUnsupported, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
}

func TestMarkResponsesEncryptedReasoningReplayDecision(t *testing.T) {
	for _, tc := range []struct {
		name       string
		capability dto.ResponsesEncryptedReasoningReplayCapability
		action     string
	}{
		{
			name:       "supported",
			capability: dto.ResponsesEncryptedReasoningReplayCapabilitySupported,
			action:     relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNative,
		},
		{
			name:       "unknown",
			capability: dto.ResponsesEncryptedReasoningReplayCapabilityUnknown,
			action:     relaycommon.ResponsesEncryptedReasoningReplayActionSelectedNativeUnverified,
		},
		{
			name:       "unsupported",
			capability: dto.ResponsesEncryptedReasoningReplayCapabilityUnsupported,
			action:     relaycommon.ResponsesEncryptedReasoningReplayActionRejectedUnsupported,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)

			markResponsesEncryptedReasoningReplayDecision(c, tc.capability)

			require.Equal(t, string(tc.capability), common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayCapability))
			require.Equal(t, tc.action, common.GetContextKeyString(c, constant.ContextKeyResponsesEncryptedReasoningReplayAction))
		})
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

func TestApplyResponsesCompactSummaryModelOverrideSkipsOpaqueEncryptedReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
	request := &dto.OpenAIResponsesRequest{
		Model: "mapped-gpt-5.6-sol",
		Input: common.RawMessage(`[
			{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},
			{"type":"message","role":"user","content":"continue"}
		]`),
	}

	applyResponsesCompactSummaryModelOverride(c, info, request, true)

	require.Equal(t, "mapped-gpt-5.6-sol", request.Model)
	require.Equal(t, "mapped-gpt-5.6-sol", info.UpstreamModelName)
}
