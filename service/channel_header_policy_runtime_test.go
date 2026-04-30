package service

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
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
		"RequestHeaderPolicyDefaultMode":              "prefer_channel",
		"RequestHeaderPolicyAuxiliaryRequestsEnabled": "true",
	}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(io.Discard, "", 0), gormlogger.Config{
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	})
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
		HeaderPolicyMode:        dto.HeaderPolicyModeMerge,
		OverrideHeaderUserAgent: true,
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
	require.NoError(t, model.DB.Where("scope_type = ? AND scope_key = ?", "channel", "channel:9:merged_tag:tag-a").First(&state).Error)
	require.Equal(t, int64(2), state.RoundRobinCursor)
	require.Equal(t, int64(2), state.Version)
}

func TestBuildChannelRuntimeHeaderOverrideMergeRoundRobinKeepsChannelStateIsolated(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})

	tagRecord := &model.TagRequestHeaderPolicy{
		Tag:                     "tag-shared",
		HeaderPolicyMode:        "merge",
		OverrideHeaderUserAgent: true,
		UserAgentStrategyJSON:   `{"enabled":true,"mode":"round_robin","user_agents":["tag-ua-1","tag-ua-2"]}`,
	}
	require.NoError(t, model.DB.Create(tagRecord).Error)

	channelA := &model.Channel{
		Id:  101,
		Tag: common.GetPointer("tag-shared"),
	}
	channelA.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderPolicyMode:        dto.HeaderPolicyModeMerge,
		OverrideHeaderUserAgent: true,
		UserAgentStrategy: &dto.UserAgentStrategy{
			Enabled:    true,
			Mode:       "round_robin",
			UserAgents: []string{"channel-a-ua"},
		},
	})

	channelB := &model.Channel{
		Id:  202,
		Tag: common.GetPointer("tag-shared"),
	}
	channelB.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderPolicyMode:        dto.HeaderPolicyModeMerge,
		OverrideHeaderUserAgent: true,
		UserAgentStrategy: &dto.UserAgentStrategy{
			Enabled:    true,
			Mode:       "round_robin",
			UserAgents: []string{"channel-b-ua"},
		},
	})

	firstA, err := BuildChannelRuntimeHeaderOverride(channelA)
	require.NoError(t, err)
	require.Equal(t, "tag-ua-1", firstA["User-Agent"])

	firstB, err := BuildChannelRuntimeHeaderOverride(channelB)
	require.NoError(t, err)
	require.Equal(t, "tag-ua-1", firstB["User-Agent"])

	secondA, err := BuildChannelRuntimeHeaderOverride(channelA)
	require.NoError(t, err)
	require.Equal(t, "tag-ua-2", secondA["User-Agent"])

	secondB, err := BuildChannelRuntimeHeaderOverride(channelB)
	require.NoError(t, err)
	require.Equal(t, "tag-ua-2", secondB["User-Agent"])

	stateA := model.RequestHeaderStrategyState{}
	require.NoError(t, model.DB.Where("scope_type = ? AND scope_key = ?", "channel", "channel:101:merged_tag:tag-shared").First(&stateA).Error)
	require.Equal(t, int64(2), stateA.RoundRobinCursor)
	require.Equal(t, int64(2), stateA.Version)

	stateB := model.RequestHeaderStrategyState{}
	require.NoError(t, model.DB.Where("scope_type = ? AND scope_key = ?", "channel", "channel:202:merged_tag:tag-shared").First(&stateB).Error)
	require.Equal(t, int64(2), stateB.RoundRobinCursor)
	require.Equal(t, int64(2), stateB.Version)
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

