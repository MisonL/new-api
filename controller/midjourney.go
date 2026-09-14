package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const midjourneyTaskTimeout = time.Hour

func midjourneyTaskTimeoutReason() string {
	return fmt.Sprintf("上游任务超时（超过%d小时）", int(midjourneyTaskTimeout/time.Hour))
}

func UpdateMidjourneyTaskBulk() {
	//imageModel := "midjourney"
	ctx := context.TODO()
	common.SleepBeforeMaintenanceLoop(common.MidjourneyTaskPollingInitialDelay)
	for {
		time.Sleep(common.MidjourneyTaskPollingInterval)
		sweepTimedOutMidjourneyTasks(ctx, time.Now())

		tasks, queryErr := model.GetAllUnFinishTasksWithError(constant.TaskQueryLimit)
		if queryErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Midjourney polling query/row error; continuing with valid tasks: %v", queryErr))
		}
		if len(tasks) == 0 {
			continue
		}

		logger.LogInfo(ctx, fmt.Sprintf("检测到未完成的任务数有: %v", len(tasks)))
		taskChannelM := make(map[int][]*model.Midjourney)
		nullTasks := make([]*model.Midjourney, 0)
		for _, task := range tasks {
			if strings.TrimSpace(task.MjId) == "" {
				// A task without an upstream ID cannot ever be polled. Transition it
				// individually so the same CAS and refund semantics apply as for
				// other locally unrecoverable failures.
				nullTasks = append(nullTasks, task)
				continue
			}
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], task)
		}
		for _, task := range nullTasks {
			if won, err := markMidjourneyTaskPollingFailure(task, "任务缺少上游任务 ID"); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix null mj_id task error: %v", err))
			} else if won {
				logger.LogInfo(ctx, fmt.Sprintf("Fix null mj_id task success: %d", task.Id))
			}
		}
		if len(taskChannelM) == 0 {
			continue
		}

		for channelId, channelTasks := range taskChannelM {
			logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(channelTasks)))
			if len(channelTasks) == 0 {
				continue
			}
			midjourneyChannel, err := model.CacheGetChannel(channelId)
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("CacheGetChannel: %v", err))
				// Channel cache/Redis outages are transient. Keep tasks pending and
				// let the timeout sweeper handle only genuinely expired tasks.
				continue
			}
			batchesByKey := make(map[midjourneyTaskPollingBatchKey][]*model.Midjourney)
			for _, task := range channelTasks {
				key, keyErr := resolveMidjourneyPollingKey(midjourneyChannel, task)
				if keyErr != nil {
					// Credential rotation/disablement is a transient control-plane
					// state for an in-flight task. Keep it pending so a later refresh
					// can restore the persisted credential; the timeout sweeper is the
					// only path that turns an unresolved task into a terminal failure.
					logger.LogError(ctx, keyErr.Error())
					continue
				}
				batchKey, identityErr := midjourneyTaskPollingBatchIdentity(midjourneyChannel, task, key)
				if identityErr != nil {
					// A route snapshot may be temporarily unavailable while channel
					// configuration is being refreshed. Do not convert that transient
					// read/parse failure into an irreversible task failure.
					logger.LogError(ctx, identityErr.Error())
					continue
				}
				batchesByKey[batchKey] = append(batchesByKey[batchKey], task)
			}
			batchKeys := make([]midjourneyTaskPollingBatchKey, 0, len(batchesByKey))
			for batchKey := range batchesByKey {
				batchKeys = append(batchKeys, batchKey)
			}
			sort.Slice(batchKeys, func(i, j int) bool { return batchKeys[i].sortKey() < batchKeys[j].sortKey() })
			for _, batchKey := range batchKeys {
				batchTasks := batchesByKey[batchKey]
				grouped := groupMidjourneyPollingTasks(batchTasks)
				for mjID, duplicates := range grouped.ambiguous {
					localIDs := make([]string, 0, len(duplicates))
					for _, task := range duplicates {
						localIDs = append(localIDs, fmt.Sprintf("%d", task.Id))
					}
					logger.LogError(ctx, fmt.Sprintf(
						"Midjourney polling skipped ambiguous duplicate upstream task ID %q; local task IDs=%s",
						mjID,
						strings.Join(localIDs, ","),
					))
				}
				if len(grouped.taskIDs) == 0 {
					continue
				}
				_, _, fetchErr := fetchMidjourneyTaskBatchWithClaimsAndApply(
					ctx,
					midjourneyChannel,
					batchKey.credential,
					grouped.taskIDs,
					grouped.unique,
					applyMidjourneyTaskPollingResponse,
				)
				if fetchErr != nil {
					logger.LogError(ctx, fetchErr.Error())
					continue
				}
			}
		}
	}
}

