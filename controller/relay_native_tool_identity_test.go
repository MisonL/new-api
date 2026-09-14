package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldSkipResponsesViaChatForCodexLiteNativeToolIdentity(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-stream", true: "stream"}[stream], func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
			channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
			info := codexLiteNativeToolIdentityRelayInfo(stream, "workspace_runtime", "read_file")

			skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

			require.NoError(t, err)
			require.True(t, skip)
			require.ErrorIs(t, info.LastError, errResponsesNativeToolIdentityUnsupported)
			requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
				ChannelID: channel.Id,
				Reason:    responsesRoutingChannelSkipReasonRequiresNativeResponsesToolIdentity,
			}})
		})
	}
}

func TestGetChannelSkipsResponsesViaChatForCodexLiteNativeToolIdentity(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(chatOnly.Id)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := codexLiteNativeToolIdentityRelayInfo(false, "dynamic_agents", "launch")

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})
	require.Nil(t, err)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	require.Equal(t, []string{strconv.Itoa(chatOnly.Id)}, ctx.GetStringSlice("use_channel"))
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: chatOnly.Id,
		Reason:    responsesRoutingChannelSkipReasonRequiresNativeResponsesToolIdentity,
	}})
}

func TestGetChannelSkipsResponsesViaChatForParsedCodexLiteNativeToolIdentity(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(chatOnly.Id)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		strings.NewReader(`{
			"model":"gpt-5.5",
			"input":[
				{"type":"additional_tools","role":"developer","tools":[
					{"type":"namespace","name":"workspace_runtime","tools":[
						{"type":"function","name":"read_file","parameters":{"type":"object","properties":{}}}
					]}
				]},
				{"type":"message","role":"developer","content":[{"type":"input_text","text":"Call this tool directly as to=functions.workspace_runtime.read_file."}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			],
			"client_metadata":{
				"x-codex-turn-metadata":"{\"code_mode_tool_names\":{\"workspace_runtime__read_file\":{\"name\":\"read_file\",\"namespace\":\"workspace_runtime\"}}}"
			}
		}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5.5")
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })

	request, err := relayhelper.GetAndValidateResponsesRequest(ctx)
	require.NoError(t, err)
	parsed := request
	require.Equal(t, "object", common.GetJsonType(parsed.Extra[codexResponsesLiteClientMetadataKey]))

	info := relaycommon.GenRelayInfoResponses(ctx, parsed)
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	channel, apiErr := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, apiErr)
	require.NotNil(t, channel)
	require.Equal(t, native.Id, channel.Id)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: chatOnly.Id,
		Reason:    responsesRoutingChannelSkipReasonRequiresNativeResponsesToolIdentity,
	}})
}

func TestGetChannelSpecificResponsesViaChatRejectsCodexLiteNativeToolIdentity(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, _ := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(chatOnly.Id)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
	ctx.Set("specific_channel_id", strconv.Itoa(chatOnly.Id))
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := codexLiteNativeToolIdentityRelayInfo(false, "squad", "delegate")

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.ErrorIs(t, err, errResponsesNativeToolIdentityUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: chatOnly.Id,
		Reason:    responsesRoutingChannelSkipReasonRequiresNativeResponsesToolIdentity,
	}})
}

func TestGetChannelReturnsServiceUnavailableWhenAllCandidatesLoseCodexLiteNativeToolIdentity(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	chatOnly, native := seedRemoteCompactionRouteChannels(t, db)

	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
		Enabled: true,
		Rules: []model_setting.ProtocolConversionRule{{
			Name:           "global-responses-to-chat",
			Enabled:        true,
			SourceEndpoint: model_setting.ProtocolEndpointResponses,
			TargetEndpoint: model_setting.ProtocolEndpointChatCompletions,
			AllChannels:    true,
			ModelPatterns:  []string{`^gpt-5(\..+)?$`},
			Options:        &model_setting.ProtocolConversionOptions{EnableCustomToolBridge: true},
		}},
	}

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
	require.Nil(t, middleware.SetupContextForSelectedChannel(ctx, chatOnly, "gpt-5.5"))
	info := codexLiteNativeToolIdentityRelayInfo(false, "team_runtime", "send_task")

	channel, err := getChannel(ctx, info, &service.RetryParam{
		Ctx:        ctx,
		TokenGroup: "default",
		ModelName:  "gpt-5.5",
		Retry:      common.GetPointer(0),
	})

	require.Nil(t, channel)
	require.NotNil(t, err)
	require.Equal(t, http.StatusServiceUnavailable, err.StatusCode)
	require.ErrorIs(t, err, errResponsesNativeToolIdentityUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{
		{ChannelID: chatOnly.Id, Reason: responsesRoutingChannelSkipReasonRequiresNativeResponsesToolIdentity},
		{ChannelID: native.Id, Reason: responsesRoutingChannelSkipReasonRequiresNativeResponsesToolIdentity},
	})
}

func TestCodexLiteNativeToolIdentityProtectionHandlesUnparseableAndNonMatchingContract(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	testCases := []struct {
		name       string
		extra      map[string]common.RawMessage
		wantSkip   bool
		wantReason string
	}{
		{
			name: "malformed metadata",
			extra: map[string]common.RawMessage{
				"client_metadata": common.RawMessage(`{"x-codex-turn-metadata":"not-json"}`),
			},
			wantSkip:   true,
			wantReason: responsesRoutingChannelSkipReasonUnparseableNativeResponsesToolIdentity,
		},
		{
			name: "unrelated metadata",
			extra: map[string]common.RawMessage{
				"client_metadata": common.RawMessage(`{"x-codex-turn-metadata":"{\"code_mode_tool_names\":{\"other__tool\":{\"name\":\"tool\",\"namespace\":\"other\"}}}"}`),
			},
			wantSkip: false,
		},
		{
			name: "metadata without namespace",
			extra: map[string]common.RawMessage{
				"client_metadata": common.RawMessage(`{"x-codex-turn-metadata":"{\"code_mode_tool_names\":{\"read_file\":{\"name\":\"read_file\",\"namespace\":null}}}"}`),
			},
			wantSkip: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
			channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
			info := codexLiteNativeToolIdentityRelayInfo(false, "workspace_runtime", "read_file")
			info.Request.(*dto.OpenAIResponsesRequest).Extra = testCase.extra

			skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

			require.NoError(t, err)
			require.Equal(t, testCase.wantSkip, skip)
			if testCase.wantSkip {
				require.ErrorIs(t, info.LastError, errResponsesNativeToolIdentityUnsupported)
				requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
					ChannelID: channel.Id,
					Reason:    testCase.wantReason,
				}})
				return
			}
			require.Nil(t, info.LastError)
		})
	}
}

func TestCodexLiteNativeToolIdentityProtectionRejectsUnparseableToolsForChatBridge(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
	channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
	info := codexLiteNativeToolIdentityRelayInfo(false, "workspace_runtime", "read_file")
	request := info.Request.(*dto.OpenAIResponsesRequest)
	request.Input = common.RawMessage(`[
		{"type":"additional_tools","role":"developer","tools":{"type":"namespace","name":"workspace_runtime"}},
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"Call this tool directly as to=functions.workspace_runtime.read_file."}]}
	]`)

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.ErrorIs(t, info.LastError, errResponsesNativeToolIdentityUnsupported)
	requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
		ChannelID: channel.Id,
		Reason:    responsesRoutingChannelSkipReasonUnparseableNativeResponsesToolIdentity,
	}})
}

func TestCodexLiteNativeToolIdentityProtectionRejectsUnsupportedDeveloperContentForChatBridge(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	testCases := []struct {
		name         string
		contentField string
	}{
		{name: "object", contentField: `,"content":{"text":"Call this tool directly as to=functions.workspace_runtime.read_file."}`},
		{name: "number", contentField: `,"content":1`},
		{name: "boolean", contentField: `,"content":true`},
		{name: "missing", contentField: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
			channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
			info := codexLiteNativeToolIdentityRelayInfo(false, "workspace_runtime", "read_file")
			request := info.Request.(*dto.OpenAIResponsesRequest)
			request.Input = common.RawMessage(`[
				{"type":"additional_tools","role":"developer","tools":[
					{"type":"namespace","name":"workspace_runtime","tools":[
						{"type":"function","name":"read_file","parameters":{"type":"object","properties":{}}}
					]}
				]},
				{"type":"message","role":"developer"` + testCase.contentField + `},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`)

			skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

			require.NoError(t, err)
			require.True(t, skip)
			require.ErrorIs(t, info.LastError, errResponsesNativeToolIdentityUnsupported)
			requireResponsesRoutingChannelSkips(t, ctx, []dto.ResponsesRoutingChannelSkip{{
				ChannelID: channel.Id,
				Reason:    responsesRoutingChannelSkipReasonUnparseableNativeResponsesToolIdentity,
			}})
		})
	}
}

func TestCodexLiteNativeToolIdentityProtectionDoesNotMatchToolNamePrefix(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
	channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
	info := codexLiteNativeToolIdentityRelayInfo(false, "workspace_runtime", "read_file")
	request := info.Request.(*dto.OpenAIResponsesRequest)
	request.Input = common.RawMessage(strings.Replace(
		string(request.Input),
		"functions.workspace_runtime.read_file.",
		"functions.workspace_runtime.read_file_backup.",
		1,
	))

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
}

func TestCodexLiteNativeToolIdentityProtectionIncludesTopLevelTools(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
	channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
	info := codexLiteNativeToolIdentityRelayInfo(false, "workspace_runtime", "read_file")
	request := info.Request.(*dto.OpenAIResponsesRequest)
	request.Tools = common.RawMessage(`[{"type":"namespace","name":"workspace_runtime","tools":[{"type":"function","name":"read_file","parameters":{"type":"object","properties":{}}}]}]`)
	request.Input = common.RawMessage(`[
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"Call this tool directly as to=functions.workspace_runtime.read_file."}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
	]`)

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.ErrorIs(t, info.LastError, errResponsesNativeToolIdentityUnsupported)
}

func TestDeveloperTextReferencesCodexNativeToolRequiresCompletePath(t *testing.T) {
	identity := responsesNativeToolIdentity{namespace: "workspace_runtime", name: "read_file"}

	testCases := []struct {
		name string
		text string
		want bool
	}{
		{name: "sentence punctuation", text: "Call functions.workspace_runtime.read_file.", want: true},
		{name: "call syntax", text: "Call functions.workspace_runtime.read_file().", want: true},
		{name: "tool name suffix", text: "Call functions.workspace_runtime.read_file_backup.", want: false},
		{name: "nested path suffix", text: "Call functions.workspace_runtime.read_file.child.", want: false},
		{name: "embedded prefix", text: "Call otherfunctions.workspace_runtime.read_file.", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, developerTextReferencesCodexNativeTool(testCase.text, identity))
		})
	}
}

func TestMalformedCodexLiteMetadataDoesNotAlterNonLiteChatBridgeEligibility(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
	info := codexLiteNativeToolIdentityRelayInfo(false, "workspace_runtime", "read_file")
	info.Request.(*dto.OpenAIResponsesRequest).Extra = map[string]common.RawMessage{
		"client_metadata": common.RawMessage(`{"x-codex-turn-metadata":"not-json"}`),
	}

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.False(t, skip)
	require.Nil(t, info.LastError)
}

func TestNonLiteResponsesToolsRemainEligibleForChatBridge(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	testCases := []struct {
		name    string
		request *dto.OpenAIResponsesRequest
	}{
		{
			name:    "namespace",
			request: namespaceToolRelayInfo(true).Request.(*dto.OpenAIResponsesRequest),
		},
		{
			name: "custom",
			request: &dto.OpenAIResponsesRequest{
				Model: "gpt-5.5",
				Input: common.RawMessage(`[{"type":"message","role":"user","content":"continue"}]`),
				Tools: common.RawMessage(`[{"type":"custom","name":"exec","format":{"type":"text"}}]`),
			},
		},
		{
			name:    "tool search",
			request: toolSearchRelayInfo().Request.(*dto.OpenAIResponsesRequest),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			info := &relaycommon.RelayInfo{
				RelayMode:       relayconstant.RelayModeResponses,
				OriginModelName: "gpt-5.5",
				Request:         testCase.request,
			}
			channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}

			skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

			require.NoError(t, err)
			require.False(t, skip)
			require.Nil(t, info.LastError)
		})
	}
}

func TestCodexLiteNamespacedCustomToolIdentityRequiresNativeResponses(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	oldPolicy := settings.ChatCompletionsToResponsesPolicy
	oldPassThrough := settings.PassThroughRequestEnabled
	t.Cleanup(func() {
		settings.ChatCompletionsToResponsesPolicy = oldPolicy
		settings.PassThroughRequestEnabled = oldPassThrough
	})
	settings.PassThroughRequestEnabled = false
	settings.ChatCompletionsToResponsesPolicy = remoteCompactionChatOnlyPolicyWithCustomToolBridge(168)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set(codexResponsesLiteHeader, "true")
	channel := &model.Channel{Id: 168, Type: constant.ChannelTypeOpenAI}
	info := codexLiteNativeToolIdentityRelayInfo(false, "terminal_ops", "execute")
	request := info.Request.(*dto.OpenAIResponsesRequest)
	request.Input = common.RawMessage(strings.Replace(
		string(request.Input),
		`"type":"function","name":"execute"`,
		`"type":"custom","name":"execute","format":{"type":"text"}`,
		1,
	))

	skip, err := shouldSkipChannelForResponsesToChatCompatibility(ctx, info, channel)

	require.NoError(t, err)
	require.True(t, skip)
	require.ErrorIs(t, info.LastError, errResponsesNativeToolIdentityUnsupported)
}

func codexLiteNativeToolIdentityRelayInfo(stream bool, namespace string, name string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TokenGroup:      "default",
		UsingGroup:      "default",
		UserGroup:       "default",
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: "gpt-5.5",
		Request: &dto.OpenAIResponsesRequest{
			Model:  "gpt-5.5",
			Stream: common.GetPointer(stream),
			Input: common.RawMessage(`[
				{"type":"additional_tools","role":"developer","tools":[
					{"type":"namespace","name":"` + namespace + `","tools":[
						{"type":"function","name":"` + name + `","parameters":{"type":"object","properties":{}}}
					]}
				]},
				{"type":"message","role":"developer","content":[{"type":"input_text","text":"Call this tool directly as to=functions.` + namespace + `.` + name + `."}]},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			]`),
			Extra: map[string]common.RawMessage{
				"client_metadata": common.RawMessage(`{"x-codex-turn-metadata":"{\"code_mode_tool_names\":{\"` + namespace + `__` + name + `\":{\"name\":\"` + name + `\",\"namespace\":\"` + namespace + `\"}}}"}`),
			},
		},
	}
}
