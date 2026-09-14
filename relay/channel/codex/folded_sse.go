package codex

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const (
	codexFoldedStep             = 518
	codexFoldedMaxContinuation  = 3
	codexFoldedNudgeText        = "Continue thinking..."
	codexFoldedMaxEventBytes    = 64 << 20
	codexFoldedStatusBodyLimit  = 1 << 20
	codexFoldedBilledUsageKey   = "proxy_billed_usage"
	codexFoldedRoundsKey        = "proxy_rounds"
	codexFoldedStoppedReasonKey = "proxy_stopped_reason"
)

var codexFoldedReadTimeout = 5 * time.Minute

type codexFoldedEvent struct {
	fields map[string]common.RawMessage
	typeID string
}

type codexFoldedReadResult struct {
	event *codexFoldedEvent
	err   error
}

type codexFoldedRound struct {
	response      map[string]common.RawMessage
	usage         *dto.Usage
	terminal      string
	terminalEvent *codexFoldedEvent
	stopReason    string
	buffered      []codexFoldedBufferedItem
	reasoning     []map[string]common.RawMessage
}

type codexFoldedBufferedItem struct {
	outputIndex int
	item        map[string]common.RawMessage
	events      []*codexFoldedEvent
}

type codexFoldedState struct {
	baseResponse       map[string]common.RawMessage
	finalOutput        []map[string]common.RawMessage
	replayTail         []map[string]common.RawMessage
	rounds             []map[string]any
	billed             dto.Usage
	firstUsage         *dto.Usage
	foldedReasoning    int
	foldedCleanOutput  int
	downstreamSequence int
	downstreamIndex    int
	turnState          string
	isCompaction       bool
}

func codexFoldedEnabled(info *relaycommon.RelayInfo) bool {
	return info != nil && info.IsStream &&
		info.RelayMode == relayconstant.RelayModeResponses &&
		info.ChannelType == constant.ChannelTypeCodex &&
		info.ChannelOtherSettings.CodexFoldedSSEEnabled
}

func codexFoldedReasoningTokens(usage *dto.Usage) (int, bool) {
	if usage == nil {
		return 0, false
	}
	details, _ := codexFoldedOutputDetails(usage)
	return details.ReasoningTokens, true
}

func codexFoldedInputDetails(usage *dto.Usage) dto.InputTokenDetails {
	if usage == nil {
		return dto.InputTokenDetails{}
	}
	if usage.InputTokensDetails != nil {
		return *usage.InputTokensDetails
	}
	return usage.PromptTokensDetails
}

func codexFoldedOutputDetails(usage *dto.Usage) (dto.OutputTokenDetails, bool) {
	if usage == nil {
		return dto.OutputTokenDetails{}, false
	}
	if usage.OutputTokensDetails == nil {
		return usage.CompletionTokenDetails, true
	}
	details := *usage.OutputTokensDetails
	// Some proxies emit both Responses and Chat usage containers. Preserve a
	// non-zero legacy reasoning count when the Responses container omits it.
	if details.ReasoningTokens == 0 && usage.CompletionTokenDetails.ReasoningTokens != 0 {
		details.ReasoningTokens = usage.CompletionTokenDetails.ReasoningTokens
	}
	return details, true
}

func codexFoldedTier(reasoningTokens int) (int, bool) {
	if reasoningTokens < codexFoldedStep-2 || (reasoningTokens+2)%codexFoldedStep != 0 {
		return 0, false
	}
	return (reasoningTokens + 2) / codexFoldedStep, true
}

func codexFoldedRawObject(raw common.RawMessage) (map[string]common.RawMessage, error) {
	var object map[string]common.RawMessage
	if err := common.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, errors.New("folded SSE object is null")
	}
	return object, nil
}

func codexFoldedAppendSSEData(data *strings.Builder, value string, maxBytes int) error {
	if data == nil {
		return errors.New("folded SSE data builder is nil")
	}
	value = strings.TrimSpace(value)
	separatorBytes := 0
	if data.Len() > 0 {
		separatorBytes = 1
	}
	if maxBytes <= 0 || data.Len() > maxBytes-separatorBytes || len(value) > maxBytes-data.Len()-separatorBytes {
		return fmt.Errorf("folded SSE event exceeds %d bytes", maxBytes)
	}
	if separatorBytes > 0 {
		data.WriteByte('\n')
	}
	data.WriteString(value)
	return nil
}

