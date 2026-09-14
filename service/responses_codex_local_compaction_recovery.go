package service

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

type CodexLocalCompaction413RecoveryStage string

const (
	CodexLocalCompaction413RecoveryStageRichVisible    CodexLocalCompaction413RecoveryStage = "rich_visible"
	CodexLocalCompaction413RecoveryStageBoundedVisible CodexLocalCompaction413RecoveryStage = "bounded_visible"
)

const (
	codexLocalCompaction413RichVisibleMaxBytes = 16 * 1024 * 1024
	// Keep a large margin below the smallest known Sub2API request-body limit.
	// The final serialized body is checked again after channel conversion.
	CodexLocalCompaction413RecoverySafeBodyMaxBytes = 32 * 1024 * 1024
	codexLocalCompaction413RecoveryRawFieldMaxBytes = 256 * 1024
	codexLocalCompaction413RecoveryExtraMaxBytes    = 64 * 1024
)

type CodexLocalCompaction413RecoveryInfo struct {
	OriginalRequestBytes  int64
	OriginalInputBytes    int
	RecoveredInputBytes   int
	RecoveredRequestBytes int
	VisiblePartCount      int
	Stage                 CodexLocalCompaction413RecoveryStage
	InstructionsPreserved bool
	VisibleInputTruncated bool
	EmbeddedMediaOmitted  bool
	ToolPayloadOmitted    bool
}

func BuildCodexLocalCompaction413RecoveryRequest(request dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, CodexLocalCompaction413RecoveryInfo, error) {
	return BuildCodexLocalCompaction413RecoveryRequestForStage(request, CodexLocalCompaction413RecoveryStageBoundedVisible)
}

func BuildCodexLocalCompaction413RecoveryRequestForStage(request dto.OpenAIResponsesRequest, stage CodexLocalCompaction413RecoveryStage) (dto.OpenAIResponsesRequest, CodexLocalCompaction413RecoveryInfo, error) {
	hasOpaqueEncryptedContent, err := ResponsesInputHasOpaqueEncryptedContent(context.Background(), request.Input)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, err
	}
	if hasOpaqueEncryptedContent {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, ErrCodexLocalCompactionOpaqueEncryptedReplay
	}
	visibleMaxBytes, err := codexLocalCompaction413VisibleMaxBytes(stage)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, err
	}
	visibleParts := visibleResponsesInputPartsWithLimit(request.Input, visibleMaxBytes)
	return buildCodexLocalCompaction413RecoveryRequestFromVisibleParts(request, visibleParts, len(request.Input), stage)
}

func buildCodexLocalCompaction413RecoveryRequestFromVisibleParts(request dto.OpenAIResponsesRequest, visibleParts []string, originalInputBytes int, stage CodexLocalCompaction413RecoveryStage) (dto.OpenAIResponsesRequest, CodexLocalCompaction413RecoveryInfo, error) {
	if len(visibleParts) == 0 {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, fmt.Errorf("codex local compaction recovery requires visible input")
	}

	visibleText := strings.TrimSpace(strings.Join(visibleParts, "\n"))
	if visibleText == "" {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, fmt.Errorf("codex local compaction recovery requires visible text")
	}
	message, err := responseMessageInput("user", visibleText)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, fmt.Errorf("build codex local compaction recovery input: %w", err)
	}
	input, err := common.Marshal([]common.RawMessage{message})
	if err != nil {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, fmt.Errorf("wrap codex local compaction recovery input: %w", err)
	}

	stream := true
	if request.Stream != nil {
		stream = *request.Stream
	}
	if !stream {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, fmt.Errorf("codex local compaction recovery requires a streaming request")
	}

	instructions := boundedCodexCompactionRawField(request.Instructions)
	instructionsPreserved := len(instructions) > 0
	if !instructionsPreserved {
		instructions, err = common.Marshal(syntheticCompactSummaryPrompt)
		if err != nil {
			return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, fmt.Errorf("marshal codex local compaction recovery instructions: %w", err)
		}
	}
	streamOptions := cloneCodexCompactionStreamOptions(request.StreamOptions)
	recovered := dto.OpenAIResponsesRequest{
		Model:            request.Model,
		Input:            input,
		Include:          boundedCodexCompactionRawField(request.Include),
		Instructions:     instructions,
		MaxOutputTokens:  request.MaxOutputTokens,
		TopLogProbs:      request.TopLogProbs,
		Metadata:         boundedCodexCompactionRawField(request.Metadata),
		Reasoning:        cloneCodexCompactionReasoning(request.Reasoning),
		ServiceTier:      request.ServiceTier,
		Store:            boundedCodexCompactionRawField(request.Store),
		Stream:           common.GetPointer(true),
		StreamOptions:    streamOptions,
		Temperature:      request.Temperature,
		Text:             boundedCodexCompactionRawField(request.Text),
		TopP:             request.TopP,
		Truncation:       boundedCodexCompactionRawField(request.Truncation),
		SafetyIdentifier: boundedCodexCompactionRawField(request.SafetyIdentifier),
		Extra:            boundedCodexCompactionRequestExtraFields(request.Extra, codexLocalCompaction413RecoveryExtraMaxBytes),
	}
	recoveredBody, err := common.Marshal(recovered)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, CodexLocalCompaction413RecoveryInfo{}, fmt.Errorf("marshal codex local compaction recovery request: %w", err)
	}

	return recovered, CodexLocalCompaction413RecoveryInfo{
		OriginalInputBytes:    originalInputBytes,
		RecoveredInputBytes:   len(input),
		RecoveredRequestBytes: len(recoveredBody),
		VisiblePartCount:      len(visibleParts),
		Stage:                 stage,
		InstructionsPreserved: instructionsPreserved,
		VisibleInputTruncated: strings.Contains(visibleText, "[truncated earlier visible input]"),
		EmbeddedMediaOmitted:  strings.Contains(visibleText, syntheticCompactEmbeddedMediaOmission),
		ToolPayloadOmitted: strings.Contains(visibleText, syntheticCompactLargeToolPayloadOmission) ||
			strings.Contains(visibleText, syntheticCompactLargeInputItemOmission),
	}, nil
}

