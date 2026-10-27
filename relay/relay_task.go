package relay

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type TaskSubmitResult struct {
	UpstreamTaskID string
	TaskData       []byte
	Platform       constant.TaskPlatform
	Quota          int
	RouteSnapshot  *model.TaskRouteSnapshot
	Response       *TaskSubmitResponse
	//PerCallPrice   types.PriceData
}

// TaskRouteSnapshotForContinuation returns the complete origin-task route
// snapshot carried through a remix/continuation request. The value is kept
// type-erased in relay/common to avoid an import cycle with model; a malformed
// value must fail explicitly instead of silently falling back to live channel
// routing.
func TaskRouteSnapshotForContinuation(info *relaycommon.RelayInfo) (*model.TaskRouteSnapshot, error) {
	if info == nil || info.TaskRelayInfo == nil || info.TaskRelayInfo.OriginRouteSnapshot == nil {
		return nil, nil
	}
	snapshot, ok := info.TaskRelayInfo.OriginRouteSnapshot.(*model.TaskRouteSnapshot)
	if !ok || snapshot == nil {
		return nil, errors.New("origin task route snapshot has invalid type")
	}
	return snapshot, nil
}

// TaskSubmitResponse is the successful provider response captured before the
// asynchronous task is durable. Adapters historically write directly through
// gin.Context; keeping the response in memory prevents a client from receiving
// a task ID that cannot later be queried when task persistence fails.
type TaskSubmitResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

var errTaskRouteSnapshotMismatch = errors.New("origin task route channel does not match selected channel")

// maxTaskSubmitResponseBodyBytes bounds provider responses before an adaptor
// gets a chance to decode them. Task creation responses are small JSON
// envelopes in every supported provider; allowing an unbounded body here can
// turn a malformed upstream response into an avoidable memory spike.
const maxTaskSubmitResponseBodyBytes int64 = 1 << 20

var errTaskSubmitResponseBodyTooLarge = fmt.Errorf("task submit response exceeds %d bytes", maxTaskSubmitResponseBodyBytes)

// boundedTaskSubmitResponseBody returns an explicit error once the provider
// response crosses the shared limit. io.LimitReader is intentionally not used
// because it converts an oversized body into a silently truncated JSON value,
// which would make the acceptance state ambiguous without preserving that
// fact for the caller.
type boundedTaskSubmitResponseBody struct {
	reader    io.Reader
	remaining int64
}

func (r *boundedTaskSubmitResponseBody) Read(p []byte) (int, error) {
	if r == nil || r.reader == nil {
		return 0, io.EOF
	}
	if r.remaining == 0 {
		// Probe one byte past the limit. An exact-limit body is valid when the
		// underlying reader is already at EOF; only an actual extra byte is an
		// oversized response.
		var probe [1]byte
		n, err := r.reader.Read(probe[:])
		if n > 0 {
			r.remaining = -1
			return 0, errTaskSubmitResponseBodyTooLarge
		}
		return 0, err
	}
	if r.remaining < 0 {
		return 0, errTaskSubmitResponseBodyTooLarge
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	if err != nil {
		return n, err
	}
	return n, nil
}

func (r *boundedTaskSubmitResponseBody) Close() error {
	if closer, ok := r.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func isStructuralTaskSubmitResponseError(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "read_response_body_failed", "unmarshal_response_body_failed", "unmarshal_response_failed", "invalid_response", "empty_response", "ambiguous_task_submission":
		return true
	default:
		return false
	}
}

// normalizeTaskSubmitResponseError applies one safety policy to all task
// adaptors. A successful HTTP status followed by a body/read/shape failure is
// not safely replayable: the provider may already have accepted the job. Keep
// the public error generic, retain the task ID for operator correlation, and
// prevent both refund and channel retry.
func normalizeTaskSubmitResponseError(taskErr *dto.TaskError, publicTaskID string) *dto.TaskError {
	if taskErr == nil || (!isStructuralTaskSubmitResponseError(taskErr.Code) && !taskErr.NoRefund) {
		return taskErr
	}
	if taskErr.NoRefund && taskErr.Code == "ambiguous_task_submission" {
		if !taskErr.LocalError {
			taskErr.LocalError = true
		}
		return taskErr
	}
	message := "upstream task submission response could not be interpreted after acceptance"
	if id := strings.TrimSpace(publicTaskID); id != "" {
		message += fmt.Sprintf(" (public_task_id=%s)", id)
	}
	normalized := service.TaskErrorWrapperLocal(errors.New(message), "ambiguous_task_submission", http.StatusBadGateway)
	normalized.NoRefund = true
	return normalized
}

// ambiguousTaskSubmitError marks an asynchronous POST whose acceptance cannot
// be proven from the upstream response. Every 5xx response is ambiguous for a
// task-creation endpoint: the provider may have persisted the job before its
// gateway generated the error. NoRefund makes the submission non-retryable and
// prevents a duplicate provider job.
func ambiguousTaskSubmitError(statusCode int, publicTaskID string) *dto.TaskError {
	if statusCode == 524 {
		statusCode = http.StatusGatewayTimeout
	}
	if statusCode < http.StatusInternalServerError || statusCode > 599 {
		statusCode = http.StatusBadGateway
	}
	message := fmt.Sprintf("upstream task submission status %d arrived before acceptance could be confirmed", statusCode)
	if id := strings.TrimSpace(publicTaskID); id != "" {
		message += fmt.Sprintf(" (public_task_id=%s)", id)
	}
	taskErr := service.TaskErrorWrapperLocal(errors.New(message), "ambiguous_task_submission", statusCode)
	taskErr.NoRefund = true
	return taskErr
}

func isTaskSubmitTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isAmbiguousTaskSubmitTransportError(err error) bool {
	if err == nil {
		return false
	}
	if isTaskSubmitTimeoutError(err) {
		return true
	}
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) && apiErr != nil {
		return apiErr.GetErrorCode() == types.ErrorCodeUpstreamTransportInterrupted
	}
	return types.IsUpstreamTransportInterruptedError(err)
}

func ambiguousTaskSubmitStatusCode(err error) int {
	if isTaskSubmitTimeoutError(err) {
		return http.StatusGatewayTimeout
	}
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) && apiErr != nil {
		statusCode := apiErr.StatusCode
		if statusCode == 524 {
			return http.StatusGatewayTimeout
		}
		if statusCode >= http.StatusInternalServerError && statusCode <= 599 {
			return statusCode
		}
	}
	return http.StatusBadGateway
}

