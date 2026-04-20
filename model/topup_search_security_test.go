package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func prepareTopUpSearchSecurityTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&TopUp{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM top_ups")
	})
}

func TestSearchUserTopUpsRejectsRepeatedWildcardPattern(t *testing.T) {
	prepareTopUpSearchSecurityTest(t)

	_, _, err := SearchUserTopUps(1, "%%", &common.PageInfo{
		Page:     1,
		PageSize: 10,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "连续的 %")
}

func TestGetUserTopUpsOnlyReturnsRecordsWithinThirtyDays(t *testing.T) {
	prepareTopUpSearchSecurityTest(t)

	now := common.GetTimestamp()
	recent := &TopUp{
		UserId:     1,
		Amount:     100,
		Money:      10,
		TradeNo:    "recent-trade-no",
		CreateTime: now,
		Status:     common.TopUpStatusPending,
	}
	legacy := &TopUp{
		UserId:     1,
		Amount:     200,
		Money:      20,
		TradeNo:    "legacy-trade-no",
		CreateTime: now - topUpQueryWindowSeconds - 1,
		Status:     common.TopUpStatusPending,
	}

	require.NoError(t, recent.Insert())
	require.NoError(t, legacy.Insert())

	topups, total, err := GetUserTopUps(1, &common.PageInfo{
		Page:     1,
		PageSize: 10,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, topups, 1)
	require.Equal(t, recent.TradeNo, topups[0].TradeNo)
}
