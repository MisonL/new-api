package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string, extraHeaders ...http.Header) (*http.Response, error)
	FetchTaskWithContext(ctx context.Context, baseURL string, key string, body map[string]any, proxy string, extraHeaders ...http.Header) (*http.Response, error)
	ParseTaskResult(body []byte, key string, extraHeaders ...http.Header) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// maxTaskPollingResponseBodyBytes bounds every asynchronous task status
// response before it is parsed or persisted. Inline video payloads are
// separately bounded when they are stored as data URLs; this larger envelope
// limit leaves room for base64 plus provider metadata without allowing an
// untrusted upstream to allocate memory without a bound.
const maxTaskPollingResponseBodyBytes int64 = 128 << 20

// ReadTaskPollingResponseBody reads a task status response with one shared
// limit for maintenance polling and request-scoped realtime fetches. Callers
// must treat an oversized body as an explicit upstream observation failure;
// never parse or persist the partial prefix returned by LimitReader.
func ReadTaskPollingResponseBody(reader io.Reader) ([]byte, error) {
	return readTaskPollingResponseBodyWithLimit(reader, maxTaskPollingResponseBodyBytes)
}

func readTaskPollingResponseBodyWithLimit(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("task polling response body is nil")
	}
	if limit <= 0 {
		return nil, errors.New("task polling response body limit must be positive")
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("task polling response body exceeds %d bytes", limit)
	}
	return body, nil
}

// TaskResultParserWithContext is an optional extension for adaptors whose
// status response requires a second upstream request to materialize content.
// The base interface remains compatible with existing providers; implementations
// that support the extension inherit the polling request's cancellation, proxy,
// and timeout semantics for that second request as well.
type TaskResultParserWithContext interface {
	ParseTaskResultWithContext(ctx context.Context, baseURL string, key string, respBody []byte, proxy string, extraHeaders ...http.Header) (*relaycommon.TaskInfo, error)
}

// taskPollingRequestTimeout bounds one upstream task-status request, including
// the body read. RELAY_TIMEOUT defaults to 0, which leaves the shared HTTP
// client without any timeout, and the polling context carries only a cancel,
// no deadline: a provider that accepts the connection and then stalls would
// park the polling loop on that single request and stop advancing every other
// task behind it.
var taskPollingRequestTimeout = 60 * time.Second