// FlushTo commits a previously buffered task response to the real response
// writer. It deliberately performs no fallback rendering: callers must surface
// a write error explicitly because the task has already been persisted.
func (r *TaskSubmitResponse) FlushTo(w http.ResponseWriter) error {
	if r == nil {
		return errors.New("task submit response is nil")
	}
	if w == nil {
		return errors.New("task submit response writer is nil")
	}
	for name, values := range r.Header {
		w.Header()[name] = append([]string(nil), values...)
	}
	statusCode := r.StatusCode
	if statusCode <= 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)
	if len(r.Body) == 0 {
		return nil
	}
	_, err := w.Write(r.Body)
	return err
}

type taskSubmitResponseBufferWriter struct {
	parent      gin.ResponseWriter
	header      http.Header
	body        bytes.Buffer
	statusCode  int
	headerSet   bool
	wroteHeader bool
}

func newTaskSubmitResponseBufferWriter(parent gin.ResponseWriter) *taskSubmitResponseBufferWriter {
	header := make(http.Header)
	if parent != nil {
		for name, values := range parent.Header() {
			header[name] = append([]string(nil), values...)
		}
	}
	return &taskSubmitResponseBufferWriter{parent: parent, header: header, statusCode: http.StatusOK}
}

func (w *taskSubmitResponseBufferWriter) Header() http.Header { return w.header }

func (w *taskSubmitResponseBufferWriter) WriteHeader(code int) {
	if w.headerSet || code <= 0 {
		return
	}
	w.statusCode = code
	w.headerSet = true
}

func (w *taskSubmitResponseBufferWriter) WriteHeaderNow() {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.headerSet = true
		if w.statusCode <= 0 {
			w.statusCode = http.StatusOK
		}
	}
}

func (w *taskSubmitResponseBufferWriter) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	return w.body.Write(data)
}

func (w *taskSubmitResponseBufferWriter) WriteString(data string) (int, error) {
	w.WriteHeaderNow()
	return w.body.WriteString(data)
}

func (w *taskSubmitResponseBufferWriter) Status() int   { return w.statusCode }
func (w *taskSubmitResponseBufferWriter) Size() int     { return w.body.Len() }
func (w *taskSubmitResponseBufferWriter) Written() bool { return w.wroteHeader }

// Flush only commits the buffered header state. Task submission responses are
// non-streaming; forwarding a flush would defeat the durability barrier.
func (w *taskSubmitResponseBufferWriter) Flush() { w.WriteHeaderNow() }

func (w *taskSubmitResponseBufferWriter) CloseNotify() <-chan bool {
	if w.parent == nil {
		return make(chan bool)
	}
	return w.parent.CloseNotify()
}

func (w *taskSubmitResponseBufferWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.parent == nil {
		return nil, nil, errors.New("task submit response writer has no parent")
	}
	return w.parent.Hijack()
}

func (w *taskSubmitResponseBufferWriter) Pusher() http.Pusher {
	if w.parent == nil {
		return nil
	}
	return w.parent.Pusher()
}

func (w *taskSubmitResponseBufferWriter) snapshot() *TaskSubmitResponse {
	return &TaskSubmitResponse{
		StatusCode: w.statusCode,
		Header:     cloneHTTPHeader(w.header),
		Body:       append([]byte(nil), w.body.Bytes()...),
	}
}

func cloneHTTPHeader(src http.Header) http.Header {
	if src == nil {
		return make(http.Header)
	}
	dst := make(http.Header, len(src))
	for name, values := range src {
		dst[name] = append([]string(nil), values...)
	}
	return dst
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，重试时复用同一渠道并轮换 key），
// 以及提取 OtherRatios（时长、分辨率）。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}
	restoreOriginTaskGroup(c, info, originTask)
	bindOriginTaskRouteIntent(c, originTask)

	// 从原始任务推导模型名称。当前 schema 的 route snapshot 优先于旧
	// properties/data 字段，确保原任务的 requested/effective model 不会因
	// 当前渠道 model_mapping 被修改而在 remix/continuation 中漂移。
	if snapshot := originTask.PrivateData.RouteSnapshot; snapshot != nil {
		info.OriginRouteSnapshot = snapshot
		requested := strings.TrimSpace(snapshot.RequestedModel)
		effective := strings.TrimSpace(snapshot.EffectiveUpstreamModel)
		if requested != "" {
			// The snapshot is authoritative for a continuation. Ignore a model
			// supplied on the new request so routing and health use the same
			// immutable requested identity as the original task.
			info.OriginModelName = requested
		}
		if requested != "" && effective != "" {
			info.OriginRouteRequestedModel = requested
			info.OriginRouteEffectiveModel = effective
			info.OriginRouteGroup = strings.TrimSpace(snapshot.Group)
			info.OriginRouteChannelID = snapshot.ChannelID
		}
	}
	if strings.TrimSpace(info.OriginModelName) == "" {
		info.OriginModelName = service.ResolveTaskOriginModelName(originTask)
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	// Legacy rows usually retain the effective model in Properties. Restore it
	// before any adaptor or helper can consult the current channel mapping. A
	// current-schema snapshot is authoritative, however: falling back to a
	// secondary field when that authenticated snapshot is incomplete would hide
	// corruption and make the continuation use a route different from the row.
	persistedEffectiveModel := service.ResolveTaskPersistedEffectiveModelName(originTask)
	snapshotIsCurrent := originTask.PrivateData.RouteSnapshot != nil &&
		originTask.PrivateData.RouteSnapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion
	if info.OriginRouteSnapshot == nil || !snapshotIsCurrent {
		if effective := strings.TrimSpace(persistedEffectiveModel); effective != "" {
			info.OriginRouteRequestedModel = strings.TrimSpace(info.OriginModelName)
			info.OriginRouteEffectiveModel = effective
			info.OriginRouteGroup = strings.TrimSpace(originTask.Group)
			info.OriginRouteChannelID = originTask.ChannelId
		} else {
			// There is no safe way to reconstruct a historical mapping that was not
			// persisted. Mark the continuation so setup and model application fail
			// closed instead of silently using today's mutable model_mapping.
			info.TaskRelayInfo.OriginRouteMappingMissing = true
		}
	}
	if snapshotIsCurrent && strings.TrimSpace(info.OriginRouteEffectiveModel) == "" {
		info.TaskRelayInfo.OriginRouteMappingMissing = true
	}

	// 锁定到原始任务的渠道，并恢复提交时的具体凭证。续作必须沿用原任务
	// 的凭证身份；渠道当前 key 的轮换/删除应显式失败，不能静默换号。
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "task_channel_disable", http.StatusBadRequest)
	}
	if snapshot := originTask.PrivateData.RouteSnapshot; snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion {
		if compatibilityErr := service.ValidateTaskRouteSnapshotChannelCompatibility(snapshot, ch); compatibilityErr != nil {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("origin task route snapshot is incompatible with current channel: %w", compatibilityErr),
				"task_route_snapshot_mismatch",
				http.StatusBadRequest,
			)
		}
	}
	info.LockedChannel = ch

	key, keyErr := service.ResolveTaskChannelKey(ch, originTask)
	if keyErr != nil {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("origin task credential unavailable: %w", keyErr),
			"channel_credential_unavailable",
			http.StatusUnauthorized,
		)
	}
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	info.TaskRelayInfo.OriginRouteCredential = key
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
	common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)

	info.ChannelBaseUrl = ch.GetBaseURL()
	info.ChannelId = originTask.ChannelId
	info.ChannelType = ch.Type
	info.ApiKey = key

	// 提取 remix 参数（时长、分辨率 -> OtherRatios）
	if info.Action == constant.TaskActionRemix {
		if originTask.PrivateData.BillingContext != nil {
			// 新的 remix 逻辑：直接从原始任务的 BillingContext 中提取 OtherRatios（如果存在）
			for s, f := range originTask.PrivateData.BillingContext.OtherRatios {
				info.PriceData.AddOtherRatio(s, f)
			}
		} else {
			// 旧的 remix 逻辑：直接从 task data 解析 seconds 和 size（如果存在）
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			secondsStr, _ := taskData["seconds"].(string)
			seconds, _ := strconv.Atoi(secondsStr)
			if seconds <= 0 {
				seconds = 4
			}
			sizeStr, _ := taskData["size"].(string)
			if info.PriceData.OtherRatios == nil {
				info.PriceData.OtherRatios = map[string]float64{}
			}
			info.PriceData.OtherRatios["seconds"] = float64(seconds)
			info.PriceData.OtherRatios["size"] = 1
			if sizeStr == "1792x1024" || sizeStr == "1024x1792" {
				info.PriceData.OtherRatios["size"] = 1.666667
			}
		}
	}

	return nil
}

