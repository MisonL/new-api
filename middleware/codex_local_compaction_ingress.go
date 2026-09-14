package middleware

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

const (
	responsesOpaqueReplayIngressMaxBodyBytes = int64(common.ResponsesOpaqueReplayIngressMaxMB) << 20
	responsesOpaqueReplayIngressMinFreeBytes = 128 << 20
)

type responsesOpaqueReplayIngressStorageOptions struct {
	MaxBodyBytes int64
	MaxDiskBytes int64
	MinFreeBytes int64
}

var responsesOpaqueReplayIngressOptions = responsesOpaqueReplayIngressStorageOptions{
	MaxBodyBytes: responsesOpaqueReplayIngressMaxBodyBytes,
	MinFreeBytes: responsesOpaqueReplayIngressMinFreeBytes,
}

// CodexResponsesOpaqueReplayIngress authenticates first at the router level,
// then decompresses and force-spools Responses requests that may carry opaque
// encrypted state. Local compaction reduction runs before this middleware; all
// other matching requests retain their original bytes for replay.
func CodexResponsesOpaqueReplayIngress() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.IsResponsesOpaqueReplayIngressRequest(c.Request) || c.Request.Body == nil {
			c.Next()
			return
		}
		if err := ActivateResponsesOpaqueReplayBody(c); err != nil {
			abortResponsesOpaqueReplayIngress(c, fmt.Errorf("activate Responses opaque replay body: %w", err))
			return
		}

		storage, err := spoolResponsesOpaqueReplayBody(c)
		if err != nil {
			abortResponsesOpaqueReplayIngress(c, err)
			return
		}
		metadata, err := common.ScanResponsesRequestMetadata(storage, storage.Size())
		if err != nil {
			_ = storage.Close()
			abortResponsesOpaqueReplayIngress(c, fmt.Errorf("invalid Responses opaque replay request: %w", err))
			return
		}

		c.Set(common.KeyBodyStorage, storage)
		common.SetResponsesRequestMetadata(c, metadata)
		common.SetContextKey(c, constant.ContextKeyRequestBodySize, storage.Size())
		c.Request.Body = io.NopCloser(storage)
		c.Request.ContentLength = storage.Size()
		c.Request.Header.Set("Content-Length", fmt.Sprintf("%d", storage.Size()))
		c.Request.Header.Del("Content-Encoding")
		c.Next()
	}
}

// CodexLocalResponsesCompactionIngress is retained as a compatibility alias
// for callers that used the former broad opaque replay middleware name.
func CodexLocalResponsesCompactionIngress() gin.HandlerFunc {
	return CodexResponsesOpaqueReplayIngress()
}

func spoolResponsesOpaqueReplayBody(c *gin.Context) (common.BodyStorage, error) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, fmt.Errorf("Responses opaque replay request body is required")
	}

	body := c.Request.Body
	defer body.Close()
	contentEncoding := requestBodyContentEncoding(c)
	var decoded io.Reader
	closeDecoded := func() error { return nil }
	if deferred, ok := body.(*deferredResponsesOpaqueReplayBody); ok {
		if !deferred.activated {
			if err := deferred.Activate(c.Writer, responsesOpaqueReplayBodyLimit(c)); err != nil {
				return nil, err
			}
		}
		contentEncoding = deferred.encoding
		decoded = deferred
	} else {
		var err error
		decoded, closeDecoded, err = decodeResponsesOpaqueReplayBody(body, contentEncoding)
		if err != nil {
			return nil, err
		}
	}
	defer closeDecoded()

	options := responsesOpaqueReplayIngressOptions
	if options.MaxDiskBytes <= 0 {
		options.MaxDiskBytes = common.GetDiskCacheMaxSizeBytes()
	}
	expectedBytes := int64(0)
	if (contentEncoding == "" || contentEncoding == "identity") && c.Request.ContentLength > 0 {
		expectedBytes = c.Request.ContentLength
	}
	return common.CreateRequiredDiskBodyStorageFromReaderWithContext(c.Request.Context(), decoded, common.RequiredDiskBodyStorageOptions{
		MaxBytes:      options.MaxBodyBytes,
		MaxDiskBytes:  options.MaxDiskBytes,
		MinFreeBytes:  options.MinFreeBytes,
		ExpectedBytes: expectedBytes,
	})
}

func decodeResponsesOpaqueReplayBody(body io.ReadCloser, contentEncoding string) (io.Reader, func() error, error) {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "", "identity":
		return body, func() error { return nil }, nil
	case "gzip":
		reader, err := gzip.NewReader(body)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid gzip Responses request body: %w", err)
		}
		return reader, reader.Close, nil
	case "br":
		return brotli.NewReader(body), func() error { return nil }, nil
	case "zstd":
		reader, err := zstd.NewReader(body)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid zstd Responses request body: %w", err)
		}
		return reader, func() error {
			reader.Close()
			return nil
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported Content-Encoding %q for Responses opaque replay", contentEncoding)
	}
}

