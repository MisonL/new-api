package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func buildChannelAffinityTemplateContextForTest(meta channelAffinityMeta) *gin.Context {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	setChannelAffinityContext(ctx, meta)
	return ctx
}

func TestCliHeaderPassthroughTemplateDefinitions(t *testing.T) {
	require.Equal(t, []string{
		"User-Agent",
		"Originator",
		"Session_id",
		"Session-Id",
		"Thread-Id",
		"X-Codex-Beta-Features",
		"X-Codex-Turn-Metadata",
		"X-Codex-Window-Id",
		"X-Client-Request-Id",
	}, operation_setting.CodexCliPassThroughHeaders)
	require.Equal(t, operation_setting.CodexCliPassThroughHeaders, operation_setting.CodexDesktopPassThroughHeaders)
	require.Equal(t, []string{
		"X-Claude-Code-Session-Id",
		"X-Stainless-Arch",
		"X-Stainless-Lang",
		"X-Stainless-OS",
		"X-Stainless-Package-Version",
		"X-Stainless-Retry-Count",
		"X-Stainless-Runtime",
		"X-Stainless-Runtime-Version",
		"X-Stainless-Timeout",
		"X-App",
		"Anthropic-Beta",
		"Anthropic-Dangerous-Direct-Browser-Access",
		"Anthropic-Version",
	}, operation_setting.ClaudeCliPassThroughHeaders)
	require.Equal(t, []string{
		"X-Stainless-Arch",
		"X-Stainless-Lang",
		"X-Stainless-OS",
		"X-Stainless-Package-Version",
		"X-Stainless-Retry-Count",
		"X-Stainless-Runtime",
		"X-Stainless-Runtime-Version",
	}, operation_setting.QwenCodeCliPassThroughHeaders)
	require.Equal(t, []string{
		"X-Stainless-Arch",
		"X-Stainless-Lang",
		"X-Stainless-OS",
		"X-Stainless-Package-Version",
		"X-Stainless-Retry-Count",
		"X-Stainless-Runtime",
		"X-Stainless-Runtime-Version",
	}, operation_setting.DroidCliPassThroughHeaders)
	require.Equal(t, []string{
		"X-Goog-Api-Client",
	}, operation_setting.GeminiCliPassThroughHeaders)
	require.Equal(t, operation_setting.DroidCliPassThroughHeaders, operation_setting.HeaderProfilePassThroughHeaders["droid"])
	require.Equal(t, operation_setting.CodexDesktopPassThroughHeaders, operation_setting.HeaderProfilePassThroughHeaders["codex-desktop"])
	_, exists := operation_setting.HeaderProfilePassThroughHeaders["opencode"]
	require.False(t, exists)
}

func TestCodexHeaderPassthroughTemplateIncludesSessionFallback(t *testing.T) {
	template := operation_setting.BuildCodexHeaderPassthroughTemplate(operation_setting.CodexCliPassThroughHeaders)
	ops, ok := template["operations"].([]map[string]interface{})
	require.True(t, ok)
	require.Len(t, ops, 2)

	require.Equal(t, map[string]interface{}{
		"mode":        "pass_headers",
		"value":       operation_setting.CodexCliPassThroughHeaders,
		"keep_origin": true,
	}, ops[0])
	require.Equal(t, map[string]interface{}{
		"mode":        "copy_header",
		"from":        "X-Client-Request-Id",
		"to":          "Session_id",
		"keep_origin": true,
	}, ops[1])
}

func TestChannelAffinitySettingDoesNotRegisterOpenCodeRuleWithoutRuntimeSource(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)

	for _, rule := range setting.Rules {
		require.NotEqual(t, "opencode cli trace", strings.TrimSpace(rule.Name))
		require.Empty(t, rule.ParamOverrideTemplate)
	}
}

func TestApplyChannelAffinityOverrideTemplate_NoTemplate(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-no-template",
	})
	base := map[string]interface{}{
		"temperature": 0.7,
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.False(t, applied)
	require.Equal(t, base, merged)
}

