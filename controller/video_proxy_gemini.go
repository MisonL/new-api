package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
)

func getGeminiVideoURL(ctx context.Context, channel *model.Channel, task *model.Task, apiKey string) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("invalid channel or task")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if url := extractGeminiVideoURLFromTaskData(task); url != "" {
		return ensureAPIKey(url, apiKey), nil
	}

	baseURL, err := service.TaskRouteSnapshotBaseURL(task.PrivateData.RouteSnapshot, channel)
	if err != nil {
		return "", fmt.Errorf("resolve task base URL: %w", err)
	}

	channelType, err := service.TaskRouteSnapshotChannelType(task.PrivateData.RouteSnapshot, channel)
	if err != nil {
		return "", fmt.Errorf("validate task channel type: %w", err)
	}
	platform, err := service.TaskRouteSnapshotAdapterPlatform(task.PrivateData.RouteSnapshot, constant.TaskPlatform(strconv.Itoa(channelType)))
	if err != nil {
		return "", fmt.Errorf("resolve task adaptor identity: %w", err)
	}
	adaptor := relay.GetTaskAdaptor(platform)
	if adaptor == nil {
		return "", fmt.Errorf("gemini task adaptor not found")
	}

	if apiKey == "" {
		return "", fmt.Errorf("api key not available for task")
	}

	proxy, err := service.TaskRouteSnapshotProxy(task.PrivateData.RouteSnapshot, channel)
	if err != nil {
		return "", fmt.Errorf("resolve task proxy: %w", err)
	}
	headers, err := service.BuildTaskRouteSnapshotRequestHeaders(channel, apiKey, http.Header{}, task.PrivateData.RouteSnapshot)
	if err != nil {
		return "", fmt.Errorf("build runtime request headers failed: %w", err)
	}
	pollBody := map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"model":   service.ResolveTaskEffectiveModelName(task),
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil {
		pollBody["polling_path"] = snapshot.PollingPath
	}
	resp, err := service.FetchTaskWithContext(ctx, adaptor, baseURL, apiKey, pollBody, proxy, headers)
	if err != nil {
		return "", types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	if resp == nil {
		return "", types.NewErrorWithStatusCode(fmt.Errorf("fetch task returned empty response"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
	}
	if resp.Body == nil {
		return "", types.NewErrorWithStatusCode(fmt.Errorf("fetch task response body is nil"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", service.RelayErrorHandler(ctx, resp, false)
	}

	body, err := service.ReadTaskPollingResponseBody(resp.Body)
	if err != nil {
		return "", types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}

	taskInfo, parseErr := adaptor.ParseTaskResult(body, apiKey, headers)
	if parseErr == nil && taskInfo != nil && taskInfo.ResolvedURL() != "" {
		return ensureAPIKey(taskInfo.ResolvedURL(), apiKey), nil
	}

	if url := extractGeminiVideoURLFromPayload(body); url != "" {
		return ensureAPIKey(url, apiKey), nil
	}

	if parseErr != nil {
		return "", types.NewErrorWithStatusCode(parseErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	return "", types.NewErrorWithStatusCode(fmt.Errorf("gemini video url not found"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
}

func extractGeminiVideoURLFromTaskData(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	var payload map[string]any
	if err := common.Unmarshal(task.Data, &payload); err != nil {
		return ""
	}
	return extractGeminiVideoURLFromMap(payload)
}

func extractGeminiVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return extractGeminiVideoURLFromMap(payload)
}

func extractGeminiVideoURLFromMap(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if uri, ok := payload["uri"].(string); ok && uri != "" {
		return uri
	}
	if resp, ok := payload["response"].(map[string]any); ok {
		if uri := extractGeminiVideoURLFromResponse(resp); uri != "" {
			return uri
		}
	}
	return ""
}

func extractGeminiVideoURLFromResponse(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	if gvr, ok := resp["generateVideoResponse"].(map[string]any); ok {
		if uri := extractGeminiVideoURLFromGeneratedSamples(gvr); uri != "" {
			return uri
		}
	}
	if videos, ok := resp["videos"].([]any); ok {
		for _, video := range videos {
			if vm, ok := video.(map[string]any); ok {
				if uri, ok := vm["uri"].(string); ok && uri != "" {
					return uri
				}
			}
		}
	}
	if uri, ok := resp["video"].(string); ok && uri != "" {
		return uri
	}
	if uri, ok := resp["uri"].(string); ok && uri != "" {
		return uri
	}
	return ""
}

func extractGeminiVideoURLFromGeneratedSamples(gvr map[string]any) string {
	if gvr == nil {
		return ""
	}
	if samples, ok := gvr["generatedSamples"].([]any); ok {
		for _, sample := range samples {
			if sm, ok := sample.(map[string]any); ok {
				if video, ok := sm["video"].(map[string]any); ok {
					if uri, ok := video["uri"].(string); ok && uri != "" {
						return uri
					}
				}
			}
		}
	}
	return ""
}

func getVertexVideoURL(ctx context.Context, channel *model.Channel, task *model.Task, persistedKey string) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("invalid channel or task")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if url := strings.TrimSpace(task.GetResultURL()); url != "" && !isTaskProxyContentURL(url, task.TaskID) {
		return url, nil
	}
	if url := extractVertexVideoURLFromTaskData(task); url != "" {
		return url, nil
	}

	baseURL, err := service.TaskRouteSnapshotBaseURL(task.PrivateData.RouteSnapshot, channel)
	if err != nil {
		return "", fmt.Errorf("resolve task base URL: %w", err)
	}

	channelType, err := service.TaskRouteSnapshotChannelType(task.PrivateData.RouteSnapshot, channel)
	if err != nil {
		return "", fmt.Errorf("validate task channel type: %w", err)
	}
	platform, err := service.TaskRouteSnapshotAdapterPlatform(task.PrivateData.RouteSnapshot, constant.TaskPlatform(strconv.Itoa(channelType)))
	if err != nil {
		return "", fmt.Errorf("resolve task adaptor identity: %w", err)
	}
	adaptor := relay.GetTaskAdaptor(platform)
	if adaptor == nil {
		return "", fmt.Errorf("vertex task adaptor not found")
	}

	key := strings.TrimSpace(persistedKey)
	if key == "" {
		return "", fmt.Errorf("vertex key not available for task")
	}

	proxy, err := service.TaskRouteSnapshotProxy(task.PrivateData.RouteSnapshot, channel)
	if err != nil {
		return "", fmt.Errorf("resolve task proxy: %w", err)
	}
	headers, err := service.BuildTaskRouteSnapshotRequestHeaders(channel, key, http.Header{}, task.PrivateData.RouteSnapshot)
	if err != nil {
		return "", fmt.Errorf("build runtime request headers failed: %w", err)
	}
	pollBody := map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"model":   service.ResolveTaskEffectiveModelName(task),
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil {
		pollBody["polling_path"] = snapshot.PollingPath
	}
	resp, err := service.FetchTaskWithContext(ctx, adaptor, baseURL, key, pollBody, proxy, headers)
	if err != nil {
		return "", types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	if resp == nil {
		return "", types.NewErrorWithStatusCode(fmt.Errorf("fetch task returned empty response"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
	}
	if resp.Body == nil {
		return "", types.NewErrorWithStatusCode(fmt.Errorf("fetch task response body is nil"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", service.RelayErrorHandler(ctx, resp, false)
	}

	body, err := service.ReadTaskPollingResponseBody(resp.Body)
	if err != nil {
		return "", types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}

	taskInfo, parseErr := adaptor.ParseTaskResult(body, key, headers)
	if parseErr == nil && taskInfo != nil && taskInfo.ResolvedURL() != "" {
		return taskInfo.ResolvedURL(), nil
	}
	if url := extractVertexVideoURLFromPayload(body); url != "" {
		return url, nil
	}
	if parseErr != nil {
		return "", types.NewErrorWithStatusCode(parseErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	return "", types.NewErrorWithStatusCode(fmt.Errorf("vertex video url not found"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
}

func isTaskProxyContentURL(rawURL string, taskID string) bool {
	rawURL = strings.TrimSpace(rawURL)
	taskID = strings.TrimSpace(taskID)
	if rawURL == "" || taskID == "" {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return false
	}

	// A path alone is not enough to identify the gateway endpoint. Provider
	// result URLs can legitimately use the same /v1/videos/<id>/content path on
	// another host; treating those URLs as local would make the proxy fetch an
	// already-issued direct result through the wrong route. The public server
	// address is the canonical gateway authority and may include a reverse-proxy
	// path prefix. Query strings and fragments are intentionally ignored.
	configured := strings.TrimSpace(system_setting.ServerAddress)
	gateway, err := url.Parse(configured)
	if err != nil || gateway.Scheme == "" || gateway.Host == "" || gateway.User != nil {
		// A malformed or unset public address must not make the gateway's own
		// content endpoint look like a provider URL. The authority cannot be
		// checked in this state, so fail closed on the exact task path; valid
		// configured addresses still use strict authority matching below.
		expectedPath := "/v1/videos/" + url.PathEscape(taskID) + "/content"
		return parsed.EscapedPath() == expectedPath
	}
	if !sameGatewayAuthority(parsed, gateway) {
		return false
	}

	pathPrefix := strings.TrimRight(gateway.EscapedPath(), "/")
	if pathPrefix == "/" {
		pathPrefix = ""
	}
	expectedPath := pathPrefix + "/v1/videos/" + url.PathEscape(taskID) + "/content"
	return parsed.EscapedPath() == expectedPath
}

func sameGatewayAuthority(candidate, gateway *url.URL) bool {
	if candidate == nil || gateway == nil || candidate.User != nil || gateway.User != nil {
		return false
	}
	if !strings.EqualFold(candidate.Scheme, gateway.Scheme) {
		return false
	}
	if !strings.EqualFold(strings.TrimSuffix(candidate.Hostname(), "."), strings.TrimSuffix(gateway.Hostname(), ".")) {
		return false
	}
	return canonicalURLPort(candidate) == canonicalURLPort(gateway)
}

func canonicalURLPort(value *url.URL) string {
	if value == nil {
		return ""
	}
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func extractVertexVideoURLFromTaskData(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	return extractVertexVideoURLFromPayload(task.Data)
}

func extractVertexVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	resp, ok := payload["response"].(map[string]any)
	if !ok || resp == nil {
		return ""
	}

	if videos, ok := resp["videos"].([]any); ok && len(videos) > 0 {
		if video, ok := videos[0].(map[string]any); ok && video != nil {
			if b64, _ := video["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
				mime, _ := video["mimeType"].(string)
				enc, _ := video["encoding"].(string)
				return buildVideoDataURL(mime, enc, b64)
			}
		}
	}
	if b64, _ := resp["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, b64)
	}
	if video, _ := resp["video"].(string); strings.TrimSpace(video) != "" {
		if strings.HasPrefix(video, "data:") || strings.HasPrefix(video, "http://") || strings.HasPrefix(video, "https://") {
			return video
		}
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, video)
	}
	return ""
}

func buildVideoDataURL(mimeType string, encoding string, base64Data string) string {
	mime := strings.TrimSpace(mimeType)
	if mime == "" {
		enc := strings.TrimSpace(encoding)
		if enc == "" {
			enc = "mp4"
		}
		if strings.Contains(enc, "/") {
			mime = enc
		} else {
			mime = "video/" + enc
		}
	}
	return "data:" + mime + ";base64," + base64Data
}

func ensureAPIKey(uri, key string) string {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(uri) == "" {
		return uri
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(uri)), "data:") {
		return uri
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		// Do not append an unescaped credential to a malformed provider URL.
		// The caller will surface the original URL through its normal validation
		// path instead of turning it into a second, harder-to-diagnose error.
		return uri
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return uri
	}
	if _, exists := query["key"]; exists {
		return uri
	}
	query.Set("key", key)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
