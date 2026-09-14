package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func foldedTestEvent(t *testing.T, value map[string]any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return "data: " + string(data) + "\n\n"
}

func foldedTestRound(t *testing.T, round int, reasoningTokens int, reasoningID string, encrypted bool, message string) string {
	t.Helper()
	reasoning := map[string]any{"id": reasoningID, "type": "reasoning", "summary": []any{}}
	doneReasoning := map[string]any{"id": reasoningID, "type": "reasoning", "summary": []any{}}
	if encrypted {
		doneReasoning["encrypted_content"] = "enc-" + reasoningID
	}
	messageItem := map[string]any{"id": "msg-" + reasoningID, "type": "message", "role": "assistant"}
	var b strings.Builder
	b.WriteString(foldedTestEvent(t, map[string]any{
		"type": "response.created", "sequence_number": round * 10,
		"response": map[string]any{"id": "resp-1", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "in_progress", "unknown_created": true},
	}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.in_progress", "sequence_number": round*10 + 1, "response": map[string]any{"id": "resp-1"}}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": reasoning, "unknown_event": "preserve"}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": doneReasoning}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_item.added", "output_index": 1, "item": messageItem}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_text.delta", "output_index": 1, "item_id": messageItem["id"], "delta": message}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_item.done", "output_index": 1, "item": map[string]any{"id": messageItem["id"], "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": message}}}}))
	b.WriteString(foldedTestEvent(t, map[string]any{
		"type": "response.completed", "response": map[string]any{
			"id": "resp-" + string(rune('1'+round)), "object": "response", "created_at": 2, "model": "gpt-5.5", "status": "completed",
			"usage": map[string]any{"input_tokens": 100, "output_tokens": reasoningTokens + 20, "total_tokens": reasoningTokens + 120, "output_tokens_details": map[string]any{"reasoning_tokens": reasoningTokens}},
		},
	}))
	return b.String()
}

func foldedTestIncompleteRound(t *testing.T, reasoningTokens int, reasoningID string, message string) string {
	t.Helper()
	reasoning := map[string]any{"id": reasoningID, "type": "reasoning", "summary": []any{}, "encrypted_content": "enc-" + reasoningID}
	messageItem := map[string]any{"id": "msg-" + reasoningID, "type": "message", "role": "assistant"}
	var b strings.Builder
	b.WriteString(foldedTestEvent(t, map[string]any{
		"type": "response.created", "sequence_number": 1,
		"response": map[string]any{"id": "resp-incomplete", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "in_progress"},
	}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": reasoningID, "type": "reasoning", "summary": []any{}}}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": reasoning}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_item.added", "output_index": 1, "item": messageItem}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_text.delta", "output_index": 1, "item_id": messageItem["id"], "delta": message}))
	b.WriteString(foldedTestEvent(t, map[string]any{"type": "response.output_item.done", "output_index": 1, "item": map[string]any{"id": messageItem["id"], "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": message}}}}))
	b.WriteString(foldedTestEvent(t, map[string]any{
		"type": "response.incomplete", "response": map[string]any{
			"id": "resp-incomplete", "object": "response", "created_at": 2, "model": "gpt-5.5", "status": "incomplete",
			"incomplete_details": map[string]any{"reason": "max_output_tokens"},
			"usage":              map[string]any{"input_tokens": 100, "output_tokens": reasoningTokens + 7, "total_tokens": reasoningTokens + 107, "output_tokens_details": map[string]any{"reasoning_tokens": reasoningTokens}},
		},
	}))
	return b.String()
}

func foldedTestContext(t *testing.T, info *relaycommon.RelayInfo) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	allowLocalCodexTestServer(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info.StreamStatus = relaycommon.NewStreamStatus()
	return c, recorder
}

func allowLocalCodexTestServer(t *testing.T) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	*setting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		AllowedPorts:           []string{"1-65535"},
		ApplyIPFilterForDomain: true,
	}
}

