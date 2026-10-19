package ollama

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	// Ollama streams are newline-delimited JSON. The default bufio.Scanner
	// limit (64 KiB) is smaller than valid tool-call arguments and can turn a
	// successful upstream response into a gateway 502. Keep a finite, explicit
	// ceiling so a malformed provider cannot allocate without bound.
	maxOllamaStreamLineBytes = 16 << 20
	maxOllamaToolCallIndex   = 1024
)

func newOllamaLineScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxOllamaStreamLineBytes)
	return scanner
}

type ollamaChatStreamChunk struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	// chat
	Message *struct {
		Role      string            `json:"role"`
		Content   string            `json:"content"`
		Thinking  common.RawMessage `json:"thinking"`
		ToolCalls []OllamaToolCall  `json:"tool_calls"`
	} `json:"message"`
	// generate
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	DoneReason         string `json:"done_reason"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	EvalCount          int    `json:"eval_count"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	EvalDuration       int64  `json:"eval_duration"`
}

func decodeOllamaThinking(raw common.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return "", nil
	}
	var thinking string
	if err := common.Unmarshal(raw, &thinking); err != nil {
		return "", fmt.Errorf("ollama thinking must be a JSON string: %w", err)
	}
	return thinking, nil
}

// ollamaToolCallAccumulator handles both the documented object-shaped
// arguments and providers that split a JSON argument string across chunks.
// Older Ollama responses omit an id, so the function index (or, when absent,
// the position in tool_calls) is the stable identity while a stream is being
// assembled.
type ollamaToolCallAccumulator struct {
	id            string
	name          string
	providerIndex *int
	arguments     map[string]any
	raw           strings.Builder
}

func (a *ollamaToolCallAccumulator) merge(call OllamaToolCall, index int) error {
	if id := strings.TrimSpace(call.ID); id != "" {
		if a.id != "" && a.id != id {
			return fmt.Errorf("ollama tool_calls[%d].id changed from %q to %q", index, a.id, id)
		}
		a.id = id
	}
	name := strings.TrimSpace(call.Function.Name)
	if name != "" {
		if a.name != "" && a.name != name {
			return fmt.Errorf("ollama tool_calls[%d].function.name changed from %q to %q", index, a.name, name)
		}
		a.name = name
	}
	if call.Function.Index != nil {
		if *call.Function.Index < 0 {
			return fmt.Errorf("ollama tool_calls[%d].function.index must not be negative", index)
		}
		if a.providerIndex != nil && *a.providerIndex != *call.Function.Index {
			return fmt.Errorf("ollama tool_calls[%d].function.index changed from %d to %d", index, *a.providerIndex, *call.Function.Index)
		}
		value := *call.Function.Index
		a.providerIndex = &value
	}
	if a.arguments == nil {
		a.arguments = make(map[string]any)
	}
	return a.mergeArguments(call.Function.Arguments, index)
}

func (a *ollamaToolCallAccumulator) mergeArguments(value any, index int) error {
	if value == nil {
		return nil
	}
	if raw, ok := value.(common.RawMessage); ok {
		return a.mergeRawMessage(raw, index)
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		var object map[string]any
		if err := common.Unmarshal([]byte(text), &object); err == nil && object != nil {
			for key, item := range object {
				a.arguments[key] = item
			}
			return nil
		}
		// A non-object string may be a fragment. Defer validation until all
		// chunks have been received so a split such as {"q":"st + atus"}
		// is not rejected prematurely.
		a.raw.WriteString(text)
		return nil
	}
	data, err := common.Marshal(value)
	if err != nil {
		return fmt.Errorf("ollama tool_calls[%d].function.arguments cannot be encoded: %w", index, err)
	}
	var object map[string]any
	if err := common.Unmarshal(data, &object); err != nil || object == nil {
		if err == nil {
			err = fmt.Errorf("arguments must be a JSON object")
		}
		return fmt.Errorf("ollama tool_calls[%d].function.arguments is invalid: %w", index, err)
	}
	for key, item := range object {
		a.arguments[key] = item
	}
	return nil
}

