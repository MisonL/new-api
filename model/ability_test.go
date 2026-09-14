package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFixAbilityPreservesV2RouteFields(t *testing.T) {
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

	legacyPriority := int64(3)
	legacyWeight := uint(1)
	routePriority := int64(42)
	routeWeight := uint(7500)
	channel := &Channel{
		Id:       90501,
		Name:     "fix-ability-route-fields",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "gpt-fix,gpt-fix-zero,gpt-fix-legacy",
		Priority: &legacyPriority,
		Weight:   &legacyWeight,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&Ability{
		Group:                    "default",
		Model:                    "gpt-fix",
		ChannelId:                channel.Id,
		Enabled:                  true,
		Priority:                 channel.Priority,
		Weight:                   legacyWeight,
		RoutePriority:            &routePriority,
		RouteWeightBPS:           &routeWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group:     "default",
		Model:     "gpt-fix-legacy",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  channel.Priority,
		Weight:    legacyWeight,
	}).Error)
	zero := int64(0)
	zeroWeight := uint(0)
	require.NoError(t, db.Create(&Ability{
		Group:                    "default",
		Model:                    "gpt-fix-zero",
		ChannelId:                channel.Id,
		Enabled:                  true,
		Priority:                 channel.Priority,
		Weight:                   legacyWeight,
		RoutePriority:            &zero,
		RouteWeightBPS:           &zeroWeight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)
	require.NoError(t, db.Create(&Ability{
		Group:     "orphaned",
		Model:     "orphaned-model",
		ChannelId: 999999,
		Enabled:   true,
	}).Error)

	success, failed, err := FixAbility()
	require.NoError(t, err)
	require.Equal(t, 1, success)
	require.Equal(t, 0, failed)

	var got Ability
	require.NoError(t, db.Where(&Ability{Group: "default", Model: "gpt-fix", ChannelId: channel.Id}).First(&got).Error)
	require.NotNil(t, got.RoutePriority)
	require.Equal(t, routePriority, *got.RoutePriority)
	require.NotNil(t, got.RouteWeightBPS)
	require.Equal(t, routeWeight, *got.RouteWeightBPS)
	require.True(t, got.RoutePriorityConfigured)
	require.True(t, got.RouteWeightBPSConfigured)

	got = Ability{}
	require.NoError(t, db.Where(&Ability{Group: "default", Model: "gpt-fix-zero", ChannelId: channel.Id}).First(&got).Error)
	require.NotNil(t, got.RoutePriority)
	require.Equal(t, int64(0), *got.RoutePriority)
	require.NotNil(t, got.RouteWeightBPS)
	require.Equal(t, uint(0), *got.RouteWeightBPS)
	require.True(t, got.RoutePriorityConfigured)
	require.True(t, got.RouteWeightBPSConfigured)

	got = Ability{}
	require.NoError(t, db.Where(&Ability{Group: "default", Model: "gpt-fix-legacy", ChannelId: channel.Id}).First(&got).Error)
	require.Equal(t, legacyPriority, got.GetEffectiveRoutePriority())
	require.False(t, got.HasExplicitRoutePriority())
	require.Equal(t, legacyWeight, got.GetEffectiveRouteWeightBPS())
	require.False(t, got.HasExplicitRouteWeight())
	var orphan Ability
	require.ErrorIs(t, db.Where("channel_id = ?", 999999).First(&orphan).Error, gorm.ErrRecordNotFound)
}

func TestAddAbilitiesSeedsV2RouteFieldsForNewScopeAndExistingScope(t *testing.T) {
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
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "true")
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	priority := int64(7)
	weight := uint(RouteWeightTotalBPS)
	existingChannel := &Channel{
		Id:       90502,
		Name:     "existing-route-member",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "shared-model",
		Priority: &priority,
	}
	require.NoError(t, db.Create(existingChannel).Error)
	require.NoError(t, db.Create(&Ability{
		Group:                    "default",
		Model:                    "shared-model",
		ChannelId:                existingChannel.Id,
		Enabled:                  true,
		RoutePriority:            &priority,
		RouteWeightBPS:           &weight,
		RoutePriorityConfigured:  true,
		RouteWeightBPSConfigured: true,
	}).Error)

	newChannel := &Channel{
		Id:       90503,
		Name:     "new-route-member",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "shared-model",
		Priority: &priority,
	}
	require.NoError(t, db.Create(newChannel).Error)
	require.NoError(t, newChannel.AddAbilities(db))

	var seeded Ability
	require.NoError(t, db.Where(&Ability{Group: "default", Model: "shared-model", ChannelId: newChannel.Id}).First(&seeded).Error)
	require.NotNil(t, seeded.RoutePriority)
	require.Equal(t, priority, *seeded.RoutePriority)
	require.True(t, seeded.RoutePriorityConfigured)
	require.NotNil(t, seeded.RouteWeightBPS)
	require.Equal(t, uint(0), *seeded.RouteWeightBPS)
	require.True(t, seeded.RouteWeightBPSConfigured)

	standalone := &Channel{
		Id:       90504,
		Name:     "new-route-scope",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Models:   "standalone-model",
		Priority: &priority,
	}
	require.NoError(t, db.Create(standalone).Error)
	require.NoError(t, standalone.AddAbilities(db))
	var standaloneAbility Ability
	require.NoError(t, db.Where(&Ability{Group: "default", Model: "standalone-model", ChannelId: standalone.Id}).First(&standaloneAbility).Error)
	require.NotNil(t, standaloneAbility.RouteWeightBPS)
	require.Equal(t, uint(RouteWeightTotalBPS), *standaloneAbility.RouteWeightBPS)
	require.True(t, standaloneAbility.RouteWeightBPSConfigured)
}
