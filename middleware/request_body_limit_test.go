package middleware

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func withAnonymousRequestBodyLimitKB(t *testing.T, kb int) {
	t.Helper()
	original := constant.AnonymousRequestBodyLimitKB
	t.Cleanup(func() { constant.AnonymousRequestBodyLimitKB = original })
	constant.AnonymousRequestBodyLimitKB = kb
}

func withMaxRequestBodyMB(t *testing.T, mb int) {
	t.Helper()
	original := constant.MaxRequestBodyMB
	t.Cleanup(func() { constant.MaxRequestBodyMB = original })
	constant.MaxRequestBodyMB = mb
}

// A body over the ceiling must be rejected before the handler runs: these
// routes are reachable without authentication, so the ceiling is the only
// bound on what a client can make the process buffer.
func TestAnonymousRequestBodyLimitRejectsOversizedBody(t *testing.T) {
	withAnonymousRequestBodyLimitKB(t, 1)
	gin.SetMode(gin.TestMode)

	handlerCalled := false
	engine := gin.New()
	engine.POST("/t", AnonymousRequestBodyLimit(), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	body := bytes.Repeat([]byte("a"), (1<<10)+1)
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body)))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.False(t, handlerCalled, "the handler must not run for an oversized body")
}

// A body exactly at the ceiling passes, the handler sees the same bytes, and
// the middleware restores the ContentLength it replaced.
func TestAnonymousRequestBodyLimitAcceptsBodyAtLimit(t *testing.T) {
	withAnonymousRequestBodyLimitKB(t, 1)
	gin.SetMode(gin.TestMode)

	payload := bytes.Repeat([]byte("b"), 1<<10)
	var got []byte
	var seenContentLength int64
	engine := gin.New()
	engine.POST("/t", AnonymousRequestBodyLimit(), func(c *gin.Context) {
		seenContentLength = c.Request.ContentLength
		var err error
		got, err = io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(payload))
	req.ContentLength = -1 // the middleware has to restore the real length
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, payload, got, "the handler must see the same bytes")
	require.Equal(t, int64(len(payload)), seenContentLength)
}

// A zero setting disables the limit, which is how an operator opts out.
func TestAnonymousRequestBodyLimitDisabledAtZero(t *testing.T) {
	withAnonymousRequestBodyLimitKB(t, 0)
	gin.SetMode(gin.TestMode)

	payload := bytes.Repeat([]byte("c"), 1<<20)
	var got []byte
	engine := gin.New()
	engine.POST("/t", AnonymousRequestBodyLimit(), func(c *gin.Context) {
		var err error
		got, err = io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(payload)))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, payload, got)
}

// A negative setting falls back to the 512KB default rather than disabling the
// limit, so a typo cannot silently remove the protection.
func TestAnonymousRequestBodyLimitNegativeFallsBackToDefault(t *testing.T) {
	withAnonymousRequestBodyLimitKB(t, -1)
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.POST("/t", AnonymousRequestBodyLimit(), func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	body := bytes.Repeat([]byte("d"), (512<<10)+1)
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body)))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

type failingBodyReader struct{}

func (failingBodyReader) Read([]byte) (int, error) {
	return 0, errors.New("synthetic read failure")
}

// A read failure that is not a size overflow is a client error, not a size
// rejection.
func TestAnonymousRequestBodyLimitReadErrorAbortsBadRequest(t *testing.T) {
	withAnonymousRequestBodyLimitKB(t, 1)
	gin.SetMode(gin.TestMode)

	handlerCalled := false
	engine := gin.New()
	engine.POST("/t", AnonymousRequestBodyLimit(), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/t", failingBodyReader{}))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, handlerCalled)
}

// The group-level ingress wrapper bounds what a handler can read and surfaces
// the failure as a body-too-large error the handler can classify.
func TestRequestBodyIngressLimitBoundsHandlerRead(t *testing.T) {
	withMaxRequestBodyMB(t, 1)
	gin.SetMode(gin.TestMode)

	var readErr error
	engine := gin.New()
	engine.POST("/t", RequestBodyIngressLimit(), func(c *gin.Context) {
		_, readErr = io.ReadAll(c.Request.Body)
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	body := bytes.Repeat([]byte("e"), (1<<20)+1)
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body)))

	require.Error(t, readErr)
	require.True(t, common.IsRequestBodyTooLargeError(readErr), "want a body-too-large error, got %v", readErr)
}

