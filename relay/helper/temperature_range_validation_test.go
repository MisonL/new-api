package helper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Temperature carries a documented OpenAI range. Before this check the value
// was forwarded verbatim, so a client sending -1 or 100 either received an
// upstream error whose wording varied per provider or, on adaptors that ignore
// the field, no signal at all.
func TestGetAndValidateTextRequestRejectsOutOfRangeTemperature(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		relayMode   int
		expectError bool
	}{
		{
			name:        "negative",
			body:        `{"model":"gpt-5.4","temperature":-1,"messages":[{"role":"user","content":"hi"}]}`,
			expectError: true,
		},
		{
			name:        "above maximum",
			body:        `{"model":"gpt-5.4","temperature":100,"messages":[{"role":"user","content":"hi"}]}`,
			expectError: true,
		},
		{
			name:        "just above maximum",
			body:        `{"model":"gpt-5.4","temperature":2.0001,"messages":[{"role":"user","content":"hi"}]}`,
			expectError: true,
		},
		{
			name:        "zero is allowed",
			body:        `{"model":"gpt-5.4","temperature":0,"messages":[{"role":"user","content":"hi"}]}`,
			expectError: false,
		},
		{
			name:        "maximum is allowed",
			body:        `{"model":"gpt-5.4","temperature":2,"messages":[{"role":"user","content":"hi"}]}`,
			expectError: false,
		},
		{
			name:        "omitted is allowed",
			body:        `{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`,
			expectError: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, err := GetAndValidateTextRequest(newValidationContext(t, "/v1/chat/completions", tc.body), tc.relayMode)
			if tc.expectError {
				require.Nil(t, request)
				require.ErrorContains(t, err, "temperature is invalid")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, request)
		})
	}
}

func TestGetAndValidateResponsesRequestRejectsOutOfRangeTemperature(t *testing.T) {
	request, err := GetAndValidateResponsesRequest(newValidationContext(
		t, "/v1/responses", `{"model":"gpt-5.4","temperature":-1,"input":"hi"}`))
	require.Nil(t, request)
	require.ErrorContains(t, err, "temperature is invalid")
}

func TestGetAndValidateResponsesCompactionRequestRejectsOutOfRangeTemperature(t *testing.T) {
	request, err := GetAndValidateResponsesCompactionRequest(newValidationContext(
		t, "/v1/responses/compact", `{"model":"gpt-5.4","temperature":100,"input":"hi"}`))
	require.Nil(t, request)
	require.ErrorContains(t, err, "temperature is invalid")
}
