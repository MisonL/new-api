package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

func TestFindResponsesViaChatRuleTrimsSyntheticPreviousResponseID(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previousPolicy := settings.ChatCompletionsToResponsesPolicy
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{
			{
				Name:           "responses-to-chat",
				Enabled:        true,
				SourceEndpoint: model_setting.ProtocolEndpointResponses,
				TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
				AllChannels:    true,
				ModelPatterns:  []string{"^gpt-5\\.5$"},
			},
		},
	}
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = previousPolicy
	})

	rule, err := FindResponsesViaChatRule(
		context.Background(),
		relayconstant.RelayModeResponses,
		false,
		dto.ChannelSettings{},
		160,
		0,
		"gpt-5.5",
		&dto.OpenAIResponsesRequest{
			PreviousResponseID: "  resp_newapi_synthcmp_nabc123_123  ",
		},
	)

	require.NoError(t, err)
	require.NotNil(t, rule)
	require.Equal(t, "responses-to-chat", rule.Name)
}