func TestRunCodexFoldedSSEContinuesAndFoldsRounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	requests := make([]map[string]any, 0, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests = append(requests, decoded)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Codex-Turn-State", "turn-"+string(rune('0'+len(requests))))
		w.WriteHeader(http.StatusOK)
		switch len(requests) {
		case 1:
			_, _ = io.WriteString(w, foldedTestRound(t, 1, 516, "rs-1", true, "discard-1"))
		case 2:
			_, _ = io.WriteString(w, foldedTestRound(t, 2, 1034, "rs-2", true, "discard-2"))
		default:
			_, _ = io.WriteString(w, foldedTestRound(t, 3, 404, "rs-3", true, "answer"))
		}
	}))
	defer upstream.Close()

	baseBody := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"hello"}],"stream":true,"previous_response_id":"resp-old","include":["foo"]}`)
	info := &relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelBaseUrl: upstream.URL, ApiKey: `{"access_token":"test","account_id":"acct"}`, ChannelOtherSettings: dto.ChannelOtherSettings{CodexFoldedSSEEnabled: true}}, CodexFoldedBaseRequestBody: baseBody}
	c, recorder := foldedTestContext(t, info)
	firstReq, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewReader(baseBody))
	require.NoError(t, err)
	firstReq.Header.Set("Content-Type", "application/json")
	firstResp, err := http.DefaultClient.Do(firstReq)
	require.NoError(t, err)
	usage, apiErr := runCodexFoldedSSE(c, info, firstResp, &Adaptor{})
	require.Nil(t, apiErr)
	require.Equal(t, 300, usage.InputTokens)
	require.Equal(t, 2014, usage.CompletionTokens)
	require.Len(t, requests, 3)
	require.NotContains(t, requests[1], "previous_response_id")
	input2, ok := requests[1]["input"].([]any)
	require.True(t, ok)
	require.Len(t, input2, 3)
	require.Equal(t, "commentary", input2[2].(map[string]any)["phase"])
	require.Equal(t, "reasoning.encrypted_content", requests[1]["include"].([]any)[1])
	require.Equal(t, "turn-1", recorder.Result().Header.Get("X-Codex-Turn-State"))
	require.Equal(t, "turn-3", recorder.Result().Trailer.Get("X-Codex-Turn-State"))
	require.Contains(t, recorder.Body.String(), "\"delta\":\"answer\"")
	require.NotContains(t, recorder.Body.String(), "discard-1")
	require.NotContains(t, recorder.Body.String(), "discard-2")
	require.Contains(t, recorder.Body.String(), "proxy_billed_usage")
}

func TestRunCodexFoldedSSEEmitsFinalTurnStateTrailerOverHTTP(t *testing.T) {
	allowLocalCodexTestServer(t)
	service.InitHttpClient()
	var requests int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Codex-Turn-State", "turn-"+string(rune('0'+requests)))
		w.WriteHeader(http.StatusOK)
		if requests == 1 {
			_, _ = io.WriteString(w, foldedTestRound(t, 1, 516, "rs-http-1", true, "discard"))
			return
		}
		_, _ = io.WriteString(w, foldedTestRound(t, 2, 404, "rs-http-2", true, "answer"))
	}))
	defer upstream.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"hello"}],"stream":true}`)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		info := foldedTestInfo(upstream.URL, body)
		firstRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstream.URL, bytes.NewReader(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		firstRequest.Header.Set("Content-Type", "application/json")
		firstResponse, err := http.DefaultClient.Do(firstRequest)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		_, apiErr := runCodexFoldedSSE(c, info, firstResponse, &Adaptor{})
		if apiErr != nil {
			http.Error(w, apiErr.Error(), http.StatusBadGateway)
		}
	}))
	defer gateway.Close()

	request, err := http.NewRequest(http.MethodPost, gateway.URL, bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "turn-1", response.Header.Get("X-Codex-Turn-State"))
	require.Equal(t, "turn-2", response.Trailer.Get("X-Codex-Turn-State"))
}