func codexFoldedReadSSE(body io.ReadCloser, done <-chan struct{}) <-chan codexFoldedReadResult {
	results := make(chan codexFoldedReadResult, 1)
	go func() {
		defer close(results)
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 64<<10), codexFoldedMaxEventBytes)
		var data strings.Builder
		emit := func() bool {
			payload := strings.TrimSpace(data.String())
			data.Reset()
			if payload == "" || payload == "[DONE]" {
				return true
			}
			fields, err := codexFoldedRawObject(common.RawMessage(payload))
			if err != nil {
				select {
				case results <- codexFoldedReadResult{err: fmt.Errorf("decode folded SSE event: %w", err)}:
				case <-done:
				}
				return false
			}
			typeID := common.JsonRawMessageToString(fields["type"])
			select {
			case results <- codexFoldedReadResult{event: &codexFoldedEvent{fields: fields, typeID: typeID}}:
				return true
			case <-done:
				return false
			}
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if !emit() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				value := strings.TrimPrefix(line, "data:")
				if err := codexFoldedAppendSSEData(&data, value, codexFoldedMaxEventBytes); err != nil {
					select {
					case results <- codexFoldedReadResult{err: err}:
					case <-done:
					}
					return
				}
			}
		}
		if data.Len() > 0 && !emit() {
			return
		}
		if err := scanner.Err(); err != nil {
			select {
			case results <- codexFoldedReadResult{err: err}:
			case <-done:
			}
			return
		}
		select {
		case results <- codexFoldedReadResult{err: io.EOF}:
		case <-done:
		}
	}()
	return results
}

func codexFoldedFieldInt(fields map[string]common.RawMessage, name string) (int, bool) {
	var value int
	raw, ok := fields[name]
	if !ok || common.Unmarshal(raw, &value) != nil {
		return 0, false
	}
	return value, true
}

func codexFoldedItem(event *codexFoldedEvent) (map[string]common.RawMessage, bool) {
	if event == nil {
		return nil, false
	}
	raw, ok := event.fields["item"]
	if !ok {
		return nil, false
	}
	item, err := codexFoldedRawObject(raw)
	return item, err == nil
}

func codexFoldedItemType(item map[string]common.RawMessage) string {
	return common.JsonRawMessageToString(item["type"])
}

func codexFoldedHasEncryptedContent(item map[string]common.RawMessage) bool {
	raw, ok := item["encrypted_content"]
	if !ok || common.GetJsonType(raw) != "string" {
		return false
	}
	var value string
	if common.Unmarshal(raw, &value) != nil {
		return false
	}
	return strings.TrimSpace(value) != ""
}

func codexFoldedRawArray(items []map[string]common.RawMessage) (common.RawMessage, error) {
	data, err := common.Marshal(items)
	return common.RawMessage(data), err
}

func codexFoldedSetInt(fields map[string]common.RawMessage, name string, value int) {
	data, _ := common.Marshal(value)
	fields[name] = data
}

func codexFoldedSend(c *gin.Context, state *codexFoldedState, event *codexFoldedEvent) bool {
	if c == nil || event == nil || state == nil || c.Request.Context().Err() != nil {
		return false
	}
	codexFoldedSetInt(event.fields, "sequence_number", state.downstreamSequence)
	state.downstreamSequence++
	data, err := common.Marshal(event.fields)
	if err != nil {
		return false
	}
	helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: event.typeID}, string(data))
	return c.Request.Context().Err() == nil
}

func codexFoldedCopyEvent(event *codexFoldedEvent) *codexFoldedEvent {
	if event == nil {
		return nil
	}
	fields := make(map[string]common.RawMessage, len(event.fields))
	for key, value := range event.fields {
		fields[key] = append(common.RawMessage(nil), value...)
	}
	return &codexFoldedEvent{fields: fields, typeID: event.typeID}
}

func codexFoldedMergeUsage(target *dto.Usage, source *dto.Usage) {
	if target == nil || source == nil {
		return
	}
	target.PromptTokens += source.PromptTokens
	target.CompletionTokens += source.CompletionTokens
	target.TotalTokens += source.TotalTokens
	target.InputTokens += source.InputTokens
	target.OutputTokens += source.OutputTokens
	inputDetails := codexFoldedInputDetails(source)
	target.PromptTokensDetails.CachedTokens += inputDetails.CachedTokens
	target.PromptTokensDetails.CachedCreationTokens += inputDetails.CachedCreationTokens
	target.PromptTokensDetails.TextTokens += inputDetails.TextTokens
	target.PromptTokensDetails.AudioTokens += inputDetails.AudioTokens
	target.PromptTokensDetails.ImageTokens += inputDetails.ImageTokens
	if source.InputTokensDetails != nil || target.InputTokensDetails != nil {
		if target.InputTokensDetails == nil {
			target.InputTokensDetails = &dto.InputTokenDetails{}
		}
		target.InputTokensDetails.CachedTokens += inputDetails.CachedTokens
		target.InputTokensDetails.CachedCreationTokens += inputDetails.CachedCreationTokens
		target.InputTokensDetails.TextTokens += inputDetails.TextTokens
		target.InputTokensDetails.AudioTokens += inputDetails.AudioTokens
		target.InputTokensDetails.ImageTokens += inputDetails.ImageTokens
	}
	outputDetails, _ := codexFoldedOutputDetails(source)
	target.CompletionTokenDetails.ReasoningTokens += outputDetails.ReasoningTokens
	target.CompletionTokenDetails.TextTokens += outputDetails.TextTokens
	target.CompletionTokenDetails.AudioTokens += outputDetails.AudioTokens
	target.CompletionTokenDetails.ImageTokens += outputDetails.ImageTokens
	if source.OutputTokensDetails != nil || target.OutputTokensDetails != nil {
		if target.OutputTokensDetails == nil {
			target.OutputTokensDetails = &dto.OutputTokenDetails{}
		}
		target.OutputTokensDetails.ReasoningTokens += outputDetails.ReasoningTokens
		target.OutputTokensDetails.TextTokens += outputDetails.TextTokens
		target.OutputTokensDetails.AudioTokens += outputDetails.AudioTokens
		target.OutputTokensDetails.ImageTokens += outputDetails.ImageTokens
	}
	if target.PromptTokens == 0 {
		target.PromptTokens = target.InputTokens
	}
	if target.CompletionTokens == 0 {
		target.CompletionTokens = target.OutputTokens
	}
	if target.TotalTokens == 0 {
		target.TotalTokens = target.PromptTokens + target.CompletionTokens
	}
}

