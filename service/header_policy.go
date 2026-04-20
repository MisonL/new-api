package service

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

func ValidateHeaderTemplate(raw string) (map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	entries, err := parseHeaderTemplateEntries(trimmed)
	if err != nil {
		return nil, err
	}

	normalized := make(map[string]string, len(entries))
	for _, entry := range entries {
		normalized[entry.name] = entry.value
	}

	return normalized, nil
}

type headerTemplateEntry struct {
	name  string
	value string
}

func parseHeaderTemplateEntries(raw string) ([]headerTemplateEntry, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("请求头覆盖必须是合法的 JSON 格式")
	}

	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, fmt.Errorf("请求头覆盖必须是 JSON 对象")
	}

	entries := make([]headerTemplateEntry, 0)
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("请求头覆盖必须是合法的 JSON 格式")
		}

		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("请求头覆盖必须是 JSON 对象")
		}

		name := strings.TrimSpace(key)
		if name == "" {
			return nil, fmt.Errorf("请求头名称不能为空")
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("请求头名称规整后重复: %s", name)
		}

		value, err := decodeHeaderTemplateValue(decoder, name)
		if err != nil {
			return nil, err
		}

		seen[name] = struct{}{}
		entries = append(entries, headerTemplateEntry{
			name:  name,
			value: value,
		})
	}

	endToken, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("请求头覆盖必须是合法的 JSON 格式")
	}
	endDelim, ok := endToken.(json.Delim)
	if !ok || endDelim != '}' {
		return nil, fmt.Errorf("请求头覆盖必须是合法的 JSON 格式")
	}

	rest, err := decoder.Token()
	if err != io.EOF {
		return nil, fmt.Errorf("请求头覆盖必须是合法的 JSON 格式")
	}
	_ = rest

	return entries, nil
}

func decodeHeaderTemplateValue(decoder *json.Decoder, name string) (string, error) {
	var value interface{}
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("请求头覆盖必须是合法的 JSON 格式")
	}

	switch v := value.(type) {
	case string:
		return v, nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case json.Number:
		return v.String(), nil
	default:
		return "", fmt.Errorf("请求头值类型不受支持: %s", name)
	}
}

func MergeUserAgents(base []string, extra []string) []string {
	merged := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]struct{}, len(base)+len(extra))
	appendUnique := func(items []string) {
		for _, item := range items {
			value := strings.TrimSpace(item)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}

	appendUnique(base)
	appendUnique(extra)

	return merged
}

type UserAgentStrategyState string

const (
	UserAgentStrategyStateUnconfigured UserAgentStrategyState = "unconfigured"
	UserAgentStrategyStateDisabled     UserAgentStrategyState = "disabled"
	UserAgentStrategyStateInvalid      UserAgentStrategyState = "invalid"
	UserAgentStrategyStateNormalized   UserAgentStrategyState = "normalized"
)

type UserAgentStrategyResult struct {
	State    UserAgentStrategyState
	Strategy *dto.UserAgentStrategy
}

func ResolveUserAgentStrategy(strategy *dto.UserAgentStrategy) (*UserAgentStrategyResult, error) {
	if strategy == nil {
		return &UserAgentStrategyResult{State: UserAgentStrategyStateUnconfigured}, nil
	}

	mode := strings.TrimSpace(strategy.Mode)
	if mode != "" {
		switch mode {
		case "round_robin", "random":
		default:
			return &UserAgentStrategyResult{State: UserAgentStrategyStateInvalid}, fmt.Errorf("UA 策略模式不合法: %s", strategy.Mode)
		}
	}

	userAgents := MergeUserAgents(strategy.UserAgents, nil)
	if !strategy.Enabled {
		if len(strategy.UserAgents) > 0 && len(userAgents) == 0 {
			return &UserAgentStrategyResult{State: UserAgentStrategyStateInvalid}, fmt.Errorf("UA 列表规整后不能为空")
		}
		return &UserAgentStrategyResult{State: UserAgentStrategyStateDisabled}, nil
	}

	if mode == "" {
		return &UserAgentStrategyResult{State: UserAgentStrategyStateInvalid}, fmt.Errorf("UA 策略启用后必须提供合法的模式")
	}
	if len(userAgents) == 0 {
		return &UserAgentStrategyResult{State: UserAgentStrategyStateInvalid}, fmt.Errorf("UA 策略启用后必须提供至少一个 UA")
	}

	normalized := &dto.UserAgentStrategy{
		Enabled:    true,
		Mode:       mode,
		UserAgents: userAgents,
	}

	return &UserAgentStrategyResult{
		State:    UserAgentStrategyStateNormalized,
		Strategy: normalized,
	}, nil
}

func NormalizeUserAgentStrategy(strategy *dto.UserAgentStrategy) (*dto.UserAgentStrategy, error) {
	result, err := ResolveUserAgentStrategy(strategy)
	if err != nil || result == nil {
		return nil, err
	}
	return result.Strategy, nil
}
