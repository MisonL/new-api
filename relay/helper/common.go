package helper

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// FlushWriter flushes the current HTTP response writer when streaming data.
func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}

	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	flusher.Flush()
	return nil
}

// ResetEventStreamHeaders undoes SetEventStreamHeaders for a request that never
// produced an SSE frame. doRequestWithClient sets the SSE headers as soon as a
// stream is attempted, so an upstream that fails before the first frame leaves
// the response claiming text/event-stream while the error handler writes a JSON
// body. gin only fills Content-Type when the header is empty, so it has to be
// removed explicitly.
func ResetEventStreamHeaders(c *gin.Context) {
	if c == nil {
		return
	}
	header := c.Writer.Header()
	header.Del("Content-Type")
	header.Del("Cache-Control")
	header.Del("Connection")
	header.Del("Transfer-Encoding")
	header.Del("X-Accel-Buffering")
	// Clear the marker so a later SetEventStreamHeaders call, which a real SSE
	// error frame still needs, is not suppressed.
	c.Set("event_stream_headers_set", false)
}

// SetEventStreamHeaders writes SSE headers once for the current response.
func SetEventStreamHeaders(c *gin.Context) {
	// 检查是否已经设置过头部。ResetEventStreamHeaders clears the flag to false
	// rather than deleting it, so the value -- not just its presence -- decides.
	if set, exists := c.Get("event_stream_headers_set"); exists {
		if alreadySet, ok := set.(bool); !ok || alreadySet {
			return
		}
	}

	// 设置标志，表示头部已经设置过
	c.Set("event_stream_headers_set", true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

type peekReadCloser struct {
	*bufio.Reader
	io.Closer
}

// ShouldTreatAsEventStream inspects mislabelled upstream responses.
// Some upstreams return a JSON body but mark the response as text/event-stream.
// When the client did not request streaming, prefer the body prefix over the
// header so non-stream requests still take the JSON response path.
func ShouldTreatAsEventStream(resp *http.Response, requestedStream bool) bool {
	if requestedStream || resp == nil {
		return requestedStream
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type"))), "text/event-stream") {
		return false
	}
	if resp.Body == nil {
		return true
	}

	reader := bufio.NewReader(resp.Body)
	resp.Body = &peekReadCloser{Reader: reader, Closer: resp.Body}

	peek, err := reader.Peek(512)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return true
	}

	trimmed := bytes.TrimLeft(peek, " \t\r\n")
	if len(trimmed) == 0 {
		return true
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return false
	}
	if bytes.HasPrefix(trimmed, []byte("data:")) || bytes.HasPrefix(trimmed, []byte("event:")) || trimmed[0] == ':' {
		return true
	}
	return true
}

// ClaudeData writes a Claude SSE event with a JSON payload.
func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	jsonData, err := common.Marshal(resp)
	if err != nil {
		common.SysError("error marshalling stream response: " + err.Error())
	} else {
		service.MarkResponsesBootstrapPayloadStarted(c)
		c.Render(-1, &common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
		c.Render(-1, &common.CustomEvent{Data: "data: " + string(jsonData)})
	}
	_ = FlushWriter(c)
	return nil
}

// ClaudeChunkData writes a Claude SSE event with a raw data payload.
func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) {
	service.MarkResponsesBootstrapPayloadStarted(c)
	c.Render(-1, &common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
	c.Render(-1, &common.CustomEvent{Data: fmt.Sprintf("data: %s\n", data)})
	_ = FlushWriter(c)
}

// ResponseChunkData writes a responses-stream SSE event with a raw payload.
func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) {
	service.MarkResponsesBootstrapPayloadStarted(c)
	c.Render(-1, &common.CustomEvent{Data: fmt.Sprintf("event: %s\n", resp.Type)})
	c.Render(-1, &common.CustomEvent{Data: fmt.Sprintf("data: %s", data)})
	_ = FlushWriter(c)
}

// StringData writes a standard OpenAI-style SSE data event.
func StringData(c *gin.Context, str string) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	service.MarkResponsesBootstrapPayloadStarted(c)
	c.Render(-1, &common.CustomEvent{Data: "data: " + str})
	return FlushWriter(c)
}