func codexFoldedUsageFromResponse(fields map[string]common.RawMessage) *dto.Usage {
	raw, ok := fields["usage"]
	if !ok {
		return nil
	}
	var usage dto.Usage
	if common.Unmarshal(raw, &usage) != nil {
		return nil
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokens == 0 {
		usage.InputTokens = usage.PromptTokens
	}
	if usage.OutputTokens == 0 {
		usage.OutputTokens = usage.CompletionTokens
	}
	return &usage
}

func codexFoldedBuildNudge() map[string]common.RawMessage {
	return map[string]common.RawMessage{
		"type":  common.RawMessage(`"message"`),
		"role":  common.RawMessage(`"assistant"`),
		"phase": common.RawMessage(`"commentary"`),
		"content": mustMarshalRaw([]map[string]common.RawMessage{{
			"type": common.RawMessage(`"output_text"`),
			"text": mustMarshalRaw(codexFoldedNudgeText),
		}}),
	}
}

func codexFoldedBuildContinuationBody(base []byte, replay []map[string]common.RawMessage) ([]byte, error) {
	var body map[string]common.RawMessage
	if err := common.Unmarshal(base, &body); err != nil {
		return nil, fmt.Errorf("decode Codex folded base body: %w", err)
	}
	rawInput, ok := body["input"]
	if !ok {
		return nil, errors.New("Codex folded continuation requires input")
	}
	var input []map[string]common.RawMessage
	switch common.GetJsonType(rawInput) {
	case "array":
		if err := common.Unmarshal(rawInput, &input); err != nil {
			return nil, errors.New("Codex folded continuation requires array input")
		}
	case "string":
		// Responses accepts a shorthand string input. Normalize it to one
		// user message before appending the replay tail for the next round.
		input = []map[string]common.RawMessage{{
			"type":    common.RawMessage(`"message"`),
			"role":    common.RawMessage(`"user"`),
			"content": append(common.RawMessage(nil), rawInput...),
		}}
	default:
		return nil, errors.New("Codex folded continuation requires array or string input")
	}
	input = append(input, replay...)
	inputRaw, err := codexFoldedRawArray(input)
	if err != nil {
		return nil, err
	}
	body["input"] = inputRaw
	body["stream"] = common.RawMessage("true")
	delete(body, "previous_response_id")
	var include []string
	if raw, ok := body["include"]; ok {
		if common.GetJsonType(raw) != "array" {
			return nil, errors.New("Codex folded continuation requires array include")
		}
		if err := common.Unmarshal(raw, &include); err != nil {
			return nil, errors.New("Codex folded continuation requires array include")
		}
	}
	found := false
	for _, value := range include {
		if value == "reasoning.encrypted_content" {
			found = true
		}
	}
	if !found {
		include = append(include, "reasoning.encrypted_content")
	}
	body["include"], err = common.Marshal(include)
	if err != nil {
		return nil, err
	}
	return common.Marshal(body)
}

func codexFoldedTerminal(fields map[string]common.RawMessage) map[string]common.RawMessage {
	if fields == nil {
		return map[string]common.RawMessage{}
	}
	response, _ := codexFoldedRawObject(fields["response"])
	return response
}

func codexFoldedCloneFields(fields map[string]common.RawMessage) map[string]common.RawMessage {
	clone := make(map[string]common.RawMessage, len(fields))
	for key, value := range fields {
		clone[key] = append(common.RawMessage(nil), value...)
	}
	return clone
}

func codexFoldedReconstructResponse(state *codexFoldedState, terminal map[string]common.RawMessage) map[string]common.RawMessage {
	response := codexFoldedCloneFields(terminal)
	if response == nil {
		response = make(map[string]common.RawMessage)
	}
	if state != nil {
		for _, key := range []string{"id", "object", "created_at", "model"} {
			if value, ok := state.baseResponse[key]; ok {
				response[key] = append(common.RawMessage(nil), value...)
			}
		}
	}
	return response
}

func codexFoldedRequestHasCompactionTrigger(body []byte) bool {
	var request map[string]common.RawMessage
	if common.Unmarshal(body, &request) != nil {
		return false
	}
	var input []map[string]common.RawMessage
	if common.Unmarshal(request["input"], &input) != nil || len(input) == 0 {
		return false
	}
	return common.JsonRawMessageToString(input[len(input)-1]["type"]) == "compaction_trigger"
}

func codexFoldedRoundResponse(round *codexFoldedRound) map[string]common.RawMessage {
	if round == nil {
		return nil
	}
	return round.response
}

func codexFoldedAddMetadata(response map[string]common.RawMessage, state *codexFoldedState, stopped string) error {
	metadata := map[string]common.RawMessage{}
	if raw, ok := response["metadata"]; ok {
		if decoded, err := codexFoldedRawObject(raw); err == nil {
			metadata = decoded
		}
	}
	rounds, err := common.Marshal(state.rounds)
	if err != nil {
		return err
	}
	billed, err := common.Marshal(state.billed)
	if err != nil {
		return err
	}
	metadata[codexFoldedRoundsKey] = rounds
	metadata[codexFoldedBilledUsageKey] = billed
	if stopped != "" {
		metadata[codexFoldedStoppedReasonKey] = common.RawMessage(fmt.Sprintf("%q", stopped))
	}
	response["metadata"], err = common.Marshal(metadata)
	return err
}

func codexFoldedBuildUsage(response map[string]common.RawMessage, state *codexFoldedState, clean bool) error {
	usage := map[string]common.RawMessage{}
	if raw, ok := response["usage"]; ok {
		if decoded, err := codexFoldedRawObject(raw); err == nil {
			usage = decoded
		}
	}
	input := 0
	if state.firstUsage != nil {
		input = state.firstUsage.InputTokens
		if input == 0 {
			input = state.firstUsage.PromptTokens
		}
	}
	output := state.foldedReasoning
	if clean {
		output += state.foldedCleanOutput
	}
	total := input + output
	for key, value := range map[string]int{"input_tokens": input, "output_tokens": output, "total_tokens": total} {
		data, _ := common.Marshal(value)
		usage[key] = data
	}
	usage["prompt_tokens"], _ = common.Marshal(input)
	usage["completion_tokens"], _ = common.Marshal(output)
	if state.firstUsage != nil {
		inputDetails := state.firstUsage.PromptTokensDetails
		if state.firstUsage.InputTokensDetails != nil {
			inputDetails = *state.firstUsage.InputTokensDetails
		}
		inputDetailsRaw := map[string]common.RawMessage{}
		inputDetailsRaw["cached_tokens"], _ = common.Marshal(inputDetails.CachedTokens)
		if inputDetails.CachedCreationTokens != 0 {
			inputDetailsRaw["cached_creation_tokens"], _ = common.Marshal(inputDetails.CachedCreationTokens)
		}
		usage["input_tokens_details"], _ = common.Marshal(inputDetailsRaw)
	}
	// The client usage describes the folded response that remains visible to the
	// caller. All upstream rounds, including discarded continuation output, are
	// retained separately in proxy_billed_usage for internal settlement.
	details := map[string]common.RawMessage{}
	if raw, ok := usage["output_tokens_details"]; ok {
		if decoded, err := codexFoldedRawObject(raw); err == nil {
			details = decoded
		}
	}
	reasonData, _ := common.Marshal(state.foldedReasoning)
	details["reasoning_tokens"] = reasonData
	usage["output_tokens_details"], _ = common.Marshal(details)
	response["usage"], _ = common.Marshal(usage)
	return nil
}

func codexFoldedMarkAbnormalEnd(info *relaycommon.RelayInfo, reason string) {
	if info == nil || info.StreamStatus == nil {
		return
	}
	switch reason {
	case "upstream_timeout":
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonTimeout, errors.New("Codex folded upstream read timed out"))
	case "upstream_eof":
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, io.ErrUnexpectedEOF)
	case "upstream_error", "invalid_terminal_response":
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonUpstreamInterrupted, errors.New("Codex folded upstream response failed"))
	}
}

