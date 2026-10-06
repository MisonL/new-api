package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/tidwall/gjson"
)

// ThinkingContentInfo tracks streaming state for reasoning/thinking content emission.
type ThinkingContentInfo struct {
	IsFirstThinkingContent  bool
	SendLastThinkingContent bool
	HasSentThinkingContent  bool
}

const (
	LastMessageTypeNone     = "none"
	LastMessageTypeText     = "text"
	LastMessageTypeTools    = "tools"
	LastMessageTypeThinking = "thinking"
)

// ClaudeConvertInfo stores incremental conversion state for Claude-compatible streaming.
type ClaudeConvertInfo struct {
	LastMessagesType string
	Index            int
	Usage            *dto.Usage
	FinishReason     string
	Done             bool

	ToolCallBaseIndex      int
	ToolCallMaxIndexOffset int
	ToolCallStarted        map[int]bool
	ToolCallIDs            map[int]string
	ToolCallNames          map[int]string
	ToolCallPendingArgs    map[int]string
}

// GeminiConvertInfo stores incremental state while an OpenAI-compatible
// upstream stream is converted to the Gemini GenerateContent stream format.
// OpenAI tool-call arguments are commonly split across several chunks and the
// Gemini functionCall.args field must be a complete JSON object, so conversion
// needs request-scoped buffering rather than validating each chunk in isolation.
type GeminiConvertInfo struct {
	ToolCalls map[int]map[int]*GeminiToolCallState
}

// GeminiToolCallState is the accumulated identity and argument payload for one
// streamed OpenAI tool call. The maps are keyed first by choice index and then
// by the OpenAI tool-call index.
type GeminiToolCallState struct {
	ID          string
	Name        string
	PendingArgs string
	Emitted     bool
}

// RerankerInfo stores rerank request details needed during downstream processing.
type RerankerInfo struct {
	Documents       []any
	ReturnDocuments bool
}

// BuildInToolInfo records usage counters for a Responses built-in tool.
type BuildInToolInfo struct {
	ToolName          string
	CallCount         int
	SearchContextSize string
	Quality           string
	Size              string
	ImageCalls        map[string]int
	DefaultSearchSize bool `json:"-"`
}

// ResponsesUsageInfo aggregates built-in tool usage collected from Responses requests.
type ResponsesUsageInfo struct {
	BuiltInTools map[string]*BuildInToolInfo
}

func ImageGenerationCallKey(quality string, size string) string {
	return strings.TrimSpace(quality) + ":" + strings.TrimSpace(size)
}

func SplitImageGenerationCallKey(key string) (string, string) {
	quality, size, ok := strings.Cut(key, ":")
	if !ok {
		return key, ""
	}
	return quality, size
}

// ChannelMeta captures selected channel metadata copied from request context.
type ChannelMeta struct {
	ChannelType          int
	ChannelId            int
	ChannelIsMultiKey    bool
	ChannelMultiKeyIndex int
	ChannelBaseUrl       string
	ApiType              int
	ApiVersion           string
	ApiKey               string
	Organization         string
	ChannelCreateTime    int64
	ParamOverride        map[string]interface{}
	HeadersOverride      map[string]interface{}
	ChannelSetting       dto.ChannelSettings
	ChannelOtherSettings dto.ChannelOtherSettings
	// ChannelOther and StatusCodeMapping are captured separately from the typed
	// settings because provider adaptors use them as raw runtime parameters.
	// They are frozen into asynchronous task route snapshots as well.
	ChannelOther         string
	StatusCodeMapping    string
	UpstreamModelName    string
	IsModelMapped        bool
	SupportStreamOptions bool // 是否支持流式选项
}

// TokenCountMeta stores token counting metadata derived during relay setup.
type TokenCountMeta struct {
	//promptTokens int
	estimatePromptTokens int
}

