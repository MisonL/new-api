package channel

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProcessHeaderOverride_ChannelTestSkipsPassthroughRules(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Empty(t, headers)
}

func TestProcessHeaderOverride_ChannelTestSkipsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	_, ok := headers["x-upstream-trace"]
	require.False(t, ok)
}

func TestProcessHeaderOverride_NonTestKeepsClientHeaderPlaceholder(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Upstream-Trace": "{client_header:X-Trace-Id}",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-upstream-trace"])
}

func TestProcessHeaderOverride_RuntimeOverrideIsFinalHeaderMap(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		IsChannelTest:             false,
		UseRuntimeHeadersOverride: true,
		RuntimeHeadersOverride: map[string]any{
			"x-static":  "runtime-value",
			"x-runtime": "runtime-only",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
				"X-Legacy": "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "runtime-value", headers["x-static"])
	require.Equal(t, "runtime-only", headers["x-runtime"])
	_, exists := headers["x-legacy"]
	require.False(t, exists)
}

func TestProcessHeaderOverride_AppliesBuiltinHeaderProfile(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(ctx, constant.ContextKeyChannelHeaderPolicyAudit, service.RuntimeHeaderPolicyAudit{
		HeaderPolicyMode: "prefer_channel",
	})

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled:            true,
					Mode:               dto.HeaderProfileModeFixed,
					SelectedProfileIDs: []string{"codex-cli"},
				},
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "OpenAI Codex CLI/0.1", headers["user-agent"])
	require.Equal(t, "codex-cli", headers["x-client-name"])
	require.Equal(t, "terminal", headers["x-client-platform"])

	audit, ok := common.GetContextKeyType[service.RuntimeHeaderPolicyAudit](ctx, constant.ContextKeyChannelHeaderPolicyAudit)
	require.True(t, ok)
	require.Equal(t, "prefer_channel", audit.HeaderPolicyMode)
	require.Equal(t, "OpenAI Codex CLI/0.1", audit.AppliedUserAgent)
	require.ElementsMatch(t, []string{"user-agent", "x-client-name", "x-client-platform"}, audit.AppliedHeaderKeys)
}

func TestProcessHeaderOverride_LegacyOverrideCreatesAuditWhenMissing(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"User-Agent": "LegacyUA/1.0",
				"X-Legacy":   "legacy-only",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "LegacyUA/1.0", headers["user-agent"])
	require.Equal(t, "legacy-only", headers["x-legacy"])

	audit, ok := common.GetContextKeyType[service.RuntimeHeaderPolicyAudit](ctx, constant.ContextKeyChannelHeaderPolicyAudit)
	require.True(t, ok)
	require.Equal(t, "LegacyUA/1.0", audit.AppliedUserAgent)
	require.ElementsMatch(t, []string{"user-agent", "x-legacy"}, audit.AppliedHeaderKeys)
}

func TestProcessHeaderOverride_AppliesUserHeaderProfileAndLegacyOverrideWins(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{"X-Custom": "from-legacy"},
			ChannelOtherSettings: dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled:            true,
					Mode:               dto.HeaderProfileModeFixed,
					SelectedProfileIDs: []string{"hp_custom"},
					Profiles: []dto.HeaderProfile{
						{
							ID:      "hp_custom",
							Name:    "Custom",
							Scope:   dto.HeaderProfileScopeUser,
							Headers: map[string]string{"User-Agent": "CustomUA/1.0", "X-Custom": "from-profile"},
						},
					},
				},
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "CustomUA/1.0", headers["user-agent"])
	require.Equal(t, "from-legacy", headers["x-custom"])
}

func TestProcessHeaderOverride_HeaderProfileRoundRobinUsesRetryIndex(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		RetryIndex: 1,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled:            true,
					Mode:               dto.HeaderProfileModeRoundRobin,
					SelectedProfileIDs: []string{"hp_a", "hp_b"},
					Profiles: []dto.HeaderProfile{
						{ID: "hp_a", Headers: map[string]string{"User-Agent": "A/1.0"}},
						{ID: "hp_b", Headers: map[string]string{"User-Agent": "B/1.0"}},
					},
				},
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "B/1.0", headers["user-agent"])
}

func TestDoTaskApiRequestAppliesHeaderOverride(t *testing.T) {
	// service.InitHttpClient mutates process-wide HTTP client state, keep this test serial.
	var upstreamHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	service.InitHttpClient()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "sk-test",
			HeadersOverride: map[string]any{
				"Authorization": "Bearer override",
				"X-Static":      "yes",
			},
			ChannelOtherSettings: dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled:            true,
					Mode:               dto.HeaderProfileModeFixed,
					SelectedProfileIDs: []string{"codex-cli"},
				},
			},
		},
	}

	resp, err := DoTaskApiRequest(&testTaskAdaptor{url: server.URL}, ctx, info, http.NoBody)
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()

	require.Equal(t, "OpenAI Codex CLI/0.1", upstreamHeaders.Get("User-Agent"))
	require.Equal(t, "codex-cli", upstreamHeaders.Get("X-Client-Name"))
	require.Equal(t, "yes", upstreamHeaders.Get("X-Static"))
	require.Equal(t, "Bearer override", upstreamHeaders.Get("Authorization"))
}

