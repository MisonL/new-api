package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	DefaultChannelHealthCooldown             = time.Duration(operation_setting.DefaultChannelHealthCooldownSeconds) * time.Second
	defaultChannelHealthProbeLease           = time.Duration(operation_setting.DefaultChannelHealthProbeLeaseSeconds) * time.Second
	channelHealthRedisTimeout                = 250 * time.Millisecond
	channelHealthRedisPrefix                 = "new-api:channel-health:v1"
	channelHealthRuntimeIndexKey             = channelHealthRedisPrefix + ":runtime-index"
	channelHealthRuntimeEventsKey            = channelHealthRedisPrefix + ":runtime-events"
	maxChannelHealthRoutingEvents            = 16
	maxChannelHealthRuntimeEntries           = 100
	maxChannelHealthRuntimeEvents            = 100
	channelHealthRuntimeEventTTL             = 15 * time.Minute
	channelHealthRuntimeEventLimit           = 20
	channelHealthProbeHistoryLeases          = 2
	channelHealthProbeRenewFailureLimit      = 3
	channelHealthAuthCooldown                = 60 * time.Second
	channelHealthRateLimitFallback           = 15 * time.Second
	channelHealthFailureAuth                 = "authentication"
	channelHealthFailureRateLimit            = "rate_limit"
	channelHealthFailureTransport            = "transport"
	channelHealthFailureGateway              = "gateway"
	channelHealthFailureTimeout              = "timeout"
	channelHealthFailureParameter            = "parameter"
	channelHealthFailureCanceled             = "canceled"
	channelHealthFailureProtocol             = "protocol"
	channelHealthFailureAdminTest            = "admin_test"
	channelHealthFailureWindow               = 60 * time.Second
	channelHealthFailureThreshold            = 3
	channelHealthRedisUnavailableLogInterval = time.Minute
)

const (
	ChannelHealthFailureClassIgnore    = "ignore"
	ChannelHealthFailureClassGateway   = "gateway"
	ChannelHealthFailureClassTransport = "transport"
	ChannelHealthFailureClassTimeout   = "timeout"
	ChannelHealthFailureClassRateLimit = "rate_limit"
	ChannelHealthFailureClassAuth      = "authentication"
	ChannelHealthFailureClassParameter = "parameter"
	ChannelHealthFailureClassCanceled  = "canceled"
	ChannelHealthFailureClassProtocol  = "protocol"
)

type ChannelHealthFailureDecision struct {
	Class         string
	HealthAction  string
	RequestRetry  string
	AutoBanAction string
	Scope         string
	Immediate     bool
	Cooldown      time.Duration
}

type ChannelHealthSelectionAction string

const (
	ChannelHealthSelectionHealthy                    ChannelHealthSelectionAction = "healthy"
	ChannelHealthSelectionCooldown                   ChannelHealthSelectionAction = "cooldown"
	ChannelHealthSelectionHalfOpenProbe              ChannelHealthSelectionAction = "half_open_probe"
	ChannelHealthSelectionHalfOpenBusy               ChannelHealthSelectionAction = "half_open_busy"
	ChannelHealthSelectionHalfOpenRecovered          ChannelHealthSelectionAction = "half_open_recovered"
	ChannelHealthSelectionHalfOpenRecoverySuperseded ChannelHealthSelectionAction = "half_open_recovery_superseded"
	ChannelHealthSelectionHalfOpenReleased           ChannelHealthSelectionAction = "half_open_released"
	ChannelHealthSelectionUnavailable                ChannelHealthSelectionAction = "redis_unavailable"
	ChannelHealthSelectionInvalidRoute               ChannelHealthSelectionAction = "invalid_route"
)

type ChannelHealthSelection struct {
	Allow      bool
	Action     ChannelHealthSelectionAction
	Group      string
	ModelName  string
	ChannelID  int
	Route      relaycommon.ChannelRouteIdentity
	RouteKey   string
	Err        error
	probeToken string
}

type channelHealthConfig struct {
	Enabled    bool
	Cooldown   time.Duration
	ProbeLease time.Duration
}

type channelHealthProbe struct {
	Group     string
	ModelName string
	ChannelID int
	Route     relaycommon.ChannelRouteIdentity
	RouteKey  string
	Token     string
}

type channelHealthProbeReservation struct {
	ProbeKey    string
	CooldownKey string
	HistoryKey  string
	MetadataKey string
	Token       string
	Digest      string
	Lease       time.Duration
}

type channelHealthFailureRecord struct {
	Group      string
	ModelName  string
	ChannelID  int
	Route      relaycommon.ChannelRouteIdentity
	RouteKey   string
	StatusCode int
	ErrorCode  types.ErrorCode
	Kind       string
	Cooldown   time.Duration
	Decision   ChannelHealthFailureDecision
	ProbeToken string
	Triggered  bool
}

type channelHealthHalfOpenInput struct {
	Config      channelHealthConfig
	Decision    ChannelHealthSelection
	CooldownKey string
	ProbeKey    string
	HistoryKey  string
}

type channelHealthSelectionScope struct {
	Group     string
	ModelName string
	RouteKey  string
}

type channelHealthRoutingState struct {
	Events            []dto.ChannelHealthRoutingEvent
	Probe             *channelHealthProbe
	RedisUnavailable  bool
	ExcludedChannels  map[channelHealthSelectionScope]map[int]struct{}
	RetryableChannels map[channelHealthSelectionScope]map[int]struct{}
	Routes            map[int]relaycommon.ChannelRouteIdentity
	LeaseRenewalMu    sync.Mutex
	LeaseRenewals     map[string]context.CancelFunc
	LeaseRenewalState map[string]*channelHealthProbeRenewal
}

type channelHealthProbeRenewal struct {
	cancel          context.CancelFunc
	owner           *gin.Context
	wrappedRequest  *http.Request
	previousRequest *http.Request
	failures        int
	terminated      bool
}

type channelHealthRuntimeMetadata struct {
	Group          string `json:"group"`
	ModelName      string `json:"model_name"`
	ChannelID      int    `json:"channel_id"`
	RouteKey       string `json:"route_key,omitempty"`
	EffectiveModel string `json:"effective_model,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	Operation      string `json:"operation,omitempty"`
	UpstreamPath   string `json:"upstream_path,omitempty"`
	MultiKeyIndex  int    `json:"multi_key_index,omitempty"`
	StatusCode     int    `json:"status_code,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	Kind           string `json:"kind,omitempty"`
	Cooldown       int    `json:"cooldown_seconds,omitempty"`
}

var channelHealthProbeSequence uint64
var channelHealthRedisUnavailableLogAt atomic.Int64

func getChannelHealthConfig() channelHealthConfig {
	settings := operation_setting.GetGeneralSettingSnapshot()
	cfg := channelHealthConfig{
		Enabled:    settings.ChannelHealthEnabled,
		Cooldown:   DefaultChannelHealthCooldown,
		ProbeLease: defaultChannelHealthProbeLease,
	}
	if settings.ChannelHealthCooldownSeconds > 0 {
		cfg.Cooldown = time.Duration(settings.ChannelHealthCooldownSeconds) * time.Second
	}
	if settings.ChannelHealthProbeLeaseSeconds > 0 {
		cfg.ProbeLease = time.Duration(settings.ChannelHealthProbeLeaseSeconds) * time.Second
	}
	if cfg.Cooldown < time.Duration(operation_setting.MinChannelHealthCooldownSeconds)*time.Second {
		cfg.Cooldown = time.Duration(operation_setting.MinChannelHealthCooldownSeconds) * time.Second
	}
	if cfg.Cooldown > time.Duration(operation_setting.MaxChannelHealthCooldownSeconds)*time.Second {
		cfg.Cooldown = time.Duration(operation_setting.MaxChannelHealthCooldownSeconds) * time.Second
	}
	if cfg.ProbeLease < time.Duration(operation_setting.MinChannelHealthProbeLeaseSeconds)*time.Second {
		cfg.ProbeLease = time.Duration(operation_setting.MinChannelHealthProbeLeaseSeconds) * time.Second
	}
	if cfg.ProbeLease > time.Duration(operation_setting.MaxChannelHealthProbeLeaseSeconds)*time.Second {
		cfg.ProbeLease = time.Duration(operation_setting.MaxChannelHealthProbeLeaseSeconds) * time.Second
	}
	return cfg
}

func channelHealthScope(group string, modelName string, channelID int) (string, string, int, bool) {
	group = strings.TrimSpace(group)
	modelName = strings.TrimSpace(modelName)
	if group == "" || common.IsAutoGroup(group) || modelName == "" || channelID <= 0 {
		return "", "", 0, false
	}
	return group, modelName, channelID, true
}

func channelHealthDigest(group string, modelName string, channelID int) string {
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         modelName,
		EffectiveUpstreamModel: modelName,
		FinalProtocol:          "unknown",
		Operation:              "legacy",
		UpstreamPath:           "/",
		ChannelID:              channelID,
	}
	return channelHealthDigestForRoute(group, route)
}

func channelHealthDigestForRoute(group string, route relaycommon.ChannelRouteIdentity) string {
	route = route.Normalized()
	scope := strings.TrimSpace(group) + "\x00" + route.CanonicalWithoutFingerprint()
	digest := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(digest[:])
}

func channelHealthKeys(group string, modelName string, channelID int) (cooldownKey string, historyKey string, probeKey string) {
	prefix := channelHealthRedisPrefix + ":" + channelHealthDigest(group, modelName, channelID)
	return prefix + ":cooldown", prefix + ":history", prefix + ":probe"
}