func codexFoldedMarkTerminalEnd(info *relaycommon.RelayInfo, stopReason string) {
	if info == nil || info.StreamStatus == nil {
		return
	}
	if stopReason != "" {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, nil)
		return
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
}

func codexFoldedBuildIncompleteResponse(state *codexFoldedState, reason string) (*codexFoldedEvent, error) {
	response := make(map[string]common.RawMessage, len(state.baseResponse)+4)
	for key, value := range state.baseResponse {
		response[key] = append(common.RawMessage(nil), value...)
	}
	response["status"] = common.RawMessage(`"incomplete"`)
	response["incomplete_details"] = common.RawMessage(fmt.Sprintf(`{"reason":%q}`, reason))
	output, err := codexFoldedRawArray(state.finalOutput)
	if err != nil {
		return nil, err
	}
	response["output"] = output
	if err := codexFoldedBuildUsage(response, state, false); err != nil {
		return nil, err
	}
	if err := codexFoldedAddMetadata(response, state, reason); err != nil {
		return nil, err
	}
	fields := map[string]common.RawMessage{
		"type":     common.RawMessage(`"response.incomplete"`),
		"response": mustMarshalRaw(response),
	}
	return &codexFoldedEvent{fields: fields, typeID: "response.incomplete"}, nil
}

func mustMarshalRaw(value any) common.RawMessage {
	data, _ := common.Marshal(value)
	return data
}

