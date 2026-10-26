package controller

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const maxVideoDataURLDecodedBytes int64 = 512 << 20

const (
	videoProxyMaxUpstreamAttempts = 3
	videoProxyRetryBaseDelay      = 150 * time.Millisecond
	videoProxyRetryMaxDelay       = 2 * time.Second
)

var videoProxyResponseHeaderTimeout = 60 * time.Second

var errVideoDataURLTooLarge = fmt.Errorf("video data url exceeds %d decoded bytes", maxVideoDataURLDecodedBytes)

func videoProxyUpstreamStatus(statusCode int) int {
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return statusCode
	case 524:
		// Cloudflare's origin timeout is equivalent to a gateway timeout to the
		// client; keep the response in the transient-retry class.
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

func copyVideoProxyRetryAfter(c *gin.Context, resp *http.Response, statusCode int) {
	if c == nil || resp == nil || resp.Header == nil {
		return
	}
	if statusCode != http.StatusTooManyRequests && statusCode != http.StatusServiceUnavailable && statusCode != http.StatusGatewayTimeout {
		return
	}
	value := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n") {
		return
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return
		}
	} else if _, err := http.ParseTime(value); err != nil {
		return
	}
	c.Header("Retry-After", value)
}

func copyVideoProxyRetryAfterSeconds(c *gin.Context, seconds int) {
	if c == nil || seconds <= 0 {
		return
	}
	// The value is generated from the parsed upstream header, so it is safe to
	// emit as a bounded delta-seconds Retry-After field.
	if seconds > 7*24*60*60 {
		seconds = 7 * 24 * 60 * 60
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
}

func videoProxyTaskRouteErrorStatus(c *gin.Context, err error) int {
	statusCode := http.StatusBadGateway
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) && apiErr != nil {
		statusCode = videoProxyUpstreamStatus(apiErr.StatusCode)
		copyVideoProxyRetryAfterSeconds(c, apiErr.RetryAfterSeconds)
	}
	return statusCode
}

// videoProxyTransportErrorStatus keeps transport-level timeout semantics
// aligned with the health outcome. A request deadline or a net.Error timeout
// means the gateway did not receive the content in time and must be exposed as
// 504; other transport failures remain 502.
func videoProxyTransportErrorStatus(err error) int {
	if err == nil {
		return http.StatusBadGateway
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}

func isVideoProxyRetryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 524:
		return true
	default:
		return false
	}
}

func videoProxyRetryDelay(resp *http.Response, attempt int) time.Duration {
	if resp != nil && resp.Header != nil {
		value := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\r\n") {
			if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
				delay := time.Duration(seconds) * time.Second
				if delay > videoProxyRetryMaxDelay {
					return videoProxyRetryMaxDelay
				}
				return delay
			}
			if retryAt, err := http.ParseTime(value); err == nil {
				delay := time.Until(retryAt)
				if delay < 0 {
					return 0
				}
				if delay > videoProxyRetryMaxDelay {
					return videoProxyRetryMaxDelay
				}
				return delay
			}
		}
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := videoProxyRetryBaseDelay * time.Duration(1<<(attempt-1))
	if delay > videoProxyRetryMaxDelay {
		return videoProxyRetryMaxDelay
	}
	return delay
}

var errVideoProxyResponseHeaderTimeout = fmt.Errorf("video proxy response header timeout: %w", context.DeadlineExceeded)

