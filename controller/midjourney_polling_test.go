package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestHasRecognizedMidjourneyPollingStatusRequiresRequestedTask(t *testing.T) {
	require.False(t, hasRecognizedMidjourneyPollingStatus(nil, []string{"mj-a"}))
	require.False(t, hasRecognizedMidjourneyPollingStatus(
		[]dto.MidjourneyDto{{MjId: "mj-other", Status: "SUCCESS"}},
		[]string{"mj-a"},
	))
	require.False(t, hasRecognizedMidjourneyPollingStatus(
		[]dto.MidjourneyDto{{MjId: "mj-a", Status: ""}},
		[]string{"mj-a"},
	))
	require.False(t, hasRecognizedMidjourneyPollingStatus(
		[]dto.MidjourneyDto{{MjId: "mj-a", Status: "PROVIDER_PENDING_UNKNOWN"}},
		[]string{"mj-a"},
	))
	require.True(t, hasRecognizedMidjourneyPollingStatus(
		[]dto.MidjourneyDto{{MjId: "mj-a", Status: "IN_PROGRESS"}},
		[]string{"mj-a"},
	))
}

func TestIsRecognizedMidjourneyTaskStatusAcceptsProviderAliases(t *testing.T) {
	for _, status := range []string{"NOT_START", "SUBMITTED", "QUEUED", "IN_PROGRESS", "SUCCESS", "FAILURE", "PENDING", "PROCESSING", "FAILED"} {
		require.Truef(t, isRecognizedMidjourneyTaskStatus(status), "status %q should be recognized", status)
	}
	require.False(t, isRecognizedMidjourneyTaskStatus(""))
	require.False(t, isRecognizedMidjourneyTaskStatus("UNKNOWN"))
}

func TestIsMidjourneyTaskTimedOutRequiresPersistedSubmitTime(t *testing.T) {
	now := time.UnixMilli(2 * midjourneyTaskTimeout.Milliseconds())

	require.False(t, isMidjourneyTaskTimedOut(nil, now))
	require.False(t, isMidjourneyTaskTimedOut(&model.Midjourney{Progress: "40%"}, now))
	require.False(t, isMidjourneyTaskTimedOut(&model.Midjourney{SubmitTime: 1, Progress: "100%"}, now))
	require.True(t, isMidjourneyTaskTimedOut(&model.Midjourney{SubmitTime: 1, Progress: "40%"}, now))
}

func newMidjourneyPollingSnapshotForTest(t *testing.T, proxy string, headerValue string) *model.TaskRouteSnapshot {
	t.Helper()
	channelType := constant.ChannelTypeMidjourney
	platform := constant.TaskPlatformMidjourney
	requestedModel := "midjourney"
	baseURL := "https://midjourney.example.test"
	submitPath := "/mj/submit"
	pollingPath := service.TaskPollingRoutePath(channelType, "image")
	contentPath := "/mj/content"
	submitOperation := "task.midjourney.image.submit"
	pollingOperation := "task.midjourney.image.poll"
	contentOperation := "task.midjourney.image.content"
	baseRoute := common.ChannelRouteIdentity{
		RequestedModel:         requestedModel,
		EffectiveUpstreamModel: requestedModel,
		FinalProtocol:          "midjourney",
		Operation:              submitOperation,
		UpstreamPath:           submitPath,
		ChannelID:              201,
	}.Normalized()
	pollingRoute := baseRoute
	pollingRoute.Operation = pollingOperation
	pollingRoute.UpstreamPath = pollingPath
	pollingRoute.Fingerprint = ""
	pollingRoute = pollingRoute.Normalized()
	contentRoute := baseRoute
	contentRoute.Operation = contentOperation
	contentRoute.UpstreamPath = contentPath
	contentRoute.Fingerprint = ""
	contentRoute = contentRoute.Normalized()
	headerPolicy := map[string]string{"X-Test-Static": headerValue}
	headerCiphertext, err := model.EncryptTaskRouteHeaderPolicy(headerPolicy)
	require.NoError(t, err)
	proxyCiphertext, err := model.EncryptTaskRouteProxy(proxy)
	require.NoError(t, err)
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:           model.TaskRouteSnapshotSchemaVersion,
		Group:                   "default",
		BaseURL:                 baseURL,
		RequestedModel:          requestedModel,
		EffectiveUpstreamModel:  requestedModel,
		Protocol:                "midjourney",
		Operation:               submitOperation,
		PollingOperation:        pollingOperation,
		ContentOperation:        contentOperation,
		SubmitPath:              submitPath,
		PollingPath:             pollingPath,
		ContentPath:             contentPath,
		RouteFingerprint:        baseRoute.Fingerprint,
		PollingRouteFingerprint: pollingRoute.Fingerprint,
		ContentRouteFingerprint: contentRoute.Fingerprint,
		RouteScopeVersion:       "v1:test",
		ChannelID:               201,
		ChannelType:             channelType,
		AdapterPlatform:         string(platform),
		AdapterIdentity:         "task-adapter/v1/" + strconv.Itoa(channelType) + "/" + string(platform),
		ProviderRuntime:         &model.TaskRouteProviderSnapshot{},
		HeaderPolicyCaptured:    true,
		HeaderPolicyCiphertext:  headerCiphertext,
		HeaderPolicyFingerprint: model.ComputeTaskRouteHeaderPolicyFingerprint(headerPolicy),
		ProxyCaptured:           true,
		ProxyCiphertext:         proxyCiphertext,
		ProxyFingerprint:        model.ComputeTaskRouteProxyFingerprint(proxy),
	}
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	return snapshot
}