// taskPollingResponseBody releases the per-request timeout when the caller is
// done with the response body, so a successful poll does not keep a timer
// armed for the rest of the timeout window.
type taskPollingResponseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *taskPollingResponseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// FetchTaskWithContext routes task polling through the context-aware adaptor.
// The interface requires this method so lease/probe cancellation can never
// silently fall back to an unbound HTTP request.
//
// The derived context is what actually bounds the request: adaptors build
// their HTTP requests from it, so both the round trip and a stalled body read
// end when the deadline passes.
func FetchTaskWithContext(ctx context.Context, adaptor TaskPollingAdaptor, baseURL string, key string, body map[string]any, proxy string, extraHeaders ...http.Header) (*http.Response, error) {
	if adaptor == nil {
		return nil, errors.New("task polling adaptor is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := taskPollingRequestTimeout
	if timeout <= 0 {
		return adaptor.FetchTaskWithContext(ctx, baseURL, key, body, proxy, extraHeaders...)
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	resp, err := adaptor.FetchTaskWithContext(requestCtx, baseURL, key, body, proxy, extraHeaders...)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp == nil || resp.Body == nil {
		// Nothing left to read; the deadline would only linger unused.
		cancel()
		return resp, nil
	}
	resp.Body = &taskPollingResponseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type sunoTaskPollingBatchKey struct {
	credential              string
	model                   string
	effective               string
	group                   string
	baseURL                 string
	proxy                   string
	operation               string
	path                    string
	fingerprint             string
	snapshotFingerprint     string
	profile                 string
	headerPolicyFingerprint string
}

type sunoTaskPollingBatch struct {
	key     sunoTaskPollingBatchKey
	taskIDs []string
}

// runSunoTaskPollingBatches isolates failures to one concrete credential/model
// batch. A failed upstream request must not prevent other valid batches for the
// same channel from being polled in this cycle.
func runSunoTaskPollingBatches(ctx context.Context, channelID int, batches []sunoTaskPollingBatch, poll func(sunoTaskPollingBatch) error) error {
	if poll == nil {
		return errors.New("suno task polling callback is nil")
	}
	var batchErrors []error
	for _, batch := range batches {
		if err := poll(batch); err != nil {
			safeMessage := redactTaskPollingErrorMessage(err.Error(), batch.key.credential)
			batchErr := fmt.Errorf(
				"Suno polling batch failed: channel=%d group=%q model=%q effective_model=%q task_count=%d: %s",
				channelID,
				batch.key.group,
				batch.key.model,
				batch.key.effective,
				len(batch.taskIDs),
				safeMessage,
			)
			logger.LogError(ctx, batchErr.Error())
			batchErrors = append(batchErrors, batchErr)
		}
	}
	return errors.Join(batchErrors...)
}

func redactTaskPollingErrorMessage(message string, credentials ...string) string {
	for _, credential := range credentials {
		credential = strings.TrimSpace(credential)
		if credential != "" {
			message = strings.ReplaceAll(message, credential, "[redacted]")
		}
	}
	return message
}

// formatTaskPollingFailureLog returns a bounded, credential-free task failure
// summary. Task.PrivateData contains the decrypted polling credential while a
// row is in memory, so serializing the whole Task here would disclose secrets
// in debug logs even though PrivateData is excluded from the public JSON DTO.
func formatTaskPollingFailureLog(task *model.Task) string {
	if task == nil {
		return "task polling failure: task=nil"
	}
	reason := redactTaskPollingErrorMessage(task.FailReason, task.PrivateData.Key)
	return fmt.Sprintf(
		"task polling failure: task_id=%s channel_id=%d platform=%q action=%q status=%q progress=%q reason=%q",
		task.TaskID,
		task.ChannelId,
		task.Platform,
		task.Action,
		task.Status,
		task.Progress,
		reason,
	)
}

// formatTaskResultDebugLog returns a bounded, credential-free summary of a
// normalized provider result. TaskInfo may contain signed URLs in Url or
// RemoteUrl and provider-controlled error text in Reason; logging the struct
// with %%+v would therefore disclose credentials or reusable content links.
func formatTaskResultDebugLog(taskResult *relaycommon.TaskInfo, credentials ...string) string {
	if taskResult == nil {
		return "task polling result: result=nil"
	}
	reason := redactTaskPollingErrorMessage(taskResult.Reason, credentials...)
	return fmt.Sprintf(
		"task polling result: task_id=%s status=%q progress=%q has_url=%t completion_tokens=%d total_tokens=%d reason=%q",
		taskResult.TaskID,
		taskResult.Status,
		taskResult.Progress,
		taskResult.ResolvedURL() != "",
		taskResult.CompletionTokens,
		taskResult.TotalTokens,
		reason,
	)
}

const defaultTaskPollingFailureReason = "Upstream task failed without an explicit reason"

// mergeTaskPollingFailureReason prevents sparse terminal responses from
// erasing a diagnostic captured earlier in the task lifecycle. A terminal
// failure must always retain a non-empty, actionable reason for API clients and
// operators even when the provider omits its error field.
func mergeTaskPollingFailureReason(existing string, incoming string) string {
	if strings.TrimSpace(incoming) != "" {
		return incoming
	}
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	return defaultTaskPollingFailureReason
}

// resolveTaskPollingKey returns the concrete credential that was used for task
// submission. New multi-key tasks are bound to the persisted credential index;
// if the channel is reordered or that index is replaced, polling must fail
// explicitly instead of finding the same key at another index.
func resolveTaskPollingKey(ch *model.Channel, task *model.Task) (string, error) {
	if ch == nil || task == nil {
		return "", errors.New("task polling channel or task is nil")
	}
	storedKey := strings.TrimSpace(task.PrivateData.Key)
	if storedKey != "" {
		return ResolveTaskCredential(ch, task.PrivateData.RouteSnapshot, storedKey)
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion {
		// Current-schema snapshots are authoritative for the concrete credential.
		// Falling back to the channel's current single key after the persisted key
		// is missing would silently cross a credential rotation boundary.
		return "", fmt.Errorf("task %s has no persisted credential for current route snapshot", task.TaskID)
	}
	if ch.ChannelInfo.IsMultiKey {
		return "", fmt.Errorf("task %s has no persisted credential for multi-key channel %d", task.TaskID, ch.Id)
	}
	key := strings.TrimSpace(ch.Key)
	if key == "" {
		return "", fmt.Errorf("channel %d has no credential", ch.Id)
	}
	return key, nil
}

// ResolveTaskCredential applies one credential-identity policy to every
// asynchronous task provider. Current multi-key snapshots are index-scoped:
// an index mismatch is a credential rotation/reorder event, not permission to
// search for a matching key elsewhere. Rows without a credential-scoped
// snapshot use an explicit legacy compatibility path and require a unique
// content match, preventing duplicate keys from making the account ambiguous.
func ResolveTaskCredential(ch *model.Channel, snapshot *model.TaskRouteSnapshot, storedKey string) (string, error) {
	if ch == nil {
		return "", errors.New("task polling channel is nil")
	}
	storedKey = strings.TrimSpace(storedKey)
	if storedKey == "" {
		return "", errors.New("persisted task credential is empty")
	}
	if snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion {
		if err := validateTaskRouteCredentialScope(snapshot, ch); err != nil {
			return "", err
		}
	}

	keys := ch.GetKeys()
	if snapshot != nil && snapshot.CredentialScoped {
		if !ch.ChannelInfo.IsMultiKey {
			return "", fmt.Errorf("persisted task credential scope does not match channel %d", ch.Id)
		}
		keyIndex := snapshot.MultiKeyIndex
		if keyIndex < 0 || keyIndex >= len(keys) {
			return "", fmt.Errorf("persisted task credential index %d is no longer available for channel %d", keyIndex, ch.Id)
		}
		if strings.TrimSpace(keys[keyIndex]) != storedKey {
			return "", fmt.Errorf("persisted task credential index mismatch for channel %d (index %d)", ch.Id, keyIndex)
		}
		if !isTaskPollingKeyEnabled(ch, keyIndex) {
			return "", fmt.Errorf("persisted task credential is disabled for channel %d (key index %d)", ch.Id, keyIndex)
		}
		return storedKey, nil
	}

	// A current-schema multi-key snapshot must always carry the concrete index.
	// Keeping this guard here also protects providers that resolve credentials
	// before materializing the full health route.
	if snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion && ch.ChannelInfo.IsMultiKey {
		return "", fmt.Errorf("task route snapshot is missing credential scope for channel %d", ch.Id)
	}

	matchingIndex := -1
	matchCount := 0
	for index, candidate := range keys {
		if strings.TrimSpace(candidate) != storedKey {
			continue
		}
		matchingIndex = index
		matchCount++
	}
	if matchCount == 0 {
		return "", fmt.Errorf("persisted task credential is no longer available for channel %d", ch.Id)
	}
	if matchCount > 1 {
		return "", fmt.Errorf("persisted task credential is ambiguous for channel %d", ch.Id)
	}
	if !isTaskPollingKeyEnabled(ch, matchingIndex) {
		return "", fmt.Errorf("persisted task credential is disabled for channel %d (key index %d)", ch.Id, matchingIndex)
	}
	return storedKey, nil
}

func isTaskPollingKeyEnabled(ch *model.Channel, keyIndex int) bool {
	if ch == nil || !ch.ChannelInfo.IsMultiKey {
		return true
	}
	if keyIndex < 0 {
		return false
	}
	if ch.ChannelInfo.MultiKeyStatusList == nil {
		return true
	}
	status, exists := ch.ChannelInfo.MultiKeyStatusList[keyIndex]
	return !exists || status == common.ChannelStatusEnabled
}

// ResolveTaskChannelKey exposes the same credential guard to result-download
// handlers. All task-phase upstream calls must use the persisted concrete key
// and must reject ambiguous legacy multi-key tasks.
func ResolveTaskChannelKey(ch *model.Channel, task *model.Task) (string, error) {
	return resolveTaskPollingKey(ch, task)
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

// ErrTaskPollingReferenceAmbiguous indicates that a maintenance-polling
// reference matched more than one local task. Upstream task IDs are not a
// global primary key, so callers must surface this condition instead of
// updating an arbitrary task.
var ErrTaskPollingReferenceAmbiguous = errors.New("task polling reference is ambiguous")

// resolveTaskPollingTask accepts every reference format used by the polling
// loop over time. New callers use task-db:<database id>; older callers may use
// task-upstream:<provider id>, a bare provider ID, or the public task ID.
// Direct map hits intentionally take precedence for compatibility. Fallback
// scans are only accepted when exactly one local task matches.
func resolveTaskPollingTask(taskRef string, taskM map[string]*model.Task) (*model.Task, error) {
	taskRef = strings.TrimSpace(taskRef)
	if taskRef == "" {
		return nil, errors.New("task polling reference is empty")
	}
	if task, ok := taskM[taskRef]; ok {
		if task == nil {
			return nil, fmt.Errorf("task polling reference %q maps to a nil task", taskRef)
		}
		return task, nil
	}

	type referenceMatcher func(*model.Task) bool
	var matcher referenceMatcher
	switch {
	case strings.HasPrefix(taskRef, "task-db:"):
		rawID := strings.TrimSpace(strings.TrimPrefix(taskRef, "task-db:"))
		databaseID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || databaseID <= 0 {
			return nil, fmt.Errorf("invalid task database reference %q", taskRef)
		}
		matcher = func(task *model.Task) bool {
			return task != nil && task.ID == databaseID
		}
	case strings.HasPrefix(taskRef, "task-upstream:"):
		upstreamID := strings.TrimSpace(strings.TrimPrefix(taskRef, "task-upstream:"))
		if upstreamID == "" {
			return nil, fmt.Errorf("invalid task upstream reference %q", taskRef)
		}
		matcher = func(task *model.Task) bool {
			return task != nil && strings.TrimSpace(task.GetUpstreamTaskID()) == upstreamID
		}
	default:
		matcher = func(task *model.Task) bool {
			if task == nil {
				return false
			}
			return strings.TrimSpace(task.TaskID) == taskRef ||
				strings.TrimSpace(task.GetUpstreamTaskID()) == taskRef
		}
	}

	// A task may be present under more than one compatibility key. Deduplicate
	// the same database row while preserving ambiguity between distinct rows.
	candidates := make(map[string]*model.Task)
	for _, task := range taskM {
		if !matcher(task) {
			continue
		}
		identity := "ptr:"
		if task.ID > 0 {
			identity = fmt.Sprintf("db:%d", task.ID)
		} else {
			identity = fmt.Sprintf("ptr:%p", task)
		}
		candidates[identity] = task
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("task %s not found in taskM", taskRef)
	}
	if len(candidates) > 1 {
		return nil, fmt.Errorf("%w: %q matches %d local tasks", ErrTaskPollingReferenceAmbiguous, taskRef, len(candidates))
	}
	for _, task := range candidates {
		return task, nil
	}
	return nil, fmt.Errorf("task %s not found in taskM", taskRef)
}

type taskPollingPlatformError struct {
	task *model.Task
	err  error
}

// taskPollingPlatform resolves the adapter registry key for one task. A
// current-schema snapshot is authoritative; the task row platform is used only
// for legacy rows without a strict adapter identity.
func taskPollingPlatform(task *model.Task) (constant.TaskPlatform, error) {
	if task == nil {
		return "", errors.New("task is nil")
	}
	platform, err := TaskRouteSnapshotAdapterPlatform(task.PrivateData.RouteSnapshot, task.Platform)
	if err != nil {
		return "", err
	}
	platform = constant.TaskPlatform(strings.TrimSpace(string(platform)))
	if platform == "" {
		return "", errors.New("task polling adapter platform is missing")
	}
	return platform, nil
}

// taskPollingPlatformForVideoTask resolves the adaptor registry key for a
// video task submitted through the compatibility entry point. A current route
// snapshot is authoritative; legacy rows retain the task-row platform and,
// only when that is absent, the outer dispatch platform.
func taskPollingPlatformForVideoTask(task *model.Task, fallback constant.TaskPlatform) (constant.TaskPlatform, error) {
	if task == nil {
		return "", errors.New("task is nil")
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion {
		return TaskRouteSnapshotAdapterPlatform(snapshot, fallback)
	}
	if platform := constant.TaskPlatform(strings.TrimSpace(string(task.Platform))); platform != "" {
		return platform, nil
	}
	if platform := constant.TaskPlatform(strings.TrimSpace(string(fallback))); platform != "" {
		return platform, nil
	}
	return "", errors.New("task polling adapter platform is missing")
}

type videoTaskPollingGroupKey struct {
	channelID int
	platform  constant.TaskPlatform
}

// groupVideoTaskRefsByAdapter prevents a direct or mixed-platform polling
// invocation from passing a task to the adaptor selected by the outer batch.
// The outer platform is only a legacy fallback; current-schema snapshots are
// resolved one task at a time and invalid rows are reported without being
// sent through an unrelated provider adaptor.
func groupVideoTaskRefsByAdapter(
	fallback constant.TaskPlatform,
	taskChannelM map[int][]string,
	taskM map[string]*model.Task,
) (map[videoTaskPollingGroupKey][]string, []error) {
	grouped := make(map[videoTaskPollingGroupKey][]string)
	var groupErrors []error
	for channelID, taskRefs := range taskChannelM {
		for _, taskRef := range taskRefs {
			task, err := resolveTaskPollingTask(taskRef, taskM)
			if err != nil {
				groupErrors = append(groupErrors, fmt.Errorf("channel %d task %q cannot be grouped for video polling: %w", channelID, taskRef, err))
				continue
			}
			if task.ChannelId > 0 && task.ChannelId != channelID {
				groupErrors = append(groupErrors, fmt.Errorf("channel %d task %q belongs to channel %d", channelID, taskRef, task.ChannelId))
				continue
			}
			platform, err := taskPollingPlatformForVideoTask(task, fallback)
			if err != nil {
				groupErrors = append(groupErrors, fmt.Errorf("channel %d task %q has invalid video adaptor identity: %w", channelID, taskRef, err))
				continue
			}
			key := videoTaskPollingGroupKey{channelID: channelID, platform: platform}
			grouped[key] = append(grouped[key], taskRef)
		}
	}
	return grouped, groupErrors
}

func groupTasksForPolling(tasks []*model.Task) (map[constant.TaskPlatform][]*model.Task, []taskPollingPlatformError) {
	grouped := make(map[constant.TaskPlatform][]*model.Task)
	invalid := make([]taskPollingPlatformError, 0)
	for _, task := range tasks {
		platform, err := taskPollingPlatform(task)
		if err != nil {
			invalid = append(invalid, taskPollingPlatformError{task: task, err: err})
			continue
		}
		grouped[platform] = append(grouped[platform], task)
	}
	return grouped, invalid
}

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
// legacyTaskCutoff 之前的任务是本功能上线（2026-02-22）前旧系统遗留的行，
// 超时后只置失败、不自动退款，交由管理员处理。
const legacyTaskCutoff int64 = 1771718400 // 2026-02-22 00:00:00 UTC

func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks, err := model.GetTimedOutUnfinishedTasksWithError(cutoff, 100)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks query/row error; continuing with valid tasks: %v", err))
	}
	if len(tasks) == 0 {
		return
	}

	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		snap := task.Snapshot()
		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
		} else {
			task.FailReason = reason
		}

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.Quota != 0 {
			if refundErr := RefundTaskQuota(ctx, task, reason); refundErr != nil {
				RevertTaskTerminalTransition(ctx, task, model.TaskStatusFailure, snap)
				continue
			}
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// TaskPollingLoop 主轮询循环，每 15 秒检查一次未完成的任务。
//
// 未完成任务可能多于单轮窗口（TASK_QUERY_LIMIT）。固定取"id 最小的 N 条"
// 时，一批卡在非终态的行会长期占满窗口，id 更大的任务直到那批超时都轮不
// 到。这里维护一个进程内游标：每轮从上一轮最后一条之后继续，取不满窗口就
// 回到开头，让所有未完成任务在若干轮内都被看到。
func TaskPollingLoop() {
	common.SleepBeforeMaintenanceLoop(common.TaskPollingInitialDelay)
	var cursor int64
	for {
		time.Sleep(common.TaskPollingInterval)
		common.SysLog("任务进度轮询开始")
		ctx := context.TODO()
		sweepTimedOutTasks(ctx)
		allTasks, err := model.GetUnfinishSyncTasksAfterIDWithError(cursor, constant.TaskQueryLimit)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("TaskPollingLoop query/row error; continuing with valid tasks: %v", err))
		}
		if len(allTasks) < constant.TaskQueryLimit {
			// 本轮取不满窗口，说明已经走到队尾；下一轮从头开始。
			cursor = 0
		} else {
			cursor = allTasks[len(allTasks)-1].ID
		}
		if len(allTasks) == 0 {
			continue
		}
		platformTask, invalidTasks := groupTasksForPolling(allTasks)
		for _, invalid := range invalidTasks {
			if invalid.task == nil {
				logger.LogError(ctx, fmt.Sprintf("invalid task route snapshot: %v", invalid.err))
				continue
			}
			reason := fmt.Sprintf("任务路由快照无效: %v", invalid.err)
			if err := markTaskPollingFailure(ctx, invalid.task, reason); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix invalid task route snapshot for %s: %v", invalid.task.TaskID, err))
			}
		}
		for platform, tasks := range platformTask {
			if len(tasks) == 0 {
				continue
			}
			taskChannelM := make(map[int][]string)
			// Keep task lookup scoped to a channel. Upstream task IDs are only
			// unique within a provider credential and may collide even within one
			// channel, so use the local database task ID as the polling reference.
			taskMByChannel := make(map[int]map[string]*model.Task)
			nullTasks := make([]*model.Task, 0)
			for _, task := range tasks {
				upstreamID := task.GetUpstreamTaskID()
				if upstreamID == "" {
					nullTasks = append(nullTasks, task)
					continue
				}
				if taskMByChannel[task.ChannelId] == nil {
					taskMByChannel[task.ChannelId] = make(map[string]*model.Task)
				}
				ref := taskPollingRef(task)
				taskMByChannel[task.ChannelId][ref] = task
				taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], ref)
			}
			for _, task := range nullTasks {
				if err := markTaskPollingFailure(ctx, task, "任务缺少上游任务 ID"); err != nil {
					logger.LogError(ctx, fmt.Sprintf("Fix null task_id task error for %s: %v", task.TaskID, err))
					continue
				}
				logger.LogInfo(ctx, fmt.Sprintf("Fix null task_id task success: %s", task.TaskID))
			}
			if len(taskChannelM) == 0 {
				continue
			}

			for channelID, channelTaskIDs := range taskChannelM {
				channelTaskM := map[int][]string{channelID: channelTaskIDs}
				DispatchPlatformUpdateWithContext(ctx, platform, channelTaskM, taskMByChannel[channelID])
			}
		}
		common.SysLog("任务进度轮询完成")
	}
}

func taskPollingRef(task *model.Task) string {
	if task == nil {
		return ""
	}
	if task.ID > 0 {
		return fmt.Sprintf("task-db:%d", task.ID)
	}
	return "task-upstream:" + task.GetUpstreamTaskID()
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	DispatchPlatformUpdateWithContext(context.Background(), platform, taskChannelM, taskM)
}

// DispatchPlatformUpdateWithContext keeps maintenance polling bound to the
// caller's cancellation context. The legacy wrapper above remains available
// for callers that do not have a context.
func DispatchPlatformUpdateWithContext(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		if err := UpdateSunoTasks(ctx, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateSunoTasks fail: %s", err))
		}
	default:
		if err := UpdateVideoTasks(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	var channelErrors []error
	for channelId, taskIds := range taskChannelM {
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
			channelErrors = append(channelErrors, err)
		}
	}
	return errors.Join(channelErrors...)
}

func isTaskTerminal(status model.TaskStatus) bool {
	return model.IsTaskTerminalStatus(status)
}

// ShouldApplyTaskStatus is the shared guard used by task pollers and webhook
// handlers. It keeps the lifecycle monotonic before any CAS write, so a stale
// upstream response cannot win a SUCCESS<->FAILURE reversal.
func ShouldApplyTaskStatus(current model.TaskStatus, incoming model.TaskStatus) bool {
	return model.IsTaskStatusTransitionAllowed(current, incoming)
}

// MergeTaskProgress applies a provider progress update without allowing a
// stale response to move the task backwards. Non-terminal tasks are capped at
// 99% so the persistence query cannot mistake them for completed work; terminal
// states always use the canonical 100% value.
func MergeTaskProgress(current, incoming string, terminal bool) string {
	current = strings.TrimSpace(current)
	if terminal {
		return taskcommon.ProgressComplete
	}
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return current
	}
	incomingValue, ok := parseTaskProgress(incoming)
	if !ok {
		return current
	}
	if incomingValue >= 100 {
		incomingValue = 99
	}
	currentValue, currentOK := parseTaskProgress(current)
	if currentOK && incomingValue < currentValue {
		return current
	}
	return fmt.Sprintf("%d%%", incomingValue)
}