func TestApplyChannelAffinityOverrideTemplate_MergeTemplate(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-with-template",
		ParamTemplate: map[string]interface{}{
			"temperature": 0.2,
			"top_p":       0.95,
		},
		UsingGroup:     "default",
		ModelName:      "gpt-4.1",
		RequestPath:    "/v1/responses",
		KeySourceType:  "gjson",
		KeySourcePath:  "prompt_cache_key",
		KeyHint:        "abcd...wxyz",
		KeyFingerprint: "abcd1234",
	})
	base := map[string]interface{}{
		"temperature": 0.7,
		"max_tokens":  2000,
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.True(t, applied)
	require.Equal(t, 0.7, merged["temperature"])
	require.Equal(t, 0.95, merged["top_p"])
	require.Equal(t, 2000, merged["max_tokens"])
	require.Equal(t, 0.7, base["temperature"])

	anyInfo, ok := ctx.Get(ginKeyChannelAffinityLogInfo)
	require.True(t, ok)
	info, ok := anyInfo.(map[string]interface{})
	require.True(t, ok)
	overrideInfoAny, ok := info["override_template"]
	require.True(t, ok)
	overrideInfo, ok := overrideInfoAny.(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, true, overrideInfo["applied"])
	require.Equal(t, "rule-with-template", overrideInfo["rule_name"])
	require.EqualValues(t, 2, overrideInfo["param_override_keys"])
}

func TestApplyChannelAffinityOverrideTemplate_MergeOperations(t *testing.T) {
	ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
		RuleName: "rule-with-ops-template",
		ParamTemplate: map[string]interface{}{
			"operations": []map[string]interface{}{
				{
					"mode":  "pass_headers",
					"value": []string{"Originator"},
				},
			},
		},
	})
	base := map[string]interface{}{
		"temperature": 0.7,
		"operations": []map[string]interface{}{
			{
				"path":  "model",
				"mode":  "trim_prefix",
				"value": "openai/",
			},
		},
	}

	merged, applied := ApplyChannelAffinityOverrideTemplate(ctx, base)
	require.True(t, applied)
	require.Equal(t, 0.7, merged["temperature"])

	opsAny, ok := merged["operations"]
	require.True(t, ok)
	ops, ok := opsAny.([]interface{})
	require.True(t, ok)
	require.Len(t, ops, 2)

	firstOp, ok := ops[0].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "pass_headers", firstOp["mode"])

	secondOp, ok := ops[1].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, "trim_prefix", secondOp["mode"])
}

func TestShouldSkipRetryAfterChannelAffinityFailure(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() *gin.Context
		want bool
	}{
		{
			name: "nil context",
			ctx: func() *gin.Context {
				return nil
			},
			want: false,
		},
		{
			name: "explicit skip retry flag in context",
			ctx: func() *gin.Context {
				ctx := buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-explicit-flag",
					SkipRetry:  false,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
				ctx.Set(ginKeyChannelAffinitySkipRetry, true)
				return ctx
			},
			want: true,
		},
		{
			name: "fallback to matched rule meta",
			ctx: func() *gin.Context {
				return buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-skip-retry",
					SkipRetry:  true,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
			},
			want: true,
		},
		{
			name: "no flag and no skip retry meta",
			ctx: func() *gin.Context {
				return buildChannelAffinityTemplateContextForTest(channelAffinityMeta{
					RuleName:   "rule-no-skip-retry",
					SkipRetry:  false,
					UsingGroup: "default",
					ModelName:  "gpt-5",
				})
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ShouldSkipRetryAfterChannelAffinityFailure(tt.ctx()))
		})
	}
}

