package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/go-redis/redis/v8"
)

const (
	syntheticCompactIDPrefix     = "resp_newapi_synthcmp_"
	syntheticCompactMarkerPrefix = "newapi.synthetic.compact:"
	syntheticCompactRedisPrefix  = "new-api:responses:synthetic-compact:"
	syntheticCompactTTL          = 24 * time.Hour
)

const syntheticCompactSummaryPrompt = "You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.\nInclude:\n- Current progress and key decisions made\n- Important context, constraints, or user preferences\n- What remains to be done (clear next steps)\n- Any critical data, examples, or references needed to continue\nBe concise, structured, and focused on helping the next LLM seamlessly continue the work.\nDo not invent facts. Return only the compact summary text."
const syntheticCompactPreviousResponsePrompt = "Use the existing previous_response_id context as the source of truth for the compaction. Create the handoff summary from the conversation available in that response chain. Return only the compact summary text."
const syntheticCompactResumeDirective = "Another language model produced the compact summary above. Use it to build on the work that has already been done and avoid duplicating work. If post-compact input is only repeated setup or repository instructions from the client, treat it as background and continue the latest pending task from the summary. If post-compact input contains a new explicit user request, answer that request using the summary as context."

type SyntheticCompactState struct {
	ID          string `json:"id"`
	Model       string `json:"model"`
	Summary     string `json:"summary"`
	UserID      int    `json:"user_id,omitempty"`
	TokenID     int    `json:"token_id,omitempty"`
	Group       string `json:"group,omitempty"`
	ChannelID   int    `json:"channel_id,omitempty"`
	ChannelType int    `json:"channel_type,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

type SyntheticCompactStateScope = types.SyntheticCompactStateScope

type SyntheticCompactScopeSource interface {
	SyntheticCompactScope() SyntheticCompactStateScope
}

var syntheticCompactMemoryStore sync.Map
var syntheticCompactMemoryJanitorOnce sync.Once

var (
	ErrSyntheticCompactStateNotFound        = errors.New("synthetic compact state not found or expired")
	ErrSyntheticCompactRequiresVisibleInput = errors.New("synthetic compact requires visible input or a stored synthetic summary")
	ErrSyntheticCompactStateScopeMismatch   = errors.New("synthetic compact state scope mismatch")
	ErrSyntheticCompactMultipleMarkers      = errors.New("synthetic compact request contains multiple markers")
)

type syntheticCompactMemoryEntry struct {
	state     SyntheticCompactState
	expiresAt time.Time
}

func resetSyntheticCompactMemoryStoreForTest() {
	syntheticCompactMemoryStore.Range(func(key, _ any) bool {
		syntheticCompactMemoryStore.Delete(key)
		return true
	})
}

func startSyntheticCompactMemoryJanitor() {
	syntheticCompactMemoryJanitorOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				pruneExpiredSyntheticCompactMemory(time.Now())
			}
		}()
	})
}

func pruneExpiredSyntheticCompactMemory(now time.Time) {
	syntheticCompactMemoryStore.Range(func(key, value any) bool {
		entry, ok := value.(syntheticCompactMemoryEntry)
		if ok && !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			syntheticCompactMemoryStore.Delete(key)
		}
		return true
	})
}

func syntheticCompactMarker(id string) string {
	return syntheticCompactMarkerPrefix + strings.TrimSpace(id)
}

func syntheticCompactIDFromMarker(marker string) (string, bool) {
	marker = strings.TrimSpace(marker)
	if !strings.HasPrefix(marker, syntheticCompactMarkerPrefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(marker, syntheticCompactMarkerPrefix))
	return id, id != ""
}

func syntheticCompactRedisKey(id string) string {
	return syntheticCompactRedisPrefix + strings.TrimSpace(id)
}

func storeSyntheticCompactState(ctx context.Context, state SyntheticCompactState) error {
	state.ID = strings.TrimSpace(state.ID)
	state.Summary = strings.TrimSpace(state.Summary)
	if state.ID == "" {
		return fmt.Errorf("synthetic compact state id is required")
	}
	if state.Summary == "" {
		return fmt.Errorf("synthetic compact summary is required")
	}
	if state.CreatedAt == 0 {
		state.CreatedAt = time.Now().Unix()
	}
	data, err := common.Marshal(state)
	if err != nil {
		return err
	}
	if common.RedisEnabled && common.RDB != nil {
		if err := common.RDB.Set(ctx, syntheticCompactRedisKey(state.ID), string(data), syntheticCompactTTL).Err(); err != nil {
			return fmt.Errorf("store synthetic compact state in redis: %w", err)
		}
		return nil
	}
	startSyntheticCompactMemoryJanitor()
	syntheticCompactMemoryStore.Store(state.ID, syntheticCompactMemoryEntry{
		state:     state,
		expiresAt: time.Now().Add(syntheticCompactTTL),
	})
	return nil
}

func loadSyntheticCompactState(ctx context.Context, id string) (*SyntheticCompactState, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, false, nil
	}
	if common.RedisEnabled && common.RDB != nil {
		raw, err := common.RDB.Get(ctx, syntheticCompactRedisKey(id)).Result()
		if err == nil {
			var state SyntheticCompactState
			if err := common.UnmarshalJsonStr(raw, &state); err != nil {
				return nil, false, err
			}
			return &state, true, nil
		}
		if !errors.Is(err, redis.Nil) {
			return nil, false, fmt.Errorf("load synthetic compact state from redis: %w", err)
		}
		return nil, false, nil
	}
	if value, ok := syntheticCompactMemoryStore.Load(id); ok {
		switch entry := value.(type) {
		case syntheticCompactMemoryEntry:
			if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
				syntheticCompactMemoryStore.Delete(id)
				return nil, false, nil
			}
			state := entry.state
			return &state, true, nil
		case SyntheticCompactState:
			state := entry
			return &state, true, nil
		}
	}
	return nil, false, nil
}

func findSyntheticCompactState(ctx context.Context, req dto.OpenAIResponsesRequest) (*SyntheticCompactState, bool, error) {
	if state, ok, err := loadSyntheticCompactState(ctx, req.PreviousResponseID); err != nil || ok {
		return state, ok, err
	}
	markerIDs := make([]string, 0, 1)
	for _, marker := range syntheticCompactMarkers(req.Input) {
		id, ok := syntheticCompactIDFromMarker(marker)
		if !ok {
			continue
		}
		markerIDs = append(markerIDs, id)
	}
	if len(markerIDs) > 1 {
		return nil, false, ErrSyntheticCompactMultipleMarkers
	}
	if len(markerIDs) == 1 {
		return loadSyntheticCompactState(ctx, markerIDs[0])
	}
	return nil, false, nil
}

func HasSyntheticCompactReference(req dto.OpenAIResponsesRequest) bool {
	if strings.HasPrefix(strings.TrimSpace(req.PreviousResponseID), syntheticCompactIDPrefix) {
		return true
	}
	for _, marker := range syntheticCompactMarkers(req.Input) {
		if _, ok := syntheticCompactIDFromMarker(marker); ok {
			return true
		}
	}
	return false
}

func SyntheticCompactScopeFromSource(source SyntheticCompactScopeSource) SyntheticCompactStateScope {
	if source == nil {
		return SyntheticCompactStateScope{}
	}
	return source.SyntheticCompactScope()
}

func syntheticCompactMarkers(input common.RawMessage) []string {
	if common.GetJsonType(input) != "array" {
		return nil
	}
	var items []common.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		return nil
	}
	markers := make([]string, 0, 1)
	for _, rawItem := range items {
		item, ok := responsesInputObject(rawItem)
		if !ok {
			continue
		}
		if rawStringField(item["type"]) != "compaction" {
			continue
		}
		if marker := rawStringField(item["encrypted_content"]); marker != "" {
			markers = append(markers, marker)
		}
	}
	return markers
}

func BuildSyntheticCompactSummaryRequest(ctx context.Context, scope SyntheticCompactStateScope, req dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, error) {
	state, found, err := findSyntheticCompactState(ctx, req)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}
	if found {
		if err := validateSyntheticCompactState(scope, req.Model, state); err != nil {
			return dto.OpenAIResponsesRequest{}, err
		}
	}
	if !found && HasSyntheticCompactReference(req) {
		return dto.OpenAIResponsesRequest{}, ErrSyntheticCompactStateNotFound
	}
	if !found {
		if previousResponseID := strings.TrimSpace(req.PreviousResponseID); previousResponseID != "" {
			return buildSyntheticCompactPreviousResponseRequest(req.Model, previousResponseID)
		}
	}
	cleanInput := removeSyntheticCompactMarkers(req.Input)
	visibleParts := visibleResponsesInputParts(cleanInput)
	if len(visibleParts) == 0 && (!found || strings.TrimSpace(state.Summary) == "") {
		return dto.OpenAIResponsesRequest{}, ErrSyntheticCompactRequiresVisibleInput
	}

	userText := buildSyntheticCompactSummaryUserText(visibleParts, state)
	input, err := syntheticCompactPromptInput(syntheticCompactSummaryPrompt, userText)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}

	out := dto.OpenAIResponsesRequest{
		Model: req.Model,
		Input: input,
	}
	return out, nil
}

func buildSyntheticCompactPreviousResponseRequest(model string, previousResponseID string) (dto.OpenAIResponsesRequest, error) {
	input, err := syntheticCompactPromptInput(syntheticCompactSummaryPrompt, syntheticCompactPreviousResponsePrompt)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}
	return dto.OpenAIResponsesRequest{
		Model:              model,
		PreviousResponseID: previousResponseID,
		Input:              input,
	}, nil
}

func ApplySyntheticCompactState(ctx context.Context, scope SyntheticCompactStateScope, req dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, bool, error) {
	state, found, err := findSyntheticCompactState(ctx, req)
	if err != nil || !found {
		if err == nil && !found && HasSyntheticCompactReference(req) {
			err = ErrSyntheticCompactStateNotFound
		}
		return req, found, err
	}
	if err := validateSyntheticCompactState(scope, req.Model, state); err != nil {
		return dto.OpenAIResponsesRequest{}, true, err
	}
	cleanInput := removeSyntheticCompactMarkers(req.Input)
	summaryText := syntheticCompactRecoveredSummaryText(state.Summary)
	summaryItem, err := responseMessageInput("developer", summaryText)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, false, err
	}
	items := []common.RawMessage{summaryItem}
	items = append(items, normalizeResponsesInputItems(cleanInput)...)
	resumeItem, err := responseMessageInput("developer", syntheticCompactResumeDirective)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, false, err
	}
	items = append(items, resumeItem)
	input, err := common.Marshal(items)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, false, err
	}
	req.Input = input
	req.PreviousResponseID = ""
	return req, true, nil
}

func syntheticCompactRecoveredSummaryText(summary string) string {
	return "Another language model started to solve this problem and produced a compact handoff summary. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary produced by the other language model, use the information in this summary to assist with your own analysis:\n\n" + strings.TrimSpace(summary)
}

func BuildSyntheticCompactResponse(ctx context.Context, scope SyntheticCompactStateScope, model string, upstream dto.OpenAIResponsesResponse) (*dto.OpenAIResponsesCompactionResponse, *dto.Usage, error) {
	summary := strings.TrimSpace(responsesOutputText(upstream.Output))
	if summary == "" {
		return nil, nil, fmt.Errorf("synthetic compact upstream response has no summary text")
	}
	id := syntheticCompactIDPrefix + common.GetUUID()
	createdAt := int64(upstream.CreatedAt)
	if createdAt == 0 {
		createdAt = time.Now().Unix()
	}
	state := SyntheticCompactState{
		ID:          id,
		Model:       model,
		Summary:     summary,
		UserID:      scope.UserID,
		TokenID:     scope.TokenID,
		Group:       strings.TrimSpace(scope.Group),
		ChannelID:   scope.ChannelID,
		ChannelType: scope.ChannelType,
		CreatedAt:   createdAt,
	}
	if err := storeSyntheticCompactState(ctx, state); err != nil {
		return nil, nil, err
	}
	output, err := common.Marshal([]map[string]string{
		{
			"type":              "compaction",
			"encrypted_content": syntheticCompactMarker(id),
		},
	})
	if err != nil {
		return nil, nil, err
	}
	usage := syntheticCompactUsage(upstream.Usage)
	return &dto.OpenAIResponsesCompactionResponse{
		ID:        id,
		Object:    "response",
		CreatedAt: int(createdAt),
		Output:    output,
		Usage:     usage,
	}, usage, nil
}

func validateSyntheticCompactState(scope SyntheticCompactStateScope, model string, state *SyntheticCompactState) error {
	if state == nil {
		return nil
	}
	// ChannelID and ChannelType are intentionally not part of the reuse boundary.
	// Synthetic compact state may be restored on another compatible channel after routing changes.
	if state.UserID != 0 && scope.UserID != 0 && state.UserID != scope.UserID {
		return fmt.Errorf("%w: synthetic compact state belongs to a different user", ErrSyntheticCompactStateScopeMismatch)
	}
	if state.TokenID != 0 && scope.TokenID != 0 && state.TokenID != scope.TokenID {
		return fmt.Errorf("%w: synthetic compact state belongs to a different token", ErrSyntheticCompactStateScopeMismatch)
	}
	if state.Group != "" && scope.Group != "" && state.Group != strings.TrimSpace(scope.Group) {
		return fmt.Errorf("%w: synthetic compact state belongs to a different group", ErrSyntheticCompactStateScopeMismatch)
	}
	if !syntheticCompactModelCompatible(state.Model, model) {
		return fmt.Errorf("%w: synthetic compact state belongs to a different model", ErrSyntheticCompactStateScopeMismatch)
	}
	return nil
}

func syntheticCompactModelCompatible(storedModel string, requestModel string) bool {
	storedModel = strings.TrimSpace(storedModel)
	requestModel = strings.TrimSpace(requestModel)
	if storedModel == "" || requestModel == "" || storedModel == requestModel {
		return true
	}
	return syntheticCompactBaseModel(storedModel) == syntheticCompactBaseModel(requestModel)
}

func syntheticCompactBaseModel(model string) string {
	baseModel, _ := ratio_setting.CompactBaseModelName(strings.TrimSpace(model))
	return baseModel
}

func syntheticCompactUsage(usage *dto.Usage) *dto.Usage {
	if usage == nil {
		return &dto.Usage{}
	}
	copied := *usage
	if copied.PromptTokens == 0 {
		copied.PromptTokens = copied.InputTokens
	}
	if copied.CompletionTokens == 0 {
		copied.CompletionTokens = copied.OutputTokens
	}
	if copied.TotalTokens == 0 {
		copied.TotalTokens = copied.PromptTokens + copied.CompletionTokens
	}
	if copied.InputTokens == 0 {
		copied.InputTokens = copied.PromptTokens
	}
	if copied.OutputTokens == 0 {
		copied.OutputTokens = copied.CompletionTokens
	}
	return &copied
}

func responsesOutputText(outputs []dto.ResponsesOutput) string {
	texts := make([]string, 0, len(outputs))
	for _, output := range outputs {
		for _, content := range output.Content {
			if content.Type != "output_text" {
				continue
			}
			if text := strings.TrimSpace(content.Text); text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func buildSyntheticCompactSummaryUserText(visibleParts []string, state *SyntheticCompactState) string {
	sections := make([]string, 0, 2)
	if state != nil && strings.TrimSpace(state.Summary) != "" {
		sections = append(sections, "Previous synthetic summary:\n"+strings.TrimSpace(state.Summary))
	}
	if len(visibleParts) > 0 {
		sections = append(sections, "Visible conversation to compact:\n"+strings.Join(visibleParts, "\n"))
	}
	return strings.Join(sections, "\n\n")
}

func syntheticCompactPromptInput(systemText string, userText string) (common.RawMessage, error) {
	systemItem, err := responseMessageInput("developer", systemText)
	if err != nil {
		return nil, err
	}
	userItem, err := responseMessageInput("user", userText)
	if err != nil {
		return nil, err
	}
	return common.Marshal([]common.RawMessage{systemItem, userItem})
}

func responseMessageInput(role string, text string) (common.RawMessage, error) {
	content := []map[string]string{
		{
			"type": "input_text",
			"text": text,
		},
	}
	item := map[string]any{
		"type":    "message",
		"role":    role,
		"content": content,
	}
	return common.Marshal(item)
}

func removeSyntheticCompactMarkers(input common.RawMessage) common.RawMessage {
	if common.GetJsonType(input) != "array" {
		return input
	}
	var items []common.RawMessage
	if err := common.Unmarshal(input, &items); err != nil {
		return input
	}
	cleaned := make([]common.RawMessage, 0, len(items))
	for _, rawItem := range items {
		item, ok := responsesInputObject(rawItem)
		if !ok {
			cleaned = append(cleaned, rawItem)
			continue
		}
		if rawStringField(item["type"]) == "compaction" {
			if marker := rawStringField(item["encrypted_content"]); marker != "" {
				if _, ok := syntheticCompactIDFromMarker(marker); ok {
					continue
				}
			}
		}
		cleaned = append(cleaned, rawItem)
	}
	data, err := common.Marshal(cleaned)
	if err != nil {
		return input
	}
	return data
}

func normalizeResponsesInputItems(input common.RawMessage) []common.RawMessage {
	switch common.GetJsonType(input) {
	case "array":
		var items []common.RawMessage
		if err := common.Unmarshal(input, &items); err != nil {
			return nil
		}
		normalized := make([]common.RawMessage, 0, len(items))
		for _, rawItem := range items {
			if normalizedItem, ok := normalizeResponsesInputItem(rawItem); ok {
				normalized = append(normalized, normalizedItem)
			}
		}
		return normalized
	case "string":
		text := rawStringField(input)
		if text != "" {
			item, err := responseMessageInput("user", text)
			if err == nil {
				return []common.RawMessage{item}
			}
		}
	}
	return nil
}

func normalizeResponsesInputItem(rawItem common.RawMessage) (common.RawMessage, bool) {
	switch common.GetJsonType(rawItem) {
	case "object":
		return rawItem, true
	case "string":
		text := strings.TrimSpace(rawStringField(rawItem))
		if text == "" {
			return nil, false
		}
		item, err := responseMessageInput("user", text)
		if err != nil {
			return nil, false
		}
		return item, true
	default:
		return nil, false
	}
}

func visibleResponsesInputParts(input common.RawMessage) []string {
	switch common.GetJsonType(input) {
	case "string":
		text := rawStringField(input)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{"[input] " + strings.TrimSpace(text)}
	case "array":
		var items []common.RawMessage
		if err := common.Unmarshal(input, &items); err != nil {
			return nil
		}
		parts := make([]string, 0, len(items))
		for _, rawItem := range items {
			if text := rawVisibleResponsesInputItem(rawItem); text != "" {
				parts = append(parts, text)
			}
		}
		return parts
	default:
		return nil
	}
}

func rawVisibleResponsesInputItem(rawItem common.RawMessage) string {
	switch common.GetJsonType(rawItem) {
	case "string":
		text := strings.TrimSpace(rawStringField(rawItem))
		if text == "" {
			return ""
		}
		return "[input] " + text
	case "object":
		item, ok := responsesInputObject(rawItem)
		if !ok || rawStringField(item["type"]) == "compaction" {
			return ""
		}
		text := visibleResponsesItemText(item)
		if text == "" {
			return ""
		}
		label := rawStringField(item["role"])
		if label == "" {
			label = rawStringField(item["type"])
		}
		if label == "" {
			label = "input"
		}
		return fmt.Sprintf("[%s] %s", label, text)
	default:
		return ""
	}
}

func responsesInputObject(rawItem common.RawMessage) (map[string]common.RawMessage, bool) {
	if common.GetJsonType(rawItem) != "object" {
		return nil, false
	}
	var item map[string]common.RawMessage
	if err := common.Unmarshal(rawItem, &item); err != nil {
		return nil, false
	}
	return item, true
}

func visibleResponsesItemText(item map[string]common.RawMessage) string {
	if text := visibleResponsesToolItemText(item); text != "" {
		return text
	}
	if text := rawStringField(item["text"]); text != "" {
		return strings.TrimSpace(text)
	}
	if output := rawVisibleResponsesField(item["output"]); output != "" {
		return strings.TrimSpace(output)
	}
	if input := rawVisibleResponsesField(item["input"]); input != "" {
		return strings.TrimSpace(input)
	}
	if arguments := rawVisibleResponsesField(item["arguments"]); arguments != "" {
		return arguments
	}
	content := item["content"]
	switch common.GetJsonType(content) {
	case "string":
		return strings.TrimSpace(rawStringField(content))
	case "array":
		var parts []map[string]common.RawMessage
		if err := common.Unmarshal(content, &parts); err != nil {
			return ""
		}
		const maxVisibleContentParts = 100
		if len(parts) > maxVisibleContentParts {
			parts = parts[:maxVisibleContentParts]
		}
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			partType := rawStringField(part["type"])
			switch partType {
			case "input_text", "output_text", "text":
				if text := strings.TrimSpace(rawStringField(part["text"])); text != "" {
					texts = append(texts, text)
				}
			case "function_call", "custom_tool_call", "function_call_output":
				if text := visibleResponsesToolItemText(part); text != "" {
					texts = append(texts, text)
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		return ""
	}
}

func visibleResponsesToolItemText(item map[string]common.RawMessage) string {
	itemType := rawStringField(item["type"])
	if !strings.Contains(itemType, "_call") {
		return ""
	}
	metadata := make([]string, 0, 2)
	if name := rawStringField(item["name"]); name != "" {
		metadata = append(metadata, "name="+name)
	}
	if callID := rawStringField(item["call_id"]); callID != "" {
		metadata = append(metadata, "call_id="+callID)
	}
	payloads := make([]string, 0, 3)
	for _, field := range []string{"output", "input", "arguments"} {
		if value := rawVisibleResponsesField(item[field]); value != "" {
			payloads = append(payloads, field+"="+value)
		}
	}
	parts := append(metadata, payloads...)
	return strings.Join(parts, "\n")
}

func rawVisibleResponsesField(raw common.RawMessage) string {
	if value := rawStringField(raw); value != "" {
		return strings.TrimSpace(value)
	}
	if value := strings.TrimSpace(string(raw)); value != "" && value != "null" {
		return value
	}
	return ""
}

func rawStringField(raw common.RawMessage) string {
	if len(raw) == 0 || common.GetJsonType(raw) != "string" {
		return ""
	}
	var value string
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}