func TestMidjourneyPollingBatchKeySeparatesSnapshotTransportIdentities(t *testing.T) {
	baseURL := "https://midjourney.example.test"
	channel := &model.Channel{
		Id:      201,
		Type:    constant.ChannelTypeMidjourney,
		BaseURL: &baseURL,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: false,
		},
	}
	first := &model.Midjourney{
		Action: "image",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: newMidjourneyPollingSnapshotForTest(t, "http://proxy-a.example.test:8080", "header-a"),
		},
	}
	second := &model.Midjourney{
		Action: "image",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: newMidjourneyPollingSnapshotForTest(t, "http://proxy-b.example.test:8080", "header-b"),
		},
	}

	firstKey, err := midjourneyTaskPollingBatchIdentity(channel, first, "credential")
	require.NoError(t, err)
	secondKey, err := midjourneyTaskPollingBatchIdentity(channel, second, "credential")
	require.NoError(t, err)

	require.NotEqual(t, firstKey, secondKey)
	require.NotEqual(t, firstKey.proxyFingerprint, secondKey.proxyFingerprint)
	require.NotEqual(t, firstKey.headerPolicyFingerprint, secondKey.headerPolicyFingerprint)
}

func TestMidjourneyPollingBatchKeySeparatesSchema5TransportIdentities(t *testing.T) {
	baseURL := "https://midjourney.example.test"
	channel := &model.Channel{
		Id:      201,
		Type:    constant.ChannelTypeMidjourney,
		BaseURL: &baseURL,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: false,
		},
	}
	firstSnapshot := newMidjourneyPollingSnapshotForTest(t, "http://proxy-a.example.test:8080", "header-a")
	firstSnapshot.SchemaVersion = model.TaskRouteSnapshotHeaderPolicySchemaVersion
	firstSnapshot.ProviderRuntime = nil
	firstSnapshot.SnapshotFingerprint = ""
	secondSnapshot := newMidjourneyPollingSnapshotForTest(t, "http://proxy-b.example.test:8080", "header-b")
	secondSnapshot.SchemaVersion = model.TaskRouteSnapshotHeaderPolicySchemaVersion
	secondSnapshot.ProviderRuntime = nil
	secondSnapshot.SnapshotFingerprint = ""

	first := &model.Midjourney{
		Action:      "image",
		PrivateData: model.TaskPrivateData{RouteSnapshot: firstSnapshot},
	}
	second := &model.Midjourney{
		Action:      "image",
		PrivateData: model.TaskPrivateData{RouteSnapshot: secondSnapshot},
	}

	firstKey, err := midjourneyTaskPollingBatchIdentity(channel, first, "credential")
	require.NoError(t, err)
	secondKey, err := midjourneyTaskPollingBatchIdentity(channel, second, "credential")
	require.NoError(t, err)

	require.NotEqual(t, firstKey, secondKey)
	require.NotEqual(t, firstKey.proxyFingerprint, secondKey.proxyFingerprint)
	require.NotEqual(t, firstKey.headerPolicyFingerprint, secondKey.headerPolicyFingerprint)
}