func TestChannelAffinityHitCodexDoesNotInjectPassHeadersByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)

	var codexRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		rule := &setting.Rules[i]
		if strings.EqualFold(strings.TrimSpace(rule.Name), "codex cli trace") {
			codexRule = rule
			break
		}
	}
	require.NotNil(t, codexRule)

	affinityValue := fmt.Sprintf("pc-hit-%d", time.Now().UnixNano())
	cacheKeySuffix := buildChannelAffinityCacheKeySuffix(*codexRule, "gpt-5", "default", affinityValue)

	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(cacheKeySuffix, 9527, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany([]string{cacheKeySuffix})
	})

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"prompt_cache_key":"%s"}`, affinityValue)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	channelID, found := GetPreferredChannelByAffinity(ctx, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 9527, channelID)

	baseOverride := map[string]interface{}{
		"temperature": 0.2,
	}
	mergedOverride, applied := ApplyChannelAffinityOverrideTemplate(ctx, baseOverride)
	require.False(t, applied)
	require.Equal(t, 0.2, mergedOverride["temperature"])
	require.NotContains(t, mergedOverride, "operations")

}

func TestChannelAffinityMissRecordsPromptCacheKeyOnSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)

	var codexRule *operation_setting.ChannelAffinityRule
	for i := range setting.Rules {
		rule := &setting.Rules[i]
		if strings.EqualFold(strings.TrimSpace(rule.Name), "codex cli trace") {
			codexRule = rule
			break
		}
	}
	require.NotNil(t, codexRule)

	affinityValue := fmt.Sprintf("pc-miss-%d", time.Now().UnixNano())
	cacheKeySuffix := buildChannelAffinityCacheKeySuffix(*codexRule, "gpt-5", "default", affinityValue)
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{"prompt_cache_key":"%s"}`, affinityValue)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	channelID, found := GetPreferredChannelByAffinity(ctx, "gpt-5", "default")
	require.False(t, found)
	require.Equal(t, 0, channelID)

	ctx.Set("channel_id", 9528)
	RecordChannelAffinity(ctx, 9527)

	recordedChannelID, found, err := getChannelAffinityCache().Get(cacheKeySuffix)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 9528, recordedChannelID)
}

