package middleware

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type ModelRequest struct {
	Model string `json:"model"`
	Group string `json:"group,omitempty"`
}

// captureTaskRouteIntent records the operation that can be inferred before a
// concrete provider channel is selected. It deliberately decodes a tolerant
// JSON object instead of TaskSubmitReq: provider adaptors may legally use
// object/array-valued image fields that the normalized request cannot model.
// The request body is reset so the task controller can parse it again later.
func captureTaskRouteIntent(c *gin.Context) error {
	if c == nil || c.Request == nil {
		return errors.New("request is nil")
	}
	contentType := strings.ToLower(c.Request.Header.Get("Content-Type"))
	if strings.Contains(contentType, gin.MIMEMultipartPOSTForm) {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return err
		}
		defer form.RemoveAll()
		payload := make(map[string]any, len(form.Value))
		for key, values := range form.Value {
			if len(values) == 1 {
				payload[key] = values[0]
			} else {
				payload[key] = values
			}
		}
		common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntentFromPayload(payload, taskRouteMultipartImageFileCount(form.File)))
		return nil
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return err
	}
	body, err := storage.Bytes()
	if err != nil {
		return err
	}
	if strings.Contains(contentType, gin.MIMEPOSTForm) {
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return err
		}
		payload := make(map[string]any, len(values))
		for key, items := range values {
			if len(items) == 1 {
				payload[key] = items[0]
			} else {
				payload[key] = items
			}
		}
		common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntentFromPayload(payload, 0))
		if _, err := storage.Seek(0, io.SeekStart); err != nil {
			return err
		}
		c.Request.Body = io.NopCloser(storage)
		return nil
	}
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return err
	}
	// Form clients sometimes encode metadata as a JSON string. Normalize that
	// representation only for route inference; the adaptor remains the owner of
	// full request validation.
	if metadata, ok := payload["metadata"].(string); ok && strings.TrimSpace(metadata) != "" {
		var metadataObject map[string]any
		if err := common.Unmarshal([]byte(metadata), &metadataObject); err == nil {
			payload["metadata"] = metadataObject
		}
	}
	common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntentFromPayload(payload, 0))
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(storage)
	return nil
}

func taskRouteMultipartImageFileCount(files map[string][]*multipart.FileHeader) int {
	count := 0
	for field, headers := range files {
		field = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(field, "[]")))
		switch field {
		case "image", "images", "input_reference", "first_frame", "last_frame":
			count += len(headers)
		}
	}
	return count
}

var (
	// ErrTokenModelAccessMissing indicates that model limits are enabled but the
	// token did not provide an allow-list at all.
	ErrTokenModelAccessMissing = errors.New("token has no model access")
	// ErrTokenModelAccessInvalid indicates a malformed model-limit context value.
	ErrTokenModelAccessInvalid = errors.New("token model access configuration is invalid")
)