type midjourneyTaskPollingBatchKey struct {
	credential              string
	baseURL                 string
	group                   string
	model                   string
	effective               string
	operation               string
	path                    string
	fingerprint             string
	snapshotFingerprint     string
	profile                 string
	proxyFingerprint        string
	headerPolicyFingerprint string
}

type midjourneyPollingTaskGroup struct {
	taskIDs   []string
	unique    map[string]*model.Midjourney
	ambiguous map[string][]*model.Midjourney
}

// groupMidjourneyPollingTasks keeps one local task per upstream ID. Duplicate
// IDs are unsafe to reconcile because one provider response cannot identify
// which local row owns it; those rows are deliberately excluded from the
// request and left pending for explicit operator reconciliation.
func groupMidjourneyPollingTasks(tasks []*model.Midjourney) midjourneyPollingTaskGroup {
	byID := make(map[string][]*model.Midjourney)
	for _, task := range tasks {
		if task == nil {
			continue
		}
		mjID := strings.TrimSpace(task.MjId)
		if mjID == "" {
			continue
		}
		byID[mjID] = append(byID[mjID], task)
	}

	grouped := midjourneyPollingTaskGroup{
		taskIDs:   make([]string, 0, len(byID)),
		unique:    make(map[string]*model.Midjourney),
		ambiguous: make(map[string][]*model.Midjourney),
	}
	for mjID, matchingTasks := range byID {
		if len(matchingTasks) != 1 {
			grouped.ambiguous[mjID] = matchingTasks
			continue
		}
		grouped.taskIDs = append(grouped.taskIDs, mjID)
		grouped.unique[mjID] = matchingTasks[0]
	}
	sort.Strings(grouped.taskIDs)
	return grouped
}

// claimedMidjourneyPollingTasks narrows response reconciliation to the task
// leases actually acquired for this request. The source task map can contain
// rows skipped because another worker owns their lease; those rows must never
// be updated from a response returned for this worker's batch.
func claimedMidjourneyPollingTasks(claimedTaskIDs []string, tasksByID map[string]*model.Midjourney) map[string]*model.Midjourney {
	claimed := make(map[string]*model.Midjourney, len(claimedTaskIDs))
	for _, taskID := range claimedTaskIDs {
		taskID = strings.TrimSpace(taskID)
		if taskID == "" {
			continue
		}
		if task := tasksByID[taskID]; task != nil {
			claimed[taskID] = task
		}
	}
	return claimed
}

func (key midjourneyTaskPollingBatchKey) sortKey() string {
	return strings.Join([]string{key.group, key.model, key.effective, key.baseURL, key.operation, key.path, key.fingerprint, key.snapshotFingerprint, key.profile, key.proxyFingerprint, key.headerPolicyFingerprint, key.credential}, "\x00")
}

