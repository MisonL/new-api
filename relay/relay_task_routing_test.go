package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmissionModelNamePrefersOriginRouteSnapshot(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName: "client-model",
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginRouteRequestedModel: "origin-model",
		},
	}

	require.Equal(t, "origin-model", taskSubmissionModelName(info, constant.TaskPlatform("sora"), constant.TaskActionRemix))
}

func TestTaskRouteSnapshotForContinuationReturnsOriginalSnapshot(t *testing.T) {
	snapshot := &model.TaskRouteSnapshot{SchemaVersion: model.TaskRouteSnapshotSchemaVersion}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginRouteSnapshot: snapshot,
		},
	}

	got, err := TaskRouteSnapshotForContinuation(info)

	require.NoError(t, err)
	require.Same(t, snapshot, got)
}

func TestTaskRouteSnapshotForContinuationRejectsInvalidContextValue(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginRouteSnapshot: "not-a-route-snapshot",
		},
	}

	_, err := TaskRouteSnapshotForContinuation(info)

	require.Error(t, err)
	require.ErrorContains(t, err, "origin task route snapshot has invalid type")
}

func TestApplyTaskSubmissionModelKeepsPersistedEffectiveModelAfterMappingChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos/task/remix", nil)
	c.Set("model_mapping", `{"origin-model":"new-provider-model"}`)

	info := &relaycommon.RelayInfo{
		OriginModelName: "client-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 201,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginRouteRequestedModel: "origin-model",
			OriginRouteEffectiveModel: "old-provider-model",
			OriginRouteChannelID:      201,
		},
	}

	require.NoError(t, applyTaskSubmissionModel(c, info, taskSubmissionModelName(info, constant.TaskPlatform("sora"), constant.TaskActionRemix)))
	require.Equal(t, "origin-model", info.OriginModelName)
	require.Equal(t, "old-provider-model", info.UpstreamModelName)
	require.Equal(t, "old-provider-model", info.ChannelMeta.UpstreamModelName)
	require.True(t, info.IsModelMapped)
}

func TestApplyTaskSubmissionModelRejectsSnapshotChannelMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 202},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginRouteEffectiveModel: "old-provider-model",
			OriginRouteChannelID:      201,
		},
	}

	err := applyTaskSubmissionModel(c, info, "origin-model")
	require.ErrorIs(t, err, errTaskRouteSnapshotMismatch)
}

func TestSyncTaskSubmissionChannelIdentityAcceptsCurrentAttemptMetadata(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 201,
		},
	}

	require.NoError(t, syncTaskSubmissionChannelIdentity(info))
	require.Equal(t, 201, info.ChannelId)
}

func TestSyncTaskSubmissionChannelIdentityRejectsMissingMetadata(t *testing.T) {
	require.Error(t, syncTaskSubmissionChannelIdentity(&relaycommon.RelayInfo{}))
}

func TestRestoreOriginTaskGroupPrefersConcreteSnapshotAndBindsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UsingGroup: "auto", TokenGroup: "auto"}
	task := &model.Task{
		Group: "row-group",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: &model.TaskRouteSnapshot{Group: "premium"},
		},
	}

	restoreOriginTaskGroup(c, info, task)

	require.Equal(t, "premium", info.UsingGroup)
	require.Equal(t, "premium", common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
	require.Equal(t, "premium", common.GetContextKeyString(c, constant.ContextKeyAutoGroup))
}

func TestRestoreOriginTaskGroupFallsBackFromAutoSnapshotToTaskRow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{UsingGroup: "auto", TokenGroup: "auto"}
	task := &model.Task{
		Group: "coding",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: &model.TaskRouteSnapshot{Group: "AUTO"},
		},
	}

	restoreOriginTaskGroup(c, info, task)

	require.Equal(t, "coding", info.UsingGroup)
	require.Equal(t, "coding", common.GetContextKeyString(c, constant.ContextKeyUsingGroup))
	require.Equal(t, "coding", common.GetContextKeyString(c, constant.ContextKeyAutoGroup))
}

func TestBindTaskSubmissionActionUsesViduIntentBeforeMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntent{
		Action: constant.TaskActionReferenceGenerate,
	})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeVidu}}

	require.Equal(t, constant.TaskActionReferenceGenerate, bindTaskSubmissionAction(c, info, constant.TaskPlatformVidu))
	require.Equal(t, constant.TaskActionReferenceGenerate, info.Action)
}

func TestGetTaskAdaptorAcceptsCanonicalViduPlatform(t *testing.T) {
	require.NotNil(t, GetTaskAdaptor(constant.TaskPlatformVidu))
}

func TestBindTaskSubmissionActionCollapsesInferredImageActionForOtherProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntent{
		Action: constant.TaskActionReferenceGenerate,
	})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}

	require.Equal(t, constant.TaskActionGenerate, bindTaskSubmissionAction(c, info, constant.TaskPlatform("video")))
	require.Equal(t, constant.TaskActionGenerate, info.Action)
}

func TestBindTaskSubmissionActionPreservesExplicitImageActionForOtherProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyTaskRouteIntent, relaycommon.TaskRouteIntent{
		Action:         constant.TaskActionReferenceGenerate,
		ExplicitAction: true,
	})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}

	require.Equal(t, constant.TaskActionReferenceGenerate, bindTaskSubmissionAction(c, info, constant.TaskPlatform("video")))
	require.Equal(t, constant.TaskActionReferenceGenerate, info.Action)
	intent, ok := common.GetContextKeyType[relaycommon.TaskRouteIntent](c, constant.ContextKeyTaskRouteIntent)
	require.True(t, ok)
	require.True(t, intent.ExplicitAction)
}

func TestTaskSubmissionActionMismatchIsLocalAndNonRetryable(t *testing.T) {
	err := taskSubmissionActionMismatchError(constant.TaskActionGenerate, constant.TaskActionTextGenerate)

	require.True(t, err.LocalError)
	require.Equal(t, "task_route_action_mismatch", err.Code)
	require.Equal(t, http.StatusInternalServerError, err.StatusCode)
}