// ValidateTokenModelAccess applies token model limits to a concrete requested
// model. Empty model names are deliberately accepted because task fetch/content
// endpoints resolve their model from the persisted task row instead of the
// request body. Callers that do have a model must invoke this helper before
// selecting or submitting a channel so all entry points use identical rules.
func ValidateTokenModelAccess(c *gin.Context, modelName string) error {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" || !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return nil
	}
	rawLimits, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	if !ok {
		return ErrTokenModelAccessMissing
	}
	limits, ok := rawLimits.(map[string]bool)
	if !ok {
		return ErrTokenModelAccessInvalid
	}
	if !ratio_setting.ModelNameMatchesLimit(modelName, limits) {
		return fmt.Errorf("token is not allowed to access model %s", modelName)
	}
	return nil
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		var channel *model.Channel
		channelId, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId)
		modelRequest, shouldSelectChannel, err := getModelRequest(c)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		// Validate an explicitly supplied model only for requests that will select
		// or submit a channel. Task query/content/remix requests deliberately defer
		// authorization to their persisted task route; applying a token model limit
		// to a transport-only follow-up would reject a valid task merely because its
		// request shape contains a provider-specific model name.
		if shouldSelectChannel {
			if modelName := strings.TrimSpace(modelRequest.Model); modelName != "" {
				if accessErr := ValidateTokenModelAccess(c, modelName); accessErr != nil {
					if errors.Is(accessErr, ErrTokenModelAccessMissing) {
						abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenNoModelAccess))
					} else {
						abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenModelForbidden, map[string]any{"Model": modelName}))
					}
					return
				}
			}
		}
		// A token-pinned channel is an explicit selection for every request. A
		// follow-up task fetch/content/remix without a pin defers to the persisted
		// task route, but a request carrying a pin must still load and initialize
		// that channel strictly so malformed, deleted, or disabled pins fail
		// explicitly instead of being silently ignored.
		if ok {
			id, err := parseSpecificChannelID(channelId)
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			if channel.Status != common.ChannelStatusEnabled {
				abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorChannelDisabled))
				return
			}
			if _, isCompactRequest := ratio_setting.CompactBaseModelName(modelRequest.Model); isCompactRequest &&
				!isSpecificChannelEnabledForCompactRequest(c, modelRequest.Model, channel.Id) {
				usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
				abortWithOpenAiMessage(c, http.StatusServiceUnavailable, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": modelRequest.Model}), types.ErrorCodeModelNotFound)
				return
			}
		} else {
			if shouldSelectChannel {
				if modelRequest.Model == "" {
					abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorModelNameRequired))
					return
				}
				bootstrapState, bootstrapErr := service.EnsureResponsesBootstrapRecoveryStateFromRequest(c)
				if bootstrapErr != nil {
					abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": bootstrapErr.Error()}))
					return
				}
				var selectGroup string
				usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
				// check path is /pg/chat/completions
				if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
					playgroundRequest := &dto.PlayGroundRequest{}
					err = common.UnmarshalBodyReusable(c, playgroundRequest)
					if err != nil {
						abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidPlayground, map[string]any{"Error": err.Error()}))
						return
					}
					if playgroundRequest.Group != "" {
						if !service.GroupInUserUsableGroups(usingGroup, playgroundRequest.Group) && playgroundRequest.Group != usingGroup {
							abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorGroupAccessDenied))
							return
						}
						usingGroup = playgroundRequest.Group
						common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
					}
				}

				if preferredChannelID, found := service.GetPreferredChannelByAffinity(c, modelRequest.Model, usingGroup); found {
					preferred, err := model.CacheGetChannel(preferredChannelID)
					if err == nil && preferred != nil {
						if preferred.Status != common.ChannelStatusEnabled {
							if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
								abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorAffinityChannelDisabled))
								return
							}
						} else if common.IsAutoGroup(usingGroup) {
							userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
							autoGroups := service.GetUserAutoGroup(userGroup)
							for _, g := range autoGroups {
								if !service.IsChannelEnabledForGroupModelForContext(c, g, modelRequest.Model, preferred.Id) {
									continue
								}
								decision := service.EvaluateChannelHealthSelectionForChannelAtBaseRoute(c, g, modelRequest.Model, preferred)
								if !decision.Allow {
									service.MarkChannelHealthAffinityBypassForChannel(c, g, modelRequest.Model, preferred)
									continue
								}
								selectGroup = g
								common.SetContextKey(c, constant.ContextKeyAutoGroup, g)
								channel = preferred
								service.MarkChannelAffinityUsed(c, g, preferred.Id)
								break
							}
						} else if service.IsChannelEnabledForGroupModelForContext(c, usingGroup, modelRequest.Model, preferred.Id) {
							decision := service.EvaluateChannelHealthSelectionForChannelAtBaseRoute(c, usingGroup, modelRequest.Model, preferred)
							if decision.Allow {
								channel = preferred
								selectGroup = usingGroup
								service.MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
							} else {
								service.MarkChannelHealthAffinityBypassForChannel(c, usingGroup, modelRequest.Model, preferred)
							}
						}
					}
				}

				if channel == nil {
					channel, selectGroup, err = getChannelWithBootstrapGrace(c, modelRequest.Model, usingGroup, bootstrapState)
					if err != nil {
						showGroup := usingGroup
						if common.IsAutoGroup(usingGroup) {
							showGroup = fmt.Sprintf("auto(%s)", selectGroup)
						}
						message := i18n.T(c, i18n.MsgDistributorGetChannelFailed, map[string]any{"Group": showGroup, "Model": modelRequest.Model, "Error": err.Error()})
						// 如果错误，但是渠道不为空，说明是数据库一致性问题
						//if channel != nil {
						//	common.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
						//	message = "数据库一致性已被破坏，请联系管理员"
						//}
						abortWithOpenAiMessage(c, http.StatusServiceUnavailable, message, types.ErrorCodeModelNotFound)
						return
					}
					if channel == nil {
						abortWithOpenAiMessage(c, http.StatusServiceUnavailable, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": modelRequest.Model}), types.ErrorCodeModelNotFound)
						return
					}
				}
			}
		}
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		// Task fetch/content endpoints deliberately defer channel restoration to
		// their controller because the task row is the source of truth. When
		// there is no token-pinned channel, do not initialize a nil channel here:
		// SetupContextForSelectedChannel would turn a valid fetch into a 500.
		// A selected channel remains strict and must still be initialized.
		if channel != nil {
			if setupErr := SetupContextForSelectedChannel(c, channel, modelRequest.Model); setupErr != nil {
				// Channel health selection may reserve a half-open probe before
				// local setup chooses a key/header/profile. No upstream request is
				// sent on this path, so release that reservation immediately instead
				// of waiting for the Redis lease TTL.
				if channel != nil {
					service.ReleaseChannelHealthProbeForChannel(c, channel.Id)
				}
				abortWithSelectedChannelSetupError(c, setupErr)
				return
			}
		}
		c.Next()
		if channel != nil && c.Writer != nil && c.Writer.Status() < http.StatusBadRequest {
			service.RecordChannelAffinity(c, channel.Id)
		}
	}
}

// parseSpecificChannelID accepts the string representation produced by token
// authentication and the integer forms used by internal middleware/tests. A
// context value is untrusted input; never use a direct type assertion here or
// a malformed value can panic the request instead of returning a 400.
func parseSpecificChannelID(value any) (int, error) {
	var id int64
	switch typed := value.(type) {
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, err
		}
		id = parsed
	case int:
		id = int64(typed)
	case int8:
		id = int64(typed)
	case int16:
		id = int64(typed)
	case int32:
		id = int64(typed)
	case int64:
		id = typed
	case uint:
		if uint64(typed) > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("specific channel id overflows int")
		}
		id = int64(typed)
	case uint8:
		id = int64(typed)
	case uint16:
		id = int64(typed)
	case uint32:
		id = int64(typed)
	case uint64:
		if typed > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("specific channel id overflows int")
		}
		id = int64(typed)
	default:
		return 0, fmt.Errorf("unsupported specific channel id type %T", value)
	}
	if id <= 0 || int64(int(id)) != id {
		return 0, fmt.Errorf("specific channel id must be positive")
	}
	return int(id), nil
}

