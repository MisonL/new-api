package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A value that arrives with surrounding whitespace used to pass validation, be
// stored as-is, and then silently fail to apply, because every consumer parses
// it with strconv without trimming: the layered config system skips the field on
// ParseBool/ParseInt failure, and updateOptionMap discards the Atoi/ParseFloat
// error. The normalizer now returns the trimmed value, so what is stored is what
// the consumer can parse. Both a dotted layered-config key and a top-level
// scalar key are covered, since they reach the consumer through different paths.
func TestUpdateOptionTrimsWhitespaceSoTheValueApplies(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:option_whitespace?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))

	previousDB := DB
	previousOptionMap := common.OptionMap
	DB = db
	common.OptionMap = map[string]string{}
	t.Cleanup(func() {
		DB = previousDB
		common.OptionMap = previousOptionMap
		db.Exec("DELETE FROM options")
	})

	// Dotted key: reflected into the layered config struct with ParseBool, which
	// does not trim and skips the field when parsing fails.
	affinity := operation_setting.GetChannelAffinitySetting()
	previousEnabled := affinity.Enabled
	t.Cleanup(func() { affinity.Enabled = previousEnabled })

	affinity.Enabled = false
	require.NoError(t, UpdateOption("channel_affinity_setting.enabled", " true "))
	require.Equal(t, "true", common.OptionMap["channel_affinity_setting.enabled"],
		"the stored value must be the canonical form the consumer parses")
	require.True(t, affinity.Enabled, "the padded value must actually take effect")

	// Top-level scalar: updateOptionMap parses it with Atoi and discards the
	// error, so an untrimmed value silently became the zero value.
	previousCount := setting.ModelRequestRateLimitCount
	t.Cleanup(func() { setting.ModelRequestRateLimitCount = previousCount })

	setting.ModelRequestRateLimitCount = 5
	require.NoError(t, UpdateOption("ModelRequestRateLimitCount", " 60 "))
	require.Equal(t, "60", common.OptionMap["ModelRequestRateLimitCount"])
	require.Equal(t, 60, setting.ModelRequestRateLimitCount,
		"the padded value must reach the runtime field, not collapse to zero")
}
