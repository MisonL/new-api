package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func bindMidjourneyChannelRoute(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, requestedModel string) error {
	if c == nil || info == nil || channel == nil {
		return errors.New("midjourney channel route context is incomplete")
	}
	if common.GetContextKeyInt(c, constant.ContextKeyChannelId) != channel.Id ||
		strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyChannelKey)) == "" {
		if setupErr := middleware.SetupContextForSelectedChannel(c, channel, requestedModel); setupErr != nil {
			return setupErr.Err
		}
	}
	if info.ChannelMeta == nil || info.ChannelMeta.ChannelId != channel.Id {
		info.InitChannelMeta(c)
	}
	route, err := service.BuildChannelHealthRouteForChannel(c, info.UsingGroup, requestedModel, channel)
	if err != nil {
		return err
	}
	effectiveModel := route.EffectiveUpstreamModel
	info.UpstreamModelName = effectiveModel
	if info.ChannelMeta != nil {
		info.ChannelMeta.UpstreamModelName = effectiveModel
		info.ChannelMeta.IsModelMapped = strings.TrimSpace(effectiveModel) != strings.TrimSpace(requestedModel)
	}
	service.BindChannelHealthRouteForModel(c, info, requestedModel, effectiveModel)
	return nil
}

// bindMidjourneyFollowupRoute installs the route identity captured by the
// originating asynchronous task. Follow-up actions must keep the submission
// credential, endpoint and header-profile version even when the channel has
// since been edited. Legacy rows without a snapshot use the normal current
// channel binding explicitly.
func bindMidjourneyFollowupRoute(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel, task *model.Midjourney, requestedModel string, operationSuffix string, healthPath string) error {
	if c == nil || info == nil || channel == nil || task == nil {
		return errors.New("midjourney follow-up route context is incomplete")
	}
	key, err := resolveMidjourneyFollowupKey(channel, task)
	if err != nil {
		return err
	}
	routeSnapshot := task.PrivateData.RouteSnapshot
	if setupErr := middleware.SetupContextForSelectedChannelWithRouteSnapshotAndCredential(c, channel, requestedModel, routeSnapshot, key); setupErr != nil {
		return setupErr.Err
	}
	var baseURL string
	var route relaycommon.ChannelRouteIdentity
	profileRef := ""
	if routeSnapshot != nil && routeSnapshot.SchemaVersion >= 1 {
		baseURL, err = service.TaskRouteSnapshotBaseURL(routeSnapshot, channel)
		if err != nil {
			return err
		}
		proxy, proxyErr := service.TaskRouteSnapshotProxy(routeSnapshot, channel)
		if proxyErr != nil {
			return proxyErr
		}
		channelSetting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
		if !ok {
			channelSetting = channel.GetSetting()
		}
		channelSetting.Proxy = proxy
		common.SetContextKey(c, constant.ContextKeyChannelSetting, channelSetting)
		route, err = service.TaskRouteIdentityFromSnapshot(routeSnapshot, "submit", channel)
		if err != nil {
			return err
		}
		if suffix := strings.Trim(strings.TrimSpace(operationSuffix), "."); suffix != "" {
			route.Operation = route.Operation + "." + suffix
		}
		path, pathErr := canonicalMidjourneyHealthPath(healthPath)
		if pathErr != nil {
			return pathErr
		}
		if path != "/" {
			route.UpstreamPath = path
		}
		route.Fingerprint = ""
		route = route.Normalized()
		profileRef = service.TaskRouteSnapshotHeaderProfileRef(routeSnapshot)
	} else {
		// Legacy compatibility: bind against the current channel model mapping
		// only when no immutable snapshot was persisted.
		if err := bindMidjourneyChannelRoute(c, info, channel, requestedModel); err != nil {
			return err
		}
		baseURL = strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
		if baseURL == "" {
			return fmt.Errorf("midjourney channel base URL is empty: channel_id=%d", channel.Id)
		}
		route = info.ChannelHealthRoute.Normalized()
	}

	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	if channel.ChannelInfo.IsMultiKey {
		keys := channel.GetKeys()
		keyIndex := -1
		if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && snapshot.CredentialScoped && snapshot.MultiKeyIndex >= 0 && snapshot.MultiKeyIndex < len(keys) && strings.TrimSpace(keys[snapshot.MultiKeyIndex]) == strings.TrimSpace(key) {
			keyIndex = snapshot.MultiKeyIndex
		}
		if keyIndex < 0 {
			for index, candidate := range keys {
				if strings.TrimSpace(candidate) == strings.TrimSpace(key) {
					keyIndex = index
					break
				}
			}
		}
		if keyIndex < 0 {
			return fmt.Errorf("midjourney follow-up credential index is unavailable for channel %d", channel.Id)
		}
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, keyIndex)
		route.MultiKeyIndex = keyIndex
		route.CredentialScoped = true
		route.Fingerprint = ""
		route = route.Normalized()
	} else {
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 0)
	}
	c.Set("base_url", baseURL)
	c.Set("channel_id", channel.Id)
	c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))

	// Pin the profile captured by legacy tasks only. Schema 5 introduced the
	// authenticated static Header Policy; schema 5 and newer must keep the
	// selector disabled so a current profile cannot add or replace headers.
	if profileRef != "" && (routeSnapshot == nil || routeSnapshot.SchemaVersion < model.TaskRouteSnapshotHeaderPolicySchemaVersion) {
		otherSettings := channel.GetOtherSettings()
		if otherSettings.HeaderProfileStrategy == nil || !otherSettings.HeaderProfileStrategy.Enabled {
			return fmt.Errorf("persisted header profile %q is no longer enabled for channel %d", profileRef, channel.Id)
		}
		strategy := *otherSettings.HeaderProfileStrategy
		strategy.Mode = dto.HeaderProfileModeFixed
		strategy.SelectedProfileIDs = []string{profileRef}
		otherSettings.HeaderProfileStrategy = &strategy
		common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, otherSettings)
	}

	info.InitChannelMeta(c)
	info.UpstreamModelName = route.EffectiveUpstreamModel
	info.ChannelMeta.UpstreamModelName = route.EffectiveUpstreamModel
	info.ChannelMeta.IsModelMapped = route.EffectiveUpstreamModel != route.RequestedModel
	info.ChannelHealthRoute = &route
	path, pathErr := canonicalMidjourneyHealthPath(healthPath)
	if pathErr != nil {
		return pathErr
	}
	info.UpstreamRequestPath = path
	return nil
}

