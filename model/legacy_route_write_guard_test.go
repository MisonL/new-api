package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestLegacyRouteModelWritesAreBlockedInStrictMode(t *testing.T) {
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
	t.Setenv("CHANNEL_ROUTING_V2_STRICT_WRITES", "true")
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	tag := "legacy-route-guard"
	oldPriority := int64(3)
	oldWeight := uint(7)
	newPriority := int64(9)
	newWeight := uint(11)
	channel := &Channel{
		Id:       90601,
		Name:     "legacy-route-guard",
		Key:      "test-key",
		Status:   common.ChannelStatusEnabled,
		Models:   "guard-model",
		Group:    "default",
		Priority: &oldPriority,
		Weight:   &oldWeight,
		Tag:      &tag,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&Ability{
		Group:     "default",
		Model:     "guard-model",
		ChannelId: channel.Id,
		Enabled:   true,
		Priority:  &oldPriority,
		Weight:    oldWeight,
		Tag:       &tag,
	}).Error)

	err := UpdateAbilityByTag(tag, nil, &newPriority, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLegacyRouteFieldWriteBlocked))
	err = UpdateAbilityByTag(tag, nil, nil, &newWeight)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLegacyRouteFieldWriteBlocked))

	err = EditChannelByTag(tag, nil, nil, nil, nil, &newPriority, nil, nil, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLegacyRouteFieldWriteBlocked))
	err = EditChannelByTag(tag, nil, nil, nil, nil, nil, &newWeight, nil, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrLegacyRouteFieldWriteBlocked))

	var persistedChannel Channel
	require.NoError(t, db.First(&persistedChannel, channel.Id).Error)
	require.NotNil(t, persistedChannel.Priority)
	require.Equal(t, oldPriority, *persistedChannel.Priority)
	require.NotNil(t, persistedChannel.Weight)
	require.Equal(t, oldWeight, *persistedChannel.Weight)

	var persistedAbility Ability
	require.NoError(t, db.Where("tag = ?", tag).First(&persistedAbility).Error)
	require.NotNil(t, persistedAbility.Priority)
	require.Equal(t, oldPriority, *persistedAbility.Priority)
	require.Equal(t, oldWeight, persistedAbility.Weight)
}

func TestEditChannelByTagClearsExplicitEmptyModelMapping(t *testing.T) {
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
	t.Setenv("CHANNEL_ROUTING_V2_ENABLED", "false")
	t.Setenv("CHANNEL_ROUTING_V2_STRICT_WRITES", "false")
	db := openTestDB(t, &Channel{}, &Ability{})
	DB, LOG_DB = db, db
	initCol()

	tag := "clear-model-mapping"
	mapping := `{"client-model":"provider-model"}`
	channel := &Channel{
		Id:           90602,
		Name:         "clear-model-mapping",
		Key:          "test-key",
		Status:       common.ChannelStatusEnabled,
		Models:       "client-model",
		Group:        "default",
		Tag:          &tag,
		ModelMapping: &mapping,
	}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&Ability{
		Group:     "default",
		Model:     "client-model",
		ChannelId: channel.Id,
		Enabled:   true,
		Tag:       &tag,
	}).Error)

	emptyMapping := ""
	require.NoError(t, EditChannelByTag(tag, nil, &emptyMapping, nil, nil, nil, nil, nil, nil))

	var persisted Channel
	require.NoError(t, db.First(&persisted, channel.Id).Error)
	require.NotNil(t, persisted.ModelMapping)
	require.Empty(t, *persisted.ModelMapping)
}