func TestCodexFoldedBoundariesAndEncryptedContentValidation(t *testing.T) {
	for _, test := range []struct {
		name     string
		tokens   int
		wantTier int
		wantOK   bool
	}{
		{name: "below first boundary", tokens: 515},
		{name: "first boundary", tokens: 516, wantTier: 1, wantOK: true},
		{name: "second boundary", tokens: 1034, wantTier: 2, wantOK: true},
		{name: "third boundary", tokens: 1552, wantTier: 3, wantOK: true},
		{name: "above boundary", tokens: 1553},
	} {
		t.Run(test.name, func(t *testing.T) {
			tier, ok := codexFoldedTier(test.tokens)
			require.Equal(t, test.wantOK, ok)
			require.Equal(t, test.wantTier, tier)
		})
	}
	for _, test := range []struct {
		name string
		item map[string]common.RawMessage
		want bool
	}{
		{name: "non-empty string", item: map[string]common.RawMessage{"encrypted_content": common.RawMessage(`"opaque"`)}, want: true},
		{name: "whitespace string", item: map[string]common.RawMessage{"encrypted_content": common.RawMessage(`"  "`)}},
		{name: "object is invalid", item: map[string]common.RawMessage{"encrypted_content": common.RawMessage(`{"value":"opaque"}`)}},
		{name: "null is invalid", item: map[string]common.RawMessage{"encrypted_content": common.RawMessage(`null`)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, codexFoldedHasEncryptedContent(test.item))
		})
	}
}

func TestCodexFoldedAppendSSEDataBoundsWholeEvent(t *testing.T) {
	var data strings.Builder
	require.NoError(t, codexFoldedAppendSSEData(&data, "first", 12))
	require.Equal(t, "first", data.String())
	require.NoError(t, codexFoldedAppendSSEData(&data, "second", 12))
	require.Equal(t, "first\nsecond", data.String())

	var oversized strings.Builder
	require.NoError(t, codexFoldedAppendSSEData(&oversized, "123456", 10))
	err := codexFoldedAppendSSEData(&oversized, "78901", 10)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds 10 bytes")
	require.Equal(t, "123456", oversized.String())
}

func TestCodexFoldedTerminalMetadataKeepsStoppedReason(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, foldedTestRound(t, 1, 516, "rs-no-encrypted", false, "answer"))
	}))
	defer server.Close()
	baseBody := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := &relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelBaseUrl: server.URL, ApiKey: `{"access_token":"test","account_id":"acct"}`, ChannelOtherSettings: dto.ChannelOtherSettings{CodexFoldedSSEEnabled: true}}, CodexFoldedBaseRequestBody: baseBody}
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(baseBody))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	var terminal map[string]any
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") || !strings.Contains(line, "response.completed") {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) == nil {
			terminal = event
		}
	}
	require.NotNil(t, terminal)
	require.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	responseObject := terminal["response"].(map[string]any)
	metadata := responseObject["metadata"].(map[string]any)
	require.Equal(t, "no_encrypted_content", metadata[codexFoldedStoppedReasonKey])
}

func TestCodexFoldedIncompleteTerminalRemainsIncomplete(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, foldedTestIncompleteRound(t, 516, "rs-incomplete", "partial"))
	}))
	defer server.Close()
	baseBody := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := &relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelBaseUrl: server.URL, ApiKey: `{"access_token":"test","account_id":"acct"}`, ChannelOtherSettings: dto.ChannelOtherSettings{CodexFoldedSSEEnabled: true}}, CodexFoldedBaseRequestBody: baseBody}
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(baseBody))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	body := recorder.Body.String()
	require.Contains(t, body, `"type":"response.output_text.delta"`)
	require.Contains(t, body, `"delta":"partial"`)
	require.Contains(t, body, `"type":"response.incomplete"`)
	require.Contains(t, body, `"status":"incomplete"`)
	require.Contains(t, body, `"reason":"max_output_tokens"`)
	require.NotContains(t, body, `"type":"response.completed"`)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
}