// RelayInfo carries request-scoped relay metadata, billing state, and conversion context.
type RelayInfo struct {
	TokenId           int
	TokenKey          string
	TokenGroup        string
	UserId            int
	UsingGroup        string // 使用的分组，当auto跨分组重试时，会变动
	UserGroup         string // 用户所在分组
	TokenUnlimited    bool
	StartTime         time.Time
	FirstResponseTime time.Time
	isFirstResponse   bool
	//SendLastReasoningResponse bool
	IsStream                            bool
	ResponsesBootstrapRecoveryAttempted bool
	ResponsesBootstrapRecoveryWaitMs    int64
	IsGeminiBatchEmbedding              bool
	IsPlayground                        bool
	UsePrice                            bool
	RelayMode                           int
	OriginModelName                     string
	RequestURLPath                      string
	RequestHeaders                      map[string]string
	ShouldIncludeUsage                  bool
	DisablePing                         bool // 是否禁止向下游发送自定义 Ping
	ClientWs                            *websocket.Conn
	TargetWs                            *websocket.Conn
	InputAudioFormat                    string
	OutputAudioFormat                   string
	RealtimeTools                       []dto.RealTimeTool
	IsFirstRequest                      bool
	AudioUsage                          bool
	ReasoningEffort                     string
	UserSetting                         dto.UserSetting
	UserEmail                           string
	UserQuota                           int
	RelayFormat                         types.RelayFormat
	SendResponseCount                   int
	ReceivedResponseCount               int
	FinalPreConsumedQuota               int // 最终预消耗的配额
	// ForcePreConsume 为 true 时禁用 BillingSession 的信任额度旁路，
	// 强制预扣全额。用于异步任务（视频/音乐生成等），因为请求返回后任务仍在运行，
	// 必须在提交前锁定全额。
	ForcePreConsume bool
	// Billing 是计费会话，封装了预扣费/结算/退款的统一生命周期。
	// 免费模型时为 nil。
	Billing BillingSettler
	// BillingSource indicates whether this request is billed from wallet quota or subscription.
	// "" or "wallet" => wallet; "subscription" => subscription
	BillingSource string
	// SubscriptionId is the user_subscriptions.id used when BillingSource == "subscription"
	SubscriptionId int
	// SubscriptionPreConsumed is the amount pre-consumed on subscription item (quota units or 1)
	SubscriptionPreConsumed int64
	// SubscriptionPostDelta is the post-consume delta applied to amount_used (quota units; can be negative).
	SubscriptionPostDelta int64
	// SubscriptionPlanId / SubscriptionPlanTitle are used for logging/UI display.
	SubscriptionPlanId    int
	SubscriptionPlanTitle string
	// RequestId is used for idempotent pre-consume/refund
	RequestId string
	// UpstreamRequestId is the request ID returned by the upstream provider.
	UpstreamRequestId     string
	UpstreamRequestPath   string
	UpstreamUsageMetadata string
	UpstreamRequestStart  time.Time
	UpstreamHeaderTime    time.Time
	UpstreamFirstByteTime time.Time
	UpstreamEndTime       time.Time
	// SubscriptionAmountTotal / SubscriptionAmountUsedAfterPreConsume are used to compute remaining in logs.
	SubscriptionAmountTotal               int64
	SubscriptionAmountUsedAfterPreConsume int64
	IsClaudeBetaQuery                     bool // /v1/messages?beta=true
	IsChannelTest                         bool // channel test request
	RetryIndex                            int
	LastError                             *types.NewAPIError
	RuntimeHeadersOverride                map[string]interface{}
	RuntimePassedRequestHeaders           map[string]string
	// ChannelHealthRoute is immutable for one channel attempt. It is populated
	// before dispatch and reused when recording a later upstream failure.
	ChannelHealthRoute        *ChannelRouteIdentity
	UseRuntimeHeadersOverride bool
	ParamOverrideAudit        []string
	// UpstreamRequestBodySize retains the marshaled upstream body size when a
	// type-erased BodyStorage reader is used, so HTTP requests keep an explicit
	// Content-Length instead of silently falling back to chunked transfer.
	UpstreamRequestBodySize int64
	// UpstreamRequestGetBody returns a fresh reader for transport-level replay
	// when the current request body is backed by BodyStorage.
	UpstreamRequestGetBody func() (io.ReadCloser, error)
	// CodexFoldedBaseRequestBody is the exact JSON body sent by the first
	// Codex Responses round when folded SSE is enabled. It is request-scoped
	// and is used only to build continuation rounds without losing opaque fields.
	CodexFoldedBaseRequestBody []byte

	PriceData types.PriceData

	// TieredBillingSnapshot is a frozen snapshot of tiered billing rules
	// captured at pre-consume time. Non-nil only when billing mode is "tiered_expr".
	TieredBillingSnapshot *billingexpr.BillingSnapshot
	BillingRequestInput   *billingexpr.RequestInput

	Request dto.Request

	// RequestConversionChain records request format conversions in order, e.g.
	// ["openai", "openai_responses"] or ["openai", "claude"].
	RequestConversionChain []types.RelayFormat
	// 最终请求到上游的格式。可由 adaptor 显式设置；
	// 若为空，调用 GetFinalRequestRelayFormat 会回退到 RequestConversionChain 的最后一项或 RelayFormat。
	FinalRequestRelayFormat types.RelayFormat

	StreamStatus               *StreamStatus
	ResponsesStreamObservation *ResponsesStreamObservation

	// ResponsesStreamAttemptStarted tracks whether an upstream Responses relay
	// has already been started for this request. It is independent of
	// RetryIndex because auto-group selection and compact fallbacks may reset
	// the retry counter while still issuing another upstream request.
	ResponsesStreamAttemptStarted bool

	ThinkingContentInfo
	TokenCountMeta
	*ClaudeConvertInfo
	*GeminiConvertInfo
	*RerankerInfo
	*ResponsesUsageInfo
	*ChannelMeta
	*TaskRelayInfo
}

func (info *RelayInfo) SyntheticCompactScope() types.SyntheticCompactStateScope {
	if info == nil {
		return types.SyntheticCompactStateScope{}
	}
	group := strings.TrimSpace(info.UsingGroup)
	if group == "" {
		group = strings.TrimSpace(info.TokenGroup)
	}
	scope := types.SyntheticCompactStateScope{
		UserID:  info.UserId,
		TokenID: info.TokenId,
		Group:   group,
		Model:   strings.TrimSpace(info.OriginModelName),
	}
	if info.ChannelMeta != nil {
		scope.ChannelID = info.ChannelId
		scope.ChannelType = info.ChannelType
	}
	return scope
}

func ShouldStripCodexEncryptedContext(info *RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || !info.ChannelOtherSettings.ShouldStripResponsesEncryptedReasoning() {
		return false
	}
	if info.RelayMode == relayconstant.RelayModeResponses {
		return true
	}
	return info.RelayMode == relayconstant.RelayModeResponsesCompact &&
		(info.ChannelType == constant.ChannelTypeOpenAI || info.ChannelType == constant.ChannelTypeAzure)
}

func ShouldConvertResponsesRequest(info *RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	if ShouldStripCodexEncryptedContext(info) {
		return true
	}
	return IsSyntheticOpenAICompatibleResponsesCompact(info)
}

func IsOpenAICompatibleResponsesCompact(info *RelayInfo) bool {
	return info != nil &&
		info.ChannelMeta != nil &&
		info.RelayMode == relayconstant.RelayModeResponsesCompact &&
		info.ChannelType == constant.ChannelTypeOpenAI
}

func IsOpenAICompatibleResponses(info *RelayInfo) bool {
	return info != nil &&
		info.ChannelMeta != nil &&
		(info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact) &&
		(info.ChannelType == constant.ChannelTypeOpenAI || info.ChannelType == constant.ChannelTypeAzure)
}

func IsNativeOpenAICompatibleResponsesCompact(info *RelayInfo) bool {
	return IsOpenAICompatibleResponsesCompact(info) &&
		info.ChannelOtherSettings.HasNativeResponsesCompact()
}

func IsSyntheticOpenAICompatibleResponsesCompact(info *RelayInfo) bool {
	return IsOpenAICompatibleResponsesCompact(info) &&
		info.ChannelOtherSettings.HasSyntheticResponsesCompact()
}

func ShouldHandleSyntheticOpenAICompatibleResponses(info *RelayInfo) bool {
	return info != nil &&
		info.ChannelMeta != nil &&
		info.ChannelType == constant.ChannelTypeOpenAI &&
		(info.RelayMode == relayconstant.RelayModeResponses || info.RelayMode == relayconstant.RelayModeResponsesCompact) &&
		info.ChannelOtherSettings.HasSyntheticResponsesCompact()
}