func abortResponsesOpaqueReplayIngress(c *gin.Context, err error) {
	statusCode := http.StatusBadRequest
	errorCode := types.ErrorCodeInvalidRequest
	message := "invalid Responses opaque replay request body"
	switch {
	case common.IsRequestBodyTooLargeError(err):
		statusCode = http.StatusRequestEntityTooLarge
		errorCode = types.ErrorCodeReadRequestBodyFailed
		message = "Responses opaque replay request body is too large"
	case errors.Is(err, common.ErrRequiredDiskBodyStorageCapacity),
		errors.Is(err, common.ErrRequiredDiskBodyStorageReserve),
		errors.Is(err, common.ErrRequiredDiskBodyStorageWrite):
		statusCode = http.StatusInsufficientStorage
		errorCode = types.ErrorCodeReadRequestBodyFailed
		message = "Responses opaque replay request storage is unavailable"
	case strings.Contains(errString(err), "requires token authentication"):
		message = "Responses opaque replay body requires token authentication"
	case strings.Contains(errString(err), "unsupported Content-Encoding"):
		// The encoding name is a safe protocol-level diagnostic; the wrapped
		// error must not be exposed because it may contain implementation detail.
		message = "unsupported Content-Encoding for Responses opaque replay"
	}
	abortWithOpenAiMessage(c, statusCode, message, errorCode)
}

// CodexLocalCompactionIngressReduction reduces only the ordinary Responses
// compaction protocol emitted by local Codex. It runs after token auth and
// before Distributor so model selection and subsequent DTO processing never
// need to materialize an oversized replayed history.
func CodexLocalCompactionIngressReduction() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.IsCodexLocalResponsesCompactionContext(c) || c.Request.Body == nil {
			c.Next()
			return
		}
		if err := ActivateCodexLocalCompactionBody(c); err != nil {
			abortCodexLocalCompactionIngressReduction(c, fmt.Errorf("activate codex local compaction body: %w", err))
			return
		}

		body, reduced, reductionInfo, err := service.PrepareCodexLocalCompactionIngressRequest(c.Request.Body)
		_ = c.Request.Body.Close()
		if err != nil {
			abortCodexLocalCompactionIngressReduction(c, err)
			return
		}

		common.SetContextKey(c, constant.ContextKeyRequestBodySize, int64(len(body)))
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		c.Request.ContentLength = int64(len(body))
		c.Request.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))

		if reduced {
			common.SetContextKey(c, constant.ContextKeyResponsesCodexLocalCompactionIngressReduced, true)
			common.SetContextKey(c, constant.ContextKeyResponsesCodexLocalCompactionIngressOriginalBodyBytes, reductionInfo.OriginalRequestBytes)
			common.SetContextKey(c, constant.ContextKeyResponsesCodexLocalCompactionIngressReducedBodyBytes, int64(len(body)))
			common.SetContextKey(c, constant.ContextKeyResponsesCodexLocalCompactionIngressInputItems, reductionInfo.InputItemCount)
			common.SetContextKey(c, constant.ContextKeyResponsesCodexLocalCompactionBodyReductionSource, "ingress_preemptive_size")
			common.SysLog(fmt.Sprintf(
				"codex local compaction ingress reduction completed: original_request_bytes=%d reduced_request_bytes=%d original_input_bytes=%d input_items=%d visible_parts=%d visible_input_truncated=%t embedded_media_omitted=%t tool_payload_omitted=%t",
				reductionInfo.OriginalRequestBytes,
				len(body),
				reductionInfo.OriginalInputBytes,
				reductionInfo.InputItemCount,
				reductionInfo.VisiblePartCount,
				reductionInfo.VisibleInputTruncated,
				reductionInfo.EmbeddedMediaOmitted,
				reductionInfo.ToolPayloadOmitted,
			))
		}
		c.Next()
	}
}

func abortCodexLocalCompactionIngressReduction(c *gin.Context, err error) {
	statusCode := http.StatusBadRequest
	errorCode := types.ErrorCodeInvalidRequest
	message := "invalid Codex local compaction request body"
	if errors.Is(err, service.ErrCodexLocalCompactionIngressValueTooLarge) ||
		errors.Is(err, service.ErrCodexLocalCompactionOpaqueEncryptedReplay) ||
		common.IsRequestBodyTooLargeError(err) {
		switch {
		case errors.Is(err, service.ErrCodexLocalCompactionOpaqueEncryptedReplay):
			message = "opaque encrypted_content cannot be safely reduced"
		case errors.Is(err, service.ErrCodexLocalCompactionIngressValueTooLarge):
			message = "input item exceeds the allowed size"
		default:
			message = "Codex local compaction request body is too large"
		}
		statusCode = http.StatusRequestEntityTooLarge
		errorCode = types.ErrorCodeReadRequestBodyFailed
	}
	abortWithOpenAiMessage(c, statusCode, message, errorCode)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