// doVideoProxyRequest retries only idempotent content GETs and only for
// transient upstream failures. The route, credential and health identity stay
// unchanged; no selector retry or cross-channel reroute is introduced here.
// Each attempt has a response-header deadline. Once a final response is
// returned, its body remains attached to the live request context so large
// content can stream beyond that deadline.
func doVideoProxyRequest(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil || req == nil {
		return nil, errors.New("video proxy request client or request is nil")
	}
	var lastErr error
	for try := 1; try <= videoProxyMaxUpstreamAttempts; try++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptContext, cancelAttempt := context.WithCancel(ctx)
		// req 携带的 SSRF 标记必须跟着每次尝试的上下文一起走：这里的
		// attemptContext 派生于调用方的请求上下文（不带标记），而 Clone 只
		// 复制请求头与 body，不复制 context。标记一旦丢失，共享 transport 的
		// 第一道门就会放行整个回环与内网地址。
		if common.RequiresSSRFValidation(req.Context()) {
			attemptContext = common.WithSSRFValidation(attemptContext)
		}
		timedOut := atomic.Bool{}
		timeoutTimer := time.AfterFunc(videoProxyResponseHeaderTimeout, func() {
			timedOut.Store(true)
			cancelAttempt()
		})
		request := req.Clone(attemptContext)
		resp, err := client.Do(request)
		timeoutTimer.Stop()
		if err == nil && resp == nil {
			err = errors.New("video proxy upstream returned an empty response")
		}
		if timedOut.Load() {
			if resp != nil {
				service.CloseResponseBodyGracefully(resp)
			}
			cancelAttempt()
			if err == nil {
				err = errVideoProxyResponseHeaderTimeout
			} else {
				err = fmt.Errorf("%w: %v", errVideoProxyResponseHeaderTimeout, err)
			}
		}
		if err == nil {
			if !isVideoProxyRetryableStatus(resp.StatusCode) || try == videoProxyMaxUpstreamAttempts || resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
				return resp, nil
			}
			retryDelay := videoProxyRetryDelay(resp, try)
			// This is an idempotent in-request retry. Do not settle the health
			// attempt on an intermediate response: ObserveAPIError finalizes a
			// half-open probe (or starts a cooldown), so doing that here would
			// make a later successful retry unable to recover the route. The
			// final response is returned to the caller and classified exactly
			// once there.
			service.CloseResponseBodyGracefully(resp)
			cancelAttempt()
			if retryDelay > 0 {
				timer := time.NewTimer(retryDelay)
				select {
				case <-ctx.Done():
					if !timer.Stop() {
						<-timer.C
					}
					return nil, ctx.Err()
				case <-timer.C:
				}
			}
			continue
		}
		lastErr = err
		cancelAttempt()
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || try == videoProxyMaxUpstreamAttempts {
			return nil, err
		}
		// As above, transport errors from an intermediate retry are not a
		// final provider outcome. The caller observes only the last error,
		// keeping health state and probe ownership aligned with the request's
		// final result.
		retryDelay := videoProxyRetryDelay(nil, try)
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, lastErr
}

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	// A streaming response may already have committed its status and headers
	// before the body copy reports a client disconnect or upstream read error.
	// Do not append a second JSON response after that point: net/http will keep
	// the original status, while the extra bytes corrupt the video body and can
	// turn a handled transport failure into a misleading mixed response.
	if c == nil || c.Writer == nil || c.Writer.Written() {
		return
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}

func applyVideoProxyRuntimeHeaders(req *http.Request, headers http.Header) {
	service.ApplyRuntimeRequestHeaders(req, headers)
}

// taskHasDirectVideoResultURL reports whether a completed task already has a
// provider-signed/content URL that can be fetched without the channel
// credential. Local proxy URLs and data URLs are deliberately excluded.
func taskHasDirectVideoResultURL(task *model.Task) bool {
	if task == nil {
		return false
	}
	resultURL := strings.TrimSpace(task.GetResultURL())
	if resultURL == "" || strings.HasPrefix(strings.ToLower(resultURL), "data:") {
		return false
	}
	return !isTaskProxyContentURL(resultURL, task.TaskID)
}

func taskHasStoredVideoDataURL(task *model.Task) bool {
	if task == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(task.GetResultURL())), "data:")
}

func taskHasStoredVideoResult(task *model.Task) bool {
	return taskHasDirectVideoResultURL(task) || taskHasStoredVideoDataURL(task)
}