// ResetBillingMetadata refunds the active billing session and clears cached billing fields.
func (info *RelayInfo) ResetBillingMetadata(c *gin.Context) {
	if info == nil {
		return
	}
	if info.Billing != nil {
		info.Billing.Refund(c)
	}
	info.Billing = nil
	info.FinalPreConsumedQuota = 0
	info.BillingSource = ""
	info.SubscriptionId = 0
	info.SubscriptionPreConsumed = 0
	info.SubscriptionPostDelta = 0
	info.SubscriptionPlanId = 0
	info.SubscriptionPlanTitle = ""
	info.SubscriptionAmountTotal = 0
	info.SubscriptionAmountUsedAfterPreConsume = 0
}

func (info *RelayInfo) InitChannelMeta(c *gin.Context) {
	// RelayInfo is reused across channel attempts. Body metadata belongs to the
	// current attempt and must not retain a closed reader or runtime override
	// state from the prior channel.
	info.RuntimeHeadersOverride = nil
	info.RuntimePassedRequestHeaders = nil
	info.ChannelHealthRoute = nil
	info.UseRuntimeHeadersOverride = false
	info.ParamOverrideAudit = nil
	info.UpstreamRequestBodySize = 0
	info.UpstreamRequestGetBody = nil
	info.CodexFoldedBaseRequestBody = nil
	info.ResponsesStreamObservation = NewResponsesStreamObservation()
	channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
	paramOverride := common.GetContextKeyStringMap(c, constant.ContextKeyChannelParamOverride)
	headerOverride := common.GetContextKeyStringMap(c, constant.ContextKeyChannelHeaderOverride)
	apiType, _ := common.ChannelType2APIType(channelType)
	channelMeta := &ChannelMeta{
		ChannelType:          channelType,
		ChannelId:            common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		ChannelIsMultiKey:    common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey),
		ChannelMultiKeyIndex: common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex),
		ChannelBaseUrl:       common.GetContextKeyString(c, constant.ContextKeyChannelBaseUrl),
		ApiType:              apiType,
		ApiVersion:           c.GetString("api_version"),
		ApiKey:               common.GetContextKeyString(c, constant.ContextKeyChannelKey),
		Organization:         c.GetString("channel_organization"),
		ChannelCreateTime:    c.GetInt64("channel_create_time"),
		ParamOverride:        paramOverride,
		HeadersOverride:      headerOverride,
		ChannelOther:         c.GetString("channel_other"),
		StatusCodeMapping:    common.GetContextKeyString(c, constant.ContextKeyChannelStatusCodeMapping),
		UpstreamModelName:    common.GetContextKeyString(c, constant.ContextKeyOriginalModel),
		IsModelMapped:        false,
		SupportStreamOptions: false,
	}

	if channelType == constant.ChannelTypeAzure {
		channelMeta.ApiVersion = GetAPIVersion(c)
	}
	if channelType == constant.ChannelTypeVertexAi {
		channelMeta.ApiVersion = c.GetString("region")
	}

	channelSetting, ok := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	if ok {
		channelMeta.ChannelSetting = channelSetting
	}

	channelOtherSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	if ok {
		channelMeta.ChannelOtherSettings = channelOtherSettings
	}

	if streamSupportedChannels[channelMeta.ChannelType] {
		channelMeta.SupportStreamOptions = true
	}

	info.ChannelMeta = channelMeta
	info.ChannelOtherSettings = channelMeta.ChannelOtherSettings

	// reset some fields based on channel meta
	// 重置某些字段，例如模型名称等
	if info.Request != nil {
		info.Request.SetModelName(info.OriginModelName)
	}
}

func (info *RelayInfo) ToString() string {
	if info == nil {
		return "RelayInfo<nil>"
	}

	// Basic info
	b := &strings.Builder{}
	fmt.Fprintf(b, "RelayInfo{ ")
	fmt.Fprintf(b, "RelayFormat: %s, ", info.RelayFormat)
	fmt.Fprintf(b, "RelayMode: %d, ", info.RelayMode)
	fmt.Fprintf(b, "IsStream: %t, ", info.IsStream)
	fmt.Fprintf(b, "IsPlayground: %t, ", info.IsPlayground)
	fmt.Fprintf(b, "RequestURLPath: %q, ", info.RequestURLPath)
	fmt.Fprintf(b, "UpstreamRequestPath: %q, ", info.UpstreamRequestPath)
	fmt.Fprintf(b, "OriginModelName: %q, ", info.OriginModelName)
	fmt.Fprintf(b, "EstimatePromptTokens: %d, ", info.estimatePromptTokens)
	fmt.Fprintf(b, "ShouldIncludeUsage: %t, ", info.ShouldIncludeUsage)
	fmt.Fprintf(b, "DisablePing: %t, ", info.DisablePing)
	fmt.Fprintf(b, "SendResponseCount: %d, ", info.SendResponseCount)
	fmt.Fprintf(b, "FinalPreConsumedQuota: %d, ", info.FinalPreConsumedQuota)

	// User & token info (mask secrets)
	fmt.Fprintf(b, "User{ Id: %d, Email: %q, Group: %q, UsingGroup: %q, Quota: %d }, ",
		info.UserId, common.MaskEmail(info.UserEmail), info.UserGroup, info.UsingGroup, info.UserQuota)
	fmt.Fprintf(b, "Token{ Id: %d, Unlimited: %t, Key: ***masked*** }, ", info.TokenId, info.TokenUnlimited)

	// Time info
	latencyMs, _ := info.FirstResponseLatencyMs()
	fmt.Fprintf(b, "Timing{ Start: %s, FirstResponse: %s, LatencyMs: %d }, ",
		info.StartTime.Format(time.RFC3339Nano), info.FirstResponseTime.Format(time.RFC3339Nano), latencyMs)

	// Audio / realtime
	if info.InputAudioFormat != "" || info.OutputAudioFormat != "" || len(info.RealtimeTools) > 0 || info.AudioUsage {
		fmt.Fprintf(b, "Realtime{ AudioUsage: %t, InFmt: %q, OutFmt: %q, Tools: %d }, ",
			info.AudioUsage, info.InputAudioFormat, info.OutputAudioFormat, len(info.RealtimeTools))
	}

	// Reasoning
	if info.ReasoningEffort != "" {
		fmt.Fprintf(b, "ReasoningEffort: %q, ", info.ReasoningEffort)
	}

	// Price data (non-sensitive)
	if info.PriceData.UsePrice {
		fmt.Fprintf(b, "PriceData{ %s }, ", info.PriceData.ToSetting())
	}

	// Channel metadata (mask ApiKey)
	if info.ChannelMeta != nil {
		cm := info.ChannelMeta
		fmt.Fprintf(b, "ChannelMeta{ Type: %d, Id: %d, IsMultiKey: %t, MultiKeyIndex: %d, BaseURL: %q, ApiType: %d, ApiVersion: %q, Organization: %q, CreateTime: %d, UpstreamModelName: %q, IsModelMapped: %t, SupportStreamOptions: %t, ApiKey: ***masked*** }, ",
			cm.ChannelType, cm.ChannelId, cm.ChannelIsMultiKey, cm.ChannelMultiKeyIndex, cm.ChannelBaseUrl, cm.ApiType, cm.ApiVersion, cm.Organization, cm.ChannelCreateTime, cm.UpstreamModelName, cm.IsModelMapped, cm.SupportStreamOptions)
	}

	// Responses usage info (non-sensitive)
	if info.ResponsesUsageInfo != nil && len(info.ResponsesUsageInfo.BuiltInTools) > 0 {
		fmt.Fprintf(b, "ResponsesTools{ ")
		first := true
		for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
			if !first {
				fmt.Fprintf(b, ", ")
			}
			first = false
			if tool != nil {
				fmt.Fprintf(b, "%s: calls=%d", name, tool.CallCount)
			} else {
				fmt.Fprintf(b, "%s: calls=0", name)
			}
		}
		fmt.Fprintf(b, " }, ")
	}

	fmt.Fprintf(b, "}")
	return b.String()
}