func channelHealthKeysForRoute(group string, route relaycommon.ChannelRouteIdentity) (cooldownKey, historyKey, probeKey, failuresKey string) {
	prefix := channelHealthRedisPrefix + ":" + channelHealthDigestForRoute(group, route)
	return prefix + ":cooldown", prefix + ":history", prefix + ":probe", prefix + ":failures"
}

func channelHealthMetadataKeyForRoute(group string, route relaycommon.ChannelRouteIdentity) string {
	return channelHealthRedisPrefix + ":" + channelHealthDigestForRoute(group, route) + ":metadata"
}

func channelHealthMetadataKey(group string, modelName string, channelID int) string {
	return channelHealthRedisPrefix + ":" + channelHealthDigest(group, modelName, channelID) + ":metadata"
}

func channelHealthMetadataKeyFromDigest(digest string) string {
	return channelHealthRedisPrefix + ":" + digest + ":metadata"
}

func channelHealthRedisClient(c *gin.Context) (*redis.Client, bool) {
	if !common.RedisEnabled || common.RDB == nil {
		markChannelHealthUnavailable(c)
		return nil, false
	}
	return common.RDB, true
}

func channelHealthRouting(c *gin.Context) *channelHealthRoutingState {
	if c == nil {
		return nil
	}
	if state, ok := common.GetContextKeyType[*channelHealthRoutingState](c, constant.ContextKeyChannelHealthRoutingState); ok && state != nil {
		return state
	}
	state := &channelHealthRoutingState{}
	common.SetContextKey(c, constant.ContextKeyChannelHealthRoutingState, state)
	return state
}

func recordChannelHealthEvent(c *gin.Context, group string, modelName string, channelID int, action ChannelHealthSelectionAction, statusCode int, errorCode types.ErrorCode) {
	recordChannelHealthEventForRoute(c, group, modelName, channelID, relaycommon.ChannelRouteIdentity{}, action, statusCode, errorCode)
}

func recordChannelHealthEventForRoute(c *gin.Context, group string, modelName string, channelID int, route relaycommon.ChannelRouteIdentity, action ChannelHealthSelectionAction, statusCode int, errorCode types.ErrorCode) {
	state := channelHealthRouting(c)
	if state == nil || len(state.Events) >= maxChannelHealthRoutingEvents {
		return
	}
	event := dto.ChannelHealthRoutingEvent{
		Group:      strings.TrimSpace(group),
		ModelName:  strings.TrimSpace(modelName),
		ChannelID:  channelID,
		Action:     string(action),
		StatusCode: statusCode,
		ErrorCode:  string(errorCode),
	}
	if route.ChannelID > 0 {
		route = route.Normalized()
		event.RouteKey = route.Fingerprint
		event.EffectiveModel = route.EffectiveUpstreamModel
		event.Protocol = route.FinalProtocol
		event.Operation = route.Operation
		event.UpstreamPath = route.UpstreamPath
		event.MultiKeyIndex = route.MultiKeyIndex
	}
	for _, existing := range state.Events {
		if existing == event {
			return
		}
	}
	state.Events = append(state.Events, event)
}

func recordChannelHealthRuntimeEvent(c *gin.Context, group string, modelName string, channelID int, action ChannelHealthSelectionAction, statusCode int, errorCode types.ErrorCode) {
	recordChannelHealthRuntimeEventForRoute(c, group, modelName, channelID, relaycommon.ChannelRouteIdentity{}, action, statusCode, errorCode)
}

func recordChannelHealthRuntimeEventForRoute(c *gin.Context, group string, modelName string, channelID int, route relaycommon.ChannelRouteIdentity, action ChannelHealthSelectionAction, statusCode int, errorCode types.ErrorCode) {
	group = effectiveChannelHealthGroup(c, group)
	group, modelName, channelID, ok := channelHealthScope(group, modelName, channelID)
	if !ok {
		return
	}
	rdb, ok := channelHealthRedisClient(c)
	if !ok {
		return
	}

	event := dto.ChannelHealthRuntimeEvent{
		OccurredAt: time.Now().UTC().Unix(),
		Group:      group,
		ModelName:  modelName,
		ChannelID:  channelID,
		Action:     string(action),
		StatusCode: statusCode,
		ErrorCode:  string(errorCode),
	}
	if route.ChannelID > 0 {
		route = route.Normalized()
		event.RouteKey = route.Fingerprint
		event.EffectiveModel = route.EffectiveUpstreamModel
		event.Protocol = route.FinalProtocol
		event.Operation = route.Operation
		event.UpstreamPath = route.UpstreamPath
		event.MultiKeyIndex = route.MultiKeyIndex
	}
	eventJSON, err := common.Marshal(event)
	if err != nil {
		common.SysError(fmt.Sprintf("channel health runtime event marshal failed: channel_id=%d error=%v", channelID, err))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), channelHealthRedisTimeout)
	defer cancel()
	pipeline := rdb.TxPipeline()
	pipeline.LPush(ctx, channelHealthRuntimeEventsKey, string(eventJSON))
	pipeline.LTrim(ctx, channelHealthRuntimeEventsKey, 0, maxChannelHealthRuntimeEvents-1)
	pipeline.Expire(ctx, channelHealthRuntimeEventsKey, channelHealthRuntimeEventTTL)
	if _, err := pipeline.Exec(ctx); err != nil {
		common.SysError(fmt.Sprintf("channel health runtime event update failed: channel_id=%d error=%v", channelID, err))
	}
}

func markChannelHealthUnavailable(c *gin.Context) {
	logChannelHealthRedisUnavailable()
	state := channelHealthRouting(c)
	if state == nil || state.RedisUnavailable {
		return
	}
	state.RedisUnavailable = true
	common.SetContextKey(c, constant.ContextKeyChannelHealthUnavailable, true)
	recordChannelHealthEvent(c, "", "", 0, ChannelHealthSelectionUnavailable, 0, "")
}

func logChannelHealthRedisUnavailable() {
	now := time.Now().UnixNano()
	for {
		previous := channelHealthRedisUnavailableLogAt.Load()
		if previous != 0 && now-previous < channelHealthRedisUnavailableLogInterval.Nanoseconds() {
			return
		}
		if channelHealthRedisUnavailableLogAt.CompareAndSwap(previous, now) {
			common.SysError("channel health Redis unavailable; routing is failing open")
			return
		}
	}
}

func effectiveChannelHealthGroup(c *gin.Context, group string) string {
	group = strings.TrimSpace(group)
	if !common.IsAutoGroup(group) {
		return group
	}
	if c == nil {
		return ""
	}
	return strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyAutoGroup))
}

func channelHealthRouteProtocol(c *gin.Context, channelType int, relayMode int) string {
	apiType, _ := common.ChannelType2APIType(channelType)
	switch apiType {
	case constant.APITypeAnthropic:
		return "claude"
	case constant.APITypeGemini:
		return "gemini"
	case constant.APITypeVertexAi:
		return "vertex"
	case constant.APITypeCodex:
		return "codex_responses"
	default:
		if relayMode == relayconstant.RelayModeResponses || relayMode == relayconstant.RelayModeResponsesCompact {
			return "openai_responses"
		}
		return "openai"
	}
}

func channelHealthRoutePath(c *gin.Context, channel *model.Channel, requestedModel string) string {
	path := "/"
	if c != nil && c.Request != nil && c.Request.URL != nil {
		path = c.Request.URL.Path
	}
	if _, compact := ratio_setting.CompactBaseModelName(strings.TrimSpace(requestedModel)); compact {
		settings := dto.ChannelOtherSettings{}
		hasSettings := false
		if channel != nil {
			settings = channel.GetOtherSettings()
			hasSettings = true
		}
		if channel == nil || channelHealthContextMatchesChannel(c, channel) {
			if contextSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting); ok {
				settings = contextSettings
				hasSettings = true
			}
		}
		if hasSettings && settings.HasSyntheticResponsesCompact() {
			path = "/v1/responses"
		}
	}
	return relaycommon.NormalizeRoutePath(path)
}

// channelHealthOperation is a stable provider/relay-operation discriminator.
// The path alone is not sufficient: different providers or actions may share
// one endpoint shape while having different authentication and response
// semantics. Asynchronous task snapshots use a richer action identity; normal
// relay requests use the channel type, relay mode and normalized path.
func channelHealthOperation(channelType int, relayMode int, path string) string {
	normalizedPath := relaycommon.NormalizeRoutePath(path)
	// A few legacy/internal callers build a health route without a request path
	// or channel metadata. Keep that compatibility identity stable; real relay
	// paths and persisted task snapshots still carry provider/action identity.
	if relayMode == relayconstant.RelayModeUnknown && normalizedPath == "/" {
		return "relay.legacy.unknown./"
	}
	return fmt.Sprintf("relay.%d.%d.%s", channelType, relayMode, normalizedPath)
}

func channelHealthContextMatchesChannel(c *gin.Context, channel *model.Channel) bool {
	return c != nil && channel != nil && channel.Id > 0 &&
		common.GetContextKeyInt(c, constant.ContextKeyChannelId) == channel.Id
}

func channelHealthBaseRoute(route relaycommon.ChannelRouteIdentity) relaycommon.ChannelRouteIdentity {
	route.MultiKeyIndex = 0
	route.CredentialScoped = false
	route.Fingerprint = ""
	return route.Normalized()
}

func channelHealthCredentialRoute(c *gin.Context, route relaycommon.ChannelRouteIdentity) relaycommon.ChannelRouteIdentity {
	route = channelHealthBaseRoute(route)
	if c == nil || route.ChannelID <= 0 || common.GetContextKeyInt(c, constant.ContextKeyChannelId) != route.ChannelID ||
		!common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey) {
		return route
	}
	route.MultiKeyIndex = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
	route.CredentialScoped = true
	route.Fingerprint = ""
	return route.Normalized()
}