func bindOriginTaskRouteIntent(c *gin.Context, originTask *model.Task) {
	if c == nil || originTask == nil {
		return
	}
	if action := strings.TrimSpace(originTask.Action); action != "" {
		common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntent{
			Action:         action,
			ExplicitAction: true,
		})
	}
}

// restoreOriginTaskGroup restores the concrete group selected for the origin
// task before pricing, health attribution, and the new task route snapshot are
// built. An auto token may have selected a non-default group for the origin;
// using the token's literal "auto" value here would silently collapse the
// continuation back to the default health and billing scope.
func restoreOriginTaskGroup(c *gin.Context, info *relaycommon.RelayInfo, originTask *model.Task) {
	if info == nil || originTask == nil {
		return
	}
	group := ""
	if snapshot := originTask.PrivateData.RouteSnapshot; snapshot != nil {
		group = strings.TrimSpace(snapshot.Group)
		if strings.EqualFold(group, "auto") {
			group = ""
		}
	}
	if group == "" {
		group = strings.TrimSpace(originTask.Group)
		if strings.EqualFold(group, "auto") {
			group = ""
		}
	}
	if group == "" {
		return
	}

	info.UsingGroup = group
	if c != nil {
		common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
		// Keep the selected group visible to billing helpers that intentionally
		// consult the auto-group context after a cross-group selection.
		common.SetContextKey(c, constant.ContextKeyAutoGroup, group)
	}
}

func taskSubmissionModelName(info *relaycommon.RelayInfo, platform constant.TaskPlatform, action string) string {
	if info != nil {
		if info.TaskRelayInfo != nil {
			if requested := strings.TrimSpace(info.OriginRouteRequestedModel); requested != "" {
				return requested
			}
		}
		if modelName := strings.TrimSpace(info.OriginModelName); modelName != "" {
			return modelName
		}
	}
	return strings.TrimSpace(service.CoverTaskActionToModelName(platform, action))
}

// bindTaskSubmissionAction installs the request-level action before model
// mapping, health selection and billing. The selector may have to apply a
// provider-specific wire-model rule (currently Vidu reference generation), so
// waiting for BuildRequestBody would make those earlier decisions observe a
// different route. Non-Vidu providers intentionally collapse inferred
// multi-image actions to their ordinary image-generation action; explicit
// provider metadata remains untouched.
func bindTaskSubmissionAction(c *gin.Context, info *relaycommon.RelayInfo, platform constant.TaskPlatform) string {
	if info == nil {
		return ""
	}
	// RelayInfo embeds TaskRelayInfo to keep task-only metadata out of the
	// synchronous relay shape. Small helpers and tests may construct a bare
	// RelayInfo, so initialize the embedded value before accessing promoted
	// task fields; otherwise a missing pointer becomes a nil-dereference panic.
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	action := strings.TrimSpace(info.Action)
	fromIntent := false
	explicitIntent := false
	if action == "" && c != nil {
		if intent, ok := common.GetContextKeyType[relaycommon.TaskRouteIntent](c, constant.ContextKeyTaskRouteIntent); ok {
			action = strings.TrimSpace(intent.Action)
			fromIntent = action != ""
			explicitIntent = intent.ExplicitAction
			if fromIntent && !intent.ExplicitAction && info.ChannelMeta != nil && info.ChannelMeta.ChannelType != constant.ChannelTypeVidu {
				switch action {
				case constant.TaskActionFirstTailGenerate, constant.TaskActionReferenceGenerate:
					action = constant.TaskActionGenerate
				}
			}
		}
	}
	if action == "" && platform == constant.TaskPlatformSuno && c != nil {
		action = strings.TrimSpace(c.Param("action"))
	}
	if action != "" {
		info.Action = action
	}
	if fromIntent {
		common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntent{
			Action:         action,
			ExplicitAction: explicitIntent,
		})
	}
	return action
}

func taskSubmissionActionMismatchError(expected, actual string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(
		fmt.Errorf("task action changed after route selection: selected=%s built=%s", expected, actual),
		"task_route_action_mismatch",
		http.StatusInternalServerError,
	)
}

