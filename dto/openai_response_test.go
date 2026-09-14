package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestIncompleteDetailsRoundTripPreservesStandardAndLegacyFields(t *testing.T) {
	var response OpenAIResponsesResponse
	err := common.Unmarshal([]byte(`{
		"status": "incomplete",
		"incomplete_details": {
			"reason": "max_output_tokens",
			"reasoning": "legacy_reasoning"
		}
	}`), &response)
	require.NoError(t, err)
	require.NotNil(t, response.IncompleteDetails)
	require.Equal(t, "max_output_tokens", response.IncompleteDetails.Reason)
	require.Equal(t, "legacy_reasoning", response.IncompleteDetails.Reasoning)

	encoded, err := common.Marshal(response)
	require.NoError(t, err)

	var roundTrip OpenAIResponsesResponse
	require.NoError(t, common.Unmarshal(encoded, &roundTrip))
	require.NotNil(t, roundTrip.IncompleteDetails)
	require.Equal(t, "max_output_tokens", roundTrip.IncompleteDetails.Reason)
	require.Equal(t, "legacy_reasoning", roundTrip.IncompleteDetails.Reasoning)
}
