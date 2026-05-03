package controller

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func validateHeaderProfilePassthrough(channel *model.Channel, strategy *dto.HeaderProfileStrategy) error {
	if channel == nil || strategy == nil || !strategy.Enabled {
		return nil
	}
	requiredHeaders := collectRequiredHeaderProfilePassthroughHeaders(strategy)
	if len(requiredHeaders) == 0 {
		return nil
	}
	passedHeaders, err := collectParamOverridePassHeaders(channel.ParamOverride)
	if err != nil {
		return fmt.Errorf("param_override pass_headers 格式错误: %s", err.Error())
	}
	return requireParamOverridePassHeaders(requiredHeaders, passedHeaders)
}

func collectRequiredHeaderProfilePassthroughHeaders(strategy *dto.HeaderProfileStrategy) []string {
	headers := newHeaderNameSet()
	for _, profileID := range strategy.SelectedProfileIDs {
		profile, exists := resolveHeaderProfileForPassthrough(profileID, strategy.Profiles)
		if !exists || !profile.PassthroughRequired {
			continue
		}
		headers.add(requiredPassthroughHeadersForProfile(profile)...)
	}
	return headers.values
}

func resolveHeaderProfileForPassthrough(profileID string, profiles []dto.HeaderProfile) (dto.HeaderProfile, bool) {
	trimmedID := strings.TrimSpace(profileID)
	if trimmedID == "" {
		return dto.HeaderProfile{}, false
	}
	if _, ok := operation_setting.HeaderProfilePassThroughHeaders[trimmedID]; ok {
		if builtinProfile, exists := dto.ResolveHeaderProfile(trimmedID, nil); exists {
			return builtinProfile, true
		}
	}
	return dto.ResolveHeaderProfile(trimmedID, profiles)
}

func requiredPassthroughHeadersForProfile(profile dto.HeaderProfile) []string {
	if headers, ok := operation_setting.HeaderProfilePassThroughHeaders[strings.TrimSpace(profile.ID)]; ok {
		return headers
	}
	headers := make([]string, 0, len(profile.Headers))
	for header := range profile.Headers {
		headers = append(headers, header)
	}
	sort.Strings(headers)
	return headers
}

func requireParamOverridePassHeaders(required []string, passed map[string]struct{}) error {
	missing := make([]string, 0)
	for _, header := range required {
		if _, exists := passed[strings.ToLower(header)]; exists {
			continue
		}
		missing = append(missing, header)
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("header_profile_strategy 选择了需要真实客户端请求头透传的 Profile，必须在 param_override.operations 配置 pass_headers: %s", strings.Join(missing, ", "))
}

func collectParamOverridePassHeaders(raw *string) (map[string]struct{}, error) {
	headers := map[string]struct{}{}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return headers, nil
	}

	var parsed map[string]interface{}
	if err := common.Unmarshal([]byte(*raw), &parsed); err != nil {
		return nil, err
	}
	for _, operation := range paramOverrideOperations(parsed) {
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", operation["mode"])), "pass_headers") {
			continue
		}
		if paramOverrideOperationIsConditional(operation) {
			continue
		}
		names, err := parseParamOverridePassHeaderNames(operation["value"])
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			headers[strings.ToLower(name)] = struct{}{}
		}
	}
	return headers, nil
}

func paramOverrideOperationIsConditional(operation map[string]interface{}) bool {
	if rawConditions, exists := operation["conditions"]; exists {
		if rawConditions == nil {
			// treat explicit null as absent
		} else if conditions, ok := rawConditions.([]interface{}); ok {
			if len(conditions) > 0 {
				return true
			}
		} else {
			return true
		}
	}
	if rawWhen, exists := operation["when"]; exists {
		switch value := rawWhen.(type) {
		case nil:
			return false
		case string:
			return strings.TrimSpace(value) != ""
		default:
			return true
		}
	}
	return false
}

func paramOverrideOperations(parsed map[string]interface{}) []map[string]interface{} {
	rawOperations, _ := parsed["operations"].([]interface{})
	operations := make([]map[string]interface{}, 0, len(rawOperations))
	for _, raw := range rawOperations {
		operation, ok := raw.(map[string]interface{})
		if ok {
			operations = append(operations, operation)
		}
	}
	return operations
}

func parseParamOverridePassHeaderNames(value interface{}) ([]string, error) {
	switch raw := value.(type) {
	case nil:
		return nil, fmt.Errorf("pass_headers value is required")
	case string:
		return parseParamOverridePassHeaderString(raw)
	case []interface{}:
		values := make([]string, 0, len(raw))
		for _, item := range raw {
			values = append(values, fmt.Sprintf("%v", item))
		}
		return normalizeParamOverridePassHeaderNames(values)
	case map[string]interface{}:
		return parseParamOverridePassHeaderObject(raw)
	default:
		return nil, fmt.Errorf("pass_headers value must be string, array or object")
	}
}

func parseParamOverridePassHeaderString(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("pass_headers value is required")
	}
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var parsed interface{}
		if err := common.UnmarshalJsonStr(trimmed, &parsed); err != nil {
			return nil, err
		}
		return parseParamOverridePassHeaderNames(parsed)
	}
	return normalizeParamOverridePassHeaderNames(strings.Split(trimmed, ","))
}

func parseParamOverridePassHeaderObject(raw map[string]interface{}) ([]string, error) {
	values := make([]string, 0)
	for _, key := range []string{"headers", "names", "header"} {
		value, exists := raw[key]
		if !exists {
			continue
		}
		names, err := parseParamOverridePassHeaderNames(value)
		if err != nil {
			return nil, fmt.Errorf("pass_headers value.%s is invalid: %s", key, err.Error())
		}
		values = append(values, names...)
	}
	return normalizeParamOverridePassHeaderNames(values)
}

func normalizeParamOverridePassHeaderNames(values []string) ([]string, error) {
	names := newHeaderNameSet()
	names.add(values...)
	if len(names.values) == 0 {
		return nil, fmt.Errorf("pass_headers value is invalid")
	}
	return names.values, nil
}

type headerNameSet struct {
	seen   map[string]struct{}
	values []string
}

func newHeaderNameSet() headerNameSet {
	return headerNameSet{seen: map[string]struct{}{}}
}

func (set *headerNameSet) add(values ...string) {
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := set.seen[key]; exists {
			continue
		}
		set.seen[key] = struct{}{}
		set.values = append(set.values, name)
	}
}