func TestBuildChannelRuntimeHeaderOverrideMergeDisabledStrategySuppressesOtherSide(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})

	channel := &model.Channel{
		Id:             12,
		HeaderOverride: common.GetPointer(`{"User-Agent":"channel-static"}`),
		Tag:            common.GetPointer("tag-a"),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderPolicyMode:        dto.HeaderPolicyModeMerge,
		UserAgentStrategy:       &dto.UserAgentStrategy{Enabled: false},
		OverrideHeaderUserAgent: true,
	})

	record := &model.TagRequestHeaderPolicy{
		Tag:                     "tag-a",
		HeaderPolicyMode:        "merge",
		OverrideHeaderUserAgent: true,
		UserAgentStrategyJSON:   `{"enabled":true,"mode":"random","user_agents":["tag-ua"]}`,
	}
	require.NoError(t, model.DB.Create(record).Error)

	headers, err := BuildChannelRuntimeHeaderOverride(channel)
	require.NoError(t, err)
	require.Equal(t, "channel-static", headers["User-Agent"])
}

func TestNextRoundRobinRuntimeUserAgentRetriesOptimisticConflicts(t *testing.T) {
	db := setupChannelHeaderRuntimeTestDB(t, &model.RequestHeaderStrategyState{})
	require.NoError(t, model.DB.Create(&model.RequestHeaderStrategyState{
		ScopeType:        "channel",
		ScopeKey:         "channel:22",
		RoundRobinCursor: 1,
		Version:          1,
		UpdatedAt:        common.GetTimestamp(),
	}).Error)

	var forcedConflicts int32
	callbackName := "test_force_request_header_strategy_state_conflict"
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Table != "request_header_strategy_states" {
			return
		}
		if atomic.AddInt32(&forcedConflicts, 1) > 6 {
			return
		}
		tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Exec(
			"UPDATE request_header_strategy_states SET version = version + 1 WHERE scope_type = ? AND scope_key = ?",
			"channel",
			"channel:22",
		)
	}))
	t.Cleanup(func() {
		db.Callback().Update().Remove(callbackName)
	})

	selected, err := nextRoundRobinRuntimeUserAgent("channel", "channel:22", []string{"ua-1", "ua-2"})
	require.NoError(t, err)
	require.Equal(t, "ua-2", selected)

	state := model.RequestHeaderStrategyState{}
	require.NoError(t, model.DB.Where("scope_type = ? AND scope_key = ?", "channel", "channel:22").First(&state).Error)
	require.Equal(t, int32(7), atomic.LoadInt32(&forcedConflicts))
	require.Equal(t, int64(8), state.Version)
	require.Equal(t, int64(2), state.RoundRobinCursor)
}

func TestNextRoundRobinRuntimeUserAgentHandlesDuplicateCreateRace(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.RequestHeaderStrategyState{})

	start := make(chan struct{})
	results := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			selected, err := nextRoundRobinRuntimeUserAgent("tag", "tag:dup", []string{"ua-1", "ua-2"})
			errs <- err
			results <- selected
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		require.NoError(t, err)
	}

	counts := map[string]int{}
	for result := range results {
		counts[result]++
	}
	require.Equal(t, 1, counts["ua-1"])
	require.Equal(t, 1, counts["ua-2"])

	state := model.RequestHeaderStrategyState{}
	require.NoError(t, model.DB.Where("scope_type = ? AND scope_key = ?", "tag", "tag:dup").First(&state).Error)
	require.Equal(t, int64(2), state.RoundRobinCursor)
	require.Equal(t, int64(2), state.Version)
}

func TestBuildChannelRuntimeHeaderOverrideRejectsInvalidChannelMode(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})

	channel := &model.Channel{Id: 13}
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

	channel := &model.Channel{Id: 14}

	_, err := BuildChannelRuntimeHeaderOverride(channel)
	require.Error(t, err)
	require.ErrorContains(t, err, "请求头优先级模式不合法")
}

func TestGetRuntimeHeaderStringValueIgnoresNilValue(t *testing.T) {
	require.Empty(t, getRuntimeHeaderStringValue(map[string]any{
		"User-Agent": nil,
	}, "User-Agent"))
}