func resolveMidjourneyFollowupKey(channel *model.Channel, task *model.Midjourney) (string, error) {
	if channel == nil || task == nil {
		return "", errors.New("midjourney follow-up channel or task is nil")
	}
	storedKey := strings.TrimSpace(task.PrivateData.Key)
	if storedKey == "" {
		if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion {
			// Current-schema snapshots freeze the submission credential. Never
			// substitute a channel key after the persisted credential is missing,
			// because that would cross a rotation boundary silently.
			return "", fmt.Errorf("midjourney task %s has no persisted credential for current route snapshot", task.MjId)
		}
		if channel.ChannelInfo.IsMultiKey {
			return "", fmt.Errorf("midjourney task %s has no persisted credential for multi-key channel %d", task.MjId, channel.Id)
		}
		storedKey = strings.TrimSpace(channel.Key)
	}
	if storedKey == "" {
		return "", fmt.Errorf("midjourney channel %d has no credential", channel.Id)
	}
	resolvedKey, err := service.ResolveTaskCredential(channel, task.PrivateData.RouteSnapshot, storedKey)
	if err != nil {
		return "", fmt.Errorf("midjourney task %s: %w", task.MjId, err)
	}
	return resolvedKey, nil
}

func ensureMidjourneyHealthSelection(c *gin.Context, info *relaycommon.RelayInfo, group string, modelName string) *dto.MidjourneyResponse {
	if info == nil || info.ChannelMeta == nil || info.ChannelHealthRoute == nil {
		if info != nil && info.ChannelMeta != nil {
			service.ReleaseChannelHealthProbeForChannel(c, info.ChannelMeta.ChannelId)
		}
		return &dto.MidjourneyResponse{Code: 31, Description: "channel_health_route_unavailable"}
	}
	routeModel := strings.TrimSpace(modelName)
	if strings.TrimSpace(info.ChannelHealthRoute.RequestedModel) != "" {
		routeModel = info.ChannelHealthRoute.RequestedModel
	}
	decision := service.EvaluateChannelHealthSelectionForRoute(c, group, routeModel, *info.ChannelHealthRoute)
	if decision.Allow {
		return nil
	}
	service.ReleaseChannelHealthProbeForChannel(c, info.ChannelMeta.ChannelId)
	return &dto.MidjourneyResponse{Code: 31, Description: "channel_health_unavailable"}
}

const maxMidjourneyImageErrorBodyBytes = 8 << 10

func respondMidjourneyImageUpstreamError(c *gin.Context, resp *http.Response) {
	statusCode := http.StatusBadGateway
	preview := "<empty>"
	if resp != nil {
		if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode <= 599 {
			statusCode = resp.StatusCode
		}
		if resp.Body != nil {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxMidjourneyImageErrorBodyBytes+1))
			truncated := len(body) > maxMidjourneyImageErrorBodyBytes
			if truncated {
				body = body[:maxMidjourneyImageErrorBodyBytes]
			}
			if text := strings.TrimSpace(string(body)); text != "" {
				preview = common.LocalLogPreview(common.MaskSensitiveInfo(text))
			}
			if readErr != nil {
				preview = fmt.Sprintf("%s; read_error=%s", preview, common.MaskSensitiveInfo(readErr.Error()))
			}
			if truncated {
				preview += " [body truncated]"
			}
		}
	}
	requestContext := context.Background()
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		requestContext = c.Request.Context()
	}
	if resp != nil {
		logger.LogError(requestContext, fmt.Sprintf("midjourney image upstream returned status %d: body=%s", resp.StatusCode, preview))
	} else {
		logger.LogError(requestContext, fmt.Sprintf("midjourney image upstream returned no response: body=%s", preview))
	}
	if c != nil {
		c.JSON(statusCode, gin.H{"error": "upstream_image_fetch_failed"})
	}
}

func finalizeMidjourneyHealth(c *gin.Context, info *relaycommon.RelayInfo, succeeded bool) {
	if info == nil {
		return
	}
	if succeeded {
		service.MarkChannelHealthProbeSuccess(c, info)
		return
	}
	service.ReleaseChannelHealthProbe(c, info)
}

func midjourneyResponseSucceeded(response *dto.MidjourneyResponseWithStatusCode) bool {
	if response == nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
		return false
	}
	// Midjourney proxies may return HTTP 200 for provider-level failures. Only
	// the documented accepted task states count as a successful health probe.
	switch response.Response.Code {
	case 1, 21, 22:
		return true
	default:
		return false
	}
}

// midjourneyTaskQuotaForRow decides the refundable amount persisted on a task
// row. The row's Quota is the amount the asynchronous failure/timeout refund
// path credits back, so a submit that never charged must leave zero there.
func midjourneyTaskQuotaForRow(charged bool, quota int) int {
	if !charged {
		return 0
	}
	return quota
}

func maybeAutoDisableMidjourneyChannel(channelID int, channel *model.Channel) {
	if channel == nil || !common.AutomaticDisableChannelEnabled || !channel.GetAutoBan() {
		return
	}
	// 与 service.DisableChannel 一致：自动禁用也要留审计（system actor），
	// 否则 MJ 渠道被自动禁用时审计链缺一条记录。
	model.UpdateChannelStatusWithActor(channelID, "", common.ChannelStatusAutoDisabled, "No available account instance", model.ChannelStatusActor{
		Source: "auto_ban",
	})
}