func parseTaskProgress(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasSuffix(value, "%") {
		return 0, false
	}
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if value == "" {
		return 0, false
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > 100 {
		return 0, false
	}
	return n, true
}

// markTaskPollingFailure moves one locally unrecoverable task to a terminal
// state. The conditional update is deliberately per-task: a concurrent poller
// may have completed the task while this error was being classified.
func markTaskPollingFailure(ctx context.Context, task *model.Task, reason string) error {
	if task == nil {
		return errors.New("task is nil")
	}
	if isTaskTerminal(task.Status) {
		return nil
	}
	snap := task.Snapshot()
	oldStatus := task.Status
	task.Status = model.TaskStatusFailure
	task.Progress = "100%"
	if task.FinishTime == 0 {
		task.FinishTime = time.Now().Unix()
	}
	if strings.TrimSpace(reason) == "" {
		reason = "Task polling failed locally"
	}
	task.FailReason = reason
	won, err := task.UpdateWithStatus(oldStatus)
	if err != nil {
		return err
	}
	if !won {
		logger.LogInfo(ctx, fmt.Sprintf("Task %s already transitioned, skip local failure", task.TaskID))
		return nil
	}
	if task.Quota != 0 {
		if refundErr := RefundTaskQuota(ctx, task, reason); refundErr != nil {
			RevertTaskTerminalTransition(ctx, task, model.TaskStatusFailure, snap)
			return fmt.Errorf("refund task %s: %w", task.TaskID, refundErr)
		}
	}
	return nil
}

// markSunoTaskPollingFailure is kept as a compatibility wrapper for Suno's
// existing polling paths; all task platforms share the same CAS and refund
// semantics for locally unrecoverable failures.
func markSunoTaskPollingFailure(ctx context.Context, task *model.Task, reason string) error {
	return markTaskPollingFailure(ctx, task, reason)
}

func normalizeSunoTaskStatus(status string) model.TaskStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "submitted":
		return model.TaskStatusSubmitted
	case "queued", "queueing":
		return model.TaskStatusQueued
	case "in_progress", "in-progress", "processing":
		return model.TaskStatusInProgress
	case "success", "succeeded", "completed":
		return model.TaskStatusSuccess
	case "failure", "failed", "error":
		return model.TaskStatusFailure
	default:
		return ""
	}
}