func channelHealthRouteMatchesProbe(route relaycommon.ChannelRouteIdentity, probe relaycommon.ChannelRouteIdentity) bool {
	route = route.Normalized()
	probe = probe.Normalized()
	return route.CanonicalWithoutFingerprint() == probe.CanonicalWithoutFingerprint()
}

// channelHealthProbeRouteCompatible accepts protocol/path changes introduced
// by a relay adaptor while keeping the model and concrete credential identity
// strict. A channel-level probe may be completed by the key selected after
// the probe is reserved, but a concrete key probe cannot be reused by another
// key or by an unscoped route.
func channelHealthProbeRouteCompatible(route relaycommon.ChannelRouteIdentity, probe relaycommon.ChannelRouteIdentity) bool {
	route = route.Normalized()
	probe = probe.Normalized()
	if route.ChannelID != probe.ChannelID ||
		route.RequestedModel != probe.RequestedModel ||
		route.EffectiveUpstreamModel != probe.EffectiveUpstreamModel {
		return false
	}
	if route.Operation != "" && probe.Operation != "" && route.Operation != probe.Operation {
		// Task snapshots encode provider/action and phase in the operation. A
		// path rewrite must not let a polling/content operation consume another
		// task's half-open lease.
		if strings.HasPrefix(route.Operation, "task.") || strings.HasPrefix(probe.Operation, "task.") {
			return false
		}
		// Normal relay adaptors may change protocol/path (for example Responses
		// -> Chat). Permit that explicit bridge, but reject an unexplained
		// operation change where the route shape is otherwise identical.
		if !strings.HasPrefix(route.Operation, "relay.") || !strings.HasPrefix(probe.Operation, "relay.") {
			return false
		}
		if route.FinalProtocol == probe.FinalProtocol &&
			relaycommon.NormalizeRoutePath(route.UpstreamPath) == relaycommon.NormalizeRoutePath(probe.UpstreamPath) {
			return false
		}
	}
	if !probe.CredentialScoped {
		// A channel-level probe may be completed by whichever concrete
		// credential the selector assigns.
		return true
	}
	// A concrete probe is only valid for the same concrete credential.
	return route.CredentialScoped && route.MultiKeyIndex == probe.MultiKeyIndex
}

// BuildChannelHealthRouteForChannel is the single route identity builder used
// by selection and relay mapping. It performs no mutation of the request.
func BuildChannelHealthRouteForChannel(c *gin.Context, group string, requestedModel string, channel *model.Channel) (relaycommon.ChannelRouteIdentity, error) {
	return buildChannelHealthRouteForChannel(c, group, requestedModel, channel, "", false)
}

// BuildChannelHealthRouteForChannelWithEffectiveModel builds the same route
// identity from a mapping result already resolved by the selector. This is used
// by candidate filters to avoid reparsing model_mapping while checking health.
func BuildChannelHealthRouteForChannelWithEffectiveModel(c *gin.Context, group string, requestedModel string, channel *model.Channel, effectiveModel string) (relaycommon.ChannelRouteIdentity, error) {
	return buildChannelHealthRouteForChannel(c, group, requestedModel, channel, effectiveModel, true)
}

func buildChannelHealthRouteForChannel(c *gin.Context, group string, requestedModel string, channel *model.Channel, effectiveModel string, effectiveModelProvided bool) (relaycommon.ChannelRouteIdentity, error) {
	group = effectiveChannelHealthGroup(c, group)
	requestedModel = strings.TrimSpace(requestedModel)
	if channel == nil || group == "" || common.IsAutoGroup(group) || requestedModel == "" || channel.Id <= 0 {
		return relaycommon.ChannelRouteIdentity{}, fmt.Errorf("invalid channel health route scope")
	}
	relayMode := relayconstant.Path2RelayMode("")
	if c != nil && c.Request != nil {
		relayMode = relayconstant.Path2RelayMode(c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeUnknown {
			relayMode = c.GetInt("relay_mode")
		}
	}
	_, requestedCompact := ratio_setting.CompactBaseModelName(requestedModel)
	compact := relayMode == relayconstant.RelayModeResponsesCompact || requestedCompact
	modelMapping := channel.GetModelMapping()
	if (strings.TrimSpace(modelMapping) == "" || channelModelMappingIsSemanticallyEmpty(modelMapping)) && channelHealthContextMatchesChannel(c, channel) {
		modelMapping = common.GetContextKeyString(c, constant.ContextKeyChannelModelMapping)
		if strings.TrimSpace(modelMapping) == "" {
			modelMapping = c.GetString("model_mapping")
		}
	}
	if !effectiveModelProvided {
		var resolved bool
		effectiveModel, resolved = selectedRouteEffectiveModel(c, channel, requestedModel, compact)
		if resolved {
			return channelHealthBaseRoute(relaycommon.ChannelRouteIdentity{
				RequestedModel:         requestedModel,
				EffectiveUpstreamModel: effectiveModel,
				FinalProtocol:          channelHealthRouteProtocol(c, channel.Type, relayMode),
				Operation:              channelHealthOperation(channel.Type, relayMode, channelHealthRoutePath(c, channel, requestedModel)),
				UpstreamPath:           channelHealthRoutePath(c, channel, requestedModel),
				ChannelID:              channel.Id,
			}).Normalized(), nil
		}
		var err error
		// Resolve through the request-scoped memoized resolver so affinity,
		// specific-channel, health, and relay dispatch observe the same mapping
		// snapshot. The local modelMapping value remains the compatibility input
		// for callers that do not carry a Gin request context.
		if c != nil {
			effectiveModel, _, err = ResolveChannelModelMappingForContext(c, channel, requestedModel, compact)
		} else {
			effectiveModel, _, err = model.ResolveChannelModelMappingForRouting(modelMapping, requestedModel, compact)
		}
		if err != nil {
			return relaycommon.ChannelRouteIdentity{}, err
		}
	}
	return channelHealthBaseRoute(relaycommon.ChannelRouteIdentity{
		RequestedModel:         requestedModel,
		EffectiveUpstreamModel: effectiveModel,
		FinalProtocol:          channelHealthRouteProtocol(c, channel.Type, relayMode),
		Operation:              channelHealthOperation(channel.Type, relayMode, channelHealthRoutePath(c, channel, requestedModel)),
		UpstreamPath:           channelHealthRoutePath(c, channel, requestedModel),
		ChannelID:              channel.Id,
	}).Normalized(), nil
}

// selectedRouteEffectiveModel returns the immutable mapping result produced by
// the selector for this exact channel and request model. The selector has
// already validated the mapping before exposing the candidate; reusing that
// result keeps health and relay dispatch from parsing model_mapping again (and
// prevents a mid-request mapping edit from changing the route identity).
func selectedRouteEffectiveModel(c *gin.Context, channel *model.Channel, requestedModel string, compact bool) (string, bool) {
	_ = compact
	if c == nil || channel == nil {
		return "", false
	}
	diagnostics, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics)
	if !ok || diagnostics.SelectedChannelID != channel.Id || strings.TrimSpace(diagnostics.RequestedModel) != strings.TrimSpace(requestedModel) {
		return "", false
	}
	if diagnostics.EffectiveRouteModel == "" {
		return "", false
	}
	return strings.TrimSpace(diagnostics.EffectiveRouteModel), true
}

func bindSelectedRouteMapping(c *gin.Context, group string, requestedModel string, channel *model.Channel, effectiveModel string) {
	if c == nil || channel == nil || channel.Id <= 0 || strings.TrimSpace(effectiveModel) == "" {
		return
	}
	diagnostics, _ := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics)
	diagnostics.Group = strings.TrimSpace(group)
	diagnostics.RequestedModel = strings.TrimSpace(requestedModel)
	diagnostics.EffectiveRouteModel = strings.TrimSpace(effectiveModel)
	diagnostics.SelectedChannelID = channel.Id
	common.SetContextKey(c, constant.ContextKeyChannelRouteDiagnostics, diagnostics)
}

func channelHealthRouteForInfo(c *gin.Context, info *relaycommon.RelayInfo, effectiveModel string) relaycommon.ChannelRouteIdentity {
	if info == nil || info.ChannelMeta == nil {
		return relaycommon.ChannelRouteIdentity{}
	}
	if info.ChannelHealthRoute != nil {
		route := info.ChannelHealthRoute.Normalized()
		if info.ChannelMeta.ChannelIsMultiKey {
			route.MultiKeyIndex = info.ChannelMeta.ChannelMultiKeyIndex
			route.CredentialScoped = true
			route.Fingerprint = ""
		}
		return route.Normalized()
	}
	return buildChannelHealthRouteForInfo(c, info, effectiveModel)
}

func buildChannelHealthRouteForInfo(c *gin.Context, info *relaycommon.RelayInfo, effectiveModel string) relaycommon.ChannelRouteIdentity {
	if info == nil || info.ChannelMeta == nil {
		return relaycommon.ChannelRouteIdentity{}
	}
	path := info.UpstreamRequestPath
	if path == "" {
		path = channelHealthRoutePath(c, nil, info.OriginModelName)
	}
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         info.OriginModelName,
		EffectiveUpstreamModel: strings.TrimSpace(effectiveModel),
		FinalProtocol:          channelHealthRouteProtocol(c, info.ChannelType, info.RelayMode),
		Operation:              channelHealthOperation(info.ChannelType, info.RelayMode, path),
		UpstreamPath:           path,
		ChannelID:              info.ChannelMeta.ChannelId,
		MultiKeyIndex:          info.ChannelMeta.ChannelMultiKeyIndex,
		CredentialScoped:       info.ChannelMeta.ChannelIsMultiKey,
	}
	return channelHealthCredentialRoute(c, route)
}

