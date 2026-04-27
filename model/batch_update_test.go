package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func resetBatchUpdateStoresForTest() {
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}
}

func TestBatchUpdateMergesUserCounters(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateStoresForTest()
	t.Cleanup(resetBatchUpdateStoresForTest)

	user := User{
		Username:     "batch_user",
		Password:     "password",
		Quota:        1000,
		UsedQuota:    10,
		RequestCount: 2,
	}
	require.NoError(t, DB.Create(&user).Error)

	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -120)
	addNewRecord(BatchUpdateTypeUserQuota, user.Id, -30)
	addNewRecord(BatchUpdateTypeUsedQuota, user.Id, 150)
	addNewRecord(BatchUpdateTypeRequestCount, user.Id, 1)
	addNewRecord(BatchUpdateTypeRequestCount, user.Id, 2)

	batchUpdate()

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.Equal(t, 850, got.Quota)
	require.Equal(t, 160, got.UsedQuota)
	require.Equal(t, 5, got.RequestCount)
}
