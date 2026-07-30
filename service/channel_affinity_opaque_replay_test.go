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
	"github.com/tidwall/gjson"
)

func TestResponsesEncryptedContentAffinityRecordsAndHitsNestedToolOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	encryptedContent := fmt.Sprintf("gAAAA-nested-tool-%d", time.Now().UnixNano())
	modelName := "gpt-5.6-sol"
	usingGroup := "default"
	channelID := 206

	recordCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	recordCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	recordCtx.Set("original_model", modelName)
	common.SetContextKey(recordCtx, constant.ContextKeyUsingGroup, usingGroup)
	RecordResponsesEncryptedContentAffinity(recordCtx, []byte(fmt.Sprintf(`{
		"model":"%s",
		"output":[{
			"type":"function_call_output",
			"call_id":"call_1",
			"output":[{"type":"encrypted_content","encrypted_content":"%s"}]
		}]
	}`, modelName, encryptedContent)), channelID)

	var rule *operation_setting.ChannelAffinityRule
	for index := range operation_setting.GetChannelAffinitySetting().Rules {
		candidate := &operation_setting.GetChannelAffinitySetting().Rules[index]
		if strings.EqualFold(strings.TrimSpace(candidate.Name), operation_setting.ResponsesEncryptedContentAffinityRuleName) {
			rule = candidate
			break
		}
	}
	require.NotNil(t, rule)
	cacheKeySuffix := buildResponsesEncryptedContentAffinityCacheKeySuffix(*rule, modelName, usingGroup, affinityFingerprint(encryptedContent))
	t.Cleanup(func() {
		_, _ = getChannelAffinityCache().DeleteMany([]string{cacheKeySuffix})
	})

	body := []byte(fmt.Sprintf(`{
		"model":"%s",
		"input":[{
			"type":"function_call_output",
			"call_id":"call_1",
			"output":[{"type":"encrypted_content","encrypted_content":"%s"}]
		}]
	}`, modelName, encryptedContent))
	metadata, err := common.ScanResponsesRequestMetadata(strings.NewReader(string(body)), int64(len(body)))
	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	common.SetResponsesRequestMetadata(recordCtx, metadata)

	preferredChannelID, found := GetPreferredChannelByAffinity(recordCtx, modelName, usingGroup)
	require.True(t, found)
	require.Equal(t, channelID, preferredChannelID)
}

func TestResponsesEncryptedContentValuesStopsAtNestingLimit(t *testing.T) {
	encryptedContent := "opaque-content-beyond-affinity-depth-limit"
	body := strings.Repeat("[", maxResponsesEncryptedContentAffinityNestingDepth+1) +
		`{"encrypted_content":"` + encryptedContent + `"}` +
		strings.Repeat("]", maxResponsesEncryptedContentAffinityNestingDepth+1)

	values := responsesEncryptedContentValues(gjson.Parse(body))

	require.Empty(t, values)
}

func TestResponsesEncryptedContentValuesAcceptsExactNestingLimit(t *testing.T) {
	encryptedContent := "opaque-content-at-affinity-depth-limit"
	body := strings.Repeat("[", maxResponsesEncryptedContentAffinityNestingDepth) +
		`{"encrypted_content":"` + encryptedContent + `"}` +
		strings.Repeat("]", maxResponsesEncryptedContentAffinityNestingDepth)

	values := responsesEncryptedContentValues(gjson.Parse(body))

	require.Equal(t, []string{encryptedContent}, values)
}

func TestResponsesEncryptedContentValuesBoundsDistinctValues(t *testing.T) {
	const expectedLimit = 32
	var body strings.Builder
	body.WriteByte('[')
	for index := 0; index < expectedLimit+8; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{"encrypted_content":"state-`)
		body.WriteString(fmt.Sprintf("%d", index))
		body.WriteString(`"}`)
	}
	body.WriteByte(']')

	values := responsesEncryptedContentValues(gjson.Parse(body.String()))

	require.Len(t, values, expectedLimit)
	require.Equal(t, "state-0", values[0])
	require.Equal(t, fmt.Sprintf("state-%d", expectedLimit-1), values[len(values)-1])
}

func TestRequestEncryptedContentFingerprintsFallsBackToBodyWhenOpaqueMetadataIsUnavailable(t *testing.T) {
	const encryptedContent = "nested-tool-output-state"
	body := `{"model":"gpt-5.6-sol","input":[{"type":"function_call_output","output":[{"type":"encrypted_content","encrypted_content":"` + encryptedContent + `"}]}]}`

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", gin.MIMEJSON)
	_, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		common.CleanupBodyStorage(ctx)
	})

	fingerprints := requestEncryptedContentFingerprints(ctx)

	require.Equal(t, []string{affinityFingerprint(encryptedContent)}, fingerprints)
}