// videoProxyCanUseStoredResult identifies provider types for which a stored
// direct URL is authoritative. OpenAI/Sora content endpoints and Gemini's
// operation lookup still require their provider-specific follow-up request.
func videoProxyCanUseStoredResult(channelType int, task *model.Task) bool {
	if !taskHasStoredVideoResult(task) {
		return false
	}
	if taskHasStoredVideoDataURL(task) {
		return true
	}
	switch channelType {
	case constant.ChannelTypeGemini, constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
		return false
	default:
		return true
	}
}

// shouldUseStoredVideoResult keeps provider-specific content lookups for the
// normal healthy-channel path, but treats a persisted direct result as
// authoritative when the channel is no longer usable. A deleted/disabled
// channel cannot provide a valid credential or operation lookup; retrying that
// path only turns an already-completed task into a 401/500. Local proxy URLs
// are excluded by taskHasDirectVideoResultURL and must still use the captured
// route.
func shouldUseStoredVideoResult(task *model.Task, channel *model.Channel, channelType int) bool {
	if !taskHasStoredVideoResult(task) {
		return false
	}
	if taskHasStoredVideoDataURL(task) {
		return true
	}
	// A current channel row can be reused for another provider type while a
	// completed task still points at the original provider. The persisted direct
	// result is authoritative in that case; attempting to validate the mutable
	// channel against the old snapshot would turn a downloadable result into a
	// false route-mismatch error.
	if taskHasDirectVideoResultURL(task) && taskRouteSnapshotChannelTypeChanged(task, channel) {
		return true
	}
	if taskHasDirectVideoResultURL(task) && (channel == nil || channel.Status != common.ChannelStatusEnabled) {
		return true
	}
	return videoProxyCanUseStoredResult(channelType, task)
}

func taskRouteSnapshotChannelTypeChanged(task *model.Task, channel *model.Channel) bool {
	if task == nil || channel == nil || task.PrivateData.RouteSnapshot == nil {
		return false
	}
	snapshot := task.PrivateData.RouteSnapshot
	return snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion &&
		constant.IsValidChannelType(snapshot.ChannelType) &&
		constant.IsValidChannelType(channel.Type) &&
		snapshot.ChannelType != channel.Type
}

