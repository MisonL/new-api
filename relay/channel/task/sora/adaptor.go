package sora

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"

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

// ============================
// Request / Response structures
// ============================

type ContentItem struct {
	Type     string    `json:"type"`                // "text" or "image_url"
	Text     string    `json:"text,omitempty"`      // for text type
	ImageURL *ImageURL `json:"image_url,omitempty"` // for image_url type
}

type ImageURL struct {
	URL string `json:"url"`
}

type responseTask struct {
	ID                 string `json:"id"`
	TaskID             string `json:"task_id,omitempty"` //兼容旧接口
	Object             string `json:"object"`
	Model              string `json:"model"`
	Status             string `json:"status"`
	Progress           int    `json:"progress"`
	CreatedAt          int64  `json:"created_at"`
	CompletedAt        int64  `json:"completed_at,omitempty"`
	ExpiresAt          int64  `json:"expires_at,omitempty"`
	Seconds            string `json:"seconds,omitempty"`
	Size               string `json:"size,omitempty"`
	RemixedFromVideoID string `json:"remixed_from_video_id,omitempty"`
	Error              *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// ============================
// Adaptor implementation
// ============================

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

func validateRemixRequest(c *gin.Context) *dto.TaskError {
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("field prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	// 存储原始请求到 context，与 ValidateMultipartDirect 路径保持一致
	c.Set("task_request", req)
	return nil
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if info.Action == constant.TaskActionRemix {
		return validateRemixRequest(c)
	}
	return relaycommon.ValidateMultipartDirect(c, info)
}

// EstimateBilling 根据用户请求的 seconds 和 size 计算 OtherRatios。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	// remix 路径的 OtherRatios 已在 ResolveOriginTask 中设置
	if info.Action == constant.TaskActionRemix {
		return nil
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	seconds, _ := strconv.Atoi(req.Seconds)
	if seconds == 0 {
		seconds = req.Duration
	}
	if seconds <= 0 {
		seconds = 4
	}

	size := req.Size
	if size == "" {
		size = "720x1280"
	}

	ratios := map[string]float64{
		"seconds": float64(seconds),
		"size":    1,
	}
	switch size {
	case "1792x1024", "1024x1792":
		// Sora 2 Pro 1024p is $0.50/s versus the $0.30/s 720p base.
		ratios["size"] = 5.0 / 3.0
	case "1920x1080", "1080x1920":
		// Sora 2 Pro 1080p is $0.70/s versus the $0.30/s 720p base.
		ratios["size"] = 7.0 / 3.0
	}
	return ratios
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.Action == constant.TaskActionRemix {
		return fmt.Sprintf("%s/v1/videos/%s/remix", a.baseURL, info.OriginTaskID), nil
	}
	return fmt.Sprintf("%s/v1/videos", a.baseURL), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
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
	contentType := c.GetHeader("Content-Type")

	if strings.HasPrefix(contentType, "application/json") {
		var bodyMap map[string]interface{}
		if err := common.Unmarshal(cachedBody, &bodyMap); err == nil {
			bodyMap["model"] = info.UpstreamModelName
			if newBody, err := common.Marshal(bodyMap); err == nil {
				return bytes.NewReader(newBody), nil
			}
		}
		return bytes.NewReader(cachedBody), nil
	}

	if strings.Contains(contentType, "multipart/form-data") {
		formData, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return bytes.NewReader(cachedBody), nil
		}
		defer formData.RemoveAll()
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		writer.WriteField("model", info.UpstreamModelName)
		for key, values := range formData.Value {
			if key == "model" {
				continue
			}
			for _, v := range values {
				writer.WriteField(key, v)
			}
		}
		for fieldName, fileHeaders := range formData.File {
			for _, fh := range fileHeaders {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				ct := fh.Header.Get("Content-Type")
				if ct == "" || ct == "application/octet-stream" {
					buf512 := make([]byte, 512)
					n, _ := io.ReadFull(f, buf512)
					ct = http.DetectContentType(buf512[:n])
					// Re-open after sniffing so the full content is copied below
					f.Close()
					f, err = fh.Open()
					if err != nil {
						continue
					}
				}
				h := make(textproto.MIMEHeader)
				// fieldName 来自客户端表单的 name 参数，与 filename 一样可经
				// RFC 2231 name* 解码出 CRLF，两者都必须清洗后再拼 header。
				h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
					common.SanitizeMultipartFieldName(fieldName), common.SanitizeMultipartFilename(fh.Filename)))
				h.Set("Content-Type", ct)
				part, err := writer.CreatePart(h)
				if err != nil {
					f.Close()
					continue
				}
				io.Copy(part, f)
				f.Close()
			}
		}
		writer.Close()
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		return &buf, nil
	}

	info.UpstreamRequestBodySize = storage.Size()
	info.UpstreamRequestGetBody = func() (io.ReadCloser, error) {
		return common.NewBodyStorageReader(storage)
	}
	return common.ReaderOnly(storage), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	if resp == nil || resp.Body == nil {
		taskErr = service.TaskErrorWrapperLocal(errors.New("upstream task response is empty"), "empty_response", http.StatusBadGateway)
		if resp != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			taskErr.Code = "ambiguous_task_submission"
			taskErr.NoRefund = true
		}
		return
	}
	defer resp.Body.Close()
	responseBody, err := service.ReadTaskPollingResponseBody(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapperLocal(err, "read_response_body_failed", http.StatusBadGateway)
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			taskErr.Code = "ambiguous_task_submission"
			taskErr.NoRefund = true
		}
		return
	}
	taskData = responseBody

	// Parse Sora response
	var dResp responseTask
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		// Some compatible gateways return a top-level array rather than the
		// documented object envelope. Preserve the bounded fallback extractor for
		// that shape; only a JSON body with no known task identity is ambiguous.
		if fallbackID := extractSoraTaskID(responseBody); fallbackID != "" {
			dResp.ID = fallbackID
		} else {
			// A 2xx response has already crossed the provider's acceptance boundary.
			// Do not include the raw body in the error: it may contain signed URLs or
			// provider credentials, and retrying an accepted task can create a second
			// upstream job. Mark the response-shape failure local so the relay does
			// not retry or attribute it as a channel-health failure.
			taskErr = service.TaskErrorWrapperLocal(errors.Wrap(err, "unmarshal task response failed"), "ambiguous_task_submission", http.StatusBadGateway)
			taskErr.NoRefund = true
			return
		}
	}

	upstreamID := firstNonEmptySora(dResp.ID, dResp.TaskID)
	if upstreamID == "" {
		upstreamID = extractSoraTaskID(responseBody)
	}
	if upstreamID == "" && resp.Header != nil {
		upstreamID = extractSoraTaskIDFromLocation(resp.Header.Get("Location"))
	}
	if upstreamID == "" {
		// The body was valid JSON but did not expose a task identifier. This is
		// still ambiguous after a successful 2xx submission; stop retries and
		// keep the response safe to diagnose without echoing provider payloads.
		publicTaskID := ""
		if info != nil {
			publicTaskID = strings.TrimSpace(info.PublicTaskID)
		}
		message := "upstream task response did not contain a task identifier"
		if publicTaskID != "" {
			message += fmt.Sprintf(" (public_task_id=%s)", publicTaskID)
		}
		taskErr = service.TaskErrorWrapperLocal(errors.New(message), "ambiguous_task_submission", http.StatusBadGateway)
		taskErr.NoRefund = true
		return
	}

	if info == nil {
		taskErr = service.TaskErrorWrapperLocal(errors.New("task relay info is empty"), "invalid_response", http.StatusInternalServerError)
		return
	}

	// 使用公开 task_xxxx ID 返回给客户端
	publicTaskID := ""
	if info.TaskRelayInfo != nil {
		publicTaskID = strings.TrimSpace(info.PublicTaskID)
	}
	dResp.ID = publicTaskID
	dResp.TaskID = publicTaskID
	c.JSON(http.StatusOK, dResp)
	return upstreamID, responseBody, nil
}