// PingData writes an SSE keepalive ping frame.
func PingData(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if c.Request != nil && c.Request.Context().Err() != nil {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
		return fmt.Errorf("write ping data failed: %w", err)
	}
	return FlushWriter(c)
}

// ObjectData marshals an object and writes it as an SSE data event.
func ObjectData(c *gin.Context, object interface{}) error {
	if object == nil {
		return errors.New("object is nil")
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	return StringData(c, string(jsonData))
}

// OpenAIErrorEvent writes an SSE error event carrying an OpenAI-compatible error body.
func OpenAIErrorEvent(c *gin.Context, openAIError types.OpenAIError) error {
	service.MarkResponsesBootstrapPayloadStarted(c)
	payload := map[string]any{
		"type":  "error",
		"error": openAIError,
	}
	jsonData, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshalling openai error event: %w", err)
	}
	c.Render(-1, &common.CustomEvent{Data: "event: error\n"})
	c.Render(-1, &common.CustomEvent{Data: "data: " + string(jsonData)})
	return FlushWriter(c)
}

// OpenAIChatErrorEvent writes an OpenAI Chat Completions-compatible terminal
// error for a stream that has already committed one or more chat chunks. Chat
// clients expect the error object directly in a data event; the Responses
// protocol's `event: error` envelope is not interchangeable here.
func OpenAIChatErrorEvent(c *gin.Context, openAIError types.OpenAIError) error {
	payload := map[string]any{"error": openAIError}
	jsonData, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error marshalling openai chat error event: %w", err)
	}
	return StringData(c, string(jsonData))
}

// Done writes the terminal [DONE] SSE marker.
func Done(c *gin.Context) {
	_ = StringData(c, "[DONE]")
}

// 实时中继的心跳参数。对端消失时 TCP 连接可以长时间保持"已建立"状态，
// 没有读超时的话两侧读协程会永久阻塞在 ReadMessage 上：连接、计费会话和
// 上游并发租约都不会释放。
var (
	realtimePingInterval = 30 * time.Second
	realtimePongWait     = 90 * time.Second
	realtimeWriteWait    = 10 * time.Second
)

// StartRealtimeKeepalive arms liveness detection on one side of the realtime
// relay: the read deadline is set, every pong extends it, and a ping is sent on
// an interval so an idle-but-alive peer keeps proving itself. The returned stop
// function ends the ping loop; it is safe to call more than once.
//
// Ping uses WriteControl, which gorilla allows concurrently with the data
// writes the relay goroutines perform on the same connection.
func StartRealtimeKeepalive(conn *websocket.Conn, label string) func() {
	if conn == nil {
		return func() {}
	}
	conn.SetReadDeadline(time.Now().Add(realtimePongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(realtimePongWait))
	})

	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() { close(done) })
	}
	go func() {
		ticker := time.NewTicker(realtimePingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(realtimeWriteWait)); err != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("realtime keepalive ping to %s failed: %s", label, err.Error()))
					return
				}
			}
		}
	}()
	return stop
}

// TouchRealtimeReadDeadline extends the read deadline after any successful
// read. A peer that sends data but never answers a ping is still alive, so
// traffic alone must count as liveness evidence.
func TouchRealtimeReadDeadline(conn *websocket.Conn) {
	if conn == nil {
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(realtimePongWait))
}

// WssString writes a string message to the websocket client.
func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

// WssObject marshals an object and writes it to the websocket client.
func WssObject(c *gin.Context, ws *websocket.Conn, object interface{}) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

// WssError sends an OpenAI-compatible realtime error event over websocket.
func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

// GetResponseID returns the synthetic chat-completions response identifier for the request.
func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

// GetLocalRealtimeID returns the synthetic realtime event identifier for the request.
func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

// GenerateStartEmptyResponse builds the initial empty assistant delta for chat streaming.
func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

// GenerateStopResponse builds the final stop chunk for chat streaming.
func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

// GenerateFinalUsageResponse builds the terminal usage chunk for chat streaming.
func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}
