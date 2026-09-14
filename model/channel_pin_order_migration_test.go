package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestMigrateChannelPinOrderBackfillsLegacyPinsOnce(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	db := openTestDB(t, &Channel{}, &Option{})
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
	})

	legacyPinned := common.GetPointer[int64](30)
	legacyUnpinned := common.GetPointer[int64](0)
	legacyNegative := common.GetPointer[int64](-1)
	explicitPinOrder := common.GetPointer[int64](900)
	require.NoError(t, db.Create(&Channel{Id: 1, Name: "legacy-pinned", Priority: legacyPinned}).Error)
	require.NoError(t, db.Create(&Channel{Id: 2, Name: "legacy-zero", Priority: legacyUnpinned}).Error)
	require.NoError(t, db.Create(&Channel{Id: 3, Name: "legacy-negative", Priority: legacyNegative}).Error)
	require.NoError(t, db.Create(&Channel{Id: 4, Name: "explicit-pin", Priority: legacyPinned, PinOrder: explicitPinOrder}).Error)

	require.NoError(t, migrateChannelPinOrder())

	var channels []Channel
	require.NoError(t, db.Order("id").Find(&channels).Error)
	require.Equal(t, int64(30), channels[0].GetPinOrder())
	require.Equal(t, int64(0), channels[1].GetPinOrder())
	require.Equal(t, int64(0), channels[2].GetPinOrder())
	require.Equal(t, int64(900), channels[3].GetPinOrder())

	// A later startup must not re-pin a channel that an administrator unpinned.
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", 1).Update("pin_order", 0).Error)
	require.NoError(t, migrateChannelPinOrder())
	var afterSecondRun Channel
	require.NoError(t, db.First(&afterSecondRun, 1).Error)
	require.Equal(t, int64(0), afterSecondRun.GetPinOrder())

	var marker Option
	require.NoError(t, db.First(&marker, "key = ?", channelPinOrderMigrationOptionKey).Error)
	require.Equal(t, "done", marker.Value)
}