func hasRecognizedSunoPollingStatus(items []dto.SunoDataResponse, expectedIDs map[string]struct{}) bool {
	if len(items) == 0 || len(expectedIDs) == 0 {
		return false
	}
	for _, item := range items {
		if _, ok := expectedIDs[strings.TrimSpace(item.TaskID)]; !ok {
			continue
		}
		if normalizeSunoTaskStatus(item.Status) != "" {
			return true
		}
	}
	return false
}

func applySunoTaskResponse(ctx context.Context, task *model.Task, responseItem dto.SunoDataResponse) error {
	if task == nil {
		return errors.New("suno task is nil")
	}
	status := normalizeSunoTaskStatus(responseItem.Status)
	if status == "" {
		return fmt.Errorf("unknown Suno task status %q", responseItem.Status)
	}
	responseItem.Status = string(status)
	if strings.TrimSpace(responseItem.FailReason) != "" && status != model.TaskStatusSuccess {
		status = model.TaskStatusFailure
		responseItem.Status = string(status)
	}
	if !ShouldApplyTaskStatus(task.Status, status) {
		logger.LogWarn(ctx, fmt.Sprintf(
			"Suno task %s ignored stale status transition: current=%s incoming=%s",
			task.TaskID,
			task.Status,
			status,
		))
		return nil
	}
	if !taskNeedsUpdate(task, responseItem) {
		return nil
	}

	snap := task.Snapshot()
	oldStatus := task.Status
	task.Status = status
	task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
	task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
	task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
	task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
	if isTaskTerminal(status) {
		task.Progress = "100%"
		if task.FinishTime == 0 {
			task.FinishTime = time.Now().Unix()
		}
		if status == model.TaskStatusFailure && strings.TrimSpace(task.FailReason) == "" {
			task.FailReason = "Suno upstream task failed"
		}
	}
	// Providers sometimes omit data while the task is still progressing. An
	// absent/null data field is not an instruction to erase a previously saved
	// result (especially for a late duplicate poll), so only replace it when a
	// non-empty payload was actually returned.
	if len(bytes.TrimSpace(responseItem.Data)) > 0 && !bytes.Equal(bytes.TrimSpace(responseItem.Data), []byte("null")) {
		task.Data = responseItem.Data
	}

	won, err := task.UpdateWithStatus(oldStatus)
	if err != nil {
		return err
	}
	if !won {
		logger.LogInfo(ctx, fmt.Sprintf("Suno task %s already transitioned, skip update and billing", task.TaskID))
		return nil
	}
	if status == model.TaskStatusFailure && oldStatus != model.TaskStatusFailure && task.Quota != 0 {
		logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
		if refundErr := RefundTaskQuota(ctx, task, task.FailReason); refundErr != nil {
			RevertTaskTerminalTransition(ctx, task, model.TaskStatusFailure, snap)
			return fmt.Errorf("refund suno task %s: %w", task.TaskID, refundErr)
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		// Channel cache/Redis failures are transient. Keep tasks pending so the
		// next maintenance cycle can retry; only timeout CAS may terminally fail
		// and refund an unfinished task.
		common.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	if GetTaskAdaptorFunc == nil {
		return errors.New("Suno task polling adaptor factory is unavailable")
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		var taskErrors []error
		for _, taskRef := range taskIds {
			t, resolveErr := resolveTaskPollingTask(taskRef, taskM)
			if resolveErr != nil {
				taskErrors = append(taskErrors, resolveErr)
				continue
			}
			if failureErr := markSunoTaskPollingFailure(ctx, t, "Suno task polling adaptor is unavailable"); failureErr != nil {
				taskErrors = append(taskErrors, fmt.Errorf("mark Suno task %s failed: %w", taskRef, failureErr))
			}
		}
		return errors.Join(append(taskErrors, errors.New("adaptor not found"))...)
	}
	// A channel can contain multiple concrete credentials and model routes.
	// Never batch task IDs that would be observed under different route identities
	// into one authenticated upstream call.
	idsByKey := make(map[sunoTaskPollingBatchKey][]string)
	var batchErrors []error
	for _, taskRef := range taskIds {
		task, resolveErr := resolveTaskPollingTask(taskRef, taskM)
		if resolveErr != nil {
			batchErrors = append(batchErrors, fmt.Errorf("resolve task %s for channel %d: %w", taskRef, channelId, resolveErr))
			continue
		}
		key, keyErr := resolveTaskPollingKey(ch, task)
		if keyErr != nil {
			// Credential cache misses and disabled multi-key entries can be
			// transient while channel configuration is being refreshed. Keep the
			// task pending so the next maintenance cycle can retry; timeout
			// sweeping remains the terminal failure/退款 path.
			batchErrors = append(batchErrors, fmt.Errorf("resolve Suno polling credential for task %s: %s", taskRef, redactTaskPollingErrorMessage(keyErr.Error(), task.PrivateData.Key)))
			continue
		}
		modelName := ResolveTaskOriginModelName(task)
		if modelName == "" {
			if failureErr := markSunoTaskPollingFailure(ctx, task, "Suno task has no persisted model name"); failureErr != nil {
				batchErrors = append(batchErrors, fmt.Errorf("mark Suno task %s failed: %w", taskRef, failureErr))
			} else {
				batchErrors = append(batchErrors, fmt.Errorf("task %s has no persisted model name", taskRef))
			}
			continue
		}
		baseURL, operation, path, fingerprint, snapshotFingerprint, profile, headerPolicyFingerprint, routeErr := sunoTaskRouteBatchIdentity(ch, task)
		if routeErr != nil {
			// Route snapshots and channel base URLs can be temporarily unavailable
			// while a channel is being refreshed. Keep the task pending and retry on
			// the next maintenance cycle; the timeout sweeper is the only terminal
			// path for an unresolved in-flight task.
			batchErrors = append(batchErrors, fmt.Errorf("resolve Suno polling route for task %s failed (pending): %s", taskRef, redactTaskPollingErrorMessage(routeErr.Error(), task.PrivateData.Key)))
			continue
		}
		if strings.TrimSpace(baseURL) == "" {
			batchErrors = append(batchErrors, fmt.Errorf("resolve Suno polling base URL failed for task %s (pending)", taskRef))
			continue
		}
		proxy, proxyErr := TaskRouteSnapshotProxy(task.PrivateData.RouteSnapshot, ch)
		if proxyErr != nil {
			batchErrors = append(batchErrors, fmt.Errorf("resolve Suno polling proxy for task %s failed (pending): %s", taskRef, redactTaskPollingErrorMessage(proxyErr.Error(), task.PrivateData.Key)))
			continue
		}
		batchKey := sunoTaskPollingBatchKey{
			credential:              key,
			model:                   modelName,
			effective:               ResolveTaskEffectiveModelName(task),
			group:                   ResolveTaskHealthGroup(task, ch),
			baseURL:                 baseURL,
			proxy:                   proxy,
			operation:               operation,
			path:                    path,
			fingerprint:             fingerprint,
			snapshotFingerprint:     snapshotFingerprint,
			profile:                 profile,
			headerPolicyFingerprint: headerPolicyFingerprint,
		}
		idsByKey[batchKey] = append(idsByKey[batchKey], taskRef)
	}
	keys := make([]sunoTaskPollingBatchKey, 0, len(idsByKey))
	for batchKey := range idsByKey {
		keys = append(keys, batchKey)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].group != keys[j].group {
			return keys[i].group < keys[j].group
		}
		if keys[i].model != keys[j].model {
			return keys[i].model < keys[j].model
		}
		if keys[i].effective != keys[j].effective {
			return keys[i].effective < keys[j].effective
		}
		if keys[i].baseURL != keys[j].baseURL {
			return keys[i].baseURL < keys[j].baseURL
		}
		if keys[i].proxy != keys[j].proxy {
			return keys[i].proxy < keys[j].proxy
		}
		if keys[i].operation != keys[j].operation {
			return keys[i].operation < keys[j].operation
		}
		if keys[i].path != keys[j].path {
			return keys[i].path < keys[j].path
		}
		if keys[i].fingerprint != keys[j].fingerprint {
			return keys[i].fingerprint < keys[j].fingerprint
		}
		if keys[i].snapshotFingerprint != keys[j].snapshotFingerprint {
			return keys[i].snapshotFingerprint < keys[j].snapshotFingerprint
		}
		if keys[i].profile != keys[j].profile {
			return keys[i].profile < keys[j].profile
		}
		if keys[i].headerPolicyFingerprint != keys[j].headerPolicyFingerprint {
			return keys[i].headerPolicyFingerprint < keys[j].headerPolicyFingerprint
		}
		return keys[i].credential < keys[j].credential
	})
	batches := make([]sunoTaskPollingBatch, 0, len(keys))
	for _, batchKey := range keys {
		batches = append(batches, sunoTaskPollingBatch{key: batchKey, taskIDs: idsByKey[batchKey]})
	}
	batchErr := runSunoTaskPollingBatches(ctx, channelId, batches, func(batch sunoTaskPollingBatch) error {
		return updateSunoTasksWithKey(ctx, channelId, ch, adaptor, batch.key.proxy, batch.key.baseURL, batch.key.credential, batch.key.group, batch.key.model, batch.key.effective, batch.taskIDs, taskM)
	})
	if batchErr != nil {
		batchErrors = append(batchErrors, batchErr)
	}
	return errors.Join(batchErrors...)
}

