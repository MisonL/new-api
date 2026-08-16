package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const codexLocalResponsesCompactionMetadata = `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`
const codexOrdinaryResponsesTurnMetadata = `{"installation_id":"install-test","session_id":"session-test","thread_id":"thread-test","turn_id":"turn-test","window_id":"thread-test:0","request_kind":"turn","sandbox":"danger-full-access","turn_started_at_unix_ms":1234567890}`

const codexLocalResponsesCompactionIngressConcurrentSpoolWait = 10 * time.Second

func TestCodexLocalResponsesCompactionIngressSpoolsCompressedPayloadAfterAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 256, 512, 0)

	payload := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"reasoning","encrypted_content":"opaque-state"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}],"tools":[{"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"lark","definition":"start: WORD"}}]}`)
	expectedDigest := sha256.Sum256(payload)

	for _, tc := range []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "identity", body: payload},
		{name: "gzip", encoding: "gzip", body: gzipCodexLocalIngressPayload(t, payload)},
		{name: "br", encoding: "br", body: compressBrotliForTest(t, payload)},
		{name: "zstd", encoding: "zstd", body: compressZstdForTest(t, payload)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotStorageDisk bool
			var gotDigest [sha256.Size]byte
			var storagePathCountBeforeCleanup int

			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.Use(func(c *gin.Context) {
				c.Set("authenticated", true)
				c.Next()
			})
			router.Use(CodexLocalResponsesCompactionIngress())
			router.Use(commonBodyStorageCleanupForTest())
			router.POST("/v1/responses", func(c *gin.Context) {
				storage, err := common.GetBodyStorage(c)
				require.NoError(t, err)
				gotStorageDisk = storage.IsDisk()
				_, err = storage.Seek(0, io.SeekStart)
				require.NoError(t, err)
				gotDigest, err = digestReader(storage)
				require.NoError(t, err)
				entries, err := os.ReadDir(common.GetDiskCacheDir())
				require.NoError(t, err)
				storagePathCountBeforeCleanup = len(entries)
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusNoContent, recorder.Code)
			require.True(t, gotStorageDisk)
			require.Equal(t, expectedDigest, gotDigest)
			require.Equal(t, 1, storagePathCountBeforeCleanup)
			entries, err := os.ReadDir(common.GetDiskCacheDir())
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestSpoolCodexLocalResponsesCompactionBodyUsesUncompressedContentLengthAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 16, 0)

	payload := []byte("12345678")
	releaseReaders := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseReaders) })
	}
	bodies := []*blockingCodexLocalCompactionBody{
		{started: make(chan struct{}), release: releaseReaders, payload: payload},
		{started: make(chan struct{}), release: releaseReaders, payload: payload},
	}

	results := make(chan error, len(bodies))
	storages := make([]common.BodyStorage, 0, len(bodies))
	var storagesMu sync.Mutex
	var waitGroup sync.WaitGroup
	for _, body := range bodies {
		waitGroup.Add(1)
		go func(body *blockingCodexLocalCompactionBody) {
			defer waitGroup.Done()
			testContext, _ := gin.CreateTestContext(httptest.NewRecorder())
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			request.Body = body
			request.ContentLength = int64(len(payload))
			testContext.Request = request
			storage, err := spoolCodexLocalResponsesCompactionBody(testContext)
			if storage != nil {
				storagesMu.Lock()
				storages = append(storages, storage)
				storagesMu.Unlock()
			}
			results <- err
		}(body)
	}
	t.Cleanup(func() {
		release()
		waitGroup.Wait()
		storagesMu.Lock()
		defer storagesMu.Unlock()
		for _, storage := range storages {
			_ = storage.Close()
		}
	})

	for _, body := range bodies {
		select {
		case <-body.started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for uncompressed local Codex compaction bodies to spool concurrently")
		}
	}

	release()
	waitGroup.Wait()
	for range bodies {
		require.NoError(t, <-results)
	}
	storagesMu.Lock()
	defer storagesMu.Unlock()
	require.Len(t, storages, len(bodies))
	for _, storage := range storages {
		require.Equal(t, int64(len(payload)), storage.Size())
	}
}

