package common

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

func TestInitPayloadAuditCapturesRequestPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-5.4","input":"hello"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	InitPayloadAudit(ctx, true, false)

	record, ok := GetContextKeyType[PayloadAuditRecord](ctx, constant.ContextKeyLogRequestPayload)
	if !ok {
		t.Fatal("expected request payload record")
	}
	if !strings.Contains(record.Content, `"input":"hello"`) {
		t.Fatalf("unexpected request payload: %s", record.Content)
	}
	if record.ContentType != "application/json" {
		t.Fatalf("unexpected request content type: %s", record.ContentType)
	}
}

func TestAppendPayloadAuditFieldsCapturesResponsePayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"input":"hello"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	InitPayloadAudit(ctx, false, true)
	ctx.Writer.Header().Set("Content-Type", "application/json")
	_, _ = ctx.Writer.Write([]byte(`{"output":"world"}`))

	other := AppendPayloadAuditFields(ctx, map[string]interface{}{})
	if other["response_content"] == nil {
		t.Fatal("expected response content in payload audit fields")
	}
	if !strings.Contains(other["response_content"].(string), `"output":"world"`) {
		t.Fatalf("unexpected response content: %v", other["response_content"])
	}
}

func TestBuildPayloadAuditRecordOmitsBinaryPayload(t *testing.T) {
	record := buildPayloadAuditRecord([]byte{0x00, 0x01, 0x02}, 3, "application/octet-stream", false)
	if record == nil {
		t.Fatal("expected payload audit record")
	}
	if !record.Omitted {
		t.Fatal("expected binary payload to be omitted")
	}
	if !strings.Contains(record.Content, "body omitted") {
		t.Fatalf("unexpected omitted payload summary: %s", record.Content)
	}
}

type spyBodyStorage struct {
	*bytes.Reader
	data      []byte
	bytesCall int
	seekCall  int
	seekErrAt int
	seekErr   error
}

func newSpyBodyStorage(data []byte) *spyBodyStorage {
	return &spyBodyStorage{
		Reader: bytes.NewReader(data),
		data:   data,
	}
}

func (s *spyBodyStorage) Close() error {
	return nil
}

func (s *spyBodyStorage) Seek(offset int64, whence int) (int64, error) {
	s.seekCall++
	if s.seekErrAt > 0 && s.seekCall == s.seekErrAt {
		return 0, s.seekErr
	}
	return s.Reader.Seek(offset, whence)
}

func (s *spyBodyStorage) Bytes() ([]byte, error) {
	s.bytesCall++
	return s.data, nil
}

func (s *spyBodyStorage) Size() int64 {
	return int64(len(s.data))
}

func (s *spyBodyStorage) IsDisk() bool {
	return true
}

func TestBuildPayloadAuditRecordFromStorageReadsPreviewOnly(t *testing.T) {
	largePayload := []byte(`{"input":"` + strings.Repeat("a", payloadAuditMaxPreviewBytes+1024) + `"}`)
	storage := newSpyBodyStorage(largePayload)

	record := buildPayloadAuditRecordFromStorage(storage, "application/json")
	if record == nil {
		t.Fatal("expected payload audit record")
	}
	if storage.bytesCall != 0 {
		t.Fatalf("expected preview reader to avoid Bytes(), got %d calls", storage.bytesCall)
	}
	if !record.Truncated {
		t.Fatal("expected large payload preview to be marked truncated")
	}
	if record.Bytes != int64(len(largePayload)) {
		t.Fatalf("unexpected payload size: %d", record.Bytes)
	}
	if !strings.Contains(record.Content, "[truncated, original size:") {
		t.Fatalf("expected truncated marker, got %s", record.Content)
	}
}

func TestUnmarshalBodyReusableDiskJSONAvoidsBytes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	storage := newSpyBodyStorage([]byte(`{"model":"gpt-5.4","stream":true}`))
	ctx.Set(KeyBodyStorage, storage)

	var got struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := UnmarshalBodyReusable(ctx, &got); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if storage.bytesCall != 0 {
		t.Fatalf("expected disk JSON decode to avoid Bytes(), got %d calls", storage.bytesCall)
	}
	if got.Model != "gpt-5.4" || !got.Stream {
		t.Fatalf("unexpected decoded body: %+v", got)
	}
}

func TestUnmarshalBodyReusablePreservesDecodeErrorWhenResetFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	resetErr := errors.New("forced reset failure")
	storage := newSpyBodyStorage([]byte(`{"model":`))
	storage.seekErrAt = 3
	storage.seekErr = resetErr
	ctx.Set(KeyBodyStorage, storage)

	var got map[string]any
	err := UnmarshalBodyReusable(ctx, &got)
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
	if !strings.Contains(err.Error(), "unexpected EOF") {
		t.Fatalf("expected JSON decode error to be preserved, got %v", err)
	}
	if errors.Is(err, resetErr) {
		t.Fatalf("expected reset error to remain context only, got %v", err)
	}
}