func sunoTaskRouteBatchIdentity(ch *model.Channel, task *model.Task) (baseURL, operation, path, fingerprint, snapshotFingerprint, profile, headerPolicyFingerprint string, err error) {
	if task != nil && task.PrivateData.RouteSnapshot != nil {
		snapshot := task.PrivateData.RouteSnapshot
		baseURL, err = TaskRouteSnapshotBaseURL(snapshot, ch)
		if err != nil {
			return baseURL, "", "", "", "", "", "", err
		}
		route, routeErr := TaskRouteIdentityFromSnapshot(snapshot, "poll", ch)
		if routeErr != nil {
			return "", "", "", "", "", "", "", routeErr
		}
		snapshotFingerprint = strings.TrimSpace(snapshot.SnapshotFingerprint)
		if snapshot.SchemaVersion < model.TaskRouteSnapshotSchemaVersion && snapshotFingerprint == "" {
			// Legacy snapshots predate the authenticated whole-snapshot digest. Keep
			// their compatibility behavior explicit while still preventing a legacy
			// and current identity from being silently treated as the same batch.
			snapshotFingerprint = fmt.Sprintf("legacy:schema-%d", snapshot.SchemaVersion)
		}
		if snapshotFingerprint == "" {
			return "", "", "", "", "", "", "", errors.New("task route snapshot fingerprint is missing")
		}
		return baseURL,
			route.Operation,
			route.UpstreamPath,
			route.Fingerprint,
			snapshotFingerprint,
			TaskRouteSnapshotHeaderProfileRef(snapshot),
			strings.TrimSpace(snapshot.HeaderPolicyFingerprint),
			nil
	}
	if ch == nil || task == nil {
		return "", "", "", "", "", "", "", errors.New("task polling channel or task is nil")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(ch.GetBaseURL()), "/")
	path = TaskPollingRoutePath(ch.Type, task.Action)
	operation = TaskRouteOperation(ch.Type, constant.TaskPlatformSuno, task.Action)
	return baseURL, operation, path, "", "legacy:none", "", "", nil
}

func updateSunoTasksWithKey(ctx context.Context, channelId int, ch *model.Channel, adaptor TaskPollingAdaptor, proxy string, baseURL string, key string, group string, modelName string, effectiveModel string, taskIds []string, taskM map[string]*model.Task) error {
	if len(taskIds) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Suno uses one upstream request for a batch, so every local task in the
	// batch must be claimed before the request is sent. A single representative
	// claim would still allow another instance to poll the remaining task IDs.
	batchCtx, cancelBatch := context.WithCancel(ctx)
	claims := make([]*TaskPollingClaimLease, 0, len(taskIds))
	claimedTaskIDs := make([]string, 0, len(taskIds))
	releaseClaims := func() {
		cancelBatch()
		for _, claim := range claims {
			claim.Release()
		}
	}
	for _, taskRef := range taskIds {
		task, resolveErr := resolveTaskPollingTask(taskRef, taskM)
		if resolveErr != nil {
			releaseClaims()
			return fmt.Errorf("resolve Suno polling claim task %s: %w", taskRef, resolveErr)
		}
		claim, claimErr := AcquireTaskPollingClaim(batchCtx, task)
		if errors.Is(claimErr, ErrTaskPollingClaimBusy) {
			logger.LogDebug(ctx, "skip Suno task polling claim held by another worker: channel=%d task=%s", channelId, task.TaskID)
			continue
		}
		if claimErr != nil {
			releaseClaims()
			return fmt.Errorf("acquire Suno task polling claim for %s: %w", taskRef, claimErr)
		}
		claims = append(claims, claim)
		claimedTaskIDs = append(claimedTaskIDs, taskRef)
		go func(done <-chan struct{}) {
			select {
			case <-done:
				cancelBatch()
			case <-batchCtx.Done():
			}
		}(claim.Done())
	}
	if len(claimedTaskIDs) == 0 {
		releaseClaims()
		return nil
	}
	defer releaseClaims()
	ctx = batchCtx
	taskIds = claimedTaskIDs
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return fmt.Errorf("Suno polling base URL is empty")
	}
	representativeTask, err := resolveTaskPollingTask(taskIds[0], taskM)
	if err != nil {
		return fmt.Errorf("resolve Suno polling representative task %s: %w", taskIds[0], err)
	}
	attempt, err := BeginTaskHealthAttempt(ctx, representativeTask, ch, key, "poll")
	if err != nil {
		return fmt.Errorf("build Suno polling health route: %w", err)
	}
	defer attempt.Release()
	if !attempt.Selection.Allow {
		if attempt.Selection.Err != nil {
			return fmt.Errorf("Suno polling health route rejected: %w", attempt.Selection.Err)
		}
		return fmt.Errorf("Suno polling health gate rejected route: %s", attempt.Selection.Action)
	}
	adaptor.Init(attempt.Info)
	headers, err := BuildTaskRouteSnapshotRequestHeaders(ch, key, http.Header{}, representativeTask.PrivateData.RouteSnapshot)
	if err != nil {
		return err
	}
	lease, err := AcquireChannelConcurrency(attempt.Context, ch)
	if err != nil {
		return fmt.Errorf("acquire channel concurrency for Suno polling: %w", err)
	}
	if lease != nil {
		defer lease.Release()
	}
	upstreamIDs := make([]string, 0, len(taskIds))
	tasksByUpstreamID := make(map[string][]*model.Task)
	seenTasks := make(map[string]struct{})
	for _, taskRef := range taskIds {
		task, resolveErr := resolveTaskPollingTask(taskRef, taskM)
		if resolveErr != nil {
			return fmt.Errorf("resolve Suno polling task %s: %w", taskRef, resolveErr)
		}
		upstreamID := strings.TrimSpace(task.GetUpstreamTaskID())
		if upstreamID == "" {
			return fmt.Errorf("Suno polling task %s has no upstream task ID", taskRef)
		}
		identity := fmt.Sprintf("ptr:%p", task)
		if task.ID > 0 {
			identity = fmt.Sprintf("db:%d", task.ID)
		}
		if _, seen := seenTasks[identity]; seen {
			continue
		}
		seenTasks[identity] = struct{}{}
		for _, candidate := range tasksByUpstreamID[upstreamID] {
			candidateIdentity := fmt.Sprintf("ptr:%p", candidate)
			if candidate.ID > 0 {
				candidateIdentity = fmt.Sprintf("db:%d", candidate.ID)
			}
			if candidateIdentity != identity {
				return fmt.Errorf("%w: upstream task ID %q maps to multiple local tasks", ErrTaskPollingReferenceAmbiguous, upstreamID)
			}
		}
		upstreamIDs = append(upstreamIDs, upstreamID)
		tasksByUpstreamID[upstreamID] = append(tasksByUpstreamID[upstreamID], task)
	}
	if len(upstreamIDs) == 0 {
		return fmt.Errorf("Suno polling batch has no upstream task IDs")
	}
	resp, err := FetchTaskWithContext(attempt.Context.Request.Context(), adaptor, baseURL, key, map[string]any{
		"ids": upstreamIDs,
	}, proxy, headers)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task Do req error: %s", redactTaskPollingErrorMessage(err.Error(), key)))
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeDoRequestFailed)
		return err
	}
	if resp == nil {
		attempt.ObserveFailure(errors.New("Get Task returned empty response"), http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		return errors.New("Get Task returned empty response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr := RelayErrorHandler(attempt.Context.Request.Context(), resp, false)
		attempt.ObserveFailure(apiErr, apiErr.StatusCode, apiErr.GetErrorCode())
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return apiErr
	}
	if resp.Body == nil {
		err := errors.New("Get Task response body is nil")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		return err
	}
	defer CloseResponseBodyGracefully(resp)
	responseBody, err := ReadTaskPollingResponseBody(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeReadResponseBodyFailed)
		return err
	}
	var responseItems dto.TaskResponse[[]dto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, response_bytes: %d", err, len(responseBody)))
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return err
	}
	if !responseItems.IsSuccess() {
		common.SysLog(fmt.Sprintf("渠道 #%d 未完成的任务有: %d, 上游返回失败（response_bytes=%d）", channelId, len(taskIds), len(responseBody)))
		err = errors.New("suno upstream returned unsuccessful response")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return err
	}
	if len(responseItems.Data) == 0 {
		// A successful Suno batch may race task visibility on the provider side.
		// Keep local tasks pending and do not mark the channel unhealthy merely
		// because this poll returned no entries for the requested IDs.
		logger.LogWarn(ctx, fmt.Sprintf("Suno polling returned no task entries for channel=%d requested=%d; keep pending", channelId, len(upstreamIDs)))
		attempt.MarkSuccess()
		return nil
	}
	expectedIDs := make(map[string]struct{}, len(upstreamIDs))
	for _, upstreamID := range upstreamIDs {
		expectedIDs[strings.TrimSpace(upstreamID)] = struct{}{}
	}
	if !hasRecognizedSunoPollingStatus(responseItems.Data, expectedIDs) {
		err = errors.New("suno upstream response contains no recognized task status")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return err
	}
	var applyErrors []error
	for _, responseItem := range responseItems.Data {
		candidates := tasksByUpstreamID[responseItem.TaskID]
		if len(candidates) > 1 {
			return fmt.Errorf("%w: upstream task ID %q maps to multiple local tasks", ErrTaskPollingReferenceAmbiguous, responseItem.TaskID)
		}
		for _, task := range candidates {
			if err = applySunoTaskResponse(ctx, task, responseItem); err != nil {
				common.SysLog("UpdateSunoTask task error: " + err.Error())
				applyErrors = append(applyErrors, fmt.Errorf("apply Suno task %s: %w", task.TaskID, err))
			}
		}
	}
	if err := errors.Join(applyErrors...); err != nil {
		// Do not finalize a half-open probe as healthy when the provider response
		// was valid but the corresponding task state could not be persisted.
		return err
	}
	attempt.MarkSuccess()
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask dto.SunoDataResponse) bool {
	if oldTask == nil {
		return false
	}
	if newTask.SubmitTime != 0 && oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if newTask.StartTime != 0 && oldTask.StartTime != newTask.StartTime {
		return true
	}
	if newTask.FinishTime != 0 && oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if strings.TrimSpace(newTask.Status) != "" && string(oldTask.Status) != newTask.Status {
		return true
	}
	if strings.TrimSpace(newTask.FailReason) != "" && oldTask.FailReason != newTask.FailReason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	if len(bytes.TrimSpace(newTask.Data)) == 0 || bytes.Equal(bytes.TrimSpace(newTask.Data), []byte("null")) {
		return false
	}
	oldData, oldValid := canonicalTaskJSONForComparison(oldTask.Data)
	newData, newValid := canonicalTaskJSONForComparison(newTask.Data)
	if oldValid && newValid {
		return !bytes.Equal(oldData, newData)
	}
	// A malformed legacy payload must not be silently normalized or treated as
	// equal to a valid provider response. Compare trimmed bytes only as a
	// deterministic fallback, while the caller still persists the new payload.
	return !bytes.Equal(bytes.TrimSpace(oldTask.Data), bytes.TrimSpace(newTask.Data))
}