func TestSpoolCodexLocalResponsesCompactionBodyTreatsGzipLengthAsUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 64, 0)

	releaseFirstBody := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseFirstBody) })
	}
	firstBody := &blockingCodexLocalCompactionBody{
		started: make(chan struct{}),
		release: releaseFirstBody,
		payload: []byte("first"),
	}
	var firstStorage common.BodyStorage
	var firstErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		firstStorage, firstErr = common.CreateRequiredDiskBodyStorageFromReaderWithContext(
			context.Background(),
			firstBody,
			common.RequiredDiskBodyStorageOptions{
				MaxBytes:     64 << 20,
				MaxDiskBytes: 64 << 20,
				MinFreeBytes: 0,
			},
		)
	}()
	t.Cleanup(func() {
		release()
		waitGroup.Wait()
		if firstStorage != nil {
			_ = firstStorage.Close()
		}
	})

	select {
	case <-firstBody.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unknown-length body storage admission")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	payload := gzipCodexLocalIngressPayload(t, []byte(`{"model":"gpt-5.6-sol","input":"continue"}`))
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload)).WithContext(ctx)
	request.Header.Set("Content-Encoding", "gzip")
	testContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	testContext.Request = request

	storage, err := spoolCodexLocalResponsesCompactionBody(testContext)

	require.Nil(t, storage)
	require.ErrorIs(t, err, context.Canceled)
	release()
	waitGroup.Wait()
	require.NoError(t, firstErr)
	require.NotNil(t, firstStorage)
	require.NoError(t, firstStorage.Close())
	firstStorage = nil
	requireNoCodexLocalResponsesCompactionIngressFiles(t)
}