func codexFoldedOpenContinuation(c *gin.Context, info *relaycommon.RelayInfo, adaptor *Adaptor, body []byte, turnState string) (*http.Response, error) {
	url, err := adaptor.GetRequestURL(info)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	info.UpstreamRequestBodySize = int64(len(body))
	info.UpstreamRequestGetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	if err := adaptor.SetupRequestHeader(c, &req.Header, info); err != nil {
		return nil, err
	}
	headerOverride, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	channel.ApplyHeaderOverrideToRequest(req, headerOverride)
	if strings.TrimSpace(turnState) != "" {
		req.Header.Set("X-Codex-Turn-State", turnState)
	}
	return channel.DoRequest(c, req, info)
}

func codexFoldedOpenResponseError(c *gin.Context, resp *http.Response) error {
	if resp == nil {
		return errors.New("Codex folded continuation returned nil response")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if !codexFoldedIsSSEResponse(resp) || resp.Body == nil {
		ctx := context.Background()
		if c != nil && c.Request != nil {
			ctx = c.Request.Context()
		}
		return service.RelayErrorHandler(ctx, resp, false)
	}

	// A non-2xx SSE response is only a valid folded stream when it carries a
	// complete response.failed terminal. Buffer the bounded error body so we can
	// inspect that terminal and then restore the reader for the normal event
	// processor. Other SSE bodies (including response.completed/incomplete,
	// malformed events, or an EOF without a failure terminal) must remain an
	// upstream HTTP error instead of being converted into a successful synthetic
	// response.incomplete.
	body, err := io.ReadAll(io.LimitReader(resp.Body, codexFoldedStatusBodyLimit+1))
	_ = resp.Body.Close()
	if err != nil {
		return types.NewOpenAIError(
			fmt.Errorf("read Codex folded non-2xx SSE response: %w", err),
			types.ErrorCodeReadResponseBodyFailed,
			resp.StatusCode,
			types.ErrOptionWithSkipRetry(),
		)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) <= codexFoldedStatusBodyLimit && codexFoldedSSEHasValidFailureTerminal(body) {
		return nil
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	return service.RelayErrorHandler(ctx, resp, false)
}

func codexFoldedIsSSEResponse(resp *http.Response) bool {
	return resp != nil && strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type"))), "text/event-stream")
}

func codexFoldedSSEHasValidFailureTerminal(body []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 4<<10), codexFoldedStatusBodyLimit)
	var data strings.Builder
	emit := func() bool {
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "" || payload == "[DONE]" {
			return false
		}
		fields, err := codexFoldedRawObject(common.RawMessage(payload))
		if err != nil || common.JsonRawMessageToString(fields["type"]) != "response.failed" {
			return false
		}
		response, err := codexFoldedRawObject(fields["response"])
		if err != nil {
			return false
		}
		status := strings.ToLower(strings.TrimSpace(common.JsonRawMessageToString(response["status"])))
		return status == "" || status == "failed"
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if emit() {
				return true
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	return emit()
}

func codexFoldedHTTPStatusError(statusCode int, detail string) *types.NewAPIError {
	if statusCode <= 0 {
		statusCode = http.StatusBadGateway
	}
	errorCode := types.ErrorCodeBadResponseStatusCode
	if mapped, ok := types.UpstreamHTTPStatusErrorCode(statusCode); ok {
		errorCode = mapped
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = fmt.Sprintf("Codex folded upstream returned HTTP status %d without a valid response.failed terminal", statusCode)
	}
	return types.NewOpenAIError(errors.New(detail), errorCode, statusCode)
}

func codexFoldedUsageRoundInfo(roundNo int, usage *dto.Usage, n int, hasN bool) map[string]any {
	info := map[string]any{"round": roundNo}
	if usage != nil {
		info["input_tokens"] = usage.InputTokens
		info["output_tokens"] = usage.OutputTokens
		info["total_tokens"] = usage.TotalTokens
		if info["input_tokens"] == 0 {
			info["input_tokens"] = usage.PromptTokens
		}
		if info["output_tokens"] == 0 {
			info["output_tokens"] = usage.CompletionTokens
		}
		reason, _ := codexFoldedReasoningTokens(usage)
		info["reasoning_tokens"] = reason
	}
	if hasN {
		info["n"] = n
	} else {
		info["n"] = nil
	}
	return info
}

func codexFoldedApplyResponseOutput(state *codexFoldedState, round *codexFoldedRound, clean bool) error {
	if round == nil {
		return nil
	}
	if clean {
		for _, item := range round.buffered {
			state.finalOutput = append(state.finalOutput, item.item)
		}
	}
	return nil
}

func codexFoldedProcessRound(c *gin.Context, state *codexFoldedState, resp *http.Response, roundNo int, first bool) (*codexFoldedRound, error) {
	if resp == nil || resp.Body == nil {
		return nil, errors.New("Codex folded response body is missing")
	}
	if first {
		helper.SetEventStreamHeaders(c)
		helper.CopyCodexResponsesHeaders(c, resp)
		if service.ShouldCopyUpstreamHeader(c, "X-Codex-Turn-State", []string{""}) {
			c.Writer.Header().Add("Trailer", "X-Codex-Turn-State")
		}
	}
	if turnState := strings.TrimSpace(resp.Header.Get("X-Codex-Turn-State")); turnState != "" {
		state.turnState = turnState
		if service.ShouldCopyUpstreamHeader(c, "X-Codex-Turn-State", []string{turnState}) {
			if first {
				c.Writer.Header().Set("X-Codex-Turn-State", turnState)
			} else {
				// The trailer is predeclared before the first SSE write. Updating
				// the ordinary key here changes the final trailer without altering
				// the already-sent first-round response header.
				c.Writer.Header().Set("X-Codex-Turn-State", turnState)
			}
		}
	}
	done := make(chan struct{})
	defer func() {
		_ = resp.Body.Close()
		close(done)
	}()
	resultCh := codexFoldedReadSSE(resp.Body, done)
	round := &codexFoldedRound{}
	kinds := map[int]string{}
	downstreamOutputIndex := map[int]int{}
	readTimeout := time.NewTimer(codexFoldedReadTimeout)
	defer readTimeout.Stop()
	resetReadTimeout := func() {
		if !readTimeout.Stop() {
			select {
			case <-readTimeout.C:
			default:
			}
		}
		readTimeout.Reset(codexFoldedReadTimeout)
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return round, nil
		case <-readTimeout.C:
			round.stopReason = "upstream_timeout"
			return round, nil
		case result, ok := <-resultCh:
			if !ok {
				round.stopReason = "upstream_eof"
				return round, nil
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					round.stopReason = "upstream_eof"
					return round, nil
				}
				round.stopReason = "upstream_error"
				return round, result.err
			}
			event := result.event
			if event == nil {
				continue
			}
			resetReadTimeout()
			switch event.typeID {
			case "response.created", "response.in_progress":
				if event.typeID == "response.created" && roundNo == 1 {
					state.baseResponse, _ = codexFoldedRawObject(event.fields["response"])
				}
				if first && !codexFoldedSend(c, state, event) {
					return round, nil
				}
			case "response.output_item.added":
				item, ok := codexFoldedItem(event)
				if !ok {
					if !codexFoldedSend(c, state, event) {
						return round, nil
					}
					continue
				}
				outputIndex, hasOutputIndex := codexFoldedOutputIndex(event)
				if !hasOutputIndex {
					if !codexFoldedSend(c, state, event) {
						return round, nil
					}
					continue
				}
				if codexFoldedItemType(item) == "reasoning" {
					mapped := state.downstreamIndex
					state.downstreamIndex++
					kinds[outputIndex] = "reasoning"
					downstreamOutputIndex[outputIndex] = mapped
					codexFoldedSetInt(event.fields, "output_index", mapped)
					if !codexFoldedSend(c, state, event) {
						return round, nil
					}
				} else {
					kinds[outputIndex] = "buffered"
					round.buffered = append(round.buffered, codexFoldedBufferedItem{outputIndex: outputIndex, item: item, events: []*codexFoldedEvent{codexFoldedCopyEvent(event)}})
				}
			case "response.output_item.done":
				item, ok := codexFoldedItem(event)
				outputIndex, hasOutputIndex := codexFoldedOutputIndex(event)
				if !ok {
					if !codexFoldedSend(c, state, event) {
						return round, nil
					}
					continue
				}
				if !hasOutputIndex {
					if !codexFoldedSend(c, state, event) {
						return round, nil
					}
					continue
				}
				if kinds[outputIndex] == "reasoning" {
					mapped, exists := downstreamOutputIndex[outputIndex]
					if !exists {
						mapped = state.downstreamIndex
						state.downstreamIndex++
						downstreamOutputIndex[outputIndex] = mapped
					}
					codexFoldedSetInt(event.fields, "output_index", mapped)
					round.reasoning = append(round.reasoning, item)
					state.finalOutput = append(state.finalOutput, item)
					if !codexFoldedSend(c, state, event) {
						return round, nil
					}
				} else if kinds[outputIndex] == "buffered" {
					for index := range round.buffered {
						if round.buffered[index].outputIndex == outputIndex {
							round.buffered[index].item = item
							round.buffered[index].events = append(round.buffered[index].events, event)
							break
						}
					}
				} else if !codexFoldedSend(c, state, event) {
					return round, nil
				}
			case "response.completed", "response.failed", "response.incomplete":
				round.terminal = event.typeID
				round.terminalEvent = event
				response, responseErr := codexFoldedRawObject(event.fields["response"])
				if responseErr != nil {
					round.stopReason = "invalid_terminal_response"
					return round, responseErr
				}
				round.response = response
				round.usage = codexFoldedUsageFromResponse(round.response)
				return round, nil
			default:
				outputIndex, hasOutputIndex := codexFoldedOutputIndex(event)
				if !hasOutputIndex {
					if !codexFoldedSend(c, state, event) {
						return round, nil
					}
					continue
				}
				switch kinds[outputIndex] {
				case "buffered":
					for index := range round.buffered {
						if round.buffered[index].outputIndex == outputIndex {
							round.buffered[index].events = append(round.buffered[index].events, event)
							break
						}
					}
					continue
				case "reasoning":
					if mapped, exists := downstreamOutputIndex[outputIndex]; exists {
						codexFoldedSetInt(event.fields, "output_index", mapped)
					}
				}
				if !codexFoldedSend(c, state, event) {
					return round, nil
				}
			}
		}
	}
}

