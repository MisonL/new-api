package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexLocalCompactionIngressLimitNeverTrustsClientMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = previousMaxRequestBodyMB
	})

	for _, tc := range []struct {
		name     string
		path     string
		metadata string
	}{
		{
			name:     "local compaction metadata",
			path:     "/v1/responses",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`,
		},
		{
			name:     "ordinary responses",
			path:     "/v1/responses",
			metadata: ``,
		},
		{
			name:     "remote compaction",
			path:     "/v1/responses",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`,
		},
		{
			name:     "wrong endpoint",
			path:     "/v1/chat/completions",
			metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
			if tc.metadata != "" {
				ctx.Request.Header.Set("X-Codex-Turn-Metadata", tc.metadata)
			}

			require.Equal(t, int64(constant.MaxRequestBodyMB)<<20, RequestBodyIngressLimit(ctx))
		})
	}
}

func TestRequestBodyIngressLimitKeepsLegacyDefaultForOrdinaryRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 0
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = previousMaxRequestBodyMB
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	require.Equal(t, int64(32<<20), RequestBodyIngressLimit(ctx))
}
