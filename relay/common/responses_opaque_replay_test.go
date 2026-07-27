package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesInputHasOpaqueEncryptedContent(t *testing.T) {
	tests := []struct {
		name  string
		input json.RawMessage
		want  bool
	}{
		{
			name:  "top level string",
			input: json.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque-state"}]`),
			want:  true,
		},
		{
			name:  "nested future object",
			input: json.RawMessage(`[{"type":"future_item","state":{"items":[{"encrypted_content":{"version":2}}]}}]`),
			want:  true,
		},
		{
			name:  "synthetic marker",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n0123456789abcdef0123456789abcdef:resp_newapi_synthcmp_n0123456789abcdef0123456789abcdef_0123456789abcdef0123456789abcdef"}]`),
			want:  true,
		},
		{
			name:  "legacy synthetic marker",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_0123456789abcdef0123456789abcdef"}]`),
			want:  true,
		},
		{
			name:  "native opaque compact marker",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_nativecmp_n0123456789abcdef0123456789abcdef_0123456789abcdef01234567"}]`),
			want:  true,
		},
		{
			name:  "malformed synthetic marker is opaque",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n1:resp_local"}]`),
			want:  true,
		},
		{
			name:  "short v2 instance is opaque",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n0123456789abcdef:resp_newapi_synthcmp_n0123456789abcdef_0123456789abcdef0123456789abcdef"}]`),
			want:  true,
		},
		{
			name:  "malformed v2 state id is opaque",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:v2:n0123456789abcdef0123456789abcdef:resp_newapi_synthcmp_n0123456789abcdef0123456789abcdef_state"}]`),
			want:  true,
		},
		{
			name:  "legacy prefix collision is opaque",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_legacy"}]`),
			want:  true,
		},
		{
			name:  "malformed native compact state id is opaque",
			input: json.RawMessage(`[{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_nativecmp_n0123456789abcdef0123456789abcdef_state"}]`),
			want:  true,
		},
		{
			name:  "synthetic marker outside compaction item is opaque",
			input: json.RawMessage(`[{"type":"reasoning","encrypted_content":"newapi.synthetic.compact:v2:n0123456789abcdef0123456789abcdef:resp_newapi_synthcmp_n0123456789abcdef0123456789abcdef_0123456789abcdef0123456789abcdef"}]`),
			want:  true,
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

func TestResponsesInputHasOpaqueEncryptedContentWithMarkerValidator(t *testing.T) {
	marker := "newapi.synthetic.compact:resp_newapi_synthcmp_0123456789abcdef0123456789abcdef"
	input := json.RawMessage(`[{"type":"compaction","encrypted_content":"` + marker + `"}]`)

	got, err := ResponsesInputHasOpaqueEncryptedContentWithMarkerValidator(input, func(candidate string) (bool, error) {
		return candidate == marker, nil
	})

	require.NoError(t, err)
	require.False(t, got)
}

func TestResponsesInputHasOpaqueEncryptedContentRejectsMalformedInput(t *testing.T) {
	got, err := ResponsesInputHasOpaqueEncryptedContent(json.RawMessage(`[{"encrypted_content":"opaque"}`))

	require.False(t, got)
	require.Error(t, err)
}
