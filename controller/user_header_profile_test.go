package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestUserSettingHeaderProfilesRoundTrip(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := &model.User{
		Username:    "header-profile-user",
		Password:    "password123",
		DisplayName: "header-profile-user",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
	}
	user.SetSetting(dto.UserSetting{
		HeaderProfiles: []dto.HeaderProfile{
			{
				ID:       "chrome-macos",
				Name:     "Chrome macOS",
				Category: dto.HeaderProfileCategoryBrowser,
				Scope:    dto.HeaderProfileScopeBuiltin,
				Headers: map[string]string{
					"User-Agent":      "Mozilla/5.0",
					"Accept-Language": "en-US,en;q=0.9",
				},
				ReadOnly:    true,
				Description: "Builtin browser profile",
			},
			{
				ID:       "codex-cli",
				Name:     "Codex CLI",
				Category: dto.HeaderProfileCategoryAICodingCLI,
				Scope:    dto.HeaderProfileScopeUser,
				Headers: map[string]string{
					"User-Agent": "Codex/1.0",
					"X-App":      "codex-cli",
				},
				Description: "User profile",
			},
		},
	})
	require.NoError(t, model.DB.Create(user).Error)

	loaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)

	settings := loaded.GetSetting()
	require.Equal(t, user.GetSetting().HeaderProfiles, settings.HeaderProfiles)
}

func TestChannelOtherSettingsHeaderProfileStrategyRoundTrip(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}))

	channel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-channel-key",
		Status: common.ChannelStatusEnabled,
		Name:   "test-channel",
		Group:  "default",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		HeaderProfileStrategy: &dto.HeaderProfileStrategy{
			Enabled: true,
			Mode:    dto.HeaderProfileModeRoundRobin,
			SelectedProfileIDs: []string{
				"chrome-macos",
				"codex-cli",
			},
		},
	})
	require.NoError(t, model.DB.Create(channel).Error)

	loaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)

	settings := loaded.GetOtherSettings()
	require.NotNil(t, settings.HeaderProfileStrategy)
	require.Equal(t, channel.GetOtherSettings().HeaderProfileStrategy, settings.HeaderProfileStrategy)
}

func TestChannelOtherSettingsOmitsEmptyHeaderProfileStrategy(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Channel{}))

	channel := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "test-channel-empty",
		Status: common.ChannelStatusEnabled,
		Name:   "test-channel-empty",
		Group:  "default",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{})
	require.NoError(t, model.DB.Create(channel).Error)

	loaded, err := model.GetChannelById(channel.Id, true)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, common.UnmarshalJsonStr(loaded.OtherSettings, &raw))
	_, exists := raw["header_profile_strategy"]
	require.False(t, exists)
	require.Nil(t, loaded.GetOtherSettings().HeaderProfileStrategy)
}