func TestGroupMidjourneyPollingTasksTrimsIDsAndSkipsAmbiguousDuplicates(t *testing.T) {
	uniqueTask := &model.Midjourney{Id: 3, MjId: " mj-unique "}
	duplicateFirst := &model.Midjourney{Id: 1, MjId: "mj-duplicate"}
	duplicateSecond := &model.Midjourney{Id: 2, MjId: " mj-duplicate "}

	grouped := groupMidjourneyPollingTasks([]*model.Midjourney{
		duplicateFirst,
		uniqueTask,
		duplicateSecond,
	})

	require.Equal(t, []string{"mj-unique"}, grouped.taskIDs)
	require.Same(t, uniqueTask, grouped.unique["mj-unique"])
	require.Len(t, grouped.ambiguous["mj-duplicate"], 2)
	require.Empty(t, grouped.unique["mj-duplicate"])
}

func TestClaimedMidjourneyPollingTasksExcludesBusyRows(t *testing.T) {
	busy := &model.Midjourney{Id: 1, MjId: "mj-busy"}
	claimed := &model.Midjourney{Id: 2, MjId: "mj-claimed"}

	got := claimedMidjourneyPollingTasks(
		[]string{"mj-claimed", " mj-missing "},
		map[string]*model.Midjourney{
			"mj-busy":    busy,
			"mj-claimed": claimed,
		},
	)

	require.Len(t, got, 1)
	require.Same(t, claimed, got["mj-claimed"])
	_, ok := got["mj-busy"]
	require.False(t, ok)
	_, ok = got["mj-missing"]
	require.False(t, ok)
}

func TestMidjourneyPollingBatchKeySeparatesProviderRuntimeSnapshotFingerprint(t *testing.T) {
	baseURL := "https://midjourney.example.test"
	channel := &model.Channel{
		Id:      201,
		Type:    constant.ChannelTypeMidjourney,
		BaseURL: &baseURL,
	}
	firstSnapshot := newMidjourneyPollingSnapshotForTest(t, "", "same")
	secondSnapshot := newMidjourneyPollingSnapshotForTest(t, "", "same")
	secondSnapshot.ProviderRuntime.Other = "provider-runtime-changed"
	secondSnapshot.SnapshotFingerprint = secondSnapshot.ComputeFingerprint()

	first := &model.Midjourney{Action: "image", PrivateData: model.TaskPrivateData{RouteSnapshot: firstSnapshot}}
	second := &model.Midjourney{Action: "image", PrivateData: model.TaskPrivateData{RouteSnapshot: secondSnapshot}}

	firstKey, err := midjourneyTaskPollingBatchIdentity(channel, first, "credential")
	require.NoError(t, err)
	secondKey, err := midjourneyTaskPollingBatchIdentity(channel, second, "credential")
	require.NoError(t, err)

	require.NotEqual(t, firstKey.snapshotFingerprint, secondKey.snapshotFingerprint)
	require.NotEqual(t, firstKey, secondKey)
}

func TestFetchMidjourneyTaskBatchForTaskRejectsNilTask(t *testing.T) {
	baseURL := "https://midjourney.example.test"
	channel := &model.Channel{Id: 201, Type: constant.ChannelTypeMidjourney, BaseURL: &baseURL}

	_, err := fetchMidjourneyTaskBatchForTask(nil, channel, nil, "credential", []string{"mj-1"})
	require.ErrorContains(t, err, "midjourney polling task is required")
}