// canonicalTaskJSONForComparison converts valid JSON to a stable representation
// using the shared JSON helpers. Map key order and insignificant whitespace are
// therefore ignored, while array order and scalar values remain meaningful.
// The bool reports whether parsing and canonical marshaling both succeeded.
func canonicalTaskJSONForComparison(raw []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, false
	}
	var value any
	if err := common.Unmarshal(trimmed, &value); err != nil {
		return append([]byte(nil), trimmed...), false
	}
	canonical, err := common.Marshal(value)
	if err != nil {
		return append([]byte(nil), trimmed...), false
	}
	return canonical, true
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	var channelErrors []error
	grouped, groupingErrors := groupVideoTaskRefsByAdapter(platform, taskChannelM, taskM)
	for _, err := range groupingErrors {
		logger.LogError(ctx, err.Error())
		channelErrors = append(channelErrors, err)
	}
	keys := make([]videoTaskPollingGroupKey, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].channelID != keys[j].channelID {
			return keys[i].channelID < keys[j].channelID
		}
		return keys[i].platform < keys[j].platform
	})
	for _, key := range keys {
		if err := updateVideoTasks(ctx, key.platform, key.channelID, grouped[key], taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Channel #%d adapter %s failed to update video async tasks: %s", key.channelID, key.platform, err.Error()))
			channelErrors = append(channelErrors, fmt.Errorf("channel %d adapter %s: %w", key.channelID, key.platform, err))
		}
	}
	return errors.Join(channelErrors...)
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		// A cache/Redis read failure is transient. Keep tasks pending so the next
		// poll can retry; the timeout sweeper is the only lifecycle that may
		// terminally fail an unfinished task and refund it via CAS.
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	if GetTaskAdaptorFunc == nil {
		return errors.New("video task polling adaptor factory is unavailable")
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	var taskErrors []error
	for index, taskId := range taskIds {
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
			taskErrors = append(taskErrors, err)
		}
		if index == len(taskIds)-1 {
			break
		}
		// Sleep between tasks to avoid upstream rate limits, but do not keep a
		// cancelled maintenance cycle alive.
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return errors.Join(append(taskErrors, ctx.Err())...)
		case <-timer.C:
		}
	}
	return errors.Join(taskErrors...)
}

