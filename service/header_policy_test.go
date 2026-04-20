package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestValidateHeaderTemplateRejectsInvalidJSON(t *testing.T) {
	_, err := ValidateHeaderTemplate("{bad json")
	require.Error(t, err)
	require.ErrorContains(t, err, "合法的 JSON 格式")
}

func TestValidateHeaderTemplateRejectsJSONArray(t *testing.T) {
	_, err := ValidateHeaderTemplate(`["x"]`)
	require.Error(t, err)
	require.ErrorContains(t, err, "JSON 对象")
}

func TestValidateHeaderTemplateReturnsNilForBlankInput(t *testing.T) {
	normalized, err := ValidateHeaderTemplate("   \n\t  ")
	require.NoError(t, err)
	require.Nil(t, normalized)
}

func TestValidateHeaderTemplateReturnsEmptyMapForEmptyJSONObject(t *testing.T) {
	normalized, err := ValidateHeaderTemplate(`{}`)
	require.NoError(t, err)
	require.Empty(t, normalized)
}

func TestValidateHeaderTemplateRejectsBlankKey(t *testing.T) {
	_, err := ValidateHeaderTemplate(`{"   ":"x"}`)
	require.Error(t, err)
	require.ErrorContains(t, err, "请求头名称不能为空")
}

func TestValidateHeaderTemplateRejectsTrimmedKeyConflict(t *testing.T) {
	_, err := ValidateHeaderTemplate(`{"X-Test":"a"," X-Test ":"b"}`)
	require.Error(t, err)
	require.ErrorContains(t, err, "规整后重复")
}

func TestValidateHeaderTemplateRejectsCaseInsensitiveKeyConflict(t *testing.T) {
	_, err := ValidateHeaderTemplate(`{"User-Agent":"a","user-agent":"b"}`)
	require.Error(t, err)
	require.ErrorContains(t, err, "规整后重复")
}

func TestValidateHeaderTemplateRejectsInvalidHeaderName(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "space", raw: `{"Bad Header":"x"}`},
		{name: "control", raw: `{"\u0001bad":"x"}`},
		{name: "separator", raw: `{"Bad:Header":"x"}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateHeaderTemplate(tc.raw)
			require.Error(t, err)
			require.ErrorContains(t, err, "请求头名称不合法")
		})
	}
}

func TestValidateHeaderTemplateRejectsUnsupportedValueTypes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "null", raw: `{"X-Test":null}`},
		{name: "object", raw: `{"X-Test":{"nested":true}}`},
		{name: "array", raw: `{"X-Test":[1,2]}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateHeaderTemplate(tc.raw)
			require.Error(t, err)
			require.ErrorContains(t, err, "请求头值类型不受支持")
		})
	}
}

func TestValidateHeaderTemplateNormalizesValidJSONObject(t *testing.T) {
	normalized, err := ValidateHeaderTemplate(`{
		"Authorization": "Bearer token",
		"X-Enabled": true,
		"X-Retry": 2.50,
		"user-agent": "client/1.0"
	}`)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"Authorization": "Bearer token",
		"X-Enabled":     "true",
		"X-Retry":       "2.50",
		"User-Agent":    "client/1.0",
	}, normalized)
}

func TestMergeUserAgentsDeduplicatesAndTrimsInput(t *testing.T) {
	merged := MergeUserAgents([]string{" a ", "b"}, []string{"b ", " c ", "a"})
	require.Equal(t, []string{"a", "b", "c"}, merged)
}

func TestResolveUserAgentStrategyReturnsStateForUnconfiguredAndDisabled(t *testing.T) {
	result, err := ResolveUserAgentStrategy(nil)
	require.NoError(t, err)
	require.Equal(t, UserAgentStrategyStateUnconfigured, result.State)
	require.Nil(t, result.Strategy)

	result, err = ResolveUserAgentStrategy(&dto.UserAgentStrategy{Enabled: false})
	require.NoError(t, err)
	require.Equal(t, UserAgentStrategyStateDisabled, result.State)
	require.Nil(t, result.Strategy)

	result, err = ResolveUserAgentStrategy(&dto.UserAgentStrategy{
		Enabled:    false,
		Mode:       " random ",
		UserAgents: []string{" ua-1 ", "ua-1"},
	})
	require.NoError(t, err)
	require.Equal(t, UserAgentStrategyStateDisabled, result.State)
	require.Nil(t, result.Strategy)
}

func TestResolveUserAgentStrategyRejectsInvalidConfig(t *testing.T) {
	cases := []struct {
		name string
		in   *dto.UserAgentStrategy
		want string
	}{
		{
			name: "blank mode",
			in: &dto.UserAgentStrategy{
				Enabled:    true,
				Mode:       "   ",
				UserAgents: []string{"a"},
			},
			want: "UA 策略启用后必须提供合法的模式",
		},
		{
			name: "blank user agents",
			in: &dto.UserAgentStrategy{
				Enabled:    true,
				Mode:       "random",
				UserAgents: []string{" ", "\t"},
			},
			want: "至少一个 UA",
		},
		{
			name: "disabled with invalid mode",
			in: &dto.UserAgentStrategy{
				Enabled:    false,
				Mode:       "invalid",
				UserAgents: []string{"a"},
			},
			want: "UA 策略模式不合法",
		},
		{
			name: "disabled with blank user agents only",
			in: &dto.UserAgentStrategy{
				Enabled:    false,
				UserAgents: []string{" ", "\t"},
			},
			want: "UA 列表规整后不能为空",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result, err := ResolveUserAgentStrategy(tc.in)
			require.Error(t, err)
			require.ErrorContains(t, err, tc.want)
			require.NotNil(t, result)
			require.Equal(t, UserAgentStrategyStateInvalid, result.State)
			require.Nil(t, result.Strategy)
		})
	}
}

func TestResolveUserAgentStrategyNormalizesAndDeduplicates(t *testing.T) {
	result, err := ResolveUserAgentStrategy(&dto.UserAgentStrategy{
		Enabled: true,
		Mode:    " round_robin ",
		UserAgents: []string{
			" a ",
			"",
			"b",
			"a",
			"c",
			"b",
		},
	})

	require.NoError(t, err)
	require.Equal(t, UserAgentStrategyStateNormalized, result.State)
	require.Equal(t, &dto.UserAgentStrategy{
		Enabled:    true,
		Mode:       "round_robin",
		UserAgents: []string{"a", "b", "c"},
	}, result.Strategy)
}

func TestNormalizeUserAgentStrategyReturnsNormalizedStrategy(t *testing.T) {
	normalized, err := NormalizeUserAgentStrategy(&dto.UserAgentStrategy{
		Enabled: true,
		Mode:    "random",
		UserAgents: []string{
			"ua-1",
			"ua-1",
			" ua-2 ",
		},
	})

	require.NoError(t, err)
	require.Equal(t, &dto.UserAgentStrategy{
		Enabled:    true,
		Mode:       "random",
		UserAgents: []string{"ua-1", "ua-2"},
	}, normalized)
}