func channelHealthActualRouteForInfo(c *gin.Context, info *relaycommon.RelayInfo) relaycommon.ChannelRouteIdentity {
	route := channelHealthRouteForInfo(c, info, "")
	if info == nil {
		return route
	}
	// Adaptors may finalize or rewrite the upstream model after the initial
	// route is bound (for example protocol bridges and provider-specific model
	// aliases). Health state must follow the model that was actually sent.
	if upstreamModel := strings.TrimSpace(info.UpstreamModelName); upstreamModel != "" &&
		upstreamModel != route.EffectiveUpstreamModel {
		route.EffectiveUpstreamModel = upstreamModel
		route.Fingerprint = ""
	}
	if strings.TrimSpace(info.UpstreamRequestPath) != "" {
		previousPath := route.UpstreamPath
		route.UpstreamPath = relaycommon.NormalizeRoutePath(info.UpstreamRequestPath)
		switch route.UpstreamPath {
		case "/v1/messages":
			route.FinalProtocol = "claude"
		case "/v1/chat/completions":
			route.FinalProtocol = "openai"
		case "/v1/responses", "/v1/responses/compact":
			route.FinalProtocol = "openai_responses"
		}
		// Normal relay identities encode the path in Operation as well as in
		// UpstreamPath. Adaptors can rewrite the path after selection (for
		// example Responses -> Chat), so recompute the generic operation before
		// hashing the actual route. Task snapshots use task.<provider>.<action>
		// operations and must retain that richer action identity across a path
		// rewrite.
		if route.UpstreamPath != previousPath &&
			(strings.TrimSpace(route.Operation) == "" || strings.HasPrefix(route.Operation, "relay.")) {
			route.Operation = channelHealthOperation(info.ChannelType, info.RelayMode, route.UpstreamPath)
		}
		route.Fingerprint = ""
	}
	return route.Normalized()
}

// BindChannelHealthRoute attaches the already-selected immutable route to the
// relay info. If a channel-test or direct handler did not pass through the
// selector, it constructs the same identity from the current channel context.
func BindChannelHealthRoute(c *gin.Context, info *relaycommon.RelayInfo, effectiveModel string) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	expectedRoute := buildChannelHealthRouteForInfo(c, info, effectiveModel)
	state := channelHealthRouting(c)
	if state != nil && state.Routes != nil {
		if route, ok := state.Routes[info.ChannelMeta.ChannelId]; ok {
			route = channelHealthCredentialRoute(c, route)
			if route.CanonicalWithoutFingerprint() == expectedRoute.CanonicalWithoutFingerprint() {
				info.ChannelHealthRoute = &route
				return
			}
		}
	}
	if expectedRoute.ChannelID > 0 {
		info.ChannelHealthRoute = &expectedRoute
	}
}

// BindChannelHealthRouteForModel binds a route whose requested model or
// upstream path is derived by a special endpoint rather than by the normal
// request selector (for example Midjourney actions or task polling). The
// route remains attempt-scoped and uses the same credential identity rules as
// the regular relay path.
func BindChannelHealthRouteForModel(c *gin.Context, info *relaycommon.RelayInfo, requestedModel string, effectiveModel string) {
	if info == nil || info.ChannelMeta == nil || info.ChannelMeta.ChannelId <= 0 {
		return
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		requestedModel = strings.TrimSpace(info.OriginModelName)
	}
	effectiveModel = strings.TrimSpace(effectiveModel)
	if effectiveModel == "" {
		effectiveModel = strings.TrimSpace(info.UpstreamModelName)
	}
	if effectiveModel == "" {
		effectiveModel = requestedModel
	}
	path := relaycommon.NormalizeRoutePath(info.UpstreamRequestPath)
	if path == "/" {
		path = channelHealthRoutePath(c, nil, requestedModel)
	}
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         requestedModel,
		EffectiveUpstreamModel: effectiveModel,
		FinalProtocol:          channelHealthRouteProtocol(c, info.ChannelType, info.RelayMode),
		Operation:              channelHealthOperation(info.ChannelType, info.RelayMode, path),
		UpstreamPath:           path,
		ChannelID:              info.ChannelMeta.ChannelId,
		MultiKeyIndex:          info.ChannelMeta.ChannelMultiKeyIndex,
		CredentialScoped:       info.ChannelMeta.ChannelIsMultiKey,
	}.Normalized()
	route = channelHealthCredentialRoute(c, route)
	info.ChannelHealthRoute = &route
}

func selectedChannelHealthRoute(c *gin.Context, route relaycommon.ChannelRouteIdentity) {
	state := channelHealthRouting(c)
	if state == nil || route.ChannelID <= 0 {
		return
	}
	if state.Routes == nil {
		state.Routes = make(map[int]relaycommon.ChannelRouteIdentity)
	}
	state.Routes[route.ChannelID] = route.Normalized()
}

func newChannelHealthProbeToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err == nil {
		return hex.EncodeToString(buf)
	}
	return fmt.Sprintf("fallback-%d", atomic.AddUint64(&channelHealthProbeSequence, 1))
}

// reserveChannelHealthProbeScript is the single Redis check-and-acquire path
// for a health route. It reads cooldown/history and reserves the half-open
// lease in one Lua transaction, so a concurrent failure cannot slip between
// the gate read and probe acquisition.
const reserveChannelHealthProbeScript = `
if redis.call("EXISTS", KEYS[2]) > 0 then
	return -2
end
if redis.call("EXISTS", KEYS[3]) == 0 then
	return -1
end
if not redis.call("SET", KEYS[1], ARGV[1], "NX", "PX", ARGV[2]) then
	return 0
end
redis.call("PEXPIRE", KEYS[3], ARGV[3])
redis.call("PEXPIRE", KEYS[4], ARGV[3])
local redis_time = redis.call("TIME")
local now_ms = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
local runtime_expiry_unix = math.floor((now_ms + tonumber(ARGV[3]) + 999) / 1000)
redis.call("ZADD", KEYS[5], runtime_expiry_unix, ARGV[4])
return 1
`

func reserveChannelHealthProbe(
	ctx context.Context,
	rdb *redis.Client,
	reservation channelHealthProbeReservation,
) (reserved bool, historyPresent bool, cooldownPresent bool, err error) {
	historyTTL := reservation.Lease * channelHealthProbeHistoryLeases
	result, err := rdb.Eval(
		ctx,
		reserveChannelHealthProbeScript,
		[]string{
			reservation.ProbeKey,
			reservation.CooldownKey,
			reservation.HistoryKey,
			reservation.MetadataKey,
			channelHealthRuntimeIndexKey,
		},
		reservation.Token,
		reservation.Lease.Milliseconds(),
		historyTTL.Milliseconds(),
		reservation.Digest,
	).Int()
	if err != nil {
		return false, false, false, err
	}
	switch result {
	case -2:
		return false, true, true, nil
	case -1:
		return false, false, false, nil
	case 1:
		return true, true, false, nil
	default:
		return false, true, false, nil
	}
}

func evaluateChannelHealthSelection(c *gin.Context, group string, modelName string, channelID int) ChannelHealthSelection {
	group = effectiveChannelHealthGroup(c, group)
	route := relaycommon.ChannelRouteIdentity{
		RequestedModel:         strings.TrimSpace(modelName),
		EffectiveUpstreamModel: strings.TrimSpace(modelName),
		FinalProtocol:          channelHealthRouteProtocol(c, 0, 0),
		Operation:              channelHealthOperation(0, 0, channelHealthRoutePath(c, nil, modelName)),
		UpstreamPath:           channelHealthRoutePath(c, nil, modelName),
		ChannelID:              channelID,
	}.Normalized()
	return evaluateChannelHealthSelectionWithRoute(c, group, modelName, channelID, route, false)
}

// EvaluateChannelHealthSelectionForChannel evaluates the same immutable route
// that ModelMappedHelper will use after the channel is installed in context.
func EvaluateChannelHealthSelectionForChannel(c *gin.Context, group string, modelName string, channel *model.Channel) ChannelHealthSelection {
	return evaluateChannelHealthSelectionForChannel(c, group, modelName, channel, true)
}

// EvaluateChannelHealthSelectionForChannelAtBaseRoute evaluates a candidate
// before SetupContextForSelectedChannel has chosen its concrete credential.
// The caller may retain a previous attempt's key in Gin context, so this path
// must never accidentally gate a newly selected candidate by that stale key.
func EvaluateChannelHealthSelectionForChannelAtBaseRoute(c *gin.Context, group string, modelName string, channel *model.Channel) ChannelHealthSelection {
	return evaluateChannelHealthSelectionForChannel(c, group, modelName, channel, false)
}

// EvaluateChannelHealthSelectionForRoute evaluates a route identity assembled
// by a special endpoint. This keeps health selection aligned with the actual
// upstream path when the incoming request path is only a public proxy path.
func EvaluateChannelHealthSelectionForRoute(c *gin.Context, group string, modelName string, route relaycommon.ChannelRouteIdentity) ChannelHealthSelection {
	route = route.Normalized()
	return evaluateChannelHealthSelectionWithRoute(c, effectiveChannelHealthGroup(c, group), modelName, route.ChannelID, route, true)
}

