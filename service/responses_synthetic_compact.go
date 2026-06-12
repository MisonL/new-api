package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

const (
	syntheticCompactIDPrefix               = "resp_newapi_synthcmp_"
	syntheticCompactMarkerPrefix           = "newapi.synthetic.compact:"
	syntheticCompactMarkerVersion          = "v2"
	syntheticCompactRedisPrefix            = "new-api:responses:synthetic-compact:"
	syntheticCompactTTL                    = 24 * time.Hour
	syntheticCompactStoreTimeout           = 10 * time.Second
	syntheticCompactTextPartMax            = 8 * 1024 * 1024
	syntheticCompactVisibleTextMax         = 96 * 1024
	syntheticCompactPreviousVisibleTextMax = 32 * 1024
	syntheticCompactSummaryMax             = 512 * 1024
	syntheticCompactVisibleContentPartsMax = 100
	syntheticCompactMemoryEntriesMax       = 256
	syntheticCompactLargeInputMinTokens    = 4096
	syntheticCompactLargeSummaryMinBytes   = 256
	syntheticCompactLargeSummaryMinTokens  = 48
)

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

var (
	ErrSyntheticCompactStateNotFound        = errors.New("synthetic compact state not found or expired")
	ErrSyntheticCompactRequiresVisibleInput = errors.New("synthetic compact requires visible input or a stored synthetic summary")
	ErrSyntheticCompactStateScopeMismatch   = errors.New("synthetic compact state scope mismatch")
	ErrSyntheticCompactMultipleMarkers      = errors.New("synthetic compact request contains multiple markers")
	ErrSyntheticCompactStateScopeRequired   = errors.New("synthetic compact state scope is required")
	ErrResponsesRESTPreviousIDUnsupported   = errors.New("responses REST previous_response_id is unsupported by upstream profile")
)

func newSyntheticCompactID(ctx context.Context) (string, error) {
	instanceID, err := syntheticCompactLocalInstanceID(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve synthetic compact instance id: %w", err)
	}
	return syntheticCompactIDPrefix + instanceID + "_" + common.GetUUID(), nil
}

func syntheticCompactMarker(ctx context.Context, id string) (string, error) {
	id = strings.TrimSpace(id)
	instanceID := syntheticCompactIDInstance(id)
	if instanceID == "" {
		var err error
		instanceID, err = syntheticCompactLocalInstanceID(ctx)
		if err != nil {
			return "", fmt.Errorf("resolve synthetic compact instance id: %w", err)
		}
	}
	return syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + instanceID + ":" + id, nil
}

func syntheticCompactIDInstance(id string) string {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, syntheticCompactIDPrefix) {
		return ""
	}
	rest := strings.TrimPrefix(id, syntheticCompactIDPrefix)
	instanceID, _, ok := strings.Cut(rest, "_")
	if !ok || !syntheticCompactInstanceIDValid(instanceID) {
		return ""
	}
	return instanceID
}

func syntheticCompactIDFromReference(ctx context.Context, id string) (string, bool, error) {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, syntheticCompactIDPrefix) {
		return "", false, nil
	}
	instanceID := syntheticCompactIDInstance(id)
	if instanceID == "" {
		return id, true, nil
	}
	matches, err := syntheticCompactMarkerInstanceMatches(ctx, instanceID)
	if err != nil {
		return "", false, err
	}
	if !matches {
		return "", false, nil
	}
	return id, true, nil
}

func syntheticCompactIDFromMarker(ctx context.Context, marker string) (string, bool, error) {
	marker = strings.TrimSpace(marker)
	if !strings.HasPrefix(marker, syntheticCompactMarkerPrefix) {
		return "", false, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(marker, syntheticCompactMarkerPrefix))
	version, rest, ok := strings.Cut(payload, ":")
	if ok && version == syntheticCompactMarkerVersion {
		instanceID, id, ok := strings.Cut(rest, ":")
		instanceID = strings.TrimSpace(instanceID)
		id = strings.TrimSpace(id)
		if !ok || !syntheticCompactInstanceIDValid(instanceID) || !strings.HasPrefix(id, syntheticCompactIDPrefix) {
			return "", false, nil
		}
		if idInstance := syntheticCompactIDInstance(id); idInstance == "" || idInstance != instanceID {
			return "", false, nil
		}
		matches, err := syntheticCompactMarkerInstanceMatches(ctx, instanceID)
		if err != nil {
			return "", false, err
		}
		if !matches {
			return "", false, nil
		}
		return id, true, nil
	}
	return syntheticCompactIDFromReference(ctx, payload)
}

