package sora

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoResponseAcceptsNestedTaskEnvelopeAfterCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{},
		Body: io.NopCloser(bytes.NewReader([]byte(`{
			"code": 0,
			"data": {"id": "video_nested", "status": "queued"}
		}`))),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "video_nested", upstreamID)
	require.JSONEq(t, `{"code":0,"data":{"id":"video_nested","status":"queued"}}`, string(taskData))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"id":"task_public"`)
}

func TestDoResponseStopsRetryForAcceptedResponseWithoutTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"code":0,"message":"accepted"}`))),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Empty(t, upstreamID)
	require.JSONEq(t, `{"code":0,"message":"accepted"}`, string(taskData))
	require.NotNil(t, taskErr)
	require.True(t, taskErr.LocalError)
	require.True(t, taskErr.NoRefund)
	require.Equal(t, "ambiguous_task_submission", taskErr.Code)
	require.Equal(t, http.StatusBadGateway, taskErr.StatusCode)
	require.NotContains(t, taskErr.Message, `"code":0`)
	require.Empty(t, recorder.Body.String())
}

func TestDoResponseTreatsMalformedAcceptedBodyAsAmbiguous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader([]byte(`<html>accepted</html>`))),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Empty(t, upstreamID)
	require.Equal(t, []byte(`<html>accepted</html>`), taskData)
	require.NotNil(t, taskErr)
	require.Equal(t, "ambiguous_task_submission", taskErr.Code)
	require.True(t, taskErr.NoRefund)
	require.Empty(t, recorder.Body.String())
}

func TestDoResponseUsesLocationTaskIDWhenBodyOmitsIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{"Location": []string{"https://provider.example/v1/videos/video_location"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"queued"}`))),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "video_location", upstreamID)
}

func TestDoResponseIgnoresUnrelatedTopLevelIDInFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{},
		Body: io.NopCloser(bytes.NewReader([]byte(`{
			"wrapper": {"id": "request-wrapper-id"},
			"data": {"task_id": "video_nested"}
		}`))),
	}
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "video_nested", upstreamID)
}

func TestDoResponseUsesExplicitLocationQueryTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{"Location": []string{"https://provider.example/submit?task_id=video_query"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"status":"queued"}`))),
	}
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "video_query", upstreamID)
}

func TestExtractSoraTaskIDFromLocationQueryWithoutPath(t *testing.T) {
	require.Equal(t, "video_query_only", extractSoraTaskIDFromLocation("https://provider.example/?id=video_query_only"))
}

func TestEstimateBillingUsesCurrentSoraResolutionPrices(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		size     string
		wantSize float64
	}{
		{name: "sora 2 pro 720p", size: "1280x720", wantSize: 1},
		{name: "sora 2 pro 1024p", size: "1792x1024", wantSize: 5.0 / 3.0},
		{name: "sora 2 pro 1080p", size: "1920x1080", wantSize: 7.0 / 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Set("task_request", relaycommon.TaskSubmitReq{
				Prompt:  "price check",
				Size:    tt.size,
				Seconds: "4",
			})

			ratios := (&TaskAdaptor{}).EstimateBilling(c, &relaycommon.RelayInfo{
				TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			})
			require.InDelta(t, 4, ratios["seconds"], 1e-9)
			require.InDelta(t, tt.wantSize, ratios["size"], 1e-9)
		})
	}
}

func TestDoResponseAcceptsTaskIDFromTopLevelArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader([]byte(`[{"task_id":"video_array"}]`))),
	}
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "video_array", upstreamID)
}
