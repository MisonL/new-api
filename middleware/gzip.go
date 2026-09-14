package middleware

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

type readCloser struct {
	io.Reader
	closeFn func() error
}

func (rc *readCloser) Close() error {
	if rc.closeFn != nil {
		return rc.closeFn()
	}
	return nil
}

// deferredResponsesOpaqueReplayBody delays decompression and the large body
// limit until authenticated relay middleware starts reading. The request
// metadata is client-controlled, so it cannot authorize a larger read while
// global middleware is still running before TokenAuth.
type deferredResponsesOpaqueReplayBody struct {
	originalBody io.ReadCloser
	encoding     string
	body         io.ReadCloser
	activated    bool
	closed       bool
}

func newDeferredResponsesOpaqueReplayBody(originalBody io.ReadCloser, encoding string) *deferredResponsesOpaqueReplayBody {
	return &deferredResponsesOpaqueReplayBody{
		originalBody: originalBody,
		encoding:     strings.ToLower(strings.TrimSpace(encoding)),
	}
}

func requestBodyContentEncoding(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if body, ok := c.Request.Body.(*deferredResponsesOpaqueReplayBody); ok && body != nil {
		return body.encoding
	}
	return strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Encoding")))
}

func (body *deferredResponsesOpaqueReplayBody) Activate(responseWriter http.ResponseWriter, maxBytes int64) error {
	if body.closed {
		return io.ErrClosedPipe
	}
	if body.activated {
		return nil
	}
	if maxBytes <= 0 {
		return fmt.Errorf("Responses opaque replay body limit must be positive")
	}

	var decoded io.ReadCloser
	switch body.encoding {
	case "", "identity":
		decoded = body.originalBody
	case "gzip":
		gzipReader, err := gzip.NewReader(body.originalBody)
		if err != nil {
			_ = body.originalBody.Close()
			return err
		}
		decoded = &readCloser{
			Reader: gzipReader,
			closeFn: func() error {
				_ = gzipReader.Close()
				return body.originalBody.Close()
			},
		}
	case "br":
		decoded = &readCloser{
			Reader: brotli.NewReader(body.originalBody),
			closeFn: func() error {
				return body.originalBody.Close()
			},
		}
	case "zstd":
		zstdReader, err := zstd.NewReader(body.originalBody)
		if err != nil {
			_ = body.originalBody.Close()
			return err
		}
		decoded = &readCloser{
			Reader: zstdReader,
			closeFn: func() error {
				zstdReader.Close()
				return body.originalBody.Close()
			},
		}
	default:
		_ = body.originalBody.Close()
		return fmt.Errorf("unsupported Content-Encoding %q for Responses opaque replay", body.encoding)
	}

	body.body = http.MaxBytesReader(responseWriter, decoded, maxBytes)
	body.activated = true
	return nil
}

func (body *deferredResponsesOpaqueReplayBody) Read(data []byte) (int, error) {
	if !body.activated {
		return 0, fmt.Errorf("Responses opaque replay body was read before authentication")
	}
	return body.body.Read(data)
}

func (body *deferredResponsesOpaqueReplayBody) Close() error {
	if body.closed {
		return nil
	}
	body.closed = true
	if body.body != nil {
		return body.body.Close()
	}
	return body.originalBody.Close()
}

func ActivateResponsesOpaqueReplayBody(c *gin.Context) error {
	if c == nil || c.Request == nil {
		return nil
	}
	body, ok := c.Request.Body.(*deferredResponsesOpaqueReplayBody)
	if !ok {
		return nil
	}
	if common.GetContextKeyInt(c, constant.ContextKeyTokenId) <= 0 {
		return fmt.Errorf("Responses opaque replay body requires token authentication")
	}
	return body.Activate(c.Writer, responsesOpaqueReplayBodyLimit(c))
}

func responsesOpaqueReplayBodyLimit(c *gin.Context) int64 {
	if c != nil && c.Request != nil && common.IsResponsesCompactionIngressRequest(c.Request) {
		return int64(common.ResponsesOpaqueReplayIngressMaxMB) << 20
	}
	return common.RequestBodyIngressLimit(c)
}

// ActivateCodexLocalCompactionBody is kept for the local reduction middleware
// and existing callers. The deferred body is shared with the other opaque
// Responses replay paths.
func ActivateCodexLocalCompactionBody(c *gin.Context) error {
	return ActivateResponsesOpaqueReplayBody(c)
}

func DecompressRequestMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil || c.Request.Method == http.MethodGet {
			c.Next()
			return
		}
		encoding := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Encoding")))
		if common.IsResponsesOpaqueReplayIngressRequest(c.Request) {
			c.Request.Body = newDeferredResponsesOpaqueReplayBody(c.Request.Body, encoding)
			switch encoding {
			case "gzip", "br", "zstd":
				c.Request.Header.Del("Content-Encoding")
			}
			c.Next()
			return
		}
		maxBytes := common.RequestBodyIngressLimit(c)

		origBody := c.Request.Body
		wrapMaxBytes := func(body io.ReadCloser) io.ReadCloser {
			return http.MaxBytesReader(c.Writer, body, maxBytes)
		}

		switch encoding {
		case "gzip":
			gzipReader, err := gzip.NewReader(origBody)
			if err != nil {
				_ = origBody.Close()
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			// Replace the request body with the decompressed data, and enforce a max size (post-decompression).
			c.Request.Body = wrapMaxBytes(&readCloser{
				Reader: gzipReader,
				closeFn: func() error {
					_ = gzipReader.Close()
					return origBody.Close()
				},
			})
			c.Request.Header.Del("Content-Encoding")
		case "br":
			reader := brotli.NewReader(origBody)
			c.Request.Body = wrapMaxBytes(&readCloser{
				Reader: reader,
				closeFn: func() error {
					return origBody.Close()
				},
			})
			c.Request.Header.Del("Content-Encoding")
		case "zstd":
			reader, err := zstd.NewReader(origBody)
			if err != nil {
				_ = origBody.Close()
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			var firstByte [1]byte
			n, readErr := reader.Read(firstByte[:])
			if readErr != nil && readErr != io.EOF {
				reader.Close()
				_ = origBody.Close()
				c.AbortWithStatus(http.StatusBadRequest)
				return
			}
			var decompressed io.Reader = reader
			if n > 0 {
				decompressed = io.MultiReader(bytes.NewReader(firstByte[:n]), reader)
			}
			c.Request.Body = wrapMaxBytes(&readCloser{
				Reader: decompressed,
				closeFn: func() error {
					reader.Close()
					return origBody.Close()
				},
			})
			c.Request.Header.Del("Content-Encoding")
		default:
			// Even for uncompressed bodies, enforce a max size to avoid huge request allocations.
			c.Request.Body = wrapMaxBytes(origBody)
		}

		// Continue processing the request
		c.Next()
	}
}