func findSyntheticCompactState(ctx context.Context, req dto.OpenAIResponsesRequest) (*SyntheticCompactState, bool, error) {
	previousResponseID := strings.TrimSpace(req.PreviousResponseID)
	if id, local, err := syntheticCompactIDFromReference(ctx, previousResponseID); err != nil {
		return nil, false, err
	} else if local {
		if state, ok, err := loadSyntheticCompactState(ctx, id); err != nil || ok {
			return state, ok, err
		}
	}
	markerIDs := make([]string, 0, 1)
	for _, marker := range syntheticCompactMarkers(req.Input) {
		id, ok, err := syntheticCompactIDFromMarker(ctx, marker)
		if err != nil {
			return nil, false, err
		}
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

func HasSyntheticCompactReference(req dto.OpenAIResponsesRequest) (bool, error) {
	return HasLocalSyntheticCompactReferenceWithContext(context.Background(), req)
}

func HasLocalSyntheticCompactReference(req dto.OpenAIResponsesRequest) (bool, error) {
	return HasLocalSyntheticCompactReferenceWithContext(context.Background(), req)
}

func HasLocalSyntheticCompactReferenceWithContext(ctx context.Context, req dto.OpenAIResponsesRequest) (bool, error) {
	if _, ok, err := syntheticCompactIDFromReference(ctx, req.PreviousResponseID); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	for _, marker := range syntheticCompactMarkers(req.Input) {
		if _, ok, err := syntheticCompactIDFromMarker(ctx, marker); err != nil {
			return false, err
		} else if ok {
			return true, nil
		}
	}
	return false, nil
}

func SyntheticCompactScopeFromSource(source SyntheticCompactScopeSource) SyntheticCompactStateScope {
	if source == nil {
		return SyntheticCompactStateScope{}
	}
	return source.SyntheticCompactScope()
}

func forceResponsesCompactVisibleOnly(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, ok := ctx.Value(constant.ContextKeyResponsesCompactVisibleOnly).(bool)
	return ok && value
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
	hasReference, err := HasLocalSyntheticCompactReferenceWithContext(ctx, req)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}
	if !found && hasReference {
		return dto.OpenAIResponsesRequest{}, ErrSyntheticCompactStateNotFound
	}
	cleanInput, err := removeSyntheticCompactMarkers(ctx, req.Input)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}
	visibleParts := visibleResponsesInputParts(cleanInput)
	forceVisibleOnly := forceResponsesCompactVisibleOnly(ctx)
	if !found && !forceVisibleOnly {
		if previousResponseID := strings.TrimSpace(req.PreviousResponseID); previousResponseID != "" {
			return buildSyntheticCompactPreviousResponseRequest(req.Model, previousResponseID, visibleParts)
		}
	}
	if len(visibleParts) == 0 && (!found || strings.TrimSpace(state.Summary) == "") {
		return dto.OpenAIResponsesRequest{}, ErrSyntheticCompactRequiresVisibleInput
	}

	userText := buildSyntheticCompactSummaryUserText(visibleParts, state)
	input, err := syntheticCompactPromptInput(syntheticCompactSummaryPrompt, userText)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}

	model := syntheticCompactSummaryModel(req.Model)
	logSyntheticCompactSummaryRequest(req.Model, model, input, len(visibleParts), false)
	out := dto.OpenAIResponsesRequest{
		Model: model,
		Input: input,
	}
	return out, nil
}

func buildSyntheticCompactPreviousResponseRequest(model string, previousResponseID string, visibleParts []string) (dto.OpenAIResponsesRequest, error) {
	userText := syntheticCompactPreviousResponsePrompt
	if len(visibleParts) > 0 {
		userText += "\n\n" + buildSyntheticCompactSummaryUserText(limitSyntheticCompactPreviousVisibleParts(visibleParts), nil)
	}
	input, err := syntheticCompactPromptInput(syntheticCompactSummaryPrompt, userText)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}
	summaryModel := syntheticCompactSummaryModel(model)
	logSyntheticCompactSummaryRequest(model, summaryModel, input, len(visibleParts), true)
	return dto.OpenAIResponsesRequest{
		Model:              summaryModel,
		PreviousResponseID: previousResponseID,
		Input:              input,
	}, nil
}