func abortWithSelectedChannelSetupError(c *gin.Context, setupErr *types.NewAPIError) {
	if setupErr == nil {
		return
	}
	statusCode := setupErr.StatusCode
	if statusCode < http.StatusBadRequest || statusCode > 599 {
		statusCode = http.StatusInternalServerError
	}
	message := setupErr.MaskSensitiveError()
	if key := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyChannelKey)); key != "" {
		message = strings.ReplaceAll(message, key, "[redacted]")
	}
	if strings.TrimSpace(message) == "" {
		message = "failed to initialize selected channel"
	}
	abortWithOpenAiMessage(c, statusCode, message, setupErr.GetErrorCode())
}

func isSpecificChannelEnabledForCompactRequest(c *gin.Context, modelName string, channelID int) bool {
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if common.IsAutoGroup(usingGroup) {
		userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		return service.IsChannelEnabledForAnyGroupModelForContext(c, service.GetUserAutoGroup(userGroup), modelName, channelID)
	}
	return service.IsChannelEnabledForGroupModelForContext(c, usingGroup, modelName, channelID)
}

// getModelFromRequest 从请求中读取模型信息
// 根据 Content-Type 自动处理：
// - application/json
// - application/x-www-form-urlencoded
// - multipart/form-data
func getModelFromRequest(c *gin.Context) (*ModelRequest, error) {
	if modelRequest, ok := getOpaqueResponsesModelRequest(c); ok {
		return modelRequest, nil
	}
	if strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		modelRequest, err := getModelFromJSONBody(c)
		if err != nil {
			// No prefix here: Distribute (the caller that turns the error into
			// a response) applies MsgDistributorInvalidRequest exactly once.
			return nil, err
		}
		return modelRequest, nil
	}

	var modelRequest ModelRequest
	err := common.UnmarshalBodyReusable(c, &modelRequest)
	if err != nil {
		// Same: the response-side caller owns the user-facing prefix.
		return nil, err
	}
	return &modelRequest, nil
}

func getModelFromJSONBody(c *gin.Context) (*ModelRequest, error) {
	if modelRequest, ok := getOpaqueResponsesModelRequest(c); ok {
		return modelRequest, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}

	modelRequest, err := decodeModelRequestFields(storage)
	if err != nil {
		return nil, err
	}

	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	c.Request.Body = io.NopCloser(storage)

	return modelRequest, nil
}

func getOpaqueResponsesModelRequest(c *gin.Context) (*ModelRequest, bool) {
	metadata, ok := common.GetResponsesRequestMetadata(c)
	if !ok || !metadata.HasOpaqueEncryptedContent {
		return nil, false
	}
	return &ModelRequest{
		Model: metadata.Model,
		Group: metadata.Group,
	}, true
}

func decodeModelRequestFields(reader io.Reader) (*ModelRequest, error) {
	decoder := common.NewJsonDecoderUseNumber(reader)
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := token.(common.JsonDelim)
	if !ok || delim != '{' {
		return nil, errors.New("invalid JSON request body")
	}

	modelRequest := &ModelRequest{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		field, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid JSON field name")
		}
		switch field {
		case "model":
			value, err := decodeOptionalJSONString(decoder, field)
			if err != nil {
				return nil, err
			}
			modelRequest.Model = value
		case "group":
			value, err := decodeOptionalJSONString(decoder, field)
			if err != nil {
				return nil, err
			}
			modelRequest.Group = value
		default:
			if err := skipJSONValue(decoder); err != nil {
				return nil, err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected JSON token after request body: %v", token)
	}
	return modelRequest, nil
}

func decodeOptionalJSONString(decoder *common.JsonDecoder, field string) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	if token == nil {
		return "", nil
	}
	value, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("field %s must be a string", field)
	}
	return value, nil
}

func skipJSONValue(decoder *common.JsonDecoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(common.JsonDelim)
	if !ok {
		return nil
	}

	var end common.JsonDelim
	switch delim {
	case '{':
		end = '}'
	case '[':
		end = ']'
	default:
		return nil
	}

	for decoder.More() {
		if delim == '{' {
			if _, err := decoder.Token(); err != nil {
				return err
			}
		}
		if err := skipJSONValue(decoder); err != nil {
			return err
		}
	}
	token, err = decoder.Token()
	if err != nil {
		return err
	}
	if token != end {
		return errors.New("invalid JSON request body")
	}
	return nil
}

