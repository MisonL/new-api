package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func codexHeaderProfileStrategy() *dto.HeaderProfileStrategy {
	return &dto.HeaderProfileStrategy{
		Enabled:            true,
		Mode:               dto.HeaderProfileModeFixed,
		SelectedProfileIDs: []string{"codex-cli"},
		Profiles: []dto.HeaderProfile{
			{
				ID:      "codex-cli",
				Name:    "codex-cli",
				Headers: map[string]string{"User-Agent": dto.BuiltinCodexCLIUserAgent},
			},
		},
	}
}

func newCodexPreflightContext(t *testing.T, settings dto.ChannelOtherSettings, channelID int) (*gin.Context, *relaycommon.RelayInfo, *model.Channel) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	channel := &model.Channel{Id: channelID, Type: constant.ChannelTypeOpenAI}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		Request:   &dto.OpenAIResponsesRequest{Model: "gpt-5.5"},
	}
	meta := relaycommon.ChannelMeta{
		ChannelOtherSettings: settings,
		ParamOverride:        map[string]interface{}{},
	}
	meta.ChannelId = channelID
	info.ChannelMeta = &meta
	return ctx, info, channel
}

// A channel whose Codex header profile cannot forward client metadata is not
// selectable for a client that sends none, which is the strict default.
func TestCodexDynamicHeaderPreflightRejectsChannelWithoutOptOut(t *testing.T) {
	settings := dto.ChannelOtherSettings{HeaderProfileStrategy: codexHeaderProfileStrategy()}
	ctx, info, channel := newCodexPreflightContext(t, settings, 221)

	missing := missingResponsesCodexDynamicHeaders(ctx, info, channel)
	require.Equal(t, []string{"x-codex-*"}, missing)

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)
	require.NoError(t, err)
	require.True(t, skip)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: channel.Id,
		Reason:    responsesRoutingChannelSkipReasonUnsupportedCodexDynamicHeaders,
	}})
}

// An explicit channel-level opt-out clears the routing preflight while the
// runtime passthrough check in the request path stays in force.
func TestCodexDynamicHeaderPreflightHonoursChannelOptOut(t *testing.T) {
	allow := true
	settings := dto.ChannelOtherSettings{
		HeaderProfileStrategy:                  codexHeaderProfileStrategy(),
		AllowMissingCodexDynamicRequestHeaders: &allow,
	}
	ctx, info, channel := newCodexPreflightContext(t, settings, 221)

	require.Empty(t, missingResponsesCodexDynamicHeaders(ctx, info, channel))
}

// An explicit false is the same as an absent field: the channel stays strict.
func TestCodexDynamicHeaderPreflightTreatsExplicitFalseAsStrict(t *testing.T) {
	deny := false
	settings := dto.ChannelOtherSettings{
		HeaderProfileStrategy:                  codexHeaderProfileStrategy(),
		AllowMissingCodexDynamicRequestHeaders: &deny,
	}
	ctx, info, channel := newCodexPreflightContext(t, settings, 221)

	require.Equal(t, []string{"x-codex-*"}, missingResponsesCodexDynamicHeaders(ctx, info, channel))
}