// 定义支持流式选项的通道类型
var streamSupportedChannels = map[int]bool{
	constant.ChannelTypeOpenAI:      true,
	constant.ChannelTypeAnthropic:   true,
	constant.ChannelTypeAws:         true,
	constant.ChannelTypeGemini:      true,
	constant.ChannelCloudflare:      true,
	constant.ChannelTypeAzure:       true,
	constant.ChannelTypeVolcEngine:  true,
	constant.ChannelTypeXai:         true,
	constant.ChannelTypeDeepSeek:    true,
	constant.ChannelTypeBaiduV2:     true,
	constant.ChannelTypeZhipu_v4:    true,
	constant.ChannelTypeAli:         true,
	constant.ChannelTypeSubmodel:    true,
	constant.ChannelTypeCodex:       true,
	constant.ChannelTypeMoonshot:    true,
	constant.ChannelTypeMiniMax:     true,
	constant.ChannelTypeSiliconFlow: true,
}

// GenRelayInfoWs builds RelayInfo for OpenAI realtime websocket relays.
func GenRelayInfoWs(c *gin.Context, ws *websocket.Conn) *RelayInfo {
	info := genBaseRelayInfo(c, nil)
	info.RelayFormat = types.RelayFormatOpenAIRealtime
	info.ClientWs = ws
	info.InputAudioFormat = "pcm16"
	info.OutputAudioFormat = "pcm16"
	info.IsFirstRequest = true
	return info
}

// GenRelayInfoClaude builds RelayInfo for Claude message relays.
func GenRelayInfoClaude(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatClaude
	info.ShouldIncludeUsage = false
	info.ClaudeConvertInfo = &ClaudeConvertInfo{
		LastMessagesType: LastMessageTypeNone,
	}
	info.IsClaudeBetaQuery = c.Query("beta") == "true" || isClaudeBetaForced(c)
	return info
}

func isClaudeBetaForced(c *gin.Context) bool {
	channelOtherSettings, ok := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	return ok && channelOtherSettings.ClaudeBetaQuery
}

// GenRelayInfoRerank builds RelayInfo for rerank requests.
func GenRelayInfoRerank(c *gin.Context, request *dto.RerankRequest) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayMode = relayconstant.RelayModeRerank
	info.RelayFormat = types.RelayFormatRerank
	info.RerankerInfo = &RerankerInfo{
		Documents:       request.Documents,
		ReturnDocuments: request.GetReturnDocuments(),
	}
	return info
}

// GenRelayInfoOpenAIAudio builds RelayInfo for OpenAI audio endpoints.
func GenRelayInfoOpenAIAudio(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAIAudio
	return info
}

// GenRelayInfoEmbedding builds RelayInfo for embedding requests.
func GenRelayInfoEmbedding(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatEmbedding
	return info
}

// GenRelayInfoResponses builds RelayInfo for OpenAI Responses API requests.
func GenRelayInfoResponses(c *gin.Context, request *dto.OpenAIResponsesRequest) *RelayInfo {
	info, _ := genRelayInfoResponses(c, request)
	return info
}

func genRelayInfoResponses(c *gin.Context, request dto.Request) (*RelayInfo, error) {
	info := genBaseRelayInfo(c, request)
	info.RelayMode = relayconstant.RelayModeResponses
	info.RelayFormat = types.RelayFormatOpenAIResponses

	info.ResponsesUsageInfo = &ResponsesUsageInfo{
		BuiltInTools: make(map[string]*BuildInToolInfo),
	}
	responsesRequest, ok := request.(*dto.OpenAIResponsesRequest)
	if !ok || responsesRequest == nil {
		if _, opaque := request.(*dto.OpaqueOpenAIResponsesRequest); opaque {
			return info, nil
		}
		return info, errors.New("request is not a OpenAIResponsesRequest")
	}
	effectiveTools, err := responsesRequest.EffectiveTools()
	if err != nil {
		return info, fmt.Errorf("resolve effective Responses tools: %w", err)
	}
	for _, rawTool := range effectiveTools {
		var tool map[string]any
		if err := common.Unmarshal(rawTool, &tool); err != nil {
			return info, fmt.Errorf("parse effective Responses tool: %w", err)
		}
		toolType := common.Interface2String(tool["type"])
		canonicalToolType, ok := CanonicalResponsesBuiltInToolType(toolType)
		if !ok {
			continue
		}
		toolInfo := &BuildInToolInfo{
			ToolName:  canonicalToolType,
			CallCount: 0,
		}
		info.ResponsesUsageInfo.BuiltInTools[canonicalToolType] = toolInfo
		switch canonicalToolType {
		case dto.BuildInToolWebSearchPreview:
			searchContextSize := common.Interface2String(tool["search_context_size"])
			if searchContextSize == "" {
				searchContextSize = "medium"
				toolInfo.DefaultSearchSize = true
			}
			toolInfo.SearchContextSize = searchContextSize
		case dto.BuildInToolImageGeneration:
			toolInfo.Quality = common.Interface2String(tool["quality"])
			toolInfo.Size = common.Interface2String(tool["size"])
		}
	}
	return info, nil
}

