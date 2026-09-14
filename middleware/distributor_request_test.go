package middleware

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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

func (s *spyRequestBodyStorage) NewReader() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
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

func TestGetModelRequestCapturesTaskRouteIntentBeforeViduSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"client-vidu","prompt":"reference","images":["one","two","three"]}`))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

	request, shouldSelect, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.True(t, shouldSelect)
	require.Equal(t, "client-vidu", request.Model)
	intent, ok := common.GetContextKeyType[relaycommon.TaskRouteIntent](ctx, constant.ContextKeyTaskRouteIntent)
	require.True(t, ok)
	require.Equal(t, relaycommon.TaskRouteIntent{Action: constant.TaskActionReferenceGenerate}, intent)
}

func TestGetModelRequestCapturesProviderSpecificObjectImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"agnes-video","prompt":"image","image":{"url":"https://image.example/input.png"}}`))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

	request, shouldSelect, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.True(t, shouldSelect)
	require.Equal(t, "agnes-video", request.Model)
	intent, ok := common.GetContextKeyType[relaycommon.TaskRouteIntent](ctx, constant.ContextKeyTaskRouteIntent)
	require.True(t, ok)
	require.Equal(t, relaycommon.TaskRouteIntent{Action: constant.TaskActionGenerate}, intent)
}

func TestGetModelRequestCountsMultipartInputReferenceFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "sora-2"))
	require.NoError(t, writer.WriteField("prompt", "image"))
	part, err := writer.CreateFormFile("input_reference", "input.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())

	_, shouldSelect, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.True(t, shouldSelect)
	intent, ok := common.GetContextKeyType[relaycommon.TaskRouteIntent](ctx, constant.ContextKeyTaskRouteIntent)
	require.True(t, ok)
	require.Equal(t, relaycommon.TaskRouteIntent{Action: constant.TaskActionGenerate}, intent)
}

func TestGetModelRequestCapturesURLFormTaskRouteIntent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("model=vidu-model&prompt=reference&images=one&images=two&images=three"))
	ctx.Request.Header.Set("Content-Type", gin.MIMEPOSTForm)

	request, shouldSelect, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.True(t, shouldSelect)
	require.Equal(t, "vidu-model", request.Model)
	intent, ok := common.GetContextKeyType[relaycommon.TaskRouteIntent](ctx, constant.ContextKeyTaskRouteIntent)
	require.True(t, ok)
	require.Equal(t, relaycommon.TaskRouteIntent{Action: constant.TaskActionReferenceGenerate}, intent)
}

func TestGetModelRequestPreservesMultipartTopLevelAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "vidu-model"))
	require.NoError(t, writer.WriteField("prompt", "reference"))
	require.NoError(t, writer.WriteField("action", constant.TaskActionGenerate))
	require.NoError(t, writer.WriteField("images", "one"))
	require.NoError(t, writer.WriteField("images", "two"))
	require.NoError(t, writer.WriteField("images", "three"))
	require.NoError(t, writer.Close())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &body)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())

	_, shouldSelect, err := getModelRequest(ctx)

	require.NoError(t, err)
	require.True(t, shouldSelect)
	intent, ok := common.GetContextKeyType[relaycommon.TaskRouteIntent](ctx, constant.ContextKeyTaskRouteIntent)
	require.True(t, ok)
	require.Equal(t, relaycommon.TaskRouteIntent{Action: constant.TaskActionGenerate, ExplicitAction: true}, intent)
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

func TestGetModelFromRequestUsesOpaqueMetadataWithoutReadingBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, contentType := range []string{gin.MIMEJSON, "text/plain", ""} {
		t.Run(contentType, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			if contentType != "" {
				ctx.Request.Header.Set("Content-Type", contentType)
			}
			storage := newSpyRequestBodyStorage([]byte(`{"model":"gpt-5.5","input":[{"type":"reasoning","encrypted_content":"opaque-state"}]}`))
			ctx.Set(common.KeyBodyStorage, storage)
			common.SetResponsesRequestMetadata(ctx, common.ResponsesRequestMetadata{
				Model:                     "gpt-5.5",
				ModelPresent:              true,
				HasOpaqueEncryptedContent: true,
				BodySize:                  storage.Size(),
			})

			got, err := getModelFromRequest(ctx)
			require.NoError(t, err)
			require.Equal(t, &ModelRequest{Model: "gpt-5.5"}, got)
			require.Zero(t, storage.bytesCall)
		})
	}
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

func TestGetModelRequestAddsCompactSuffixForBothPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, path := range []string{"/v1/responses/compact", "/responses/compact"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5.5"}`))
			ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)

			got, _, err := getModelRequest(ctx)
			require.NoError(t, err)
			require.Equal(t, "gpt-5.5-openai-compact", got.Model)
		})
	}
}

func TestValidateTokenModelAccessSkipsEmptyModelForTaskQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{"gpt-5.5": true})

	require.NoError(t, ValidateTokenModelAccess(ctx, ""))
}

func TestValidateTokenModelAccessUsesTheSharedModelMatcher(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{"gpt-5.5": true})

	require.NoError(t, ValidateTokenModelAccess(ctx, "gpt-5.5-openai-compact"))
	require.Error(t, ValidateTokenModelAccess(ctx, "claude-opus-4"))
}

func TestValidateTokenModelAccessDistinguishesMissingAndInvalidConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	missing, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(missing, constant.ContextKeyTokenModelLimitEnabled, true)
	require.ErrorIs(t, ValidateTokenModelAccess(missing, "gpt-5.5"), ErrTokenModelAccessMissing)

	invalid, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(invalid, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(invalid, constant.ContextKeyTokenModelLimit, "not-a-limit-map")
	require.ErrorIs(t, ValidateTokenModelAccess(invalid, "gpt-5.5"), ErrTokenModelAccessInvalid)

	require.False(t, errors.Is(ValidateTokenModelAccess(invalid, "gpt-5.5"), ErrTokenModelAccessMissing))
}
