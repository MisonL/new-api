package service

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestChannelHealthAttemptObserveAPIErrorIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useChannelHealthRedisForTest(t)

	channel := &model.Channel{
		Id:    90406,
		Type:  constant.ChannelTypeOpenAI,
		Group: "coding",
		Key:   "channel-health-attempt-key",
	}
	attempt, err := BeginChannelHealthAttempt(
		context.Background(),
		"coding",
		"gpt-5.6-luna",
		channel,
		"/v1/chat/completions",
		channel.Key,
	)
	require.NoError(t, err)
	require.True(t, attempt.Selection.Allow)
	require.Equal(t, ChannelHealthSelectionHealthy, attempt.Selection.Action)

	apiErr := types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	const callers = 16
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			attempt.ObserveAPIError(apiErr)
		}()
	}
	wg.Wait()

	require.True(t, attempt.finalized.Load())
	require.NotNil(t, attempt.Info)
	require.NotNil(t, attempt.Info.ChannelHealthRoute)
	_, _, _, failuresKey := channelHealthKeysForRoute("coding", *attempt.Info.ChannelHealthRoute)
	failureCount, err := common.RDB.Get(context.Background(), failuresKey).Int()
	require.NoError(t, err)
	require.Equal(t, 1, failureCount)
}

func TestBeginChannelHealthAttemptAllowsJimengNativeRootPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	channel := &model.Channel{
		Id:    90407,
		Type:  constant.ChannelTypeJimeng,
		Group: "default",
		Key:   "access-key|secret-key",
	}

	attempt, err := BeginChannelHealthAttempt(
		context.Background(),
		"default",
		"jimeng_vgfm_t2v_l20",
		channel,
		"/",
		channel.Key,
	)
	require.NoError(t, err)
	require.Equal(t, "/", attempt.Info.UpstreamRequestPath)
	require.Equal(t, "/", attempt.Info.ChannelHealthRoute.UpstreamPath)

	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "jimeng_vgfm_t2v_l20",
		EffectiveUpstreamModel: "jimeng_vgfm_t2v_l20",
		FinalProtocol:          channelHealthRouteProtocol(nil, channel.Type, relayconstant.RelayModeVideoFetchByID),
		Operation:              "task.jimeng.poll",
		UpstreamPath:           "/",
		ChannelID:              channel.Id,
	}.Normalized()
	withRoute, err := BeginChannelHealthAttemptWithRoute(
		context.Background(),
		"default",
		route,
		channel,
		channel.Key,
	)
	require.NoError(t, err)
	require.Equal(t, "/", withRoute.Info.UpstreamRequestPath)
	require.Equal(t, "/", withRoute.Info.ChannelHealthRoute.UpstreamPath)
}