func TestRequestBodyIngressLimitAllowsBodyWithinLimit(t *testing.T) {
	withMaxRequestBodyMB(t, 1)
	gin.SetMode(gin.TestMode)

	payload := bytes.Repeat([]byte("f"), 1<<10)
	var got []byte
	engine := gin.New()
	engine.POST("/t", RequestBodyIngressLimit(), func(c *gin.Context) {
		var err error
		got, err = io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(payload)))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, payload, got)
}

type countingBodyReader struct{ reads int }

func (r *countingBodyReader) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

// The ingress wrapper is lazy: a route whose handler never reads the body must
// not pay for a read, and must not be able to be made to allocate one.
func TestRequestBodyIngressLimitDoesNotReadUnreadBodies(t *testing.T) {
	withMaxRequestBodyMB(t, 1)
	gin.SetMode(gin.TestMode)

	reader := &countingBodyReader{}
	engine := gin.New()
	engine.POST("/t", RequestBodyIngressLimit(), func(c *gin.Context) { c.Status(http.StatusOK) })

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/t", reader))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Zero(t, reader.reads, "the ingress wrapper must not read the body on its own")
}

// 匿名路由的上限必须按解压后的内容计。原来的实现读的是压缩后的原始字节，
// 614KB 的请求体压成 663B 就能绕过 512KB 上限，8MB 的压缩炸弹也能被接受：
// 匿名端点因此可以被用来制造解压放大的 CPU/内存消耗。
func TestAnonymousRequestBodyLimitCountsDecompressedSize(t *testing.T) {
	withAnonymousRequestBodyLimitKB(t, 1)
	gin.SetMode(gin.TestMode)

	handlerCalled := false
	engine := gin.New()
	engine.POST("/t", AnonymousRequestBodyLimit(), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	payload := bytes.Repeat([]byte("z"), (1<<10)+1) // 解压后超过 1KB 上限
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err := zw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.Less(t, compressed.Len(), 1<<10, "前提：压缩后的线上字节数低于上限")

	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code,
		"解压后超过上限的 gzip 请求体必须被拒绝")
	require.False(t, handlerCalled, "超限时 handler 不得运行")
}

// 反向对照：压缩后和解压后都在上限内的 gzip 请求体必须放行，且 handler
// 拿到的是解压后的原始字节。
func TestAnonymousRequestBodyLimitAcceptsSmallGzipBody(t *testing.T) {
	withAnonymousRequestBodyLimitKB(t, 1)
	gin.SetMode(gin.TestMode)

	payload := bytes.Repeat([]byte("y"), 1<<9) // 512B，压缩前后都低于 1KB
	var got []byte
	engine := gin.New()
	engine.POST("/t", AnonymousRequestBodyLimit(), func(c *gin.Context) {
		var readErr error
		got, readErr = io.ReadAll(c.Request.Body)
		require.NoError(t, readErr)
		c.Status(http.StatusOK)
	})

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err := zw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, payload, got, "handler 必须看到解压后的原始字节")
}

// 无法解析的压缩体是客户端错误，按 400 拒绝而不是放行。
func TestAnonymousRequestBodyLimitRejectsUndecodableGzip(t *testing.T) {
	withAnonymousRequestBodyLimitKB(t, 1)
	gin.SetMode(gin.TestMode)

	handlerCalled := false
	engine := gin.New()
	engine.POST("/t", AnonymousRequestBodyLimit(), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader([]byte("not a gzip stream")))
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.False(t, handlerCalled)
}
