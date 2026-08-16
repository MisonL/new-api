package middleware

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type spyRequestBodyStorage struct {
	*bytes.Reader
	data      []byte
	bytesCall int
	readCalls int
}

type opaqueModelRequestBody struct {
	readCalls int
}

func (b *opaqueModelRequestBody) Read(_ []byte) (int, error) {
	b.readCalls++
	return 0, errors.New("opaque request HTTP body must not be decoded while selecting a channel")
}

func (b *opaqueModelRequestBody) Close() error {
	return nil
}

func setDistributorTestGinMode(t *testing.T) {
	t.Helper()
	previousGinMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		gin.SetMode(previousGinMode)
	})
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

func (s *spyRequestBodyStorage) Read(p []byte) (int, error) {
	s.readCalls++
	return s.Reader.Read(p)
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
	setDistributorTestGinMode(t)
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
	setDistributorTestGinMode(t)
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

func TestGetModelFromJSONBodyUsesOpaqueResponsesMetadataWithoutRescanning(t *testing.T) {
	setDistributorTestGinMode(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{}`))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

	storage := newSpyRequestBodyStorage([]byte(`{"model":"gpt-5.6-sol","group":"priority","input":[{"type":"reasoning","encrypted_content":"opaque-state"}]}`))
	ctx.Set(common.KeyBodyStorage, storage)
	common.SetResponsesRequestMetadata(ctx, common.ResponsesRequestMetadata{
		Model:                     "gpt-5.6-sol",
		ModelPresent:              true,
		Group:                     "priority",
		InputPresent:              true,
		InputKind:                 common.ResponsesJSONValueKindArray,
		HasOpaqueEncryptedContent: true,
		BodySize:                  storage.Size(),
	})

	got, err := getModelFromJSONBody(ctx)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", got.Model)
	require.Equal(t, "priority", got.Group)
	require.Zero(t, storage.readCalls)
	require.Zero(t, storage.bytesCall)
}

func TestGetModelFromRequestUsesOpaqueResponsesMetadataBeforeContentTypeDispatch(t *testing.T) {
	setDistributorTestGinMode(t)

	for _, testCase := range []struct {
		name        string
		contentType string
	}{
		{name: "JSON content type", contentType: gin.MIMEJSON},
		{name: "plain text content type", contentType: "text/plain"},
		{name: "missing content type"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			requestBody := &opaqueModelRequestBody{}
			ctx.Request.Body = requestBody
			if testCase.contentType != "" {
				ctx.Request.Header.Set("Content-Type", testCase.contentType)
			}

			storage := newSpyRequestBodyStorage([]byte(`{"model":"gpt-5.6-sol","group":"priority","input":[{"type":"reasoning","encrypted_content":"opaque-state"}]}`))
			ctx.Set(common.KeyBodyStorage, storage)
			common.SetResponsesRequestMetadata(ctx, common.ResponsesRequestMetadata{
				Model:                     "gpt-5.6-sol",
				ModelPresent:              true,
				Group:                     "priority",
				InputPresent:              true,
				InputKind:                 common.ResponsesJSONValueKindArray,
				HasOpaqueEncryptedContent: true,
				BodySize:                  storage.Size(),
			})

			got, err := getModelFromRequest(ctx)

			require.NoError(t, err)
			require.Equal(t, "gpt-5.6-sol", got.Model)
			require.Equal(t, "priority", got.Group)
			require.Zero(t, requestBody.readCalls)
			require.Zero(t, storage.readCalls)
			require.Zero(t, storage.bytesCall)
		})
	}
}

func TestGetModelRequestUsesCompactModelSuffixForResponsesCompactAliases(t *testing.T) {
	setDistributorTestGinMode(t)

	for _, path := range []string{"/v1/responses/compact", "/responses/compact"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5.6-sol"}`))
			ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

			modelRequest, shouldSelectChannel, err := getModelRequest(ctx)

			require.NoError(t, err)
			require.True(t, shouldSelectChannel)
			require.Equal(t, ratio_setting.WithCompactModelSuffix("gpt-5.6-sol"), modelRequest.Model)
		})
	}
}

func TestGetModelFromJSONBodyRejectsNonStringModel(t *testing.T) {
	setDistributorTestGinMode(t)
	require.NoError(t, i18n.Init())
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":123}`))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

	_, err := getModelFromRequest(ctx)
	require.Error(t, err)
	require.Contains(t, err.Error(), "field model must be a string")
}
