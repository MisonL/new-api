package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmitResponseBufferDelaysCommitUntilFlush(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	original := c.Writer
	buffer := newTaskSubmitResponseBufferWriter(original)
	c.Writer = buffer
	c.Header("X-Task", "buffered")
	c.JSON(http.StatusCreated, gin.H{"task_id": "task_test"})
	c.Writer = original

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Body.Bytes())

	response := buffer.snapshot()
	require.Equal(t, http.StatusCreated, response.StatusCode)
	require.Equal(t, "buffered", response.Header.Get("X-Task"))
	require.Contains(t, string(response.Body), "task_test")
	require.NoError(t, response.FlushTo(c.Writer))
	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Contains(t, recorder.Body.String(), "task_test")
}

func TestTaskSubmitResponseBufferDiscardDoesNotWrite(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	original := c.Writer
	buffer := newTaskSubmitResponseBufferWriter(original)
	c.Writer = buffer
	c.JSON(http.StatusOK, gin.H{"task_id": "discarded"})
	c.Writer = original

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Empty(t, recorder.Body.Bytes())
}

func TestTaskSubmitResponseBufferUsesFirstStatusCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	buffer := newTaskSubmitResponseBufferWriter(c.Writer)

	buffer.WriteHeader(http.StatusCreated)
	buffer.WriteHeader(http.StatusBadGateway)
	_, err := buffer.Write([]byte("task"))

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, buffer.Status())
	require.NoError(t, buffer.snapshot().FlushTo(c.Writer))
	require.Equal(t, http.StatusCreated, recorder.Code)
	require.Equal(t, "task", recorder.Body.String())
}

func TestBoundedTaskSubmitResponseBodyRejectsOversizedPayload(t *testing.T) {
	payload := strings.NewReader(strings.Repeat("x", int(maxTaskSubmitResponseBodyBytes)+1))
	body := &boundedTaskSubmitResponseBody{reader: payload, remaining: maxTaskSubmitResponseBodyBytes}
	data, err := io.ReadAll(body)
	require.Error(t, err)
	require.True(t, errors.Is(err, errTaskSubmitResponseBodyTooLarge))
	require.Len(t, data, int(maxTaskSubmitResponseBodyBytes))
}

func TestBoundedTaskSubmitResponseBodyAcceptsExactLimit(t *testing.T) {
	payload := strings.NewReader(strings.Repeat("x", int(maxTaskSubmitResponseBodyBytes)))
	body := &boundedTaskSubmitResponseBody{reader: payload, remaining: maxTaskSubmitResponseBodyBytes}
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Len(t, data, int(maxTaskSubmitResponseBodyBytes))
}

func TestNormalizeTaskSubmitResponseErrorMarksStructuralFailureAmbiguous(t *testing.T) {
	err := normalizeTaskSubmitResponseError(
		service.TaskErrorWrapper(errors.New("invalid json"), "unmarshal_response_failed", http.StatusInternalServerError),
		"task_public",
	)
	require.NotNil(t, err)
	require.True(t, err.LocalError)
	require.True(t, err.NoRefund)
	require.Equal(t, "ambiguous_task_submission", err.Code)
	require.Equal(t, http.StatusBadGateway, err.StatusCode)
	require.Contains(t, err.Message, "task_public")
}

func TestNormalizeTaskSubmitResponseErrorPreservesProviderRejection(t *testing.T) {
	original := service.TaskErrorWrapper(errors.New("provider rejected request"), "ali_api_error", http.StatusCreated)
	err := normalizeTaskSubmitResponseError(original, "task_public")
	require.Same(t, original, err)
	require.False(t, err.NoRefund)
}

func TestNormalizeTaskSubmitResponseErrorKeepsExistingAmbiguousError(t *testing.T) {
	original := &dto.TaskError{Code: "ambiguous_task_submission", NoRefund: true}
	err := normalizeTaskSubmitResponseError(original, "task_public")
	require.Same(t, original, err)
	require.True(t, err.LocalError)
}

func TestAmbiguousTaskSubmitErrorIsLocalAndNonRetryable(t *testing.T) {
	err := ambiguousTaskSubmitError(http.StatusNetworkAuthenticationRequired, "task_public")

	require.True(t, err.LocalError)
	require.True(t, err.NoRefund)
	require.Equal(t, "ambiguous_task_submission", err.Code)
	require.Equal(t, http.StatusNetworkAuthenticationRequired, err.StatusCode)
	require.Contains(t, err.Message, "task_public")
}

func TestAmbiguousTaskSubmitErrorNormalizesCloudflareTimeout(t *testing.T) {
	err := ambiguousTaskSubmitError(524, "task_public")

	require.Equal(t, http.StatusGatewayTimeout, err.StatusCode)
	require.True(t, err.LocalError)
	require.True(t, err.NoRefund)
}

type taskSubmitTimeoutTestError struct{}

func (taskSubmitTimeoutTestError) Error() string   { return "task submit timed out" }
func (taskSubmitTimeoutTestError) Timeout() bool   { return true }
func (taskSubmitTimeoutTestError) Temporary() bool { return true }

func TestAmbiguousTaskSubmitTransportErrorRecognizesTimeouts(t *testing.T) {
	deadlineErr := fmt.Errorf("wrapped deadline: %w", context.DeadlineExceeded)
	require.True(t, isAmbiguousTaskSubmitTransportError(deadlineErr))
	require.Equal(t, http.StatusGatewayTimeout, ambiguousTaskSubmitStatusCode(deadlineErr))

	netTimeoutErr := fmt.Errorf("wrapped network timeout: %w", taskSubmitTimeoutTestError{})
	require.True(t, isAmbiguousTaskSubmitTransportError(netTimeoutErr))
	require.Equal(t, http.StatusGatewayTimeout, ambiguousTaskSubmitStatusCode(netTimeoutErr))

	require.False(t, isAmbiguousTaskSubmitTransportError(errors.New("request validation failed")))
}
