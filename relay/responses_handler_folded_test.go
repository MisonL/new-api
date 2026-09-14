package relay

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	codexchannel "github.com/QuantumNous/new-api/relay/channel/codex"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldParseCodexFoldedSSEStatusRequiresStrictScope(t *testing.T) {
	settings := dto.ChannelOtherSettings{CodexFoldedSSEEnabled: true}
	resp := &http.Response{StatusCode: http.StatusBadGateway, Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}}}
	require.True(t, shouldParseCodexFoldedSSEStatus(&relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelOtherSettings: settings}}, resp))
	require.False(t, shouldParseCodexFoldedSSEStatus(&relaycommon.RelayInfo{IsStream: false, RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelOtherSettings: settings}}, resp))
	require.False(t, shouldParseCodexFoldedSSEStatus(&relaycommon.RelayInfo{IsStream: true, RelayMode: relayconstant.RelayModeResponsesCompact, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex, ChannelOtherSettings: settings}}, resp))
}

func foldedRelayTestEvent(t *testing.T, event map[string]any) string {
	t.Helper()
	body, err := json.Marshal(event)
	require.NoError(t, err)
	return "data: " + string(body) + "\n\n"
}

func foldedRelayTestRound(t *testing.T, reasoning bool) string {
	t.Helper()
	created := map[string]any{
		"type":     "response.created",
		"response": map[string]any{"id": "resp-1", "object": "response", "created_at": 1, "model": "upstream-model", "status": "in_progress"},
	}
	completed := map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "resp-1", "object": "response", "created_at": 1, "model": "upstream-model", "status": "completed",
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 520, "total_tokens": 530, "output_tokens_details": map[string]any{"reasoning_tokens": 516}},
		},
	}
	if reasoning {
		reasoningItem := map[string]any{"id": "reasoning-1", "type": "reasoning", "summary": []any{}, "encrypted_content": "encrypted-1"}
		return foldedRelayTestEvent(t, created) +
			foldedRelayTestEvent(t, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "reasoning-1", "type": "reasoning", "summary": []any{}}}) +
			foldedRelayTestEvent(t, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": reasoningItem}) +
			foldedRelayTestEvent(t, completed)
	}
	messageItem := map[string]any{"id": "message-1", "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "answer"}}}
	completed["response"].(map[string]any)["usage"].(map[string]any)["output_tokens_details"].(map[string]any)["reasoning_tokens"] = 404
	completed["response"].(map[string]any)["usage"].(map[string]any)["output_tokens"] = 408
	completed["response"].(map[string]any)["usage"].(map[string]any)["total_tokens"] = 418
	return foldedRelayTestEvent(t, created) +
		foldedRelayTestEvent(t, map[string]any{"type": "response.output_item.added", "output_index": 0, "item": messageItem}) +
		foldedRelayTestEvent(t, map[string]any{"type": "response.output_item.done", "output_index": 0, "item": messageItem}) +
		foldedRelayTestEvent(t, completed)
}

func TestBuildCodexFoldedPassThroughBodyFeedsContinuation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	rawBody := []byte(`{"model":"client-model","input":[{"type":"message","role":"user","content":"hello"}],"stream":true,"future":{"keep":true}}`)
	var received [][]byte
	var handlerErr error
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			handlerErr = err
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received = append(received, body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Codex-Turn-State", "turn-"+string(rune('0'+len(received))))
		if len(received) == 1 {
			_, _ = io.WriteString(w, foldedRelayTestRound(t, true))
			return
		}
		_, _ = io.WriteString(w, foldedRelayTestRound(t, false))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawBody))
	storage, err := common.CreateBodyStorage(rawBody)
	require.NoError(t, err)
	defer storage.Close()
	c.Set(common.KeyBodyStorage, storage)

	request := &dto.OpenAIResponsesRequest{Model: "upstream-model", Input: common.RawMessage(`[{"type":"message","role":"user","content":"hello"}]`)}
	info := &relaycommon.RelayInfo{
		IsStream:        true,
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "client-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelBaseUrl: upstream.URL,
			ApiKey:         `{"access_token":"test","account_id":"acct"}`,
			ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
			ChannelOtherSettings: dto.ChannelOtherSettings{
				CodexFoldedSSEEnabled: true,
			},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)
	require.Nil(t, apiErr)
	firstRequest, err := http.NewRequest(http.MethodPost, upstream.URL+"/backend-api/codex/responses", body)
	require.NoError(t, err)
	firstResponse, err := http.DefaultClient.Do(firstRequest)
	require.NoError(t, err)
	_, apiErr = (&codexchannel.Adaptor{}).DoResponse(c, firstResponse, info)
	require.Nil(t, apiErr)

	require.NoError(t, handlerErr)
	require.Len(t, received, 2)
	var first, continuation map[string]any
	require.NoError(t, json.Unmarshal(received[0], &first))
	require.NoError(t, json.Unmarshal(received[1], &continuation))
	require.Equal(t, "upstream-model", first["model"])
	require.Equal(t, true, first["future"].(map[string]any)["keep"])
	require.Equal(t, "upstream-model", continuation["model"])
	require.Equal(t, true, continuation["stream"])
	require.NotContains(t, continuation, "previous_response_id")
	require.Equal(t, true, continuation["future"].(map[string]any)["keep"])
	input := continuation["input"].([]any)
	require.Len(t, input, 3)
	require.Equal(t, "reasoning", input[1].(map[string]any)["type"])
	require.Equal(t, "message", input[2].(map[string]any)["type"])
	require.Contains(t, recorder.Body.String(), "answer")
}
