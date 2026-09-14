package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestChannelRouteResolverAppliesFinalTaskWireModelBeforeSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntent{
		Action: constant.TaskActionReferenceGenerate,
	})
	mapping := `{"client-vidu":"mapped-vidu"}`
	channel := &model.Channel{
		Id:           201,
		Type:         constant.ChannelTypeVidu,
		ModelMapping: &mapping,
	}

	effective, mapped, err := ResolveChannelModelMappingForContext(ctx, channel, "client-vidu", false)

	require.NoError(t, err)
	require.Equal(t, "viduq2", effective)
	require.True(t, mapped)
}

func TestChannelRouteResolverLeavesNonViduTaskModelsUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)
	common.SetContextKey(ctx, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntent{
		Action: constant.TaskActionReferenceGenerate,
	})
	mapping := `{"client-model":"mapped-model"}`
	channel := &model.Channel{
		Id:           202,
		Type:         constant.ChannelTypeOpenAI,
		ModelMapping: &mapping,
	}

	effective, mapped, err := ResolveChannelModelMappingForContext(ctx, channel, "client-model", false)

	require.NoError(t, err)
	require.Equal(t, "mapped-model", effective)
	require.True(t, mapped)
}