func getModelRequest(c *gin.Context) (*ModelRequest, bool, error) {
	var modelRequest ModelRequest
	shouldSelectChannel := true
	var err error
	if strings.Contains(c.Request.URL.Path, "/mj/") {
		relayMode := relayconstant.Path2RelayModeMidjourney(c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeMidjourneyTaskFetch ||
			relayMode == relayconstant.RelayModeMidjourneyTaskFetchByCondition ||
			relayMode == relayconstant.RelayModeMidjourneyNotify ||
			relayMode == relayconstant.RelayModeMidjourneyTaskImageSeed {
			shouldSelectChannel = false
		} else {
			midjourneyRequest := dto.MidjourneyRequest{}
			err = common.UnmarshalBodyReusable(c, &midjourneyRequest)
			if err != nil {
				return nil, false, errors.New(i18n.T(c, i18n.MsgDistributorInvalidMidjourney, map[string]any{"Error": err.Error()}))
			}
			midjourneyModel, mjErr, success := service.GetMjRequestModel(relayMode, &midjourneyRequest)
			if mjErr != nil {
				return nil, false, fmt.Errorf("%s", mjErr.Description)
			}
			if midjourneyModel == "" {
				if !success {
					return nil, false, fmt.Errorf("%s", i18n.T(c, i18n.MsgDistributorInvalidParseModel))
				} else {
					// task fetch, task fetch by condition, notify
					shouldSelectChannel = false
				}
			}
			modelRequest.Model = midjourneyModel
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/suno/") {
		relayMode := relayconstant.Path2RelaySuno(c.Request.Method, c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeSunoFetch ||
			relayMode == relayconstant.RelayModeSunoFetchByID {
			shouldSelectChannel = false
		} else {
			modelName := service.CoverTaskActionToModelName(constant.TaskPlatformSuno, c.Param("action"))
			modelRequest.Model = modelName
		}
		c.Set("platform", string(constant.TaskPlatformSuno))
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos/") && strings.HasSuffix(c.Request.URL.Path, "/remix") {
		relayMode := relayconstant.RelayModeVideoSubmit
		c.Set("relay_mode", relayMode)
		shouldSelectChannel = false
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos") {
		//curl https://api.openai.com/v1/videos \
		//  -H "Authorization: Bearer $OPENAI_API_KEY" \
		//  -F "model=sora-2" \
		//  -F "prompt=A calico cat playing a piano on stage"
		//	-F input_reference="@image.jpg"
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			relayMode = relayconstant.RelayModeVideoSubmit
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			if req != nil {
				modelRequest.Model = req.Model
			}
			if err := captureTaskRouteIntent(c); err != nil {
				return nil, false, err
			}
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/video/generations") {
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			modelRequest.Model = req.Model
			if err := captureTaskRouteIntent(c); err != nil {
				return nil, false, err
			}
			relayMode = relayconstant.RelayModeVideoSubmit
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
		}
		if _, ok := c.Get("relay_mode"); !ok {
			c.Set("relay_mode", relayMode)
		}
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models/") || strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
		// Gemini API 路径处理: /v1beta/models/gemini-2.0-flash:generateContent
		relayMode := relayconstant.RelayModeGemini
		modelName := extractModelNameFromGeminiPath(c.Request.URL.Path)
		if modelName != "" {
			modelRequest.Model = modelName
		}
		c.Set("relay_mode", relayMode)
	} else if !strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") && !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/realtime") {
		//wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01
		modelRequest.Model = c.Query("model")
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/moderations") {
		if modelRequest.Model == "" {
			modelRequest.Model = "text-moderation-stable"
		}
	}
	if strings.HasSuffix(c.Request.URL.Path, "embeddings") {
		if modelRequest.Model == "" {
			modelRequest.Model = c.Param("model")
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/images/generations") {
		modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "dall-e")
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1/images/edits") {
		//modelRequest.Model = common.GetStringIfEmpty(c.PostForm("model"), "gpt-image-1")
		contentType := c.ContentType()
		if slices.Contains([]string{gin.MIMEPOSTForm, gin.MIMEMultipartPOSTForm}, contentType) {
			req, err := getModelFromRequest(c)
			if err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/audio") {
		relayMode := relayconstant.RelayModeAudioSpeech
		if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/speech") {

			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "tts-1")
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/translations") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranslation
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranscription
		}
		c.Set("relay_mode", relayMode)
	}
	if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
		// playground chat completions
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
		modelRequest.Group = req.Group
		common.SetContextKey(c, constant.ContextKeyTokenGroup, modelRequest.Group)
	}

	if common.IsResponsesCompactPath(c.Request.URL.Path) && modelRequest.Model != "" {
		modelRequest.Model = ratio_setting.WithCompactModelSuffix(modelRequest.Model)
	}
	return &modelRequest, shouldSelectChannel, nil
}

func getChannelWithBootstrapGrace(c *gin.Context, modelName string, usingGroup string, state *service.ResponsesBootstrapRecoveryState) (*model.Channel, string, error) {
	if !service.ShouldWaitForResponsesBootstrapRecoverySelection(c, usingGroup, modelName, state) {
		if state != nil && state.Enabled {
			logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery skipped: group=%s model=%s reason=no_candidate_channel", usingGroup, modelName))
		}
		state = nil
	}
	for {
		channel, selectGroup, err := service.CacheGetRandomSatisfiedChannel(&service.RetryParam{
			Ctx:        c,
			ModelName:  modelName,
			TokenGroup: usingGroup,
			Retry:      common.GetPointer(0),
		})
		if channel != nil || err != nil || state == nil {
			if state != nil && state.WaitAttempts > 0 {
				if channel != nil {
					service.MarkResponsesBootstrapRecoveryMetric(c)
					logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery selected channel: group=%s model=%s channel_id=%d attempts=%d waited_ms=%d", selectGroup, modelName, channel.Id, state.WaitAttempts, state.WaitDuration.Milliseconds()))
				} else if err != nil {
					service.RecordResponsesBootstrapRecovery(modelName, usingGroup, false, state.WaitDuration.Milliseconds())
					logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery stopped with error: group=%s model=%s attempts=%d waited_ms=%d error=%q", usingGroup, modelName, state.WaitAttempts, state.WaitDuration.Milliseconds(), err.Error()))
				}
			}
			return channel, selectGroup, err
		}

		waitDuration, sendPing, ok := service.NextResponsesBootstrapWait(c, time.Now())
		if !ok {
			service.RecordResponsesBootstrapRecovery(modelName, usingGroup, false, state.WaitDuration.Milliseconds())
			logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery deadline reached: group=%s model=%s attempts=%d waited_ms=%d", usingGroup, modelName, state.WaitAttempts, state.WaitDuration.Milliseconds()))
			return nil, selectGroup, nil
		}

		if sendPing {
			helper.SetEventStreamHeaders(c)
			service.MarkResponsesBootstrapHeadersSent(c)
			now := time.Now()
			if err := helper.PingData(c); err != nil {
				return nil, selectGroup, err
			}
			service.MarkResponsesBootstrapPingSent(c, now)
			logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery ping: group=%s model=%s attempts=%d waited_ms=%d", usingGroup, modelName, state.WaitAttempts, state.WaitDuration.Milliseconds()))
		}

		timer := time.NewTimer(waitDuration)
		select {
		case <-c.Request.Context().Done():
			timer.Stop()
			service.RecordResponsesBootstrapRecovery(modelName, usingGroup, false, state.WaitDuration.Milliseconds())
			logger.LogInfo(c, fmt.Sprintf("responses bootstrap recovery client disconnected: group=%s model=%s attempts=%d waited_ms=%d error=%q", usingGroup, modelName, state.WaitAttempts, state.WaitDuration.Milliseconds(), c.Request.Context().Err().Error()))
			return nil, selectGroup, c.Request.Context().Err()
		case <-timer.C:
		}
	}
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	return setupContextForSelectedChannel(c, channel, modelName, nil, "", "")
}

