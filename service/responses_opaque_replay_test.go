package service

import (
	"context"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestHasRemoteResponsesCompactionInputTreatsNonStringEncryptedContentAsOpaque(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "object", value: `{"version":2}`},
		{name: "array", value: `["opaque"]`},
		{name: "boolean", value: `true`},
		{name: "number", value: `42`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := dto.OpenAIResponsesRequest{
				Input: common.RawMessage(`[{"type":"compaction","encrypted_content":` + tt.value + `}]`),
			}

			hasRemote, err := HasRemoteResponsesCompactionInput(context.Background(), req)

			require.NoError(t, err)
			require.True(t, hasRemote)
		})
	}
}

func TestResponsesInputHasOpaqueEncryptedContentDecodesEscapedFieldNames(t *testing.T) {
	input := common.RawMessage(`[{"type":"future_item","state":{"\u0065ncrypted_content":"opaque-state"}}]`)

	hasOpaque, err := ResponsesInputHasOpaqueEncryptedContent(context.Background(), input)

	require.NoError(t, err)
	require.True(t, hasOpaque)
}

func TestResponsesInputHasOpaqueEncryptedContentBoundsLargeStringClassification(t *testing.T) {
	largeOpaque := strings.Repeat("x", responsesOpaqueEncryptedContentCaptureMaxBytes+1024)
	input := common.RawMessage(`[{"type":"reasoning","encrypted_content":"` + largeOpaque + `"}]`)

	hasOpaque, err := ResponsesInputHasOpaqueEncryptedContent(context.Background(), input)

	require.NoError(t, err)
	require.True(t, hasOpaque)
}

func TestResponsesInputHasOpaqueEncryptedContentClassifiesSyntheticMarkers(t *testing.T) {
	instanceID := syntheticCompactInstanceIDPrefix + strings.Repeat("a", syntheticCompactInstanceHexLen)
	stateID := syntheticCompactIDPrefix + instanceID + "_" + strings.Repeat("b", syntheticCompactStateHexLen)
	validMarker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + instanceID + ":" + stateID
	malformedMarker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":not-an-instance:" + stateID

	for _, test := range []struct {
		name   string
		marker string
		want   bool
	}{
		{name: "valid synthetic marker", marker: validMarker, want: false},
		{name: "malformed synthetic marker", marker: malformedMarker, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := common.RawMessage(`[{"type":"compaction","encrypted_content":"` + test.marker + `"}]`)
			hasOpaque, err := ResponsesInputHasOpaqueEncryptedContent(context.Background(), input)
			require.NoError(t, err)
			require.Equal(t, test.want, hasOpaque)
		})
	}
}