func TestCodexFoldedBuildContinuationBodyPreservesUnknownFields(t *testing.T) {
	body, err := codexFoldedBuildContinuationBody([]byte(`{"model":"gpt-5.5","stream":false,"previous_response_id":"old","future":{"keep":true},"input":"hello"}`), nil)
	require.NoError(t, err)
	var decodedStringInput map[string]any
	require.NoError(t, json.Unmarshal(body, &decodedStringInput))
	require.NotContains(t, decodedStringInput, "previous_response_id")
	require.Equal(t, true, decodedStringInput["stream"])
	stringInput, ok := decodedStringInput["input"].([]any)
	require.True(t, ok)
	require.Len(t, stringInput, 1)
	require.Equal(t, "message", stringInput[0].(map[string]any)["type"])
	require.Equal(t, "user", stringInput[0].(map[string]any)["role"])
	require.Equal(t, "hello", stringInput[0].(map[string]any)["content"])
	require.Equal(t, []any{"reasoning.encrypted_content"}, decodedStringInput["include"])

	body, err = codexFoldedBuildContinuationBody([]byte(`{"model":"gpt-5.5","stream":false,"previous_response_id":"old","include":"usage","input":[]}`), nil)
	require.Error(t, err)
	require.Nil(t, body)

	body, err = codexFoldedBuildContinuationBody([]byte(`{"model":"gpt-5.5","stream":false,"previous_response_id":"old","future":{"keep":true},"input":[{"type":"message","role":"user"}]}`), []map[string]common.RawMessage{{"type": common.RawMessage(`"message"`), "phase": common.RawMessage(`"commentary"`)}})
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.NotContains(t, decoded, "previous_response_id")
	require.Equal(t, true, decoded["future"].(map[string]any)["keep"])
	require.Equal(t, true, decoded["stream"])
}

func TestCodexFoldedTerminalFailureIsStreamed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":     "response.failed",
			"response": map[string]any{"status": "failed", "error": map[string]any{"code": "bad_response", "message": "upstream failed"}},
		}))
	}))
	defer server.Close()
	service.InitHttpClient()
	baseBody := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := &relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelBaseUrl: server.URL, ApiKey: `{"access_token":"test","account_id":"acct"}`, ChannelOtherSettings: dto.ChannelOtherSettings{CodexFoldedSSEEnabled: true}}, CodexFoldedBaseRequestBody: baseBody}
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(baseBody))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	usage, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), "response.failed")
	require.NotContains(t, recorder.Body.String(), "response.incomplete")
}

func TestRunCodexFoldedSSENon2xxWithoutFailureTerminalReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type": "response.completed",
			"response": map[string]any{
				"status": "completed",
				"output": []any{},
			},
		}))
	}))
	defer server.Close()
	service.InitHttpClient()
	baseBody := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, baseBody)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(baseBody))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)

	usage, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	require.Contains(t, apiErr.Error(), "bad response status code 502")
	require.Empty(t, recorder.Body.String())
}

func TestCodexFoldedReadSSEStopsWhenCanceled(t *testing.T) {
	reader, writer := io.Pipe()
	done := make(chan struct{})
	results := codexFoldedReadSSE(reader, done)
	close(done)
	_ = reader.Close()
	_ = writer.Close()
	for range results {
	}
}

func foldedTestInfo(baseURL string, body []byte) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:  true,
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelBaseUrl: baseURL,
			ApiKey:         `{"access_token":"test","account_id":"acct"}`,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				CodexFoldedSSEEnabled: true,
			},
		},
		CodexFoldedBaseRequestBody: body,
	}
}

func foldedTestResponse(t *testing.T, response map[string]any, terminalType string, extra map[string]any) string {
	t.Helper()
	event := map[string]any{"type": terminalType, "response": response}
	for key, value := range extra {
		event[key] = value
	}
	return foldedTestEvent(t, event)
}