// SetupContextForSelectedChannelWithRouteSnapshot installs channel context for
// an asynchronous follow-up. Current-schema snapshots own the route and the
// resolved static header policy, so setup must not re-read mutable routing or
// header-profile configuration before the caller binds the persisted route.
func SetupContextForSelectedChannelWithRouteSnapshot(c *gin.Context, channel *model.Channel, modelName string, snapshot *model.TaskRouteSnapshot) *types.NewAPIError {
	return SetupContextForSelectedChannelWithRouteSnapshotAndCredential(c, channel, modelName, snapshot, "")
}

// SetupContextForSelectedChannelWithRouteSnapshotAndCredential installs a
// persisted asynchronous route and validates the concrete credential captured
// when the task was submitted. The credential is request-local and is never
// persisted by this helper.
func SetupContextForSelectedChannelWithRouteSnapshotAndCredential(c *gin.Context, channel *model.Channel, modelName string, snapshot *model.TaskRouteSnapshot, persistedCredential string) *types.NewAPIError {
	if snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotHeaderPolicySchemaVersion {
		if err := snapshot.ValidateHeaderPolicy(); err != nil {
			return types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid, types.ErrOptionWithSkipRetry())
		}
	}
	if snapshot == nil || snapshot.SchemaVersion < model.TaskRouteSnapshotSchemaVersion {
		if snapshot != nil && strings.TrimSpace(snapshot.EffectiveUpstreamModel) == "" {
			return types.NewError(service.ErrTaskRouteMappingMissing, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
		}
		effectiveModel := ""
		if snapshot != nil {
			effectiveModel = strings.TrimSpace(snapshot.EffectiveUpstreamModel)
		}
		return setupContextForSelectedChannel(c, channel, modelName, snapshot, persistedCredential, effectiveModel)
	}
	if err := snapshot.Validate(); err != nil {
		return types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid, types.ErrOptionWithSkipRetry())
	}
	// A current-schema snapshot is bound to the exact channel identity selected
	// at submission time. Validate that identity before installing any
	// channel-scoped context; channel IDs can be reused after an administrator
	// deletes and recreates a provider, and accepting that row would replay a
	// task through a different adapter or credential scope.
	if err := service.ValidateTaskRouteSnapshotChannelCompatibility(snapshot, channel); err != nil {
		return types.NewError(
			fmt.Errorf("task route snapshot is incompatible with selected channel: %w", err),
			types.ErrorCodeGetChannelFailed,
			types.ErrOptionWithSkipRetry(),
		)
	}
	return setupContextForSelectedChannel(c, channel, modelName, snapshot, persistedCredential, "")
}

func setupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string, snapshot *model.TaskRouteSnapshot, persistedCredential string, effectiveModelOverride string) *types.NewAPIError {
	usePersistedRoutePolicy := snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotSchemaVersion
	useCapturedHeaderPolicy := snapshot != nil && snapshot.SchemaVersion >= model.TaskRouteSnapshotHeaderPolicySchemaVersion
	useSnapshotRoutePolicy := usePersistedRoutePolicy || useCapturedHeaderPolicy
	if useCapturedHeaderPolicy {
		if err := snapshot.ValidateHeaderPolicy(); err != nil {
			return types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid, types.ErrOptionWithSkipRetry())
		}
	}
	effectiveModelOverride = strings.TrimSpace(effectiveModelOverride)
	if effectiveModelOverride != "" && usePersistedRoutePolicy && effectiveModelOverride != strings.TrimSpace(snapshot.EffectiveUpstreamModel) {
		return types.NewError(errors.New("effective model override does not match task route snapshot"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	resolvedPersistedCredential := ""
	if usePersistedRoutePolicy && strings.TrimSpace(persistedCredential) != "" {
		resolved, err := service.ResolveTaskCredential(channel, snapshot, persistedCredential)
		if err != nil {
			return types.NewError(err, types.ErrorCodeChannelNoAvailableKey, types.ErrOptionWithSkipRetry())
		}
		resolvedPersistedCredential = resolved
	}
	// Channel context and its capacity lease are attempt-scoped. A retry may
	// call this function directly (for example task submissions locked to a
	// channel), so release any previous reservation before installing the next
	// channel identity.
	service.ReleaseChannelConcurrency(c)
	c.Set("original_model", modelName) // for retry
	if channel == nil {
		return types.NewError(errors.New("channel is nil"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	resetSelectedChannelContext(c, channel, modelName)
	// Keep the complete selected channel available to Relay's initial selection
	// path. That path must reuse this immutable snapshot instead of rebuilding a
	// credential-less shell from channel_id/type/name and then losing mapping or
	// multi-key metadata.
	common.SetContextKey(c, constant.ContextKeySelectedChannelSnapshot, channel.CloneForCache())
	var channelSetting dto.ChannelSettings
	var channelOtherSettings dto.ChannelOtherSettings
	channelOtherRaw := ""
	channelOther := ""
	channelApiVersion := ""
	channelStatusCodeMapping := ""
	channelOrganization := ""
	if usePersistedRoutePolicy {
		providerRuntime, err := service.TaskRouteSnapshotProviderRuntime(snapshot)
		if err != nil {
			return types.NewError(
				fmt.Errorf("resolve task route snapshot provider runtime: %w", err),
				types.ErrorCodeChannelHeaderOverrideInvalid,
				types.ErrOptionWithSkipRetry(),
			)
		}
		channelSetting = providerRuntime.ChannelSettings
		proxy, err := service.TaskRouteSnapshotProxy(snapshot, channel)
		if err != nil {
			return types.NewError(
				fmt.Errorf("resolve task route snapshot proxy: %w", err),
				types.ErrorCodeGetChannelFailed,
				types.ErrOptionWithSkipRetry(),
			)
		}
		channelSetting.Proxy = proxy
		channelOtherSettings = providerRuntime.ChannelOtherSettings
		channelOtherRaw = service.TaskRouteProviderOtherSettingsRaw(providerRuntime)
		channelOther = providerRuntime.Other
		channelApiVersion = providerRuntime.ApiVersion
		channelStatusCodeMapping = providerRuntime.StatusCodeMapping
		channelOrganization = providerRuntime.OpenAIOrganization
	} else {
		channelSetting = channel.GetSetting()
		channelOtherSettings = channel.GetOtherSettings()
		channelOtherRaw = channel.OtherSettings
		channelOther = channel.Other
		channelStatusCodeMapping = channel.GetStatusCodeMapping()
		if channel.OpenAIOrganization != nil {
			channelOrganization = strings.TrimSpace(*channel.OpenAIOrganization)
		}
		if useCapturedHeaderPolicy && snapshot.ProxyCaptured {
			proxy, err := service.TaskRouteSnapshotProxy(snapshot, channel)
			if err != nil {
				return types.NewError(
					fmt.Errorf("resolve task route snapshot proxy: %w", err),
					types.ErrorCodeGetChannelFailed,
					types.ErrOptionWithSkipRetry(),
				)
			}
			channelSetting.Proxy = proxy
		}
	}
	attemptIndex := -1
	if value, ok := common.GetContextKeyType[int](c, constant.ContextKeyChannelHeaderProfileAttempt); ok {
		attemptIndex = value
	}
	if !useSnapshotRoutePolicy {
		if _, err := service.PrepareChannelRuntimeHeaderProfileSelection(
			c,
			channel.Id,
			channelOtherSettings.HeaderProfileStrategy,
			attemptIndex,
		); err != nil {
			return types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid)
		}
	}
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	common.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channelSetting)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channelOtherSettings)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSettingRaw, channelOtherRaw)
	c.Set("channel_other", channelOther)
	c.Set("channel_organization", channelOrganization)
	var paramOverride map[string]any
	if !useSnapshotRoutePolicy {
		paramOverride = channel.GetParamOverride()
	}
	headerOverride := map[string]any{}
	var headerPolicyAudit *service.RuntimeHeaderPolicyAudit
	if useCapturedHeaderPolicy {
		capturedHeaders, err := model.DecryptTaskRouteHeaderPolicy(snapshot.HeaderPolicyCiphertext)
		if err != nil {
			return types.NewError(fmt.Errorf("decrypt task route header policy: %w", err), types.ErrorCodeChannelHeaderOverrideInvalid, types.ErrOptionWithSkipRetry())
		}
		if model.ComputeTaskRouteHeaderPolicyFingerprint(capturedHeaders) != strings.TrimSpace(snapshot.HeaderPolicyFingerprint) {
			return types.NewError(errors.New("task route snapshot header policy fingerprint mismatch"), types.ErrorCodeChannelHeaderOverrideInvalid, types.ErrOptionWithSkipRetry())
		}
		for name, value := range capturedHeaders {
			name = strings.TrimSpace(name)
			value = strings.TrimSpace(value)
			if name == "" || value == "" {
				continue
			}
			headerOverride[name] = value
		}
		// Midjourney's request builder resolves profile headers from this
		// settings value. Disable that mutable selector after installing the
		// captured static policy so it cannot consult current profile state.
		channelOtherSettings.HeaderProfileStrategy = nil
		common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channelOtherSettings)
		headerPolicyAudit = &service.RuntimeHeaderPolicyAudit{HeaderPolicyMode: "task_snapshot"}
	} else {
		var err error
		headerOverride, headerPolicyAudit, err = service.BuildChannelRuntimeHeaderOverrideWithAudit(channel)
		if err != nil {
			return types.NewError(err, types.ErrorCodeChannelHeaderOverrideInvalid)
		}
	}
	if !useSnapshotRoutePolicy {
		if mergedParam, applied := service.ApplyChannelAffinityOverrideTemplate(c, paramOverride); applied {
			paramOverride = mergedParam
		}
	}
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, paramOverride)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, headerOverride)
	if headerPolicyAudit != nil {
		common.SetContextKey(c, constant.ContextKeyChannelHeaderPolicyAudit, *headerPolicyAudit)
	}
	common.SetContextKey(c, constant.ContextKeyChannelOrganization, channelOrganization)
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if strings.TrimSpace(usingGroup) == "" {
		usingGroup = c.GetString("group")
	}
	if useSnapshotRoutePolicy {
		// The route snapshot is authoritative. Keep the mutable channel mapping
		// out of context and expose the captured effective model through the same
		// diagnostics consumed by relay helpers.
		snapshotGroup := strings.TrimSpace(snapshot.Group)
		if snapshotGroup == "" {
			snapshotGroup = strings.TrimSpace(usingGroup)
		}
		common.SetContextKey(c, constant.ContextKeyChannelModelMapping, "")
		common.SetContextKey(c, constant.ContextKeyChannelRouteDiagnostics, dto.ChannelRouteSelectionDiagnostics{
			Group:                  snapshotGroup,
			RequestedModel:         strings.TrimSpace(modelName),
			SelectedCandidateModel: strings.TrimSpace(modelName),
			EffectiveRouteModel:    strings.TrimSpace(snapshot.EffectiveUpstreamModel),
			SelectedChannelID:      channel.Id,
		})
	} else if effectiveModelOverride != "" {
		// Legacy continuations may have persisted only the effective model in
		// Properties. That value is sufficient to reconstruct the route, but the
		// channel's current mapping is not historical evidence and must not be
		// exposed to downstream helpers.
		common.SetContextKey(c, constant.ContextKeyChannelModelMapping, "")
		common.SetContextKey(c, constant.ContextKeyChannelRouteDiagnostics, dto.ChannelRouteSelectionDiagnostics{
			Group:                  strings.TrimSpace(usingGroup),
			RequestedModel:         strings.TrimSpace(modelName),
			SelectedCandidateModel: strings.TrimSpace(modelName),
			EffectiveRouteModel:    effectiveModelOverride,
			SelectedChannelID:      channel.Id,
		})
	} else {
		common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	}
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channelStatusCodeMapping)
	common.SetContextKey(c, constant.ContextKeyChannelMaxConcurrency, channel.MaxConcurrency)

	var key string
	index := 0
	if usePersistedRoutePolicy {
		// A current-schema asynchronous task owns the concrete credential that
		// was selected at submission time. Never call the live round-robin/random
		// selector here: doing so can bind a follow-up to a different key before
		// the provider-specific handler has a chance to install the persisted key.
		if snapshot.CredentialScoped != channel.ChannelInfo.IsMultiKey {
			return types.NewError(
				fmt.Errorf("task route snapshot credential scope does not match channel %d", channel.Id),
				types.ErrorCodeChannelNoAvailableKey,
				types.ErrOptionWithSkipRetry(),
			)
		}
		if resolvedPersistedCredential != "" {
			key = resolvedPersistedCredential
			if channel.ChannelInfo.IsMultiKey {
				index = snapshot.MultiKeyIndex
			}
		} else if snapshot.CredentialScoped {
			keys := channel.GetKeys()
			index = snapshot.MultiKeyIndex
			if index < 0 || index >= len(keys) {
				return types.NewError(
					fmt.Errorf("task route snapshot credential index %d is no longer available for channel %d", index, channel.Id),
					types.ErrorCodeChannelNoAvailableKey,
					types.ErrOptionWithSkipRetry(),
				)
			}
			candidate := strings.TrimSpace(keys[index])
			if candidate == "" {
				return types.NewError(
					fmt.Errorf("task route snapshot credential at index %d is empty for channel %d", index, channel.Id),
					types.ErrorCodeChannelNoAvailableKey,
					types.ErrOptionWithSkipRetry(),
				)
			}
			resolved, err := service.ResolveTaskCredential(channel, snapshot, candidate)
			if err != nil {
				return types.NewError(err, types.ErrorCodeChannelNoAvailableKey, types.ErrOptionWithSkipRetry())
			}
			key = resolved
		} else {
			// Current-schema unscoped snapshots are valid only for single-key
			// channels (checked above). Keep the original key without consulting
			// health state or mutating a polling cursor.
			key = channel.Key
		}
	} else {
		excludedKeyIndexes := service.GetChannelHealthExcludedMultiKeyIndexes(c, usingGroup, modelName, channel)
		var newAPIError *types.NewAPIError
		key, index, newAPIError = channel.GetNextEnabledKeyExcluding(excludedKeyIndexes)
		if newAPIError != nil {
			return newAPIError
		}
	}
	if channel.ChannelInfo.IsMultiKey {
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, index)
	} else {
		// 必须设置为 false，否则在重试到单个 key 的时候会导致日志显示错误
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, 0)
	}
	// c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	baseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	if usePersistedRoutePolicy {
		baseURL = strings.TrimRight(strings.TrimSpace(snapshot.BaseURL), "/")
		if baseURL == "" {
			return types.NewError(
				errors.New("task route snapshot base URL is missing"),
				types.ErrorCodeGetChannelFailed,
				types.ErrOptionWithSkipRetry(),
			)
		}
	}
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, baseURL)

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)

	// TODO: api_version统一
	if strings.TrimSpace(channelApiVersion) == "" {
		channelApiVersion = channelOther
	}
	switch channel.Type {
	case constant.ChannelTypeAzure:
		c.Set("api_version", channelApiVersion)
	case constant.ChannelTypeVertexAi:
		c.Set("region", channelApiVersion)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", channelApiVersion)
	case constant.ChannelTypeGemini:
		c.Set("api_version", channelApiVersion)
	case constant.ChannelTypeAli:
		c.Set("plugin", channelOther)
	case constant.ChannelCloudflare:
		c.Set("api_version", channelApiVersion)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", channelApiVersion)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", channelOther)
	}
	return nil
}

