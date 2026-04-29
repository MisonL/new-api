package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
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