func applyTaskSubmissionModel(c *gin.Context, info *relaycommon.RelayInfo, modelName string) error {
	if info == nil || info.ChannelMeta == nil {
		return errors.New("task submission channel metadata is incomplete")
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return errors.New("task submission model is empty")
	}
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	if info.TaskRelayInfo != nil && info.TaskRelayInfo.OriginRouteMappingMissing {
		return errors.New("origin task effective model is missing; refusing mutable channel model mapping")
	}
	if effectiveModel := strings.TrimSpace(info.OriginRouteEffectiveModel); effectiveModel != "" {
		if info.OriginRouteChannelID > 0 && info.OriginRouteChannelID != info.ChannelMeta.ChannelId {
			return errTaskRouteSnapshotMismatch
		}
		info.UpstreamModelName = effectiveModel
		info.ChannelMeta.UpstreamModelName = effectiveModel
		info.IsModelMapped = effectiveModel != modelName
		service.BindChannelHealthRoute(c, info, effectiveModel)
		return nil
	}
	return helper.ModelMappedHelper(c, info, nil)
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 -> 确定 platform/adaptor -> 验证请求 ->
// 估算计费(EstimateBilling) -> 计算价格 -> 预扣费（仅首次）->
// 构建/发送/解析上游请求 -> 提交后计费调整(AdjustBillingOnSubmit)。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	if info == nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("task submission relay info is nil"), "invalid_task_submission", http.StatusBadRequest)
	}
	info.InitChannelMeta(c)
	if err := syncTaskSubmissionChannelIdentity(info); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "channel_not_selected", http.StatusServiceUnavailable)
	}

	// 1. 确定 platform -> 创建适配器
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	adaptor := GetTaskAdaptor(platform)
	if adaptor == nil {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest)
	}
	plannedAction := bindTaskSubmissionAction(c, info, platform)
	if info.ChannelMeta != nil && info.ChannelMeta.ChannelType == constant.ChannelTypeVidu && plannedAction == "" {
		return nil, service.TaskErrorWrapperLocal(errors.New("task route action is unavailable before model mapping"), "task_route_action_missing", http.StatusInternalServerError)
	}
	adaptor.Init(info)

	// 2. 确定并应用模型映射。适配器校验和计费都必须看到同一个有效模型；
	// 如果先校验再映射，带有 provider-specific 约束的适配器（例如 Agnes）
	// 会按客户端别名校验，而请求体却按映射后的模型发送。
	// 续作请求必须优先使用原任务快照里的公开模型，
	// 即使客户端重新提交了不同的 model 字段，也不能改变既有任务身份。
	modelName := taskSubmissionModelName(info, platform, info.Action)

	// 2.1 应用渠道的模型映射（与同步任务对齐）。Remix/continuation
	// 若携带原任务 snapshot，必须复用原 effective model，不能重新读取
	// 当前渠道的 model_mapping；否则管理员修改映射后同一任务续作会漂移。
	if strings.TrimSpace(modelName) != "" {
		if err := applyTaskSubmissionModel(c, info, modelName); err != nil {
			if errors.Is(err, errTaskRouteSnapshotMismatch) {
				return nil, service.TaskErrorWrapperLocal(err, "task_route_snapshot_mismatch", http.StatusBadRequest)
			}
			return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
		}
	}

	// 2.2 适配器校验必须在有效模型已绑定后执行。模型为空时保留校验器
	// 的原有 missing_model/invalid_request 错误语义，再在校验成功后补一次
	// action-derived model 解析。
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}
	// ValidateBasicTaskRequest stores its provider default action on info. The
	// request-level intent is authoritative for the route and must be restored
	// before EstimateBilling and the final body build. Provider-specific
	// validation still runs above, so malformed requests retain their normal
	// validation errors.
	if plannedAction != "" {
		info.Action = plannedAction
	}
	if strings.TrimSpace(modelName) == "" {
		modelName = taskSubmissionModelName(info, platform, info.Action)
		if err := applyTaskSubmissionModel(c, info, modelName); err != nil {
			if errors.Is(err, errTaskRouteSnapshotMismatch) {
				return nil, service.TaskErrorWrapperLocal(err, "task_route_snapshot_mismatch", http.StatusBadRequest)
			}
			return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
		}
	}

	// 3. 预生成公开 task ID（仅首次）
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}

	// 4. 价格计算：基础模型价格
	info.OriginModelName = modelName
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "model_price_error", http.StatusBadRequest)
	}
	info.PriceData = priceData

	// 5. 计费估算：让适配器根据用户请求提供 OtherRatios（时长、分辨率等）
	//    必须在 ModelPriceHelperPerCall 之后调用（它会重建 PriceData）。
	//    ResolveOriginTask 可能已在 remix 路径中预设了 OtherRatios，此处合并。
	if estimatedRatios := adaptor.EstimateBilling(c, info); len(estimatedRatios) > 0 {
		for k, v := range estimatedRatios {
			info.PriceData.AddOtherRatio(k, v)
		}
	}

	// 6. 将 OtherRatios 应用到基础额度
	if !common.StringsContains(constant.TaskPricePatches, modelName) {
		for _, ra := range info.PriceData.OtherRatios {
			if ra != 1.0 {
				info.PriceData.Quota = int(float64(info.PriceData.Quota) * ra)
			}
		}
	}

	// 7. 预扣费（仅首次 - 重试时 info.Billing 已存在，跳过）
	if info.Billing == nil && !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, info.PriceData.Quota, info); apiErr != nil {
			return nil, service.TaskErrorFromAPIError(apiErr)
		}
	}

	// 8. 构建请求体
	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	}
	// Some compatibility middleware and task adaptors derive the final action
	// while normalizing the request body (for example Kling text-to-video and
	// multipart image variants). BuildRequestURL and the persisted task route
	// snapshot must observe that same final action before either is constructed.
	if action := strings.TrimSpace(c.GetString("action")); action != "" {
		if plannedAction != "" && !strings.EqualFold(action, plannedAction) {
			return nil, taskSubmissionActionMismatchError(plannedAction, action)
		}
		info.Action = action
	}
	if plannedAction != "" && !strings.EqualFold(strings.TrimSpace(info.Action), plannedAction) {
		return nil, taskSubmissionActionMismatchError(plannedAction, strings.TrimSpace(info.Action))
	}

	// Build the request URL once after the final request body/model mapping is
	// known. DoTaskApiRequest reuses this request-scoped plan, and the route
	// snapshot records the same sanitized submit path instead of reconstructing a
	// potentially different operation later.
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	info.TaskRelayInfo.SubmitRequestURL = ""
	submitRequestURL, err := adaptor.BuildRequestURL(info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "build_request_url_failed", http.StatusInternalServerError)
	}
	if strings.TrimSpace(submitRequestURL) == "" {
		return nil, service.TaskErrorWrapperLocal(errors.New("task submit request URL is empty"), "build_request_url_failed", http.StatusInternalServerError)
	}
	info.TaskRelayInfo.SubmitRequestURL = submitRequestURL

	// A task that cannot preserve its selected endpoint, mapping, credential
	// scope, and proxy must never be submitted as an asynchronous task.
	routeSnapshot, snapshotErr := service.BuildTaskRouteSnapshot(c, info, platform, info.Action)
	if snapshotErr != nil {
		return nil, service.TaskErrorWrapperLocal(snapshotErr, "build_task_route_snapshot_failed", http.StatusInternalServerError)
	}

	// 9. 发送请求
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		// A reset/GOAWAY/unexpected EOF can happen after the provider accepted a
		// task but before the response reached the gateway. Do not replay this POST
		// on another channel; retain the route so the controller can persist a
		// reconciliation row and settle the original pre-consume exactly once.
		if isAmbiguousTaskSubmitTransportError(err) {
			return &TaskSubmitResult{
				Platform:      platform,
				Quota:         info.PriceData.Quota,
				RouteSnapshot: routeSnapshot,
			}, ambiguousTaskSubmitError(ambiguousTaskSubmitStatusCode(err), info.PublicTaskID)
		}
		return nil, service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if resp == nil {
		return &TaskSubmitResult{
			Platform:      platform,
			Quota:         info.PriceData.Quota,
			RouteSnapshot: routeSnapshot,
		}, ambiguousTaskSubmitError(http.StatusBadGateway, info.PublicTaskID)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		statusCode := resp.StatusCode
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		// Any 5xx from an asynchronous task-creation POST is ambiguous, not a
		// safe cross-channel retry. A provider can persist the job before its
		// gateway emits any 5xx, including 500/502/503. The controller persists a
		// terminal reconciliation row and retains 524's public 504 semantics.
		if statusCode >= http.StatusInternalServerError && statusCode <= 599 {
			taskErr := ambiguousTaskSubmitError(statusCode, info.PublicTaskID)
			return &TaskSubmitResult{
				Platform:      platform,
				Quota:         info.PriceData.Quota,
				RouteSnapshot: routeSnapshot,
			}, taskErr
		}
		// Do not echo provider HTML, signed URLs, or authentication details in a
		// task error. The status is sufficient for the client; detailed provider
		// diagnostics remain in the upstream/channel logs.
		message := fmt.Sprintf("upstream returned status %d", resp.StatusCode)
		return nil, service.TaskErrorWrapper(errors.New(message), "fail_to_fetch_task", resp.StatusCode)
	}
	// Every task adaptor reads resp.Body directly. Install one shared bounded
	// reader so a provider cannot force unbounded allocation or silently return a
	// truncated JSON envelope that looks like a normal response.
	if resp.Body == nil {
		return &TaskSubmitResult{
			Platform:      platform,
			Quota:         info.PriceData.Quota,
			RouteSnapshot: routeSnapshot,
		}, normalizeTaskSubmitResponseError(
			service.TaskErrorWrapperLocal(errors.New("task submit upstream response body is empty"), "empty_response", http.StatusBadGateway),
			info.PublicTaskID,
		)
	}
	resp.Body = &boundedTaskSubmitResponseBody{reader: resp.Body, remaining: maxTaskSubmitResponseBodyBytes}
	defer service.CloseResponseBodyGracefully(resp)

	// 10. 返回 OtherRatios 给下游（header 必须在 DoResponse 写 body 之前设置）
	otherRatios := info.PriceData.OtherRatios
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	ratiosJSON, _ := common.Marshal(otherRatios)
	c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))

	// 11. 解析响应
	originalWriter := c.Writer
	bufferWriter := newTaskSubmitResponseBufferWriter(originalWriter)
	c.Writer = bufferWriter
	var upstreamTaskID string
	var taskData []byte
	var taskErr *dto.TaskError
	func() {
		// Adaptors are third-party/provider-specific code and may panic while
		// decoding an unexpected response. Always restore Gin's writer before the
		// panic reaches the outer recovery middleware, so the request cannot retain
		// a buffer writer for subsequent handlers. A panic after a successful
		// upstream response is treated as an ambiguous submission rather than a
		// retryable channel failure; the provider may already be processing it.
		defer func() {
			c.Writer = originalWriter
			if recovered := recover(); recovered != nil {
				message := "task response handling failed after upstream acceptance"
				if publicTaskID := strings.TrimSpace(info.PublicTaskID); publicTaskID != "" {
					message += fmt.Sprintf(" (public_task_id=%s)", publicTaskID)
				}
				taskErr = service.TaskErrorWrapperLocal(errors.New(message), "ambiguous_task_submission", http.StatusBadGateway)
				taskErr.NoRefund = true
			}
		}()
		upstreamTaskID, taskData, taskErr = adaptor.DoResponse(c, resp, info)
	}()
	if taskErr != nil {
		taskErr = normalizeTaskSubmitResponseError(taskErr, info.PublicTaskID)
		// Keep the request-scoped route and the bounded provider payload when a
		// successful upstream submission cannot be decoded into a task ID. The
		// controller uses this partial result to persist an operator-visible
		// failure task and deliberately avoids retrying an already-accepted job.
		return &TaskSubmitResult{
			UpstreamTaskID: strings.TrimSpace(upstreamTaskID),
			TaskData:       append([]byte(nil), taskData...),
			Platform:       platform,
			Quota:          info.PriceData.Quota,
			RouteSnapshot:  routeSnapshot,
			Response:       bufferWriter.snapshot(),
		}, taskErr
	}
	if strings.TrimSpace(upstreamTaskID) == "" {
		taskErr := normalizeTaskSubmitResponseError(
			service.TaskErrorWrapperLocal(errors.New("upstream task id is empty"), "invalid_response", http.StatusBadGateway),
			info.PublicTaskID,
		)
		return &TaskSubmitResult{
			Platform:      platform,
			Quota:         info.PriceData.Quota,
			RouteSnapshot: routeSnapshot,
			Response:      bufferWriter.snapshot(),
		}, taskErr
	}

	// 11. 提交后计费调整：让适配器根据上游实际返回调整 OtherRatios
	finalQuota := info.PriceData.Quota
	if adjustedRatios := adaptor.AdjustBillingOnSubmit(info, taskData); len(adjustedRatios) > 0 {
		// 基于调整后的 ratios 重新计算 quota
		finalQuota = recalcQuotaFromRatios(info, adjustedRatios)
		info.PriceData.OtherRatios = adjustedRatios
		info.PriceData.Quota = finalQuota
	}

	return &TaskSubmitResult{
		UpstreamTaskID: upstreamTaskID,
		TaskData:       taskData,
		Platform:       platform,
		Quota:          finalQuota,
		RouteSnapshot:  routeSnapshot,
		Response:       bufferWriter.snapshot(),
	}, nil
}