// parseNewAPITaskPollingEnvelope recognizes only the persisted task shape that
// new-api emits from its task query endpoints. Providers are free to use the
// same string code (for example code:"success"), so Code alone is not a
// protocol discriminator. Requiring the stable persisted-task identity fields
// keeps provider-native responses on their adapter parser path. When an
// expected task is supplied, its persisted upstream task ID and action are
// additionally checked before the envelope can affect local state. The
// remote user/channel IDs intentionally are not compared with local IDs: a
// new-api upstream may be a separate deployment with its own database.
func parseNewAPITaskPollingEnvelope(body []byte, expectedTasks ...*model.Task) (*model.Task, bool) {
	var envelope struct {
		Code string            `json:"code"`
		Data common.RawMessage `json:"data"`
	}
	if err := common.Unmarshal(body, &envelope); err != nil || envelope.Code != dto.TaskSuccessCode || common.GetJsonType(envelope.Data) != "object" {
		return nil, false
	}
	var task model.Task
	if err := common.Unmarshal(envelope.Data, &task); err != nil {
		return nil, false
	}
	// TaskDto exposes result_url at the envelope's top level, while model.Task
	// deliberately keeps its private result URL out of JSON. Decode that public
	// field separately so a remote new-api deployment cannot lose an already
	// issued direct URL during asynchronous polling.
	var taskDTO struct {
		ResultURL string `json:"result_url"`
	}
	if err := common.Unmarshal(envelope.Data, &taskDTO); err != nil {
		return nil, false
	}
	if resultURL := strings.TrimSpace(taskDTO.ResultURL); resultURL != "" {
		task.PrivateData.ResultURL = resultURL
	}
	if task.ID <= 0 || strings.TrimSpace(task.TaskID) == "" ||
		strings.TrimSpace(string(task.Platform)) == "" || task.UserId <= 0 ||
		task.ChannelId <= 0 || strings.TrimSpace(task.Group) == "" ||
		strings.TrimSpace(task.Action) == "" || strings.TrimSpace(string(task.Status)) == "" {
		return nil, false
	}
	if len(expectedTasks) > 0 {
		expected := expectedTasks[0]
		if expected == nil || strings.TrimSpace(expected.GetUpstreamTaskID()) == "" ||
			strings.TrimSpace(task.TaskID) != strings.TrimSpace(expected.GetUpstreamTaskID()) {
			return nil, false
		}
		if expectedAction := strings.TrimSpace(expected.Action); expectedAction != "" &&
			!strings.EqualFold(strings.TrimSpace(task.Action), expectedAction) {
			return nil, false
		}
	}
	return &task, true
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	if ctx == nil {
		ctx = context.Background()
	}
	task, err := resolveTaskPollingTask(taskId, taskM)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s cannot be resolved in taskM: %s", taskId, err))
		return err
	}
	claim, err := AcquireTaskPollingClaim(ctx, task)
	if errors.Is(err, ErrTaskPollingClaimBusy) {
		logger.LogDebug(ctx, "skip video task polling claim held by another worker: task=%s", task.TaskID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("acquire task polling claim for %s: %w", taskId, err)
	}
	if claim != nil {
		defer claim.Release()
		ctx = claim.Context()
	}
	key, err := resolveTaskPollingKey(ch, task)
	if err != nil {
		return fmt.Errorf("resolve polling credential for task %s: %w", taskId, err)
	}
	if ResolveTaskOriginModelName(task) == "" {
		return fmt.Errorf("task %s has no persisted model name", taskId)
	}
	baseURL, err := TaskRouteSnapshotBaseURL(task.PrivateData.RouteSnapshot, ch)
	if err != nil {
		return fmt.Errorf("resolve polling base URL for task %s: %w", taskId, err)
	}
	proxy, err := TaskRouteSnapshotProxy(task.PrivateData.RouteSnapshot, ch)
	if err != nil {
		return fmt.Errorf("resolve polling proxy for task %s: %w", taskId, err)
	}
	attempt, err := BeginTaskHealthAttempt(ctx, task, ch, key, "poll")
	if err != nil {
		return fmt.Errorf("build video task polling health route for task %s: %w", taskId, err)
	}
	defer attempt.Release()
	if !attempt.Selection.Allow {
		if attempt.Selection.Err != nil {
			return fmt.Errorf("video task polling health route rejected for task %s: %w", taskId, attempt.Selection.Err)
		}
		return fmt.Errorf("video task polling health gate rejected for task %s: %s", taskId, attempt.Selection.Action)
	}
	adaptor.Init(attempt.Info)
	headers, err := BuildTaskRouteSnapshotRequestHeaders(ch, key, http.Header{}, task.PrivateData.RouteSnapshot)
	if err != nil {
		return fmt.Errorf("build runtime request headers failed for task %s: %w", taskId, err)
	}
	lease, err := AcquireChannelConcurrency(attempt.Context, ch)
	if err != nil {
		return fmt.Errorf("acquire channel concurrency for task %s: %w", taskId, err)
	}
	if lease != nil {
		defer lease.Release()
	}
	pollBody := map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"model":   ResolveTaskEffectiveModelName(task),
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil {
		pollBody["polling_path"] = snapshot.PollingPath
	}
	resp, err := FetchTaskWithContext(attempt.Context.Request.Context(), adaptor, baseURL, key, pollBody, proxy, headers)
	if err != nil {
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeDoRequestFailed)
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	if resp == nil {
		attempt.ObserveFailure(errors.New("fetchTask returned empty response"), http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		return fmt.Errorf("fetchTask returned empty response for task %s", taskId)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr := RelayErrorHandler(attempt.Context.Request.Context(), resp, false)
		attempt.ObserveAPIError(apiErr)
		return fmt.Errorf("fetchTask returned status %d for task %s: %w", resp.StatusCode, taskId, apiErr)
	}
	if resp.Body == nil {
		err := errors.New("fetchTask response body is nil")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		return fmt.Errorf("fetchTask returned an empty response body for task %s: %w", taskId, err)
	}
	defer CloseResponseBodyGracefully(resp)
	responseBody, err := ReadTaskPollingResponseBody(resp.Body)
	if err != nil {
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeReadResponseBodyFailed)
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		err = errors.New("fetchTask returned an empty response body")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		return fmt.Errorf("fetchTask returned an empty response body for task %s", taskId)
	}

	// Upstream responses can contain signed URLs or provider-specific
	// credentials. Keep the debug log bounded to metadata and never emit the
	// response body verbatim.
	logger.LogDebug(ctx, fmt.Sprintf("updateVideoSingleTask response bytes: %d", len(responseBody)))

	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	var parsedTaskData []byte
	parsedAsNewAPIResponse := false
	// Try the new-api task envelope only after checking its persisted-task
	// identity. A provider-native response may also contain code:"success" and
	// must remain on the adapter-specific parser path.
	if t, ok := parseNewAPITaskPollingEnvelope(responseBody, task); ok {
		// The task envelope contains decrypted PrivateData while it is in
		// memory. Never format the full model with %%+v: that would emit the
		// polling credential (and any other private fields) into debug logs.
		logger.LogDebug(ctx, fmt.Sprintf(
			"updateVideoSingleTask parsed as new api response format: task_id=%s status=%s progress=%s data_bytes=%d",
			t.TaskID,
			t.Status,
			t.Progress,
			len(t.Data),
		))
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		parsedTaskData = append([]byte(nil), t.Data...)
		parsedAsNewAPIResponse = true
	} else if parser, ok := adaptor.(TaskResultParserWithContext); ok {
		taskResult, err = parser.ParseTaskResultWithContext(attempt.Context.Request.Context(), baseURL, key, responseBody, proxy, headers)
		if err != nil {
			attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
			return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
		}
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody, key, headers); err != nil {
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	}
	if taskResult == nil {
		err = errors.New("parseTaskResult returned nil task result")
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return fmt.Errorf("parseTaskResult returned nil task result for task %s", taskId)
	}
	applyTaskPollingPayload(task, parsedAsNewAPIResponse, parsedTaskData, responseBody)

	logger.LogDebug(ctx, formatTaskResultDebugLog(taskResult, key))
	if taskResult.Status != "" {
		taskResult.Status = string(model.TaskStatus(strings.ToUpper(strings.TrimSpace(taskResult.Status))))
	}

	now := time.Now().Unix()
	upstreamReportedStatus := taskResult.Status != ""
	if taskResult.Status == "" {
		// An HTTP 2xx response without a concrete task state is not a provider
		// failure. Treating it as FAILURE would permanently terminate a task on a
		// transient/malformed poll response and may refund a charge that is still
		// in flight. Observe the payload problem and leave the task pending so the
		// next maintenance cycle (or the timeout sweeper) owns the lifecycle.
		errorResult := &dto.GeneralErrorResponse{}
		if err = common.Unmarshal(responseBody, &errorResult); err == nil {
			openaiError := errorResult.TryToOpenAIError()
			if openaiError != nil {
				apiErr := types.WithOpenAIError(*openaiError, http.StatusBadGateway)
				if openaiError.Code == "429" {
					apiErr.StatusCode = http.StatusTooManyRequests
					attempt.ObserveAPIError(apiErr)
					// 429 errors usually represent a temporary polling rate limit;
					// keep the task pending and let the health cooldown gate pace the
					// next attempt.
					return nil
				}
				attempt.ObserveAPIError(apiErr)
				logger.LogWarn(ctx, fmt.Sprintf("Task %s returned an upstream error without an explicit task failure status; keep pending", taskId))
				return nil
			} else {
				err = errors.New("upstream returned unrecognized error format")
			}
		} else {
			err = fmt.Errorf("parse error response failed: %w", err)
		}
		logger.LogWarn(ctx, fmt.Sprintf("Task %s returned empty status; keep pending: %v", taskId, err))
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return nil
	}
	if taskResult.Status != "" && !isRecognizedTaskPollingStatus(model.TaskStatus(taskResult.Status)) {
		err = fmt.Errorf("unknown task status %q", taskResult.Status)
		attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		return fmt.Errorf("unknown task status %q for task %s", taskResult.Status, taskId)
	}
	// Billing is tied to the first terminal transition, not to every successful
	// response. A duplicate poll may legitimately return the same terminal
	// status after another worker has already settled or refunded the task.
	wasTerminal := isTaskTerminal(snap.Status)
	if !ShouldApplyTaskStatus(snap.Status, model.TaskStatus(taskResult.Status)) {
		logger.LogWarn(ctx, fmt.Sprintf(
			"Task %s ignored stale status transition: current=%s incoming=%s",
			task.TaskID,
			snap.Status,
			taskResult.Status,
		))
		attempt.MarkSuccess()
		return nil
	}
	shouldRefund := false
	shouldSettle := false
	quota := task.Quota
	previousProgress := task.Progress

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		if err := ApplyTaskResultURL(task, taskResult.ResolvedURL()); err != nil {
			attempt.ObserveFailure(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
			return fmt.Errorf("persist task result URL for %s: %w", taskId, err)
		}
		shouldSettle = !wasTerminal
	case model.TaskStatusFailure:
		logger.LogDebug(ctx, formatTaskPollingFailureLog(task))
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		task.FailReason = mergeTaskPollingFailureReason(task.FailReason, taskResult.Reason)
		logger.LogInfo(ctx, formatTaskPollingFailureLog(task))
		taskResult.Progress = taskcommon.ProgressComplete
		if quota != 0 && !wasTerminal {
			shouldRefund = true
		}
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	// Apply the status baseline first, then the provider value, while always
	// comparing against the progress persisted before this poll. This preserves
	// monotonicity even when a stale response reports a lower status baseline.
	task.Progress = MergeTaskProgress(previousProgress, task.Progress, isTaskTerminal(task.Status))
	task.Progress = MergeTaskProgress(task.Progress, taskResult.Progress, isTaskTerminal(task.Status))

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
			return fmt.Errorf("persist task status for %s: %w", taskId, err)
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
			shouldRefund = false
			shouldSettle = false
		}
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatus(snap.Status); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
			return fmt.Errorf("persist task state for %s: %w", taskId, err)
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, fmt.Sprintf("No update needed for task %s", task.TaskID))
	}
	if upstreamReportedStatus {
		attempt.MarkSuccess()
	}

	if shouldSettle {
		if settleErr := settleTaskBillingOnComplete(ctx, adaptor, task, taskResult); settleErr != nil {
			RevertTaskTerminalTransition(ctx, task, model.TaskStatusSuccess, snap)
			return fmt.Errorf("settle task %s: %w", task.TaskID, settleErr)
		}
	}
	if shouldRefund {
		if refundErr := RefundTaskQuota(ctx, task, task.FailReason); refundErr != nil {
			RevertTaskTerminalTransition(ctx, task, model.TaskStatusFailure, snap)
			return fmt.Errorf("refund task %s: %w", task.TaskID, refundErr)
		}
	}

	return nil
}

func isRecognizedTaskPollingStatus(status model.TaskStatus) bool {
	switch status {
	case model.TaskStatusSubmitted, model.TaskStatusQueued, model.TaskStatusInProgress, model.TaskStatusSuccess, model.TaskStatusFailure:
		return true
	default:
		return false
	}
}