func syntheticCompactSummaryModel(model string) string {
	baseModel, isCompact := ratio_setting.CompactBaseModelName(strings.TrimSpace(model))
	if isCompact && strings.TrimSpace(baseModel) != "" {
		return baseModel
	}
	return strings.TrimSpace(model)
}

func logSyntheticCompactSummaryRequest(originalModel string, summaryModel string, input common.RawMessage, visiblePartCount int, hasUpstreamPreviousID bool) {
	common.SysLog(fmt.Sprintf(
		"responses synthetic compact summary request prepared: original_model=%s summary_model=%s input_bytes=%d visible_parts=%d upstream_previous_response_id=%t",
		strings.TrimSpace(originalModel),
		strings.TrimSpace(summaryModel),
		len(input),
		visiblePartCount,
		hasUpstreamPreviousID,
	))
}

func ApplySyntheticCompactState(ctx context.Context, scope SyntheticCompactStateScope, req dto.OpenAIResponsesRequest) (dto.OpenAIResponsesRequest, bool, error) {
	state, found, err := findSyntheticCompactState(ctx, req)
	if err != nil || !found {
		if err == nil && !found {
			var hasReference bool
			hasReference, err = HasLocalSyntheticCompactReferenceWithContext(ctx, req)
			if err == nil && hasReference {
				err = ErrSyntheticCompactStateNotFound
			}
		}
		return req, found, err
	}
	if err := validateSyntheticCompactState(scope, req.Model, state); err != nil {
		return dto.OpenAIResponsesRequest{}, true, err
	}
	cleanInput, err := removeSyntheticCompactMarkers(ctx, req.Input)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, true, err
	}
	contextText := syntheticCompactRecoveredContextText(state.Summary)
	contextItem, err := responseMessageInput("developer", contextText)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, false, err
	}
	items := []common.RawMessage{contextItem}
	items = append(items, normalizeResponsesInputItems(cleanInput)...)
	input, err := common.Marshal(items)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, false, err
	}
	req.Input = input
	req.PreviousResponseID = ""
	return req, true, nil
}

