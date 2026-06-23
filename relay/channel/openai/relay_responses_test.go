package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

func TestOaiResponsesHandlerMarksCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_compact_v2",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"compaction","encrypted_content":"opaque"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 15, usage.TotalTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
	require.JSONEq(t, body, recorder.Body.String())
}

func TestOaiResponsesStreamHandlerMarksContextCompactionItemDone(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","item":{"type":"context_compaction","encrypted_content":"opaque"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_context_compact","object":"response","created_at":1710000000,"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
}

func TestOaiResponsesStreamHandlerReturnsRetryableErrorBeforeFirstWrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, err.GetErrorCode())
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Contains(t, err.Error(), "stream disconnected before completion")
	require.False(t, c.Writer.Written())
	require.Empty(t, recorder.Body.String())
}

func TestRetryableUpstreamStreamInterruptedAllowsRetryAfterKeepaliveOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, writeErr := c.Writer.Write([]byte(": PING\n\n"))
	require.NoError(t, writeErr)
	require.True(t, c.Writer.Written())

	info := &relaycommon.RelayInfo{
		StreamStatus: relaycommon.NewStreamStatus(),
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, io.ErrUnexpectedEOF)

	err := retryableUpstreamStreamInterruptedError(c, info)

	require.NotNil(t, err)
	require.Equal(t, types.ErrorCodeUpstreamTransportInterrupted, err.GetErrorCode())
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
}

func TestRetryableUpstreamStreamInterruptedDoesNotRetryAfterUpstreamChunk(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		ReceivedResponseCount: 1,
		StreamStatus:          relaycommon.NewStreamStatus(),
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, io.ErrUnexpectedEOF)

	err := retryableUpstreamStreamInterruptedError(c, info)

	require.Nil(t, err)
	require.False(t, c.Writer.Written())
}

func TestOaiResponsesStreamHandlerDoesNotRecordAffinityBeforeCompleted(t *testing.T) {
	service.ClearChannelAffinityCacheAll()
	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	encryptedContent := fmt.Sprintf("gAAAA-stream-interrupt-%d", time.Now().UnixNano())
	body := strings.Join([]string{
		fmt.Sprintf(`data: {"type":"response.output_item.done","item":{"type":"reasoning","encrypted_content":"%s"}}`, encryptedContent),
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         203,
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, relaycommon.StreamEndReasonUpstreamInterrupted, info.StreamStatus.EndReason)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"gpt-5.5",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	channelID, found := service.GetPreferredChannelByAffinity(hitCtx, "gpt-5.5", "default")
	require.False(t, found)
	require.Equal(t, 0, channelID)
}

func TestOaiResponsesStreamHandlerRecordsAffinityAfterCompleted(t *testing.T) {
	service.ClearChannelAffinityCacheAll()
	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	encryptedContent := fmt.Sprintf("gAAAA-stream-complete-%d", time.Now().UnixNano())
	body := strings.Join([]string{
		fmt.Sprintf(`data: {"type":"response.completed","response":{"id":"resp_affinity","object":"response","created_at":1710000000,"model":"gpt-5.5","output":[{"type":"reasoning","encrypted_content":"%s"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`, encryptedContent),
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         203,
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"gpt-5.5",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	channelID, found := service.GetPreferredChannelByAffinity(hitCtx, "gpt-5.5", "default")
	require.True(t, found)
	require.Equal(t, 203, channelID)
}

func TestOaiResponsesHandlerSkipsAffinityAfterEncryptedContextRetry(t *testing.T) {
	service.ClearChannelAffinityCacheAll()
	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyResponsesEncryptedContextRetry, true)
	encryptedContent := fmt.Sprintf("gAAAA-encrypted-retry-%d", time.Now().UnixNano())
	body := fmt.Sprintf(`{
		"id":"resp_encrypted_retry",
		"object":"response",
		"created_at":1710000000,
		"model":"gpt-5.5",
		"output":[{"type":"reasoning","encrypted_content":"%s"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`, encryptedContent)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 203,
		},
	}

	usage, err := OaiResponsesHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"gpt-5.5",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	channelID, found := service.GetPreferredChannelByAffinity(hitCtx, "gpt-5.5", "default")
	require.False(t, found)
	require.Equal(t, 0, channelID)
}

func TestOaiResponsesStreamHandlerSkipsAffinityAfterEncryptedContextRetry(t *testing.T) {
	service.ClearChannelAffinityCacheAll()
	t.Cleanup(func() {
		service.ClearChannelAffinityCacheAll()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyResponsesEncryptedContextRetry, true)
	encryptedContent := fmt.Sprintf("gAAAA-stream-encrypted-retry-%d", time.Now().UnixNano())
	body := strings.Join([]string{
		fmt.Sprintf(`data: {"type":"response.completed","response":{"id":"resp_stream_encrypted_retry","object":"response","created_at":1710000000,"model":"gpt-5.5","output":[{"type":"reasoning","encrypted_content":"%s"}],"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}}}`, encryptedContent),
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         203,
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"gpt-5.5",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	channelID, found := service.GetPreferredChannelByAffinity(hitCtx, "gpt-5.5", "default")
	require.False(t, found)
	require.Equal(t, 0, channelID)
}

func TestOaiResponsesStreamHandlerTreatsCompletedWithoutDoneAsNormalEnd(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		`data: {"type":"response.completed","response":{"id":"resp_partial","object":"response","created_at":1710000000,"usage":{"input_tokens":8,"output_tokens":1,"total_tokens":9}}}`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gpt-5.5",
		},
		StreamStatus: relaycommon.NewStreamStatus(),
		DisablePing:  true,
	}

	usage, err := OaiResponsesStreamHandler(c, info, resp)

	require.Nil(t, err)
	require.NotNil(t, usage)
	require.Equal(t, 8, usage.PromptTokens)
	require.Equal(t, 1, usage.CompletionTokens)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.True(t, info.StreamStatus.IsNormalEnd())
	require.Nil(t, info.StreamStatus.EndError)
	require.True(t, c.Writer.Written())
	require.Contains(t, recorder.Body.String(), "response.output_text.delta")
}

func TestOaiResponsesHandlerAllowsNonStringCompactionEncryptedContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_compact_v2",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"compaction","encrypted_content":{"opaque":true}}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
	require.JSONEq(t, body, recorder.Body.String())
}

func TestOaiResponsesHandlerMarksContextCompactionOutput(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	body := `{
		"id":"resp_context_compact",
		"object":"response",
		"created_at":1710000000,
		"output":[{"type":"context_compaction","encrypted_content":"opaque"}],
		"usage":{"input_tokens":12,"output_tokens":3,"total_tokens":15}
	}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	usage, err := OaiResponsesHandler(c, nil, resp)

	require.Nil(t, err)
	require.Equal(t, 12, usage.PromptTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactionOutput))
	require.JSONEq(t, body, recorder.Body.String())
}