// SetupContextForSelectedChannelWithEffectiveModel is used by asynchronous
// follow-up submissions whose route snapshot already captured the provider
// model. Seeding the request-scoped route diagnostics before setup keeps key
// health exclusions and later route binding on that immutable model instead of
// re-reading a channel model_mapping that may have changed since submission.
func SetupContextForSelectedChannelWithEffectiveModel(c *gin.Context, channel *model.Channel, modelName string, effectiveModel string) *types.NewAPIError {
	effectiveModel = strings.TrimSpace(effectiveModel)
	if effectiveModel == "" {
		return SetupContextForSelectedChannel(c, channel, modelName)
	}
	return setupContextForSelectedChannel(c, channel, modelName, nil, "", effectiveModel)
}

// resetSelectedChannelContext clears every channel-scoped value before a retry
// installs the next candidate. A Gin context is reused across retry attempts;
// leaving a provider-specific value behind (for example api_version, region,
// plugin, bot_id, or channel_organization) can make the next channel send a
// request with the previous channel's routing identity.
func resetSelectedChannelContext(c *gin.Context, nextChannel *model.Channel, nextModel string) {
	if c == nil {
		return
	}
	// Route diagnostics and the derived scope/weight/priority keys belong to
	// the selector candidate that is about to be installed. Preserve them only
	// when the new setup is for the exact same channel and requested model;
	// otherwise stale diagnostics could make relay/health code reuse the
	// previous channel's effective model or scope.
	preserveRouteDiagnostics := false
	preserveHeaderProfileSelection := false
	if nextChannel != nil {
		if diagnostics, ok := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics); ok {
			preserveRouteDiagnostics = diagnostics.SelectedChannelID == nextChannel.Id &&
				strings.TrimSpace(diagnostics.RequestedModel) == strings.TrimSpace(nextModel) &&
				strings.TrimSpace(diagnostics.EffectiveRouteModel) != ""
		}
		if selection, ok := common.GetContextKeyType[service.RuntimeHeaderProfileSelection](c, constant.ContextKeyChannelHeaderProfileSelection); ok && selection.ChannelID == nextChannel.Id {
			attemptIndex := -1
			if value, attemptOK := common.GetContextKeyType[int](c, constant.ContextKeyChannelHeaderProfileAttempt); attemptOK {
				attemptIndex = value
			}
			preserveHeaderProfileSelection = service.RuntimeHeaderProfileAttemptMatches(selection.AttemptIndex, attemptIndex)
		}
	}
	zeroValues := map[constant.ContextKey]any{
		constant.ContextKeyChannelId:                           0,
		constant.ContextKeyChannelName:                         "",
		constant.ContextKeyChannelType:                         0,
		constant.ContextKeyChannelCreateTime:                   int64(0),
		constant.ContextKeyChannelBaseUrl:                      "",
		constant.ContextKeyChannelSetting:                      dto.ChannelSettings{},
		constant.ContextKeyChannelOtherSetting:                 dto.ChannelOtherSettings{},
		constant.ContextKeyChannelOtherSettingRaw:              "",
		constant.ContextKeyChannelParamOverride:                map[string]any(nil),
		constant.ContextKeyChannelHeaderOverride:               map[string]any(nil),
		constant.ContextKeyChannelHeaderPolicyAudit:            service.RuntimeHeaderPolicyAudit{},
		constant.ContextKeyChannelHeaderProfileSelection:       service.RuntimeHeaderProfileSelection{},
		constant.ContextKeyChannelOrganization:                 "",
		constant.ContextKeyChannelAutoBan:                      false,
		constant.ContextKeyChannelModelMapping:                 "",
		constant.ContextKeyChannelStatusCodeMapping:            "",
		constant.ContextKeyChannelIsMultiKey:                   false,
		constant.ContextKeyChannelMultiKeyIndex:                0,
		constant.ContextKeyChannelKey:                          "",
		constant.ContextKeyChannelMaxConcurrency:               0,
		constant.ContextKeySelectedChannelSnapshot:             (*model.Channel)(nil),
		constant.ContextKeySystemPromptOverride:                false,
		constant.ContextKeyChannelRouteScope:                   "",
		constant.ContextKeyChannelRouteWeightBPS:               uint(0),
		constant.ContextKeyChannelRoutePriority:                int64(0),
		constant.ContextKeyChannelRouteDiagnostics:             dto.ChannelRouteSelectionDiagnostics{},
		constant.ContextKeyChannelRouteModelMappingProvisional: false,
	}
	if preserveRouteDiagnostics {
		diagnostics, _ := common.GetContextKeyType[dto.ChannelRouteSelectionDiagnostics](c, constant.ContextKeyChannelRouteDiagnostics)
		zeroValues[constant.ContextKeyChannelRouteScope] = diagnostics.RouteScope
		zeroValues[constant.ContextKeyChannelRouteWeightBPS] = diagnostics.RouteWeightBPS
		zeroValues[constant.ContextKeyChannelRoutePriority] = diagnostics.RoutePriority
		zeroValues[constant.ContextKeyChannelRouteDiagnostics] = diagnostics
	}
	if preserveHeaderProfileSelection {
		selection, _ := common.GetContextKeyType[service.RuntimeHeaderProfileSelection](c, constant.ContextKeyChannelHeaderProfileSelection)
		zeroValues[constant.ContextKeyChannelHeaderProfileSelection] = selection
	}
	for key, value := range zeroValues {
		common.SetContextKey(c, key, value)
	}
	for _, key := range []string{"api_version", "region", "plugin", "bot_id", "channel_other", "channel_organization", "model_mapping", "request_model"} {
		c.Set(key, "")
	}
}

// extractModelNameFromGeminiPath 从 Gemini API URL 路径中提取模型名
// 输入格式: /v1beta/models/gemini-2.0-flash:generateContent
// 输出: gemini-2.0-flash
func extractModelNameFromGeminiPath(path string) string {
	// 查找 "/models/" 的位置
	modelsPrefix := "/models/"
	modelsIndex := strings.Index(path, modelsPrefix)
	if modelsIndex == -1 {
		return ""
	}

	// 从 "/models/" 之后开始提取
	startIndex := modelsIndex + len(modelsPrefix)
	if startIndex >= len(path) {
		return ""
	}

	// 查找 ":" 的位置，模型名在 ":" 之前
	colonIndex := strings.Index(path[startIndex:], ":")
	if colonIndex == -1 {
		// 如果没有找到 ":"，返回从 "/models/" 到路径结尾的部分
		return path[startIndex:]
	}

	// 返回模型名部分
	return path[startIndex : startIndex+colonIndex]
}