func evaluateChannelHealthSelectionForChannel(c *gin.Context, group string, modelName string, channel *model.Channel, includeCredential bool) ChannelHealthSelection {
	route, err := BuildChannelHealthRouteForChannel(c, group, modelName, channel)
	if err != nil {
		if !model.IsChannelModelMappingError(err) {
			return ChannelHealthSelection{Allow: true, Action: ChannelHealthSelectionHealthy, Group: effectiveChannelHealthGroup(c, group), ModelName: strings.TrimSpace(modelName), ChannelID: channelID(channel)}
		}
		group = effectiveChannelHealthGroup(c, group)
		requestedModel := strings.TrimSpace(modelName)
		fallbackRoute := channelHealthBaseRoute(relaycommon.ChannelRouteIdentity{
			RequestedModel:         requestedModel,
			EffectiveUpstreamModel: requestedModel,
			FinalProtocol:          channelHealthRouteProtocol(c, 0, 0),
			Operation:              channelHealthOperation(channelType(channel), 0, channelHealthRoutePath(c, channel, requestedModel)),
			UpstreamPath:           channelHealthRoutePath(c, channel, requestedModel),
			ChannelID:              channelID(channel),
		})
		decision := ChannelHealthSelection{
			Allow:     false,
			Action:    ChannelHealthSelectionInvalidRoute,
			Group:     group,
			ModelName: requestedModel,
			ChannelID: channelID(channel),
			Route:     fallbackRoute,
			RouteKey:  fallbackRoute.Fingerprint,
			Err:       err,
		}
		recordChannelHealthEventForRoute(c, group, requestedModel, decision.ChannelID, fallbackRoute, decision.Action, http.StatusServiceUnavailable, types.ErrorCodeChannelModelMappedError)
		return decision
	}
	bindSelectedRouteMapping(c, effectiveChannelHealthGroup(c, group), modelName, channel, route.EffectiveUpstreamModel)
	if includeCredential {
		return evaluateChannelHealthSelectionWithRoute(c, effectiveChannelHealthGroup(c, group), modelName, route.ChannelID, route, true)
	}
	return evaluateChannelHealthSelectionWithRoute(c, effectiveChannelHealthGroup(c, group), modelName, route.ChannelID, route, false)
}

func channelID(channel *model.Channel) int {
	if channel == nil {
		return 0
	}
	return channel.Id
}

func channelType(channel *model.Channel) int {
	if channel == nil {
		return 0
	}
	return channel.Type
}

func evaluateChannelHealthSelectionWithRoute(c *gin.Context, group string, modelName string, channelID int, route relaycommon.ChannelRouteIdentity, includeCredential bool) ChannelHealthSelection {
	group = effectiveChannelHealthGroup(c, group)
	decision := ChannelHealthSelection{
		Allow:     true,
		Action:    ChannelHealthSelectionHealthy,
		Group:     group,
		ModelName: strings.TrimSpace(modelName),
		ChannelID: channelID,
		Route:     route.Normalized(),
	}
	decision.RouteKey = decision.Route.Fingerprint
	cfg := getChannelHealthConfig()
	if !cfg.Enabled {
		return decision
	}
	if _, _, _, ok := channelHealthScope(decision.Group, decision.ModelName, decision.ChannelID); !ok {
		return decision
	}
	// A multi-key channel is selected before SetupContextForSelectedChannel
	// chooses its concrete credential. If that choice changes the route key,
	// release any probe reserved for the pre-key identity before evaluating the
	// credential-specific gate.
	if state := channelHealthRouting(c); state != nil && state.Probe != nil &&
		state.Probe.ChannelID == decision.ChannelID && state.Probe.RouteKey != decision.RouteKey {
		ReleaseChannelHealthProbeForChannel(c, decision.ChannelID)
	}
	if probe := currentChannelHealthProbe(c, decision); probe != nil {
		decision.Action = ChannelHealthSelectionHalfOpenProbe
		decision.probeToken = probe.Token
		decision.Route = probe.Route
		decision.RouteKey = probe.RouteKey
		return decision
	}
	rdb, ok := channelHealthRedisClient(c)
	if !ok {
		decision.Action = ChannelHealthSelectionUnavailable
		return decision
	}

	ctx, cancel := context.WithTimeout(context.Background(), channelHealthRedisTimeout)
	decision = evaluateChannelHealthRedisSelection(c, ctx, rdb, cfg, decision)
	cancel()
	if !includeCredential || !decision.Allow || decision.Route.CredentialScoped || decision.Action != ChannelHealthSelectionHealthy {
		return decision
	}
	credentialRoute := channelHealthCredentialRoute(c, decision.Route)
	if credentialRoute.Fingerprint == decision.Route.Fingerprint {
		return decision
	}
	credentialDecision := decision
	credentialDecision.Route = credentialRoute
	credentialDecision.RouteKey = credentialRoute.Fingerprint
	credentialCtx, credentialCancel := context.WithTimeout(context.Background(), channelHealthRedisTimeout)
	defer credentialCancel()
	return evaluateChannelHealthRedisSelection(c, credentialCtx, rdb, cfg, credentialDecision)
}

func currentChannelHealthProbe(c *gin.Context, decision ChannelHealthSelection) *channelHealthProbe {
	state := channelHealthRouting(c)
	if state == nil || state.Probe == nil ||
		state.Probe.Group != decision.Group ||
		state.Probe.ModelName != decision.ModelName ||
		state.Probe.ChannelID != decision.ChannelID ||
		(state.Probe.RouteKey != "" && state.Probe.RouteKey != decision.RouteKey) {
		return nil
	}
	return state.Probe
}

func evaluateChannelHealthRedisSelection(
	c *gin.Context,
	ctx context.Context,
	rdb *redis.Client,
	cfg channelHealthConfig,
	decision ChannelHealthSelection,
) ChannelHealthSelection {
	cooldownKey, historyKey, probeKey, _ := channelHealthKeysForRoute(decision.Group, decision.Route)
	return evaluateChannelHealthHalfOpenSelection(c, ctx, rdb, channelHealthHalfOpenInput{
		Config:      cfg,
		Decision:    decision,
		CooldownKey: cooldownKey,
		ProbeKey:    probeKey,
		HistoryKey:  historyKey,
	})
}

func evaluateChannelHealthHalfOpenSelection(
	c *gin.Context,
	ctx context.Context,
	rdb *redis.Client,
	input channelHealthHalfOpenInput,
) ChannelHealthSelection {
	decision := input.Decision
	token := newChannelHealthProbeToken()
	reserved, historyPresent, cooldownPresent, err := reserveChannelHealthProbe(ctx, rdb, channelHealthProbeReservation{
		ProbeKey:    input.ProbeKey,
		CooldownKey: input.CooldownKey,
		HistoryKey:  input.HistoryKey,
		MetadataKey: channelHealthMetadataKeyForRoute(decision.Group, decision.Route),
		Token:       token,
		Digest:      channelHealthDigestForRoute(decision.Group, decision.Route),
		Lease:       input.Config.ProbeLease,
	})
	if err != nil {
		common.SysError(fmt.Sprintf("channel health half-open reservation failed: channel_id=%d error=%v", decision.ChannelID, err))
		markChannelHealthUnavailable(c)
		decision.Action = ChannelHealthSelectionUnavailable
		return decision
	}
	if cooldownPresent {
		decision.Allow = false
		decision.Action = ChannelHealthSelectionCooldown
		recordChannelHealthEventForRoute(c, decision.Group, decision.ModelName, decision.ChannelID, decision.Route, decision.Action, 0, "")
		return decision
	}
	if !historyPresent {
		selectedChannelHealthRoute(c, decision.Route)
		recordChannelHealthEventForRoute(c, decision.Group, decision.ModelName, decision.ChannelID, decision.Route, decision.Action, 0, "")
		return decision
	}
	if !reserved {
		decision.Allow = false
		decision.Action = ChannelHealthSelectionHalfOpenBusy
		recordChannelHealthEventForRoute(c, decision.Group, decision.ModelName, decision.ChannelID, decision.Route, decision.Action, 0, "")
		return decision
	}

	decision.Action = ChannelHealthSelectionHalfOpenProbe
	decision.probeToken = token
	state := channelHealthRouting(c)
	if state != nil {
		state.Probe = &channelHealthProbe{
			Group:     decision.Group,
			ModelName: decision.ModelName,
			ChannelID: decision.ChannelID,
			Route:     decision.Route,
			RouteKey:  decision.RouteKey,
			Token:     token,
		}
	}
	selectedChannelHealthRoute(c, decision.Route)
	startChannelHealthProbeLeaseRenewal(c)
	recordChannelHealthEventForRoute(c, decision.Group, decision.ModelName, decision.ChannelID, decision.Route, decision.Action, 0, "")
	recordChannelHealthRuntimeEventForRoute(c, decision.Group, decision.ModelName, decision.ChannelID, decision.Route, decision.Action, 0, "")
	common.SysLog(fmt.Sprintf("channel health half-open probe selected: group=%s model=%s channel_id=%d", decision.Group, decision.ModelName, decision.ChannelID))
	return decision
}

// EvaluateChannelHealthSelection determines whether a selected channel may receive this request.
// Redis failures deliberately fail open so a cache outage does not block traffic.
func EvaluateChannelHealthSelection(c *gin.Context, group string, modelName string, channelID int) ChannelHealthSelection {
	return evaluateChannelHealthSelection(c, group, modelName, channelID)
}

func upstreamStatusCode(err *types.NewAPIError) int {
	if err == nil {
		return 0
	}
	if err.OriginalStatusCode != 0 {
		return err.OriginalStatusCode
	}
	return err.StatusCode
}

