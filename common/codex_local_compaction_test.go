package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsCodexLocalResponsesCompactionRequest(t *testing.T) {
	testCases := []struct {
		name     string
		method   string
		target   string
		metadata string
		want     bool
	}{
		{
			name:     "exact local responses compaction",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`,
			want:     true,
		},
		{
			name:     "allows unrelated metadata fields",
			method:   http.MethodPost,
			target:   "/v1/responses?trace=true",
			metadata: `{"request_kind":"compaction","turn_id":"turn_123","compaction":{"implementation":"responses","other":"value"}}`,
			want:     true,
		},
		{
			name:     "remote v2 compaction is not local ingress",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`,
			want:     false,
		},
		{
			name:     "remote compact endpoint is not local ingress",
			method:   http.MethodPost,
			target:   "/v1/responses/compact",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`,
			want:     false,
		},
		{
			name:     "non compaction request is not local ingress",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"turn","compaction":{"implementation":"responses"}}`,
			want:     false,
		},
		{
			name:     "non post request is not local ingress",
			method:   http.MethodGet,
			target:   "/v1/responses",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`,
			want:     false,
		},
		{
			name:     "invalid metadata is not local ingress",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":`,
			want:     false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.target, nil)
			request.Header.Set("X-Codex-Turn-Metadata", testCase.metadata)

			require.Equal(t, testCase.want, IsCodexLocalResponsesCompactionRequest(request))
		})
	}
}