func TestResponsesEncryptedContentAffinityRecordsAndHits(t *testing.T) {
	gin.SetMode(gin.TestMode)

	encryptedContent := fmt.Sprintf("gAAAA-test-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	usingGroup := "default"
	channelID := 203

	recorder := httptest.NewRecorder()
	recordCtx, _ := gin.CreateTestContext(recorder)
	recordCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recordCtx.Set("original_model", modelName)
	common.SetContextKey(recordCtx, constant.ContextKeyUsingGroup, usingGroup)
	RecordResponsesEncryptedContentAffinity(recordCtx, []byte(fmt.Sprintf(`{
		"model":"%s",
		"output":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)), channelID)

	var rule *operation_setting.ChannelAffinityRule
	for i := range operation_setting.GetChannelAffinitySetting().Rules {
		if strings.EqualFold(strings.TrimSpace(operation_setting.GetChannelAffinitySetting().Rules[i].Name), "responses encrypted content") {
			rule = &operation_setting.GetChannelAffinitySetting().Rules[i]
			break
		}
	}
	require.NotNil(t, rule)
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*rule, modelName, usingGroup, affinityFingerprint(encryptedContent))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"input":[
			{"type":"reasoning","encrypted_content":"%s"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]
	}`, modelName, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	preferredChannelID, found := GetPreferredChannelByAffinity(hitCtx, modelName, usingGroup)
	require.True(t, found)
	require.Equal(t, channelID, preferredChannelID)

	statsCtx, ok := GetChannelAffinityStatsContext(hitCtx)
	require.True(t, ok)
	require.Equal(t, "responses encrypted content", statsCtx.RuleName)
	require.Equal(t, usingGroup, statsCtx.UsingGroup)
	require.Equal(t, affinityFingerprint(encryptedContent), statsCtx.KeyFingerprint)
}

func TestResponsesEncryptedContentAffinityDefaultTTLIsShortLived(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	require.NotNil(t, setting)
	for _, rule := range setting.Rules {
		if strings.EqualFold(strings.TrimSpace(rule.Name), operation_setting.ResponsesEncryptedContentAffinityRuleName) {
			require.Equal(t, operation_setting.ResponsesEncryptedContentAffinityTTLSeconds, rule.TTLSeconds)
			return
		}
	}
	require.Fail(t, "responses encrypted content affinity rule not found")
}

func TestResponsesEncryptedContentAffinityContinuesAfterNonMatchingRule(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	originalRules := append([]operation_setting.ChannelAffinityRule(nil), setting.Rules...)
	t.Cleanup(func() {
		setting.Rules = originalRules
	})
	setting.Rules = []operation_setting.ChannelAffinityRule{
		{
			Name:       "responses encrypted content",
			ModelRegex: []string{"^claude-.*$"},
			PathRegex:  []string{"/v1/responses"},
			KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "responses_encrypted_content"}},
		},
		{
			Name:              "responses encrypted content",
			ModelRegex:        []string{"^gpt-.*$"},
			PathRegex:         []string{"/v1/responses"},
			KeySources:        []operation_setting.ChannelAffinityKeySource{{Type: "responses_encrypted_content"}},
			IncludeUsingGroup: true,
			IncludeRuleName:   true,
		},
	}

	encryptedContent := fmt.Sprintf("gAAAA-multi-rule-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	usingGroup := "default"
	channelID := 203
	matchingRule := setting.Rules[1]
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(matchingRule, modelName, usingGroup, affinityFingerprint(encryptedContent))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	recordCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recordCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recordCtx.Set("original_model", modelName)
	common.SetContextKey(recordCtx, constant.ContextKeyUsingGroup, usingGroup)
	RecordResponsesEncryptedContentAffinity(recordCtx, []byte(fmt.Sprintf(`{
		"model":"%s",
		"output":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)), channelID)

	recordedChannelID, found, err := getChannelAffinityCache().Get(cacheKeySuffix)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, channelID, recordedChannelID)
}

func TestResponsesEncryptedContentAffinityAlwaysScopesByModelAndGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	originalRules := append([]operation_setting.ChannelAffinityRule(nil), setting.Rules...)
	t.Cleanup(func() {
		setting.Rules = originalRules
	})
	setting.Rules = []operation_setting.ChannelAffinityRule{
		{
			Name:              "responses encrypted content",
			ModelRegex:        []string{"^gpt-.*$"},
			PathRegex:         []string{"/v1/responses"},
			KeySources:        []operation_setting.ChannelAffinityKeySource{{Type: "responses_encrypted_content"}},
			IncludeRuleName:   true,
			IncludeModelName:  false,
			IncludeUsingGroup: false,
		},
	}

	encryptedContent := fmt.Sprintf("gAAAA-scoped-%d", time.Now().UnixNano())
	valueFingerprint := affinityFingerprint(encryptedContent)
	modelName := "gpt-5.5"
	otherModelName := "gpt-5.4-mini"
	usingGroup := "default"
	otherGroup := "vip"
	channelID := 203
	rule := setting.Rules[0]
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(rule, modelName, usingGroup, valueFingerprint)
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{
			cacheKeySuffix,
			buildResponsesEncryptedContentAffinityCacheKeySuffix(rule, otherModelName, usingGroup, valueFingerprint),
			buildResponsesEncryptedContentAffinityCacheKeySuffix(rule, modelName, otherGroup, valueFingerprint),
		})
	})

	recordCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recordCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recordCtx.Set("original_model", modelName)
	common.SetContextKey(recordCtx, constant.ContextKeyUsingGroup, usingGroup)
	RecordResponsesEncryptedContentAffinity(recordCtx, []byte(fmt.Sprintf(`{
		"model":"%s",
		"output":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)), channelID)

	hitCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	hitCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)))
	hitCtx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(hitCtx)
	require.NoError(t, storageErr)

	preferredChannelID, found := GetPreferredChannelByAffinity(hitCtx, modelName, usingGroup)
	require.True(t, found)
	require.Equal(t, channelID, preferredChannelID)

	_, found = GetPreferredChannelByAffinity(hitCtx, otherModelName, usingGroup)
	require.False(t, found)

	_, found = GetPreferredChannelByAffinity(hitCtx, modelName, otherGroup)
	require.False(t, found)
}

func TestResponsesEncryptedContentAffinityTrimsKeySourceType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	setting := operation_setting.GetChannelAffinitySetting()
	originalRules := append([]operation_setting.ChannelAffinityRule(nil), setting.Rules...)
	t.Cleanup(func() {
		setting.Rules = originalRules
	})
	setting.Rules = []operation_setting.ChannelAffinityRule{
		{
			Name:              "responses encrypted content",
			ModelRegex:        []string{"^gpt-.*$"},
			PathRegex:         []string{"/v1/responses"},
			KeySources:        []operation_setting.ChannelAffinityKeySource{{Type: " responses_encrypted_content "}},
			IncludeRuleName:   true,
			IncludeModelName:  true,
			IncludeUsingGroup: true,
		},
	}

	encryptedContent := fmt.Sprintf("gAAAA-trimmed-type-%d", time.Now().UnixNano())
	valueFingerprint := affinityFingerprint(encryptedContent)
	modelName := "gpt-5.5"
	usingGroup := "default"
	channelID := 203
	rule := setting.Rules[0]
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(rule, modelName, usingGroup, valueFingerprint)
	require.NoError(t, getChannelAffinityCache().SetWithTTL(cacheKeySuffix, channelID, time.Minute))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(ctx)
	require.NoError(t, storageErr)

	preferredChannelID, found := GetPreferredChannelByAffinity(ctx, modelName, usingGroup)
	require.True(t, found)
	require.Equal(t, channelID, preferredChannelID)
}