func TestBuildChannelRuntimeRequestHeadersRespectsGlobalAuxiliarySwitch(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})
	common.OptionMap["RequestHeaderPolicyAuxiliaryRequestsEnabled"] = "false"

	channel := &model.Channel{
		Id:             15,
		HeaderOverride: common.GetPointer(`{"X-Debug":"enabled"}`),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderProfileStrategy: &dto.HeaderProfileStrategy{
			Enabled:            true,
			Mode:               dto.HeaderProfileModeFixed,
			SelectedProfileIDs: []string{"codex-cli"},
		},
	})
	baseHeaders := http.Header{}
	baseHeaders.Set("Authorization", "Bearer sk-test")

	headers, err := BuildChannelRuntimeRequestHeaders(channel, "sk-test", baseHeaders)
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-test", headers.Get("Authorization"))
	require.Empty(t, headers.Get("X-Debug"))
	require.Empty(t, headers.Get("User-Agent"))
}

func TestBuildChannelRuntimeRequestHeadersRespectsChannelAuxiliarySwitch(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})
	common.OptionMap["RequestHeaderPolicyAuxiliaryRequestsEnabled"] = "true"

	channel := &model.Channel{
		Id:             16,
		HeaderOverride: common.GetPointer(`{"X-Debug":"enabled"}`),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AuxiliaryRequestHeaderPolicyEnabled: common.GetPointer(false),
	})

	headers, err := BuildChannelRuntimeRequestHeaders(channel, "sk-test", http.Header{})
	require.NoError(t, err)
	require.Empty(t, headers.Get("X-Debug"))
}

func TestBuildChannelRuntimeRequestHeadersAppliesChannelAuxiliaryPolicy(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})
	common.OptionMap["RequestHeaderPolicyAuxiliaryRequestsEnabled"] = "true"

	channel := &model.Channel{
		Id:             17,
		HeaderOverride: common.GetPointer(`{"X-Debug":"enabled"}`),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AuxiliaryRequestHeaderPolicyEnabled: common.GetPointer(true),
		HeaderProfileStrategy: &dto.HeaderProfileStrategy{
			Enabled:            true,
			Mode:               dto.HeaderProfileModeFixed,
			SelectedProfileIDs: []string{"codex-cli"},
		},
	})
	baseHeaders := http.Header{}
	baseHeaders.Set("Authorization", "Bearer sk-test")

	headers, err := BuildChannelRuntimeRequestHeaders(channel, "sk-test", baseHeaders)
	require.NoError(t, err)
	require.Equal(t, "Bearer sk-test", headers.Get("Authorization"))
	require.Equal(t, "enabled", headers.Get("X-Debug"))
	require.Equal(t, dto.BuiltinCodexCLIUserAgent, headers.Get("User-Agent"))
	require.Equal(t, dto.BuiltinCodexCLIOriginator, headers.Get("Originator"))
}

func TestBuildChannelRuntimeRequestHeadersAppliesOverrideWhenProfileDisabled(t *testing.T) {
	setupChannelHeaderRuntimeTestDB(t, &model.TagRequestHeaderPolicy{}, &model.RequestHeaderStrategyState{})
	common.OptionMap["RequestHeaderPolicyAuxiliaryRequestsEnabled"] = "true"

	channel := &model.Channel{
		Id:             18,
		HeaderOverride: common.GetPointer(`{"X-Debug":"enabled"}`),
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AuxiliaryRequestHeaderPolicyEnabled: common.GetPointer(true),
		HeaderProfileStrategy: &dto.HeaderProfileStrategy{
			Enabled:            false,
			Mode:               dto.HeaderProfileModeFixed,
			SelectedProfileIDs: []string{"codex-cli"},
		},
	})

	headers, err := BuildChannelRuntimeRequestHeaders(channel, "sk-test", http.Header{})
	require.NoError(t, err)
	require.Equal(t, "enabled", headers.Get("X-Debug"))
	require.Empty(t, headers.Get("User-Agent"))
}
