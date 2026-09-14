package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestDecompressRequestMiddlewareSupportsZstd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := []byte(`{"model":"gpt-5.5","input":"hello"}`)
	compressed := compressZstdForTest(t, payload)

	for _, encoding := range []string{"zstd", "ZSTD"} {
		t.Run(encoding, func(t *testing.T) {
			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.POST("/v1/responses", func(c *gin.Context) {
				body, err := io.ReadAll(c.Request.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				if string(body) != string(payload) {
					t.Fatalf("unexpected decompressed body: %s", string(body))
				}
				if got := c.GetHeader("Content-Encoding"); got != "" {
					t.Fatalf("expected Content-Encoding to be removed, got %q", got)
				}
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressed))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Content-Encoding", encoding)
			recorder := httptest.NewRecorder()

			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusNoContent {
				t.Fatalf("unexpected status code: %d", recorder.Code)
			}
		})
	}
}

func TestDecompressRequestMiddlewareRejectsInvalidZstd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/v1/responses", func(c *gin.Context) {
		t.Fatal("handler should not run for invalid zstd body")
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "zstd")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: %d", recorder.Code)
	}
}

func TestDeferredCodexLocalCompactionBodyDoesNotReadBeforeAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	metadata := `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`
	payload := []byte(`{"input":"` + strings.Repeat("x", 2<<20) + `"}`)

	for _, tc := range []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "identity", body: payload},
		{name: "gzip", encoding: "gzip", body: compressGzipForTest(t, payload)},
		{name: "br", encoding: "br", body: compressBrotliForTest(t, payload)},
		{name: "zstd", encoding: "zstd", body: compressZstdForTest(t, payload)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &countingReadCloser{Reader: bytes.NewReader(tc.body)}
			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.POST("/v1/responses", func(c *gin.Context) {
				require.Error(t, ActivateCodexLocalCompactionBody(c))
				require.Zero(t, tracker.bytesRead)
				c.Status(http.StatusUnauthorized)
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			req.Body = tracker
			req.ContentLength = int64(len(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Codex-Turn-Metadata", metadata)
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			require.Zero(t, tracker.bytesRead)
		})
	}
}

func TestDeferredCodexLocalCompactionBodyExtendsLimitOnlyAfterTokenAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = previousMaxRequestBodyMB
	})

	metadata := `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`
	payload := []byte(`{"input":"` + strings.Repeat("x", 2<<20) + `"}`)

	for _, tc := range []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "identity", body: payload},
		{name: "gzip", encoding: "gzip", body: compressGzipForTest(t, payload)},
		{name: "br", encoding: "br", body: compressBrotliForTest(t, payload)},
		{name: "zstd", encoding: "zstd", body: compressZstdForTest(t, payload)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.Use(func(c *gin.Context) {
				c.Set("token_id", 1)
				c.Next()
			})
			router.POST("/v1/responses", func(c *gin.Context) {
				require.NoError(t, ActivateCodexLocalCompactionBody(c))
				body, err := io.ReadAll(c.Request.Body)
				require.NoError(t, err)
				require.Equal(t, payload, body)
				require.Empty(t, c.GetHeader("Content-Encoding"))
				c.Status(http.StatusNoContent)
			})

			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Codex-Turn-Metadata", metadata)
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusNoContent, recorder.Code)
		})
	}
}

func TestCodexLocalCompactionLargeBodyRequiresTokenAuthBeforeActivation(t *testing.T) {
	setupAuthMiddlewareTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.Ability{}))
	// The application initializes model column names during database startup.
	// Exercise the same lazy model path for this isolated SQLite test database.
	_ = model.GetGroupEnabledModels("default")
	user := createAuthMiddlewareTestUser(t, "codex-compaction-token-user", common.RoleCommonUser, common.UserStatusEnabled, "default")
	token := &model.Token{
		UserId:         user.Id,
		Key:            "codexcompactionlargebodytoken",
		Status:         common.TokenStatusEnabled,
		Name:           "codex-compaction-large-body",
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		Group:          "default",
	}
	require.NoError(t, model.DB.Create(token).Error)

	previousMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = previousMaxRequestBodyMB
	})

	metadata := `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`
	payload := []byte(`{"model":"gpt-5.6-sol","input":"` + strings.Repeat("x", 2<<20) + `"}`)

	newRouter := func() *gin.Engine {
		router := gin.New()
		router.Use(DecompressRequestMiddleware())
		router.Use(TokenAuth())
		router.Use(CodexLocalCompactionIngressReduction())
		router.POST("/v1/responses", func(c *gin.Context) {
			body, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, payload, body)
			c.Status(http.StatusNoContent)
		})
		return router
	}

	t.Run("invalid token leaves body unread", func(t *testing.T) {
		tracker := &countingReadCloser{Reader: bytes.NewReader(payload)}
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		request.Body = tracker
		request.ContentLength = int64(len(payload))
		request.Header.Set("Authorization", "Bearer sk-invalidtoken")
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Codex-Turn-Metadata", metadata)
		recorder := httptest.NewRecorder()

		newRouter().ServeHTTP(recorder, request)

		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		require.Zero(t, tracker.bytesRead)
	})

	t.Run("valid token activates bounded large body", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
		request.Header.Set("Authorization", "Bearer sk-"+token.Key)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Codex-Turn-Metadata", metadata)
		recorder := httptest.NewRecorder()

		newRouter().ServeHTTP(recorder, request)

		require.Equal(t, http.StatusNoContent, recorder.Code)
	})
}

func TestDecompressRequestMiddlewareKeepsDefaultLimitForOrdinaryRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousMaxRequestBodyMB := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() {
		constant.MaxRequestBodyMB = previousMaxRequestBodyMB
	})

	payload := []byte(`{"input":"` + strings.Repeat("x", 2<<20) + `"}`)
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.POST("/v1/responses", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		require.Error(t, err)
		require.True(t, common.IsRequestBodyTooLargeError(err))
		c.Status(http.StatusRequestEntityTooLarge)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func compressZstdForTest(t *testing.T, payload []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("create zstd writer: %v", err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("write zstd payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zstd writer: %v", err)
	}
	return buf.Bytes()
}

func compressGzipForTest(t *testing.T, payload []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.Bytes()
}

func compressBrotliForTest(t *testing.T, payload []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := brotli.NewWriter(&buf)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.Bytes()
}

type countingReadCloser struct {
	io.Reader
	bytesRead int64
}

func (reader *countingReadCloser) Read(data []byte) (int, error) {
	n, err := reader.Reader.Read(data)
	reader.bytesRead += int64(n)
	return n, err
}

func (reader *countingReadCloser) Close() error {
	return nil
}
