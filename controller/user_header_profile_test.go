package controller

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userHeaderProfileAPIResponse struct {
	Success bool              `json:"success"`
	Message string            `json:"message"`
	Data    common.RawMessage `json:"data"`
}

func decodeUserHeaderProfileAPIResponse(t *testing.T, recorderBody []byte) userHeaderProfileAPIResponse {
	t.Helper()

	var response userHeaderProfileAPIResponse
	require.NoError(t, common.Unmarshal(recorderBody, &response))
	return response
}

func seedUserHeaderProfileTestUser(t *testing.T, username string, profiles []dto.HeaderProfile) *model.User {
	t.Helper()

	user := &model.User{
		Username:    username,
		Password:    "password123",
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Group:       "default",
		AffCode:     common.GetRandomString(8),
	}
	user.SetSetting(dto.UserSetting{
		HeaderProfiles: profiles,
	})
	require.NoError(t, model.DB.Create(user).Error)
	return user
}

func TestUserSettingHeaderProfilesRoundTrip(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := seedUserHeaderProfileTestUser(t, "header-profile-user", []dto.HeaderProfile{
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
	})

	loaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)

	settings := loaded.GetSetting()
	require.Equal(t, user.GetSetting().HeaderProfiles, settings.HeaderProfiles)
}

func TestListUserHeaderProfilesReturnsCurrentUsersProfiles(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	currentUser := seedUserHeaderProfileTestUser(t, "header-profile-list-self", []dto.HeaderProfile{
		{
			ID:       "hp_self_1",
			Name:     "Self Profile",
			Category: dto.HeaderProfileCategoryCustom,
			Scope:    dto.HeaderProfileScopeUser,
			Headers: map[string]string{
				"X-Self": "1",
			},
		},
	})
	seedUserHeaderProfileTestUser(t, "header-profile-list-other", []dto.HeaderProfile{
		{
			ID:       "hp_other_1",
			Name:     "Other Profile",
			Category: dto.HeaderProfileCategoryCustom,
			Scope:    dto.HeaderProfileScopeUser,
			Headers: map[string]string{
				"X-Other": "1",
			},
		},
	})

	ctx, recorder := newAuthenticatedContext(t, http.MethodGet, "/api/user/header_profiles", nil, currentUser.Id)
	ListUserHeaderProfiles(ctx)

	response := decodeUserHeaderProfileAPIResponse(t, recorder.Body.Bytes())
	require.True(t, response.Success, response.Message)

	var profiles []dto.HeaderProfile
	require.NoError(t, common.Unmarshal(response.Data, &profiles))
	require.Len(t, profiles, 1)
	require.Equal(t, "hp_self_1", profiles[0].ID)
	require.Equal(t, "Self Profile", profiles[0].Name)
}

func TestCreateUserHeaderProfileRejectsInvalidHeaders(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := seedUserHeaderProfileTestUser(t, "header-profile-create-invalid", nil)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/user/header_profiles", map[string]any{
		"name":    "Invalid Headers",
		"headers": map[string]string{},
	}, user.Id)
	CreateUserHeaderProfile(ctx)

	response := decodeUserHeaderProfileAPIResponse(t, recorder.Body.Bytes())
	require.False(t, response.Success)

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	require.Empty(t, reloaded.GetSetting().HeaderProfiles)
}

func TestUpdateUserHeaderProfileRejectsDuplicateName(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := seedUserHeaderProfileTestUser(t, "header-profile-update-duplicate", []dto.HeaderProfile{
		{
			ID:       "hp_alpha",
			Name:     "Alpha",
			Category: dto.HeaderProfileCategoryCustom,
			Scope:    dto.HeaderProfileScopeUser,
			Headers: map[string]string{
				"X-One": "1",
			},
		},
		{
			ID:       "hp_beta",
			Name:     "Beta",
			Category: dto.HeaderProfileCategoryCustom,
			Scope:    dto.HeaderProfileScopeUser,
			Headers: map[string]string{
				"X-Two": "2",
			},
		},
	})

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/user/header_profiles/hp_beta", map[string]any{
		"name":    " Alpha ",
		"headers": map[string]string{"X-Updated": "true"},
	}, user.Id)
	ctx.Params = gin.Params{{Key: "id", Value: "hp_beta"}}
	UpdateUserHeaderProfile(ctx)

	response := decodeUserHeaderProfileAPIResponse(t, recorder.Body.Bytes())
	require.False(t, response.Success)

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	settings := reloaded.GetSetting()
	require.Len(t, settings.HeaderProfiles, 2)
	require.Equal(t, "Beta", settings.HeaderProfiles[1].Name)
	require.Equal(t, map[string]string{"X-Two": "2"}, settings.HeaderProfiles[1].Headers)
}