func RelayMidjourneyImage(c *gin.Context) {
	taskId := c.Param("id")
	// MJ ids are externally visible and are not guaranteed to be globally
	// secret. Scope the lookup to the authenticated owner before reading the
	// stored image URL, channel proxy, or route snapshot; otherwise any valid
	// token could fetch another user's generated image by guessing its id.
	midjourneyTask := model.GetByMJId(common.GetContextKeyInt(c, constant.ContextKeyUserId), taskId)
	if midjourneyTask == nil {
		c.JSON(400, gin.H{
			"error": "midjourney_task_not_found",
		})
		return
	}
	channel, channelErr := model.CacheGetChannel(midjourneyTask.ChannelId)
	if channelErr != nil && midjourneyTask.PrivateData.RouteSnapshot == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "get_channel_info_failed"})
		return
	}
	// A persisted route snapshot owns the proxy used by the original task. It
	// remains usable after the channel row is deleted; an old task without a
	// snapshot cannot safely recover a deleted channel's proxy and is rejected.
	proxy := ""
	if midjourneyTask.PrivateData.RouteSnapshot != nil || channel != nil {
		var err error
		proxy, err = service.TaskRouteSnapshotProxy(midjourneyTask.PrivateData.RouteSnapshot, channel)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "task_route_proxy_unavailable"})
			return
		}
	}
	httpClient, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "proxy_url_invalid"})
		return
	}
	requestContext := context.Background()
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		requestContext = c.Request.Context()
	}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, midjourneyTask.ImageUrl, nil)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image_url_invalid"})
		return
	}
	request = request.WithContext(common.WithSSRFValidation(request.Context()))
	resp, err := httpClient.Do(request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "http_get_image_failed",
		})
		return
	}
	if resp.Body == nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": "http_get_image_empty_response",
		})
		return
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode != http.StatusOK {
		respondMidjourneyImageUpstreamError(c, resp)
		return
	}
	// 从Content-Type头获取MIME类型
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		// 如果无法确定内容类型，则默认为jpeg
		contentType = "image/jpeg"
	}
	// 设置响应的内容类型
	c.Writer.Header().Set("Content-Type", contentType)
	// 将图片流式传输到响应体
	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		log.Println("Failed to stream image:", err)
	}
	return
}

// acquireMidjourneyChannelConcurrency covers the Midjourney proxy paths that
// call the upstream directly instead of entering controller.Relay's selector
// loop. A saturated channel is represented as code 30 to preserve the
// existing Midjourney 429 response contract; coordinator failures use code 31
// and are mapped to 503 by the controller.
func acquireMidjourneyChannelConcurrency(c *gin.Context, channel *model.Channel) *dto.MidjourneyResponse {
	lease, err := service.AcquireChannelConcurrency(c, channel)
	if err == nil {
		service.SetChannelConcurrencyLease(c, lease)
		return nil
	}
	if errors.Is(err, service.ErrChannelConcurrencyExceeded) {
		return &dto.MidjourneyResponse{Code: 30, Description: "channel_concurrency_exceeded"}
	}
	return &dto.MidjourneyResponse{Code: 31, Description: "channel_concurrency_unavailable"}
}

func RelayMidjourneyNotify(c *gin.Context) *dto.MidjourneyResponse {
	var midjRequest dto.MidjourneyDto
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "bind_request_body_failed",
			Properties:  nil,
			Result:      "",
		}
	}
	midjourneyTask := model.GetByOnlyMJId(midjRequest.MjId)
	if midjourneyTask == nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "midjourney_task_not_found",
			Properties:  nil,
			Result:      "",
		}
	}
	previousStatusRaw := midjourneyTask.Status
	previousStatus := strings.ToUpper(strings.TrimSpace(previousStatusRaw))
	incomingStatus := strings.ToUpper(strings.TrimSpace(midjRequest.Status))
	if normalizedStatus, recognized := service.NormalizeMidjourneyTaskStatus(incomingStatus); recognized {
		incomingStatus = string(normalizedStatus)
		midjRequest.Status = incomingStatus
	}
	if incomingStatus != "" && !service.ShouldApplyTaskStatus(model.TaskStatus(previousStatus), model.TaskStatus(incomingStatus)) {
		logger.LogWarn(c, fmt.Sprintf(
			"Midjourney notify ignored stale status transition: task_id=%s current=%s incoming=%s",
			midjourneyTask.MjId,
			previousStatus,
			incomingStatus,
		))
		return nil
	}
	mergeResult, mergeErr := service.MergeMidjourneyTask(midjourneyTask, midjRequest)
	if mergeErr != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "merge_midjourney_task_failed",
		}
	}
	if !mergeResult.Changed {
		return nil
	}
	won, updateErr := midjourneyTask.UpdateWithStatus(previousStatusRaw)
	if updateErr != nil {
		err = updateErr
	} else if !won {
		return nil
	} else if mergeResult.TerminalFailureTransition {
		service.RefundMidjourneyTaskQuota(c, midjourneyTask, "构图失败")
	}
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "update_midjourney_task_failed",
		}
	}

	return nil
}