func VideoProxy(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return
	}

	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}

	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}

	var videoURL string
	taskKey := ""
	storedResultAvailable := taskHasStoredVideoResult(task)
	channel, channelErr := model.CacheGetChannel(task.ChannelId)
	if channelErr != nil && !storedResultAvailable {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to get channel for task %s: %s", taskID, channelErr.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to retrieve channel information")
		return
	}
	if channelErr != nil {
		// A completed task with a provider-signed URL is independently
		// downloadable. The channel is only needed for proxy credentials,
		// routing and optional proxy settings; do not make a deleted channel
		// invalidate an already-issued result URL. SSRF validation below still
		// applies before any outbound request.
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("Channel unavailable for direct video result; fetching stored URL: task_id=%s channel_id=%d", taskID, task.ChannelId))
		channel = nil
	}
	routeChannelType := taskRouteChannelType(task.PrivateData.RouteSnapshot, channel)
	useStoredResult := storedResultAvailable && shouldUseStoredVideoResult(task, channel, routeChannelType)
	if useStoredResult && taskHasStoredVideoDataURL(task) {
		if err := writeVideoDataURL(c, task.GetResultURL()); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		}
		return
	}
	if !useStoredResult && channel != nil {
		if routeChannelType, err = service.TaskRouteSnapshotChannelType(task.PrivateData.RouteSnapshot, channel); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to validate task channel type for %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Task channel type is unavailable")
			return
		}
	}
	var baseURL string
	var contentAttempt *service.ChannelHealthAttempt
	if !useStoredResult {
		baseURL, err = service.TaskRouteSnapshotBaseURL(task.PrivateData.RouteSnapshot, channel)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve task base URL for %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Task route is unavailable")
			return
		}
	}
	if !useStoredResult && channel != nil && (routeChannelType == constant.ChannelTypeGemini || routeChannelType == constant.ChannelTypeVertexAi ||
		routeChannelType == constant.ChannelTypeOpenAI || routeChannelType == constant.ChannelTypeSora ||
		channel.ChannelInfo.IsMultiKey || strings.TrimSpace(task.PrivateData.Key) != "") {
		taskKey, err = service.ResolveTaskChannelKey(channel, task)
		if err != nil {
			// A provider direct result URL is independently downloadable. If the
			// channel still exists but its credential was rotated/removed, use that
			// persisted URL rather than attempting an unauthenticated content call.
			if taskHasDirectVideoResultURL(task) {
				logger.LogWarn(c.Request.Context(), fmt.Sprintf("Task credential unavailable; fetching stored direct video result: task_id=%s channel_id=%d", taskID, task.ChannelId))
				useStoredResult = true
				taskKey = ""
			} else {
				logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve task credential for %s: %s", taskID, err.Error()))
				videoProxyError(c, http.StatusUnauthorized, "channel_credential_unavailable", "Task credential is unavailable")
				return
			}
		}
	}
	// Gemini and Vertex require a status/operation fetch before their final
	// content URL is known. Keep that upstream operation on its own immutable
	// health and concurrency identity; the subsequent content download gets a
	// separate content-phase identity so a polling failure cannot cool down the
	// content route (or vice versa).
	var pollAttempt *service.ChannelHealthAttempt
	var pollLease *service.ChannelConcurrencyLease
	if !useStoredResult && channel != nil && (routeChannelType == constant.ChannelTypeGemini || routeChannelType == constant.ChannelTypeVertexAi) {
		pollAttempt, err = service.BeginTaskHealthAttempt(c.Request.Context(), task, channel, taskKey, "poll")
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to build task polling health route for %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve task route")
			return
		}
		defer pollAttempt.Release()
		if !pollAttempt.Selection.Allow {
			if pollAttempt.Selection.Err != nil {
				videoProxyError(c, http.StatusServiceUnavailable, "channel_health_unavailable", pollAttempt.Selection.Err.Error())
			} else {
				videoProxyError(c, http.StatusServiceUnavailable, "channel_health_unavailable", "Channel health gate rejected the route")
			}
			return
		}
		var leaseErr error
		pollLease, leaseErr = service.AcquireChannelConcurrency(pollAttempt.Context, channel)
		if leaseErr != nil {
			statusCode := http.StatusServiceUnavailable
			if errors.Is(leaseErr, service.ErrChannelConcurrencyExceeded) {
				statusCode = http.StatusTooManyRequests
			}
			videoProxyError(c, statusCode, "channel_concurrency_error", leaseErr.Error())
			return
		}
		if pollLease != nil {
			defer func() {
				if pollLease != nil {
					pollLease.Release()
				}
			}()
		}
		pollContext := pollAttempt.Context.Request.Context()
		if routeChannelType == constant.ChannelTypeGemini {
			videoURL, err = getGeminiVideoURL(pollContext, channel, task, taskKey)
		} else {
			videoURL, err = getVertexVideoURL(pollContext, channel, task, taskKey)
		}
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve task result URL for %s: %s", taskID, err.Error()))
			observeVideoProxyHealthFailure(pollAttempt, err)
			videoProxyError(c, videoProxyTaskRouteErrorStatus(c, err), "server_error", "Failed to resolve task result URL")
			return
		}
		pollAttempt.MarkSuccess()
		if pollLease != nil {
			pollLease.Release()
			pollLease = nil
		}
	}

	if !useStoredResult {
		// Completed-task content fetches use the route snapshot captured at
		// submission. Legacy rows explicitly use the compatibility path inside
		// BeginTaskHealthAttempt; new rows never consult current model_mapping.
		contentAttempt, err = service.BeginTaskHealthAttempt(c.Request.Context(), task, channel, taskKey, "content")
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to build video content health route for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve video route")
			return
		}
		defer contentAttempt.Release()
		if !contentAttempt.Selection.Allow {
			if contentAttempt.Selection.Err != nil {
				videoProxyError(c, http.StatusServiceUnavailable, "channel_health_unavailable", contentAttempt.Selection.Err.Error())
			} else {
				videoProxyError(c, http.StatusServiceUnavailable, "channel_health_unavailable", "Channel health gate rejected the route")
			}
			return
		}

		lease, err := service.AcquireChannelConcurrency(contentAttempt.Context, channel)
		if err != nil {
			statusCode := http.StatusServiceUnavailable
			if errors.Is(err, service.ErrChannelConcurrencyExceeded) {
				statusCode = http.StatusTooManyRequests
			}
			videoProxyError(c, statusCode, "channel_concurrency_error", err.Error())
			return
		}
		if lease != nil {
			defer lease.Release()
		}
	}
	proxy := ""
	// A provider-signed direct result does not need the channel to remain
	// present. In that case there is no proxy configuration to resolve after a
	// channel deletion; the URL is still validated by the SSRF gate below.
	proxy, err = resolveVideoProxy(task.PrivateData.RouteSnapshot, channel)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve task proxy for %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Task proxy is unavailable")
		return
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
		return
	}

	requestContext := c.Request.Context()
	if contentAttempt != nil && contentAttempt.Context != nil && contentAttempt.Context.Request != nil {
		requestContext = contentAttempt.Context.Request.Context()
	}
	ctx := requestContext
	// The final URL is selected below from the stored result or the captured
	// content route. Use a valid placeholder until that selection is complete;
	// an empty URL is rejected by net/http before the real URL can be assigned.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://invalid.local/", nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create request: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	if useStoredResult {
		videoURL = task.GetResultURL()
	} else {
		switch routeChannelType {
		case constant.ChannelTypeGemini:
			req.Header.Set("x-goog-api-key", taskKey)
		case constant.ChannelTypeVertexAi:
		case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
			escapedTaskID := url.PathEscape(task.GetUpstreamTaskID())
			contentPath := "/v1/videos/" + escapedTaskID + "/content"
			if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && strings.TrimSpace(snapshot.ContentPath) != "" {
				// ContentPath is the stable operation shape captured at submission.
				// Keep the task id in the URL while retaining any provider-specific
				// prefix represented by the captured path.
				capturedPath := strings.TrimRight(strings.TrimSpace(snapshot.ContentPath), "/")
				if strings.HasSuffix(capturedPath, "/content") {
					contentPath = strings.TrimRight(capturedPath, "/")
					if strings.Contains(contentPath, "/videos/") {
						prefix := contentPath[:strings.Index(contentPath, "/videos/")+len("/videos/")]
						contentPath = prefix + escapedTaskID + "/content"
					}
				}
			}
			videoURL = baseURL + contentPath
			req.Header.Set("Authorization", "Bearer "+taskKey)
		default:
			// Video URL is stored in PrivateData.ResultURL (fallback to FailReason for old data)
			videoURL = task.GetResultURL()
		}
	}
	if taskKey != "" {
		runtimeHeaders, headerErr := service.BuildTaskRouteSnapshotRequestHeaders(channel, taskKey, req.Header, task.PrivateData.RouteSnapshot)
		if headerErr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to build task runtime headers for %s: %s", taskID, headerErr.Error()))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to build request headers")
			return
		}
		applyVideoProxyRuntimeHeaders(req, runtimeHeaders)
	}

	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL is empty for task %s", taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}

	if isVideoDataURL(videoURL) {
		if err := writeVideoDataURL(c, videoURL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		} else if contentAttempt != nil {
			contentAttempt.MarkSuccess()
		}
		return
	}

	req.URL, err = url.Parse(videoURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to parse video URL %s: %s", safeVideoURLForLog(videoURL), err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}
	req = req.WithContext(common.WithSSRFValidation(req.Context()))

	resp, err := doVideoProxyRequest(ctx, client, req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video from %s: %s", safeVideoURLForLog(videoURL), err.Error()))
		if !isVideoProxyClientCancellation(c, err) {
			observeVideoProxyHealthFailure(contentAttempt, err)
		}
		videoProxyError(c, videoProxyTransportErrorStatus(err), "server_error", "Failed to fetch video content")
		return
	}
	if resp.Body == nil {
		err = errors.New("video proxy upstream response body is nil")
		logger.LogError(c.Request.Context(), err.Error())
		observeVideoProxyHealthFailure(contentAttempt, err)
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer service.CloseResponseBodyGracefully(resp)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d for %s", resp.StatusCode, safeVideoURLForLog(videoURL)))
		if contentAttempt != nil {
			apiErr := service.RelayErrorHandler(requestContext, resp, false)
			contentAttempt.ObserveAPIError(apiErr)
		}
		statusCode := videoProxyUpstreamStatus(resp.StatusCode)
		copyVideoProxyRetryAfter(c, resp, statusCode)
		videoProxyError(c, statusCode, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	service.CopyUpstreamHeaders(c, resp.Header)

	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content: %s", err.Error()))
		if !isVideoProxyClientCancellation(c, err) {
			observeVideoProxyHealthFailure(contentAttempt, err)
		}
	} else if contentAttempt != nil {
		contentAttempt.MarkSuccess()
	}
}

// taskRouteChannelType returns the provider identity captured when the task
// was submitted. The snapshot remains usable after a channel row is deleted;
// using it here is important because OpenAI/Sora/Gemini direct URLs must not be
// mistaken for generic stored results merely because the current channel is
// unavailable.
func taskRouteChannelType(snapshot *model.TaskRouteSnapshot, channel *model.Channel) int {
	if snapshot != nil && snapshot.SchemaVersion >= 1 && constant.IsValidChannelType(snapshot.ChannelType) {
		return snapshot.ChannelType
	}
	if channel != nil {
		return channel.Type
	}
	return constant.ChannelTypeUnknown
}

// resolveVideoProxy keeps direct result URLs usable after a channel deletion.
// Legacy snapshots never captured proxy state, so an absent channel means an
// explicitly empty proxy. Current snapshots must still pass strict decryption
// and validation; a corrupted captured proxy is not silently ignored.
func resolveVideoProxy(snapshot *model.TaskRouteSnapshot, channel *model.Channel) (string, error) {
	if snapshot == nil {
		if channel == nil {
			return "", nil
		}
		return service.TaskRouteSnapshotProxy(nil, channel)
	}
	if channel != nil {
		return service.TaskRouteSnapshotProxy(snapshot, channel)
	}
	// Snapshots before the current schema predate explicit proxy capture. An
	// absent channel therefore means that the legacy follow-up has no proxy,
	// rather than that a captured proxy is missing. Current snapshots remain
	// strict: their authenticated proxy field is mandatory and must not be
	// silently bypassed when the channel is unavailable.
	if snapshot.SchemaVersion < model.TaskRouteSnapshotSchemaVersion && !snapshot.ProxyCaptured {
		return "", nil
	}
	if snapshot.ProxyCaptured {
		return service.TaskRouteSnapshotProxy(snapshot, nil)
	}
	return "", nil
}

func observeVideoProxyHealthFailure(attempt *service.ChannelHealthAttempt, err error) {
	if attempt == nil || err == nil {
		return
	}
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) && apiErr != nil {
		attempt.ObserveAPIError(apiErr)
		return
	}
	statusCode := http.StatusBadGateway
	errorCode := types.ErrorCodeDoRequestFailed
	if errors.Is(err, context.DeadlineExceeded) {
		statusCode = http.StatusGatewayTimeout
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			statusCode = http.StatusGatewayTimeout
		}
	}
	if statusCode != http.StatusGatewayTimeout && types.IsUpstreamTransportInterruptedError(err) {
		errorCode = types.ErrorCodeUpstreamTransportInterrupted
	}
	attempt.ObserveFailure(err, statusCode, errorCode)
}

