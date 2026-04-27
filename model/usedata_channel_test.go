package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// TestGetAllChannelQuotaData validates hourly channel aggregation and user filters.
func TestGetAllChannelQuotaData(t *testing.T) {
	truncateTables(t)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})

	channelAlpha := &Channel{
		Name:  "Alpha",
		Key:   "alpha-key",
		Group: "default",
	}
	channelBeta := &Channel{
		Name:  "Beta",
		Key:   "beta-key",
		Group: "default",
	}
	require.NoError(t, DB.Create(channelAlpha).Error)
	require.NoError(t, DB.Create(channelBeta).Error)

	logs := []*Log{
		{
			UserId:           1,
			Username:         "alice",
			ModelName:        "gpt-4o",
			ChannelId:        channelAlpha.Id,
			Type:             LogTypeConsume,
			Quota:            100,
			PromptTokens:     10,
			CompletionTokens: 20,
			CreatedAt:        3601,
		},
		{
			UserId:           2,
			Username:         "bob",
			ModelName:        "gpt-4o-mini",
			ChannelId:        channelAlpha.Id,
			Type:             LogTypeConsume,
			Quota:            50,
			PromptTokens:     5,
			CompletionTokens: 15,
			CreatedAt:        3610,
		},
		{
			UserId:           1,
			Username:         "alice",
			ModelName:        "claude-3-7-sonnet",
			ChannelId:        channelBeta.Id,
			Type:             LogTypeConsume,
			Quota:            70,
			PromptTokens:     7,
			CompletionTokens: 8,
			CreatedAt:        7201,
		},
		{
			UserId:           1,
			Username:         "alice",
			ModelName:        "ignored",
			ChannelId:        channelAlpha.Id,
			Type:             LogTypeError,
			Quota:            999,
			PromptTokens:     99,
			CompletionTokens: 99,
			CreatedAt:        3605,
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	allData, err := GetAllChannelQuotaData(0, 8000, "", DashboardTimeHour)
	require.NoError(t, err)
	require.Len(t, allData, 2)

	dataByKey := make(map[string]*ChannelQuotaData, len(allData))
	for _, item := range allData {
		dataByKey[item.ChannelName] = item
	}

	require.Contains(t, dataByKey, "Alpha")
	require.Equal(t, int64(3600), dataByKey["Alpha"].CreatedAt)
	require.Equal(t, 2, dataByKey["Alpha"].Count)
	require.Equal(t, 150, dataByKey["Alpha"].Quota)
	require.Equal(t, 50, dataByKey["Alpha"].TokenUsed)

	require.Contains(t, dataByKey, "Beta")
	require.Equal(t, int64(7200), dataByKey["Beta"].CreatedAt)
	require.Equal(t, 1, dataByKey["Beta"].Count)
	require.Equal(t, 70, dataByKey["Beta"].Quota)
	require.Equal(t, 15, dataByKey["Beta"].TokenUsed)

	aliceData, err := GetChannelQuotaDataByUserId(1, 0, 8000, DashboardTimeHour)
	require.NoError(t, err)
	require.Len(t, aliceData, 2)

	bobData, err := GetAllChannelQuotaData(0, 8000, "bob", DashboardTimeHour)
	require.NoError(t, err)
	require.Len(t, bobData, 1)
	require.Equal(t, "Alpha", bobData[0].ChannelName)
	require.Equal(t, 1, bobData[0].Count)
	require.Equal(t, 50, bobData[0].Quota)
	require.Equal(t, 20, bobData[0].TokenUsed)

	if LOG_DB.Dialector.Name() != common.DatabaseTypeSQLite {
		allDataNonSQLite, err := GetAllChannelQuotaData(0, 8000, "", DashboardTimeHour)
		require.NoError(t, err)
		require.Len(t, allDataNonSQLite, 2)

		userDataNonSQLite, err := GetChannelQuotaDataByUserId(1, 0, 8000, DashboardTimeHour)
		require.NoError(t, err)
		require.Len(t, userDataNonSQLite, 2)
	}
}

func TestQuotaDataAggregatesByRequestedGranularity(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&QuotaData{}))
	require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM quota_data")
	})

	rows := []*QuotaData{
		{
			UserID:    1,
			Username:  "alice",
			ModelName: "gpt-5",
			CreatedAt: 3600,
			Count:     1,
			Quota:     100,
			TokenUsed: 10,
		},
		{
			UserID:    1,
			Username:  "alice",
			ModelName: "gpt-5",
			CreatedAt: 7200,
			Count:     2,
			Quota:     200,
			TokenUsed: 20,
		},
		{
			UserID:    2,
			Username:  "bob",
			ModelName: "gpt-5",
			CreatedAt: 7200,
			Count:     3,
			Quota:     300,
			TokenUsed: 30,
		},
		{
			UserID:    1,
			Username:  "alice",
			ModelName: "gpt-4",
			CreatedAt: 90000,
			Count:     4,
			Quota:     400,
			TokenUsed: 40,
		},
	}
	require.NoError(t, DB.Create(&rows).Error)

	modelData, err := GetAllQuotaDates(0, 100000, "", DashboardTimeDay)
	require.NoError(t, err)
	require.Len(t, modelData, 2)

	modelByKey := make(map[string]*QuotaData, len(modelData))
	for _, item := range modelData {
		modelByKey[fmt.Sprintf("%s-%d", item.ModelName, item.CreatedAt)] = item
	}

	require.Contains(t, modelByKey, "gpt-5-0")
	require.Contains(t, modelByKey, "gpt-4-86400")
	require.Equal(t, int64(0), modelByKey["gpt-5-0"].CreatedAt)
	require.Equal(t, 6, modelByKey["gpt-5-0"].Count)
	require.Equal(t, 600, modelByKey["gpt-5-0"].Quota)
	require.Equal(t, 60, modelByKey["gpt-5-0"].TokenUsed)
	require.Equal(t, int64(86400), modelByKey["gpt-4-86400"].CreatedAt)

	userData, err := GetQuotaDataGroupByUser(0, 100000, DashboardTimeDay)
	require.NoError(t, err)
	require.Len(t, userData, 3)

	userByKey := make(map[string]*QuotaData, len(userData))
	for _, item := range userData {
		userByKey[fmt.Sprintf("%s-%d", item.Username, item.CreatedAt)] = item
	}

	require.Contains(t, userByKey, "alice-0")
	require.Contains(t, userByKey, "bob-0")
	require.Contains(t, userByKey, "alice-86400")
	require.Equal(t, 3, userByKey["alice-0"].Count)
	require.Equal(t, 300, userByKey["alice-0"].Quota)
	require.Equal(t, 3, userByKey["bob-0"].Count)
	require.Equal(t, 300, userByKey["bob-0"].Quota)
	require.Equal(t, 4, userByKey["alice-86400"].Count)
}

func TestNormalizeDashboardTimeGranularityForRange(t *testing.T) {
	require.Equal(t, DashboardTimeHour, NormalizeDashboardTimeGranularityForRange(DashboardTimeHour, 0, 86400))
	require.Equal(t, DashboardTimeDay, NormalizeDashboardTimeGranularityForRange(DashboardTimeHour, 0, 8*86400))
	require.Equal(t, DashboardTimeDay, NormalizeDashboardTimeGranularityForRange(DashboardTimeDay, 0, 30*86400))
	require.Equal(t, DashboardTimeWeek, NormalizeDashboardTimeGranularityForRange(DashboardTimeHour, 0, 31*86400))
	require.Equal(t, DashboardTimeWeek, NormalizeDashboardTimeGranularityForRange(DashboardTimeDay, 0, 31*86400))
}