func coverMidjourneyTaskDto(c *gin.Context, originTask *model.Midjourney) (midjourneyTask dto.MidjourneyDto) {
	midjourneyTask.MjId = originTask.MjId
	midjourneyTask.Progress = originTask.Progress
	midjourneyTask.PromptEn = originTask.PromptEn
	midjourneyTask.State = originTask.State
	midjourneyTask.SubmitTime = originTask.SubmitTime
	midjourneyTask.StartTime = originTask.StartTime
	midjourneyTask.FinishTime = originTask.FinishTime
	midjourneyTask.ImageUrl = ""
	if originTask.ImageUrl != "" && setting.MjForwardUrlEnabled {
		midjourneyTask.ImageUrl = system_setting.ServerAddress + "/mj/image/" + originTask.MjId
		if originTask.Status != "SUCCESS" {
			midjourneyTask.ImageUrl += "?rand=" + strconv.FormatInt(time.Now().UnixNano(), 10)
		}
	} else {
		midjourneyTask.ImageUrl = originTask.ImageUrl
	}
	if originTask.VideoUrl != "" {
		midjourneyTask.VideoUrl = originTask.VideoUrl
	}
	midjourneyTask.Status = originTask.Status
	midjourneyTask.FailReason = originTask.FailReason
	midjourneyTask.Action = originTask.Action
	midjourneyTask.Description = originTask.Description
	midjourneyTask.Prompt = originTask.Prompt
	if originTask.Buttons != "" {
		var buttons []dto.ActionButton
		err := common.Unmarshal([]byte(originTask.Buttons), &buttons)
		if err == nil {
			midjourneyTask.Buttons = buttons
		}
	}
	if originTask.VideoUrls != "" {
		var videoUrls []dto.ImgUrls
		err := common.Unmarshal([]byte(originTask.VideoUrls), &videoUrls)
		if err == nil {
			midjourneyTask.VideoUrls = videoUrls
		}
	}
	if originTask.Properties != "" {
		var properties dto.Properties
		err := common.Unmarshal([]byte(originTask.Properties), &properties)
		if err == nil {
			midjourneyTask.Properties = &properties
		}
	}
	return
}

func RelaySwapFace(c *gin.Context, info *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	var swapFaceRequest dto.SwapFaceRequest
	err := common.UnmarshalBodyReusable(c, &swapFaceRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}

	info.InitChannelMeta(c)
	channel, err := model.GetChannelById(info.ChannelId, true)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
	}
	modelName := service.CovertMjpActionToModelName(constant.MjActionSwapFace)
	info.OriginModelName = modelName

	if swapFaceRequest.SourceBase64 == "" || swapFaceRequest.TargetBase64 == "" {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "sour_base64_and_target_base64_is_required")
	}
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}

	userQuota, err := model.GetUserQuota(info.UserId, false)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}

	if userQuota-priceData.Quota < 0 {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "quota_not_enough",
		}
	}
	requestURL, pathErr := canonicalMidjourneyRequestTarget(c.Request.URL.String())
	if pathErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "invalid_midjourney_request_path")
	}
	healthPath, pathErr := canonicalMidjourneyHealthPath(requestURL)
	if pathErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "invalid_midjourney_request_path")
	}
	info.UpstreamRequestPath = healthPath
	if err := bindMidjourneyChannelRoute(c, info, channel, modelName); err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "setup_channel_context_failed")
	}
	routeSnapshot, snapshotErr := service.BuildTaskRouteSnapshot(c, info, constant.TaskPlatformMidjourney, constant.MjActionSwapFace)
	if snapshotErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "build_task_route_snapshot_failed")
	}
	if healthErr := ensureMidjourneyHealthSelection(c, info, info.UsingGroup, modelName); healthErr != nil {
		return healthErr
	}
	requestSucceeded := false
	defer func() { finalizeMidjourneyHealth(c, info, requestSucceeded) }()
	if concurrencyErr := acquireMidjourneyChannelConcurrency(c, channel); concurrencyErr != nil {
		return concurrencyErr
	}
	defer service.ReleaseChannelConcurrency(c)
	baseURL := c.GetString("base_url")
	fullRequestURL := fmt.Sprintf("%s%s", baseURL, requestURL)
	mjResp, _, err := service.DoMidjourneyHttpRequest(c, time.Second*60, fullRequestURL)
	if err != nil {
		observeMidjourneyFailure(c, info, mjResp, err)
		if mjResp == nil {
			return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "midjourney_empty_response")
		}
		return &mjResp.Response
	}
	if mjResp == nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "midjourney_empty_response")
	}
	observeMidjourneyHTTPStatus(c, info, mjResp)
	requestSucceeded = midjourneyResponseSucceeded(mjResp)
	defer func() {
		if mjResp.StatusCode == 200 && mjResp.Response.Code == 1 {
			err := service.PostConsumeQuota(info, priceData.Quota, 0, true)
			if err != nil {
				common.SysLog("error consuming token remain quota: " + err.Error())
			}

			tokenName := c.GetString("token_name")
			logContent := fmt.Sprintf("模型固定价格 %.2f，分组倍率 %.2f，操作 %s", priceData.ModelPrice, priceData.GroupRatioInfo.GroupRatio, constant.MjActionSwapFace)
			other := service.GenerateMjOtherInfo(info, priceData)
			usageStatsErr := service.RecordUsageStats(info.UserId, info.ChannelId, priceData.Quota)
			service.MarkUsageStatsFailure(c, info, priceData.Quota, usageStatsErr, other)
			model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
				ChannelId: info.ChannelId,
				ModelName: modelName,
				TokenName: tokenName,
				Quota:     priceData.Quota,
				Content:   logContent,
				TokenId:   info.TokenId,
				Group:     info.UsingGroup,
				Other:     other,
			})
		}
	}()
	midjResponse := &mjResp.Response
	midjourneyTask := &model.Midjourney{
		UserId:      info.UserId,
		Code:        midjResponse.Code,
		Action:      constant.MjActionSwapFace,
		MjId:        midjResponse.Result,
		Prompt:      "InsightFace",
		PromptEn:    "",
		Description: midjResponse.Description,
		State:       "",
		SubmitTime:  info.StartTime.UnixNano() / int64(time.Millisecond),
		StartTime:   time.Now().UnixNano() / int64(time.Millisecond),
		FinishTime:  0,
		ImageUrl:    "",
		Status:      "",
		Progress:    "0%",
		FailReason:  "",
		ChannelId:   info.ChannelId,
		Quota:       priceData.Quota,
		PrivateData: model.TaskPrivateData{
			Key:           common.GetContextKeyString(c, constant.ContextKeyChannelKey),
			RouteSnapshot: routeSnapshot,
			// 异步退款按 token_id 退还令牌额度；不落库就只能退钱包。
			TokenId: info.TokenId,
		},
	}
	// 只有 200 + Code==1 才会真正扣费（见上面的 defer）。任务行上的 Quota
	// 同时被异步失败退款路径使用：未扣费却留下可退款金额，等于失败时凭空
	// 加额度。主提交路径有同样的防护，swap_face 此前缺失。
	midjourneyTask.Quota = midjourneyTaskQuotaForRow(
		mjResp.StatusCode == 200 && midjResponse.Code == 1, priceData.Quota)
	err = midjourneyTask.Insert()
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "insert_midjourney_task_failed")
	}
	c.Writer.WriteHeader(mjResp.StatusCode)
	respBody, err := common.Marshal(midjResponse)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "unmarshal_response_body_failed")
	}
	_, err = io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "copy_response_body_failed")
	}
	return nil
}