// maxPersistedTaskDataURLDecodedBytes bounds inline provider results that are
// copied into the task row. The video proxy has a larger streaming limit, but
// persisting an unbounded base64 payload in JSON would make every task query
// and database backup proportional to the video size.
const (
	maxPersistedTaskDataURLDecodedBytes int64 = 64 << 20
	// Task.Data is returned by the task query endpoint and is also retained
	// between polls. Keep provider diagnostics and arbitrary metadata bounded;
	// direct URLs get a larger budget because signed URLs can carry query
	// parameters, but are bounded as well to avoid untrusted response blowups.
	maxPersistedTaskPayloadStringBytes = 256 << 10
	maxPersistedTaskPayloadURLBytes    = 16 << 10
)

// ApplyTaskResultURL records a provider result URL. Inline data URLs are kept
// only when they are valid base64 video payloads within the bounded persistence
// limit; callers must treat an error as a transient task-observation failure so
// an oversized result is never silently replaced with a gateway proxy URL.
func ApplyTaskResultURL(task *model.Task, resultURL string) error {
	if task == nil {
		return errors.New("task is nil")
	}
	resultURL = strings.TrimSpace(resultURL)
	if resultURL != "" {
		if strings.HasPrefix(strings.ToLower(resultURL), "data:") {
			if err := validatePersistedTaskDataURL(resultURL); err != nil {
				return err
			}
		} else {
			task.PrivateData.ResultURL = resultURL
			return nil
		}
		task.PrivateData.ResultURL = resultURL
		return nil
	}
	if strings.TrimSpace(task.GetResultURL()) == "" {
		task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	}
	return nil
}

func validatePersistedTaskDataURL(dataURL string) error {
	for _, r := range dataURL {
		if r < 0x20 || r == 0x7f {
			return errors.New("task result data URL contains control characters")
		}
	}
	comma := strings.IndexByte(dataURL, ',')
	if comma <= len("data:") || comma == len(dataURL)-1 {
		return errors.New("task result data URL is invalid")
	}
	metadata := dataURL[len("data:"):comma]
	parts := strings.Split(metadata, ";")
	mimeType := strings.ToLower(strings.TrimSpace(parts[0]))
	if !strings.HasPrefix(mimeType, "video/") && mimeType != "application/octet-stream" {
		return fmt.Errorf("task result data URL MIME type %q is not a video", mimeType)
	}
	base64Flag := false
	for _, part := range parts[1:] {
		part = strings.TrimSpace(part)
		if strings.EqualFold(part, "base64") {
			if base64Flag {
				return errors.New("task result data URL has duplicate base64 flag")
			}
			base64Flag = true
		}
	}
	if !base64Flag {
		return errors.New("task result data URL is not base64 encoded")
	}
	payload := dataURL[comma+1:]
	if payload == "" || strings.ContainsAny(payload, "\r\n\t ") {
		return errors.New("task result data URL payload is invalid")
	}
	var lastErr error
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		decoded, err := io.Copy(io.Discard, io.LimitReader(base64.NewDecoder(encoding, strings.NewReader(payload)), maxPersistedTaskDataURLDecodedBytes+1))
		if err == nil && decoded <= maxPersistedTaskDataURLDecodedBytes {
			return nil
		}
		if decoded > maxPersistedTaskDataURLDecodedBytes {
			return fmt.Errorf("task result data URL exceeds %d decoded bytes", maxPersistedTaskDataURLDecodedBytes)
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("invalid base64 payload")
	}
	return fmt.Errorf("task result data URL payload is invalid: %w", lastErr)
}

// applyTaskPollingPayload stores provider data after a successful poll. A
// recognized new-api envelope with empty data is intentionally not allowed to
// overwrite the last provider payload: the outer TaskDto is control-plane
// metadata, not a provider response body.
func applyTaskPollingPayload(task *model.Task, parsedAsNewAPIResponse bool, parsedTaskData, responseBody []byte) {
	if task == nil {
		return
	}
	if parsedAsNewAPIResponse {
		if !hasTaskPayloadData(parsedTaskData) {
			return
		}
		task.Data = redactVideoResponseBody(parsedTaskData)
		return
	}
	task.Data = redactVideoResponseBody(responseBody)
}

func hasTaskPayloadData(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return false
	}
	switch common.GetJsonType(trimmed) {
	case "object":
		var object map[string]any
		if err := common.Unmarshal(trimmed, &object); err == nil {
			return len(object) > 0
		}
	case "array":
		var array []any
		if err := common.Unmarshal(trimmed, &array); err == nil {
			return len(array) > 0
		}
	}
	return true
}

func redactVideoResponseBody(body []byte) []byte {
	var value any
	if err := common.Unmarshal(body, &value); err != nil {
		return omittedVideoResponseBody(len(body))
	}
	value = redactVideoPayloadValue(value)
	b, err := common.Marshal(value)
	if err != nil {
		return omittedVideoResponseBody(len(body))
	}
	return b
}

// omittedVideoResponseBody is used when a provider response cannot be safely
// parsed as JSON. Returning the original bytes here would persist arbitrary
// upstream HTML/text (including credentials echoed by a gateway) in Task.Data,
// which is later exposed through the task query API.
func omittedVideoResponseBody(size int) []byte {
	fallback, err := common.Marshal(map[string]any{
		"response_body_omitted": true,
		"response_body_bytes":   size,
	})
	if err == nil {
		return fallback
	}
	return []byte(`{"response_body_omitted":true}`)
}

// redactVideoPayloadValue applies the same policy at every nesting level. New
// API TaskDto envelopes carry provider data verbatim under data, so only
// looking at a top-level response object leaves nested binary and credential
// fields behind. URL references remain intact; inline video strings are
// bounded because the actual result URL is persisted separately.
func redactVideoPayloadValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for key, item := range current {
			normalizedKey := normalizeVideoPayloadKey(key)
			if normalizedKey == "bytesbase64encoded" || isSensitiveVideoPayloadKey(normalizedKey) {
				continue
			}
			if normalizedKey == "video" {
				if text, ok := item.(string); ok {
					out[key] = redactVideoReference(text)
					continue
				}
			}
			out[key] = redactVideoPayloadValue(item)
		}
		return out
	case []any:
		out := make([]any, len(current))
		for index, item := range current {
			out[index] = redactVideoPayloadValue(item)
		}
		return out
	case string:
		return redactTaskPayloadString(current)
	default:
		return value
	}
}

func redactTaskPayloadString(value string) string {
	limit := maxPersistedTaskPayloadStringBytes
	trimmed := strings.TrimSpace(value)
	if strings.Contains(trimmed, "://") {
		limit = maxPersistedTaskPayloadURLBytes
	}
	if len(value) <= limit {
		return value
	}
	return trimTaskPayloadString(value, limit)
}

func trimTaskPayloadString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= len("...") {
		return value[:limit]
	}
	prefix := trimToRuneBoundary(value[:limit-len("...")])
	return prefix + "..."
}

func normalizeVideoPayloadKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "")
	key = strings.ReplaceAll(key, "_", "")
	return key
}

func isSensitiveVideoPayloadKey(key string) bool {
	switch key {
	case "apikey", "authorization", "proxyauthorization", "cookie", "setcookie", "accesstoken", "refreshtoken", "clientsecret", "secretkey", "password", "credentials":
		return true
	default:
		return false
	}
}

func redactVideoReference(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:") {
		if comma := strings.IndexByte(value, ','); comma >= 0 {
			return value[:comma+1] + truncateBase64(value[comma+1:])
		}
	}
	if strings.Contains(value, "://") {
		return trimTaskPayloadString(value, maxPersistedTaskPayloadURLBytes)
	}
	return truncateBase64(value)
}

// RedactVideoResponseBody applies the bounded persistence policy used by both
// maintenance polling and request-scoped realtime task fetches.
func RedactVideoResponseBody(body []byte) []byte {
	return redactVideoResponseBody(body)
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 优先级：1. adaptor.AdjustBillingOnComplete 返回正数 -> 使用 adaptor 计算的额度
//
//  2. taskResult.TotalTokens > 0 -> 按 token 重算
//  3. 都不满足 -> 保持预扣额度不变
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) error {
	// 0. 按次计费的任务不做差额结算
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return nil
	}
	// 1. 优先让 adaptor 决定最终额度
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		return RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
	}
	// 2. 回退到 token 重算。TaskInfo 只给 total 时无法区分输入/输出单价，
	//    此时保持预扣额度不变，不能整体按输入价重算而少收输出 token。
	if taskResult.TotalTokens > 0 && taskResult.CompletionTokens > 0 &&
		taskResult.CompletionTokens <= taskResult.TotalTokens {
		return RecalculateTaskQuotaByTokensWithUsage(ctx, task,
			taskResult.TotalTokens-taskResult.CompletionTokens, taskResult.CompletionTokens)
	}
	// 3. 无调整，保持预扣额度
	return nil
}

// SettleTaskBillingOnComplete exposes the same idempotent terminal settlement
// used by the maintenance poller to request-scoped realtime fetches. Callers
// must invoke it only after winning the task status CAS transition.
func SettleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) error {
	return settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
}
