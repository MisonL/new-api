package hailuo

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
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// https://platform.minimaxi.com/docs/api-reference/video-generation-intro
type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s%s", a.baseURL, TextToVideoEndpoint), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return nil, fmt.Errorf("invalid request type in context")
	}

	body, err := a.convertToRequestPayload(&req, info)
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(data), nil
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

	var hResp VideoResponse
	if err := common.Unmarshal(responseBody, &hResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if hResp.BaseResp.StatusCode != StatusSuccess {
		taskErr = service.TaskErrorWrapper(
			fmt.Errorf("hailuo api error: %s", hResp.BaseResp.StatusMsg),
			strconv.Itoa(hResp.BaseResp.StatusCode),
			http.StatusBadRequest,
		)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return hResp.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string, extraHeaders ...http.Header) (*http.Response, error) {
	return a.FetchTaskWithContext(context.Background(), baseUrl, key, body, proxy, extraHeaders...)
}

func (a *TaskAdaptor) FetchTaskWithContext(ctx context.Context, baseUrl, key string, body map[string]any, proxy string, extraHeaders ...http.Header) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	path := taskcommon.CapturedTaskPollingPath(body, QueryTaskEndpoint)
	uri := fmt.Sprintf("%s%s?task_id=%s", strings.TrimRight(baseUrl, "/"), path, url.QueryEscape(taskID))

	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
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

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*VideoRequest, error) {
	modelConfig := GetModelConfig(info.UpstreamModelName)
	duration := DefaultDuration
	if req.Duration > 0 {
		duration = req.Duration
	}
	resolution := modelConfig.DefaultResolution
	if req.Size != "" {
		resolution = a.parseResolutionFromSize(req.Size, modelConfig)
	}

	videoRequest := &VideoRequest{
		Model:      info.UpstreamModelName,
		Prompt:     req.Prompt,
		Duration:   &duration,
		Resolution: resolution,
	}
	if err := taskcommon.UnmarshalMetadata(req.Metadata, videoRequest); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata to video request failed")
	}

	return videoRequest, nil
}

func (a *TaskAdaptor) parseResolutionFromSize(size string, modelConfig ModelConfig) string {
	switch {
	case strings.Contains(size, "1080"):
		return Resolution1080P
	case strings.Contains(size, "768"):
		return Resolution768P
	case strings.Contains(size, "720"):
		return Resolution720P
	case strings.Contains(size, "512"):
		return Resolution512P
	default:
		return modelConfig.DefaultResolution
	}
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte, key string, extraHeaders ...http.Header) (*relaycommon.TaskInfo, error) {
	return a.parseTaskResultWithContext(context.Background(), a.baseURL, key, respBody, "", extraHeaders...)
}

// ParseTaskResultWithContext keeps Hailuo's secondary file lookup on the same
// cancellation, proxy and timeout boundary as the status poll.
func (a *TaskAdaptor) ParseTaskResultWithContext(ctx context.Context, baseURL, key string, respBody []byte, proxy string, extraHeaders ...http.Header) (*relaycommon.TaskInfo, error) {
	return a.parseTaskResultWithContext(ctx, baseURL, key, respBody, proxy, extraHeaders...)
}

func (a *TaskAdaptor) parseTaskResultWithContext(ctx context.Context, baseURL, key string, respBody []byte, proxy string, extraHeaders ...http.Header) (*relaycommon.TaskInfo, error) {
	resTask := QueryTaskResponse{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{}

	if resTask.BaseResp.StatusCode == StatusSuccess {
		taskResult.Code = 0
	} else {
		taskResult.Code = resTask.BaseResp.StatusCode
		taskResult.Reason = resTask.BaseResp.StatusMsg
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
	}

	switch strings.ToLower(strings.TrimSpace(resTask.Status)) {
	case strings.ToLower(TaskStatusPreparing), strings.ToLower(TaskStatusQueueing), strings.ToLower(TaskStatusProcessing):
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
		if strings.EqualFold(strings.TrimSpace(resTask.Status), TaskStatusProcessing) {
			taskResult.Progress = "50%"
		}
	case strings.ToLower(TaskStatusSuccess):
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		videoURL, err := a.buildVideoURLWithContext(ctx, baseURL, resTask.FileID, key, proxy, extraHeaders...)
		if err != nil {
			return nil, errors.Wrap(err, "resolve hailuo video URL failed")
		}
		taskResult.Url = videoURL
	case strings.ToLower(TaskStatusFailed):
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
	default:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var hailuoResp QueryTaskResponse
	if err := common.Unmarshal(originTask.Data, &hailuoResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal hailuo task data failed")
	}

	openAIVideo := originTask.ToOpenAIVideo()
	if hailuoResp.BaseResp.StatusCode != StatusSuccess {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: hailuoResp.BaseResp.StatusMsg,
			Code:    strconv.Itoa(hailuoResp.BaseResp.StatusCode),
		}
	}

	jsonData, err := common.Marshal(openAIVideo)
	if err != nil {
		return nil, errors.Wrap(err, "marshal openai video failed")
	}

	return jsonData, nil
}

func (a *TaskAdaptor) buildVideoURL(_, fileID string, apiKey string, extraHeaders ...http.Header) string {
	videoURL, err := a.buildVideoURLWithContext(context.Background(), a.baseURL, fileID, apiKey, "", extraHeaders...)
	if err != nil {
		return ""
	}
	return videoURL
}

func (a *TaskAdaptor) buildVideoURLWithContext(ctx context.Context, baseURL, fileID, apiKey, proxy string, extraHeaders ...http.Header) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		apiKey = a.apiKey
	}
	if apiKey == "" {
		return "", fmt.Errorf("hailuo API key is empty")
	}
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return "", fmt.Errorf("hailuo file ID is empty")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("hailuo base URL is empty")
	}
	query := url.Values{}
	query.Set("file_id", fileID)
	retrieveURL := baseURL + "/v1/files/retrieve?" + query.Encode()

	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, retrieveURL, nil)
	if err != nil {
		return "", fmt.Errorf("create hailuo file request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	taskcommon.ApplyExtraHeaders(req, cloneRuntimeHeaders(extraHeaders...))

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return "", fmt.Errorf("create hailuo file HTTP client: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hailuo file request failed: %w", err)
	}
	if resp == nil {
		return "", fmt.Errorf("hailuo file request returned empty response")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("hailuo file request returned status %d", resp.StatusCode)
	}

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read hailuo file response: %w", err)
	}

	var retrieveResp RetrieveFileResponse
	if err := common.Unmarshal(responseBody, &retrieveResp); err != nil {
		return "", fmt.Errorf("unmarshal hailuo file response: %w", err)
	}

	if retrieveResp.BaseResp.StatusCode != StatusSuccess {
		message := strings.TrimSpace(retrieveResp.BaseResp.StatusMsg)
		if message == "" {
			message = "provider rejected file retrieval"
		}
		return "", fmt.Errorf("hailuo file retrieval failed: %s", message)
	}

	downloadURL := strings.TrimSpace(retrieveResp.File.DownloadURL)
	if downloadURL == "" {
		return "", fmt.Errorf("hailuo file retrieval returned an empty download URL")
	}
	return downloadURL, nil
}

func cloneRuntimeHeaders(extraHeaders ...http.Header) http.Header {
	var cloned http.Header
	for _, headers := range extraHeaders {
		for key, values := range headers {
			for _, value := range values {
				if cloned == nil {
					cloned = http.Header{}
				}
				cloned.Add(key, value)
			}
		}
	}
	return cloned
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsInt(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
