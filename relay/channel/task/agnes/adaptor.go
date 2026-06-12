package agnes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/tidwall/sjson"
)

type videoRequest struct {
	Prompt          string                 `json:"prompt"`
	Model           string                 `json:"model,omitempty"`
	Mode            string                 `json:"mode,omitempty"`
	Image           common.RawMessage      `json:"image,omitempty"`
	Images          []string               `json:"images,omitempty"`
	ExtraBody       common.RawMessage      `json:"extra_body,omitempty"`
	Size            string                 `json:"size,omitempty"`
	Duration        int                    `json:"duration,omitempty"`
	Seconds         string                 `json:"seconds,omitempty"`
	InputReference  string                 `json:"input_reference,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	NumFrames       int                    `json:"num_frames,omitempty"`
	FrameRate       float64                `json:"frame_rate,omitempty"`
	normalizedImage string
}

func (r videoRequest) taskSubmitReq() relaycommon.TaskSubmitReq {
	image := r.normalizedImage
	if image == "" && len(r.Images) > 0 {
		image = r.Images[0]
	}
	return relaycommon.TaskSubmitReq{
		Prompt:         r.Prompt,
		Model:          r.Model,
		Mode:           r.Mode,
		Image:          image,
		Images:         r.Images,
		Size:           r.Size,
		Duration:       r.Duration,
		Seconds:        r.Seconds,
		InputReference: r.InputReference,
		Metadata:       r.Metadata,
	}
}

type responseTask struct {
	ID                 string `json:"id,omitempty"`
	TaskID             string `json:"task_id,omitempty"`
	VideoID            string `json:"video_id,omitempty"`
	Object             string `json:"object,omitempty"`
	Model              string `json:"model,omitempty"`
	Status             string `json:"status,omitempty"`
	Progress           int    `json:"progress,omitempty"`
	CreatedAt          int64  `json:"created_at,omitempty"`
	CompletedAt        int64  `json:"completed_at,omitempty"`
	ExpiresAt          int64  `json:"expires_at,omitempty"`
	Seconds            string `json:"seconds,omitempty"`
	Size               string `json:"size,omitempty"`
	URL                string `json:"url,omitempty"`
	OutputURL          string `json:"output_url,omitempty"`
	VideoURL           string `json:"video_url,omitempty"`
	RemixedFromVideoID string `json:"remixed_from_video_id,omitempty"`
	Error              *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

const agnesTaskFetchTimeout = 60 * time.Second

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var req videoRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Model) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model field is required"), "missing_model", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	if err := req.normalizeImages(); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if req.Metadata == nil {
		req.Metadata = map[string]interface{}{}
	}
	if req.NumFrames > 0 {
		req.Metadata["num_frames"] = req.NumFrames
	}
	if req.FrameRate > 0 {
		req.Metadata["frame_rate"] = req.FrameRate
	}

	action := constant.TaskActionTextGenerate
	if len(req.Images) > 0 {
		action = constant.TaskActionGenerate
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	info.Action = action
	c.Set("task_request", req.taskSubmitReq())
	return nil
}

func (r *videoRequest) normalizeImages() error {
	images := appendNonEmptyStrings(nil, r.Images...)
	topLevelImages, err := agnesVideoImagesFromRaw(r.Image, "image")
	if err != nil {
		return err
	}
	images = appendNonEmptyStrings(images, topLevelImages...)
	extraBodyImages, err := agnesVideoImagesFromExtraBody(r.ExtraBody)
	if err != nil {
		return err
	}
	images = appendNonEmptyStrings(images, extraBodyImages...)
	if len(images) == 0 {
		images = appendNonEmptyStrings(images, r.InputReference)
	}
	r.Images = images
	if len(images) > 0 {
		r.normalizedImage = images[0]
	}
	return nil
}

func agnesVideoImagesFromExtraBody(raw common.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if common.GetJsonType(trimmed) != "object" {
		return nil, nil
	}
	var extraBody map[string]common.RawMessage
	if err := common.Unmarshal(trimmed, &extraBody); err != nil {
		return nil, fmt.Errorf("extra_body must be a JSON object: %w", err)
	}
	image, ok := extraBody["image"]
	if !ok {
		return nil, nil
	}
	return agnesVideoImagesFromRaw(image, "extra_body.image")
}

func agnesVideoImagesFromRaw(raw common.RawMessage, field string) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	switch common.GetJsonType(trimmed) {
	case "string":
		var image string
		if err := common.Unmarshal(trimmed, &image); err != nil {
			return nil, fmt.Errorf("%s must be a string: %w", field, err)
		}
		return appendNonEmptyStrings(nil, image), nil
	case "array":
		var images []string
		if err := common.Unmarshal(trimmed, &images); err != nil {
			return nil, fmt.Errorf("%s must be a string array: %w", field, err)
		}
		return appendNonEmptyStrings(nil, images...), nil
	default:
		return nil, fmt.Errorf("%s must be a string or string array", field)
	}
}

func appendNonEmptyStrings(dst []string, values ...string) []string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		dst = append(dst, trimmed)
	}
	return dst
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	seconds := requestSeconds(req)
	if seconds <= 0 {
		seconds = 4
	}
	return map[string]float64{
		"seconds": seconds,
	}
}

func requestSeconds(req relaycommon.TaskSubmitReq) float64 {
	if seconds, err := strconv.ParseFloat(strings.TrimSpace(req.Seconds), 64); err == nil && seconds > 0 {
		return seconds
	}
	if req.Duration > 0 {
		return float64(req.Duration)
	}
	numFrames := metadataFloat(req.Metadata, "num_frames")
	frameRate := metadataFloat(req.Metadata, "frame_rate")
	if numFrames > 0 && frameRate > 0 {
		return numFrames / frameRate
	}
	return 0
}

func metadataFloat(metadata map[string]interface{}, key string) float64 {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case string:
		v, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return v
	default:
		return 0
	}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return strings.TrimRight(a.baseURL, "/") + "/v1/videos", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	cachedBody, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}
	bodyMap := map[string]interface{}{}
	if err := common.Unmarshal(cachedBody, &bodyMap); err != nil {
		return nil, errors.Wrap(err, "decode_agnes_video_request_failed")
	}
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_task_request_failed")
	}
	applyNormalizedAgnesVideoRequest(bodyMap, taskReq, agnesUpstreamModelName(info))
	newBody, err := common.Marshal(bodyMap)
	if err != nil {
		return nil, errors.Wrap(err, "marshal_agnes_video_request_failed")
	}
	return bytes.NewReader(newBody), nil
}

func agnesUpstreamModelName(info *relaycommon.RelayInfo) string {
	if info == nil || info.ChannelMeta == nil {
		return ""
	}
	return info.ChannelMeta.UpstreamModelName
}

func applyNormalizedAgnesVideoRequest(bodyMap map[string]interface{}, req relaycommon.TaskSubmitReq, upstreamModel string) {
	model := strings.TrimSpace(upstreamModel)
	if model == "" {
		model = req.Model
	}
	bodyMap["model"] = model
	bodyMap["prompt"] = req.Prompt
	if req.Mode != "" {
		bodyMap["mode"] = req.Mode
	}
	if req.Image != "" && bodyMap["image"] == nil {
		bodyMap["image"] = req.Image
	}
	if len(req.Images) > 0 {
		bodyMap["images"] = req.Images
	}
	if req.InputReference != "" {
		bodyMap["input_reference"] = req.InputReference
	}
	if req.Size != "" {
		bodyMap["size"] = req.Size
	}
	if req.Duration > 0 {
		bodyMap["duration"] = req.Duration
	}
	if req.Seconds != "" {
		bodyMap["seconds"] = req.Seconds
	}
	if len(req.Metadata) > 0 {
		bodyMap["metadata"] = req.Metadata
	}
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var dResp responseTask
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	upstreamID := firstNonEmpty(dResp.VideoID, dResp.ID, dResp.TaskID)
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("task_id is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	dResp.ID = info.PublicTaskID
	dResp.TaskID = info.PublicTaskID
	dResp.VideoID = info.PublicTaskID
	c.JSON(http.StatusOK, dResp)
	return upstreamID, responseBody, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string, extraHeaders ...http.Header) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}

	uri := strings.TrimRight(baseUrl, "/") + "/agnesapi?video_id=" + url.QueryEscape(taskID)
	ctx, cancel := context.WithTimeout(context.Background(), agnesTaskFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+key)
	taskcommon.ApplyExtraHeaders(req, extraHeaders...)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte, _ string, _ ...http.Header) (*relaycommon.TaskInfo, error) {
	resTask := responseTask{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{
		Code: 0,
	}

	switch strings.ToLower(strings.TrimSpace(resTask.Status)) {
	case "queued", "pending", "submitted":
		taskResult.Status = model.TaskStatusQueued
	case "processing", "in_progress", "running":
		taskResult.Status = model.TaskStatusInProgress
	case "completed", "succeeded", "success", "succeed":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Url = firstNonEmpty(resTask.URL, resTask.OutputURL, resTask.VideoURL)
	case "failed", "cancelled", "canceled":
		taskResult.Status = model.TaskStatusFailure
		if resTask.Error != nil {
			taskResult.Reason = resTask.Error.Message
		} else {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusFailure
		upstreamStatus := strings.TrimSpace(resTask.Status)
		if upstreamStatus == "" {
			taskResult.Reason = "unknown upstream status"
		} else {
			taskResult.Reason = fmt.Sprintf("unknown upstream status: %s", upstreamStatus)
		}
	}
	if resTask.Progress > 0 && resTask.Progress < 100 {
		taskResult.Progress = fmt.Sprintf("%d%%", resTask.Progress)
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	data := task.Data
	var err error
	if data, err = sjson.SetBytes(data, "id", task.TaskID); err != nil {
		return nil, errors.Wrap(err, "set id failed")
	}
	if data, err = sjson.SetBytes(data, "task_id", task.TaskID); err != nil {
		return nil, errors.Wrap(err, "set task_id failed")
	}
	if data, err = sjson.SetBytes(data, "video_id", task.TaskID); err != nil {
		return nil, errors.Wrap(err, "set video_id failed")
	}
	return data, nil
}