// safeVideoURLForLog keeps credentials embedded in signed/query URLs out of
// application logs. The full URL remains in-memory for the outbound request;
// only scheme, host and path are retained for diagnostics.
func safeVideoURLForLog(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "[empty]"
	}
	if isVideoDataURL(raw) {
		return "data:[redacted]"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "[invalid-video-url]"
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	return parsed.Scheme + "://" + parsed.Host + path
}

func isVideoProxyClientCancellation(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func writeVideoDataURL(c *gin.Context, dataURL string) error {
	return writeVideoDataURLWithLimit(c, dataURL, maxVideoDataURLDecodedBytes)
}

func writeVideoDataURLWithLimit(c *gin.Context, dataURL string, maxDecodedBytes int64) error {
	if maxDecodedBytes <= 0 {
		return fmt.Errorf("video data url decoded byte limit must be positive")
	}
	spec, err := parseVideoDataURLSpec(dataURL)
	if err != nil {
		return err
	}
	encoding, err := validateVideoDataURLPayloadWithLimit(spec.payload, maxDecodedBytes)
	if err != nil {
		return err
	}

	c.Writer.Header().Set("Content-Type", spec.mimeType)
	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(http.StatusOK)
	decoder := base64.NewDecoder(encoding, strings.NewReader(spec.payload))
	decoded, err := io.Copy(c.Writer, io.LimitReader(decoder, maxDecodedBytes+1))
	if err != nil {
		return err
	}
	if decoded > maxDecodedBytes {
		return videoDataURLTooLargeError(maxDecodedBytes)
	}
	return nil
}

func isVideoDataURL(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:")
}

type videoDataURLSpec struct {
	mimeType string
	payload  string
}

// parseVideoDataURLSpec validates metadata and returns the encoded payload
// without decoding it. The HTTP response path uses this representation so a
// large video is not duplicated as a second in-memory byte slice.
func parseVideoDataURLSpec(dataURL string) (videoDataURLSpec, error) {
	raw := strings.TrimSpace(dataURL)
	if raw == "" || !isVideoDataURL(raw) {
		return videoDataURLSpec{}, fmt.Errorf("unsupported data url")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return videoDataURLSpec{}, fmt.Errorf("data url contains control characters")
		}
	}
	comma := strings.IndexByte(raw, ',')
	if comma <= len("data:") || comma == len(raw)-1 {
		return videoDataURLSpec{}, fmt.Errorf("invalid data url")
	}
	header := raw[:comma]
	payload := raw[comma+1:]
	if !strings.EqualFold(header[:len("data:")], "data:") {
		return videoDataURLSpec{}, fmt.Errorf("unsupported data url")
	}

	metadata := header[len("data:"):]
	parts := strings.Split(metadata, ";")
	mimeType := strings.TrimSpace(parts[0])
	if mimeType == "" {
		mimeType = "video/mp4"
	}
	base64Flag := false
	parameters := make([]string, 0, len(parts)-1)
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if part == "" {
			return videoDataURLSpec{}, fmt.Errorf("invalid data url parameter")
		}
		if strings.EqualFold(part, "base64") {
			if base64Flag {
				return videoDataURLSpec{}, fmt.Errorf("duplicate base64 flag in data url")
			}
			base64Flag = true
			continue
		}
		parameters = append(parameters, part)
	}
	if !base64Flag {
		return videoDataURLSpec{}, fmt.Errorf("unsupported data url encoding")
	}

	mediaSpec := mimeType
	if len(parameters) > 0 {
		mediaSpec += ";" + strings.Join(parameters, ";")
	}
	parsedMime, _, err := mime.ParseMediaType(mediaSpec)
	if err != nil {
		return videoDataURLSpec{}, fmt.Errorf("invalid data url MIME type: %w", err)
	}
	parsedMime = strings.ToLower(strings.TrimSpace(parsedMime))
	if !strings.HasPrefix(parsedMime, "video/") && parsedMime != "application/octet-stream" {
		return videoDataURLSpec{}, fmt.Errorf("unsupported video MIME type %q", parsedMime)
	}
	if payload == "" || strings.ContainsAny(payload, "\r\n\t ") {
		return videoDataURLSpec{}, fmt.Errorf("invalid base64 video payload")
	}
	return videoDataURLSpec{mimeType: parsedMime, payload: payload}, nil
}