func CanonicalResponsesBuiltInToolType(toolType string) (string, bool) {
	switch toolType {
	case dto.BuildInToolImageGeneration:
		return dto.BuildInToolImageGeneration, true
	case dto.BuildInToolWebSearch, dto.BuildInToolWebSearchPreview, dto.BuildInToolWebSearchPreview2025_03_11:
		return dto.BuildInToolWebSearchPreview, true
	case dto.BuildInToolFileSearch:
		return dto.BuildInToolFileSearch, true
	default:
		return "", false
	}
}

// GenRelayInfoGemini builds RelayInfo for Gemini requests.
func GenRelayInfoGemini(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatGemini
	info.ShouldIncludeUsage = false
	info.GeminiConvertInfo = &GeminiConvertInfo{}

	return info
}

// GenRelayInfoImage builds RelayInfo for image generation requests.
func GenRelayInfoImage(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAIImage
	return info
}

// GenRelayInfoOpenAI builds RelayInfo for standard OpenAI-compatible requests.
func GenRelayInfoOpenAI(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	info.RelayFormat = types.RelayFormatOpenAI
	return info
}

func genBaseRelayInfo(c *gin.Context, request dto.Request) *RelayInfo {

	//channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
	//channelId := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	//paramOverride := common.GetContextKeyStringMap(c, constant.ContextKeyChannelParamOverride)

	tokenGroup := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	// 当令牌分组为空时，表示使用用户分组
	if tokenGroup == "" {
		tokenGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}

	startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
	if startTime.IsZero() {
		startTime = time.Now()
	}

	isStream := false

	if request != nil {
		isStream = request.IsStream(c)
	}
	c.Set(string(constant.ContextKeyIsStream), isStream)

	// firstResponseTime = time.Now() - 1 second

	reqId := common.GetContextKeyString(c, common.RequestIdKey)
	if reqId == "" {
		reqId = common.GetTimeString() + common.GetRandomString(8)
	}
	info := &RelayInfo{
		Request: request,

		RequestId:  reqId,
		UserId:     common.GetContextKeyInt(c, constant.ContextKeyUserId),
		UsingGroup: common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		UserGroup:  common.GetContextKeyString(c, constant.ContextKeyUserGroup),
		UserQuota:  common.GetContextKeyInt(c, constant.ContextKeyUserQuota),
		UserEmail:  common.GetContextKeyString(c, constant.ContextKeyUserEmail),

		OriginModelName: common.GetContextKeyString(c, constant.ContextKeyOriginalModel),

		TokenId:        common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		TokenKey:       common.GetContextKeyString(c, constant.ContextKeyTokenKey),
		TokenUnlimited: common.GetContextKeyBool(c, constant.ContextKeyTokenUnlimited),
		TokenGroup:     tokenGroup,

		isFirstResponse: true,
		RelayMode:       relayconstant.Path2RelayMode(c.Request.URL.Path),
		RequestURLPath:  c.Request.URL.String(),
		RequestHeaders:  cloneRequestHeaders(c),
		IsStream:        isStream,

		StartTime:         startTime,
		FirstResponseTime: time.Time{},
		ThinkingContentInfo: ThinkingContentInfo{
			IsFirstThinkingContent:  true,
			SendLastThinkingContent: false,
		},
		TokenCountMeta: TokenCountMeta{
			//promptTokens: common.GetContextKeyInt(c, constant.ContextKeyPromptTokens),
			estimatePromptTokens: common.GetContextKeyInt(c, constant.ContextKeyEstimatedTokens),
		},
	}

	if info.RelayMode == relayconstant.RelayModeUnknown {
		info.RelayMode = c.GetInt("relay_mode")
	}

	if strings.HasPrefix(c.Request.URL.Path, "/pg") {
		info.IsPlayground = true
		info.RequestURLPath = strings.TrimPrefix(info.RequestURLPath, "/pg")
		info.RequestURLPath = "/v1" + info.RequestURLPath
	}

	userSetting, ok := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting)
	if ok {
		info.UserSetting = userSetting
	}

	return info
}