func TestRunCodexFoldedSSEDoesNotContinueAfterFirstRoundWithZeroReasoning(t *testing.T) {
	service.InitHttpClient()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, foldedTestRound(t, requests, 0, "rs-zero", true, "answer"))
	}))
	defer server.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, body)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	usage, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 1, requests)
	require.Contains(t, recorder.Body.String(), `"delta":"answer"`)
	require.NotContains(t, recorder.Body.String(), `"proxy_stopped_reason":"max_continue"`)
}

func TestRunCodexFoldedSSEStopsAfterThreeStalledContinuations(t *testing.T) {
	service.InitHttpClient()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		reasoning := 0
		encrypted := true
		if requests == 1 {
			reasoning = 516
		}
		_, _ = io.WriteString(w, foldedTestRound(t, requests, reasoning, fmt.Sprintf("rs-%d", requests), encrypted, fmt.Sprintf("answer-%d", requests)))
	}))
	defer server.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, body)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	require.Equal(t, codexFoldedMaxContinuation+1, requests)
	require.Contains(t, recorder.Body.String(), `"delta":"answer-4"`)
	require.Contains(t, recorder.Body.String(), `"proxy_stopped_reason":"max_continue"`)
	require.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
}

func TestRunCodexFoldedSSEContinuationNonSSEErrorProducesIncomplete(t *testing.T) {
	service.InitHttpClient()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, foldedTestRound(t, 1, 516, "rs-first", true, "discard"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, `{"error":{"type":"upstream_error","message":"continuation failed","code":"bad_gateway"}}`)
	}))
	defer server.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, body)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	require.Equal(t, 2, requests)
	require.Contains(t, recorder.Body.String(), `"type":"response.incomplete"`)
	require.Contains(t, recorder.Body.String(), `"reason":"upstream_error"`)
	require.NotContains(t, recorder.Body.String(), "continuation failed")
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)
}

func TestRunCodexFoldedSSETimeoutProducesIncomplete(t *testing.T) {
	service.InitHttpClient()
	previousTimeout := codexFoldedReadTimeout
	codexFoldedReadTimeout = 20 * time.Millisecond
	defer func() { codexFoldedReadTimeout = previousTimeout }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":     "response.created",
			"response": map[string]any{"id": "resp-timeout", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "in_progress"},
		}))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, body)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), `"type":"response.incomplete"`)
	require.Contains(t, recorder.Body.String(), `"reason":"upstream_timeout"`)
	require.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
}

func TestRunCodexFoldedSSEResetsTimeoutAfterEachEvent(t *testing.T) {
	service.InitHttpClient()
	previousTimeout := codexFoldedReadTimeout
	codexFoldedReadTimeout = 100 * time.Millisecond
	defer func() { codexFoldedReadTimeout = previousTimeout }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":     "response.created",
			"response": map[string]any{"id": "resp-idle-reset", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "in_progress"},
		}))
		flusher.Flush()
		time.Sleep(70 * time.Millisecond)
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{"type": "response.in_progress"}))
		flusher.Flush()
		time.Sleep(70 * time.Millisecond)
		_, _ = io.WriteString(w, foldedTestResponse(t, map[string]any{
			"id": "resp-idle-reset", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "completed",
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
		}, "response.completed", nil))
		flusher.Flush()
	}))
	defer server.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, body)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), `"type":"response.completed"`)
	require.NotContains(t, recorder.Body.String(), `"reason":"upstream_timeout"`)
}

