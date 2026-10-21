package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Quota charged during the final batch interval lives only in memory until the
// ticker fires. FlushBatchUpdates is the graceful-shutdown hook that writes it
// out; without it a SIGTERM in that window loses the delta permanently.
func TestFlushBatchUpdatesWritesPendingCounters(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateStoresForTest()
	t.Cleanup(resetBatchUpdateStoresForTest)

	user := User{
		Username: "flush_user",
		Password: "password",
		Quota:    1000,
	}
	require.NoError(t, DB.Create(&user).Error)

	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -250)
	require.NoError(t, DB.First(&User{}, user.Id).Error)

	var before User
	require.NoError(t, DB.First(&before, user.Id).Error)
	require.Equal(t, 1000, before.Quota, "delta must still be pending before the flush")

	FlushBatchUpdates()

	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	require.Equal(t, 750, after.Quota)
}

// The flush runs on a path that can race the background ticker, so it must be
// callable repeatedly without applying the same delta twice.
func TestFlushBatchUpdatesIsIdempotentWhenNothingIsPending(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateStoresForTest()
	t.Cleanup(resetBatchUpdateStoresForTest)

	user := User{
		Username: "flush_idempotent_user",
		Password: "password",
		Quota:    1000,
	}
	require.NoError(t, DB.Create(&user).Error)

	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -100)
	FlushBatchUpdates()
	FlushBatchUpdates()
	FlushBatchUpdates()

	var after User
	require.NoError(t, DB.First(&after, user.Id).Error)
	require.Equal(t, 900, after.Quota)
}

func TestBatchUpdateRequeuesFailedUserDelta(t *testing.T) {
	previousDB := DB
	db := openTestDB(t)
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		resetBatchUpdateStoresForTest()
	})
	resetBatchUpdateStoresForTest()

	addNewRecord(BatchUpdateTypeUserQuota, 1, -125)
	batchUpdate()

	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	requeued := batchUpdateStores[BatchUpdateTypeUserQuota][1]
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	require.Equal(t, -125, requeued, "a failed database write must remain pending")

	require.NoError(t, db.AutoMigrate(&User{}))
	user := User{Id: 1, Username: "requeue_user", Password: "password", Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	batchUpdate()

	var after User
	require.NoError(t, db.First(&after, user.Id).Error)
	require.Equal(t, 875, after.Quota, "the failed delta must be applied after the database recovers")

	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -125)
	batchUpdate()
	require.NoError(t, db.First(&after, user.Id).Error)
	require.Equal(t, 750, after.Quota)
}