func cloneRequestHeaders(c *gin.Context) map[string]string {
	if c == nil || c.Request == nil {
		return nil
	}
	if len(c.Request.Header) == 0 {
		return nil
	}
	headers := make(map[string]string, len(c.Request.Header))
	for key := range c.Request.Header {
		value := strings.TrimSpace(c.Request.Header.Get(key))
		if value == "" {
			continue
		}
		headers[key] = value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// GenRelayInfo selects the correct RelayInfo builder for the resolved relay format.
func GenRelayInfo(c *gin.Context, relayFormat types.RelayFormat, request dto.Request, ws *websocket.Conn) (*RelayInfo, error) {
	var info *RelayInfo
	var err error
	switch relayFormat {
	case types.RelayFormatOpenAI:
		info = GenRelayInfoOpenAI(c, request)
	case types.RelayFormatOpenAIAudio:
		info = GenRelayInfoOpenAIAudio(c, request)
	case types.RelayFormatOpenAIImage:
		info = GenRelayInfoImage(c, request)
	case types.RelayFormatOpenAIRealtime:
		info = GenRelayInfoWs(c, ws)
	case types.RelayFormatClaude:
		info = GenRelayInfoClaude(c, request)
	case types.RelayFormatRerank:
		if request, ok := request.(*dto.RerankRequest); ok {
			info = GenRelayInfoRerank(c, request)
			break
		}
		err = errors.New("request is not a RerankRequest")
	case types.RelayFormatGemini:
		info = GenRelayInfoGemini(c, request)
	case types.RelayFormatEmbedding:
		info = GenRelayInfoEmbedding(c, request)
	case types.RelayFormatOpenAIResponses:
		info, err = genRelayInfoResponses(c, request)
	case types.RelayFormatOpenAIResponsesCompaction:
		switch request := request.(type) {
		case *dto.OpenAIResponsesCompactionRequest:
			info = GenRelayInfoResponsesCompaction(c, request)
		case *dto.OpaqueOpenAIResponsesRequest:
			info = genRelayInfoResponsesCompaction(c, request)
		default:
			err = errors.New("request is not a OpenAIResponsesCompactionRequest")
		}
	case types.RelayFormatTask:
		info = genBaseRelayInfo(c, nil)
		info.TaskRelayInfo = &TaskRelayInfo{}
	case types.RelayFormatMjProxy:
		info = genBaseRelayInfo(c, nil)
		info.TaskRelayInfo = &TaskRelayInfo{}
	default:
		err = errors.New("invalid relay format")
	}

	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, errors.New("failed to build relay info")
	}

	info.InitRequestConversionChain()
	return info, nil
}

func (info *RelayInfo) InitRequestConversionChain() {
	if info == nil {
		return
	}
	if len(info.RequestConversionChain) > 0 {
		return
	}
	if info.RelayFormat == "" {
		return
	}
	info.RequestConversionChain = []types.RelayFormat{info.RelayFormat}
}

func (info *RelayInfo) AppendRequestConversion(format types.RelayFormat) {
	if info == nil {
		return
	}
	if format == "" {
		return
	}
	if len(info.RequestConversionChain) == 0 {
		info.RequestConversionChain = []types.RelayFormat{format}
		return
	}
	last := info.RequestConversionChain[len(info.RequestConversionChain)-1]
	if last == format {
		return
	}
	info.RequestConversionChain = append(info.RequestConversionChain, format)
}

func (info *RelayInfo) GetFinalRequestRelayFormat() types.RelayFormat {
	if info == nil {
		return ""
	}
	if info.FinalRequestRelayFormat != "" {
		return info.FinalRequestRelayFormat
	}
	if n := len(info.RequestConversionChain); n > 0 {
		return info.RequestConversionChain[n-1]
	}
	return info.RelayFormat
}

// GenRelayInfoResponsesCompaction builds RelayInfo for responses compaction requests.
func GenRelayInfoResponsesCompaction(c *gin.Context, request *dto.OpenAIResponsesCompactionRequest) *RelayInfo {
	return genRelayInfoResponsesCompaction(c, request)
}

func genRelayInfoResponsesCompaction(c *gin.Context, request dto.Request) *RelayInfo {
	info := genBaseRelayInfo(c, request)
	if info.RelayMode == relayconstant.RelayModeUnknown {
		info.RelayMode = relayconstant.RelayModeResponsesCompact
	}
	info.RelayFormat = types.RelayFormatOpenAIResponsesCompaction
	return info
}

//func (info *RelayInfo) SetPromptTokens(promptTokens int) {
//	info.promptTokens = promptTokens
//}

func (info *RelayInfo) SetEstimatePromptTokens(promptTokens int) {
	info.estimatePromptTokens = promptTokens
}

func (info *RelayInfo) GetEstimatePromptTokens() int {
	return info.estimatePromptTokens
}

func (info *RelayInfo) SetFirstResponseTime() {
	info.SetFirstResponseTimeAt(time.Now())
}

func (info *RelayInfo) HasSendResponse() bool {
	if info == nil || info.FirstResponseTime.IsZero() {
		return false
	}
	return !info.FirstResponseTime.Before(info.StartTime)
}

func (info *RelayInfo) SetFirstResponseTimeAt(ts time.Time) {
	if info == nil || !info.isFirstResponse || ts.IsZero() {
		return
	}
	if !info.StartTime.IsZero() && ts.Before(info.StartTime) {
		return
	}
	info.FirstResponseTime = ts
	info.isFirstResponse = false
}

func (info *RelayInfo) FirstResponseLatencyMs() (int64, bool) {
	if info == nil || info.StartTime.IsZero() || !info.HasSendResponse() {
		return 0, false
	}
	latencyMs := info.FirstResponseTime.Sub(info.StartTime).Milliseconds()
	if latencyMs < 0 {
		return 0, false
	}
	return latencyMs, true
}

func (info *RelayInfo) SetUpstreamRequestStartTime() {
	info.SetUpstreamRequestStartTimeAt(time.Now())
}

func (info *RelayInfo) SetUpstreamRequestStartTimeAt(ts time.Time) {
	if info == nil || ts.IsZero() {
		return
	}
	info.UpstreamRequestStart = ts
	info.UpstreamHeaderTime = time.Time{}
	info.UpstreamFirstByteTime = time.Time{}
	info.UpstreamEndTime = time.Time{}
}

func (info *RelayInfo) SetUpstreamHeaderTime() {
	info.SetUpstreamHeaderTimeAt(time.Now())
}

func (info *RelayInfo) SetUpstreamHeaderTimeAt(ts time.Time) {
	if !info.validUpstreamEventTime(ts) || !info.UpstreamHeaderTime.IsZero() {
		return
	}
	info.UpstreamHeaderTime = ts
}

func (info *RelayInfo) SetUpstreamFirstByteTime() {
	info.SetUpstreamFirstByteTimeAt(time.Now())
}

func (info *RelayInfo) SetUpstreamFirstByteTimeAt(ts time.Time) {
	if !info.validUpstreamEventTime(ts) || !info.UpstreamFirstByteTime.IsZero() {
		return
	}
	info.UpstreamFirstByteTime = ts
}

func (info *RelayInfo) SetUpstreamEndTime() {
	info.SetUpstreamEndTimeAt(time.Now())
}

func (info *RelayInfo) SetUpstreamEndTimeAt(ts time.Time) {
	if !info.validUpstreamEventTime(ts) || !info.UpstreamEndTime.IsZero() {
		return
	}
	info.UpstreamEndTime = ts
}

func (info *RelayInfo) validUpstreamEventTime(ts time.Time) bool {
	return info != nil && !ts.IsZero() && !info.UpstreamRequestStart.IsZero() && !ts.Before(info.UpstreamRequestStart)
}

func (info *RelayInfo) UpstreamHeaderLatencyMs() (int64, bool) {
	if info == nil {
		return 0, false
	}
	return upstreamLatencyMs(info, info.UpstreamHeaderTime)
}

func (info *RelayInfo) UpstreamFirstByteLatencyMs() (int64, bool) {
	if info == nil {
		return 0, false
	}
	return upstreamLatencyMs(info, info.UpstreamFirstByteTime)
}

func (info *RelayInfo) UpstreamTotalLatencyMs() (int64, bool) {
	if info == nil {
		return 0, false
	}
	return upstreamLatencyMs(info, info.UpstreamEndTime)
}

func upstreamLatencyMs(info *RelayInfo, eventTime time.Time) (int64, bool) {
	if info == nil || info.UpstreamRequestStart.IsZero() || eventTime.IsZero() {
		return 0, false
	}
	latencyMs := eventTime.Sub(info.UpstreamRequestStart).Milliseconds()
	if latencyMs < 0 {
		return 0, false
	}
	return latencyMs, true
}

func SafeElapsedSeconds(startTime time.Time, endTime time.Time) int64 {
	if startTime.IsZero() || endTime.IsZero() {
		return 0
	}
	elapsed := endTime.Sub(startTime)
	if elapsed < 0 {
		return 0
	}
	return int64(elapsed / time.Second)
}

func (info *RelayInfo) UseTimeSeconds(now time.Time) int64 {
	if info == nil {
		return 0
	}
	return SafeElapsedSeconds(info.StartTime, now)
}

// TaskRelayInfo stores request-scoped metadata for asynchronous task relays.
type TaskRelayInfo struct {
	Action       string
	OriginTaskID string
	// PublicTaskID 是提交时预生成的 task_xxxx 格式公开 ID，
	// 供 DoResponse 在返回给客户端时使用（避免暴露上游真实 ID）。
	PublicTaskID string

	ConsumeQuota bool

	// LockedChannel holds the full channel object when the request is bound to
	// a specific channel (e.g., remix on origin task's channel). Stored as any
	// to avoid an import cycle with model; callers type-assert to *model.Channel.
	LockedChannel any
	// OriginRouteSnapshot holds the complete route snapshot of the origin task
	// for remix/continuation submits. Stored as any to avoid an import cycle with
	// model; the relay package validates the concrete *model.TaskRouteSnapshot
	// before the controller installs channel context.
	OriginRouteSnapshot any
	// OriginRouteRequestedModel and OriginRouteEffectiveModel preserve the
	// immutable model identity of an origin task for remix/continuation submits.
	// They are request-local only and never serialized to task rows.
	OriginRouteRequestedModel string `json:"-"`
	OriginRouteEffectiveModel string `json:"-"`
	OriginRouteGroup          string `json:"-"`
	OriginRouteChannelID      int    `json:"-"`
	// OriginRouteMappingMissing marks a legacy origin row that never persisted
	// its effective provider model. Such a continuation must fail closed rather
	// than re-reading the mutable channel model_mapping and silently drifting.
	OriginRouteMappingMissing bool `json:"-"`
	// OriginRouteCredential is the concrete credential persisted with the
	// origin task. It is request-local only and is never serialized; locked
	// continuation setup must use it instead of selecting a live channel key.
	OriginRouteCredential string `json:"-"`
	// SubmitRequestURL is the request-scoped URL plan built immediately before
	// an asynchronous task is submitted. It is never persisted; the route
	// snapshot stores only its sanitized operation path.
	SubmitRequestURL string `json:"-"`
}

// TaskSubmitReq describes the normalized task submission payload.
type TaskSubmitReq struct {
	Prompt         string                 `json:"prompt"`
	Model          string                 `json:"model,omitempty"`
	Mode           string                 `json:"mode,omitempty"`
	Image          string                 `json:"image,omitempty"`
	Images         []string               `json:"images,omitempty"`
	Size           string                 `json:"size,omitempty"`
	Duration       int                    `json:"duration,omitempty"`
	Seconds        string                 `json:"seconds,omitempty"`
	InputReference string                 `json:"input_reference,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

func (t *TaskSubmitReq) GetPrompt() string {
	return t.Prompt
}

func (t *TaskSubmitReq) HasImage() bool {
	return len(t.Images) > 0
}

func (t *TaskSubmitReq) UnmarshalJSON(data []byte) error {
	type Alias TaskSubmitReq
	aux := &struct {
		Metadata json.RawMessage `json:"metadata,omitempty"`
		Action   json.RawMessage `json:"action,omitempty"`
		Duration json.RawMessage `json:"duration,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(t),
	}

	if err := common.Unmarshal(data, &aux); err != nil {
		return err
	}

	if len(aux.Duration) > 0 {
		var durationInt int
		if err := common.Unmarshal(aux.Duration, &durationInt); err == nil {
			t.Duration = durationInt
		} else {
			var durationStr string
			if err := common.Unmarshal(aux.Duration, &durationStr); err == nil && durationStr != "" {
				if v, err := strconv.Atoi(durationStr); err == nil {
					t.Duration = v
				}
			}
		}
	}

	if len(aux.Metadata) > 0 {
		var metadataStr string
		if err := common.Unmarshal(aux.Metadata, &metadataStr); err == nil && metadataStr != "" {
			var metadataObj map[string]interface{}
			if err := common.Unmarshal([]byte(metadataStr), &metadataObj); err == nil {
				t.Metadata = metadataObj
				// Continue so a top-level action can be normalized when metadata
				// was supplied as a JSON string.
			}
		}

		var metadataObj map[string]interface{}
		if err := common.Unmarshal(aux.Metadata, &metadataObj); err == nil {
			t.Metadata = metadataObj
		}
	}
	if len(aux.Action) > 0 {
		var action string
		if err := common.Unmarshal(aux.Action, &action); err == nil {
			action = strings.TrimSpace(action)
			if action != "" {
				if t.Metadata == nil {
					t.Metadata = make(map[string]interface{})
				}
				existing, exists := t.Metadata["action"]
				if _, valid := taskRouteActionValue(existing); !exists || !valid {
					t.Metadata["action"] = action
				}
			}
		}
	}

	return nil
}
func (t *TaskSubmitReq) UnmarshalMetadata(v any) error {
	metadata := t.Metadata
	if metadata != nil {
		metadataBytes, err := common.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata failed: %w", err)
		}
		err = common.Unmarshal(metadataBytes, v)
		if err != nil {
			return fmt.Errorf("unmarshal metadata to target failed: %w", err)
		}
	}
	return nil
}