func RelayMidjourneyTaskImageSeed(c *gin.Context, relayInfo *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	taskId := c.Param("id")
	userId := c.GetInt("id")
	originTask := model.GetByMJId(userId, taskId)
	if originTask == nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_no_found")
	}
	channel, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "该任务所属渠道已被禁用")
	}
	modelName := service.CovertMjpActionToModelName(strings.TrimSpace(originTask.Action))
	if strings.TrimSpace(originTask.Action) == "" {
		modelName = "midjourney"
	}

	requestURL, pathErr := canonicalMidjourneyRequestTarget(c.Request.URL.String())
	if pathErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "invalid_midjourney_request_path")
	}
	healthPath, pathErr := canonicalMidjourneyHealthPath(requestURL)
	if pathErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "invalid_midjourney_request_path")
	}
	if relayInfo == nil {
		relayInfo = &relaycommon.RelayInfo{}
	}
	relayInfo.OriginModelName = modelName
	relayInfo.UsingGroup = common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	relayInfo.RelayMode = relayconstant.RelayModeMidjourneyTaskImageSeed
	relayInfo.UpstreamRequestPath = healthPath
	if err := bindMidjourneyFollowupRoute(c, relayInfo, channel, originTask, modelName, "image_seed", healthPath); err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "setup_channel_context_failed")
	}
	if healthErr := ensureMidjourneyHealthSelection(c, relayInfo, relayInfo.UsingGroup, modelName); healthErr != nil {
		return healthErr
	}
	requestSucceeded := false
	defer func() { finalizeMidjourneyHealth(c, relayInfo, requestSucceeded) }()
	if concurrencyErr := acquireMidjourneyChannelConcurrency(c, channel); concurrencyErr != nil {
		return concurrencyErr
	}
	defer service.ReleaseChannelConcurrency(c)
	fullRequestURL := fmt.Sprintf("%s%s", common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl), requestURL)
	midjResponseWithStatus, _, err := service.DoMidjourneyHttpRequest(c, time.Second*30, fullRequestURL)
	if err != nil {
		observeMidjourneyFailure(c, relayInfo, midjResponseWithStatus, err)
		if midjResponseWithStatus == nil {
			return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "midjourney_empty_response")
		}
		return &midjResponseWithStatus.Response
	}
	if midjResponseWithStatus == nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "midjourney_empty_response")
	}
	observeMidjourneyHTTPStatus(c, relayInfo, midjResponseWithStatus)
	requestSucceeded = midjourneyResponseSucceeded(midjResponseWithStatus)
	midjResponse := &midjResponseWithStatus.Response
	c.Writer.WriteHeader(midjResponseWithStatus.StatusCode)
	respBody, err := common.Marshal(midjResponse)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "unmarshal_response_body_failed")
	}
	service.IOCopyBytesGracefully(c, nil, respBody)
	return nil
}

func RelayMidjourneyTask(c *gin.Context, relayMode int) *dto.MidjourneyResponse {
	userId := c.GetInt("id")
	var err error
	var respBody []byte
	switch relayMode {
	case relayconstant.RelayModeMidjourneyTaskFetch:
		taskId := c.Param("id")
		originTask := model.GetByMJId(userId, taskId)
		if originTask == nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "task_no_found",
			}
		}
		midjourneyTask := coverMidjourneyTaskDto(c, originTask)
		respBody, err = common.Marshal(midjourneyTask)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	case relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		var condition = struct {
			IDs []string `json:"ids"`
		}{}
		err = c.BindJSON(&condition)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "do_request_failed",
			}
		}
		var tasks []dto.MidjourneyDto
		if len(condition.IDs) != 0 {
			originTasks := model.GetByMJIds(userId, condition.IDs)
			for _, originTask := range originTasks {
				midjourneyTask := coverMidjourneyTaskDto(c, originTask)
				tasks = append(tasks, midjourneyTask)
			}
		}
		if tasks == nil {
			tasks = make([]dto.MidjourneyDto, 0)
		}
		respBody, err = common.Marshal(tasks)
		if err != nil {
			return &dto.MidjourneyResponse{
				Code:        4,
				Description: "unmarshal_response_body_failed",
			}
		}
	}

	c.Writer.Header().Set("Content-Type", "application/json")

	_, err = io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "copy_response_body_failed",
		}
	}
	return nil
}

