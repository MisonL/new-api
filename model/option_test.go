package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionRejectsInvalidBillingExprBeforePersist(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
	})

	err := UpdateOption("billing_setting.billing_expr", `{"bad-model":"invalid +-+ expr"}`)
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "billing_setting.billing_expr").Count(&count).Error)
	require.Zero(t, count)
}

func TestUpdateOptionRejectsInvalidToolPriceBeforePersist(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
	})

	err := UpdateOption("tool_price_setting.prices", `{"web_search":-1}`)
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "tool_price_setting.prices").Count(&count).Error)
	require.Zero(t, count)
}

func TestUpdateOptionRejectsInvalidRequestHeaderPolicyDefaultModeBeforePersist(t *testing.T) {
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
	})

	err := UpdateOption("RequestHeaderPolicyDefaultMode", "")
	require.Error(t, err)

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "RequestHeaderPolicyDefaultMode").Count(&count).Error)
	require.Zero(t, count)

	err = UpdateOption("RequestHeaderPolicyDefaultMode", "broken")
	require.Error(t, err)
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "RequestHeaderPolicyDefaultMode").Count(&count).Error)
	require.Zero(t, count)
}

func TestUpdateOptionRejectsInvalidRequestHeaderPolicyAuxiliarySwitchBeforePersist(t *testing.T) {
	// Do not run this test in parallel: it mutates the process-wide OptionMap.
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
		common.OptionMap = previousOptionMap
	})

	err := UpdateOption("RequestHeaderPolicyAuxiliaryRequestsEnabled", "broken")
	require.Error(t, err)
	require.NotContains(t, common.OptionMap, "RequestHeaderPolicyAuxiliaryRequestsEnabled")

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "RequestHeaderPolicyAuxiliaryRequestsEnabled").Count(&count).Error)
	require.Zero(t, count)

	err = UpdateOption("RequestHeaderPolicyAuxiliaryRequestsEnabled", "1")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "RequestHeaderPolicyAuxiliaryRequestsEnabled").Count(&count).Error)
	require.EqualValues(t, 1, count)
	var option Option
	require.NoError(t, DB.Where("key = ?", "RequestHeaderPolicyAuxiliaryRequestsEnabled").First(&option).Error)
	require.Equal(t, "true", option.Value)
	require.Equal(t, "true", common.OptionMap["RequestHeaderPolicyAuxiliaryRequestsEnabled"])
}

func TestUpdateOptionNormalizesLegacyEmptyStatusCodeRules(t *testing.T) {
	previousDisableRanges := operation_setting.AutomaticDisableStatusCodeRanges
	previousRetryRanges := operation_setting.AutomaticRetryStatusCodeRanges
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB.Where("key IN ?", []string{"AutomaticDisableStatusCodes", "AutomaticRetryStatusCodes"}).Delete(&Option{})
		operation_setting.AutomaticDisableStatusCodeRanges = previousDisableRanges
		operation_setting.AutomaticRetryStatusCodeRanges = previousRetryRanges
		common.OptionMap = previousOptionMap
	})

	for _, key := range []string{"AutomaticDisableStatusCodes", "AutomaticRetryStatusCodes"} {
		require.NoError(t, UpdateOption(key, "[]"))

		var option Option
		require.NoError(t, DB.Where("key = ?", key).First(&option).Error)
		require.Empty(t, option.Value)
		require.Empty(t, common.OptionMap[key])
	}

	require.Empty(t, operation_setting.AutomaticDisableStatusCodeRanges)
	require.Empty(t, operation_setting.AutomaticRetryStatusCodeRanges)
}

func TestUpdateOptionValidatesUpstreamRateLimitRetryLimitBeforePersist(t *testing.T) {
	previousLimit := operation_setting.GetUpstreamRateLimitRetryLimit()
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
		operation_setting.SetUpstreamRateLimitRetryLimit(previousLimit)
		common.OptionMap = previousOptionMap
	})

	require.Error(t, UpdateOption("UpstreamRateLimitRetryLimit", "-1"))
	require.Error(t, UpdateOption("UpstreamRateLimitRetryLimit", "11"))
	require.Error(t, UpdateOption("UpstreamRateLimitRetryLimit", "invalid"))

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key = ?", "UpstreamRateLimitRetryLimit").Count(&count).Error)
	require.Zero(t, count)

	require.NoError(t, UpdateOption("UpstreamRateLimitRetryLimit", " 1 "))
	var option Option
	require.NoError(t, DB.Where("key = ?", "UpstreamRateLimitRetryLimit").First(&option).Error)
	require.Equal(t, "1", option.Value)
	require.Equal(t, 1, operation_setting.GetUpstreamRateLimitRetryLimit())
}