// TaskInfo represents the normalized task status payload returned to clients.
type TaskInfo struct {
	Code             int    `json:"code"`
	TaskID           string `json:"task_id"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"`
	Url              string `json:"url,omitempty"`
	RemoteUrl        string `json:"remote_url,omitempty"`
	Progress         string `json:"progress,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"` // 用于按倍率计费
	TotalTokens      int    `json:"total_tokens,omitempty"`      // 用于按倍率计费
}

// ResolvedURL returns the normalized result location emitted by an adaptor.
// Gemini historically used RemoteUrl while most adaptors used Url; callers
// must treat both fields identically to avoid losing completed video results.
func (t *TaskInfo) ResolvedURL() string {
	if t == nil {
		return ""
	}
	if url := strings.TrimSpace(t.Url); url != "" {
		return url
	}
	return strings.TrimSpace(t.RemoteUrl)
}

// FailTaskInfo builds a failed TaskInfo with the provided reason.
func FailTaskInfo(reason string) *TaskInfo {
	return &TaskInfo{
		Status: "FAILURE",
		Reason: reason,
	}
}

// RemoveDisabledFields 从请求 JSON 数据中移除渠道设置中禁用的字段
// service_tier: 服务层级字段，可能导致额外计费（OpenAI、Claude、Responses API 支持）
// inference_geo: Claude 数据驻留推理区域字段（仅 Claude 支持，默认过滤）
// speed: Claude 推理速度模式字段（仅 Claude 支持，默认过滤）
// store: 数据存储授权字段，涉及用户隐私（仅 OpenAI、Responses API 支持，默认允许透传，禁用后可能导致 Codex 无法使用）
// safety_identifier: 安全标识符，用于向 OpenAI 报告违规用户（仅 OpenAI 支持，涉及用户隐私）
// stream_options.include_obfuscation: 响应流混淆控制字段（仅 OpenAI Responses API 支持）
func RemoveDisabledFields(jsonData []byte, channelOtherSettings dto.ChannelOtherSettings, actualPassThroughBody bool) ([]byte, error) {
	if actualPassThroughBody {
		return jsonData, nil
	}
	if !hasRemovableDisabledField(jsonData, channelOtherSettings) {
		return jsonData, nil
	}

	var data map[string]common.RawMessage
	if err := common.Unmarshal(jsonData, &data); err != nil {
		common.SysError("RemoveDisabledFields Unmarshal error :" + err.Error())
		return jsonData, nil
	}

	// 默认移除 service_tier，除非明确允许（避免额外计费风险）
	if !channelOtherSettings.AllowServiceTier {
		if _, exists := data["service_tier"]; exists {
			delete(data, "service_tier")
		}
	}

	// 默认移除 inference_geo，除非明确允许（避免在未授权情况下透传数据驻留区域）
	if !channelOtherSettings.AllowInferenceGeo {
		if _, exists := data["inference_geo"]; exists {
			delete(data, "inference_geo")
		}
	}

	// 默认移除 speed，除非明确允许（避免意外切换 Claude 推理速度模式）
	if !channelOtherSettings.AllowSpeed {
		if _, exists := data["speed"]; exists {
			delete(data, "speed")
		}
	}

	// 默认允许 store 透传，除非明确禁用（禁用可能影响 Codex 使用）
	if channelOtherSettings.DisableStore {
		if _, exists := data["store"]; exists {
			delete(data, "store")
		}
	}

	// 默认移除 safety_identifier，除非明确允许（保护用户隐私，避免向 OpenAI 报告用户信息）
	if !channelOtherSettings.AllowSafetyIdentifier {
		if _, exists := data["safety_identifier"]; exists {
			delete(data, "safety_identifier")
		}
	}

	// 默认移除 stream_options.include_obfuscation，除非明确允许（避免关闭响应流混淆保护）
	if !channelOtherSettings.AllowIncludeObfuscation {
		if streamOptionsRaw, exists := data["stream_options"]; exists {
			var streamOptions map[string]common.RawMessage
			if err := common.Unmarshal(streamOptionsRaw, &streamOptions); err == nil {
				if _, includeExists := streamOptions["include_obfuscation"]; includeExists {
					delete(streamOptions, "include_obfuscation")
				}
				if len(streamOptions) == 0 {
					delete(data, "stream_options")
				} else {
					encodedStreamOptions, err := common.Marshal(streamOptions)
					if err != nil {
						common.SysError("RemoveDisabledFields Marshal stream_options error :" + err.Error())
						return jsonData, nil
					}
					data["stream_options"] = encodedStreamOptions
				}
			}
		}
	}

	jsonDataAfter, err := common.Marshal(data)
	if err != nil {
		common.SysError("RemoveDisabledFields Marshal error :" + err.Error())
		return jsonData, nil
	}
	return jsonDataAfter, nil
}

