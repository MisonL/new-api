package controller

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type copyChannelAPIResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Id int `json:"id"`
	} `json:"data"`
	Message string `json:"message"`
}

func decodeCopyChannelAPIResponse(t *testing.T, recorderBody []byte) copyChannelAPIResponse {
	t.Helper()

	var response copyChannelAPIResponse
	require.NoError(t, common.Unmarshal(recorderBody, &response))
	return response
}

func countCSVParts(value string) int64 {
	count := int64(0)
	for _, item := range strings.Split(value, ",") {
		if strings.TrimSpace(item) != "" {
			count++
		}
	}
	return count
}

func TestCopyChannelPreservesRuntimeConfiguration(t *testing.T) {
	setupChannelControllerTestDB(t)

	baseURL := "https://upstream.example"
	modelMapping := `{"gpt-5.5-compact":"gpt-5.4"}`
	statusCodeMapping := `{"524":"502"}`
	setting := `{"proxy":"http://127.0.0.1:7890","system_prompt":"keep"}`
	paramOverride := `{"operations":[{"mode":"pass_headers","value":["User-Agent","Originator"]}]}`
	headerOverride := `{"User-Agent":"` + dto.BuiltinCodexCLIUserAgent + `","Originator":"` + dto.BuiltinCodexCLIOriginator + `"}`
	remark := "copy all config"
	tag := "copy-test-tag"
	org := "org-test"
	testModel := "gpt-5.5"
	priority := int64(12)
	weight := uint(3)
	autoBan := 0
	settings := marshalChannelOtherSettingsForTest(t, dto.ChannelOtherSettings{
		HeaderPolicyMode: dto.HeaderPolicyModeMerge,
		UserAgentStrategy: &dto.UserAgentStrategy{
			Enabled:    true,
			Mode:       "round_robin",
			UserAgents: []string{dto.BuiltinCodexCLIUserAgent, "amp/1.0.0"},
		},
		HeaderProfileStrategy: &dto.HeaderProfileStrategy{
			Enabled:            true,
			Mode:               dto.HeaderProfileModeFixed,
			SelectedProfileIDs: []string{"codex-cli"},
			Profiles: []dto.HeaderProfile{
				{
					ID:       "codex-cli",
					Name:     "Codex CLI",
					Category: dto.HeaderProfileCategoryAICodingCLI,
					Headers: map[string]string{
						"User-Agent": dto.BuiltinCodexCLIUserAgent,
						"Originator": dto.BuiltinCodexCLIOriginator,
					},
				},
			},
		},
		AuxiliaryRequestHeaderPolicyEnabled: common.GetPointer(true),
		UpstreamModelUpdateCheckEnabled:     true,
	})

	origin := &model.Channel{
		Type:               constant.ChannelTypeOpenAI,
		Key:                "sk-copy-source",
		OpenAIOrganization: &org,
		TestModel:          &testModel,
		Status:             common.ChannelStatusEnabled,
		Name:               "copy-source",
		Weight:             &weight,
		CreatedTime:        123,
		TestTime:           456,
		ResponseTime:       789,
		BaseURL:            &baseURL,
		Other:              "origin-other",
		Balance:            12.34,
		BalanceUpdatedTime: 987,
		Models:             "gpt-5.5,gpt-5.5-compact",
		Group:              "default,codex",
		UsedQuota:          5678,
		ModelMapping:       &modelMapping,
		StatusCodeMapping:  &statusCodeMapping,
		Priority:           &priority,
		AutoBan:            &autoBan,
		OtherInfo:          `{"upstream":"mynav"}`,
		Tag:                &tag,
		Setting:            &setting,
		ParamOverride:      &paramOverride,
		HeaderOverride:     &headerOverride,
		Remark:             &remark,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyStatusList:   map[int]int{0: common.ChannelStatusEnabled, 1: common.ChannelStatusManuallyDisabled},
			MultiKeyPollingIndex: 1,
			MultiKeyMode:         constant.MultiKeyModePolling,
		},
		OtherSettings: settings,
	}
	require.NoError(t, model.DB.Create(origin).Error)

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel/copy/"+strconv.Itoa(origin.Id), map[string]any{})
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(origin.Id)}}

	beforeCopy := common.GetTimestamp()
	CopyChannel(ctx)
	afterCopy := common.GetTimestamp()

	response := decodeCopyChannelAPIResponse(t, recorder.Body.Bytes())
	require.True(t, response.Success, response.Message)
	require.NotZero(t, response.Data.Id)
	require.NotEqual(t, origin.Id, response.Data.Id)

	clone, err := model.GetChannelById(response.Data.Id, true)
	require.NoError(t, err)
	require.Equal(t, origin.Name+"_复制", clone.Name)
	require.NotEqual(t, origin.CreatedTime, clone.CreatedTime)
	require.GreaterOrEqual(t, clone.CreatedTime, beforeCopy)
	require.LessOrEqual(t, clone.CreatedTime, afterCopy)
	require.Equal(t, int64(0), clone.TestTime)
	require.Equal(t, 0, clone.ResponseTime)
	require.Zero(t, clone.Balance)
	require.Zero(t, clone.UsedQuota)
	require.Zero(t, clone.BalanceUpdatedTime)

	require.Equal(t, origin.Type, clone.Type)
	require.Equal(t, origin.Key, clone.Key)
	require.Equal(t, origin.OpenAIOrganization, clone.OpenAIOrganization)
	require.Equal(t, origin.TestModel, clone.TestModel)
	require.Equal(t, origin.Status, clone.Status)
	require.Equal(t, origin.Weight, clone.Weight)
	require.Equal(t, origin.BaseURL, clone.BaseURL)
	require.Equal(t, origin.Other, clone.Other)
	require.Equal(t, origin.Models, clone.Models)
	require.Equal(t, origin.Group, clone.Group)
	require.Equal(t, origin.ModelMapping, clone.ModelMapping)
	require.Equal(t, origin.StatusCodeMapping, clone.StatusCodeMapping)
	require.Equal(t, origin.Priority, clone.Priority)
	require.Equal(t, origin.AutoBan, clone.AutoBan)
	require.Equal(t, origin.OtherInfo, clone.OtherInfo)
	require.Equal(t, origin.Tag, clone.Tag)
	require.Equal(t, origin.Setting, clone.Setting)
	require.Equal(t, origin.ParamOverride, clone.ParamOverride)
	require.Equal(t, origin.HeaderOverride, clone.HeaderOverride)
	require.Equal(t, origin.Remark, clone.Remark)
	require.Equal(t, origin.ChannelInfo, clone.ChannelInfo)
	require.Equal(t, origin.OtherSettings, clone.OtherSettings)

	clonedSettings := clone.GetOtherSettings()
	require.Equal(t, dto.HeaderPolicyModeMerge, clonedSettings.HeaderPolicyMode)
	require.NotNil(t, clonedSettings.HeaderProfileStrategy)
	require.Equal(t, []string{"codex-cli"}, clonedSettings.HeaderProfileStrategy.SelectedProfileIDs)
	require.NotNil(t, clonedSettings.UserAgentStrategy)
	require.Equal(t, []string{dto.BuiltinCodexCLIUserAgent, "amp/1.0.0"}, clonedSettings.UserAgentStrategy.UserAgents)

	var abilityCount int64
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ?", clone.Id).Count(&abilityCount).Error)
	expectedAbilities := countCSVParts(origin.Models) * countCSVParts(origin.Group)
	require.Equal(t, expectedAbilities, abilityCount)
}

