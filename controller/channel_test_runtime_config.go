package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type channelTestOptions struct {
	UseRuntimeConfig bool
	UseHeaderConfig  bool
	UseParamOverride bool
	UseProxy         bool
	UseModelMapping  bool
	ResponseProtocol string
	TestPrompt       string
	MaxTokens        *uint
	SourceHeaders    map[string]string
}

const (
	channelTestDefaultPrompt                   = "hi"
	channelTestResponseProtocolNative          = "native"
	channelTestResponseProtocolChatCompletions = "chat_completions"
)

func defaultChannelTestOptions() channelTestOptions {
	return channelTestOptions{
		UseRuntimeConfig: true,
		UseHeaderConfig:  true,
		UseParamOverride: true,
		UseProxy:         true,
		UseModelMapping:  true,
		ResponseProtocol: channelTestResponseProtocolNative,
		TestPrompt:       channelTestDefaultPrompt,
	}
}

func normalizeChannelTestOptions(options channelTestOptions) channelTestOptions {
	options.ResponseProtocol = strings.TrimSpace(options.ResponseProtocol)
	if options.ResponseProtocol != channelTestResponseProtocolChatCompletions {
		options.ResponseProtocol = channelTestResponseProtocolNative
	}
	options.TestPrompt = strings.TrimSpace(options.TestPrompt)
	if options.TestPrompt == "" {
		options.TestPrompt = channelTestDefaultPrompt
	}
	if options.MaxTokens != nil && *options.MaxTokens == 0 {
		options.MaxTokens = nil
	}
	if !options.UseRuntimeConfig {
		options.UseHeaderConfig = false
		options.UseParamOverride = false
		options.UseProxy = false
		options.UseModelMapping = false
	}
	return options
}

func parseChannelTestOptions(c *gin.Context) channelTestOptions {
	options := defaultChannelTestOptions()
	options.UseRuntimeConfig = getChannelTestQueryBool(c, options.UseRuntimeConfig, "runtime_config", "use_runtime_config")
	options.UseHeaderConfig = getChannelTestQueryBool(c, options.UseHeaderConfig, "header_config", "use_header_config", "header_override")
	options.UseParamOverride = getChannelTestQueryBool(c, options.UseParamOverride, "param_override", "use_param_override")
	options.UseProxy = getChannelTestQueryBool(c, options.UseProxy, "proxy", "use_proxy")
	options.UseModelMapping = getChannelTestQueryBool(c, options.UseModelMapping, "model_mapping", "use_model_mapping")
	options.ResponseProtocol = getChannelTestQueryString(c, options.ResponseProtocol, "response_protocol", "protocol_strategy")
	options.TestPrompt = getChannelTestQueryString(c, options.TestPrompt, "test_prompt", "prompt")
	options.MaxTokens = getChannelTestQueryUint(c, options.MaxTokens, "max_tokens", "max_output_tokens")
	options.SourceHeaders = collectChannelTestSourceHeaders(c)
	return normalizeChannelTestOptions(options)
}

func collectChannelTestSourceHeaders(c *gin.Context) map[string]string {
	if c == nil || c.Request == nil || len(c.Request.Header) == 0 {
		return nil
	}
	headers := make(map[string]string)
	for key := range c.Request.Header {
		name := strings.TrimSpace(key)
		value := strings.TrimSpace(c.Request.Header.Get(key))
		if name == "" || value == "" || isSensitiveChannelTestSourceHeader(name) {
			continue
		}
		headers[name] = value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func isSensitiveChannelTestSourceHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "cookie", "new-api-user", "proxy-authorization", "x-api-key":
		return true
	default:
		return false
	}
}