func hasRemovableDisabledField(jsonData []byte, channelOtherSettings dto.ChannelOtherSettings) bool {
	values := gjson.GetManyBytes(
		jsonData,
		"service_tier",
		"inference_geo",
		"speed",
		"store",
		"safety_identifier",
		"stream_options.include_obfuscation",
	)

	return (!channelOtherSettings.AllowServiceTier && values[0].Exists()) ||
		(!channelOtherSettings.AllowInferenceGeo && values[1].Exists()) ||
		(!channelOtherSettings.AllowSpeed && values[2].Exists()) ||
		(channelOtherSettings.DisableStore && values[3].Exists()) ||
		(!channelOtherSettings.AllowSafetyIdentifier && values[4].Exists()) ||
		(!channelOtherSettings.AllowIncludeObfuscation && values[5].Exists())
}

// RemoveGeminiDisabledFields removes disabled fields from Gemini request JSON data
// Currently supports removing functionResponse.id field which Vertex AI does not support
func RemoveGeminiDisabledFields(jsonData []byte) ([]byte, error) {
	if !model_setting.GetGeminiSettings().RemoveFunctionResponseIdEnabled {
		return jsonData, nil
	}

	var data map[string]interface{}
	if err := common.Unmarshal(jsonData, &data); err != nil {
		common.SysError("RemoveGeminiDisabledFields Unmarshal error: " + err.Error())
		return jsonData, nil
	}

	// Process contents array
	// Handle both camelCase (functionResponse) and snake_case (function_response)
	if contents, ok := data["contents"].([]interface{}); ok {
		for _, content := range contents {
			if contentMap, ok := content.(map[string]interface{}); ok {
				if parts, ok := contentMap["parts"].([]interface{}); ok {
					for _, part := range parts {
						if partMap, ok := part.(map[string]interface{}); ok {
							// Check functionResponse (camelCase)
							if funcResp, ok := partMap["functionResponse"].(map[string]interface{}); ok {
								delete(funcResp, "id")
							}
							// Check function_response (snake_case)
							if funcResp, ok := partMap["function_response"].(map[string]interface{}); ok {
								delete(funcResp, "id")
							}
						}
					}
				}
			}
		}
	}

	jsonDataAfter, err := common.Marshal(data)
	if err != nil {
		common.SysError("RemoveGeminiDisabledFields Marshal error: " + err.Error())
		return jsonData, nil
	}
	return jsonDataAfter, nil
}
