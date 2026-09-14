package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesInputHasOpaqueEncryptedContentLegacyCases(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
		want  bool
	}{
		{
			name:  "top level opaque reasoning",
			input: json.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-state"}]`),
			want:  true,
		},
		{
			name:  "unknown nested field",
			input: json.RawMessage(`[{"type":"future_item","state":{"items":[{"encrypted_content":{"version":2}}]}}]`),
			want:  false,
		},
		{
			name:  "synthetic marker is local state",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n0123456789abcdef0123456789abcdef:resp_newapi_synthcmp_n0123456789abcdef0123456789abcdef_0123456789abcdef0123456789abcdef"}]`),
			want:  false,
		},
		{
			name:  "synthetic marker outside compaction item is local state",
			input: json.RawMessage(`[{"type":"reasoning","encrypted_content":"newapi.synthetic.compact:v2:n0123456789abcdef0123456789abcdef:resp_newapi_synthcmp_n0123456789abcdef0123456789abcdef_0123456789abcdef0123456789abcdef"}]`),
			want:  false,
		},
		{
			name:  "empty and null",
			input: json.RawMessage(`[{"encrypted_content":" "},{"encrypted_content":null}]`),
		},
		{
			name:  "unrelated field",
			input: json.RawMessage(`[{"not_encrypted_content":"opaque-state"}]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResponsesInputHasOpaqueEncryptedContent(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResponsesInputHasOpaqueEncryptedContentWithMarkerValidatorLegacy(t *testing.T) {
	marker := "newapi.synthetic.compact:resp_newapi_synthcmp_0123456789abcdef0123456789abcdef"
	input := json.RawMessage(`[{"type":"compaction","encrypted_content":"` + marker + `"}]`)

	got, err := ResponsesInputHasOpaqueEncryptedContentWithMarkerValidator(input, func(candidate string) (bool, error) {
		return candidate == marker, nil
	})

	require.NoError(t, err)
	require.False(t, got)
}

func TestResponsesInputHasOpaqueEncryptedContentLegacyRejectsMalformedInput(t *testing.T) {
	got, err := ResponsesInputHasOpaqueEncryptedContent(json.RawMessage(`[{"encrypted_content":"opaque"}`))

	require.False(t, got)
	require.Error(t, err)
}