func TestResolveMidjourneyPollingKeyRejectsMissingCurrentSnapshotCredential(t *testing.T) {
	baseURL := "https://midjourney.example.test"
	channel := &model.Channel{
		Id:      202,
		Type:    constant.ChannelTypeMidjourney,
		Key:     "rotated-current-key",
		BaseURL: &baseURL,
	}
	task := &model.Midjourney{
		MjId: "mj-missing-persisted-key",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: &model.TaskRouteSnapshot{
				SchemaVersion: model.TaskRouteSnapshotSchemaVersion,
			},
		},
	}

	key, err := resolveMidjourneyPollingKey(channel, task)
	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "no persisted credential for current route snapshot")
}

func TestFetchMidjourneyTaskBatchWithClaimsSkipsBusyTaskAndReleasesClaim(t *testing.T) {
	service.InitHttpClient()
	fetchSetting := system_setting.GetFetchSetting()
	previousSSRF := fetchSetting.EnableSSRFProtection
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { fetchSetting.EnableSSRFProtection = previousSSRF })
	redisServer := miniredis.RunT(t)
	previousRedisEnabled := appcommon.RedisEnabled
	previousRDB := appcommon.RDB
	appcommon.RedisEnabled = true
	appcommon.RDB = redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = appcommon.RDB.Close()
		appcommon.RedisEnabled = previousRedisEnabled
		appcommon.RDB = previousRDB
	})

	var requestCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		var requestBody struct {
			IDs []string `json:"ids"`
		}
		if body, readErr := io.ReadAll(r.Body); readErr == nil {
			_ = appcommon.Unmarshal(body, &requestBody)
		}
		responseID := "mj-b"
		if len(requestBody.IDs) > 0 && strings.TrimSpace(requestBody.IDs[0]) != "" {
			responseID = strings.TrimSpace(requestBody.IDs[0])
		}
		w.Header().Set("Content-Type", "application/json")
		responseBody, _ := appcommon.Marshal([]map[string]any{{"id": responseID, "status": "IN_PROGRESS"}})
		_, _ = w.Write(responseBody)
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	channel := &model.Channel{
		Id:      201,
		Type:    constant.ChannelTypeMidjourney,
		Key:     "credential",
		Status:  appcommon.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	snapshot := newMidjourneyPollingSnapshotForTest(t, "", "static")
	snapshot.BaseURL = baseURL
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	taskA := &model.Midjourney{
		Id:        1801,
		ChannelId: channel.Id,
		MjId:      "mj-a",
		Action:    "image",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}
	taskB := &model.Midjourney{
		Id:        1802,
		ChannelId: channel.Id,
		MjId:      "mj-b",
		Action:    "image",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}
	tasksByID := map[string]*model.Midjourney{"mj-a": taskA, "mj-b": taskB}
	busyClaim, err := service.AcquireTaskPollingClaim(context.Background(), midjourneyPollingClaimTask(taskA))
	require.NoError(t, err)
	require.NotNil(t, busyClaim)

	applyObservedClaim := false
	claimed, responseItems, err := fetchMidjourneyTaskBatchWithClaimsAndApply(
		context.Background(),
		channel,
		"credential",
		[]string{"mj-a", "mj-b"},
		tasksByID,
		func(_ context.Context, _ []string, _ []dto.MidjourneyDto, _ map[string]*model.Midjourney) error {
			applyObservedClaim = true
			claimDuringApply, claimErr := service.AcquireTaskPollingClaim(context.Background(), midjourneyPollingClaimTask(taskB))
			require.ErrorIs(t, claimErr, service.ErrTaskPollingClaimBusy)
			require.Nil(t, claimDuringApply)
			return nil
		},
	)
	require.NoError(t, err)
	require.True(t, applyObservedClaim)
	require.Equal(t, []string{"mj-b"}, claimed)
	require.Len(t, responseItems, 1)
	require.Equal(t, "mj-b", responseItems[0].MjId)
	require.Equal(t, int32(1), requestCount.Load())
	claimAfterApply, err := service.AcquireTaskPollingClaim(context.Background(), midjourneyPollingClaimTask(taskB))
	require.NoError(t, err)
	require.NotNil(t, claimAfterApply)
	claimAfterApply.Release()

	busyClaim.Release()
	claimAfterRelease, err := service.AcquireTaskPollingClaim(context.Background(), midjourneyPollingClaimTask(taskA))
	require.NoError(t, err)
	require.NotNil(t, claimAfterRelease)
	claimAfterRelease.Release()

	claimed, responseItems, err = fetchMidjourneyTaskBatchWithClaims(
		context.Background(),
		channel,
		"credential",
		[]string{"mj-a"},
		map[string]*model.Midjourney{"mj-a": taskA},
	)
	require.NoError(t, err)
	require.Equal(t, []string{"mj-a"}, claimed)
	require.Len(t, responseItems, 1)
	require.Equal(t, "mj-a", responseItems[0].MjId)
	require.Equal(t, int32(2), requestCount.Load())
}

