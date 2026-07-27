package middleware

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

const (
	codexLocalResponsesCompactionIngressMaxBodyBytes = 768 << 20
	codexLocalResponsesCompactionIngressMinFreeBytes = 128 << 20
)

type codexLocalResponsesCompactionIngressStorageOptions struct {
	MaxBodyBytes int64
	MaxDiskBytes int64
	MinFreeBytes int64
}

var codexLocalResponsesCompactionIngressOptions = codexLocalResponsesCompactionIngressStorageOptions{
	MaxBodyBytes: codexLocalResponsesCompactionIngressMaxBodyBytes,
	MinFreeBytes: codexLocalResponsesCompactionIngressMinFreeBytes,
}

// CodexLocalResponsesCompactionIngress authenticates first at the router level,
// then decompresses and force-spools only the exact local Codex Responses
// compaction shape. The body is never reduced or replaced with visible content.
func CodexLocalResponsesCompactionIngress() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.IsCodexLocalResponsesCompactionRequest(c.Request) || c.Request.Body == nil {
			c.Next()
			return
		}

		storage, err := spoolCodexLocalResponsesCompactionBody(c)
		if err != nil {
			abortCodexLocalResponsesCompactionIngress(c, err)
			return
		}

		c.Set(common.KeyBodyStorage, storage)
		common.SetContextKey(c, constant.ContextKeyRequestBodySize, storage.Size())
		c.Request.Body = io.NopCloser(storage)
		c.Request.ContentLength = storage.Size()
		c.Request.Header.Set("Content-Length", fmt.Sprintf("%d", storage.Size()))
		c.Request.Header.Del("Content-Encoding")
		c.Next()
	}
}

func spoolCodexLocalResponsesCompactionBody(c *gin.Context) (common.BodyStorage, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, fmt.Errorf("codex local compaction request body is required")
	}

	body := c.Request.Body
	defer body.Close()
	decoded, closeDecoded, err := decodeCodexLocalResponsesCompactionBody(body, c.GetHeader("Content-Encoding"))
	if err != nil {
		return nil, err
	}
	defer closeDecoded()

	options := codexLocalResponsesCompactionIngressOptions
	if options.MaxDiskBytes <= 0 {
		options.MaxDiskBytes = common.GetDiskCacheMaxSizeBytes()
	}
	return common.CreateRequiredDiskBodyStorageFromReader(decoded, common.RequiredDiskBodyStorageOptions{
		MaxBytes:     options.MaxBodyBytes,
		MaxDiskBytes: options.MaxDiskBytes,
		MinFreeBytes: options.MinFreeBytes,
	})
}

func decodeCodexLocalResponsesCompactionBody(body io.ReadCloser, contentEncoding string) (io.Reader, func() error, error) {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "", "identity":
		return body, func() error { return nil }, nil
	case "gzip":
		reader, err := gzip.NewReader(body)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid gzip request body: %w", err)
		}
		return reader, reader.Close, nil
	case "br":
		return brotli.NewReader(body), func() error { return nil }, nil
	case "zstd":
		reader, err := zstd.NewReader(body)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid zstd request body: %w", err)
		}
		return reader, func() error {
			reader.Close()
			return nil
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported Content-Encoding %q for local Codex compaction", contentEncoding)
	}
}

func abortCodexLocalResponsesCompactionIngress(c *gin.Context, err error) {
	statusCode := http.StatusBadRequest
	errorCode := types.ErrorCodeInvalidRequest
	switch {
	case common.IsRequestBodyTooLargeError(err):
		statusCode = http.StatusRequestEntityTooLarge
		errorCode = types.ErrorCodeReadRequestBodyFailed
	case errors.Is(err, common.ErrRequiredDiskBodyStorageCapacity),
		errors.Is(err, common.ErrRequiredDiskBodyStorageReserve),
		errors.Is(err, common.ErrRequiredDiskBodyStorageWrite):
		statusCode = http.StatusInsufficientStorage
		errorCode = types.ErrorCodeReadRequestBodyFailed
	}
	abortWithOpenAiMessage(c, statusCode, err.Error(), errorCode)
}
