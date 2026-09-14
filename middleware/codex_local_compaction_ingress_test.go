package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const codexLocalCompactionMetadata = `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`

func TestCodexLocalCompactionIngressReductionPreservesSmallAndNonLocalBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"ORIGINAL_BODY_MUST_SURVIVE"}]}],"tools":[{"type":"function","name":"keep"}]}`)

	for _, testCase := range []struct {
		name     string
		path     string
		metadata string
	}{
		{name: "small local compaction", path: "/v1/responses", metadata: codexLocalCompactionMetadata},
		{name: "small local compaction compatibility alias", path: "/responses", metadata: codexLocalCompactionMetadata},
		{name: "ordinary responses", path: "/v1/responses"},
		{name: "remote compaction", path: "/v1/responses", metadata: `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`},
		{name: "wrong endpoint", path: "/v1/responses/compact", metadata: codexLocalCompactionMetadata},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			router.Use(CodexLocalCompactionIngressReduction())
			router.POST("/*path", func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, original, body)
				require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCodexLocalCompactionIngressReduced))
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodPost, testCase.path, bytes.NewReader(original))
			if testCase.metadata != "" {
				request.Header.Set("X-Codex-Turn-Metadata", testCase.metadata)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func TestCodexLocalCompactionIngressReductionRejectsAnOversizedInputItem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := 0
	router := gin.New()
	router.Use(CodexLocalCompactionIngressReduction())
	router.POST("/v1/responses", func(c *gin.Context) {
		called++
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		newOversizedCodexLocalCompactionInputItemReader(service.CodexLocalCompaction413RecoverySafeBodyMaxBytes+1),
	)
	request.Header.Set("X-Codex-Turn-Metadata", codexLocalCompactionMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Zero(t, called)
	require.Contains(t, recorder.Body.String(), "input item exceeds")
}

func TestCodexLocalCompactionIngressReductionRejectsOpaqueEncryptedReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := 0
	router := gin.New()
	router.Use(CodexLocalCompactionIngressReduction())
	router.POST("/v1/responses", func(c *gin.Context) {
		called++
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		newOversizedCodexLocalCompactionOpaqueReplayReader(service.CodexLocalCompaction413RecoverySafeBodyMaxBytes+1),
	)
	request.Header.Set("X-Codex-Turn-Metadata", codexLocalCompactionMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Zero(t, called)
	require.Contains(t, recorder.Body.String(), "opaque encrypted_content")
}

func TestCodexResponsesOpaqueReplayIngressPreservesDirectAndOrdinaryRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = previousMaxRequestBodyMB
	})
	previousConfig := common.GetDiskCacheConfig()
	cachePath := t.TempDir()
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 1,
		MaxSizeMB:   64,
		Path:        cachePath,
	})
	common.ResetDiskCacheUsage()
	t.Cleanup(func() {
		common.SetDiskCacheConfig(previousConfig)
		common.SyncDiskCacheStats()
	})

	payload := []byte(`{"model":"gpt-5.5","input":"` + strings.Repeat("x", 2<<20) + `"}`)
	for _, testCase := range []struct {
		name          string
		path          string
		metadata      string
		wantStatus    int
		expectHandler bool
	}{
		{name: "direct compact", path: "/v1/responses/compact", wantStatus: http.StatusNoContent, expectHandler: true},
		{name: "compact compatibility alias", path: "/responses/compact", wantStatus: http.StatusNoContent, expectHandler: true},
		{name: "ordinary Codex turn", path: "/v1/responses", metadata: `{"request_kind":"turn"}`, wantStatus: http.StatusRequestEntityTooLarge},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			called := 0
			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.Use(func(c *gin.Context) {
				c.Set("token_id", 1)
				c.Next()
			})
			router.Use(CodexResponsesOpaqueReplayIngress())
			router.Use(BodyStorageCleanup())
			router.POST("/*path", func(c *gin.Context) {
				called++
				storage, err := common.GetBodyStorage(c)
				require.NoError(t, err)
				require.True(t, storage.IsDisk())
				body, err := io.ReadAll(storage)
				require.NoError(t, err)
				require.Equal(t, payload, body)
				c.Status(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodPost, testCase.path, bytes.NewReader(payload))
			request.Header.Set("Content-Type", gin.MIMEJSON)
			if testCase.metadata != "" {
				request.Header.Set("X-Codex-Turn-Metadata", testCase.metadata)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			require.Equal(t, testCase.wantStatus, recorder.Code)
			if testCase.expectHandler {
				require.Equal(t, 1, called)
			} else {
				require.Zero(t, called)
			}
		})
	}
}

func TestCodexResponsesOpaqueReplayIngressDecodesDeferredCompressedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousConfig := common.GetDiskCacheConfig()
	common.SetDiskCacheConfig(common.DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 1,
		MaxSizeMB:   64,
		Path:        t.TempDir(),
	})
	common.ResetDiskCacheUsage()
	t.Cleanup(func() {
		common.SetDiskCacheConfig(previousConfig)
		common.SyncDiskCacheStats()
	})

	payload := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"compressed"}]}`)
	for _, testCase := range []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "gzip", encoding: "gzip", body: compressGzipForTest(t, payload)},
		{name: "brotli", encoding: "br", body: compressBrotliForTest(t, payload)},
		{name: "zstd", encoding: "zstd", body: compressZstdForTest(t, payload)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.Use(func(c *gin.Context) {
				c.Set("token_id", 1)
				c.Next()
			})
			router.Use(CodexResponsesOpaqueReplayIngress())
			router.Use(BodyStorageCleanup())
			router.POST("/v1/responses/compact", func(c *gin.Context) {
				storage, err := common.GetBodyStorage(c)
				require.NoError(t, err)
				body, err := io.ReadAll(storage)
				require.NoError(t, err)
				require.Equal(t, payload, body)
				require.Empty(t, c.GetHeader("Content-Encoding"))
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(testCase.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", testCase.encoding)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func TestCodexResponsesOpaqueReplayIngressRejectsUnsupportedDeferredEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	called := 0
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("token_id", 1)
		c.Next()
	})
	router.Use(CodexResponsesOpaqueReplayIngress())
	router.POST("/v1/responses/compact", func(c *gin.Context) {
		called++
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewBufferString(`{"model":"gpt-5.5"}`))
	request.Header.Set("Content-Type", gin.MIMEJSON)
	request.Header.Set("Content-Encoding", "compress")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, called)
	require.Contains(t, recorder.Body.String(), "unsupported Content-Encoding")
}

func newOversizedCodexLocalCompactionInputItemReader(mediaBytes int) io.Reader {
	return io.MultiReader(
		strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"custom_tool_call_output","call_id":"call_large_image","output":[{"type":"input_image","image_url":"data:image/png;base64,`),
		&testRepeatedByteReader{remaining: int64(mediaBytes), value: 'A'},
		strings.NewReader(`"}]}]}`),
	)
}

func newOversizedCodexLocalCompactionOpaqueReplayReader(prefixBytes int) io.Reader {
	return io.MultiReader(
		strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"tools":[{"type":"function","name":"large_schema","description":"`),
		&testRepeatedByteReader{remaining: int64(prefixBytes), value: 'A'},
		strings.NewReader(`"}],"input":[{"type":"reasoning","encrypted_content":"opaque-reasoning-state"},{"type":"message","role":"user","content":"continue"}]}`),
	)
}

type testRepeatedByteReader struct {
	remaining int64
	value     byte
}

func (reader *testRepeatedByteReader) Read(data []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(data)) > reader.remaining {
		data = data[:reader.remaining]
	}
	for index := range data {
		data[index] = reader.value
	}
	reader.remaining -= int64(len(data))
	return len(data), nil
}
