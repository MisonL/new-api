package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type spyRequestBodyStorage struct {
	*bytes.Reader
	data      []byte
	bytesCall int
}

func newSpyRequestBodyStorage(data []byte) *spyRequestBodyStorage {
	return &spyRequestBodyStorage{
		Reader: bytes.NewReader(data),
		data:   data,
	}
}

func (s *spyRequestBodyStorage) Close() error {
	return nil
}

func (s *spyRequestBodyStorage) Bytes() ([]byte, error) {
	s.bytesCall++
	return s.data, nil
}

func (s *spyRequestBodyStorage) Size() int64 {
	return int64(len(s.data))
}

func (s *spyRequestBodyStorage) IsDisk() bool {
	return true
}

func TestGetModelFromJSONBodyPreservesRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"model":"gpt-5.4","group":"vip","messages":[{"role":"user","content":"hi"}]}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

	got, err := getModelFromRequest(ctx)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", got.Model)
	require.Equal(t, "vip", got.Group)

	remaining, err := io.ReadAll(ctx.Request.Body)
	require.NoError(t, err)
	require.JSONEq(t, body, string(remaining))
}

func TestGetModelFromJSONBodyAvoidsBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

	storage := newSpyRequestBodyStorage([]byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"model":"gpt-5.4"}`))
	ctx.Set(common.KeyBodyStorage, storage)

	got, err := getModelFromRequest(ctx)
	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", got.Model)
	require.Equal(t, 0, storage.bytesCall)
}

func TestGetModelFromJSONBodyRejectsNonStringModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":123}`))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

	_, err := getModelFromRequest(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "field model must be a string")
}