func TestRunCodexFoldedSSEPreservesTerminalIdentityUnknownFieldsAndOutputItems(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type": "response.created", "response": map[string]any{
				"id": "resp-original", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "in_progress",
			},
		}))
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type": "response.output_item.added", "output_index": 0,
			"item": map[string]any{"id": "call-1", "type": "function_call", "call_id": "call-1", "name": "exec", "arguments": "{}"},
		}))
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type": "response.output_item.done", "output_index": 0,
			"item": map[string]any{"id": "call-1", "type": "function_call", "call_id": "call-1", "name": "exec", "arguments": "{}"},
		}))
		_, _ = io.WriteString(w, foldedTestResponse(t, map[string]any{
			"id": "resp-terminal", "object": "response", "created_at": 2, "model": "gpt-5.5", "status": "completed",
			"future_response": map[string]any{"keep": true},
			"usage":           map[string]any{"input_tokens": 100, "output_tokens": 4, "total_tokens": 104, "output_tokens_details": map[string]any{"reasoning_tokens": 0}},
		}, "response.completed", map[string]any{"future_event": "keep"}))
	}))
	defer server.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, body)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	output := recorder.Body.String()
	require.Contains(t, output, `"id":"resp-original"`)
	require.NotContains(t, output, `"id":"resp-terminal"`)
	require.Contains(t, output, `"future_response":{"keep":true}`)
	require.Contains(t, output, `"future_event":"keep"`)
	require.Contains(t, output, `"type":"function_call"`)
}

func TestCodexFoldedUsageKeepsCacheDetailsSeparateFromBilledUsage(t *testing.T) {
	state := &codexFoldedState{
		firstUsage: &dto.Usage{
			InputTokens:         100,
			PromptTokens:        100,
			PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 7},
		},
		foldedReasoning:   516,
		foldedCleanOutput: 20,
	}
	response := map[string]common.RawMessage{
		"usage": common.RawMessage(`{"output_tokens_details":{"text_tokens":9,"reasoning_tokens":1}}`),
	}
	require.NoError(t, codexFoldedBuildUsage(response, state, true))
	var usage map[string]any
	require.NoError(t, json.Unmarshal(response["usage"], &usage))
	require.Equal(t, float64(100), usage["input_tokens"])
	require.Equal(t, float64(536), usage["output_tokens"])
	require.Equal(t, float64(636), usage["total_tokens"])
	require.Equal(t, float64(7), usage["input_tokens_details"].(map[string]any)["cached_tokens"])
	require.Equal(t, float64(516), usage["output_tokens_details"].(map[string]any)["reasoning_tokens"])
	require.Equal(t, float64(9), usage["output_tokens_details"].(map[string]any)["text_tokens"])

	var responsesShaped dto.Usage
	responsesShaped.InputTokens = 120
	responsesShaped.OutputTokens = 516
	responsesShaped.TotalTokens = 636
	responsesShaped.InputTokensDetails = &dto.InputTokenDetails{CachedTokens: 12, TextTokens: 108}
	responsesShaped.OutputTokensDetails = &dto.OutputTokenDetails{ReasoningTokens: 516, TextTokens: 0, AudioTokens: 2, ImageTokens: 1}
	responsesShaped.CompletionTokenDetails = dto.OutputTokenDetails{ReasoningTokens: 516, TextTokens: 99}
	var normalized dto.Usage
	codexFoldedMergeUsage(&normalized, &responsesShaped)
	require.Equal(t, 12, normalized.PromptTokensDetails.CachedTokens)
	require.Equal(t, 108, normalized.PromptTokensDetails.TextTokens)
	require.Equal(t, 516, normalized.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, 0, normalized.CompletionTokenDetails.TextTokens)
	require.Equal(t, 2, normalized.CompletionTokenDetails.AudioTokens)
	require.Equal(t, 1, normalized.CompletionTokenDetails.ImageTokens)

	var billed dto.Usage
	codexFoldedMergeUsage(&billed, &dto.Usage{InputTokens: 100, OutputTokens: 516, PromptTokens: 100, CompletionTokens: 516, TotalTokens: 616, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 7}, OutputTokensDetails: &dto.OutputTokenDetails{ReasoningTokens: 516, TextTokens: 11, AudioTokens: 2, ImageTokens: 1}})
	codexFoldedMergeUsage(&billed, &dto.Usage{InputTokens: 90, OutputTokens: 20, PromptTokens: 90, CompletionTokens: 20, TotalTokens: 110, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 3}, OutputTokensDetails: &dto.OutputTokenDetails{ReasoningTokens: 0, TextTokens: 4, AudioTokens: 5, ImageTokens: 6}})
	require.Equal(t, 190, billed.InputTokens)
	require.Equal(t, 10, billed.PromptTokensDetails.CachedTokens)
	require.Equal(t, 516, billed.OutputTokensDetails.ReasoningTokens)
	require.Equal(t, 15, billed.OutputTokensDetails.TextTokens)
	require.Equal(t, 7, billed.OutputTokensDetails.AudioTokens)
	require.Equal(t, 7, billed.OutputTokensDetails.ImageTokens)
	require.Equal(t, 516, billed.CompletionTokenDetails.ReasoningTokens)
	require.Equal(t, 15, billed.CompletionTokenDetails.TextTokens)
	require.Equal(t, 7, billed.CompletionTokenDetails.AudioTokens)
	require.Equal(t, 7, billed.CompletionTokenDetails.ImageTokens)
}