func validateVideoDataURLPayload(payload string) (*base64.Encoding, error) {
	return validateVideoDataURLPayloadWithLimit(payload, maxVideoDataURLDecodedBytes)
}

func validateVideoDataURLPayloadWithLimit(payload string, maxDecodedBytes int64) (*base64.Encoding, error) {
	if maxDecodedBytes <= 0 {
		return nil, fmt.Errorf("video data url decoded byte limit must be positive")
	}
	if payload == "" {
		return nil, fmt.Errorf("invalid base64 video payload")
	}

	encodedLength := int64(len(payload))
	if strings.ContainsRune(payload, '=') {
		if len(payload)%4 != 0 {
			return nil, fmt.Errorf("invalid base64 video payload: padded length is not a multiple of 4")
		}
		paddingStart := strings.IndexByte(payload, '=')
		paddingLength := len(payload) - paddingStart
		if paddingLength > 2 || paddingStart == 0 {
			return nil, fmt.Errorf("invalid base64 video payload padding")
		}
		for index := 0; index < paddingStart; index++ {
			if !isBase64StdByte(payload[index]) {
				return nil, fmt.Errorf("invalid base64 video payload character")
			}
		}
		for index := paddingStart; index < len(payload); index++ {
			if payload[index] != '=' {
				return nil, fmt.Errorf("invalid base64 video payload padding")
			}
		}
		decodedLength := encodedLength/4*3 - int64(paddingLength)
		if decodedLength > maxDecodedBytes {
			return nil, videoDataURLTooLargeError(maxDecodedBytes)
		}
		return base64.StdEncoding, nil
	}

	if len(payload)%4 == 1 {
		return nil, fmt.Errorf("invalid base64 video payload length")
	}
	for index := range payload {
		if !isBase64StdByte(payload[index]) {
			return nil, fmt.Errorf("invalid base64 video payload character")
		}
	}
	decodedLength := encodedLength / 4 * 3
	switch len(payload) % 4 {
	case 2:
		decodedLength++
	case 3:
		decodedLength += 2
	}
	if decodedLength > maxDecodedBytes {
		return nil, videoDataURLTooLargeError(maxDecodedBytes)
	}
	return base64.RawStdEncoding, nil
}