func codexFoldedOutputIndex(event *codexFoldedEvent) (int, bool) {
	if event == nil {
		return 0, false
	}
	return codexFoldedFieldInt(event.fields, "output_index")
}

func codexFoldedFlushBuffered(c *gin.Context, state *codexFoldedState, buffered []codexFoldedBufferedItem) bool {
	for _, item := range buffered {
		mapped := state.downstreamIndex
		state.downstreamIndex++
		for _, event := range item.events {
			clone := codexFoldedCopyEvent(event)
			if clone == nil {
				continue
			}
			if _, ok := clone.fields["output_index"]; ok {
				codexFoldedSetInt(clone.fields, "output_index", mapped)
			}
			if !codexFoldedSend(c, state, clone) {
				return false
			}
		}
	}
	return true
}

func runCodexFoldedSSE(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, adaptor *Adaptor) (*dto.Usage, *types.NewAPIError) {
	if !codexFoldedEnabled(info) || adaptor == nil {
		return nil, types.NewError(errors.New("Codex folded SSE is not enabled"), types.ErrorCodeInvalidRequest)
	}
	if len(info.CodexFoldedBaseRequestBody) == 0 {
		return nil, types.NewError(errors.New("Codex folded SSE base request body is unavailable"), types.ErrorCodeBadResponse)
	}
	state := &codexFoldedState{isCompaction: codexFoldedRequestHasCompactionTrigger(info.CodexFoldedBaseRequestBody)}
	defer func() {
		common.SetContextKey(c, constant.ContextKeyResponsesProxyBilledUsage, state.billed)
	}()
	if info.StreamStatus == nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
	}
	if err := codexFoldedOpenResponseError(c, resp); err != nil {
		codexFoldedMarkAbnormalEnd(info, "upstream_error")
		var apiErr *types.NewAPIError
		if errors.As(err, &apiErr) && apiErr != nil {
			return nil, apiErr
		}
		statusCode := http.StatusBadGateway
		if resp != nil {
			statusCode = resp.StatusCode
		}
		return nil, codexFoldedHTTPStatusError(statusCode, err.Error())
	}
	for roundNo := 1; roundNo <= codexFoldedMaxContinuation+1; roundNo++ {
		round, parseErr := codexFoldedProcessRound(c, state, resp, roundNo, roundNo == 1)
		if round == nil {
			round = &codexFoldedRound{}
		}
		if round.usage != nil {
			if state.firstUsage == nil {
				copyUsage := *round.usage
				state.firstUsage = &copyUsage
			}
			codexFoldedMergeUsage(&state.billed, round.usage)
			reasoning, _ := codexFoldedReasoningTokens(round.usage)
			state.foldedReasoning += reasoning
			n, hasN := codexFoldedTier(reasoning)
			state.rounds = append(state.rounds, codexFoldedUsageRoundInfo(roundNo, round.usage, n, hasN))
		} else {
			state.rounds = append(state.rounds, codexFoldedUsageRoundInfo(roundNo, nil, 0, false))
		}
		if parseErr != nil {
			if c.Request.Context().Err() != nil {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
				common.SetContextKey(c, constant.ContextKeyResponsesProxyBilledUsage, state.billed)
				return &state.billed, nil
			}
			if resp != nil && resp.StatusCode >= http.StatusMultipleChoices && codexFoldedIsSSEResponse(resp) {
				codexFoldedMarkAbnormalEnd(info, "upstream_error")
				return nil, codexFoldedHTTPStatusError(resp.StatusCode, "Codex folded upstream SSE ended with an invalid event")
			}
			reason := round.stopReason
			if reason == "" {
				reason = "upstream_error"
			}
			codexFoldedMarkAbnormalEnd(info, reason)
			incomplete, err := codexFoldedBuildIncompleteResponse(state, reason)
			if err == nil {
				_ = codexFoldedSend(c, state, incomplete)
			}
			return &state.billed, nil
		}
		if c.Request.Context().Err() != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
			common.SetContextKey(c, constant.ContextKeyResponsesProxyBilledUsage, state.billed)
			return &state.billed, nil
		}
		if resp != nil && resp.StatusCode >= http.StatusMultipleChoices && codexFoldedIsSSEResponse(resp) && round.terminal != "response.failed" {
			codexFoldedMarkAbnormalEnd(info, "upstream_error")
			return nil, codexFoldedHTTPStatusError(resp.StatusCode, "Codex folded upstream SSE did not provide a valid response.failed terminal")
		}
		reasoning, _ := codexFoldedReasoningTokens(round.usage)
		_, hasN := codexFoldedTier(reasoning)
		lastReasoningHasEncrypted := len(round.reasoning) > 0 && codexFoldedHasEncryptedContent(round.reasoning[len(round.reasoning)-1])
		zeroStall := roundNo >= 2 && round.usage != nil && reasoning == 0
		shouldContinue := !state.isCompaction && round.terminal == "response.completed" &&
			roundNo <= codexFoldedMaxContinuation &&
			((hasN && lastReasoningHasEncrypted) || zeroStall)
		if state.isCompaction {
			round.stopReason = "compaction_request"
		} else if hasN && !lastReasoningHasEncrypted {
			round.stopReason = "no_encrypted_content"
		} else if round.terminal == "response.completed" && (hasN || zeroStall) && roundNo > codexFoldedMaxContinuation {
			round.stopReason = "max_continue"
		}
		if shouldContinue {
			state.replayTail = append(state.replayTail, round.reasoning...)
			state.replayTail = append(state.replayTail, codexFoldedBuildNudge())
			body, err := codexFoldedBuildContinuationBody(info.CodexFoldedBaseRequestBody, state.replayTail)
			if err != nil {
				codexFoldedMarkAbnormalEnd(info, "upstream_error")
				incomplete, _ := codexFoldedBuildIncompleteResponse(state, "upstream_error")
				_ = codexFoldedSend(c, state, incomplete)
				return &state.billed, nil
			}
			resp, err = codexFoldedOpenContinuation(c, info, adaptor, body, state.turnState)
			if err != nil {
				codexFoldedMarkAbnormalEnd(info, "upstream_error")
				incomplete, _ := codexFoldedBuildIncompleteResponse(state, "upstream_error")
				_ = codexFoldedSend(c, state, incomplete)
				return &state.billed, nil
			}
			if err := codexFoldedOpenResponseError(c, resp); err != nil {
				if codexFoldedIsSSEResponse(resp) {
					codexFoldedMarkAbnormalEnd(info, "upstream_error")
					var apiErr *types.NewAPIError
					if errors.As(err, &apiErr) && apiErr != nil {
						return &state.billed, apiErr
					}
					return &state.billed, codexFoldedHTTPStatusError(resp.StatusCode, err.Error())
				}
				codexFoldedMarkAbnormalEnd(info, "upstream_error")
				incomplete, _ := codexFoldedBuildIncompleteResponse(state, "upstream_error")
				_ = codexFoldedSend(c, state, incomplete)
				return &state.billed, nil
			}
			continue
		}
		if round.terminal == "" {
			reason := round.stopReason
			if reason == "" {
				reason = "upstream_eof"
			}
			codexFoldedMarkAbnormalEnd(info, reason)
			incomplete, _ := codexFoldedBuildIncompleteResponse(state, reason)
			_ = codexFoldedSend(c, state, incomplete)
			return &state.billed, nil
		}
		if round.terminal == "response.failed" && round.terminalEvent != nil {
			codexFoldedMarkAbnormalEnd(info, "upstream_error")
			if !codexFoldedSend(c, state, round.terminalEvent) {
				return &state.billed, nil
			}
			common.SetContextKey(c, constant.ContextKeyResponsesProxyBilledUsage, state.billed)
			return &state.billed, nil
		}
		if round.terminal != "response.completed" && round.terminal != "response.incomplete" {
			codexFoldedMarkAbnormalEnd(info, "upstream_error")
			incomplete, _ := codexFoldedBuildIncompleteResponse(state, "upstream_error")
			_ = codexFoldedSend(c, state, incomplete)
			return &state.billed, nil
		}
		if !codexFoldedFlushBuffered(c, state, round.buffered) {
			return &state.billed, nil
		}
		finalOutputTokens := 0
		if round.usage != nil {
			finalOutputTokens = round.usage.OutputTokens
			if finalOutputTokens == 0 {
				finalOutputTokens = round.usage.CompletionTokens
			}
		}
		state.foldedCleanOutput = finalOutputTokens - reasoning
		if state.foldedCleanOutput < 0 {
			state.foldedCleanOutput = 0
		}
		_ = codexFoldedApplyResponseOutput(state, round, true)
		response := codexFoldedReconstructResponse(state, codexFoldedRoundResponse(round))
		output, _ := codexFoldedRawArray(state.finalOutput)
		response["output"] = output
		_ = codexFoldedBuildUsage(response, state, true)
		_ = codexFoldedAddMetadata(response, state, round.stopReason)
		terminalType := round.terminal
		if terminalType == "" {
			terminalType = "response.completed"
		}
		terminalFields := map[string]common.RawMessage{"type": mustMarshalRaw(terminalType), "response": mustMarshalRaw(response)}
		if round.terminalEvent != nil {
			for key, value := range round.terminalEvent.fields {
				if key != "type" && key != "response" && key != "sequence_number" {
					terminalFields[key] = append(common.RawMessage(nil), value...)
				}
			}
		}
		terminal := &codexFoldedEvent{typeID: terminalType, fields: terminalFields}
		_ = codexFoldedSend(c, state, terminal)
		if terminalType == "response.completed" || terminalType == "response.incomplete" {
			codexFoldedMarkTerminalEnd(info, round.stopReason)
		} else {
			codexFoldedMarkAbnormalEnd(info, "upstream_error")
		}
		common.SetContextKey(c, constant.ContextKeyResponsesProxyBilledUsage, state.billed)
		return &state.billed, nil
	}
	incomplete, _ := codexFoldedBuildIncompleteResponse(state, "max_continue")
	_ = codexFoldedSend(c, state, incomplete)
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, nil)
	return &state.billed, nil
}
