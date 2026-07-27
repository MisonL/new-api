package middleware

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const codexLocalResponsesCompactionMetadata = `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`

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
	<-enteredHandler
	secondStatusCode := <-results
	require.Equal(t, http.StatusInsufficientStorage, secondStatusCode)
	close(releaseHandler)
	waitGroup.Wait()
	close(results)

	statusCodes := []int{secondStatusCode}
	for statusCode := range results {
		statusCodes = append(statusCodes, statusCode)
	}
	require.ElementsMatch(t, []int{http.StatusNoContent, http.StatusInsufficientStorage}, statusCodes)
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
