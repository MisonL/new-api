package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

// AnonymousRequestBodyLimit bounds the request body a route reachable without
// authentication will buffer. Those routes live under /api, which has no
// request-body ceiling of its own beyond the 128MB ingress backstop, so each
// one mounts this tighter ceiling explicitly (ANONYMOUS_REQUEST_BODY_LIMIT_KB,
// default 512KB; 0 disables it).
//
// The ceiling applies to the DECOMPRESSED body. /api never receives
// DecompressRequestMiddleware, so a compressed body arrives still encoded
// here; measuring the wire bytes would let a 600KB body compressed to 663
// bytes through, and an 8MB compression bomb onto an anonymous endpoint. The
// body is decoded per Content-Encoding, read up to the ceiling, and replaced
// with a re-readable copy of the plaintext so handlers downstream see the same
// bytes with Content-Encoding removed and a matching ContentLength. An
// oversized body is rejected with 413 before the handler runs; an undecodable
// or otherwise unreadable body is a client error and aborts with 400.
func AnonymousRequestBodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		maxBytes := common.GetAnonymousRequestBodyLimitBytes()
		if maxBytes <= 0 || c.Request.Body == nil {
			c.Next()
			return
		}

		originalBody := c.Request.Body
		encoding := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Encoding")))
		decodedBody, decodeErr := decodeAnonymousRequestBody(originalBody, encoding)
		if decodeErr != nil {
			_ = originalBody.Close()
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		limitedBody, err := readAnonymousRequestBody(decodedBody, maxBytes)
		_ = decodedBody.Close()
		_ = originalBody.Close()
		if err != nil {
			if common.IsRequestBodyTooLargeError(err) {
				c.AbortWithStatus(http.StatusRequestEntityTooLarge)
				return
			}
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		c.Request.Body = io.NopCloser(bytes.NewReader(limitedBody))
		c.Request.ContentLength = int64(len(limitedBody))
		switch encoding {
		case "gzip", "br", "zstd":
			// The body is now plaintext; leaving the header would make handlers
			// try to decode it a second time.
			c.Request.Header.Del("Content-Encoding")
		}
		c.Next()
	}
}

// decodeAnonymousRequestBody wraps body with the decoder named by encoding.
// Unknown or absent encodings pass the body through unchanged; a decoder that
// cannot be constructed is a client error.
func decodeAnonymousRequestBody(body io.ReadCloser, encoding string) (io.ReadCloser, error) {
	switch encoding {
	case "gzip":
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			return nil, err
		}
		return &readCloser{
			Reader:  gzipReader,
			closeFn: func() error { return gzipReader.Close() },
		}, nil
	case "br":
		return &readCloser{
			Reader:  brotli.NewReader(body),
			closeFn: func() error { return nil },
		}, nil
	case "zstd":
		reader, err := zstd.NewReader(body)
		if err != nil {
			return nil, err
		}
		return &readCloser{
			Reader:  reader,
			closeFn: func() error { reader.Close(); return nil },
		}, nil
	default:
		return body, nil
	}
}

// RequestBodyIngressLimit bounds every request body in the group it is
// registered on with common.RequestBodyIngressLimit (MAX_REQUEST_BODY_MB,
// default 128MB) — the same ceiling DecompressRequestMiddleware installs for
// the relay routes. The /api and /dashboard groups are registered before that
// middleware is installed and never receive it, so this closes the gap for
// /api.
//
// It is lazy: the wrapper reads nothing and allocates nothing until a handler
// reads the body, so routes that ignore the body pay nothing, and a handler
// that reads past the ceiling sees a *http.MaxBytesError it can classify with
// common.IsRequestBodyTooLargeError.
func RequestBodyIngressLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil {
			c.Next()
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, common.RequestBodyIngressLimit(c))
		c.Next()
	}
}

func readAnonymousRequestBody(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, common.ErrRequestBodyTooLarge
	}
	return data, nil
}