func TestResponsesEncryptedContentAffinityDoesNotRecordNonSuccessStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	encryptedContent := fmt.Sprintf("gAAAA-status-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	usingGroup := "default"
	channelID := 203

	recordCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recordCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recordCtx.Set("original_model", modelName)
	common.SetContextKey(recordCtx, constant.ContextKeyUsingGroup, usingGroup)
	RecordResponsesEncryptedContentAffinityWithStatus(recordCtx, []byte(fmt.Sprintf(`{
		"model":"%s",
		"output":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)), channelID, http.StatusBadRequest)

	var rule *operation_setting.ChannelAffinityRule
	for i := range operation_setting.GetChannelAffinitySetting().Rules {
		if strings.EqualFold(strings.TrimSpace(operation_setting.GetChannelAffinitySetting().Rules[i].Name), "responses encrypted content") {
			rule = &operation_setting.GetChannelAffinitySetting().Rules[i]
			break
		}
	}
	require.NotNil(t, rule)
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*rule, modelName, usingGroup, affinityFingerprint(encryptedContent))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	_, found, err := getChannelAffinityCache().Get(cacheKeySuffix)
	require.NoError(t, err)
	require.False(t, found)
}

func TestGenericRecordAffinityDoesNotCaptureEncryptedContentAffinity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	encryptedContent := fmt.Sprintf("gAAAA-miss-only-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	usingGroup := "default"

	var rule *operation_setting.ChannelAffinityRule
	for i := range operation_setting.GetChannelAffinitySetting().Rules {
		if strings.EqualFold(strings.TrimSpace(operation_setting.GetChannelAffinitySetting().Rules[i].Name), "responses encrypted content") {
			rule = &operation_setting.GetChannelAffinitySetting().Rules[i]
			break
		}
	}
	require.NotNil(t, rule)
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*rule, modelName, usingGroup, affinityFingerprint(encryptedContent))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(ctx)
	require.NoError(t, storageErr)

	channelID, found := GetPreferredChannelByAffinity(ctx, modelName, usingGroup)
	require.False(t, found)
	require.Equal(t, 0, channelID)

	ctx.Set("channel_id", 203)
	RecordChannelAffinity(ctx, 203)

	_, found, err := getChannelAffinityCache().Get(cacheKeySuffix)
	require.NoError(t, err)
	require.False(t, found)
}

func TestResponsesEncryptedContentAffinityTakesPrecedenceOverPromptCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	encryptedContent := fmt.Sprintf("gAAAA-priority-%d", time.Now().UnixNano())
	promptCacheKey := fmt.Sprintf("pc-priority-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	usingGroup := "default"

	var encryptedRule *operation_setting.ChannelAffinityRule
	var promptRule *operation_setting.ChannelAffinityRule
	for i := range operation_setting.GetChannelAffinitySetting().Rules {
		rule := &operation_setting.GetChannelAffinitySetting().Rules[i]
		switch strings.TrimSpace(rule.Name) {
		case "responses encrypted content":
			encryptedRule = rule
		case "codex cli trace":
			promptRule = rule
		}
	}
	require.NotNil(t, encryptedRule)
	require.NotNil(t, promptRule)

	encryptedKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*encryptedRule, modelName, usingGroup, affinityFingerprint(encryptedContent))
	promptKeySuffix := buildChannelAffinityCacheKeySuffix(*promptRule, modelName, usingGroup, promptCacheKey)
	require.NoError(t, getChannelAffinityCache().SetWithTTL(encryptedKeySuffix, 203, time.Minute))
	require.NoError(t, getChannelAffinityCache().SetWithTTL(promptKeySuffix, 204, time.Minute))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{encryptedKeySuffix, promptKeySuffix})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"prompt_cache_key":"%s",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, promptCacheKey, encryptedContent)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(ctx)
	require.NoError(t, storageErr)

	preferredChannelID, found := GetPreferredChannelByAffinity(ctx, modelName, usingGroup)
	require.True(t, found)
	require.Equal(t, 203, preferredChannelID)

	statsCtx, ok := GetChannelAffinityStatsContext(ctx)
	require.True(t, ok)
	require.Equal(t, "responses encrypted content", statsCtx.RuleName)
}

func TestResponsesEncryptedContentAffinityMissFallsThroughToPromptCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	encryptedContent := fmt.Sprintf("gAAAA-fallthrough-%d", time.Now().UnixNano())
	promptCacheKey := fmt.Sprintf("pc-fallthrough-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	usingGroup := "default"

	var encryptedRule *operation_setting.ChannelAffinityRule
	var promptRule *operation_setting.ChannelAffinityRule
	for i := range operation_setting.GetChannelAffinitySetting().Rules {
		rule := &operation_setting.GetChannelAffinitySetting().Rules[i]
		switch strings.TrimSpace(rule.Name) {
		case "responses encrypted content":
			encryptedRule = rule
		case "codex cli trace":
			promptRule = rule
		}
	}
	require.NotNil(t, encryptedRule)
	require.NotNil(t, promptRule)

	encryptedKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*encryptedRule, modelName, usingGroup, affinityFingerprint(encryptedContent))
	promptKeySuffix := buildChannelAffinityCacheKeySuffix(*promptRule, modelName, usingGroup, promptCacheKey)
	require.NoError(t, getChannelAffinityCache().SetWithTTL(promptKeySuffix, 204, time.Minute))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{encryptedKeySuffix, promptKeySuffix})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"prompt_cache_key":"%s",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, promptCacheKey, encryptedContent)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(ctx)
	require.NoError(t, storageErr)

	preferredChannelID, found := GetPreferredChannelByAffinity(ctx, modelName, usingGroup)
	require.True(t, found)
	require.Equal(t, 204, preferredChannelID)

	statsCtx, ok := GetChannelAffinityStatsContext(ctx)
	require.True(t, ok)
	require.Equal(t, "codex cli trace", statsCtx.RuleName)
}

func TestResponsesEncryptedContentAffinityTriesLaterReasoningItems(t *testing.T) {
	gin.SetMode(gin.TestMode)

	missingEncryptedContent := fmt.Sprintf("gAAAA-miss-%d", time.Now().UnixNano())
	hitEncryptedContent := fmt.Sprintf("gAAAA-hit-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	usingGroup := "default"
	channelID := 207

	var rule *operation_setting.ChannelAffinityRule
	for i := range operation_setting.GetChannelAffinitySetting().Rules {
		if strings.EqualFold(strings.TrimSpace(operation_setting.GetChannelAffinitySetting().Rules[i].Name), "responses encrypted content") {
			rule = &operation_setting.GetChannelAffinitySetting().Rules[i]
			break
		}
	}
	require.NotNil(t, rule)
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*rule, modelName, usingGroup, affinityFingerprint(hitEncryptedContent))
	require.NoError(t, getChannelAffinityCache().SetWithTTL(cacheKeySuffix, channelID, time.Minute))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"input":[
			{"type":"reasoning","encrypted_content":"%s"},
			{"type":"reasoning","encrypted_content":"%s"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]
	}`, modelName, missingEncryptedContent, hitEncryptedContent)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(ctx)
	require.NoError(t, storageErr)

	preferredChannelID, found := GetPreferredChannelByAffinity(ctx, modelName, usingGroup)
	require.True(t, found)
	require.Equal(t, channelID, preferredChannelID)
}

func TestResponsesEncryptedContentAffinityUsesSelectedAutoGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	encryptedContent := fmt.Sprintf("gAAAA-auto-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	selectedGroup := "premium"
	channelID := 209

	recordCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recordCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recordCtx.Set("original_model", modelName)
	common.SetContextKey(recordCtx, constant.ContextKeyUsingGroup, "auto")
	common.SetContextKey(recordCtx, constant.ContextKeyAutoGroup, selectedGroup)
	RecordResponsesEncryptedContentAffinity(recordCtx, []byte(fmt.Sprintf(`{
		"model":"%s",
		"output":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)), channelID)

	var rule *operation_setting.ChannelAffinityRule
	for i := range operation_setting.GetChannelAffinitySetting().Rules {
		if strings.EqualFold(strings.TrimSpace(operation_setting.GetChannelAffinitySetting().Rules[i].Name), "responses encrypted content") {
			rule = &operation_setting.GetChannelAffinitySetting().Rules[i]
			break
		}
	}
	require.NotNil(t, rule)
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*rule, modelName, selectedGroup, affinityFingerprint(encryptedContent))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(ctx)
	require.NoError(t, storageErr)

	preferredChannelID, found := GetPreferredChannelByAffinity(ctx, modelName, selectedGroup)
	require.True(t, found)
	require.Equal(t, channelID, preferredChannelID)
}

func TestResponsesEncryptedContentAffinityAutoGroupFallsBackToDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)

	encryptedContent := fmt.Sprintf("gAAAA-auto-default-%d", time.Now().UnixNano())
	modelName := "gpt-5.5"
	channelID := 210

	recordCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recordCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recordCtx.Set("original_model", modelName)
	common.SetContextKey(recordCtx, constant.ContextKeyUsingGroup, "auto")
	RecordResponsesEncryptedContentAffinity(recordCtx, []byte(fmt.Sprintf(`{
		"model":"%s",
		"output":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)), channelID)

	var rule *operation_setting.ChannelAffinityRule
	for i := range operation_setting.GetChannelAffinitySetting().Rules {
		if strings.EqualFold(strings.TrimSpace(operation_setting.GetChannelAffinitySetting().Rules[i].Name), "responses encrypted content") {
			rule = &operation_setting.GetChannelAffinitySetting().Rules[i]
			break
		}
	}
	require.NotNil(t, rule)
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*rule, modelName, channelAffinityDefaultUsingGroup, affinityFingerprint(encryptedContent))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
		"model":"%s",
		"input":[{"type":"reasoning","encrypted_content":"%s"}]
	}`, modelName, encryptedContent)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	_, storageErr := common.GetBodyStorage(ctx)
	require.NoError(t, storageErr)

	preferredChannelID, found := GetPreferredChannelByAffinity(ctx, modelName, channelAffinityDefaultUsingGroup)
	require.True(t, found)
	require.Equal(t, channelID, preferredChannelID)
}
