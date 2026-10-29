package common

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// deadlineRecorder 记录通过 http.ResponseController 设置的写超时。
type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
	calls    int
}

func (r *deadlineRecorder) SetWriteDeadline(t time.Time) error {
	r.deadline = t
	r.calls++
	return nil
}

// 流式响应收尾依赖 http.ResponseController.SetWriteDeadline 给残留协程
// 设置写超时。payload 审计把 c.Writer 换成自己的包装类型后，如果包装没有
// Unwrap，ResponseController 无法穿透到下层 writer，写超时会静默失效。
func TestPayloadAuditWriterPreservesWriteDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(recorder)

	wrapped := &payloadAuditResponseWriter{ResponseWriter: c.Writer}
	controller := http.NewResponseController(wrapped)

	deadline := time.Now().Add(5 * time.Second)
	require.NoError(t, controller.SetWriteDeadline(deadline),
		"SetWriteDeadline must reach the underlying writer through the audit wrapper")
	require.Equal(t, 1, recorder.calls)
	require.WithinDuration(t, deadline, recorder.deadline, time.Millisecond)

	require.NoError(t, controller.SetWriteDeadline(time.Time{}))
	require.Equal(t, 2, recorder.calls)
	require.True(t, recorder.deadline.IsZero())
}

// 审计包装仍必须捕获写入内容，Unwrap 不能破坏原有行为。
func TestPayloadAuditWriterStillCapturesThroughUnwrap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	wrapped := &payloadAuditResponseWriter{ResponseWriter: c.Writer}
	_, err := wrapped.WriteString("hello audit")
	require.NoError(t, err)

	record := wrapped.Record("text/plain")
	require.NotNil(t, record)
	require.Equal(t, "hello audit", record.Content)
	require.Equal(t, "hello audit", recorder.Body.String())
}
