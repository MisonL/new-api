package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelSortTestDB(t *testing.T) {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousDB := DB
	previousLogDB := LOG_DB

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	initCol()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	DB = db
	LOG_DB = db
	require.NoError(t, DB.AutoMigrate(&Channel{}))

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		DB = previousDB
		LOG_DB = previousLogDB
		initCol()

		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func createChannelSortFixtures(t *testing.T) {
	t.Helper()

	channels := []*Channel{
		{Id: 1, Name: "beta", Key: "sk-beta", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](30), Balance: 9, ResponseTime: 300, TestTime: 30},
		{Id: 2, Name: "alpha", Key: "sk-alpha", Models: "gpt-5", Group: "default", Priority: common.GetPointer[int64](10), Balance: 3, ResponseTime: 100, TestTime: 10},
		{Id: 3, Name: "gamma", Key: "sk-gamma", Models: "gpt-4", Group: "default", Priority: common.GetPointer[int64](20), Balance: 6, ResponseTime: 200, TestTime: 20},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
	}
}

func channelIDs(channels []*Channel) []int {
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.Id)
	}
	return ids
}

func TestGetAllChannelsAppliesWhitelistedServerSideSort(t *testing.T) {
	setupChannelSortTestDB(t)
	createChannelSortFixtures(t)

	channels, err := GetAllChannels(0, 10, false, false, NewChannelSortOptions("name", "asc", false))
	require.NoError(t, err)
	require.Equal(t, []int{2, 1, 3}, channelIDs(channels))
}

func TestGetAllChannelsRejectsUnknownSortAndFallsBackToDefault(t *testing.T) {
	setupChannelSortTestDB(t)
	createChannelSortFixtures(t)

	channels, err := GetAllChannels(0, 10, false, false, NewChannelSortOptions("name desc; drop table channels", "asc", false))
	require.NoError(t, err)
	require.Equal(t, []int{1, 3, 2}, channelIDs(channels))
}

func TestSearchChannelsAppliesWhitelistedServerSideSort(t *testing.T) {
	setupChannelSortTestDB(t)
	createChannelSortFixtures(t)

	channels, err := SearchChannels("a", "", "gpt", false, NewChannelSortOptions("response_time", "desc", false))
	require.NoError(t, err)
	require.Equal(t, []int{1, 3, 2}, channelIDs(channels))
}

func TestSearchChannelsEscapesGroupFilterWildcards(t *testing.T) {
	setupChannelSortTestDB(t)
	channels := []*Channel{
		{Id: 1, Name: "alpha", Key: "sk-alpha", Models: "gpt-5", Group: "paid%team", Priority: common.GetPointer[int64](30)},
		{Id: 2, Name: "beta", Key: "sk-beta", Models: "gpt-5", Group: "paid-team", Priority: common.GetPointer[int64](20)},
	}
	for _, channel := range channels {
		require.NoError(t, DB.Create(channel).Error)
	}

	matched, err := SearchChannels("", "paid%team", "gpt", false, NewChannelSortOptions("id", "asc", false))
	require.NoError(t, err)
	require.Equal(t, []int{1}, channelIDs(matched))
}

func insertPreferredOwnerCandidate(
	t *testing.T,
	channelID int,
	modelName string,
	group string,
	channelType int,
	priority int64,
	weight uint,
	channelStatus int,
	abilityEnabled bool,
) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{
		Id:     channelID,
		Type:   channelType,
		Key:    fmt.Sprintf("key-%d", channelID),
		Status: channelStatus,
		Name:   fmt.Sprintf("channel-%d", channelID),
	}).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   abilityEnabled,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func TestGetPreferredModelOwnerChannelTypes(t *testing.T) {
	setupChannelSortTestDB(t)
	require.NoError(t, DB.AutoMigrate(&Ability{}))

	insertPreferredOwnerCandidate(t, 1, "gpt-5", "default", constant.ChannelTypeOpenAI, 1, 100, common.ChannelStatusEnabled, true)
	insertPreferredOwnerCandidate(t, 2, "gpt-5", "default", constant.ChannelTypeCodex, 2, 0, common.ChannelStatusEnabled, true)
	insertPreferredOwnerCandidate(t, 3, "gpt-5", "vip", constant.ChannelTypeAnthropic, 10, 100, common.ChannelStatusEnabled, true)
	insertPreferredOwnerCandidate(t, 4, "gpt-4", "default", constant.ChannelTypeGemini, 1, 0, common.ChannelStatusManuallyDisabled, true)

	owners, err := GetPreferredModelOwnerChannelTypes([]string{"gpt-5", "gpt-4"}, []string{"default"})
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeCodex, owners["gpt-5"])
	_, ok := owners["gpt-4"]
	require.False(t, ok)
}