func TestDoTaskApiRequestLetsSignedAdaptorApplyOverrideBeforeSigning(t *testing.T) {
	// service.InitHttpClient mutates process-wide HTTP client state, keep this test serial.
	var upstreamHeaders http.Header
	var upstreamHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHeaders = r.Header.Clone()
		upstreamHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	service.InitHttpClient()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/generations", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "ak|sk",
			HeadersOverride: map[string]any{
				"Content-Type": "application/vnd.signed+json",
				"Host":         "signed.example.test",
			},
		},
	}

	resp, err := DoTaskApiRequest(&signedHeaderTaskAdaptor{
		testTaskAdaptor: testTaskAdaptor{url: server.URL},
	}, ctx, info, http.NoBody)
	require.NoError(t, err)
	require.NotNil(t, resp)
	defer resp.Body.Close()

	require.Equal(t, "application/vnd.signed+json", upstreamHeaders.Get("Content-Type"))
	require.Equal(t, "signed.example.test", upstreamHost)
	require.Equal(t, "signed:application/vnd.signed+json:signed.example.test", upstreamHeaders.Get("Authorization"))
}

type testTaskAdaptor struct {
	url string
}

func (a *testTaskAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *testTaskAdaptor) ValidateRequestAndSetAction(_ *gin.Context, _ *relaycommon.RelayInfo) *dto.TaskError {
	return nil
}

func (a *testTaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

func (a *testTaskAdaptor) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, _ []byte) map[string]float64 {
	return nil
}

func (a *testTaskAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func (a *testTaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a *testTaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer original")
	return nil
}

func (a *testTaskAdaptor) BuildRequestBody(_ *gin.Context, _ *relaycommon.RelayInfo) (io.Reader, error) {
	return http.NoBody, nil
}

func (a *testTaskAdaptor) DoRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (*http.Response, error) {
	return nil, fmt.Errorf("DoRequest should not be called in this test")
}

func (a *testTaskAdaptor) DoResponse(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	return "", nil, nil
}

func (a *testTaskAdaptor) GetModelList() []string {
	return nil
}

func (a *testTaskAdaptor) GetChannelName() string {
	return "test"
}

func (a *testTaskAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string, _ ...http.Header) (*http.Response, error) {
	return nil, nil
}

func (a *testTaskAdaptor) ParseTaskResult(_ []byte, _ string, _ ...http.Header) (*relaycommon.TaskInfo, error) {
	return nil, nil
}

type signedHeaderTaskAdaptor struct {
	testTaskAdaptor
}

func (a *signedHeaderTaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a *signedHeaderTaskAdaptor) BuildRequestHeaderWithRuntimeHeaderOverride(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo, headerOverride map[string]string) error {
	req.Header.Set("Content-Type", "application/json")
	applyHeaderOverrideToRequest(req, headerOverride)
	req.Header.Set("Authorization", "signed:"+req.Header.Get("Content-Type")+":"+req.Host)
	return nil
}

func TestProcessHeaderOverride_HeaderProfileMissingFails(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelOtherSettings: dto.ChannelOtherSettings{
				HeaderProfileStrategy: &dto.HeaderProfileStrategy{
					Enabled:            true,
					Mode:               dto.HeaderProfileModeFixed,
					SelectedProfileIDs: []string{"missing"},
				},
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.Error(t, err)
	require.Nil(t, headers)
}

func TestProcessHeaderOverride_PassthroughSkipsAcceptEncoding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("X-Trace-Id", "trace-123")
	ctx.Request.Header.Set("Accept-Encoding", "gzip")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		ChannelMeta: &relaycommon.ChannelMeta{
			HeadersOverride: map[string]any{
				"*": "",
			},
		},
	}

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "trace-123", headers["x-trace-id"])

	_, hasAcceptEncoding := headers["accept-encoding"]
	require.False(t, hasAcceptEncoding)
}

func TestProcessHeaderOverride_PassHeadersTemplateSetsRuntimeHeaders(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("Session_id", "sess-123")

	info := &relaycommon.RelayInfo{
		IsChannelTest: false,
		RequestHeaders: map[string]string{
			"Originator": "Codex CLI",
			"Session_id": "sess-123",
		},
		ChannelMeta: &relaycommon.ChannelMeta{
			ParamOverride: map[string]any{
				"operations": []any{
					map[string]any{
						"mode":  "pass_headers",
						"value": []any{"Originator", "Session_id", "X-Codex-Beta-Features"},
					},
				},
			},
			HeadersOverride: map[string]any{
				"X-Static": "legacy-value",
			},
		},
	}

	_, err := relaycommon.ApplyParamOverrideWithRelayInfo([]byte(`{"model":"gpt-4.1"}`), info)
	require.NoError(t, err)
	require.True(t, info.UseRuntimeHeadersOverride)
	require.Equal(t, "Codex CLI", info.RuntimeHeadersOverride["originator"])
	require.Equal(t, "sess-123", info.RuntimeHeadersOverride["session_id"])
	_, exists := info.RuntimeHeadersOverride["x-codex-beta-features"]
	require.False(t, exists)
	require.Equal(t, "legacy-value", info.RuntimeHeadersOverride["x-static"])

	headers, err := processHeaderOverride(info, ctx)
	require.NoError(t, err)
	require.Equal(t, "Codex CLI", headers["originator"])
	require.Equal(t, "sess-123", headers["session_id"])
	_, exists = headers["x-codex-beta-features"]
	require.False(t, exists)

	upstreamReq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/responses", nil)
	applyHeaderOverrideToRequest(upstreamReq, headers)
	require.Equal(t, "Codex CLI", upstreamReq.Header.Get("Originator"))
	require.Equal(t, "sess-123", upstreamReq.Header.Get("Session_id"))
	require.Empty(t, upstreamReq.Header.Get("X-Codex-Beta-Features"))
}
