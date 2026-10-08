package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// AnonymousRequestBodyLimit bounds the request body a route reachable without
// authentication will buffer. Those routes live under /api, which has no
// request-body ceiling of its own beyond the 128MB ingress backstop, so each
// one mounts this tighter ceiling explicitly (ANONYMOUS_REQUEST_BODY_LIMIT_KB,
// default 512KB; 0 disables it).
//
// The body is read once, up to the ceiling, and replaced with a re-readable
// copy so handlers downstream still see the same bytes and a ContentLength
// that matches them. An oversized body is rejected with 413 before the handler
// runs; any other read failure is a client error and aborts with 400.
func AnonymousRequestBodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		maxBytes := common.GetAnonymousRequestBodyLimitBytes()
		if maxBytes <= 0 || c.Request.Body == nil {
			c.Next()
			return
		}

		originalBody := c.Request.Body
		limitedBody, err := readAnonymousRequestBody(originalBody, maxBytes)
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
		c.Next()
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
