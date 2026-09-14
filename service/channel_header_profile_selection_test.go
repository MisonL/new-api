package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newChannelHeaderProfileSelectionTestContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return ctx
}

func TestPrepareChannelRuntimeHeaderProfileSelectionReusesPerChannelPreflightSelection(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.RequestHeaderStrategyState{})

	ctx := newChannelHeaderProfileSelectionTestContext()
	strategy := &dto.HeaderProfileStrategy{
		Enabled:            true,
		Mode:               dto.HeaderProfileModeRoundRobin,
		SelectedProfileIDs: []string{"codex-cli", "claude-code"},
	}

	firstA, err := PrepareChannelRuntimeHeaderProfileSelection(ctx, 401, strategy, 0)
	require.NoError(t, err)
	firstB, err := PrepareChannelRuntimeHeaderProfileSelection(ctx, 402, strategy, 0)
	require.NoError(t, err)
	require.Equal(t, "codex-cli", firstA.ProfileID)
	require.Equal(t, "codex-cli", firstB.ProfileID)

	// Re-visiting channel A after channel B has been preflighted must reuse A's
	// selection instead of advancing A's persistent round-robin cursor.
	repeatedA, err := PrepareChannelRuntimeHeaderProfileSelection(ctx, 401, strategy, 0)
	require.NoError(t, err)
	require.Equal(t, firstA.ProfileID, repeatedA.ProfileID)
	require.Equal(t, firstA.Headers, repeatedA.Headers)

	scopeKeyA := "channel:401:" + runtimeRoundRobinValuesFingerprint([]string{"codex-cli", "claude-code"})
	stateA := model.RequestHeaderStrategyState{}
	require.NoError(t, model.DB.Where("scope_type = ? AND scope_key = ?", runtimeHeaderProfileScopeType, scopeKeyA).First(&stateA).Error)
	require.Equal(t, int64(1), stateA.RoundRobinCursor)

	nextA, err := PrepareChannelRuntimeHeaderProfileSelection(ctx, 401, strategy, 1)
	require.NoError(t, err)
	require.Equal(t, "claude-code", nextA.ProfileID)
	repeatedNextA, err := PrepareChannelRuntimeHeaderProfileSelection(ctx, 401, strategy, 1)
	require.NoError(t, err)
	require.Equal(t, nextA.ProfileID, repeatedNextA.ProfileID)

	require.Equal(t, nextA, func() RuntimeHeaderProfileSelection {
		selection, ok := GetChannelRuntimeHeaderProfileSelection(ctx)
		require.True(t, ok)
		return selection
	}())
}

func TestPrepareChannelRuntimeHeaderProfileSelectionDisabledCandidateKeepsOtherPreflightSelections(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.RequestHeaderStrategyState{})

	ctx := newChannelHeaderProfileSelectionTestContext()
	strategy := &dto.HeaderProfileStrategy{
		Enabled:            true,
		Mode:               dto.HeaderProfileModeRoundRobin,
		SelectedProfileIDs: []string{"codex-cli", "claude-code"},
	}

	firstA, err := PrepareChannelRuntimeHeaderProfileSelection(ctx, 411, strategy, 0)
	require.NoError(t, err)
	_, err = PrepareChannelRuntimeHeaderProfileSelection(ctx, 412, &dto.HeaderProfileStrategy{Enabled: false}, 0)
	require.NoError(t, err)

	repeatedA, err := PrepareChannelRuntimeHeaderProfileSelection(ctx, 411, strategy, 0)
	require.NoError(t, err)
	require.Equal(t, firstA.ProfileID, repeatedA.ProfileID)

	scopeKeyA := "channel:411:" + runtimeRoundRobinValuesFingerprint([]string{"codex-cli", "claude-code"})
	stateA := model.RequestHeaderStrategyState{}
	require.NoError(t, model.DB.Where("scope_type = ? AND scope_key = ?", runtimeHeaderProfileScopeType, scopeKeyA).First(&stateA).Error)
	require.Equal(t, int64(1), stateA.RoundRobinCursor)
}
