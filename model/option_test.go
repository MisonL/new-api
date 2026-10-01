package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/log_retention_setting"
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

func TestUpdateOptionValidatesChannelHealthSettingsBeforePersist(t *testing.T) {
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	settings := operation_setting.GetGeneralSetting()
	previousEnabled := settings.ChannelHealthEnabled
	previousCooldown := settings.ChannelHealthCooldownSeconds
	previousProbeLease := settings.ChannelHealthProbeLeaseSeconds
	keys := []string{
		"general_setting.channel_health_enabled",
		"general_setting.channel_health_cooldown_seconds",
		"general_setting.channel_health_probe_lease_seconds",
	}
	t.Cleanup(func() {
		_ = DB.Where("key IN ?", keys).Delete(&Option{}).Error
		common.OptionMap = previousOptionMap
		settings.ChannelHealthEnabled = previousEnabled
		settings.ChannelHealthCooldownSeconds = previousCooldown
		settings.ChannelHealthProbeLeaseSeconds = previousProbeLease
	})

	for _, testCase := range []struct {
		key   string
		value string
	}{
		{"general_setting.channel_health_enabled", "not-a-bool"},
		{"general_setting.channel_health_cooldown_seconds", "0"},
		{"general_setting.channel_health_cooldown_seconds", "301"},
		{"general_setting.channel_health_cooldown_seconds", "1.5"},
		{"general_setting.channel_health_probe_lease_seconds", "0"},
		{"general_setting.channel_health_probe_lease_seconds", "61"},
	} {
		require.Error(t, UpdateOption(testCase.key, testCase.value))
		require.NotContains(t, common.OptionMap, testCase.key)
	}

	var count int64
	require.NoError(t, DB.Model(&Option{}).Where("key IN ?", keys).Count(&count).Error)
	require.Zero(t, count)

	require.NoError(t, UpdateOption("general_setting.channel_health_enabled", "1"))
	require.NoError(t, UpdateOption("general_setting.channel_health_cooldown_seconds", " 45 "))
	require.NoError(t, UpdateOption("general_setting.channel_health_probe_lease_seconds", "20"))
	require.Equal(t, "true", common.OptionMap["general_setting.channel_health_enabled"])
	require.Equal(t, "45", common.OptionMap["general_setting.channel_health_cooldown_seconds"])
	require.Equal(t, "20", common.OptionMap["general_setting.channel_health_probe_lease_seconds"])
	require.True(t, settings.ChannelHealthEnabled)
	require.Equal(t, 45, settings.ChannelHealthCooldownSeconds)
	require.Equal(t, 20, settings.ChannelHealthProbeLeaseSeconds)
}

// A value the option accepts must also be the value the in-memory config ends up
// with. Accepting "1" or "TRUE" while storing it verbatim leaves updateOptionMap
// comparing against the literal "true", so the switch reads as on in the
// database and in the settings API while the rate limiter keeps treating it as
// off. Every accepted spelling must normalise to "true" or "false".
func TestUpdateOptionNormalizesAdminRateLimitExemptBeforePersist(t *testing.T) {
	// Do not run this test in parallel: it mutates the process-wide OptionMap.
	previousOptionMap := common.OptionMap
	previousExempt := operation_setting.GetRateLimitAdminExempt()
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
		common.OptionMap = previousOptionMap
		operation_setting.SetRateLimitAdminExempt(previousExempt)
	})

	trueSpellings := []string{"true", "True", "TRUE", " true ", "1", "t", "T"}
	for _, raw := range trueSpellings {
		require.NoError(t, UpdateOption("GlobalRateLimitAdminExemptEnabled", raw), "raw=%q", raw)

		var option Option
		require.NoError(t, DB.Where("key = ?", "GlobalRateLimitAdminExemptEnabled").First(&option).Error)
		require.Equal(t, "true", option.Value, "raw=%q must be stored as the canonical form", raw)
		require.Equal(t, "true", common.OptionMap["GlobalRateLimitAdminExemptEnabled"], "raw=%q", raw)
		require.True(t, operation_setting.GetRateLimitAdminExempt(), "raw=%q must enable the exemption at runtime", raw)
	}

	falseSpellings := []string{"false", "False", "FALSE", " false ", "0", "f", "F"}
	for _, raw := range falseSpellings {
		require.NoError(t, UpdateOption("GlobalRateLimitAdminExemptEnabled", raw), "raw=%q", raw)

		var option Option
		require.NoError(t, DB.Where("key = ?", "GlobalRateLimitAdminExemptEnabled").First(&option).Error)
		require.Equal(t, "false", option.Value, "raw=%q must be stored as the canonical form", raw)
		require.Equal(t, "false", common.OptionMap["GlobalRateLimitAdminExemptEnabled"], "raw=%q", raw)
		require.False(t, operation_setting.GetRateLimitAdminExempt(), "raw=%q must disable the exemption at runtime", raw)
	}

	// Unparseable input must be rejected before it reaches the database.
	for _, raw := range []string{"broken", "yes", "", "2"} {
		require.Error(t, UpdateOption("GlobalRateLimitAdminExemptEnabled", raw), "raw=%q", raw)
	}
	var option Option
	require.NoError(t, DB.Where("key = ?", "GlobalRateLimitAdminExemptEnabled").First(&option).Error)
	require.Equal(t, "false", option.Value, "a rejected value must leave the stored value untouched")
}

// The log_retention_setting keys go through a second path: handleConfigUpdate
// reflects the string into a struct with strconv, and a value strconv cannot
// parse is skipped with `continue`, leaving the field untouched while the API
// reports success. ValidateValue trims before checking, so " true " and " 500 "
// passed validation and then silently did nothing. Every accepted spelling must
// be normalised to the form that actually reaches the struct.
func TestUpdateOptionNormalizesLogRetentionValuesBeforePersist(t *testing.T) {
	// Do not run this test in parallel: it mutates the process-wide OptionMap
	// and the registered log_retention_setting config.
	previousOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB.Exec("DELETE FROM options")
		common.OptionMap = previousOptionMap
	})

	require.NoError(t, UpdateOption(log_retention_setting.ConfigName+".enabled", " true "))
	require.Equal(t, "true", common.OptionMap[log_retention_setting.ConfigName+".enabled"])
	require.True(t, log_retention_setting.GetLogRetentionSetting().Enabled, `" true " must enable retention at runtime`)

	require.NoError(t, UpdateOption(log_retention_setting.ConfigName+".batch_size", " 500 "))
	require.Equal(t, "500", common.OptionMap[log_retention_setting.ConfigName+".batch_size"])
	require.Equal(t, 500, log_retention_setting.GetLogRetentionSetting().BatchSize, `" 500 " must reach the struct`)

	// A value out of range must still be rejected before it reaches the database.
	require.Error(t, UpdateOption(log_retention_setting.ConfigName+".batch_size", " 999999 "))
}
