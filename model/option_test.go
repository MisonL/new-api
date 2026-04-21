package model

import (
	"testing"

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