func TestCopyChannelMissingSourceReturnsError(t *testing.T) {
	setupChannelControllerTestDB(t)

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel/copy/404", map[string]any{})
	ctx.Params = gin.Params{{Key: "id", Value: "404"}}

	CopyChannel(ctx)

	response := decodeCopyChannelAPIResponse(t, recorder.Body.Bytes())
	require.False(t, response.Success)
	require.NotEmpty(t, response.Message)
}

func TestCopyChannelAllowsEmptyOtherSettings(t *testing.T) {
	setupChannelControllerTestDB(t)

	origin := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-copy-source",
		Status: common.ChannelStatusEnabled,
		Name:   "copy-empty-settings",
		Models: "gpt-5.5",
		Group:  "default",
	}
	require.NoError(t, model.DB.Create(origin).Error)

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel/copy/"+strconv.Itoa(origin.Id), map[string]any{})
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(origin.Id)}}

	CopyChannel(ctx)

	response := decodeCopyChannelAPIResponse(t, recorder.Body.Bytes())
	require.True(t, response.Success, response.Message)

	clone, err := model.GetChannelById(response.Data.Id, true)
	require.NoError(t, err)
	require.Empty(t, clone.OtherSettings)
	require.Equal(t, dto.ChannelOtherSettings{}, clone.GetOtherSettings())
}

func TestCopyChannelPreservesDisabledStatusAndAbilities(t *testing.T) {
	setupChannelControllerTestDB(t)

	origin := &model.Channel{
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-copy-source",
		Status: common.ChannelStatusManuallyDisabled,
		Name:   "copy-disabled-source",
		Models: "gpt-5.5",
		Group:  "default",
	}
	require.NoError(t, model.DB.Create(origin).Error)

	ctx, recorder := newChannelControllerContext(t, http.MethodPost, "/api/channel/copy/"+strconv.Itoa(origin.Id), map[string]any{})
	ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(origin.Id)}}

	CopyChannel(ctx)

	response := decodeCopyChannelAPIResponse(t, recorder.Body.Bytes())
	require.True(t, response.Success, response.Message)

	clone, err := model.GetChannelById(response.Data.Id, true)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusManuallyDisabled, clone.Status)

	var enabledCount int64
	require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id = ? AND enabled = ?", clone.Id, true).Count(&enabledCount).Error)
	require.Zero(t, enabledCount)
}