func isUpstreamConnectionTimeout(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"context deadline exceeded",
		"i/o timeout",
		"timeout awaiting response headers",
		"connection refused",
		"connection timed out",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func isUpstreamEOFOrReset(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || err.GetErrorCode() == types.ErrorCodeUpstreamTransportInterrupted {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "server sent goaway")
}

// ClassifyChannelHealthFailure is deliberately independent from status-code
// mapping. It returns separate actions for health state, request retry and
// permanent auto-ban decisions.
func ClassifyChannelHealthFailure(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) ChannelHealthFailureDecision {
	decision := ChannelHealthFailureDecision{
		Class:         ChannelHealthFailureClassIgnore,
		HealthAction:  "ignore",
		RequestRetry:  "retry",
		AutoBanAction: "allow",
		Scope:         "none",
		Cooldown:      getChannelHealthConfig().Cooldown,
	}
	if err == nil || types.IsChannelError(err) {
		return decision
	}
	if info != nil && info.IsChannelTest {
		decision.Class = channelHealthFailureAdminTest
		decision.Scope = "admin_test"
		return decision
	}
	if c != nil {
		if _, specified := c.Get("specific_channel_id"); specified {
			decision.Scope = "explicit_channel"
			return decision
		}
	}
	if info != nil && info.StreamStatus != nil && info.StreamStatus.IsCanceled() {
		decision.Class = ChannelHealthFailureClassCanceled
		decision.Scope = "client_cancel"
		return decision
	}
	openAIError := err.ToOpenAIError()
	if types.IsOpenAIRequestParameterError(&openAIError) ||
		upstreamStatusCode(err) == http.StatusBadRequest ||
		upstreamStatusCode(err) == http.StatusRequestEntityTooLarge ||
		upstreamStatusCode(err) == http.StatusUnprocessableEntity {
		decision.Class = ChannelHealthFailureClassParameter
		decision.Scope = "request"
		return decision
	}
	if err.GetErrorCode() == types.ErrorCodeChannelModelMappedError {
		decision.Class = ChannelHealthFailureClassProtocol
		decision.Scope = "protocol"
		return decision
	}

	statusCode := upstreamStatusCode(err)
	if c != nil && common.GetContextKeyBool(c, constant.ContextKeyResponsesRemoteCompactFirstDataTimeout) {
		decision.Class = ChannelHealthFailureClassTimeout
		decision.HealthAction = "cooldown"
		decision.RequestRetry = "exclude"
		decision.AutoBanAction = "suppress"
		decision.Scope = "first_data_timeout"
		decision.Immediate = true
		return decision
	}
	if statusCode == http.StatusGatewayTimeout || statusCode == 524 {
		decision.Class = ChannelHealthFailureClassTimeout
		decision.HealthAction = "cooldown"
		decision.RequestRetry = "exclude"
		decision.AutoBanAction = "suppress"
		decision.Scope = "gateway_timeout"
		decision.Immediate = true
		return decision
	}
	if isChannelCredentialUnavailable(err) {
		decision.Class = ChannelHealthFailureClassAuth
		decision.HealthAction = "cooldown"
		// Keep the failed route out of the current request while allowing the
		// retry gate to continue with another credential or channel. The
		// cooldown independently protects subsequent requests.
		decision.RequestRetry = "exclude"
		decision.AutoBanAction = "suppress"
		decision.Scope = "credential"
		decision.Cooldown = channelHealthAuthCooldown
		decision.Immediate = true
		return decision
	}
	if statusCode == http.StatusTooManyRequests {
		decision.Class = ChannelHealthFailureClassRateLimit
		decision.HealthAction = "cooldown"
		decision.AutoBanAction = "suppress"
		decision.Scope = "rate_limit"
		decision.Cooldown = channelHealthRateLimitFallback
		if err.RetryAfterSeconds > 0 {
			decision.Cooldown = time.Duration(err.RetryAfterSeconds) * time.Second
		}
		decision.Immediate = true
		return decision
	}
	if isUpstreamConnectionTimeout(err) {
		decision.Class = ChannelHealthFailureClassTimeout
		decision.HealthAction = "cooldown"
		decision.RequestRetry = "exclude"
		decision.AutoBanAction = "suppress"
		decision.Scope = "transport_timeout"
		decision.Immediate = true
		return decision
	}
	if statusCode == http.StatusInternalServerError || statusCode == http.StatusBadGateway || statusCode == http.StatusServiceUnavailable || isUpstreamEOFOrReset(err) {
		decision.Class = ChannelHealthFailureClassGateway
		decision.HealthAction = "cooldown"
		decision.AutoBanAction = "suppress"
		decision.Scope = "upstream_transient"
		return decision
	}
	return decision
}

func isChannelHealthFailure(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	return getChannelHealthConfig().Enabled && ClassifyChannelHealthFailure(c, info, err).HealthAction != "ignore"
}

// ShouldRetryAfterChannelHealthAuthenticationFailure permits the current
// request to move to another channel or credential after an authentication
// provider has no usable key. The health cooldown remains scoped to the
// failed credential (or channel when no key index is available).
func ShouldRetryAfterChannelHealthAuthenticationFailure(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	decision := ClassifyChannelHealthFailure(nil, nil, err)
	return decision.Class == ChannelHealthFailureClassAuth
}

func isChannelCredentialUnavailable(err *types.NewAPIError) bool {
	if err == nil {
		return false
	}
	statusCode := err.StatusCode
	if err.OriginalStatusCode != 0 {
		statusCode = err.OriginalStatusCode
	}
	// Credential-provider failures may surface as 503, while a gateway 502
	// containing the same words is still an upstream transport failure.
	if statusCode != http.StatusUnauthorized && statusCode != http.StatusForbidden && statusCode != http.StatusServiceUnavailable {
		return false
	}
	message := strings.ToLower(strings.Join(strings.Fields(err.Error()), " "))
	if strings.Contains(message, "auth_not_found") && strings.Contains(message, "no auth available") {
		return true
	}
	if strings.Contains(message, "no auth available") || strings.Contains(message, "credential unavailable") {
		return true
	}
	if (statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden) &&
		(strings.Contains(message, "invalid api key") || strings.Contains(message, "authentication failed") || strings.Contains(message, "invalid credential")) {
		return true
	}
	return false
}

// ShouldExcludeChannelForRetry identifies failures that should not immediately
// select the same channel again in the current request.
func ShouldExcludeChannelForRetry(err *types.NewAPIError) bool {
	decision := ClassifyChannelHealthFailure(nil, nil, err)
	return decision.RequestRetry == "exclude"
}

// ShouldSuppressChannelAutoBanForHealth prevents transient gateway failures
// from being converted into permanent channel disablement by a broad 5xx rule.
func ShouldSuppressChannelAutoBanForHealth(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) bool {
	return getChannelHealthConfig().Enabled && ClassifyChannelHealthFailure(c, info, err).AutoBanAction == "suppress"
}

// ObserveChannelHealthFailure records only final upstream transport or gateway failures.
func ObserveChannelHealthFailure(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) {
	decision := ClassifyChannelHealthFailure(c, info, err)
	if decision.HealthAction == "ignore" || info == nil || info.ChannelMeta == nil {
		return
	}
	cfg := getChannelHealthConfig()
	if !cfg.Enabled {
		return
	}
	record, ok := newChannelHealthFailureRecord(c, info, err)
	if !ok {
		return
	}
	var probe *channelHealthProbe
	if state := channelHealthRouting(c); state != nil && state.Probe != nil &&
		state.Probe.ChannelID == record.ChannelID && state.Probe.Group == record.Group &&
		state.Probe.ModelName == record.ModelName {
		probeCopy := *state.Probe
		probe = &probeCopy
		if channelHealthRouteMatchesProbe(record.Route, probe.Route) {
			record.ProbeToken = probe.Token
			record.Decision.Immediate = true
			record.Decision.HealthAction = "cooldown"
		} else if decision.Class != ChannelHealthFailureClassAuth && channelHealthProbeRouteCompatible(record.Route, probe.Route) {
			// The adaptor may change protocol/path after selection (for example
			// Responses -> Chat). A failed half-open attempt must still cool the
			// selected route immediately, but its token belongs to the original
			// route and is written below as an alias.
			record.Decision.Immediate = true
			record.Decision.HealthAction = "cooldown"
		}
	}
	rdb, ok := channelHealthRedisClient(c)
	if !ok {
		if state := channelHealthRouting(c); state != nil && state.Probe != nil && state.Probe.ChannelID == record.ChannelID {
			clearLocalChannelHealthProbe(state, state.Probe.Token)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), channelHealthRedisTimeout)
	defer cancel()
	triggered, storeErr := storeChannelHealthFailure(ctx, rdb, cfg, record)
	record.Triggered = triggered
	if storeErr != nil {
		common.SysError(fmt.Sprintf("channel health cooldown update failed: channel_id=%d error=%v", record.ChannelID, storeErr))
		markChannelHealthUnavailable(c)
		if state := channelHealthRouting(c); state != nil && state.Probe != nil && state.Probe.ChannelID == record.ChannelID {
			clearLocalChannelHealthProbe(state, state.Probe.Token)
		}
		return
	}
	// Selection occurs before an adaptor may finalize a protocol conversion and
	// upstream path (for example Responses -> Chat). Preserve the same cooldown
	// under the pre-dispatch route as an alias so the next request cannot select
	// the failed channel through the older route identity.
	if decision.Class != ChannelHealthFailureClassAuth {
		if state := channelHealthRouting(c); state != nil && state.Routes != nil {
			if selectedRoute, exists := state.Routes[record.ChannelID]; exists {
				selectedRoute = selectedRoute.Normalized()
				if selectedRoute.Fingerprint != record.Route.Fingerprint {
					alias := record
					alias.Route = channelHealthBaseRoute(selectedRoute)
					alias.RouteKey = alias.Route.Fingerprint
					alias.ProbeToken = ""
					if probe != nil && channelHealthProbeRouteCompatible(record.Route, probe.Route) && channelHealthRouteMatchesProbe(alias.Route, probe.Route) {
						alias.ProbeToken = probe.Token
						alias.Decision.Immediate = true
					}
					aliasCtx, aliasCancel := context.WithTimeout(context.Background(), channelHealthRedisTimeout)
					_, aliasErr := storeChannelHealthFailure(aliasCtx, rdb, cfg, alias)
					aliasCancel()
					if aliasErr != nil {
						common.SysError(fmt.Sprintf("channel health route alias update failed: channel_id=%d error=%v", record.ChannelID, aliasErr))
					}
				}
			}
		}
	}

	state := channelHealthRouting(c)
	if state != nil {
		probeCredentialMismatch := state.Probe != nil && record.Decision.Class == ChannelHealthFailureClassAuth &&
			(record.Route.CredentialScoped != state.Probe.Route.CredentialScoped ||
				(record.Route.CredentialScoped && record.Route.MultiKeyIndex != state.Probe.Route.MultiKeyIndex))
		if state.Probe != nil && (!channelHealthProbeRouteCompatible(record.Route, state.Probe.Route) || probeCredentialMismatch) {
			// Authentication failures may be recorded under a credential-specific
			// key while the probe itself was reserved under the channel-wide
			// transport key. Release the original lease explicitly.
			ReleaseChannelHealthProbe(c, info)
		}
		if state.Probe != nil {
			stopChannelHealthProbeLeaseRenewal(state, state.Probe.Token)
		}
		state.Probe = nil
	}
	action := ChannelHealthSelectionAction("health_accumulated")
	if triggered {
		action = ChannelHealthSelectionCooldown
	}
	recordChannelHealthEventForRoute(c, record.Group, record.ModelName, record.ChannelID, record.Route, action, record.StatusCode, record.ErrorCode)
	recordChannelHealthRuntimeEventForRoute(c, record.Group, record.ModelName, record.ChannelID, record.Route, action, record.StatusCode, record.ErrorCode)
	common.SysLog(fmt.Sprintf("channel health state updated: group=%s model=%s effective_model=%s protocol=%s path=%s channel_id=%d status_code=%d error_code=%s class=%s action=%s cooldown_seconds=%d", record.Group, record.ModelName, record.Route.EffectiveUpstreamModel, record.Route.FinalProtocol, record.Route.UpstreamPath, record.ChannelID, record.StatusCode, record.ErrorCode, record.Decision.Class, action, int(record.Cooldown.Seconds())))
}

func newChannelHealthFailureRecord(c *gin.Context, info *relaycommon.RelayInfo, err *types.NewAPIError) (channelHealthFailureRecord, bool) {
	decision := ClassifyChannelHealthFailure(c, info, err)
	route := channelHealthActualRouteForInfo(c, info)
	if decision.Class != ChannelHealthFailureClassAuth {
		route = channelHealthBaseRoute(route)
	}
	if route.EffectiveUpstreamModel == "" {
		route.EffectiveUpstreamModel = strings.TrimSpace(info.OriginModelName)
		route = route.Normalized()
	}
	record := channelHealthFailureRecord{
		Group:      effectiveChannelHealthGroup(c, info.UsingGroup),
		ModelName:  strings.TrimSpace(info.OriginModelName),
		ChannelID:  info.ChannelMeta.ChannelId,
		Route:      route,
		RouteKey:   route.Fingerprint,
		StatusCode: err.StatusCode,
		ErrorCode:  err.GetErrorCode(),
		Kind:       channelHealthFailureTransport,
		Cooldown:   decision.Cooldown,
		Decision:   decision,
	}
	if err.OriginalStatusCode != 0 {
		record.StatusCode = err.OriginalStatusCode
	}
	if decision.Class == ChannelHealthFailureClassAuth {
		record.Kind = channelHealthFailureAuth
		record.Cooldown = channelHealthAuthCooldown
	} else if decision.Class == ChannelHealthFailureClassRateLimit {
		record.Kind = channelHealthFailureRateLimit
		record.Cooldown = channelHealthRateLimitFallback
		if err.RetryAfterSeconds > 0 {
			record.Cooldown = time.Duration(err.RetryAfterSeconds) * time.Second
		}
	}
	if record.Cooldown < time.Second {
		record.Cooldown = time.Second
	}
	maxCooldown := time.Duration(operation_setting.MaxChannelHealthCooldownSeconds) * time.Second
	if record.Cooldown > maxCooldown {
		record.Cooldown = maxCooldown
	}
	_, _, _, ok := channelHealthScope(record.Group, record.ModelName, record.ChannelID)
	if record.Route.ChannelID <= 0 {
		ok = false
	}
	return record, ok
}

const recordChannelHealthFailureScript = `
local immediate = ARGV[1] == "1"
local threshold = tonumber(ARGV[2])
local window_ms = tonumber(ARGV[3])
local cooldown_ms = tonumber(ARGV[4])
local state_ttl_ms = tonumber(ARGV[5])
local metadata = ARGV[6]
local digest = ARGV[7]
local probe_token = ARGV[8]
local count = tonumber(redis.call("GET", KEYS[6]) or "0")
if immediate then
  count = threshold
else
  if count <= 0 then
    count = 1
    redis.call("SET", KEYS[6], count, "PX", window_ms)
  else
    count = redis.call("INCR", KEYS[6])
  end
end
local triggered = immediate or count >= threshold
if triggered then
  redis.call("SET", KEYS[2], "1", "PX", cooldown_ms)
  redis.call("SET", KEYS[3], "1", "PX", state_ttl_ms)
  redis.call("SET", KEYS[4], metadata, "PX", state_ttl_ms)
  redis.call("DEL", KEYS[6])
  if probe_token ~= "" and redis.call("GET", KEYS[1]) == probe_token then
    redis.call("DEL", KEYS[1])
  end
	  local redis_time = redis.call("TIME")
	  local now_ms = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
	  local runtime_expiry_unix = math.floor((now_ms + state_ttl_ms + 999) / 1000)
  redis.call("ZADD", KEYS[5], runtime_expiry_unix, digest)
else
  redis.call("PEXPIRE", KEYS[6], window_ms)
end
return {count, triggered and 1 or 0}
`

func storeChannelHealthFailure(
	ctx context.Context,
	rdb *redis.Client,
	cfg channelHealthConfig,
	record channelHealthFailureRecord,
) (bool, error) {
	metadataBytes, err := common.Marshal(channelHealthRuntimeMetadata{
		Group:          record.Group,
		ModelName:      record.ModelName,
		ChannelID:      record.ChannelID,
		RouteKey:       record.RouteKey,
		EffectiveModel: record.Route.EffectiveUpstreamModel,
		Protocol:       record.Route.FinalProtocol,
		Operation:      record.Route.Operation,
		UpstreamPath:   record.Route.UpstreamPath,
		MultiKeyIndex:  record.Route.MultiKeyIndex,
		StatusCode:     record.StatusCode,
		ErrorCode:      string(record.ErrorCode),
		Kind:           record.Kind,
		Cooldown:       int(record.Cooldown.Seconds()),
	})
	if err != nil {
		return false, fmt.Errorf("marshal metadata: %w", err)
	}
	cooldownKey, historyKey, probeKey, failuresKey := channelHealthKeysForRoute(record.Group, record.Route)
	metadataKey := channelHealthMetadataKeyForRoute(record.Group, record.Route)
	digest := channelHealthDigestForRoute(record.Group, record.Route)
	cooldown := record.Cooldown
	if cooldown <= 0 {
		cooldown = cfg.Cooldown
	}
	stateTTL := cooldown + cfg.ProbeLease*channelHealthProbeHistoryLeases
	values, err := rdb.Eval(ctx, recordChannelHealthFailureScript, []string{
		probeKey,
		cooldownKey,
		historyKey,
		metadataKey,
		channelHealthRuntimeIndexKey,
		failuresKey,
	},
		boolToRedisArg(record.Decision.Immediate),
		channelHealthFailureThreshold,
		channelHealthFailureWindow.Milliseconds(),
		cooldown.Milliseconds(),
		stateTTL.Milliseconds(),
		string(metadataBytes),
		digest,
		record.ProbeToken,
	).Int64Slice()
	if err != nil {
		return false, err
	}
	return len(values) > 1 && values[1] == 1, nil
}

func boolToRedisArg(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func MarkChannelHealthAffinityBypass(c *gin.Context, group string, modelName string, channelID int) {
	group = effectiveChannelHealthGroup(c, group)
	group, modelName, channelID, ok := channelHealthScope(group, modelName, channelID)
	if !ok {
		return
	}
	recordChannelHealthEvent(c, group, modelName, channelID, "affinity_bypassed", 0, "")
}

func MarkChannelHealthAffinityBypassForChannel(c *gin.Context, group string, modelName string, channel *model.Channel) {
	if channel == nil {
		return
	}
	route, err := BuildChannelHealthRouteForChannel(c, group, modelName, channel)
	if err != nil {
		MarkChannelHealthAffinityBypass(c, group, modelName, channel.Id)
		return
	}
	recordChannelHealthEventForRoute(c, effectiveChannelHealthGroup(c, group), modelName, channel.Id, route, "affinity_bypassed", 0, "")
}

// ExcludeChannelHealthSelection records an in-request exclusion after a
// selected channel is found to be temporarily unavailable.
func ExcludeChannelHealthSelection(c *gin.Context, group string, modelName string, channelID int) bool {
	return markChannelHealthSelectionExclusion(c, group, modelName, channelID)
}

func ExcludeChannelHealthSelectionForRoute(c *gin.Context, group string, modelName string, route relaycommon.ChannelRouteIdentity) bool {
	return markChannelHealthSelectionExclusionForRoute(c, group, modelName, route.ChannelID, route.Fingerprint)
}

// IsChannelHealthSelectionExcludedForRoute reports whether a candidate is
// excluded by the current request's health state for its exact route. Route
// keyed exclusions must not be collapsed to a channel-wide exclusion: the same
// channel may expose a different mapped model, protocol, path, or credential.
func IsChannelHealthSelectionExcludedForRoute(c *gin.Context, group string, modelName string, channel *model.Channel) bool {
	route, err := BuildChannelHealthRouteForChannel(c, group, modelName, channel)
	if err != nil {
		// Invalid mappings are handled by the selector's explicit route
		// validation. Do not turn an unresolvable route into a broad exclusion.
		return false
	}
	return isChannelHealthSelectionExcludedForBuiltRoute(c, group, modelName, channel, route)
}

// IsChannelHealthSelectionExcludedForRouteWithEffectiveModel is the
// mapping-aware candidate-filter variant. The effective model must come from
// the selector's request-scoped mapping snapshot.
func IsChannelHealthSelectionExcludedForRouteWithEffectiveModel(c *gin.Context, group string, modelName string, channel *model.Channel, effectiveModel string) bool {
	route, err := BuildChannelHealthRouteForChannelWithEffectiveModel(c, group, modelName, channel, effectiveModel)
	if err != nil {
		return false
	}
	return isChannelHealthSelectionExcludedForBuiltRoute(c, group, modelName, channel, route)
}

func isChannelHealthSelectionExcludedForBuiltRoute(c *gin.Context, group string, modelName string, channel *model.Channel, route relaycommon.ChannelRouteIdentity) bool {
	if c == nil || channel == nil || channel.Id <= 0 {
		return false
	}
	group = effectiveChannelHealthGroup(c, group)
	group, modelName, channelID, ok := channelHealthScope(group, modelName, channel.Id)
	if !ok {
		return false
	}
	state, ok := common.GetContextKeyType[*channelHealthRoutingState](c, constant.ContextKeyChannelHealthRoutingState)
	if !ok || state == nil || len(state.ExcludedChannels) == 0 {
		return false
	}
	for _, routeKey := range []string{"", route.Fingerprint} {
		scope := channelHealthSelectionScope{Group: group, ModelName: modelName, RouteKey: routeKey}
		excluded := state.ExcludedChannels[scope]
		if _, exists := excluded[channelID]; !exists {
			continue
		}
		if _, retryable := state.RetryableChannels[scope][channelID]; retryable {
			continue
		}
		return true
	}
	return false
}

// MarkChannelHealthSelectionRetryableForRoute records that a rejected
// credential-specific health decision may retry the same multi-key channel
// with another credential. Generic relay failures must not call this helper:
// those failures may be channel-wide protocol or capability errors.
func MarkChannelHealthSelectionRetryableForRoute(c *gin.Context, group string, modelName string, route relaycommon.ChannelRouteIdentity) {
	if route.CredentialScoped {
		markChannelHealthSelectionRetryableForRoute(c, group, modelName, route)
	}
}

func markChannelHealthSelectionExclusion(c *gin.Context, group string, modelName string, channelID int) bool {
	return markChannelHealthSelectionExclusionForRoute(c, group, modelName, channelID, "")
}

func markChannelHealthSelectionExclusionForRoute(c *gin.Context, group string, modelName string, channelID int, routeKey string) bool {
	group = effectiveChannelHealthGroup(c, group)
	group, modelName, channelID, ok := channelHealthScope(group, modelName, channelID)
	if !ok {
		return false
	}
	state := channelHealthRouting(c)
	if state == nil {
		return false
	}
	if state.ExcludedChannels == nil {
		state.ExcludedChannels = make(map[channelHealthSelectionScope]map[int]struct{})
	}
	scope := channelHealthSelectionScope{Group: group, ModelName: modelName, RouteKey: strings.TrimSpace(routeKey)}
	excluded := state.ExcludedChannels[scope]
	if excluded == nil {
		excluded = make(map[int]struct{})
		state.ExcludedChannels[scope] = excluded
	}
	if _, exists := excluded[channelID]; exists {
		return false
	}
	excluded[channelID] = struct{}{}
	return true
}

func markChannelHealthSelectionRetryableForRoute(c *gin.Context, group string, modelName string, route relaycommon.ChannelRouteIdentity) {
	group = effectiveChannelHealthGroup(c, group)
	group, modelName, channelID, ok := channelHealthScope(group, modelName, route.ChannelID)
	if !ok || strings.TrimSpace(route.Fingerprint) == "" {
		return
	}
	state := channelHealthRouting(c)
	if state == nil {
		return
	}
	if state.RetryableChannels == nil {
		state.RetryableChannels = make(map[channelHealthSelectionScope]map[int]struct{})
	}
	scope := channelHealthSelectionScope{Group: group, ModelName: modelName, RouteKey: strings.TrimSpace(route.Fingerprint)}
	retryable := state.RetryableChannels[scope]
	if retryable == nil {
		retryable = make(map[int]struct{})
		state.RetryableChannels[scope] = retryable
	}
	retryable[channelID] = struct{}{}
}

func channelHealthSelectionExclusions(c *gin.Context, group string, modelName string) map[int]struct{} {
	group = effectiveChannelHealthGroup(c, group)
	group, modelName, _, ok := channelHealthScope(group, modelName, 1)
	if !ok || c == nil {
		return nil
	}
	state, ok := common.GetContextKeyType[*channelHealthRoutingState](c, constant.ContextKeyChannelHealthRoutingState)
	if !ok || state == nil || len(state.ExcludedChannels) == 0 {
		return nil
	}
	result := make(map[int]struct{})
	for scope, excluded := range state.ExcludedChannels {
		if scope.Group != group || scope.ModelName != modelName || scope.RouteKey != "" {
			continue
		}
		retryable := state.RetryableChannels[scope]
		for channelID := range excluded {
			if _, canRetry := retryable[channelID]; canRetry {
				continue
			}
			result[channelID] = struct{}{}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func channelHealthSelectionRetryableChannels(c *gin.Context, group string, modelName string) map[int]struct{} {
	group = effectiveChannelHealthGroup(c, group)
	group, modelName, _, ok := channelHealthScope(group, modelName, 1)
	if !ok || c == nil {
		return nil
	}
	state, ok := common.GetContextKeyType[*channelHealthRoutingState](c, constant.ContextKeyChannelHealthRoutingState)
	if !ok || state == nil || len(state.RetryableChannels) == 0 {
		return nil
	}
	result := make(map[int]struct{})
	for scope, retryable := range state.RetryableChannels {
		if scope.Group != group || scope.ModelName != modelName {
			continue
		}
		for channelID := range retryable {
			result[channelID] = struct{}{}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// GetChannelHealthExcludedMultiKeyIndexes returns the ephemeral credential
// indexes excluded for this request. It never mutates the channel or its
// persisted status list.
func GetChannelHealthExcludedMultiKeyIndexes(c *gin.Context, group string, modelName string, channel *model.Channel) map[int]struct{} {
	if channel == nil || !channel.ChannelInfo.IsMultiKey {
		return nil
	}
	state, ok := common.GetContextKeyType[*channelHealthRoutingState](c, constant.ContextKeyChannelHealthRoutingState)
	if !ok || state == nil || len(state.ExcludedChannels) == 0 {
		return nil
	}
	baseRoute, err := BuildChannelHealthRouteForChannel(c, group, modelName, channel)
	if err != nil {
		return nil
	}
	group = effectiveChannelHealthGroup(c, group)
	group, modelName, channelID, ok := channelHealthScope(group, modelName, channel.Id)
	if !ok || channelID != baseRoute.ChannelID {
		return nil
	}
	result := make(map[int]struct{})
	for index := range channel.GetKeys() {
		route := baseRoute
		route.MultiKeyIndex = index
		route.CredentialScoped = true
		route.Fingerprint = ""
		scope := channelHealthSelectionScope{Group: group, ModelName: modelName, RouteKey: route.Normalized().Fingerprint}
		if excluded := state.ExcludedChannels[scope]; excluded != nil {
			if _, exists := excluded[channelID]; exists {
				result[index] = struct{}{}
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func AppendChannelHealthRoutingAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil {
		return
	}
	state, ok := common.GetContextKeyType[*channelHealthRoutingState](c, constant.ContextKeyChannelHealthRoutingState)
	if !ok || state == nil || (len(state.Events) == 0 && !state.RedisUnavailable) {
		return
	}
	info := dto.ChannelHealthRoutingInfo{
		RedisUnavailable: state.RedisUnavailable,
	}
	if len(state.Events) > 0 {
		info.Events = append([]dto.ChannelHealthRoutingEvent(nil), state.Events...)
	}
	adminInfo["channel_health"] = info
}

// HasChannelHealthRoutingAudit reports whether this request made a non-default
// health-routing decision that must remain visible when selection terminates
// before any upstream request is sent.
func HasChannelHealthRoutingAudit(c *gin.Context) bool {
	if c == nil {
		return false
	}
	state, ok := common.GetContextKeyType[*channelHealthRoutingState](c, constant.ContextKeyChannelHealthRoutingState)
	if !ok || state == nil {
		return false
	}
	if state.RedisUnavailable {
		return true
	}
	for _, event := range state.Events {
		if event.Action != string(ChannelHealthSelectionHealthy) {
			return true
		}
	}
	return false
}
