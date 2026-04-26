package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
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
