package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Listing paths and the router must agree on which abilities are reachable.
// getEnabledAbilityQuery (model/ability.go) requires channels.status = enabled,
// so an ability row whose channel is disabled is never selected for a request.
// /api/pricing and /v1/models previously aggregated abilities.enabled alone,
// which advertised groups, endpoint types, and whole models that could not be
// called. This pins the read-side filter.
func TestAbilityListingExcludesDisabledChannels(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	previousMemoryCache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		common.MemoryCacheEnabled = previousMemoryCache
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	common.MemoryCacheEnabled = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	require.NoError(t, db.Create(&[]Channel{
		{Id: 91001, Name: "enabled", Key: "k1", Type: 1, Status: common.ChannelStatusEnabled, Group: "default"},
		{Id: 91002, Name: "disabled", Key: "k2", Type: 1, Status: common.ChannelStatusManuallyDisabled, Group: "default"},
	}).Error)
	require.NoError(t, db.Create(&[]Ability{
		// Routable: the channel is enabled.
		{Group: "default", Model: "routable-model", ChannelId: 91001, Enabled: true},
		// Not routable: the ability is enabled but its channel is not, so the
		// router will never select it.
		{Group: "default", Model: "phantom-model", ChannelId: 91002, Enabled: true},
		// Also not routable: the channel does not exist at all.
		{Group: "default", Model: "orphan-model", ChannelId: 91999, Enabled: true},
	}).Error)

	// The router's own boundary must reject the phantom rows, otherwise this
	// test would be asserting against a moving target.
	require.Contains(t, GetGroupEnabledModels("default"), "routable-model")
	require.NotContains(t, GetGroupEnabledModels("default"), "phantom-model")
	require.NotContains(t, GetGroupEnabledModels("default"), "orphan-model")
	require.Contains(t, GetEnabledModels(), "routable-model")
	require.NotContains(t, GetEnabledModels(), "phantom-model")
	require.NotContains(t, GetEnabledModels(), "orphan-model")

	abilities, err := GetAllEnableAbilityWithChannels()
	require.NoError(t, err)
	listed := make(map[string]int, len(abilities))
	for _, ability := range abilities {
		listed[ability.Model] = ability.ChannelId
	}
	require.Equal(t, map[string]int{"routable-model": 91001}, listed)
}

// A disabled ability row on an enabled channel must stay excluded as well: the
// filter adds a channel-status condition, it does not replace the ability one.
func TestAbilityListingStillExcludesDisabledAbilities(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	previousSQLite, previousPostgres := common.UsingSQLite, common.UsingPostgreSQL
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.UsingSQLite, common.UsingPostgreSQL = previousSQLite, previousPostgres
		initCol()
	})

	common.UsingSQLite = true
	common.UsingPostgreSQL = false
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	require.NoError(t, db.Create(&Channel{
		Id: 91101, Name: "enabled", Key: "k1", Type: 1, Status: common.ChannelStatusEnabled, Group: "default",
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group: "default", Model: "disabled-ability-model", ChannelId: 91101, Enabled: false,
	}).Error)

	require.NotContains(t, GetGroupEnabledModels("default"), "disabled-ability-model")
	require.NotContains(t, GetEnabledModels(), "disabled-ability-model")
	abilities, err := GetAllEnableAbilityWithChannels()
	require.NoError(t, err)
	require.Empty(t, abilities)
}