func TestCodexLocalResponsesCompactionIngressStoresOpaqueResponsesMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 64, 0)

	payload := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"reasoning","encrypted_content":"opaque-state"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	var metadata common.ResponsesRequestMetadata
	metadataFound := false
	var replayedBody []byte

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(CodexLocalResponsesCompactionIngress())
	router.Use(commonBodyStorageCleanupForTest())
	router.POST("/v1/responses", func(c *gin.Context) {
		value, ok := c.Get(string(constant.ContextKeyResponsesRequestMetadata))
		metadata, metadataFound = value.(common.ResponsesRequestMetadata)
		require.True(t, ok)
		var err error
		replayedBody, err = io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.True(t, metadataFound)
	require.Equal(t, "gpt-5.6-sol", metadata.Model)
	require.NotNil(t, metadata.Stream)
	require.True(t, *metadata.Stream)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Len(t, metadata.EncryptedContentFingerprints, 1)
	require.NotEqual(t, "opaque-state", metadata.EncryptedContentFingerprints[0])
	require.Equal(t, payload, replayedBody)
}

func TestCodexLocalResponsesCompactionIngressSpoolsOrdinaryCodexOpaqueResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 64, 0)

	payload := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"reasoning","encrypted_content":"opaque-state"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}],"future_top_level":{"preserve":true}}`)
	var metadata common.ResponsesRequestMetadata
	var metadataFound bool
	var storageIsDisk bool
	var replayedBody []byte

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("authenticated", true)
		c.Next()
	})
	router.Use(CodexLocalResponsesCompactionIngress())
	router.Use(commonBodyStorageCleanupForTest())
	router.POST("/v1/responses", func(c *gin.Context) {
		metadata, metadataFound = common.GetResponsesRequestMetadata(c)
		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		storageIsDisk = storage.IsDisk()
		replayedBody, err = io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codex-Turn-Metadata", codexOrdinaryResponsesTurnMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.True(t, metadataFound)
	require.True(t, storageIsDisk)
	require.Equal(t, "gpt-5.6-sol", metadata.Model)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Equal(t, payload, replayedBody)
	requireNoCodexLocalResponsesCompactionIngressFiles(t)
}

func TestCodexLocalResponsesCompactionIngressLeavesNonCodexResponsesUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := &countingCodexLocalIngressBody{payload: []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque-state"}]}`)}

	router := gin.New()
	router.Use(CodexLocalResponsesCompactionIngress())
	router.POST("/v1/responses", func(c *gin.Context) {
		_, bodyStorageExists := c.Get(common.KeyBodyStorage)
		require.False(t, bodyStorageExists)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Zero(t, body.reads)
}

func TestCodexLocalResponsesCompactionIngressDefersCompressedOrdinaryCodexBodyUntilAfterAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := gzipCodexLocalIngressPayload(t, []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"opaque-state"}]}`))
	body := &countingCodexLocalIngressBody{payload: payload}

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	router.Use(CodexLocalResponsesCompactionIngress())
	router.POST("/v1/responses", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-Codex-Turn-Metadata", codexOrdinaryResponsesTurnMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Zero(t, body.reads)
}

func TestCodexLocalResponsesCompactionIngressSpoolsCompressedOrdinaryCodexOpaqueResponsesAfterAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 64, 0)

	payload := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":[{"type":"reasoning","encrypted_content":"opaque-state"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}],"future_top_level":{"preserve":true}}`)
	var metadata common.ResponsesRequestMetadata
	var storageIsDisk bool
	var replayedBody []byte

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("authenticated", true)
		c.Next()
	})
	router.Use(CodexLocalResponsesCompactionIngress())
	router.Use(commonBodyStorageCleanupForTest())
	router.POST("/v1/responses", func(c *gin.Context) {
		metadata, _ = common.GetResponsesRequestMetadata(c)
		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		storageIsDisk = storage.IsDisk()
		replayedBody, err = io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(gzipCodexLocalIngressPayload(t, payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-Codex-Turn-Metadata", codexOrdinaryResponsesTurnMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.True(t, storageIsDisk)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Equal(t, payload, replayedBody)
	require.Empty(t, req.Header.Get("Content-Encoding"))
	require.Equal(t, int64(len(payload)), req.ContentLength)
	requireNoCodexLocalResponsesCompactionIngressFiles(t)
}

func TestCodexLocalResponsesCompactionIngressSpoolsDirectResponsesCompactEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 64, 0)

	payload := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"compaction","encrypted_content":{"version":2,"ciphertext":"opaque-state"},"future_state":{"preserve":true}}],"future_top_level":{"preserve":true}}`)
	var metadata common.ResponsesRequestMetadata
	var storageIsDisk bool

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(CodexLocalResponsesCompactionIngress())
	router.Use(commonBodyStorageCleanupForTest())
	router.POST("/v1/responses/compact", func(c *gin.Context) {
		var ok bool
		metadata, ok = common.GetResponsesRequestMetadata(c)
		require.True(t, ok)
		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		storageIsDisk = storage.IsDisk()
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.True(t, storageIsDisk)
	require.Equal(t, "gpt-5.6-sol", metadata.Model)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.True(t, metadata.HasCompactionItem)
	require.Equal(t, int64(len(payload)), metadata.BodySize)
	requireNoCodexLocalResponsesCompactionIngressFiles(t)
}

func TestCodexLocalResponsesCompactionIngressPreservesDirectCompactAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	payload := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"compaction","encrypted_content":{"version":2,"ciphertext":"opaque-state"},"future_state":{"preserve":true}}],"future_top_level":{"preserve":true}}`)

	for _, path := range []string{"/v1/responses/compact", "/responses/compact"} {
		for _, testCase := range []struct {
			name     string
			encoding string
			body     []byte
		}{
			{name: "identity", body: payload},
			{name: "gzip", encoding: "gzip", body: gzipCodexLocalIngressPayload(t, payload)},
			{name: "brotli", encoding: "br", body: compressBrotliForTest(t, payload)},
			{name: "zstd", encoding: "zstd", body: compressZstdForTest(t, payload)},
		} {
			t.Run(path+"/"+testCase.name, func(t *testing.T) {
				cacheDir := t.TempDir()
				configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 64, 0)

				var metadata common.ResponsesRequestMetadata
				var storageIsDisk bool
				var replayedBody []byte
				router := gin.New()
				router.Use(DecompressRequestMiddleware())
				router.Use(CodexLocalResponsesCompactionIngress())
				router.Use(commonBodyStorageCleanupForTest())
				router.POST(path, func(c *gin.Context) {
					var ok bool
					metadata, ok = common.GetResponsesRequestMetadata(c)
					require.True(t, ok)
					storage, err := common.GetBodyStorage(c)
					require.NoError(t, err)
					storageIsDisk = storage.IsDisk()
					replayedBody, err = io.ReadAll(c.Request.Body)
					require.NoError(t, err)
					c.Status(http.StatusNoContent)
				})

				req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(testCase.body))
				req.Header.Set("Content-Type", "application/json")
				if testCase.encoding != "" {
					req.Header.Set("Content-Encoding", testCase.encoding)
				}
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, req)

				require.Equal(t, http.StatusNoContent, recorder.Code)
				require.True(t, storageIsDisk)
				require.Equal(t, payload, replayedBody)
				require.Empty(t, req.Header.Get("Content-Encoding"))
				require.Equal(t, int64(len(payload)), req.ContentLength)
				require.Equal(t, "gpt-5.6-sol", metadata.Model)
				require.True(t, metadata.HasOpaqueEncryptedContent)
				require.True(t, metadata.HasCompactionItem)
				require.Equal(t, int64(len(payload)), metadata.BodySize)
				requireNoCodexLocalResponsesCompactionIngressFiles(t)
			})
		}
	}
}