func firstNonEmptySora(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// extractSoraTaskID accepts the provider response shapes seen in front of the
// Sora-compatible endpoint. Some gateways wrap the provider object in
// {data:{...}} or {result:{...}} while preserving a 2xx status. Only known task
// container keys are traversed so an unrelated nested id cannot become the
// asynchronous task identity by accident.
func extractSoraTaskID(body []byte) string {
	var payload any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return extractSoraTaskIDFromValue(payload, 0)
}

func extractSoraTaskIDFromValue(value any, depth int) string {
	if depth > 4 {
		return ""
	}
	switch current := value.(type) {
	case map[string]any:
		// At the top level only known task containers are authoritative. A
		// generic top-level `id` may belong to a request, account, or wrapper
		// object and must not become the asynchronous task identity by accident.
		if depth > 0 {
			for _, key := range []string{"id", "task_id", "video_id"} {
				if candidate, ok := current[key].(string); ok && strings.TrimSpace(candidate) != "" {
					return strings.TrimSpace(candidate)
				}
			}
		}
		for _, key := range []string{"data", "result", "task", "video"} {
			if nested, ok := current[key]; ok {
				if candidate := extractSoraTaskIDFromValue(nested, depth+1); candidate != "" {
					return candidate
				}
			}
		}
	case []any:
		for _, item := range current {
			if candidate := extractSoraTaskIDFromValue(item, depth+1); candidate != "" {
				return candidate
			}
		}
	}
	return ""
}

func extractSoraTaskIDFromLocation(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	parsed, err := url.Parse(location)
	if err != nil {
		return ""
	}
	path := strings.Trim(parsed.Path, "/")
	if path != "" {
		segments := strings.Split(path, "/")
		for index, segment := range segments {
			if !strings.EqualFold(segment, "videos") && !strings.EqualFold(segment, "tasks") && !strings.EqualFold(segment, "operations") {
				continue
			}
			if index+1 >= len(segments) {
				continue
			}
			candidate, err := url.PathUnescape(segments[index+1])
			if err == nil && strings.TrimSpace(candidate) != "" {
				return strings.TrimSpace(candidate)
			}
		}
	}
	query := parsed.Query()
	for _, key := range []string{"id", "task_id", "video_id"} {
		if candidate := strings.TrimSpace(query.Get(key)); candidate != "" {
			return candidate
		}
	}
	return ""
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string, extraHeaders ...http.Header) (*http.Response, error) {
	return a.FetchTaskWithContext(context.Background(), baseUrl, key, body, proxy, extraHeaders...)
}

func (a *TaskAdaptor) FetchTaskWithContext(ctx context.Context, baseUrl, key string, body map[string]any, proxy string, extraHeaders ...http.Header) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	path := taskcommon.CapturedTaskPollingPath(body, "/v1/videos")
	uri := fmt.Sprintf("%s%s/%s", strings.TrimRight(baseUrl, "/"), path, url.PathEscape(taskID))

	if ctx == nil {
		ctx = context.Background()
	}
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

	switch resTask.Status {
	case "queued", "pending":
		taskResult.Status = model.TaskStatusQueued
	case "processing", "in_progress":
		taskResult.Status = model.TaskStatusInProgress
	case "completed":
		taskResult.Status = model.TaskStatusSuccess
		// Url intentionally left empty - the caller constructs the proxy URL using the public task ID
	case "failed", "cancelled":
		taskResult.Status = model.TaskStatusFailure
		if resTask.Error != nil {
			taskResult.Reason = resTask.Error.Message
		} else {
			taskResult.Reason = "task failed"
		}
	default:
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
	return data, nil
}