func midjourneyTaskPollingBatchIdentity(channel *model.Channel, task *model.Midjourney, credential string) (midjourneyTaskPollingBatchKey, error) {
	if channel == nil || task == nil {
		return midjourneyTaskPollingBatchKey{}, fmt.Errorf("midjourney polling batch route is incomplete")
	}
	baseURL, route, profile, err := midjourneyTaskPollingRoute(channel, task)
	if err != nil {
		return midjourneyTaskPollingBatchKey{}, err
	}
	group := midjourneyPollingGroup(task, channel)
	proxyFingerprint := ""
	headerPolicyFingerprint := ""
	snapshotFingerprint := "legacy:none"
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil {
		if snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion {
			snapshotFingerprint = strings.TrimSpace(snapshot.SnapshotFingerprint)
			if snapshotFingerprint == "" {
				return midjourneyTaskPollingBatchKey{}, fmt.Errorf("midjourney task route snapshot fingerprint is missing")
			}
		} else {
			snapshotFingerprint = fmt.Sprintf("legacy:schema-%d", snapshot.SchemaVersion)
		}
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotHeaderPolicySchemaVersion {
		// Schema-5 snapshots already bind the static Header Policy, and snapshots
		// that captured a proxy bind that transport identity as well. A batch may
		// only share a representative request when every captured identity is
		// equal; otherwise the first task's transport settings would leak into the
		// other tasks. Older schema-5 rows without ProxyCaptured intentionally keep
		// the compatibility path, where all tasks use the current channel proxy.
		proxyFingerprint = strings.TrimSpace(snapshot.ProxyFingerprint)
		headerPolicyFingerprint = strings.TrimSpace(snapshot.HeaderPolicyFingerprint)
	}
	return midjourneyTaskPollingBatchKey{
		credential:              credential,
		baseURL:                 baseURL,
		group:                   group,
		model:                   route.RequestedModel,
		effective:               route.EffectiveUpstreamModel,
		operation:               route.Operation,
		path:                    route.UpstreamPath,
		fingerprint:             route.Fingerprint,
		snapshotFingerprint:     snapshotFingerprint,
		profile:                 profile,
		proxyFingerprint:        proxyFingerprint,
		headerPolicyFingerprint: headerPolicyFingerprint,
	}, nil
}

func midjourneyTaskPollingRoute(channel *model.Channel, task *model.Midjourney) (string, relaycommon.ChannelRouteIdentity, string, error) {
	if channel == nil || task == nil {
		return "", relaycommon.ChannelRouteIdentity{}, "", fmt.Errorf("midjourney polling route is incomplete")
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && snapshot.SchemaVersion >= 1 {
		baseURL, err := service.TaskRouteSnapshotBaseURL(snapshot, channel)
		if err != nil {
			return "", relaycommon.ChannelRouteIdentity{}, "", fmt.Errorf("resolve Midjourney task base URL: %w", err)
		}
		route, err := service.TaskRouteIdentityFromSnapshot(snapshot, "poll", channel)
		if err != nil {
			return "", relaycommon.ChannelRouteIdentity{}, "", fmt.Errorf("resolve Midjourney task route: %w", err)
		}
		return baseURL, route, service.TaskRouteSnapshotHeaderProfileRef(snapshot), nil
	}
	modelName := service.CovertMjpActionToModelName(task.Action)
	path := service.TaskPollingRoutePath(channel.Type, task.Action)
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         modelName,
		EffectiveUpstreamModel: modelName,
		FinalProtocol:          "midjourney",
		Operation:              service.TaskRouteOperation(channel.Type, constant.TaskPlatformMidjourney, task.Action) + ".poll",
		UpstreamPath:           path,
		ChannelID:              channel.Id,
	}.Normalized()
	baseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	if baseURL == "" {
		return "", relaycommon.ChannelRouteIdentity{}, "", fmt.Errorf("Midjourney channel base URL is empty: channel_id=%d", channel.Id)
	}
	return baseURL, route, "", nil
}

func resolveMidjourneyPollingKey(channel *model.Channel, task *model.Midjourney) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("midjourney polling channel or task is nil")
	}
	storedKey := strings.TrimSpace(task.PrivateData.Key)
	if storedKey != "" {
		key, err := service.ResolveTaskCredential(channel, task.PrivateData.RouteSnapshot, storedKey)
		if err != nil {
			return "", fmt.Errorf("midjourney task %s: %w", task.MjId, err)
		}
		return key, nil
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion {
		// Current-schema snapshots are authoritative for the concrete credential.
		// Falling back to the channel's current single key after the persisted key
		// is missing would silently cross a credential rotation boundary.
		return "", fmt.Errorf("midjourney task %s has no persisted credential for current route snapshot", task.MjId)
	}
	if channel.ChannelInfo.IsMultiKey {
		return "", fmt.Errorf("midjourney task %s has no persisted credential for multi-key channel %d", task.MjId, channel.Id)
	}
	key := strings.TrimSpace(channel.Key)
	if key == "" {
		return "", fmt.Errorf("midjourney channel %d has no credential", channel.Id)
	}
	return key, nil
}

