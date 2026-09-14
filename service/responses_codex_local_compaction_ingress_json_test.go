package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexLocalCompactionIngressJSONReaderDiscardValueValidatesJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		valid bool
	}{
		{
			name:  "nested object with escaped string and number",
			value: `{"tools":[{"name":"apply_patch","definition":"line\\n\\u4e2d","limits":{"count":1.5e+2}}]}`,
			valid: true,
		},
		{
			name:  "trailing comma",
			value: `[{"name":"apply_patch",}]`,
			valid: false,
		},
		{
			name:  "invalid unicode escape",
			value: `{"definition":"\u12x4"}`,
			valid: false,
		},
		{
			name:  "trailing document",
			value: `{"name":"apply_patch"} true`,
			valid: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := newCodexLocalCompactionIngressJSONReader(strings.NewReader(tc.value))
			err := reader.discardValue()
			if err == nil {
				err = reader.expectEnd()
			}
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCodexLocalCompactionIngressJSONReaderBoundsRetainedValue(t *testing.T) {
	reader := newCodexLocalCompactionIngressJSONReader(strings.NewReader(`"` + strings.Repeat("A", 65) + `"`))
	_, err := reader.readRawValue(64, "retained field")

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCodexLocalCompactionIngressValueTooLarge))
}

func TestCodexLocalCompactionIngressJSONReaderDetectsOpaqueEncryptedContentRecursively(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  string
		opaque bool
	}{
		{
			name:  "plain object",
			value: `{"future":{"payload":"visible"}}`,
		},
		{
			name:  "null value",
			value: `{"future":{"encrypted_content":null}}`,
		},
		{
			name:  "blank value",
			value: `{"future":{"encrypted_content":"  \t "}}`,
		},
		{
			name:  "synthetic marker",
			value: `{"future":{"encrypted_content":"newapi.synthetic.compact:v2:n1:resp_local"}}`,
		},
		{
			name:   "opaque inside discarded tools shape",
			value:  `{"tools":[{"schema":{"future":[{"encrypted_content":"opaque-state"}]}}]}`,
			opaque: true,
		},
		{
			name:   "escaped field name",
			value:  `{"future":{"encrypted\u005fcontent":"opaque-state"}}`,
			opaque: true,
		},
		{
			name:   "non string value",
			value:  `{"future":{"encrypted_content":{"state":true}}}`,
			opaque: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := newCodexLocalCompactionIngressJSONReader(strings.NewReader(tc.value))

			require.NoError(t, reader.discardValue())
			require.NoError(t, reader.expectEnd())
			require.Equal(t, tc.opaque, reader.hasOpaqueEncryptedContent)
		})
	}
}

func TestCodexLocalCompactionIngressJSONReaderDetectsDirectInputEncryptedContent(t *testing.T) {
	reader := newCodexLocalCompactionIngressJSONReader(strings.NewReader(
		`{"type":"reasoning","encrypted_content":"opaque-state"}`,
	))
	first, err := reader.readByte(nil)
	require.NoError(t, err)
	require.Equal(t, byte('{'), first)

	_, err = reader.readInputObjectWithoutTools()

	require.NoError(t, err)
	require.NoError(t, reader.expectEnd())
	require.True(t, reader.hasOpaqueEncryptedContent)
}

func TestCodexLocalCompactionIngressJSONReaderReadInputObjectWithoutTools(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
		valid bool
	}{
		{
			name:  "drops tools regardless of field order",
			input: `{"tools":[{"type":"custom","name":"apply_patch"}],"role":"developer","type":"additional_tools","additional_tools":[{"name":"ignored"}]}`,
			want:  `{"role":"developer","type":"additional_tools"}`,
			valid: true,
		},
		{
			name:  "rejects malformed discarded tools",
			input: `{"tools":[{"type":"custom","name":"apply_patch"},],"type":"additional_tools"}`,
			valid: false,
		},
		{
			name:  "rejects non-array additional tools after discard",
			input: `{"tools":{"type":"custom","name":"apply_patch"},"type":"additional_tools"}`,
			valid: false,
		},
		{
			name:  "rejects non-array tool search output tools after discard",
			input: `{"type":"tool_search_output","tools":"not an array"}`,
			valid: false,
		},
		{
			name:  "preserves nullable item type semantics",
			input: `{"type":null,"tools":{"not":"an array"}}`,
			want:  `{"type":null}`,
			valid: true,
		},
		{
			name:  "null duplicate type keeps the prior string value",
			input: `{"type":"additional_tools","type":null,"tools":[{"type":"custom","name":"apply_patch"}]}`,
			want:  `{"type":"additional_tools","type":null}`,
			valid: true,
		},
		{
			name:  "rejects non-null non-string item type",
			input: `{"type":123,"tools":[{"type":"custom","name":"apply_patch"}]}`,
			valid: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := newCodexLocalCompactionIngressJSONReader(strings.NewReader(tc.input))
			first, err := reader.readByte(nil)
			require.NoError(t, err)
			require.Equal(t, byte('{'), first)

			raw, err := reader.readInputObjectWithoutTools()
			if tc.valid {
				require.NoError(t, err)
				require.JSONEq(t, tc.want, string(raw))
				require.NoError(t, reader.expectEnd())
				return
			}
			require.Error(t, err)
		})
	}
}
