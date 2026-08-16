package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestOpaqueOpenAIResponsesRequestClonesMetadataAndPreservesExplicitZeroValues(t *testing.T) {
	stream := false
	maxOutputTokens := uint(0)
	metadata := common.ResponsesRequestMetadata{
		Model:                        "gpt-5.6-sol",
		ModelPresent:                 true,
		Stream:                       &stream,
		MaxOutputTokens:              &maxOutputTokens,
		HasOpaqueEncryptedContent:    true,
		EncryptedContentFingerprints: []string{"fingerprint-one"},
		BodySize:                     128,
		Shape: common.ResponsesRequestShapeMetadata{
			InputItemTypes:    map[string]int{"message": 1},
			TopLevelToolTypes: map[string]int{"function": 1},
		},
	}
	request := NewOpaqueOpenAIResponsesRequest(metadata)

	stream = true
	maxOutputTokens = 4096
	metadata.Model = "mutated-source"
	metadata.EncryptedContentFingerprints[0] = "mutated-source-fingerprint"
	metadata.Shape.InputItemTypes["message"] = 9
	metadata.Shape.TopLevelToolTypes["function"] = 9

	stored := request.OpaqueResponsesMetadata()
	require.Equal(t, "gpt-5.6-sol", stored.Model)
	require.NotNil(t, stored.Stream)
	require.False(t, *stored.Stream)
	require.NotNil(t, stored.MaxOutputTokens)
	require.Zero(t, *stored.MaxOutputTokens)
	require.Equal(t, []string{"fingerprint-one"}, stored.EncryptedContentFingerprints)
	require.Equal(t, map[string]int{"message": 1}, stored.Shape.InputItemTypes)
	require.Equal(t, map[string]int{"function": 1}, stored.Shape.TopLevelToolTypes)
	require.False(t, request.IsStream(nil))
	require.Zero(t, request.GetTokenCountMeta().MaxTokens)

	*stored.Stream = true
	*stored.MaxOutputTokens = 2048
	stored.EncryptedContentFingerprints[0] = "mutated-returned-fingerprint"
	stored.Shape.InputItemTypes["message"] = 8
	stored.Shape.TopLevelToolTypes["function"] = 8
	storedAgain := request.OpaqueResponsesMetadata()
	require.NotNil(t, storedAgain.Stream)
	require.False(t, *storedAgain.Stream)
	require.NotNil(t, storedAgain.MaxOutputTokens)
	require.Zero(t, *storedAgain.MaxOutputTokens)
	require.Equal(t, []string{"fingerprint-one"}, storedAgain.EncryptedContentFingerprints)
	require.Equal(t, map[string]int{"message": 1}, storedAgain.Shape.InputItemTypes)
	require.Equal(t, map[string]int{"function": 1}, storedAgain.Shape.TopLevelToolTypes)

	request.SetModelName("mapped-gpt-5.6-sol")
	request.SetModelName("")
	require.Equal(t, "mapped-gpt-5.6-sol", request.Model)
	require.Equal(t, "gpt-5.6-sol", request.OpaqueResponsesMetadata().Model)
}

func TestOpaqueOpenAIResponsesRequestKeepsNilMetadataValuesAndRejectsJSONMarshal(t *testing.T) {
	request := NewOpaqueOpenAIResponsesRequest(common.ResponsesRequestMetadata{
		Model: "gpt-5.6-sol",
	})

	metadata := request.OpaqueResponsesMetadata()
	require.Nil(t, metadata.Stream)
	require.Nil(t, metadata.MaxOutputTokens)
	require.False(t, request.IsStream(nil))

	encoded, err := common.Marshal(request)
	require.Nil(t, encoded)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be replayed from storage")

	var nilRequest *OpaqueOpenAIResponsesRequest
	require.False(t, nilRequest.IsOpaqueResponsesRequest())
	require.Empty(t, nilRequest.OpaqueResponsesMetadata())
	require.False(t, nilRequest.IsStream(nil))
	require.Zero(t, nilRequest.GetTokenCountMeta().MaxTokens)
}