// syncTaskSubmissionChannelIdentity keeps the legacy RelayInfo field aligned
// with the attempt-scoped ChannelMeta. RelayInfo survives selector retries, so
// ChannelId may otherwise remain zero on a forward submit or point at the
// previous candidate after a retry. The persisted task and its route snapshot
// must always name the same selected channel.
func syncTaskSubmissionChannelIdentity(info *relaycommon.RelayInfo) error {
	if info == nil {
		return errors.New("task submission relay info is nil")
	}
	if info.ChannelMeta == nil || info.ChannelMeta.ChannelId <= 0 {
		return errors.New("task submission selected channel is missing")
	}
	info.ChannelId = info.ChannelMeta.ChannelId
	return nil
}

// recalcQuotaFromRatios 根据 adjustedRatios 重新计算 quota。
// 公式: baseQuota x ∏(ratio) - 其中 baseQuota 是不含 OtherRatios 的基础额度。
func recalcQuotaFromRatios(info *relaycommon.RelayInfo, ratios map[string]float64) int {
	// 从 PriceData 获取不含 OtherRatios 的基础价格
	baseQuota := info.PriceData.Quota
	// 先除掉原有的 OtherRatios 恢复基础额度
	for _, ra := range info.PriceData.OtherRatios {
		if ra != 1.0 && ra > 0 {
			baseQuota = int(float64(baseQuota) / ra)
		}
	}
	// 应用新的 ratios
	result := float64(baseQuota)
	for _, ra := range ratios {
		if ra != 1.0 {
			result *= ra
		}
	}
	return int(result)
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
		return taskResp
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		return service.TaskErrorWrapperLocal(errors.New("task fetch response body is empty"), "empty_response", http.StatusBadGateway)
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			tasks = append(tasks, TaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	if err != nil {
		// A task row whose stored data column is not valid JSON fails here.
		// Swallowing the error would surface as a generic empty_response 502
		// and hide the real cause from the client.
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
		return
	}
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")

	// Gemini/Vertex 支持实时查询：用户 fetch 时直接从上游拉取最新状态。
	// 实时查询是对已持久化任务状态的增强；临时的渠道、凭证、网络或
	// 解析故障不能阻断已有任务结果，否则一个短暂上游故障会让已完成任务
	// 变成不可读取。tryRealtimeFetch 仍会记录健康失败，这里只回退到本地
	// 快照并记录不含凭证/响应体的诊断信息。
	if realtimeResp, realtimeErr := tryRealtimeFetch(c, originTask, isOpenAIVideoAPI); realtimeErr != nil {
		logRealtimeFetchFallback(c, originTask, realtimeErr)
	} else if len(realtimeResp) > 0 {
		respBody = realtimeResp
		return
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isOpenAIVideoAPI {
		platform, adapterErr := service.TaskRouteSnapshotAdapterPlatform(originTask.PrivateData.RouteSnapshot, originTask.Platform)
		if adapterErr != nil {
			taskResp = service.TaskErrorWrapperLocal(adapterErr, "task_route_snapshot_invalid", http.StatusBadGateway)
			return
		}
		adaptor := GetTaskAdaptor(platform)
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
			return
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			respBody = openAIVideoData
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
		return
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

func logRealtimeFetchFallback(c *gin.Context, task *model.Task, err error) {
	if c == nil || err == nil {
		return
	}
	taskID := ""
	if task != nil {
		taskID = task.TaskID
	}
	code := "unknown"
	statusCode := 0
	var apiErr *types.NewAPIError
	if errors.As(err, &apiErr) && apiErr != nil {
		code = string(apiErr.GetErrorCode())
		statusCode = apiErr.StatusCode
	} else if errors.Is(err, service.ErrChannelConcurrencyExceeded) {
		code = "channel_concurrency_exceeded"
		statusCode = http.StatusTooManyRequests
	}
	logger.LogWarn(c.Request.Context(), fmt.Sprintf(
		"realtime task fetch failed; using persisted task state: task_id=%s code=%s status=%d",
		taskID, code, statusCode,
	))
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道返回 nil，实时查询
// 出错时返回 error，由调用方记录并回退到已持久化任务状态。
// 当非 OpenAI Video API 时，还会构建自定义格式的响应体。
func tryRealtimeFetch(c *gin.Context, task *model.Task, isOpenAIVideoAPI bool) ([]byte, error) {
	if c == nil || task == nil {
		return nil, errors.New("realtime fetch context or task is nil")
	}
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil, fmt.Errorf("get task channel failed: %w", err)
	}
	channelType, err := service.TaskRouteSnapshotChannelType(task.PrivateData.RouteSnapshot, channelModel)
	if err != nil {
		return nil, fmt.Errorf("validate task channel type: %w", err)
	}
	if channelType != constant.ChannelTypeVertexAi && channelType != constant.ChannelTypeGemini {
		return nil, nil
	}
	claim, claimErr := service.AcquireTaskPollingClaim(c.Request.Context(), task)
	if errors.Is(claimErr, service.ErrTaskPollingClaimBusy) {
		return nil, claimErr
	}
	if claimErr != nil {
		return nil, fmt.Errorf("acquire realtime task polling claim: %w", claimErr)
	}
	if claim != nil {
		defer claim.Release()
	}
	requestContext := c.Request.Context()
	if claim != nil {
		requestContext = claim.Context()
	}
	requestedModel := service.ResolveTaskOriginModelName(task)
	if requestedModel == "" {
		return nil, fmt.Errorf("task %s has no persisted model name", task.TaskID)
	}
	taskKey, err := service.ResolveTaskChannelKey(channelModel, task)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeChannelNoAvailableKey, http.StatusUnauthorized, types.ErrOptionWithSkipRetry())
	}
	attempt, err := service.BeginTaskHealthAttempt(requestContext, task, channelModel, taskKey, "poll")
	if err != nil {
		return nil, fmt.Errorf("build task health route: %w", err)
	}
	defer attempt.Release()
	if !attempt.Selection.Allow {
		if attempt.Selection.Err != nil {
			return nil, types.NewErrorWithStatusCode(attempt.Selection.Err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("task channel health gate rejected route: %s", attempt.Selection.Action), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	info := attempt.Info
	lease, err := service.AcquireChannelConcurrency(attempt.Context, channelModel)
	if err != nil {
		return nil, err
	}
	if lease != nil {
		defer lease.Release()
	}

	baseURL, err := service.TaskRouteSnapshotBaseURL(task.PrivateData.RouteSnapshot, channelModel)
	if err != nil {
		return nil, fmt.Errorf("resolve task base URL: %w", err)
	}
	proxy, err := service.TaskRouteSnapshotProxy(task.PrivateData.RouteSnapshot, channelModel)
	if err != nil {
		return nil, fmt.Errorf("resolve task proxy: %w", err)
	}
	platform, err := service.TaskRouteSnapshotAdapterPlatform(task.PrivateData.RouteSnapshot, task.Platform)
	if err != nil {
		return nil, fmt.Errorf("resolve task adaptor identity: %w", err)
	}
	if platform == "" {
		platform = constant.TaskPlatform(strconv.Itoa(channelType))
	}
	adaptor := GetTaskAdaptor(platform)
	if adaptor == nil {
		return nil, fmt.Errorf("task adaptor not found for channel type %d", channelType)
	}

	headers, err := service.BuildTaskRouteSnapshotRequestHeaders(channelModel, taskKey, http.Header{}, task.PrivateData.RouteSnapshot)
	if err != nil {
		return nil, fmt.Errorf("build runtime request headers failed for realtime fetch: %w", err)
	}
	adaptor.Init(info)
	pollBody := map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
		"model":   service.ResolveTaskEffectiveModelName(task),
	}
	if snapshot := task.PrivateData.RouteSnapshot; snapshot != nil {
		pollBody["polling_path"] = snapshot.PollingPath
	}
	resp, err := service.FetchTaskWithContext(attempt.Context.Request.Context(), adaptor, baseURL, taskKey, pollBody, proxy, headers)
	if err != nil {
		apiErr := realtimeFetchAPIError(err, http.StatusBadGateway, types.ErrorCodeDoRequestFailed)
		attempt.ObserveAPIError(apiErr)
		return nil, apiErr
	}
	if resp == nil {
		apiErr := realtimeFetchAPIError(errors.New("realtime fetch returned empty response"), http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		attempt.ObserveAPIError(apiErr)
		return nil, apiErr
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		apiErr := service.RelayErrorHandler(attempt.Context.Request.Context(), resp, false)
		attempt.ObserveAPIError(apiErr)
		return nil, apiErr
	}
	if resp.Body == nil {
		apiErr := realtimeFetchAPIError(errors.New("realtime fetch response body is nil"), http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		attempt.ObserveAPIError(apiErr)
		return nil, apiErr
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, err := service.ReadTaskPollingResponseBody(resp.Body)
	if err != nil {
		apiErr := realtimeFetchAPIError(err, http.StatusBadGateway, types.ErrorCodeReadResponseBodyFailed)
		attempt.ObserveAPIError(apiErr)
		return nil, apiErr
	}
	if len(bytes.TrimSpace(body)) == 0 {
		apiErr := realtimeFetchAPIError(errors.New("realtime fetch returned empty response body"), http.StatusBadGateway, types.ErrorCodeEmptyResponse)
		attempt.ObserveAPIError(apiErr)
		return nil, apiErr
	}

	ti, err := adaptor.ParseTaskResult(body, info.ApiKey, headers)
	if err != nil {
		apiErr := realtimeFetchAPIError(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		attempt.ObserveAPIError(apiErr)
		return nil, apiErr
	}
	if ti == nil {
		apiErr := realtimeFetchAPIError(errors.New("realtime fetch returned empty task result"), http.StatusBadGateway, types.ErrorCodeBadResponseBody)
		attempt.ObserveAPIError(apiErr)
		return nil, apiErr
	}

	snap := task.Snapshot()

	// 将上游最新状态更新到 task。上游可能在终态后返回旧的或相反的
	// 状态；先做单向状态校验，再允许状态被接受时的进度/结果字段落库。
	statusAccepted := true
	statusRecognized := false
	terminalTransition := false
	if rawStatus := strings.TrimSpace(ti.Status); rawStatus != "" {
		incomingStatus := model.TaskStatus(strings.ToUpper(rawStatus))
		if !isRecognizedRealtimeTaskStatus(incomingStatus) {
			apiErr := types.NewErrorWithStatusCode(fmt.Errorf("unknown realtime task status %q", ti.Status), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			attempt.ObserveAPIError(apiErr)
			return nil, apiErr
		}
		statusRecognized = true
		if !service.ShouldApplyTaskStatus(snap.Status, incomingStatus) {
			logger.LogWarn(c, fmt.Sprintf(
				"realtime task fetch ignored stale status transition: task_id=%s current=%s incoming=%s",
				task.TaskID,
				snap.Status,
				incomingStatus,
			))
			statusAccepted = false
		} else {
			task.Status = incomingStatus
			terminalTransition = !model.IsTaskTerminalStatus(snap.Status) && model.IsTaskTerminalStatus(incomingStatus)
		}
	}
	if statusAccepted {
		// Persist the latest provider payload for the task DTO and for the
		// provider-specific content extractor. Keep the same bounded redaction
		// policy as maintenance polling; raw inline video bytes must not be
		// copied into the task row or returned by later task queries.
		// A sparse 2xx response with no recognized status is not authoritative
		// task state. Do not replace an existing payload with it: the next poll
		// must be able to recover the provider result from the last valid body.
		applyRealtimeTaskData(task, body, statusAccepted, statusRecognized)
		terminal := model.IsTaskTerminalStatus(task.Status)
		if rawStatus := strings.TrimSpace(ti.Status); rawStatus != "" {
			task.Progress = service.MergeTaskProgress(snap.Progress, realtimeTaskStatusProgress(task.Status), terminal)
			switch task.Status {
			case model.TaskStatusInProgress:
				if task.StartTime == 0 {
					task.StartTime = time.Now().Unix()
				}
			case model.TaskStatusSuccess, model.TaskStatusFailure:
				if task.FinishTime == 0 {
					task.FinishTime = time.Now().Unix()
				}
				if task.Status == model.TaskStatusFailure {
					if reason := strings.TrimSpace(ti.Reason); reason != "" {
						task.FailReason = reason
					} else if strings.TrimSpace(task.FailReason) == "" {
						task.FailReason = "realtime task fetch reported failure"
					}
				}
			}
		}
		if strings.TrimSpace(ti.Progress) != "" {
			task.Progress = service.MergeTaskProgress(task.Progress, ti.Progress, terminal)
		}
		if rawStatus := strings.TrimSpace(ti.Status); rawStatus != "" && task.Status == model.TaskStatusSuccess {
			if err := service.ApplyTaskResultURL(task, ti.ResolvedURL()); err != nil {
				apiErr := realtimeFetchAPIError(err, http.StatusBadGateway, types.ErrorCodeBadResponseBody)
				attempt.ObserveAPIError(apiErr)
				return nil, apiErr
			}
		}
	}
	casWon := true
	if !snap.Equal(task.Snapshot()) {
		var updateWon bool
		if updateWon, err = task.UpdateWithStatus(snap.Status); err != nil {
			return nil, fmt.Errorf("persist realtime task state: %w", err)
		}
		if !updateWon {
			// Another poller won the state transition. Reload the authoritative row
			// so the response and OpenAI conversion never expose this stale object.
			fresh, exists, reloadErr := model.GetByTaskId(task.UserId, task.TaskID)
			if reloadErr != nil {
				return nil, fmt.Errorf("reload realtime task state after CAS loss: %w", reloadErr)
			}
			if !exists || fresh == nil {
				return nil, errors.New("realtime task disappeared after CAS loss")
			}
			*task = *fresh
			casWon = false
		}
	}
	// A valid, recognized response is health evidence only after the task state
	// has been persisted (or a stale transition was safely ignored). This keeps
	// CAS/database failures from finalizing a half-open probe as healthy.
	if statusRecognized && statusAccepted {
		attempt.MarkSuccess()
	}

	// Realtime fetches can be the first observer to move a task into a terminal
	// state. Keep settlement/refund behind the same CAS winner guard as the
	// maintenance poller, otherwise a later poll would see a terminal task and
	// skip billing entirely.
	if casWon && terminalTransition {
		// Billing owns the terminal transition: when the funding step fails
		// before any money moved, roll the row back so the next poll retries.
		// A terminal row is never selected again by the poller, so leaving it
		// terminal would drop the settlement or refund permanently.
		if task.Status == model.TaskStatusSuccess {
			if settleErr := service.SettleTaskBillingOnComplete(c, adaptor, task, ti); settleErr != nil {
				service.RevertTaskTerminalTransition(c, task, model.TaskStatusSuccess, snap)
			}
		} else if task.Status == model.TaskStatusFailure && task.Quota != 0 {
			if refundErr := service.RefundTaskQuota(c, task, ti.Reason); refundErr != nil {
				service.RevertTaskTerminalTransition(c, task, model.TaskStatusFailure, snap)
			}
		}
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil, nil
	}

	// 非 OpenAI Video API: 构建自定义格式响应
	format := detectVideoFormat(body)
	out := map[string]any{
		"error":    nil,
		"format":   format,
		"metadata": nil,
		"status":   mapTaskStatusToSimple(task.Status),
		"task_id":  task.TaskID,
		"url":      task.GetResultURL(),
	}
	respBody, marshalErr := common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: out,
	})
	if marshalErr != nil {
		return nil, fmt.Errorf("marshal realtime task response: %w", marshalErr)
	}
	return respBody, nil
}

// applyRealtimeTaskData persists a provider payload only when its status was
// recognized and accepted by the monotonic task-state gate. A successful HTTP
// response without a task status is merely an observation error, not a new
// source of truth; preserving the previous payload keeps later result
// conversion recoverable.
func applyRealtimeTaskData(task *model.Task, body []byte, statusAccepted, statusRecognized bool) {
	if task == nil || !statusAccepted || !statusRecognized {
		return
	}
	task.Data = service.RedactVideoResponseBody(body)
}

func isRecognizedRealtimeTaskStatus(status model.TaskStatus) bool {
	switch status {
	case model.TaskStatusSubmitted, model.TaskStatusQueued, model.TaskStatusInProgress, model.TaskStatusSuccess, model.TaskStatusFailure:
		return true
	default:
		return false
	}
}

func realtimeTaskStatusProgress(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSubmitted:
		return taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		return taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		return taskcommon.ProgressInProgress
	case model.TaskStatusSuccess, model.TaskStatusFailure:
		return taskcommon.ProgressComplete
	default:
		return ""
	}
}

func taskOriginModelName(task *model.Task) string {
	if task == nil {
		return ""
	}
	if task.PrivateData.BillingContext != nil && strings.TrimSpace(task.PrivateData.BillingContext.OriginModelName) != "" {
		return strings.TrimSpace(task.PrivateData.BillingContext.OriginModelName)
	}
	if strings.TrimSpace(task.Properties.OriginModelName) != "" {
		return strings.TrimSpace(task.Properties.OriginModelName)
	}
	if strings.TrimSpace(task.Properties.UpstreamModelName) != "" {
		return strings.TrimSpace(task.Properties.UpstreamModelName)
	}
	var taskData map[string]any
	if err := common.Unmarshal(task.Data, &taskData); err == nil {
		if modelName, ok := taskData["model"].(string); ok {
			return strings.TrimSpace(modelName)
		}
	}
	return ""
}

func taskFetchRoutePath(channelType int) string {
	switch channelType {
	case constant.ChannelTypeGemini:
		return "/v1beta/operations"
	case constant.ChannelTypeVertexAi:
		return "/v1/projects:fetchPredictOperation"
	default:
		return "/v1/tasks/fetch"
	}
}

func realtimeFetchAPIError(err error, statusCode int, code types.ErrorCode) *types.NewAPIError {
	if err == nil {
		err = errors.New("realtime task fetch failed")
	}
	return types.NewOpenAIError(err, code, statusCode)
}

// detectVideoFormat 从 Gemini/Vertex 原始响应中探测视频格式
func detectVideoFormat(rawBody []byte) string {
	var raw map[string]any
	if err := common.Unmarshal(rawBody, &raw); err != nil {
		return "mp4"
	}
	respObj, ok := raw["response"].(map[string]any)
	if !ok {
		return "mp4"
	}
	vids, ok := respObj["videos"].([]any)
	if !ok || len(vids) == 0 {
		return "mp4"
	}
	v0, ok := vids[0].(map[string]any)
	if !ok {
		return "mp4"
	}
	mt, ok := v0["mimeType"].(string)
	if !ok || mt == "" || strings.Contains(mt, "mp4") {
		return "mp4"
	}
	return mt
}

// mapTaskStatusToSimple 将内部 TaskStatus 映射为简化状态字符串
func mapTaskStatusToSimple(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "queued"
	default:
		return "processing"
	}
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      task.Quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		ResultURL:  task.GetResultURL(),
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.Data,
	}
}
