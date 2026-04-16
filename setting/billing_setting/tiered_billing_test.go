package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBillingModeJSON(t *testing.T) {
	require.NoError(t, ValidateBillingModeJSON(`{"gpt-5":"tiered_expr","gpt-4.1":"ratio"}`))
	require.Error(t, ValidateBillingModeJSON(`{"gpt-5":"unknown"}`))
}

func TestValidateBillingExprJSON(t *testing.T) {
	require.NoError(t, ValidateBillingExprJSON(`{"gpt-5":"tier(\"base\", p * 2 + c * 10)"}`))
	require.Error(t, ValidateBillingExprJSON(`{"gpt-5":"invalid +-+ expr"}`))
	require.Error(t, ValidateBillingExprJSON(`{"gpt-5":""}`))
}