func sweepTimedOutMidjourneyTasks(ctx context.Context, now time.Time) {
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-midjourneyTaskTimeout).UnixNano() / int64(time.Millisecond)
	tasks, queryErr := model.GetTimedOutUnfinishedMidjourneyTasksWithError(cutoff, constant.TaskQueryLimit)
	if queryErr != nil {
		logger.LogError(ctx, fmt.Sprintf("Midjourney timeout query/row error; continuing with valid tasks: %v", queryErr))
	}
	for _, task := range tasks {
		if won, err := markMidjourneyTaskPollingFailure(task, midjourneyTaskTimeoutReason()); err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweep Midjourney task timeout failed: %v", err))
		} else if won {
			logger.LogInfo(ctx, fmt.Sprintf("sweep Midjourney task timeout success: %d", task.Id))
		}
	}
}

func isMidjourneyTaskTimedOut(task *model.Midjourney, now time.Time) bool {
	if task == nil || task.SubmitTime <= 0 || strings.TrimSpace(task.Progress) == "100%" {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	return now.UnixNano()/int64(time.Millisecond)-task.SubmitTime > midjourneyTaskTimeout.Milliseconds()
}

func markMidjourneyTaskPollingFailure(task *model.Midjourney, reason string) (bool, error) {
	if task == nil {
		return false, errors.New("midjourney task is nil")
	}
	if task.Status == "FAILURE" || task.Status == "SUCCESS" {
		return false, nil
	}
	previousStatus := task.Status
	candidate := *task
	candidate.Status = "FAILURE"
	candidate.Progress = "100%"
	if strings.TrimSpace(reason) == "" {
		reason = "Midjourney task polling failed locally"
	}
	candidate.FailReason = reason
	if candidate.FinishTime == 0 {
		candidate.FinishTime = time.Now().UnixNano() / int64(time.Millisecond)
	}
	won, err := candidate.UpdateWithStatus(previousStatus)
	if err != nil {
		return false, err
	}
	if !won {
		common.SysLog(fmt.Sprintf("Midjourney task %s transitioned while marking polling failure", task.MjId))
		return false, nil
	}
	*task = candidate
	service.RefundMidjourneyTaskQuota(context.Background(), &candidate, reason)
	return true, nil
}

func fetchMidjourneyTaskBatchForTask(ctx context.Context, channel *model.Channel, task *model.Midjourney, key string, taskIDs []string) ([]dto.MidjourneyDto, error) {
	if channel == nil {
		return nil, fmt.Errorf("midjourney polling channel is required")
	}
	if task == nil {
		return nil, fmt.Errorf("midjourney polling task is required")
	}
	if len(taskIDs) == 0 {
		return nil, fmt.Errorf("midjourney polling task IDs are empty")
	}
	baseURL, route, profileRef, err := midjourneyTaskPollingRoute(channel, task)
	if err != nil {
		return nil, err
	}
	routeSnapshot := task.PrivateData.RouteSnapshot
	body, err := common.Marshal(map[string]any{"ids": taskIDs})
	if err != nil {
		return nil, fmt.Errorf("marshal midjourney task ids: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	proxy, err := service.TaskRouteSnapshotProxy(task.PrivateData.RouteSnapshot, channel)
	if err != nil {
		return nil, fmt.Errorf("resolve midjourney polling proxy: %w", err)
	}
	var attempt *service.ChannelHealthAttempt
	if routeSnapshot != nil && routeSnapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion {
		providerRuntime, runtimeErr := service.TaskRouteSnapshotProviderRuntime(routeSnapshot)
		if runtimeErr != nil {
			return nil, fmt.Errorf("resolve midjourney polling provider runtime: %w", runtimeErr)
		}
		attempt, err = service.BeginChannelHealthAttemptWithRouteAndBaseURLAndProviderRuntime(
			requestCtx,
			midjourneyPollingGroup(task, channel),
			route,
			channel,
			key,
			baseURL,
			providerRuntime,
			proxy,
		)
	} else {
		attempt, err = service.BeginChannelHealthAttemptWithRouteAndBaseURL(requestCtx, midjourneyPollingGroup(task, channel), route, channel, key, baseURL)
	}
	if err != nil {
		return nil, fmt.Errorf("begin midjourney polling health attempt: %w", err)
	}
	defer attempt.Release()
	if !attempt.Selection.Allow {
		if attempt.Selection.Err != nil {
			return nil, fmt.Errorf("midjourney polling health gate rejected: %w", attempt.Selection.Err)
		}
		return nil, fmt.Errorf("midjourney polling health gate rejected: %s", attempt.Selection.Action)
	}
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("mj-api-secret", key)
	if routeSnapshot != nil {
		headers, err = service.BuildTaskRouteSnapshotRequestHeaders(channel, key, headers, routeSnapshot)
	} else {
		headers, err = service.BuildChannelRuntimeRequestHeadersForProfile(channel, key, headers, profileRef)
	}
	if err != nil {
		return nil, fmt.Errorf("build midjourney runtime request headers: %w", err)
	}
	lease, err := service.AcquireChannelConcurrency(attempt.Context, channel)
	if err != nil {
		return nil, fmt.Errorf("acquire midjourney polling concurrency: %w", err)
	}
	if lease != nil {
		defer lease.Release()
	}
	reqContext := attempt.Context.Request.Context()
	req, err := http.NewRequestWithContext(reqContext, http.MethodPost, fmt.Sprintf("%s%s", baseURL, relaycommon.NormalizeRoutePath(route.UpstreamPath)), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create midjourney polling request: %w", err)
	}
	service.ApplyRuntimeRequestHeaders(req, headers)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("create midjourney polling proxy client: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeDoRequestFailed)
		return nil, fmt.Errorf("midjourney polling request failed: %w", err)
	}
	if resp == nil {
		attempt.ObserveFailure(errors.New("midjourney polling returned empty response"), http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		return nil, fmt.Errorf("midjourney polling returned empty response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr := service.RelayErrorHandler(attempt.Context.Request.Context(), resp, false)
		attempt.ObserveAPIError(apiErr)
		return nil, fmt.Errorf("midjourney polling returned status %d", resp.StatusCode)
	}
	if resp.Body == nil {
		err := errors.New("midjourney polling response body is nil")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		return nil, err
	}
	defer service.CloseResponseBodyGracefully(resp)
	responseBody, err := service.ReadTaskPollingResponseBody(resp.Body)
	if err != nil {
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeReadResponseBodyFailed)
		return nil, fmt.Errorf("read midjourney polling response: %w", err)
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		attempt.ObserveFailure(errors.New("midjourney polling returned empty response body"), http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		return nil, fmt.Errorf("midjourney polling returned empty response body")
	}
	var responseItems []dto.MidjourneyDto
	if err = common.Unmarshal(responseBody, &responseItems); err != nil {
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return nil, fmt.Errorf("parse midjourney polling response: %w", err)
	}
	if responseItems == nil {
		err = errors.New("midjourney polling response items are null")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return nil, err
	}
	if !hasRecognizedMidjourneyPollingStatus(responseItems, taskIDs) {
		// HTTP 2xx only proves that the proxy accepted the request. A successful
		// health probe requires a response item for one of the requested tasks
		// with a concrete lifecycle state; an empty array, an unrelated task, or
		// an unknown/empty status must not make a half-open channel healthy.
		err = errors.New("midjourney polling response contains no recognized task status")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return nil, err
	}
	attempt.MarkSuccess()
	return responseItems, nil
}

func hasRecognizedMidjourneyPollingStatus(items []dto.MidjourneyDto, taskIDs []string) bool {
	expected := make(map[string]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		if taskID = strings.TrimSpace(taskID); taskID != "" {
			expected[taskID] = struct{}{}
		}
	}
	if len(expected) == 0 {
		return false
	}
	for _, item := range items {
		mjID := strings.TrimSpace(item.MjId)
		if _, ok := expected[mjID]; !ok {
			continue
		}
		if isRecognizedMidjourneyTaskStatus(item.Status) {
			return true
		}
	}
	return false
}

func isRecognizedMidjourneyTaskStatus(status string) bool {
	_, recognized := service.NormalizeMidjourneyTaskStatus(status)
	return recognized
}

func midjourneyPollingGroup(task *model.Midjourney, channel *model.Channel) string {
	if task != nil && task.PrivateData.RouteSnapshot != nil {
		if group := strings.TrimSpace(task.PrivateData.RouteSnapshot.Group); group != "" && !strings.EqualFold(group, "auto") {
			return group
		}
	}
	if channel != nil {
		for _, group := range channel.GetGroups() {
			if group != "" && !strings.EqualFold(group, "auto") {
				return group
			}
		}
	}
	return "default"
}

func midjourneyPollingClaimTask(task *model.Midjourney) *model.Task {
	if task == nil {
		return nil
	}
	return &model.Task{
		ID:        int64(task.Id),
		ChannelId: task.ChannelId,
		UserId:    task.UserId,
		Platform:  constant.TaskPlatformMidjourney,
		TaskID:    task.MjId,
		Action:    task.Action,
	}
}

// fetchMidjourneyTaskBatchWithClaims is the compatibility wrapper used by
// callers that only need the provider response. It releases its leases when
// the request returns. The polling loop uses the apply variant below so the
// leases also cover response reconciliation and the guarded database writes.
func fetchMidjourneyTaskBatchWithClaims(ctx context.Context, channel *model.Channel, key string, taskIDs []string, tasksByID map[string]*model.Midjourney) ([]string, []dto.MidjourneyDto, error) {
	return fetchMidjourneyTaskBatchWithClaimsAndApply(ctx, channel, key, taskIDs, tasksByID, nil)
}

type midjourneyTaskPollingResponseApplier func(context.Context, []string, []dto.MidjourneyDto, map[string]*model.Midjourney) error

// fetchMidjourneyTaskBatchWithClaimsAndApply acquires a task-level lease for
// every ID included in one provider batch. A batch-level representative lease
// is not sufficient because it would not identify which local task rows are
// owned by this worker. Busy tasks are skipped; coordinator failures are
// fail-closed and do not send an unclaimed request. When apply is non-nil, the
// leases remain held until response reconciliation and its CAS updates finish.
func fetchMidjourneyTaskBatchWithClaimsAndApply(ctx context.Context, channel *model.Channel, key string, taskIDs []string, tasksByID map[string]*model.Midjourney, apply midjourneyTaskPollingResponseApplier) ([]string, []dto.MidjourneyDto, error) {
	if len(taskIDs) == 0 {
		return nil, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	batchCtx, cancelBatch := context.WithCancel(ctx)
	defer cancelBatch()
	claims := make([]*service.TaskPollingClaimLease, 0, len(taskIDs))
	claimedTaskIDs := make([]string, 0, len(taskIDs))
	releaseClaims := func() {
		for _, claim := range claims {
			claim.Release()
		}
	}
	defer releaseClaims()

	for _, taskID := range taskIDs {
		task := tasksByID[taskID]
		if task == nil {
			return nil, nil, fmt.Errorf("midjourney polling task %s is missing from unique task map", taskID)
		}
		claim, err := service.AcquireTaskPollingClaim(batchCtx, midjourneyPollingClaimTask(task))
		if errors.Is(err, service.ErrTaskPollingClaimBusy) {
			logger.LogDebug(ctx, "skip Midjourney task polling claim held by another worker: task=%s", task.MjId)
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("acquire Midjourney task polling claim for %s: %w", task.MjId, err)
		}
		claims = append(claims, claim)
		claimedTaskIDs = append(claimedTaskIDs, taskID)
		go func(done <-chan struct{}) {
			select {
			case <-done:
				cancelBatch()
			case <-batchCtx.Done():
			}
		}(claim.Done())
	}
	if len(claimedTaskIDs) == 0 {
		return nil, nil, nil
	}
	responseItems, err := fetchMidjourneyTaskBatchForTask(
		batchCtx,
		channel,
		tasksByID[claimedTaskIDs[0]],
		key,
		claimedTaskIDs,
	)
	if err != nil {
		return claimedTaskIDs, responseItems, err
	}
	if claimErr := batchCtx.Err(); claimErr != nil {
		return claimedTaskIDs, nil, fmt.Errorf("midjourney polling task claim lost before response reconciliation: %w", claimErr)
	}
	if apply != nil {
		if err := apply(batchCtx, claimedTaskIDs, responseItems, tasksByID); err != nil {
			return claimedTaskIDs, responseItems, err
		}
		if claimErr := batchCtx.Err(); claimErr != nil {
			return claimedTaskIDs, responseItems, fmt.Errorf("midjourney polling task claim lost after response reconciliation: %w", claimErr)
		}
	}
	return claimedTaskIDs, responseItems, err
}

func applyMidjourneyTaskPollingResponse(ctx context.Context, claimedTaskIDs []string, responseItems []dto.MidjourneyDto, tasksByID map[string]*model.Midjourney) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("midjourney polling task claim lost before response merge: %w", err)
	}
	if len(claimedTaskIDs) == 0 {
		return nil
	}

	seenResponseIDs := make(map[string]struct{}, len(responseItems))
	claimedTasks := claimedMidjourneyPollingTasks(claimedTaskIDs, tasksByID)
	for _, responseItem := range responseItems {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("midjourney polling task claim lost during response merge: %w", err)
		}
		responseID := strings.TrimSpace(responseItem.MjId)
		matchedTask, matched := claimedTasks[responseID]
		if !matched {
			logger.LogError(ctx, fmt.Sprintf("Midjourney upstream returned unclaimed task %s", responseItem.MjId))
			continue
		}
		if _, duplicateResponse := seenResponseIDs[responseID]; duplicateResponse {
			logger.LogWarn(ctx, fmt.Sprintf("Midjourney upstream returned duplicate task response %q; keeping first response", responseID))
			continue
		}
		seenResponseIDs[responseID] = struct{}{}
		task := matchedTask
		// Copy the response before applying local timeout normalization.
		item := responseItem

		if isMidjourneyTaskTimedOut(task, time.Now()) {
			item.FailReason = midjourneyTaskTimeoutReason()
			item.Status = "FAILURE"
		}
		incomingStatus := strings.ToUpper(strings.TrimSpace(item.Status))
		if normalizedStatus, recognized := service.NormalizeMidjourneyTaskStatus(incomingStatus); recognized {
			incomingStatus = string(normalizedStatus)
			item.Status = incomingStatus
		}
		if incomingStatus == "" {
			logger.LogWarn(ctx, fmt.Sprintf("Midjourney task %s returned empty status, merge non-empty fields only", task.MjId))
		}
		preStatus := task.Status
		if incomingStatus != "" && !service.ShouldApplyTaskStatus(model.TaskStatus(preStatus), model.TaskStatus(incomingStatus)) {
			logger.LogWarn(ctx, fmt.Sprintf(
				"Midjourney task %s ignored stale status transition: current=%s incoming=%s",
				task.MjId,
				preStatus,
				incomingStatus,
			))
			continue
		}
		mergeResult, mergeErr := service.MergeMidjourneyTask(task, item)
		if mergeErr != nil {
			logger.LogError(ctx, fmt.Sprintf("Merge Midjourney task %s failed: %v", task.MjId, mergeErr))
			continue
		}
		if !mergeResult.Changed {
			continue
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("midjourney polling task claim lost before database update: %w", err)
		}
		won, err := task.UpdateWithStatus(preStatus)
		if err != nil {
			logger.LogError(ctx, "UpdateMidjourneyTask task error: "+err.Error())
		} else if won && mergeResult.TerminalFailureTransition {
			logger.LogInfo(ctx, task.MjId+" 构建失败，"+task.FailReason)
			service.RefundMidjourneyTaskQuota(ctx, task, "构图失败")
		}
	}
	return nil
}

func checkMjTaskNeedUpdate(oldTask *model.Midjourney, newTask dto.MidjourneyDto) bool {
	if oldTask == nil {
		return false
	}
	copyOfTask := *oldTask
	result, err := service.MergeMidjourneyTask(&copyOfTask, newTask)
	return err == nil && result.Changed
}

func GetAllMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	// 解析其他查询参数
	queryParams := model.TaskQueryParams{
		ChannelID:      c.Query("channel_id"),
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items, err := model.GetAllTasksWithError(pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	if err != nil {
		common.ApiError(c, fmt.Errorf("查询 Midjourney 任务失败: %w", err))
		return
	}
	total := model.CountAllTasks(queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetUserMidjourney(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)

	userId := c.GetInt("id")

	queryParams := model.TaskQueryParams{
		MjID:           c.Query("mj_id"),
		StartTimestamp: c.Query("start_timestamp"),
		EndTimestamp:   c.Query("end_timestamp"),
	}

	items, err := model.GetAllUserTaskWithError(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), queryParams)
	if err != nil {
		common.ApiError(c, fmt.Errorf("查询 Midjourney 任务失败: %w", err))
		return
	}
	total := model.CountAllUserTask(userId, queryParams)

	if setting.MjForwardUrlEnabled {
		for i, midjourney := range items {
			midjourney.ImageUrl = system_setting.ServerAddress + "/mj/image/" + midjourney.MjId
			items[i] = midjourney
		}
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}
