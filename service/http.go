package service

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

func CloseResponseBodyGracefully(httpResponse *http.Response) {
	if httpResponse == nil || httpResponse.Body == nil {
		return
	}
	err := httpResponse.Body.Close()
	if err != nil {
		common.SysError("failed to close response body: " + err.Error())
	}
}

// upstreamHeaderDenylistLower holds response headers that must never be
// mirrored onto new-api's own origin. Set-Cookie is the significant one: the
// upstream would otherwise set a session cookie on the new-api domain, which
// is cookie tossing and a foothold for session fixation. The RFC 7230
// hop-by-hop set describes a connection between new-api and the upstream that
// does not exist between the client and new-api, so forwarding it is wrong too.
var upstreamHeaderDenylistLower = map[string]struct{}{
	"set-cookie":          {},
	"connection":          {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

func ShouldCopyUpstreamHeader(c *gin.Context, key string, values []string) bool {
	if strings.EqualFold(key, "Content-Length") {
		return false
	}
	if _, denied := upstreamHeaderDenylistLower[strings.ToLower(strings.TrimSpace(key))]; denied {
		return false
	}
	if captureUpstreamRequestID(c, key, values) {
		return false
	}
	return true
}

var upstreamRequestIDHeaderPriority = []string{
	common.RequestIdKey,
	"X-Request-Id",
	"Request-Id",
}

// CaptureUpstreamRequestID records the first non-empty provider request ID
// using a stable header priority, regardless of map iteration order.
func CaptureUpstreamRequestID(c *gin.Context, headers http.Header) {
	if c == nil || len(headers) == 0 || c.GetString(common.UpstreamRequestIdKey) != "" {
		return
	}
	for _, headerName := range upstreamRequestIDHeaderPriority {
		values := headerValuesCaseInsensitive(headers, headerName)
		if captureUpstreamRequestID(c, headerName, values) && c.GetString(common.UpstreamRequestIdKey) != "" {
			return
		}
	}
}

// headerValuesCaseInsensitive also handles hand-built http.Header maps whose
// keys were not canonicalized by net/http. Responses received from the
// standard transport are canonical, but custom transports and tests are not
// required to normalize header names first.
func headerValuesCaseInsensitive(headers http.Header, name string) []string {
	if values := headers.Values(name); len(values) > 0 {
		return values
	}
	for key, values := range headers {
		if strings.EqualFold(key, name) {
			return values
		}
	}
	return nil
}

// captureUpstreamRequestID records provider request identifiers without
// allowing an upstream response to replace the gateway's local request ID.
func captureUpstreamRequestID(c *gin.Context, key string, values []string) bool {
	if !isUpstreamRequestIDHeader(key) {
		return false
	}
	if c != nil && c.GetString(common.UpstreamRequestIdKey) == "" {
		for _, value := range values {
			if requestID := strings.TrimSpace(value); requestID != "" {
				c.Set(common.UpstreamRequestIdKey, requestID)
				break
			}
		}
	}
	return true
}

func isUpstreamRequestIDHeader(key string) bool {
	for _, headerName := range upstreamRequestIDHeaderPriority {
		if strings.EqualFold(key, headerName) {
			return true
		}
	}
	return false
}

func IOCopyBytesGracefully(c *gin.Context, src *http.Response, data []byte) {
	if c.Writer == nil {
		return
	}

	body := io.NopCloser(bytes.NewBuffer(data))

	// We shouldn't set the header before we parse the response body, because the parse part may fail.
	// And then we will have to send an error response, but in this case, the header has already been set.
	// So the httpClient will be confused by the response.
	// For example, Postman will report error, and we cannot check the response at all.
	if src != nil {
		CaptureUpstreamRequestID(c, src.Header)
		for k, v := range src.Header {
			if !ShouldCopyUpstreamHeader(c, k, v) {
				continue
			}
			c.Writer.Header().Set(k, v[0])
		}
	}

	// set Content-Length header manually BEFORE calling WriteHeader
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write header with status code (this sends the headers)
	if src != nil {
		c.Writer.WriteHeader(src.StatusCode)
	} else {
		c.Writer.WriteHeader(http.StatusOK)
	}

	_, err := io.Copy(c.Writer, body)
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to copy response body: %s", err.Error()))
	}
	c.Writer.Flush()
}
