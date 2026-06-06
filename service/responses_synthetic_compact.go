package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
)

const (
	syntheticCompactIDPrefix               = "resp_newapi_synthcmp_"
	syntheticCompactMarkerPrefix           = "newapi.synthetic.compact:"
	syntheticCompactRedisPrefix            = "new-api:responses:synthetic-compact:"
	syntheticCompactTTL                    = 24 * time.Hour
	syntheticCompactStoreTimeout           = 10 * time.Second
	syntheticCompactTextPartMax            = 8 * 1024 * 1024
	syntheticCompactSummaryMax             = 512 * 1024
	syntheticCompactVisibleContentPartsMax = 100
	syntheticCompactMemoryEntriesMax       = 256
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
)

func syntheticCompactMarker(id string) string {
	return syntheticCompactMarkerPrefix + strings.TrimSpace(id)
}

func syntheticCompactIDFromMarker(marker string) (string, bool) {
	marker = strings.TrimSpace(marker)
	if !strings.HasPrefix(marker, syntheticCompactMarkerPrefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(marker, syntheticCompactMarkerPrefix))
	return id, strings.HasPrefix(id, syntheticCompactIDPrefix)
}

func findSyntheticCompactState(ctx context.Context, req dto.OpenAIResponsesRequest) (*SyntheticCompactState, bool, error) {
	previousResponseID := strings.TrimSpace(req.PreviousResponseID)
	if strings.HasPrefix(previousResponseID, syntheticCompactIDPrefix) {
		if state, ok, err := loadSyntheticCompactState(ctx, previousResponseID); err != nil || ok {
			return state, ok, err
		}
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
	cleanInput, err := removeSyntheticCompactMarkers(req.Input)
	if err != nil {
		return dto.OpenAIResponsesRequest{}, err
	}
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
	cleanInput, err := removeSyntheticCompactMarkers(req.Input)
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
	if state.UserID != 0 && state.UserID != scope.UserID {
		return fmt.Errorf("%w: synthetic compact state belongs to a different user", ErrSyntheticCompactStateScopeMismatch)
	}
	if state.TokenID != 0 && state.TokenID != scope.TokenID {
		return fmt.Errorf("%w: synthetic compact state belongs to a different token", ErrSyntheticCompactStateScopeMismatch)
	}
	if state.Group != "" && state.Group != strings.TrimSpace(scope.Group) {
		return fmt.Errorf("%w: synthetic compact state belongs to a different group", ErrSyntheticCompactStateScopeMismatch)
	}
	if !syntheticCompactModelCompatible(state.Model, model) {
		return fmt.Errorf("%w: synthetic compact state belongs to a different model", ErrSyntheticCompactStateScopeMismatch)
	}
	return nil
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
