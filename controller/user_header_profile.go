package controller

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type userHeaderProfileUpsertRequest struct {
	Name        string                    `json:"name"`
	Category    dto.HeaderProfileCategory `json:"category"`
	Headers     map[string]string         `json:"headers"`
	Description string                    `json:"description"`
}

func ListUserHeaderProfiles(c *gin.Context) {
	user, err := getCurrentUserForHeaderProfiles(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, user.GetSetting().HeaderProfiles)
}

func CreateUserHeaderProfile(c *gin.Context) {
	var req userHeaderProfileUpsertRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		common.ApiError(c, fmt.Errorf("invalid request body"))
		return
	}

	user, err := getCurrentUserForHeaderProfiles(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	name := strings.TrimSpace(req.Name)
	normalizedHeaders, err := validateUserHeaderProfileInput(user.GetSetting().HeaderProfiles, "", name, req.Headers)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	settings := user.GetSetting()
	profile := dto.HeaderProfile{
		ID:          generateUserHeaderProfileID(),
		Name:        name,
		Category:    req.Category,
		Scope:       dto.HeaderProfileScopeUser,
		Headers:     normalizedHeaders,
		ReadOnly:    false,
		Description: req.Description,
	}
	settings.HeaderProfiles = append(settings.HeaderProfiles, profile)

	if err := saveUserHeaderProfiles(user, settings); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, profile)
}

func UpdateUserHeaderProfile(c *gin.Context) {
	var req userHeaderProfileUpsertRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		common.ApiError(c, fmt.Errorf("invalid request body"))
		return
	}

	profileID := strings.TrimSpace(c.Param("id"))
	if profileID == "" {
		common.ApiError(c, fmt.Errorf("invalid header profile id"))
		return
	}

	user, err := getCurrentUserForHeaderProfiles(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	settings := user.GetSetting()
	name := strings.TrimSpace(req.Name)
	normalizedHeaders, err := validateUserHeaderProfileInput(settings.HeaderProfiles, profileID, name, req.Headers)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	index := findHeaderProfileIndex(settings.HeaderProfiles, profileID)
	if index < 0 {
		common.ApiError(c, fmt.Errorf("header profile not found"))
		return
	}

	profile := settings.HeaderProfiles[index]
	if isProtectedHeaderProfile(profile) {
		common.ApiError(c, fmt.Errorf("readonly header profile cannot be updated"))
		return
	}
	profile.Name = name
	profile.Category = req.Category
	profile.Scope = dto.HeaderProfileScopeUser
	profile.Headers = normalizedHeaders
	profile.ReadOnly = false
	profile.Description = req.Description
	settings.HeaderProfiles[index] = profile

	if err := saveUserHeaderProfiles(user, settings); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, profile)
}

func DeleteUserHeaderProfile(c *gin.Context) {
	profileID := strings.TrimSpace(c.Param("id"))
	if profileID == "" {
		common.ApiError(c, fmt.Errorf("invalid header profile id"))
		return
	}

	user, err := getCurrentUserForHeaderProfiles(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	settings := user.GetSetting()
	index := findHeaderProfileIndex(settings.HeaderProfiles, profileID)
	if index < 0 {
		common.ApiError(c, fmt.Errorf("header profile not found"))
		return
	}
	if isProtectedHeaderProfile(settings.HeaderProfiles[index]) {
		common.ApiError(c, fmt.Errorf("readonly header profile cannot be deleted"))
		return
	}

	settings.HeaderProfiles = append(settings.HeaderProfiles[:index], settings.HeaderProfiles[index+1:]...)
	if err := saveUserHeaderProfiles(user, settings); err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, nil)
}

func getCurrentUserForHeaderProfiles(c *gin.Context) (*model.User, error) {
	return model.GetUserById(c.GetInt("id"), true)
}

func validateUserHeaderProfileInput(profiles []dto.HeaderProfile, currentID string, name string, headers map[string]string) (map[string]string, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("headers must be a non-empty object")
	}
	normalizedHeaders, err := dto.NormalizeHeaderProfileHeaders(headers)
	if err != nil {
		return nil, fmt.Errorf("headers are invalid: %w", err)
	}
	for _, profile := range profiles {
		if profile.ID == currentID {
			continue
		}
		if strings.TrimSpace(profile.Name) == name {
			return nil, fmt.Errorf("header profile name already exists")
		}
	}
	return normalizedHeaders, nil
}

func findHeaderProfileIndex(profiles []dto.HeaderProfile, profileID string) int {
	for i, profile := range profiles {
		if profile.ID == profileID {
			return i
		}
	}
	return -1
}

func isProtectedHeaderProfile(profile dto.HeaderProfile) bool {
	return profile.ReadOnly || profile.Scope == dto.HeaderProfileScopeBuiltin
}

func saveUserHeaderProfiles(user *model.User, settings dto.UserSetting) error {
	user.SetSetting(settings)
	return user.Update(false)
}

func generateUserHeaderProfileID() string {
	return "hp_" + common.GetUUID()[:12]
}