func TestDeleteUserHeaderProfileRemovesProfileFromSetting(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := seedUserHeaderProfileTestUser(t, "header-profile-delete", []dto.HeaderProfile{
		{
			ID:       "hp_keep",
			Name:     "Keep",
			Category: dto.HeaderProfileCategoryCustom,
			Scope:    dto.HeaderProfileScopeUser,
			Headers: map[string]string{
				"X-Keep": "1",
			},
		},
		{
			ID:       "hp_remove",
			Name:     "Remove",
			Category: dto.HeaderProfileCategoryCustom,
			Scope:    dto.HeaderProfileScopeUser,
			Headers: map[string]string{
				"X-Remove": "1",
			},
		},
	})

	ctx, recorder := newAuthenticatedContext(t, http.MethodDelete, "/api/user/header_profiles/hp_remove", nil, user.Id)
	ctx.Params = gin.Params{{Key: "id", Value: "hp_remove"}}
	DeleteUserHeaderProfile(ctx)

	response := decodeUserHeaderProfileAPIResponse(t, recorder.Body.Bytes())
	require.True(t, response.Success, response.Message)

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	settings := reloaded.GetSetting()
	require.Len(t, settings.HeaderProfiles, 1)
	require.Equal(t, "hp_keep", settings.HeaderProfiles[0].ID)
}

func TestUpdateUserHeaderProfileRejectsReadonlyProfile(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := seedUserHeaderProfileTestUser(t, "header-profile-update-readonly", []dto.HeaderProfile{
		{
			ID:       "hp_builtin",
			Name:     "Builtin",
			Category: dto.HeaderProfileCategoryBrowser,
			Scope:    dto.HeaderProfileScopeBuiltin,
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0",
			},
			ReadOnly: true,
		},
	})

	ctx, recorder := newAuthenticatedContext(t, http.MethodPut, "/api/user/header_profiles/hp_builtin", map[string]any{
		"name":    "Changed Builtin",
		"headers": map[string]string{"User-Agent": "Changed"},
	}, user.Id)
	ctx.Params = gin.Params{{Key: "id", Value: "hp_builtin"}}
	UpdateUserHeaderProfile(ctx)

	response := decodeUserHeaderProfileAPIResponse(t, recorder.Body.Bytes())
	require.False(t, response.Success)

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	settings := reloaded.GetSetting()
	require.Len(t, settings.HeaderProfiles, 1)
	require.Equal(t, dto.HeaderProfileScopeBuiltin, settings.HeaderProfiles[0].Scope)
	require.True(t, settings.HeaderProfiles[0].ReadOnly)
	require.Equal(t, "Builtin", settings.HeaderProfiles[0].Name)
	require.Equal(t, map[string]string{"User-Agent": "Mozilla/5.0"}, settings.HeaderProfiles[0].Headers)
}

func TestDeleteUserHeaderProfileRejectsReadonlyProfile(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := seedUserHeaderProfileTestUser(t, "header-profile-delete-readonly", []dto.HeaderProfile{
		{
			ID:       "hp_builtin",
			Name:     "Builtin",
			Category: dto.HeaderProfileCategoryBrowser,
			Scope:    dto.HeaderProfileScopeBuiltin,
			Headers: map[string]string{
				"User-Agent": "Mozilla/5.0",
			},
			ReadOnly: true,
		},
	})

	ctx, recorder := newAuthenticatedContext(t, http.MethodDelete, "/api/user/header_profiles/hp_builtin", nil, user.Id)
	ctx.Params = gin.Params{{Key: "id", Value: "hp_builtin"}}
	DeleteUserHeaderProfile(ctx)

	response := decodeUserHeaderProfileAPIResponse(t, recorder.Body.Bytes())
	require.False(t, response.Success)

	reloaded, err := model.GetUserById(user.Id, false)
	require.NoError(t, err)
	settings := reloaded.GetSetting()
	require.Len(t, settings.HeaderProfiles, 1)
	require.Equal(t, "hp_builtin", settings.HeaderProfiles[0].ID)
	require.Equal(t, dto.HeaderProfileScopeBuiltin, settings.HeaderProfiles[0].Scope)
	require.True(t, settings.HeaderProfiles[0].ReadOnly)
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

func TestCreateUserHeaderProfileGeneratesReadableIDPrefix(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	user := seedUserHeaderProfileTestUser(t, "header-profile-create-id", nil)

	ctx, recorder := newAuthenticatedContext(t, http.MethodPost, "/api/user/header_profiles", map[string]any{
		"name":    "Generated ID",
		"headers": map[string]string{"X-Test": "1"},
	}, user.Id)
	CreateUserHeaderProfile(ctx)

	response := decodeUserHeaderProfileAPIResponse(t, recorder.Body.Bytes())
	require.True(t, response.Success, response.Message)

	var profile dto.HeaderProfile
	require.NoError(t, common.Unmarshal(response.Data, &profile))
	require.True(t, strings.HasPrefix(profile.ID, "hp_"))
}