func BuildSyntheticCompactResponse(ctx context.Context, scope SyntheticCompactStateScope, model string, upstream dto.OpenAIResponsesResponse) (*dto.OpenAIResponsesCompactionResponse, *dto.Usage, error) {
	if err := validateSyntheticCompactScopeForStore(scope); err != nil {
		return nil, nil, err
	}
	summary := strings.TrimSpace(responsesOutputText(upstream.Output))
	if summary == "" {
		return nil, nil, fmt.Errorf("synthetic compact upstream response has no summary text")
	}
	if err := validateSyntheticCompactSummaryUsable(summary, upstream.Usage); err != nil {
		return nil, nil, err
	}
	id, err := newSyntheticCompactID(ctx)
	if err != nil {
		return nil, nil, err
	}
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
	marker, err := syntheticCompactMarker(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	output, err := common.Marshal([]map[string]string{
		{
			"type":              "compaction",
			"encrypted_content": marker,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	usage := syntheticCompactUsage(upstream.Usage, model, summary)
	return &dto.OpenAIResponsesCompactionResponse{
		ID:        id,
		Object:    "response",
		CreatedAt: int(createdAt),
		Output:    output,
		Usage:     usage,
	}, usage, nil
}

func validateSyntheticCompactState(scope SyntheticCompactStateScope, _ string, state *SyntheticCompactState) error {
	if state == nil {
		return nil
	}
	// ChannelID and ChannelType are intentionally not part of the reuse boundary.
	// Synthetic compact state may be restored on another compatible channel after routing changes.
	if state.UserID != 0 && state.UserID != scope.UserID {
		return fmt.Errorf("%w: synthetic compact state belongs to a different user", ErrSyntheticCompactStateScopeMismatch)
	}
	if state.TokenID != 0 && state.TokenID != scope.TokenID {
		return fmt.Errorf("%w: synthetic compact state belongs to a different token", ErrSyntheticCompactStateScopeMismatch)
	}
	if state.Group != "" && state.Group != strings.TrimSpace(scope.Group) {
		return fmt.Errorf("%w: synthetic compact state belongs to a different group", ErrSyntheticCompactStateScopeMismatch)
	}
	return nil
}

func validateSyntheticCompactSummaryUsable(summary string, usage *dto.Usage) error {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return fmt.Errorf("synthetic compact upstream response has no summary text")
	}
	inputTokens := syntheticCompactInputTokens(usage)
	outputTokens := syntheticCompactOutputTokens(usage, inputTokens)
	if syntheticCompactSummaryLooksLikeLostTask(summary) &&
		(inputTokens >= syntheticCompactLargeInputMinTokens || len(summary) < syntheticCompactLargeSummaryMinBytes) {
		return fmt.Errorf("synthetic compact upstream response is not a recoverable handoff summary: appears to have lost the active task")
	}
	if inputTokens < syntheticCompactLargeInputMinTokens {
		return nil
	}
	if len(summary) < syntheticCompactLargeSummaryMinBytes {
		return fmt.Errorf("synthetic compact upstream response is too short for large input: summary_bytes=%d input_tokens=%d", len(summary), inputTokens)
	}
	if outputTokens > 0 && outputTokens < syntheticCompactLargeSummaryMinTokens {
		return fmt.Errorf("synthetic compact upstream response output is too short for large input: output_tokens=%d input_tokens=%d", outputTokens, inputTokens)
	}
	return nil
}

func syntheticCompactInputTokens(usage *dto.Usage) int {
	if usage == nil {
		return 0
	}
	if usage.InputTokens > 0 {
		return usage.InputTokens
	}
	if usage.PromptTokens > 0 {
		return usage.PromptTokens
	}
	outputTokens := syntheticCompactOutputTokens(usage, 0)
	if usage.TotalTokens > outputTokens {
		return usage.TotalTokens - outputTokens
	}
	return 0
}

func syntheticCompactOutputTokens(usage *dto.Usage, inputTokens int) int {
	if usage == nil {
		return 0
	}
	if usage.OutputTokens > 0 {
		return usage.OutputTokens
	}
	if usage.CompletionTokens > 0 {
		return usage.CompletionTokens
	}
	if inputTokens > 0 && usage.TotalTokens > inputTokens {
		return usage.TotalTokens - inputTokens
	}
	return 0
}

func syntheticCompactSummaryLooksLikeLostTask(summary string) bool {
	normalized := strings.ToLower(strings.TrimSpace(summary))
	for _, pattern := range []string{
		"no explicit task",
		"no clear task",
		"no actionable task",
		"no pending task",
		"no task to",
		"当前没有明确",
		"没有明确任务",
		"没有明确的开发",
		"没有可执行任务",
		"无明确任务",
	} {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}

func validateSyntheticCompactScopeForStore(scope SyntheticCompactStateScope) error {
	if scope.UserID == 0 {
		return fmt.Errorf("%w: user id is required", ErrSyntheticCompactStateScopeRequired)
	}
	if scope.TokenID == 0 {
		return fmt.Errorf("%w: token id is required", ErrSyntheticCompactStateScopeRequired)
	}
	if strings.TrimSpace(scope.Group) == "" {
		return fmt.Errorf("%w: group is required", ErrSyntheticCompactStateScopeRequired)
	}
	return nil
}

func syntheticCompactUsage(usage *dto.Usage, model string, summary string) *dto.Usage {
	if usage == nil {
		return estimatedSyntheticCompactUsage(model, summary)
	}
	copied := *usage
	if copied.PromptTokens == 0 {
		copied.PromptTokens = copied.InputTokens
	}
	if copied.CompletionTokens == 0 {
		copied.CompletionTokens = copied.OutputTokens
	}
	if copied.PromptTokens == 0 && copied.CompletionTokens == 0 && copied.TotalTokens > 0 {
		copied.CompletionTokens = copied.TotalTokens
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
	if copied.TotalTokens == 0 && strings.TrimSpace(summary) != "" {
		return estimatedSyntheticCompactUsage(model, summary)
	}
	return &copied
}

func estimatedSyntheticCompactUsage(model string, summary string) *dto.Usage {
	completionTokens := EstimateTokenByModel(model, summary)
	if completionTokens == 0 && strings.TrimSpace(summary) != "" {
		completionTokens = 1
	}
	return &dto.Usage{
		CompletionTokens: completionTokens,
		TotalTokens:      completionTokens,
		OutputTokens:     completionTokens,
	}
}