func TestCodexLocalResponsesCompactionIngressRejectsInvalidJSONAfterSpoolAndCleansStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 64, 0)

	called := 0
	router := newCodexLocalResponsesCompactionIngressTestRouter(&called)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":[}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, called)
	requireNoCodexLocalResponsesCompactionIngressFiles(t)
}

func TestCodexLocalResponsesCompactionIngressRejectsCorruptCompressionBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name     string
		encoding string
		body     string
	}{
		{name: "gzip", encoding: "gzip", body: "not-a-gzip-stream"},
		{name: "brotli", encoding: "br", body: "not-a-brotli-stream"},
		{name: "zstd", encoding: "zstd", body: "not-a-zstd-stream"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cacheDir := t.TempDir()
			configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 256, 512, 0)

			called := 0
			router := newCodexLocalResponsesCompactionIngressTestRouter(&called)
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(testCase.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", testCase.encoding)
			req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Zero(t, called)
			requireNoCodexLocalResponsesCompactionIngressFiles(t)
		})
	}
}

func TestCodexLocalResponsesCompactionIngressRejectsUnsupportedCompressionBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 256, 512, 0)

	called := 0
	router := newCodexLocalResponsesCompactionIngressTestRouter(&called)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":"checkpoint"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "deflate")
	req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, called)
	requireNoCodexLocalResponsesCompactionIngressFiles(t)
}

func TestCodexLocalResponsesCompactionIngressRejectsCapacityExhaustionBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 8, 0)

	called := 0
	router := newCodexLocalResponsesCompactionIngressTestRouter(&called)

	payload := append([]byte(`{"model":"gpt-5.6-sol","input":"`), bytes.Repeat([]byte("x"), 9<<20)...)
	payload = append(payload, []byte(`"}`)...)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusInsufficientStorage, recorder.Code)
	require.Zero(t, called)
	requireNoCodexLocalResponsesCompactionIngressFiles(t)
}

func TestCodexLocalResponsesCompactionIngressRejectsSingleBodyLimitBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 8, 64, 0)

	called := 0
	router := newCodexLocalResponsesCompactionIngressTestRouter(&called)
	payload := append([]byte(`{"model":"gpt-5.6-sol","input":"`), bytes.Repeat([]byte("x"), 9<<20)...)
	payload = append(payload, []byte(`"}`)...)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	require.Zero(t, called)
	requireNoCodexLocalResponsesCompactionIngressFiles(t)
}

