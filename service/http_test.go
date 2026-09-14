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

func TestShouldCopyUpstreamHeaderCapturesStandardProviderRequestId(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	require.False(t, ShouldCopyUpstreamHeader(ctx, "X-Request-Id", []string{"provider-req"}))
	require.Equal(t, "provider-req", ctx.GetString(common.UpstreamRequestIdKey))
	require.False(t, ShouldCopyUpstreamHeader(ctx, "Request-Id", []string{"provider-req-2"}))
	require.Equal(t, "provider-req", ctx.GetString(common.UpstreamRequestIdKey))
}

func TestCaptureUpstreamRequestIDUsesStableHeaderPriority(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	responseHeaders := http.Header{
		"Request-Id":        []string{"request-id"},
		"X-Request-Id":      []string{"x-request-id"},
		common.RequestIdKey: []string{"oneapi-request-id"},
		"Content-Type":      []string{"application/json"},
	}

	CaptureUpstreamRequestID(ctx, responseHeaders)

	require.Equal(t, "oneapi-request-id", ctx.GetString(common.UpstreamRequestIdKey))
}

func TestCaptureUpstreamRequestIDSkipsWhitespaceAndUsesNextHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	responseHeaders := http.Header{
		common.RequestIdKey: []string{"  \t"},
		"X-Request-Id":      []string{"  "},
		"Request-Id":        []string{" provider-request-id "},
	}

	CaptureUpstreamRequestID(ctx, responseHeaders)

	require.Equal(t, "provider-request-id", ctx.GetString(common.UpstreamRequestIdKey))
}

func TestCaptureUpstreamRequestIDSkipsEmptyValuesWithinHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	responseHeaders := http.Header{
		common.RequestIdKey: []string{"", "  ", "provider-request-id"},
	}

	CaptureUpstreamRequestID(ctx, responseHeaders)

	require.Equal(t, "provider-request-id", ctx.GetString(common.UpstreamRequestIdKey))
}

func TestCaptureUpstreamRequestIDHandlesNonCanonicalHeaderNames(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	responseHeaders := http.Header{
		"x-request-id": []string{"provider-request-id"},
		"request-id":   []string{"fallback-request-id"},
	}

	CaptureUpstreamRequestID(ctx, responseHeaders)

	require.Equal(t, "provider-request-id", ctx.GetString(common.UpstreamRequestIdKey))
}

func TestApplyDefaultUserAgentHeaderUsesGenericDefault(t *testing.T) {
	headers := http.Header{}
	ApplyDefaultUserAgentHeader(headers)

	require.Equal(t, DefaultUserAgent, headers.Get("User-Agent"))
	require.Equal(t, DefaultUserAgentArch, headers.Get("Sec-CH-UA-Arch"))
	require.Equal(t, DefaultUserAgentBitness, headers.Get("Sec-CH-UA-Bitness"))
}

func TestApplyDefaultUserAgentHeaderPreservesExplicitHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("User-Agent", "Client/1.0")
	ApplyDefaultUserAgentHeader(headers)

	require.Equal(t, "Client/1.0", headers.Get("User-Agent"))
	require.Empty(t, headers.Get("Sec-CH-UA-Arch"))
	require.Empty(t, headers.Get("Sec-CH-UA-Bitness"))
}

func TestDefaultUserAgentTransportAddsHeaderWithoutChangingOriginalRequest(t *testing.T) {
	var receivedUserAgent string
	var receivedArch string
	var receivedBitness string
	transport := defaultUserAgentTransport{base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		receivedUserAgent = req.Header.Get("User-Agent")
		receivedArch = req.Header.Get("Sec-CH-UA-Arch")
		receivedBitness = req.Header.Get("Sec-CH-UA-Bitness")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: http.NoBody, Request: req}, nil
	})}
	client := &http.Client{Transport: transport}
	req, err := http.NewRequest(http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, DefaultUserAgent, receivedUserAgent)
	require.Equal(t, DefaultUserAgentArch, receivedArch)
	require.Equal(t, DefaultUserAgentBitness, receivedBitness)
	require.Empty(t, req.Header.Get("User-Agent"))
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

func TestIOCopyBytesGracefullySkipsStandardProviderRequestIdHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Header: http.Header{
			"Request-Id":   []string{"request-id"},
			"X-Request-Id": []string{"x-request-id"},
			"Content-Type": []string{"application/json"},
		},
	}

	IOCopyBytesGracefully(ctx, resp, []byte(`{"error":"upstream"}`))

	require.Empty(t, recorder.Header().Get("Request-Id"))
	require.Empty(t, recorder.Header().Get("X-Request-Id"))
	require.Equal(t, "x-request-id", ctx.GetString(common.UpstreamRequestIdKey))
}