func TestFetchMidjourneyTaskBatchWithClaimsFailsClosedWhenRedisUnavailable(t *testing.T) {
	service.InitHttpClient()
	previousRedisEnabled := appcommon.RedisEnabled
	previousRDB := appcommon.RDB
	appcommon.RedisEnabled = false
	appcommon.RDB = nil
	t.Setenv("TASK_POLLING_CLAIM_STRICT", "true")
	t.Cleanup(func() {
		appcommon.RedisEnabled = previousRedisEnabled
		appcommon.RDB = previousRDB
	})

	var requestCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	channel := &model.Channel{
		Id:      201,
		Type:    constant.ChannelTypeMidjourney,
		Key:     "credential",
		Status:  appcommon.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	task := &model.Midjourney{
		Id:        1803,
		ChannelId: channel.Id,
		MjId:      "mj-unclaimed",
		Action:    "image",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: func() *model.TaskRouteSnapshot {
				snapshot := newMidjourneyPollingSnapshotForTest(t, "", "static")
				snapshot.BaseURL = baseURL
				snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
				return snapshot
			}(),
		},
	}

	claimed, responseItems, err := fetchMidjourneyTaskBatchWithClaims(
		context.Background(),
		channel,
		"credential",
		[]string{task.MjId},
		map[string]*model.Midjourney{task.MjId: task},
	)
	require.Nil(t, claimed)
	require.Nil(t, responseItems)
	require.ErrorIs(t, err, service.ErrTaskPollingClaimUnavailable)
	require.Equal(t, int32(0), requestCount.Load())
}

func TestFetchMidjourneyTaskBatchWithClaimsRejects2xxWithoutRecognizedStatus(t *testing.T) {
	service.InitHttpClient()
	fetchSetting := system_setting.GetFetchSetting()
	previousSSRF := fetchSetting.EnableSSRFProtection
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { fetchSetting.EnableSSRFProtection = previousSSRF })

	redisServer := miniredis.RunT(t)
	previousRedisEnabled := appcommon.RedisEnabled
	previousRDB := appcommon.RDB
	appcommon.RedisEnabled = true
	appcommon.RDB = redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() {
		_ = appcommon.RDB.Close()
		appcommon.RedisEnabled = previousRedisEnabled
		appcommon.RDB = previousRDB
	})

	var requestCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	channel := &model.Channel{
		Id:      201,
		Type:    constant.ChannelTypeMidjourney,
		Key:     "credential",
		Status:  appcommon.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}
	snapshot := newMidjourneyPollingSnapshotForTest(t, "", "static")
	snapshot.BaseURL = baseURL
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	task := &model.Midjourney{
		Id:        1804,
		ChannelId: channel.Id,
		MjId:      "mj-no-recognized-status",
		Action:    "image",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}

	claimed, responseItems, err := fetchMidjourneyTaskBatchWithClaims(
		context.Background(),
		channel,
		"credential",
		[]string{task.MjId},
		map[string]*model.Midjourney{task.MjId: task},
	)
	require.Equal(t, []string{task.MjId}, claimed)
	require.Nil(t, responseItems)
	require.ErrorContains(t, err, "no recognized task status")
	require.Equal(t, int32(1), requestCount.Load())
}
