package agnes

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFetchTaskUsesOfficialAgnesVideoIDEndpoint(t *testing.T) {
	allowLocalAgnesTestServer(t)
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

func allowLocalAgnesTestServer(t *testing.T) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	*setting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		AllowedPorts:           []string{"1-65535"},
		ApplyIPFilterForDomain: true,
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

func TestAgnesVideo25BillingUsesOfficialResolutionRatios(t *testing.T) {
	for _, tt := range []struct {
		name          string
		size          string
		resolutionFee float64
	}{
		{name: "720p", size: "720P", resolutionFee: 1},
		{name: "960p", size: "960P", resolutionFee: 1.6},
		{name: "2k", size: "2K", resolutionFee: 2.2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newAgnesVideoTestContext(`{"model":"agnes-video-2.5","prompt":"make a wave","size":"` + tt.size + `","duration":8}`)
			info := &relaycommon.RelayInfo{OriginModelName: "agnes-video-2.5"}
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.Nil(t, taskErr)

			ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
			require.InDelta(t, 8.0, ratios["seconds"], 1e-9)
			require.InDelta(t, tt.resolutionFee, ratios["resolution"], 1e-9)
			require.InDelta(t, 1.0, ratios["input_image_fees"], 1e-9)
		})
	}
}

func TestAgnesVideo25BillingChargesImagesAfterFreeAllowance(t *testing.T) {
	c := newAgnesVideoTestContext(`{"model":"agnes-video-2.5","prompt":"make a wave","duration":4,"images":["https://example.com/1.png","https://example.com/2.png","https://example.com/3.png","https://example.com/4.png","https://example.com/5.png","https://example.com/6.png","https://example.com/7.png"]}`)
	info := &relaycommon.RelayInfo{OriginModelName: "agnes-video-2.5"}
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	// Base cost is 4 seconds * $0.025; two excess images add $0.010.
	require.InDelta(t, 1.1, ratios["input_image_fees"], 1e-9)
}

func TestAgnesVideo25FirstAndLastFramesCountAsImageInputs(t *testing.T) {
	c := newAgnesVideoTestContext(`{"model":"agnes-video-2.5-flash","prompt":"make a wave","first_frame":"https://example.com/start.png","last_frame":"https://example.com/end.png"}`)
	info := &relaycommon.RelayInfo{OriginModelName: "agnes-video-2.5-flash"}
	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	require.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestAgnesVideo25FlashValidationUsesListPriceConstraints(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "non 720p size",
			body:    `{"model":"agnes-video-2.5-flash","prompt":"make a wave","size":"2K"}`,
			message: "size must be 720P",
		},
		{
			name:    "too many images",
			body:    `{"model":"agnes-video-2.5-flash","prompt":"make a wave","images":["1","2","3","4","5","6"]}`,
			message: "images length must not exceed 5",
		},
		{
			name:    "input video",
			body:    `{"model":"agnes-video-2.5-flash","prompt":"make a wave","videos":[{"url":"https://example.com/source.mp4"}]}`,
			message: "videos is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newAgnesVideoTestContext(tt.body)
			info := &relaycommon.RelayInfo{OriginModelName: "agnes-video-2.5-flash"}
			taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			require.Equal(t, "invalid_request", taskErr.Code)
			require.Contains(t, strings.ToLower(taskErr.Message), strings.ToLower(tt.message))
		})
	}
}

func TestMappedAgnesVideo25ValidationUsesEffectiveModel(t *testing.T) {
	c := newAgnesVideoTestContext(`{"model":"client-video-alias","prompt":"make a wave","videos":[{"url":"https://example.com/source.mp4"}]}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "client-video-alias",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "agnes-video-2.5",
		},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	require.Equal(t, "invalid_request", taskErr.Code)
	require.Contains(t, strings.ToLower(taskErr.Message), "duration cannot be billed exactly")
}

func TestMappedAgnesVideo25BillingUsesEffectiveModel(t *testing.T) {
	c := newAgnesVideoTestContext(`{"model":"client-video-alias","prompt":"make a wave","size":"2K","duration":8,"images":["https://example.com/input.png"]}`)
	info := &relaycommon.RelayInfo{
		OriginModelName: "client-video-alias",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "agnes-video-2.5",
		},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)

	ratios := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.InDelta(t, 2.2, ratios["resolution"], 1e-9)
	require.InDelta(t, 8.0, ratios["seconds"], 1e-9)
}

func TestAgnesVideo25RejectsUnpricedInputVideoDuration(t *testing.T) {
	c := newAgnesVideoTestContext(`{"model":"agnes-video-2.5","prompt":"make a wave","videos":[{"url":"https://example.com/source.mp4"}]}`)
	info := &relaycommon.RelayInfo{OriginModelName: "agnes-video-2.5"}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	require.Equal(t, "invalid_request", taskErr.Code)
	require.Contains(t, strings.ToLower(taskErr.Message), "duration cannot be billed exactly")
}

func newAgnesVideoTestContext(body string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c
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
	require.ErrorContains(t, err, "unknown upstream status: waiting_for_review")
	require.Nil(t, taskInfo)
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