func TestCodexFoldedReasoningTokensFallsBackToCompletionDetails(t *testing.T) {
	usage := &dto.Usage{
		CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 516},
		OutputTokensDetails:    &dto.OutputTokenDetails{TextTokens: 20},
	}
	require.Equal(t, 516, mustCodexFoldedReasoningTokens(t, usage))
}

func TestCodexFoldedUnknownEventWithoutOutputIndexIsNotReclassified(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":     "response.created",
			"response": map[string]any{"id": "resp-unknown-index", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "in_progress"},
		}))
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":         "response.output_item.added",
			"output_index": 0,
			"item":         map[string]any{"id": "reasoning-1", "type": "reasoning", "summary": []any{}},
		}))
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":       "response.reasoning.unknown",
			"future_key": "preserve",
		}))
		_, _ = io.WriteString(w, foldedTestResponse(t, map[string]any{
			"id": "resp-unknown-index", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "completed",
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
		}, "response.completed", nil))
	}))
	defer server.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, body)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), `"type":"response.reasoning.unknown"`)
	require.Contains(t, recorder.Body.String(), `"future_key":"preserve"`)
	require.NotContains(t, recorder.Body.String(), `"output_index":0,"future_key"`)
}

func TestCodexFoldedKnownOutputItemWithoutOutputIndexIsNotReclassified(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":     "response.created",
			"response": map[string]any{"id": "resp-known-missing-index", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "in_progress"},
		}))
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":         "response.output_item.added",
			"item":         map[string]any{"id": "reasoning-missing-index", "type": "reasoning", "summary": []any{}},
			"future_added": true,
		}))
		_, _ = io.WriteString(w, foldedTestEvent(t, map[string]any{
			"type":        "response.output_item.done",
			"item":        map[string]any{"id": "reasoning-missing-index", "type": "reasoning", "summary": []any{}, "encrypted_content": "opaque"},
			"future_done": true,
		}))
		_, _ = io.WriteString(w, foldedTestResponse(t, map[string]any{
			"id": "resp-known-missing-index", "object": "response", "created_at": 1, "model": "gpt-5.5", "status": "completed",
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
		}, "response.completed", nil))
	}))
	defer server.Close()

	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user"}],"stream":true}`)
	info := foldedTestInfo(server.URL, body)
	c, recorder := foldedTestContext(t, info)
	request, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	_, apiErr := runCodexFoldedSSE(c, info, response, &Adaptor{})
	require.Nil(t, apiErr)
	output := recorder.Body.String()
	require.Contains(t, output, `"future_added":true`)
	require.Contains(t, output, `"future_done":true`)
	require.NotContains(t, output, `"output_index":0,"future_added"`)
	require.NotContains(t, output, `"output_index":0,"future_done"`)
}

func mustCodexFoldedReasoningTokens(t *testing.T, usage *dto.Usage) int {
	t.Helper()
	value, ok := codexFoldedReasoningTokens(usage)
	require.True(t, ok)
	return value
}