func isBase64StdByte(value byte) bool {
	return value >= 'A' && value <= 'Z' ||
		value >= 'a' && value <= 'z' ||
		value >= '0' && value <= '9' ||
		value == '+' || value == '/'
}

func videoDataURLTooLargeError(limit int64) error {
	if limit == maxVideoDataURLDecodedBytes {
		return errVideoDataURLTooLarge
	}
	return fmt.Errorf("video data url exceeds %d decoded bytes", limit)
}

// parseVideoDataURL keeps the decoded helper used by tests and legacy callers.
// The HTTP response path uses writeVideoDataURL, which streams the payload.
func parseVideoDataURL(dataURL string) (string, []byte, error) {
	spec, err := parseVideoDataURLSpec(dataURL)
	if err != nil {
		return "", nil, err
	}
	encoding, err := validateVideoDataURLPayload(spec.payload)
	if err != nil {
		return "", nil, err
	}
	var videoBytes bytes.Buffer
	decoded, err := io.Copy(&videoBytes, io.LimitReader(base64.NewDecoder(encoding, strings.NewReader(spec.payload)), maxVideoDataURLDecodedBytes+1))
	if err != nil {
		return "", nil, err
	}
	if decoded > maxVideoDataURLDecodedBytes {
		return "", nil, errVideoDataURLTooLarge
	}
	return spec.mimeType, videoBytes.Bytes(), nil
}
