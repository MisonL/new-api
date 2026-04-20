package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelHeaderRuntimeTestDB(t *testing.T, tables ...interface{}) *gorm.DB {
	t.Helper()

	previousUsingSQLite := common.UsingSQLite
	previousUsingMySQL := common.UsingMySQL
	previousUsingPostgreSQL := common.UsingPostgreSQL
	previousRedisEnabled := common.RedisEnabled
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousOptionMap := common.OptionMap

	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false
	common.OptionMap = map[string]string{
		"RequestHeaderPolicyDefaultMode": "prefer_channel",
	}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(tables...))

	t.Cleanup(func() {
		common.UsingSQLite = previousUsingSQLite
		common.UsingMySQL = previousUsingMySQL
		common.UsingPostgreSQL = previousUsingPostgreSQL
		common.RedisEnabled = previousRedisEnabled
		common.OptionMap = previousOptionMap
		model.DB = previousDB
		model.LOG_DB = previousLogDB

		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func TestBuildChannelRuntimeHeaderOverrideUsesTagPolicyWhenPreferred(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})

	channel := &model.Channel{
		Id:             7,
		HeaderOverride: common.GetPointer(`{"User-Agent":"channel-static","X-From":"channel"}`),
		Tag:            common.GetPointer("tag-a"),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderPolicyMode: dto.HeaderPolicyModeSystemDefault,
	})

	record := &model.TagRequestHeaderPolicy{
		Tag:                     "tag-a",
		HeaderOverride:          `{"X-From":"tag"}`,
		HeaderPolicyMode:        "prefer_tag",
		OverrideHeaderUserAgent: true,
		UserAgentStrategyJSON:   `{"enabled":true,"mode":"random","user_agents":["tag-ua"]}`,
	}
	require.NoError(t, model.DB.Create(record).Error)

	headers, err := BuildChannelRuntimeHeaderOverride(channel)
	require.NoError(t, err)
	require.Equal(t, "tag", headers["X-From"])
	require.Equal(t, "tag-ua", headers["User-Agent"])
}

func TestBuildChannelRuntimeHeaderOverrideRespectsSystemDefaultMode(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})
	common.OptionMap["RequestHeaderPolicyDefaultMode"] = "prefer_tag"

	channel := &model.Channel{
		Id:             8,
		HeaderOverride: common.GetPointer(`{"X-From":"channel"}`),
		Tag:            common.GetPointer("tag-a"),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{})

	record := &model.TagRequestHeaderPolicy{
		Tag:            "tag-a",
		HeaderOverride: `{"X-From":"tag"}`,
	}
	require.NoError(t, model.DB.Create(record).Error)

	headers, err := BuildChannelRuntimeHeaderOverride(channel)
	require.NoError(t, err)
	require.Equal(t, "tag", headers["X-From"])
}

func TestBuildChannelRuntimeHeaderOverrideMergesHeadersAndAdvancesRoundRobin(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})

	channel := &model.Channel{
		Id:             9,
		HeaderOverride: common.GetPointer(`{"X-Channel":"channel","User-Agent":"channel-static"}`),
		Tag:            common.GetPointer("tag-a"),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderPolicyMode:         dto.HeaderPolicyModeMerge,
		OverrideHeaderUserAgent:  true,
		UserAgentStrategy: &dto.UserAgentStrategy{
			Enabled:    true,
			Mode:       "round_robin",
			UserAgents: []string{"channel-ua"},
		},
	})

	record := &model.TagRequestHeaderPolicy{
		Tag:                     "tag-a",
		HeaderOverride:          `{"X-Tag":"tag","User-Agent":"tag-static"}`,
		HeaderPolicyMode:        "merge",
		OverrideHeaderUserAgent: true,
		UserAgentStrategyJSON:   `{"enabled":true,"mode":"round_robin","user_agents":["tag-ua-1","tag-ua-2"]}`,
	}
	require.NoError(t, model.DB.Create(record).Error)

	first, err := BuildChannelRuntimeHeaderOverride(channel)
	require.NoError(t, err)
	require.Equal(t, "tag", first["X-Tag"])
	require.Equal(t, "channel", first["X-Channel"])
	require.Equal(t, "tag-ua-1", first["User-Agent"])

	second, err := BuildChannelRuntimeHeaderOverride(channel)
	require.NoError(t, err)
	require.Equal(t, "tag-ua-2", second["User-Agent"])

	state := model.RequestHeaderStrategyState{}
	require.NoError(t, model.DB.Where("scope_type = ? AND scope_key = ?", "tag", "tag:tag-a").First(&state).Error)
	require.Equal(t, int64(2), state.RoundRobinCursor)
	require.Equal(t, int64(2), state.Version)
}

func TestBuildChannelRuntimeHeaderOverrideFallsBackWhenPreferredStrategyUnconfigured(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})

	channel := &model.Channel{
		Id:             10,
		HeaderOverride: common.GetPointer(`{"X-From":"channel"}`),
		Tag:            common.GetPointer("tag-a"),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderPolicyMode: dto.HeaderPolicyModePreferChannel,
	})

	record := &model.TagRequestHeaderPolicy{
		Tag:                     "tag-a",
		HeaderPolicyMode:        "prefer_channel",
		OverrideHeaderUserAgent: true,
		UserAgentStrategyJSON:   `{"enabled":true,"mode":"random","user_agents":["tag-ua"]}`,
	}
	require.NoError(t, model.DB.Create(record).Error)

	headers, err := BuildChannelRuntimeHeaderOverride(channel)
	require.NoError(t, err)
	require.Equal(t, "tag-ua", headers["User-Agent"])
}

func TestBuildChannelRuntimeHeaderOverrideDisabledPreferredStrategySuppressesFallback(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})

	channel := &model.Channel{
		Id:             11,
		HeaderOverride: common.GetPointer(`{"User-Agent":"channel-static"}`),
		Tag:            common.GetPointer("tag-a"),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderPolicyMode:        dto.HeaderPolicyModePreferChannel,
		UserAgentStrategy:       &dto.UserAgentStrategy{Enabled: false},
		OverrideHeaderUserAgent: true,
	})

	record := &model.TagRequestHeaderPolicy{
		Tag:                     "tag-a",
		OverrideHeaderUserAgent: true,
		UserAgentStrategyJSON:   `{"enabled":true,"mode":"random","user_agents":["tag-ua"]}`,
	}
	require.NoError(t, model.DB.Create(record).Error)

	headers, err := BuildChannelRuntimeHeaderOverride(channel)
	require.NoError(t, err)
	require.Equal(t, "channel-static", headers["User-Agent"])
}

func TestBuildChannelRuntimeHeaderOverrideRejectsInvalidChannelMode(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})

	channel := &model.Channel{Id: 12}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderPolicyMode: dto.HeaderPolicyMode("broken"),
	})

	_, err := BuildChannelRuntimeHeaderOverride(channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "请求头优先级模式不合法")
}

func TestBuildChannelRuntimeHeaderOverrideRejectsInvalidSystemDefaultMode(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})
	common.OptionMap["RequestHeaderPolicyDefaultMode"] = "broken"

	channel := &model.Channel{Id: 13}

	_, err := BuildChannelRuntimeHeaderOverride(channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "请求头优先级模式不合法")
}