func codexLocalCompaction413VisibleMaxBytes(stage CodexLocalCompaction413RecoveryStage) (int, error) {
	switch stage {
	case CodexLocalCompaction413RecoveryStageRichVisible:
		return codexLocalCompaction413RichVisibleMaxBytes, nil
	case CodexLocalCompaction413RecoveryStageBoundedVisible:
		return syntheticCompactVisibleTextMax, nil
	default:
		return 0, fmt.Errorf("unsupported codex local compaction recovery stage %q", stage)
	}
}

func boundedCodexCompactionRawField(value common.RawMessage) common.RawMessage {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || len(trimmed) > codexLocalCompaction413RecoveryRawFieldMaxBytes {
		return nil
	}
	var decoded any
	if err := common.Unmarshal(trimmed, &decoded); err != nil {
		return nil
	}
	return append(common.RawMessage(nil), trimmed...)
}

func boundedCodexCompactionRawFields(values map[string]common.RawMessage, maxBytes int) map[string]common.RawMessage {
	if len(values) == 0 || maxBytes <= 0 {
		return nil
	}
	result := make(map[string]common.RawMessage)
	used := 0
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		bounded := boundedCodexCompactionRawField(value)
		if len(bounded) == 0 || used+len(key)+len(bounded) > maxBytes {
			continue
		}
		result[key] = bounded
		used += len(key) + len(bounded)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func boundedCodexCompactionRequestExtraFields(values map[string]common.RawMessage, maxBytes int) map[string]common.RawMessage {
	if len(values) == 0 || maxBytes <= 0 {
		return nil
	}
	filtered := make(map[string]common.RawMessage, len(values))
	for key, value := range values {
		// A reduced compaction request must not replay tool definitions.
		if key == "additional_tools" {
			continue
		}
		filtered[key] = value
	}
	return boundedCodexCompactionRawFields(filtered, maxBytes)
}

func cloneCodexCompactionReasoning(value *dto.Reasoning) *dto.Reasoning {
	if value == nil {
		return nil
	}
	cloned := value.Clone()
	cloned.Mode = boundedCodexCompactionRawField(cloned.Mode)
	cloned.Context = boundedCodexCompactionRawField(cloned.Context)
	cloned.Extra = boundedCodexCompactionRawFields(cloned.Extra, codexLocalCompaction413RecoveryExtraMaxBytes)
	return cloned
}

func cloneCodexCompactionStreamOptions(value *dto.StreamOptions) *dto.StreamOptions {
	if value == nil {
		return nil
	}
	raw, err := common.Marshal(value)
	if err == nil && len(raw) <= codexLocalCompaction413RecoveryRawFieldMaxBytes {
		var cloned dto.StreamOptions
		if err := common.Unmarshal(raw, &cloned); err == nil {
			cloned.Extra = boundedCodexCompactionRawFields(cloned.Extra, codexLocalCompaction413RecoveryExtraMaxBytes)
			return &cloned
		}
	}
	cloned := value.Clone()
	cloned.Extra = boundedCodexCompactionRawFields(cloned.Extra, codexLocalCompaction413RecoveryExtraMaxBytes)
	return cloned
}