func RelayMidjourneySubmit(c *gin.Context, relayInfo *relaycommon.RelayInfo) *dto.MidjourneyResponse {
	consumeQuota := true
	var originTask *model.Midjourney
	var midjRequest dto.MidjourneyRequest
	err := common.UnmarshalBodyReusable(c, &midjRequest)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "bind_request_body_failed")
	}
	requestURL, pathErr := canonicalMidjourneyRequestTarget(c.Request.URL.String())
	if pathErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "invalid_midjourney_request_path")
	}
	healthPath, pathErr := canonicalMidjourneyHealthPath(requestURL)
	if pathErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "invalid_midjourney_request_path")
	}

	relayInfo.InitChannelMeta(c)

	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyAction { // midjourney plus，需要从customId中获取任务信息
		mjErr := service.CoverPlusActionToNormalAction(&midjRequest)
		if mjErr != nil {
			return mjErr
		}
		relayInfo.RelayMode = relayconstant.RelayModeMidjourneyChange
	}
	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyVideo {
		midjRequest.Action = constant.MjActionVideo
	}
	// The relay mode may normalize or derive the action below (simple-change,
	// modal, plus actions, and the fixed endpoint modes). Do not derive the
	// route model from the raw request action before that normalization.
	modelName := ""

	if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyImagine { //绘画任务，此类任务可重复
		if midjRequest.Prompt == "" {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "prompt_is_required")
		}
		midjRequest.Action = constant.MjActionImagine
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyDescribe { //按图生文任务，此类任务可重复
		midjRequest.Action = constant.MjActionDescribe
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyEdits { //编辑任务，此类任务可重复
		midjRequest.Action = constant.MjActionEdits
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyShorten { //缩短任务，此类任务可重复，plus only
		midjRequest.Action = constant.MjActionShorten
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyBlend { //绘画任务，此类任务可重复
		midjRequest.Action = constant.MjActionBlend
	} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyUpload { //绘画任务，此类任务可重复
		midjRequest.Action = constant.MjActionUpload
	} else if midjRequest.TaskId != "" { //放大、变换任务，此类任务，如果重复且已有结果，远端api会直接返回最终结果
		mjId := ""
		if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyChange {
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			} else if midjRequest.Index == 0 {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "index_is_required")
			}
			//action = midjRequest.Action
			mjId = midjRequest.TaskId
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneySimpleChange {
			if midjRequest.Content == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_is_required")
			}
			params := service.ConvertSimpleChangeParams(midjRequest.Content)
			if params == nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "content_parse_failed")
			}
			mjId = params.TaskId
			midjRequest.Action = params.Action
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyModal {
			//if midjRequest.MaskBase64 == "" {
			//	return service.MidjourneyErrorWrapper(constant.MjRequestError, "mask_base64_is_required")
			//}
			mjId = midjRequest.TaskId
			midjRequest.Action = constant.MjActionModal
		} else if relayInfo.RelayMode == relayconstant.RelayModeMidjourneyVideo {
			midjRequest.Action = constant.MjActionVideo
			if midjRequest.TaskId == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_id_is_required")
			} else if midjRequest.Action == "" {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
			}
			mjId = midjRequest.TaskId
		}

		originTask = model.GetByMJId(relayInfo.UserId, mjId)
		if originTask == nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_not_found")
		} else { //原任务的Status=SUCCESS，则可以做放大UPSCALE、变换VARIATION等动作，此时必须使用原来的请求地址才能正确处理
			if setting.MjActionCheckSuccessEnabled {
				if originTask.Status != "SUCCESS" && relayInfo.RelayMode != relayconstant.RelayModeMidjourneyModal {
					return service.MidjourneyErrorWrapper(constant.MjRequestError, "task_status_not_success")
				}
			}
			channel, err := model.GetChannelById(originTask.ChannelId, true)
			if err != nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
			}
			if channel.Status != common.ChannelStatusEnabled {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "该任务所属渠道已被禁用")
			}
			modelName = service.CovertMjpActionToModelName(midjRequest.Action)
			if err := bindMidjourneyFollowupRoute(c, relayInfo, channel, originTask, modelName, "followup_"+strings.ToLower(midjRequest.Action), healthPath); err != nil {
				return service.MidjourneyErrorWrapper(constant.MjRequestError, "setup_channel_context_failed")
			}
			log.Printf("检测到此操作为放大、变换、重绘，获取原channel信息: %s,%s", strconv.Itoa(originTask.ChannelId), channel.GetBaseURL())
		}
		midjRequest.Prompt = originTask.Prompt

		//if channelType == common.ChannelTypeMidjourneyPlus {
		//	// plus
		//} else {
		//	// 普通版渠道
		//
		//}
	}
	// Recompute after every action-normalization branch. This keeps the route
	// identity aligned with the action actually sent upstream.
	modelName = service.CovertMjpActionToModelName(midjRequest.Action)

	if midjRequest.Action == constant.MjActionInPaint || midjRequest.Action == constant.MjActionCustomZoom {
		consumeQuota = false
	}

	//baseURL := common.ChannelBaseURLs[channelType]
	channelID := c.GetInt("channel_id")
	if channelID <= 0 {
		channelID = relayInfo.ChannelId
	}
	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "get_channel_info_failed")
	}
	relayInfo.UpstreamRequestPath = healthPath
	if originTask != nil {
		if err := bindMidjourneyFollowupRoute(c, relayInfo, channel, originTask, modelName, "followup_"+strings.ToLower(midjRequest.Action), healthPath); err != nil {
			return service.MidjourneyErrorWrapper(constant.MjRequestError, "setup_channel_context_failed")
		}
	} else if err := bindMidjourneyChannelRoute(c, relayInfo, channel, modelName); err != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "setup_channel_context_failed")
	}
	routeSnapshot, snapshotErr := service.BuildTaskRouteSnapshot(c, relayInfo, constant.TaskPlatformMidjourney, midjRequest.Action)
	if snapshotErr != nil {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "build_task_route_snapshot_failed")
	}
	baseURL := common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl)
	if strings.TrimSpace(baseURL) == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	}
	if strings.TrimSpace(baseURL) == "" {
		return service.MidjourneyErrorWrapper(constant.MjRequestError, "channel_base_url_empty")
	}
	//midjRequest.NotifyHook = "http://127.0.0.1:3000/mj/notify"
	fullRequestURL := fmt.Sprintf("%s%s", strings.TrimRight(baseURL, "/"), requestURL)
	if healthErr := ensureMidjourneyHealthSelection(c, relayInfo, relayInfo.UsingGroup, modelName); healthErr != nil {
		return healthErr
	}
	requestSucceeded := false
	defer func() { finalizeMidjourneyHealth(c, relayInfo, requestSucceeded) }()
	if concurrencyErr := acquireMidjourneyChannelConcurrency(c, channel); concurrencyErr != nil {
		return concurrencyErr
	}
	defer service.ReleaseChannelConcurrency(c)

	priceData, err := helper.ModelPriceHelperPerCall(c, relayInfo)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}

	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: err.Error(),
		}
	}

	if consumeQuota && userQuota-priceData.Quota < 0 {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "quota_not_enough",
		}
	}

	midjResponseWithStatus, responseBody, err := service.DoMidjourneyHttpRequest(c, time.Second*60, fullRequestURL)
	if err != nil {
		observeMidjourneyFailure(c, relayInfo, midjResponseWithStatus, err)
		if midjResponseWithStatus == nil {
			return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "midjourney_empty_response")
		}
		return &midjResponseWithStatus.Response
	}
	if midjResponseWithStatus == nil {
		return service.MidjourneyErrorWrapper(constant.MjErrorUnknown, "midjourney_empty_response")
	}
	observeMidjourneyHTTPStatus(c, relayInfo, midjResponseWithStatus)
	requestSucceeded = midjourneyResponseSucceeded(midjResponseWithStatus)
	midjResponse := &midjResponseWithStatus.Response

	defer func() {
		if consumeQuota && midjResponseWithStatus.StatusCode == 200 {
			err := service.PostConsumeQuota(relayInfo, priceData.Quota, 0, true)
			if err != nil {
				common.SysLog("error consuming token remain quota: " + err.Error())
			}
			tokenName := c.GetString("token_name")
			logContent := fmt.Sprintf("模型固定价格 %.2f，分组倍率 %.2f，操作 %s，ID %s", priceData.ModelPrice, priceData.GroupRatioInfo.GroupRatio, midjRequest.Action, midjResponse.Result)
			other := service.GenerateMjOtherInfo(relayInfo, priceData)
			usageStatsErr := service.RecordUsageStats(relayInfo.UserId, relayInfo.ChannelId, priceData.Quota)
			service.MarkUsageStatsFailure(c, relayInfo, priceData.Quota, usageStatsErr, other)
			model.RecordConsumeLog(c, relayInfo.UserId, model.RecordConsumeLogParams{
				ChannelId: relayInfo.ChannelId,
				ModelName: modelName,
				TokenName: tokenName,
				Quota:     priceData.Quota,
				Content:   logContent,
				TokenId:   relayInfo.TokenId,
				Group:     relayInfo.UsingGroup,
				Other:     other,
			})
		}
	}()

	// 文档：https://github.com/novicezk/midjourney-proxy/blob/main/docs/api.md
	//1-提交成功
	// 21-任务已存在（处理中或者有结果了） {"code":21,"description":"任务已存在","result":"0741798445574458","properties":{"status":"SUCCESS","imageUrl":"https://xxxx"}}
	// 22-排队中 {"code":22,"description":"排队中，前面还有1个任务","result":"0741798445574458","properties":{"numberOfQueues":1,"discordInstanceId":"1118138338562560102"}}
	// 23-队列已满，请稍后再试 {"code":23,"description":"队列已满，请稍后尝试","result":"14001929738841620","properties":{"discordInstanceId":"1118138338562560102"}}
	// 24-prompt包含敏感词 {"code":24,"description":"可能包含敏感词","properties":{"promptEn":"nude body","bannedWord":"nude"}}
	// other: 提交错误，description为错误描述
	midjourneyTask := &model.Midjourney{
		UserId:      relayInfo.UserId,
		Code:        midjResponse.Code,
		Action:      midjRequest.Action,
		MjId:        midjResponse.Result,
		Prompt:      midjRequest.Prompt,
		PromptEn:    "",
		Description: midjResponse.Description,
		State:       "",
		SubmitTime:  time.Now().UnixNano() / int64(time.Millisecond),
		StartTime:   0,
		FinishTime:  0,
		ImageUrl:    "",
		Status:      "",
		Progress:    "0%",
		FailReason:  "",
		ChannelId:   relayInfo.ChannelId,
		Quota:       priceData.Quota,
		PrivateData: model.TaskPrivateData{
			Key:           common.GetContextKeyString(c, constant.ContextKeyChannelKey),
			RouteSnapshot: routeSnapshot,
			// 异步退款按 token_id 退还令牌额度；不落库就只能退钱包。
			TokenId: relayInfo.TokenId,
		},
	}
	if midjResponse.Code == 3 {
		//无实例账号自动禁用渠道（No available account instance）
		channel, err := model.GetChannelById(midjourneyTask.ChannelId, true)
		if err != nil {
			common.SysLog("get_channel_null: " + err.Error())
		}
		maybeAutoDisableMidjourneyChannel(midjourneyTask.ChannelId, channel)
	}
	if midjResponse.Code != 1 && midjResponse.Code != 21 && midjResponse.Code != 22 {
		//非1-提交成功,21-任务已存在和22-排队中，则记录错误原因
		midjourneyTask.FailReason = midjResponse.Description
		consumeQuota = false
	}
	// Quota is also used later by the asynchronous failure/timeout refund path.
	// A provider-level submission failure (or a non-billable follow-up action)
	// never charged the caller, so do not leave a refundable amount on its task
	// row.
	midjourneyTask.Quota = midjourneyTaskQuotaForRow(consumeQuota, priceData.Quota)

	if midjResponse.Code == 21 { //21-任务已存在（处理中或者有结果了）
		// 将 properties 转换为一个 map
		properties, ok := midjResponse.Properties.(map[string]interface{})
		if ok {
			imageUrl, ok1 := properties["imageUrl"].(string)
			status, ok2 := properties["status"].(string)
			if ok1 && ok2 {
				midjourneyTask.ImageUrl = imageUrl
				midjourneyTask.Status = status
				if status == "SUCCESS" {
					midjourneyTask.Progress = "100%"
					midjourneyTask.StartTime = time.Now().UnixNano() / int64(time.Millisecond)
					midjourneyTask.FinishTime = time.Now().UnixNano() / int64(time.Millisecond)
					midjResponse.Code = 1
				}
			}
		}
		//修改返回值
		if midjRequest.Action != constant.MjActionInPaint && midjRequest.Action != constant.MjActionCustomZoom {
			newBody := strings.Replace(string(responseBody), `"code":21`, `"code":1`, -1)
			responseBody = []byte(newBody)
		}
	}
	if midjResponse.Code == 1 && midjRequest.Action == "UPLOAD" {
		midjourneyTask.Progress = "100%"
		midjourneyTask.Status = "SUCCESS"
	}
	err = midjourneyTask.Insert()
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "insert_midjourney_task_failed",
		}
	}

	if midjResponse.Code == 22 { //22-排队中，说明任务已存在
		//修改返回值
		newBody := strings.Replace(string(responseBody), `"code":22`, `"code":1`, -1)
		responseBody = []byte(newBody)
	}
	//resp.Body = io.NopCloser(bytes.NewBuffer(responseBody))
	bodyReader := io.NopCloser(bytes.NewBuffer(responseBody))

	//for k, v := range resp.Header {
	//	c.Writer.Header().Set(k, v[0])
	//}
	c.Writer.WriteHeader(midjResponseWithStatus.StatusCode)

	_, err = io.Copy(c.Writer, bodyReader)
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "copy_response_body_failed",
		}
	}
	err = bodyReader.Close()
	if err != nil {
		return &dto.MidjourneyResponse{
			Code:        4,
			Description: "close_response_body_failed",
		}
	}
	return nil
}

