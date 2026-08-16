package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

type countingReadCloser struct {
	reader io.Reader
	reads  int
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	r.reads++
	return r.reader.Read(p)
}

func (r *countingReadCloser) Close() error {
	return nil
}

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

func TestDecompressRequestMiddlewareDefersExactCodexLocalResponsesCompactionBeforeRejectedAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := []byte(`{"model":"gpt-5.6-sol","input":"checkpoint"}`)
	testCases := []struct {
		name     string
		encoding string
		body     []byte
	}{
		{name: "identity", body: payload},
		{name: "gzip", encoding: "gzip", body: compressGzipForTest(t, payload)},
		{name: "br", encoding: "br", body: compressBrotliForTest(t, payload)},
		{name: "zstd", encoding: "zstd", body: compressZstdForTest(t, payload)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(DecompressRequestMiddleware())
			router.Use(func(c *gin.Context) {
				c.AbortWithStatus(http.StatusUnauthorized)
			})
			router.POST("/v1/responses", func(c *gin.Context) {
				t.Fatal("handler should not run after rejected authentication")
			})

			body := &countingReadCloser{reader: bytes.NewReader(tc.body)}
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			req.Body = body
			req.ContentLength = -1
			req.Header.Set("Content-Type", "application/json")
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			req.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses"}}`)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("unexpected status code: %d", recorder.Code)
			}
			if body.reads != 0 {
				t.Fatalf("expected authentication rejection before reading %s body, got %d reads", tc.name, body.reads)
			}
			if got := req.Header.Get("Content-Encoding"); got != tc.encoding {
				t.Fatalf("expected Content-Encoding %q to remain untouched before authentication, got %q", tc.encoding, got)
			}
		})
	}
}

func TestDecompressRequestMiddlewareDoesNotDeferRemoteCodexCompaction(t *testing.T) {
	gin.SetMode(gin.TestMode)

	payload := []byte(`{"model":"gpt-5.6-sol","input":"checkpoint"}`)
	compressed := compressGzipForTest(t, payload)
	router := gin.New()
	router.Use(DecompressRequestMiddleware())
	router.Use(func(c *gin.Context) {
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	router.POST("/v1/responses", func(c *gin.Context) {
		t.Fatal("handler should not run after rejected authentication")
	})

	body := &countingReadCloser{reader: bytes.NewReader(compressed)}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Body = body
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("X-Codex-Turn-Metadata", `{"request_kind":"compaction","compaction":{"implementation":"responses_compaction_v2"}}`)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status code: %d", recorder.Code)
	}
	if body.reads == 0 {
		t.Fatal("expected remote compaction request to retain the existing decompression path")
	}
	if got := req.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("expected remote compaction Content-Encoding to be removed, got %q", got)
	}
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
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("write gzip payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func compressBrotliForTest(t *testing.T, payload []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := brotli.NewWriter(&buf)
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("write brotli payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close brotli writer: %v", err)
	}
	return buf.Bytes()
}