func TestCodexLocalResponsesCompactionIngressRejectsUnwritableSpoolBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cachePathFile, err := os.CreateTemp(t.TempDir(), "not-a-directory")
	require.NoError(t, err)
	require.NoError(t, cachePathFile.Close())
	configureCodexLocalResponsesCompactionIngressForTest(t, cachePathFile.Name(), 64, 64, 0)

	called := 0
	router := newCodexLocalResponsesCompactionIngressTestRouter(&called)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":"continue"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusInsufficientStorage, recorder.Code)
	require.Zero(t, called)
}

func TestCodexLocalResponsesCompactionIngressEnforcesAggregateCapacityConcurrently(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 12, 0)

	releaseHandler := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseHandler) })
	}
	enteredHandler := make(chan struct{}, 2)
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("authenticated", true)
		c.Next()
	})
	router.Use(CodexLocalResponsesCompactionIngress())
	router.Use(commonBodyStorageCleanupForTest())
	router.POST("/v1/responses", func(c *gin.Context) {
		enteredHandler <- struct{}{}
		<-releaseHandler
		c.Status(http.StatusNoContent)
	})

	payload := append([]byte(`{"model":"gpt-5.6-sol","input":"`), bytes.Repeat([]byte("x"), 8<<20)...)
	payload = append(payload, []byte(`"}`)...)

	results := make(chan int, 2)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	t.Cleanup(func() {
		release()
		waitGroup.Wait()
	})
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			results <- recorder.Code
		}()
	}
	close(start)

	timer := time.NewTimer(codexLocalResponsesCompactionIngressConcurrentSpoolWait)
	defer timer.Stop()
	statusCodes := make([]int, 0, 2)
	handlerEntered := false
	for !handlerEntered && len(statusCodes) < 2 {
		select {
		case <-enteredHandler:
			handlerEntered = true
		case statusCode := <-results:
			statusCodes = append(statusCodes, statusCode)
		case <-timer.C:
			release()
			waitGroup.Wait()
			t.Fatalf("timed out waiting for concurrent spool result with statuses %v and disk stats %+v", statusCodes, common.GetDiskCacheStats())
		}
	}
	if !handlerEntered {
		release()
		waitGroup.Wait()
		t.Fatalf("expected one concurrent spool to reach the handler, got statuses %v with disk stats %+v", statusCodes, common.GetDiskCacheStats())
	}
	if len(statusCodes) == 0 {
		select {
		case statusCode := <-results:
			statusCodes = append(statusCodes, statusCode)
		case <-timer.C:
			release()
			waitGroup.Wait()
			t.Fatalf("timed out waiting for rejected concurrent spool result with disk stats %+v", common.GetDiskCacheStats())
		}
	}
	require.Equal(t, http.StatusInsufficientStorage, statusCodes[0])
	release()
	waitGroup.Wait()
	close(results)

	for statusCode := range results {
		statusCodes = append(statusCodes, statusCode)
	}
	require.ElementsMatch(t, []int{http.StatusNoContent, http.StatusInsufficientStorage}, statusCodes)
}