func (a *ollamaToolCallAccumulator) mergeRawMessage(raw common.RawMessage, index int) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if common.GetJsonType(raw) == "string" {
		var text string
		if err := common.Unmarshal(raw, &text); err != nil {
			return fmt.Errorf("ollama tool_calls[%d].function.arguments is invalid: %w", index, err)
		}
		return a.mergeArguments(text, index)
	}
	var object map[string]any
	if err := common.Unmarshal(raw, &object); err != nil || object == nil {
		if err == nil {
			err = fmt.Errorf("arguments must be a JSON object")
		}
		return fmt.Errorf("ollama tool_calls[%d].function.arguments is invalid: %w", index, err)
	}
	if a.arguments == nil {
		a.arguments = make(map[string]any)
	}
	for key, item := range object {
		a.arguments[key] = item
	}
	return nil
}

func (a *ollamaToolCallAccumulator) convert(index int) (dto.ToolCallResponse, error) {
	if strings.TrimSpace(a.name) == "" {
		return dto.ToolCallResponse{}, fmt.Errorf("ollama tool_calls[%d].function.name is empty", index)
	}
	arguments := make(map[string]any, len(a.arguments))
	for key, value := range a.arguments {
		arguments[key] = value
	}
	if raw := strings.TrimSpace(a.raw.String()); raw != "" {
		var object map[string]any
		if err := common.Unmarshal([]byte(raw), &object); err != nil || object == nil {
			if err == nil {
				err = fmt.Errorf("arguments must be a JSON object")
			}
			return dto.ToolCallResponse{}, fmt.Errorf("ollama tool_calls[%d].function.arguments is invalid: %w", index, err)
		}
		for key, value := range object {
			arguments[key] = value
		}
	}
	argumentBytes, err := common.Marshal(arguments)
	if err != nil {
		return dto.ToolCallResponse{}, fmt.Errorf("ollama tool_calls[%d].function.arguments cannot be encoded: %w", index, err)
	}
	id := a.id
	if id == "" {
		id = fmt.Sprintf("call_%d", index)
	}
	result := dto.ToolCallResponse{
		ID:   id,
		Type: "function",
		Function: dto.FunctionResponse{
			Name:      a.name,
			Arguments: string(argumentBytes),
		},
	}
	result.SetIndex(index)
	return result, nil
}

func mergeOllamaToolCalls(accumulators *[]ollamaToolCallAccumulator, calls []OllamaToolCall) error {
	for position, call := range calls {
		index := position
		if call.Function.Index != nil {
			index = *call.Function.Index
			if index < 0 {
				return fmt.Errorf("ollama tool_calls[%d].function.index must not be negative", position)
			}
		}
		if index > maxOllamaToolCallIndex {
			return fmt.Errorf("ollama tool_calls[%d].function.index %d exceeds maximum %d", position, index, maxOllamaToolCallIndex)
		}
		for len(*accumulators) <= index {
			*accumulators = append(*accumulators, ollamaToolCallAccumulator{})
		}
		if err := (*accumulators)[index].merge(call, index); err != nil {
			return err
		}
	}
	return nil
}

func convertOllamaToolCalls(accumulators []ollamaToolCallAccumulator) ([]dto.ToolCallResponse, error) {
	if len(accumulators) == 0 {
		return nil, nil
	}
	result := make([]dto.ToolCallResponse, 0, len(accumulators))
	for _, accumulator := range accumulators {
		// Providers occasionally emit explicit, non-contiguous function.index
		// values. mergeOllamaToolCalls must retain those positions while the
		// stream is being assembled, but the synthetic holes are not tool calls
		// and must not be converted (an empty hole would otherwise fail on the
		// missing function name).
		if strings.TrimSpace(accumulator.name) == "" && accumulator.id == "" && len(accumulator.arguments) == 0 && accumulator.raw.Len() == 0 {
			continue
		}
		index := len(result)
		converted, err := accumulator.convert(index)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}
	return result, nil
}

func toUnix(ts string) int64 {
	if ts == "" {
		return time.Now().Unix()
	}
	// try time.RFC3339 or with nanoseconds
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339, ts)
		if err2 == nil {
			return t2.Unix()
		}
		return time.Now().Unix()
	}
	return t.Unix()
}

func ollamaStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("empty response"), types.ErrorCodeBadResponse, http.StatusBadRequest)
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, err := readOllamaResponseBody(resp.Body)
		if err != nil {
			return nil, types.NewOpenAIError(fmt.Errorf("read Ollama error response: %w", err), types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
		}
		return nil, types.NewOpenAIError(fmt.Errorf("Ollama returned HTTP %d: %s", resp.StatusCode, common.LocalUpstreamErrorPreview(string(body))), types.ErrorCodeBadResponseBody, resp.StatusCode)
	}
	if info == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("Ollama relay info is nil"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	helper.SetEventStreamHeaders(c)
	scanner := newOllamaLineScanner(resp.Body)
	usage := &dto.Usage{}
	model := info.UpstreamModelName
	responseId := common.GetUUID()
	created := time.Now().Unix()
	pendingToolCalls := make([]ollamaToolCallAccumulator, 0)
	sawDone := false
	start := helper.GenerateStartEmptyResponse(responseId, created, model, nil)
	data, err := common.Marshal(start)
	if err != nil {
		return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if err := helper.StringData(c, string(data)); err != nil {
		return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var chunk ollamaChatStreamChunk
		if err := common.Unmarshal([]byte(line), &chunk); err != nil {
			logger.LogError(c, "ollama stream json decode error: "+err.Error()+" line="+line)
			return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		created = toUnix(chunk.CreatedAt)
		if chunk.Message != nil && len(chunk.Message.ToolCalls) > 0 {
			if err := mergeOllamaToolCalls(&pendingToolCalls, chunk.Message.ToolCalls); err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		// Although current Ollama releases normally place the final text and
		// thinking in non-done chunks, preserve any payload carried by the done
		// frame instead of silently dropping it.
		if chunk.Done && chunk.Message != nil && len(chunk.Message.Thinking) > 0 {
			thinkingContent, err := decodeOllamaThinking(chunk.Message.Thinking)
			if err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if thinkingContent != "" {
				delta := dto.ChatCompletionsStreamResponse{
					Id: responseId, Object: "chat.completion.chunk", Created: created, Model: model,
					Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{}}},
				}
				delta.Choices[0].Delta.SetReasoningContent(thinkingContent)
				data, err := common.Marshal(delta)
				if err != nil {
					return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
				if err := helper.StringData(c, string(data)); err != nil {
					return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
			}
		}

		if !chunk.Done {
			content := ""
			if chunk.Message != nil {
				content = chunk.Message.Content
			}
			if content == "" {
				content = chunk.Response
			}
			delta := dto.ChatCompletionsStreamResponse{
				Id: responseId, Object: "chat.completion.chunk", Created: created, Model: model,
				Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}}},
			}
			if content != "" {
				delta.Choices[0].Delta.SetContentString(content)
			}
			if chunk.Message != nil && len(chunk.Message.Thinking) > 0 {
				thinkingContent, err := decodeOllamaThinking(chunk.Message.Thinking)
				if err != nil {
					return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
				if thinkingContent != "" {
					delta.Choices[0].Delta.SetReasoningContent(thinkingContent)
				}
			}
			data, err := common.Marshal(delta)
			if err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if err := helper.StringData(c, string(data)); err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			continue
		}
		content := ""
		if chunk.Message != nil {
			content = chunk.Message.Content
		}
		if content == "" {
			content = chunk.Response
		}
		if content != "" {
			delta := dto.ChatCompletionsStreamResponse{
				Id: responseId, Object: "chat.completion.chunk", Created: created, Model: model,
				Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{}}},
			}
			delta.Choices[0].Delta.SetContentString(content)
			data, err := common.Marshal(delta)
			if err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if err := helper.StringData(c, string(data)); err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}

		sawDone = true
		usage.PromptTokens = chunk.PromptEvalCount
		usage.CompletionTokens = chunk.EvalCount
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		finishReason := chunk.DoneReason
		if finishReason == "" {
			finishReason = "stop"
		}
		if len(pendingToolCalls) > 0 {
			toolCalls, err := convertOllamaToolCalls(pendingToolCalls)
			if err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			toolDelta := dto.ChatCompletionsStreamResponse{
				Id: responseId, Object: "chat.completion.chunk", Created: created, Model: model,
				Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: toolCalls}}},
			}
			data, err := common.Marshal(toolDelta)
			if err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if err := helper.StringData(c, string(data)); err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			finishReason = "tool_calls"
		}
		if stop := helper.GenerateStopResponse(responseId, created, model, finishReason); stop != nil {
			data, err := common.Marshal(stop)
			if err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if err := helper.StringData(c, string(data)); err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		if final := helper.GenerateFinalUsageResponse(responseId, created, model, *usage); final != nil {
			data, err := common.Marshal(final)
			if err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if err := helper.StringData(c, string(data)); err != nil {
				return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		if err := helper.StringData(c, "[DONE]"); err != nil {
			return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		break
	}
	if err := scanner.Err(); err != nil && err != io.EOF {
		logger.LogError(c, "ollama stream scan error: "+err.Error())
		return usage, types.NewOpenAIError(err, types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)
	}
	if !sawDone {
		return usage, types.NewOpenAIError(fmt.Errorf("ollama stream ended without done frame"), types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)
	}
	helper.RecordStreamEnd(c, info, true)
	return usage, nil
}

// non-stream handler for chat/generate
func ollamaChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("ollama response is empty"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, err := readOllamaResponseBody(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, types.NewOpenAIError(fmt.Errorf("ollama returned HTTP %d: %s", resp.StatusCode, common.LocalUpstreamErrorPreview(string(body))), types.ErrorCodeBadResponseBody, resp.StatusCode)
	}
	if info == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("ollama relay info is nil"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	raw := string(body)
	if common.DebugEnabled {
		println("ollama non-stream raw resp:", raw)
	}

	lines := strings.Split(raw, "\n")
	var (
		aggContent       strings.Builder
		reasoningBuilder strings.Builder
		lastChunk        ollamaChatStreamChunk
		parsedAny        bool
		sawDone          bool
		pendingToolCalls []ollamaToolCallAccumulator
	)
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ck ollamaChatStreamChunk
		if err := common.Unmarshal([]byte(ln), &ck); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		parsedAny = true
		lastChunk = ck
		if ck.Done {
			sawDone = true
		}
		if ck.Message != nil && len(ck.Message.ToolCalls) > 0 {
			if err := mergeOllamaToolCalls(&pendingToolCalls, ck.Message.ToolCalls); err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		if ck.Message != nil && len(ck.Message.Thinking) > 0 {
			thinkingContent, err := decodeOllamaThinking(ck.Message.Thinking)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
			if thinkingContent != "" {
				reasoningBuilder.WriteString(thinkingContent)
			}
		}
		if ck.Message != nil && ck.Message.Content != "" {
			aggContent.WriteString(ck.Message.Content)
		} else if ck.Response != "" {
			aggContent.WriteString(ck.Response)
		}
	}

	if !parsedAny {
		var single ollamaChatStreamChunk
		if err := common.Unmarshal(body, &single); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		lastChunk = single
		sawDone = single.Done
		if single.Message != nil && len(single.Message.ToolCalls) > 0 {
			if err := mergeOllamaToolCalls(&pendingToolCalls, single.Message.ToolCalls); err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		if single.Message != nil {
			if len(single.Message.Thinking) > 0 {
				thinkingContent, err := decodeOllamaThinking(single.Message.Thinking)
				if err != nil {
					return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
				}
				if thinkingContent != "" {
					reasoningBuilder.WriteString(thinkingContent)
				}
			}
			aggContent.WriteString(single.Message.Content)
		} else {
			aggContent.WriteString(single.Response)
		}
	}
	if !sawDone {
		return nil, types.NewOpenAIError(fmt.Errorf("ollama response ended without done frame"), types.ErrorCodeUpstreamTransportInterrupted, http.StatusBadGateway)
	}

	model := lastChunk.Model
	if model == "" {
		model = info.UpstreamModelName
	}
	created := toUnix(lastChunk.CreatedAt)
	usage := &dto.Usage{PromptTokens: lastChunk.PromptEvalCount, CompletionTokens: lastChunk.EvalCount, TotalTokens: lastChunk.PromptEvalCount + lastChunk.EvalCount}
	content := aggContent.String()
	finishReason := lastChunk.DoneReason
	if finishReason == "" {
		finishReason = "stop"
	}
	toolCalls, err := convertOllamaToolCalls(pendingToolCalls)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{Role: "assistant", Content: contentPtr(content)}
	if rc := reasoningBuilder.String(); rc != "" {
		msg.ReasoningContent = &rc
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
	}
	full := dto.OpenAITextResponse{
		Id:      common.GetUUID(),
		Model:   model,
		Object:  "chat.completion",
		Created: created,
		Choices: []dto.OpenAITextResponseChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: *usage,
	}
	out, _ := common.Marshal(full)
	service.IOCopyBytesGracefully(c, resp, out)
	return usage, nil
}

func contentPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