func observeMidjourneyFailure(c *gin.Context, info *relaycommon.RelayInfo, response *dto.MidjourneyResponseWithStatusCode, err error) {
	if err == nil {
		return
	}
	statusCode := http.StatusBadGateway
	if response != nil && response.StatusCode >= http.StatusBadRequest {
		statusCode = response.StatusCode
	}
	apiErr := types.NewOpenAIError(err, types.ErrorCodeBadResponse, statusCode)
	service.ObserveChannelHealthFailure(c, info, apiErr)
}

func observeMidjourneyHTTPStatus(c *gin.Context, info *relaycommon.RelayInfo, response *dto.MidjourneyResponseWithStatusCode) {
	if response == nil || response.StatusCode < http.StatusBadRequest {
		return
	}
	message := strings.TrimSpace(response.Response.Description)
	if message == "" {
		message = fmt.Sprintf("midjourney upstream returned status %d", response.StatusCode)
	}
	apiErr := types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, response.StatusCode)
	service.ObserveChannelHealthFailure(c, info, apiErr)
}

type taskChangeParams struct {
	ID     string
	Action string
	Index  int
}

func canonicalMidjourneyRequestTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("midjourney request path is empty")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return "", fmt.Errorf("parse midjourney request path: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("midjourney request path must be relative")
	}
	path := parsed.Path
	if path == "" {
		return "", errors.New("midjourney request path is empty")
	}
	rawPath := parsed.RawPath
	if rawPath == "" {
		rawPath = path
	}
	if strings.ContainsAny(rawPath, "\\") || strings.Contains(strings.ToLower(rawPath), "%2f") || strings.Contains(strings.ToLower(rawPath), "%5c") {
		return "", errors.New("midjourney request path contains an encoded separator")
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("midjourney request path contains traversal")
		}
	}
	if index := strings.Index(path, "/mj/"); index > 0 {
		prefix := path[:index]
		if strings.Count(prefix, "/") != 1 || strings.Trim(prefix, "/") == "" {
			return "", errors.New("midjourney request path has an invalid mode prefix")
		}
		path = path[index:]
	} else if !strings.HasPrefix(path, "/mj/") {
		return "", errors.New("midjourney request path must target the mj route")
	}
	// Registered Midjourney endpoints are canonicalized without a trailing
	// slash. The same normalized path is used for the outbound request and the
	// health-route identity; retaining the slash here would split those two
	// identities even though the route validator accepts it.
	path = strings.TrimRight(path, "/")
	if !isCanonicalMidjourneyRequestPath(path) {
		return "", fmt.Errorf("midjourney request path is not registered: %s", path)
	}
	if parsed.RawQuery != "" {
		return path + "?" + parsed.RawQuery, nil
	}
	return path, nil
}

