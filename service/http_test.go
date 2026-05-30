package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldCopyUpstreamHeaderPreservesLocalRequestId(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	require.False(t, ShouldCopyUpstreamHeader(ctx, common.RequestIdKey, []string{"upstream-req"}))
	require.Equal(t, "upstream-req", ctx.GetString(common.UpstreamRequestIdKey))

	recorder.Header().Set(common.RequestIdKey, "local-req")
	require.Equal(t, "local-req", recorder.Header().Get(common.RequestIdKey))
}

func TestIOCopyBytesGracefullySkipsUpstreamRequestIdHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			common.RequestIdKey: []string{"upstream-req"},
			"Content-Type":      []string{"application/json"},
		},
	}

	IOCopyBytesGracefully(ctx, resp, []byte(`{"ok":true}`))

	require.Empty(t, recorder.Header().Get(common.RequestIdKey))
	require.Equal(t, "upstream-req", ctx.GetString(common.UpstreamRequestIdKey))
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, `{"ok":true}`, recorder.Body.String())
}
