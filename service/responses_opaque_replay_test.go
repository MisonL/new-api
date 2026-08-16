package service

import (
	"context"
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