func isCanonicalMidjourneyRequestPath(path string) bool {
	path = strings.TrimRight(path, "/")
	switch path {
	case "/mj/submit/action",
		"/mj/submit/shorten",
		"/mj/submit/modal",
		"/mj/submit/imagine",
		"/mj/submit/change",
		"/mj/submit/simple-change",
		"/mj/submit/describe",
		"/mj/submit/blend",
		"/mj/submit/edits",
		"/mj/submit/video",
		"/mj/insight-face/swap",
		"/mj/submit/upload-discord-images",
		"/mj/task/list-by-condition":
		return true
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "mj" || parts[1] != "task" {
		return false
	}
	return parts[2] != "" && (parts[3] == "fetch" || parts[3] == "image-seed")
}

func canonicalMidjourneyHealthPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("midjourney health path is empty")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return "", fmt.Errorf("parse midjourney health path: %w", err)
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", errors.New("midjourney health path must be a relative path")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		path = "/"
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return "", errors.New("midjourney health path contains traversal")
		}
	}
	// These two short forms are the canonical health identities for the
	// parameterized task endpoints. They are accepted for callers that already
	// reduced the task ID before binding the health route.
	if path == "/mj/task/fetch" || path == "/mj/task/image-seed" {
		return path, nil
	}
	if !isCanonicalMidjourneyRequestPath(path) {
		return "", fmt.Errorf("midjourney health path is not registered: %s", path)
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 4 && parts[0] == "mj" && parts[1] == "task" {
		return "/mj/task/" + parts[3], nil
	}
	return path, nil
}
