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

func TestIsResponsesCompactPath(t *testing.T) {
	testCases := []struct {
		path string
		want bool
	}{
		{path: "/v1/responses/compact", want: true},
		{path: "/responses/compact", want: true},
		{path: "/v1/responses", want: false},
		{path: "/v1/responses/compact/extra", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.path, func(t *testing.T) {
			require.Equal(t, testCase.want, IsResponsesCompactPath(testCase.path))
		})
	}
}

func TestIsResponsesCompactionIngressRequest(t *testing.T) {
	testCases := []struct {
		name     string
		method   string
		target   string
		metadata string
		want     bool
	}{
		{
			name:     "local Codex responses compaction",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`,
			want:     true,
		},
		{
			name:   "direct responses compact endpoint",
			method: http.MethodPost,
			target: "/v1/responses/compact",
			want:   true,
		},
		{
			name:   "responses compact compatibility endpoint",
			method: http.MethodPost,
			target: "/responses/compact",
			want:   true,
		},
		{
			name:   "direct compact requires post",
			method: http.MethodGet,
			target: "/v1/responses/compact",
			want:   false,
		},
		{
			name:   "ordinary responses request",
			method: http.MethodPost,
			target: "/v1/responses",
			want:   false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.target, nil)
			if testCase.metadata != "" {
				request.Header.Set("X-Codex-Turn-Metadata", testCase.metadata)
			}

			require.Equal(t, testCase.want, IsResponsesCompactionIngressRequest(request))
		})
	}
}

func TestIsCodexOrdinaryResponsesRequest(t *testing.T) {
	testCases := []struct {
		name     string
		method   string
		target   string
		metadata string
		want     bool
	}{
		{
			name:     "ordinary turn metadata",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"installation_id":"install","session_id":"session","thread_id":"thread","turn_id":"turn","window_id":"thread:0","request_kind":"turn","turn_started_at_unix_ms":1}`,
			want:     true,
		},
		{
			name:     "prewarm metadata remains a control flow",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"prewarm","session_id":"session","thread_id":"thread"}`,
			want:     false,
		},
		{
			name:     "memory metadata remains a control flow",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"memory"}`,
			want:     false,
		},
		{
			name:     "future request kind remains on the existing path",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"future_kind","new_nested_field":{"preserve":true}}`,
			want:     false,
		},
		{
			name:     "remote compaction remains on the existing path",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`,
			want:     false,
		},
		{
			name:     "empty request kind",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"  "}`,
			want:     false,
		},
		{
			name:     "invalid metadata",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":`,
			want:     false,
		},
		{
			name:   "ordinary OpenAI responses request",
			method: http.MethodPost,
			target: "/v1/responses",
			want:   false,
		},
		{
			name:     "wrong endpoint",
			method:   http.MethodPost,
			target:   "/v1/chat/completions",
			metadata: `{"request_kind":"turn"}`,
			want:     false,
		},
		{
			name:     "wrong method",
			method:   http.MethodGet,
			target:   "/v1/responses",
			metadata: `{"request_kind":"turn"}`,
			want:     false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.target, nil)
			if testCase.metadata != "" {
				request.Header.Set("X-Codex-Turn-Metadata", testCase.metadata)
			}

			require.Equal(t, testCase.want, IsCodexOrdinaryResponsesRequest(request))
		})
	}
}

func TestIsResponsesOpaqueReplayIngressRequest(t *testing.T) {
	testCases := []struct {
		name     string
		method   string
		target   string
		metadata string
		want     bool
	}{
		{
			name:     "ordinary Codex turn",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"turn"}`,
			want:     true,
		},
		{
			name:   "direct compact endpoint",
			method: http.MethodPost,
			target: "/v1/responses/compact",
			want:   true,
		},
		{
			name:   "non Codex ordinary responses",
			method: http.MethodPost,
			target: "/v1/responses",
			want:   false,
		},
		{
			name:     "remote compaction remains outside opaque replay ingress",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`,
			want:     false,
		},
		{
			name:     "invalid Codex metadata",
			method:   http.MethodPost,
			target:   "/v1/responses",
			metadata: `{"request_kind":`,
			want:     false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(testCase.method, testCase.target, nil)
			if testCase.metadata != "" {
				request.Header.Set("X-Codex-Turn-Metadata", testCase.metadata)
			}

			require.Equal(t, testCase.want, IsResponsesOpaqueReplayIngressRequest(request))
		})
	}
}
