package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestMigrateResponsesCompactModeAutoUpdatesAllChannels(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	db := openTestDB(t, &Channel{}, &Option{})
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
	})

	openAIChannel := Channel{
		Id:            9501,
		Type:          constant.ChannelTypeOpenAI,
		Name:          "openai",
		Key:           "test-key",
		Models:        "gpt-5",
		Group:         "default",
		OtherSettings: `{"responses_compact_mode":"native","responses_compact_auto_fallback_date":20260526,"responses_compact_auto_fallback_at":1780000000,"responses_compact_auto_fallback_reason":"status_code=404","azure_responses_version":"preview"}`,
	}
	anthropicChannel := Channel{
		Id:            9502,
		Type:          constant.ChannelTypeAnthropic,
		Name:          "anthropic",
		Key:           "test-key",
		Models:        "claude-sonnet-4-20250514",
		Group:         "default",
		OtherSettings: `{"responses_compact_mode":"native"}`,
	}
	require.NoError(t, db.Create(&openAIChannel).Error)
	require.NoError(t, db.Create(&anthropicChannel).Error)

	require.NoError(t, migrateResponsesCompactModeAuto())

	var migratedOpenAI Channel
	require.NoError(t, db.First(&migratedOpenAI, openAIChannel.Id).Error)
	openAISettings := migratedOpenAI.GetOtherSettings()
	require.Equal(t, dto.ResponsesCompactModeAuto, openAISettings.ResponsesCompactMode)
	require.Zero(t, openAISettings.ResponsesCompactAutoFallbackDate)
	require.Zero(t, openAISettings.ResponsesCompactAutoFallbackAt)
	require.Empty(t, openAISettings.ResponsesCompactAutoFallbackReason)
	require.Equal(t, "preview", openAISettings.AzureResponsesVersion)

	var untouchedAnthropic Channel
	require.NoError(t, db.First(&untouchedAnthropic, anthropicChannel.Id).Error)
	anthropicSettings := untouchedAnthropic.GetOtherSettings()
	require.Equal(t, dto.ResponsesCompactModeAuto, anthropicSettings.ResponsesCompactMode)

	openAISettings.ResponsesCompactMode = dto.ResponsesCompactModeSynthetic
	migratedOpenAI.SetOtherSettings(openAISettings)
	require.NoError(t, db.Model(&Channel{}).Where("id = ?", migratedOpenAI.Id).Update("settings", migratedOpenAI.OtherSettings).Error)

	require.NoError(t, migrateResponsesCompactModeAuto())

	var afterSecondRun Channel
	require.NoError(t, db.First(&afterSecondRun, openAIChannel.Id).Error)
	require.Equal(t, dto.ResponsesCompactModeSynthetic, afterSecondRun.GetOtherSettings().ResponsesCompactMode)

	for _, markerKey := range []string{
		responsesCompactModeAutoMigrationOptionKey,
		responsesCompactModeAutoAllChannelsMigrationOptionKey,
	} {
		var marker Option
		require.NoError(t, db.First(&marker, "key = ?", markerKey).Error)
		require.Equal(t, "done", marker.Value)
	}
}

func TestMigrateResponsesCompactModeAutoResetsInvalidSettings(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	db := openTestDB(t, &Channel{}, &Option{})
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
	})

	invalidChannel := Channel{
		Id:            9503,
		Type:          constant.ChannelTypeOpenAI,
		Name:          "openai-invalid",
		Key:           "test-key",
		Models:        "gpt-5",
		Group:         "default",
		OtherSettings: `{bad json`,
	}
	validChannel := Channel{
		Id:            9505,
		Type:          constant.ChannelTypeOpenAI,
		Name:          "openai-valid",
		Key:           "test-key",
		Models:        "gpt-5",
		Group:         "default",
		OtherSettings: `{"responses_compact_mode":"native"}`,
	}
	require.NoError(t, db.Create(&invalidChannel).Error)
	require.NoError(t, db.Create(&validChannel).Error)

	require.NoError(t, migrateResponsesCompactModeAuto())

	var reset Channel
	require.NoError(t, db.First(&reset, invalidChannel.Id).Error)
	require.Equal(t, dto.ResponsesCompactModeAuto, reset.GetOtherSettings().ResponsesCompactMode)

	var migrated Channel
	require.NoError(t, db.First(&migrated, validChannel.Id).Error)
	require.Equal(t, dto.ResponsesCompactModeAuto, migrated.GetOtherSettings().ResponsesCompactMode)

	for _, markerKey := range []string{
		responsesCompactModeAutoMigrationOptionKey,
		responsesCompactModeAutoAllChannelsMigrationOptionKey,
	} {
		var marker Option
		require.NoError(t, db.First(&marker, "key = ?", markerKey).Error)
		require.Equal(t, "done", marker.Value)
	}
}

func TestMigrateResponsesCompactModeAutoAllChannelsRunsWhenLegacyMarkerExists(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	db := openTestDB(t, &Channel{}, &Option{})
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
	})
	require.NoError(t, db.Create(&Option{
		Key:   responsesCompactModeAutoMigrationOptionKey,
		Value: "done",
	}).Error)
	channels := []Channel{
		{
			Id:            9504,
			Type:          constant.ChannelTypeOpenAI,
			Name:          "openai-upgraded",
			Key:           "test-key",
			Models:        "gpt-5",
			Group:         "default",
			OtherSettings: `{"responses_compact_mode":"native"}`,
		},
		{
			Id:            9506,
			Type:          constant.ChannelTypeAnthropic,
			Name:          "anthropic-upgraded",
			Key:           "test-key",
			Models:        "claude-sonnet-4-20250514",
			Group:         "default",
			OtherSettings: `{}`,
		},
	}
	require.NoError(t, db.Create(&channels).Error)

	require.NoError(t, migrateResponsesCompactModeAuto())

	for _, id := range []int{9504, 9506} {
		var got Channel
		require.NoError(t, db.First(&got, id).Error)
		require.Equal(t, dto.ResponsesCompactModeAuto, got.GetOtherSettings().ResponsesCompactMode)
	}

	var marker Option
	require.NoError(t, db.First(&marker, "key = ?", responsesCompactModeAutoAllChannelsMigrationOptionKey).Error)
	require.Equal(t, "done", marker.Value)
}

func TestMigrateResponsesCompactModeAutoSkipsWhenAllChannelsMarkerExists(t *testing.T) {
	previousDB := DB
	previousLogDB := LOG_DB
	db := openTestDB(t, &Channel{}, &Option{})
	DB = db
	LOG_DB = db
	t.Cleanup(func() {
		DB = previousDB
		LOG_DB = previousLogDB
	})
	require.NoError(t, db.Create(&Option{
		Key:   responsesCompactModeAutoAllChannelsMigrationOptionKey,
		Value: "done",
	}).Error)
	channel := Channel{
		Id:            9507,
		Type:          constant.ChannelTypeOpenAI,
		Name:          "openai-skipped",
		Key:           "test-key",
		Models:        "gpt-5",
		Group:         "default",
		OtherSettings: `{"responses_compact_mode":"native"}`,
	}
	require.NoError(t, db.Create(&channel).Error)

	require.NoError(t, migrateResponsesCompactModeAuto())

	var got Channel
	require.NoError(t, db.First(&got, channel.Id).Error)
	require.Equal(t, dto.ResponsesCompactModeNative, got.GetOtherSettings().ResponsesCompactMode)
}