func TestCodexLocalResponsesCompactionIngressAllowsConcurrentBodiesWithinAggregateCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cacheDir := t.TempDir()
	configureCodexLocalResponsesCompactionIngressForTest(t, cacheDir, 64, 16, 0)

	releaseHandler := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseHandler) })
	}
	enteredHandler := make(chan struct{}, 2)
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("authenticated", true)
		c.Next()
	})
	router.Use(CodexLocalResponsesCompactionIngress())
	router.Use(commonBodyStorageCleanupForTest())
	router.POST("/v1/responses", func(c *gin.Context) {
		enteredHandler <- struct{}{}
		<-releaseHandler
		c.Status(http.StatusNoContent)
	})

	payload := append([]byte(`{"model":"gpt-5.6-sol","input":"`), bytes.Repeat([]byte("x"), 6<<20)...)
	payload = append(payload, []byte(`"}`)...)
	results := make(chan int, 2)
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	t.Cleanup(func() {
		release()
		waitGroup.Wait()
	})
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Codex-Turn-Metadata", codexLocalResponsesCompactionMetadata)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			results <- recorder.Code
		}()
	}
	close(start)

	for range 2 {
		select {
		case <-enteredHandler:
		case <-time.After(codexLocalResponsesCompactionIngressConcurrentSpoolWait):
			release()
			waitGroup.Wait()
			t.Fatalf("timed out waiting for concurrent spool within aggregate capacity with disk stats %+v", common.GetDiskCacheStats())
		}
	}
	release()
	waitGroup.Wait()
	close(results)

	statusCodes := make([]int, 0, 2)
	for statusCode := range results {
		statusCodes = append(statusCodes, statusCode)
	}
	require.ElementsMatch(t, []int{http.StatusNoContent, http.StatusNoContent}, statusCodes)
}

func configureCodexLocalResponsesCompactionIngressForTest(t *testing.T, cachePath string, maxBodyMB, maxDiskMB int64, minFreeBytes int64) {
	t.Helper()
	previous := codexLocalResponsesCompactionIngressOptions
	previousDiskCacheConfig := common.GetDiskCacheConfig()
	codexLocalResponsesCompactionIngressOptions = codexLocalResponsesCompactionIngressStorageOptions{
		MaxBodyBytes: int64(maxBodyMB) << 20,
		MaxDiskBytes: int64(maxDiskMB) << 20,
		MinFreeBytes: minFreeBytes,
	}
	common.SetDiskCacheConfig(common.DiskCacheConfig{Path: cachePath})
	common.SyncDiskCacheStats()
	t.Cleanup(func() {
		codexLocalResponsesCompactionIngressOptions = previous
		common.SetDiskCacheConfig(previousDiskCacheConfig)
		common.SyncDiskCacheStats()
	})
}

func newCodexLocalResponsesCompactionIngressTestRouter(called *int) *gin.Engine {
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(func(c *gin.Context) {
		c.Set("authenticated", true)
		c.Next()
	})
	router.Use(CodexLocalResponsesCompactionIngress())
	router.POST("/v1/responses", func(c *gin.Context) {
		*called = *called + 1
		c.Status(http.StatusNoContent)
	})
	return router
}

func requireNoCodexLocalResponsesCompactionIngressFiles(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(common.GetDiskCacheDir())
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	require.NoError(t, err)
	require.Empty(t, entries)
}

func commonBodyStorageCleanupForTest() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		common.CleanupBodyStorage(c)
	}
}

type blockingCodexLocalCompactionBody struct {
	started chan struct{}
	release <-chan struct{}
	payload []byte
	offset  int
}

type countingCodexLocalIngressBody struct {
	payload []byte
	offset  int
	reads   int
}

func (b *countingCodexLocalIngressBody) Read(target []byte) (int, error) {
	b.reads++
	if b.offset >= len(b.payload) {
		return 0, io.EOF
	}
	n := copy(target, b.payload[b.offset:])
	b.offset += n
	return n, nil
}

func (b *countingCodexLocalIngressBody) Close() error {
	return nil
}

func (b *blockingCodexLocalCompactionBody) Read(target []byte) (int, error) {
	if b.offset >= len(b.payload) {
		return 0, io.EOF
	}
	if b.offset == 0 {
		close(b.started)
		<-b.release
	}
	n := copy(target, b.payload[b.offset:])
	b.offset += n
	return n, nil
}

func (b *blockingCodexLocalCompactionBody) Close() error {
	return nil
}

func digestReader(reader io.Reader) ([sha256.Size]byte, error) {
	hash := sha256.New()
	_, err := io.Copy(hash, reader)
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, err
}

func gzipCodexLocalIngressPayload(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}