func getChannelTestQueryBool(c *gin.Context, defaultValue bool, keys ...string) bool {
	if c == nil {
		return defaultValue
	}
	for _, key := range keys {
		raw, exists := c.GetQuery(key)
		if !exists {
			continue
		}
		parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getChannelTestQueryString(c *gin.Context, defaultValue string, keys ...string) string {
	if c == nil {
		return defaultValue
	}
	for _, key := range keys {
		raw, exists := c.GetQuery(key)
		if !exists {
			continue
		}
		return strings.TrimSpace(raw)
	}
	return defaultValue
}

func getChannelTestQueryUint(c *gin.Context, defaultValue *uint, keys ...string) *uint {
	if c == nil {
		return defaultValue
	}
	for _, key := range keys {
		raw, exists := c.GetQuery(key)
		if !exists {
			continue
		}
		parsed, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 32)
		if err != nil {
			return defaultValue
		}
		value := uint(parsed)
		return &value
	}
	return defaultValue
}

func parseChannelTestSetting(channel *model.Channel) (dto.ChannelSettings, error) {
	setting := dto.ChannelSettings{}
	if channel == nil || channel.Setting == nil || strings.TrimSpace(*channel.Setting) == "" {
		return setting, nil
	}
	if err := common.Unmarshal([]byte(*channel.Setting), &setting); err != nil {
		return setting, err
	}
	return setting, nil
}

func parseChannelTestOtherSettings(channel *model.Channel) (dto.ChannelOtherSettings, error) {
	settings := dto.ChannelOtherSettings{}
	if channel == nil || strings.TrimSpace(channel.OtherSettings) == "" {
		return settings, nil
	}
	if err := common.UnmarshalJsonStr(channel.OtherSettings, &settings); err != nil {
		return settings, err
	}
	return settings, nil
}

func marshalChannelTestSetting(setting dto.ChannelSettings) (*string, error) {
	raw, err := common.Marshal(setting)
	if err != nil {
		return nil, err
	}
	return common.GetPointer(string(raw)), nil
}

func marshalChannelTestOtherSettings(settings dto.ChannelOtherSettings) (string, error) {
	raw, err := common.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func buildChannelForTestOptions(channel *model.Channel, options channelTestOptions) (*model.Channel, error) {
	if channel == nil {
		return nil, nil
	}
	options = normalizeChannelTestOptions(options)
	cloned := *channel

	if !options.UseProxy {
		setting, err := parseChannelTestSetting(channel)
		if err != nil {
			return nil, fmt.Errorf("channel setting invalid: %w", err)
		}
		setting.Proxy = ""
		rawSetting, err := marshalChannelTestSetting(setting)
		if err != nil {
			return nil, err
		}
		cloned.Setting = rawSetting
	}

	if !options.UseParamOverride {
		cloned.ParamOverride = nil
	}

	if !options.UseHeaderConfig {
		cloned.HeaderOverride = nil
		settings, err := parseChannelTestOtherSettings(channel)
		if err != nil {
			return nil, fmt.Errorf("channel settings invalid: %w", err)
		}
		settings.HeaderPolicyMode = ""
		settings.OverrideHeaderUserAgent = false
		settings.UserAgentStrategy = nil
		settings.HeaderProfileStrategy = nil
		rawSettings, err := marshalChannelTestOtherSettings(settings)
		if err != nil {
			return nil, err
		}
		cloned.OtherSettings = rawSettings
	}

	if !options.UseModelMapping {
		cloned.ModelMapping = nil
	}

	return &cloned, nil
}

func prepareChannelTestRequestHeaders(c *gin.Context, channel *model.Channel) (string, error) {
	if c == nil || c.Request == nil || channel == nil {
		return "", nil
	}
	settings, err := parseChannelTestOtherSettings(channel)
	if err != nil {
		return "", fmt.Errorf("channel settings invalid: %w", err)
	}
	strategy := settings.HeaderProfileStrategy
	if strategy == nil || !strategy.Enabled {
		return "", nil
	}

	headers, profileID, err := dto.ResolveHeaderProfileStrategyHeaders(strategy, 0)
	if err != nil {
		return "", err
	}
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		c.Request.Header.Set(key, value)
	}

	if profile, exists := dto.ResolveHeaderProfile(profileID, strategy.Profiles); exists {
		settings.HeaderProfileStrategy = &dto.HeaderProfileStrategy{
			Enabled:            true,
			Mode:               dto.HeaderProfileModeFixed,
			SelectedProfileIDs: []string{profileID},
			Profiles:           []dto.HeaderProfile{profile},
		}
		rawSettings, err := marshalChannelTestOtherSettings(settings)
		if err != nil {
			return "", err
		}
		channel.OtherSettings = rawSettings
	}

	return profileID, nil
}
