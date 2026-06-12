package agnes

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFetchTaskUsesOfficialAgnesVideoIDEndpoint(t *testing.T) {
	service.InitHttpClient()

	seen := make(chan struct {
		path          string
		videoID       string
		authorization string
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- struct {
			path          string
			videoID       string
			authorization string
		}{
			path:          r.URL.Path,
			videoID:       r.URL.Query().Get("video_id"),
			authorization: r.Header.Get("Authorization"),
		}
		_, _ = w.Write([]byte(`{"status":"completed"}`))
	}))
	defer server.Close()

	resp, err := (&TaskAdaptor{}).FetchTask(server.URL, "sk-test", map[string]any{
		"task_id": "video-123",
	}, "")
	require.NoError(t, err)
	defer resp.Body.Close()

	select {
	case req := <-seen:
		require.Equal(t, "/agnesapi", req.path)
		require.Equal(t, "video-123", req.videoID)
		require.Equal(t, "Bearer sk-test", req.authorization)
	case <-time.After(time.Second):
		require.Fail(t, "upstream request was not observed")
	}
}

func TestDoResponseStoresVideoIDAsUpstreamTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(bytes.NewReader([]byte(`{
			"id":"task-upstream",
			"task_id":"task-legacy",
			"video_id":"video-official",
			"status":"queued"
		}`))),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "video-official", upstreamID)
	require.JSONEq(t, `{
		"id":"task-upstream",
		"task_id":"task-legacy",
		"video_id":"video-official",
		"status":"queued"
	}`, string(taskData))
	require.JSONEq(t, `{
		"id":"task_public",
		"task_id":"task_public",
		"video_id":"task_public",
		"status":"queued"
	}`, recorder.Body.String())
}

func TestEstimateBillingUsesNumFramesAndFrameRate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{}
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{
			"num_frames": float64(121),
			"frame_rate": float64(24),
		},
	})

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.InDelta(t, 121.0/24.0, ratios["seconds"], 0.000001)
}

func TestConvertToOpenAIVideoMasksUpstreamIDs(t *testing.T) {
	data := []byte(`{"id":"upstream","task_id":"legacy","video_id":"video-official","status":"completed"}`)
	task := &model.Task{
		TaskID: "task_public",
		Data:   data,
	}

	converted, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"id":"task_public",
		"task_id":"task_public",
		"video_id":"task_public",
		"status":"completed"
	}`, string(converted))
}

func TestParseTaskResultDoesNotUseRemixedFromVideoIDAsURL(t *testing.T) {
	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"status":"completed",
		"remixed_from_video_id":"video_source"
	}`), "")
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, taskInfo.Status)
	require.Empty(t, taskInfo.Url)
}

func TestParseTaskResultUnknownStatusFailsWithUpstreamStatus(t *testing.T) {
	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"status":"waiting_for_review",
		"progress":42
	}`), "")
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, taskInfo.Status)
	require.Equal(t, "unknown upstream status: waiting_for_review", taskInfo.Reason)
	require.Equal(t, "42%", taskInfo.Progress)
}

func TestValidateRequestStoresFrameBillingMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader([]byte(`{
		"model":"agnes-video-v2.0",
		"prompt":"make a wave",
		"num_frames":121,
		"frame_rate":24,
		"image":"https://example.com/input.png"
	}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	require.Equal(t, constant.TaskActionGenerate, info.Action)

	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/input.png"}, req.Images)
	require.Equal(t, 121, req.Metadata["num_frames"])
	require.Equal(t, 24.0, req.Metadata["frame_rate"])
}

func TestValidateRequestAcceptsOfficialImageArray(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader([]byte(`{
		"model":"agnes-video-v2.0",
		"prompt":"animate these keyframes",
		"image":["https://example.com/start.png","https://example.com/end.png"]
	}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	require.Equal(t, constant.TaskActionGenerate, info.Action)

	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/start.png", req.Image)
	require.Equal(t, []string{
		"https://example.com/start.png",
		"https://example.com/end.png",
	}, req.Images)
}

func TestValidateRequestDetectsExtraBodyImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader([]byte(`{
		"model":"agnes-video-v2.0",
		"prompt":"animate these keyframes",
		"mode":"keyframes",
		"extra_body":{
			"image":["https://example.com/start.png","https://example.com/end.png"]
		}
	}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	require.Equal(t, constant.TaskActionGenerate, info.Action)

	req, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/start.png", req.Image)
	require.Equal(t, []string{
		"https://example.com/start.png",
		"https://example.com/end.png",
	}, req.Images)
}

func TestBuildRequestBodySendsNormalizedAgnesVideoFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader([]byte(`{
		"model":"agnes-video-v2.0",
		"prompt":"animate these keyframes",
		"mode":"keyframes",
		"vendor_option":"keep",
		"num_frames":121,
		"frame_rate":24,
		"extra_body":{
			"image":["https://example.com/start.png","https://example.com/end.png"]
		}
	}`)))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "upstream-agnes-video",
		},
	}
	adaptor := &TaskAdaptor{}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"upstream-agnes-video",
		"prompt":"animate these keyframes",
		"mode":"keyframes",
		"vendor_option":"keep",
		"num_frames":121,
		"frame_rate":24,
		"image":"https://example.com/start.png",
		"images":["https://example.com/start.png","https://example.com/end.png"],
		"metadata":{"num_frames":121,"frame_rate":24},
		"extra_body":{
			"image":["https://example.com/start.png","https://example.com/end.png"]
		}
	}`, string(bodyBytes))
}
