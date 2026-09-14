package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func newTaskCredentialTestChannel(id int, keys []string) *model.Channel {
	return &model.Channel{
		Id:   id,
		Keys: keys,
		Key:  strings.Join(keys, "\n"),
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
}

func TestResolveTaskChannelKeyUsesPersistedConcreteKey(t *testing.T) {
	channel := &model.Channel{
		Id:   101,
		Key:  "key-a\nkey-b",
		Keys: []string{"key-a", "key-b"},
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	task := &model.Task{TaskID: "task-1", PrivateData: model.TaskPrivateData{Key: "key-b"}}

	key, err := ResolveTaskChannelKey(channel, task)

	require.NoError(t, err)
	require.Equal(t, "key-b", key)
}

func TestResolveTaskCredentialRejectsCurrentSnapshotIndexReorder(t *testing.T) {
	channel := newTaskCredentialTestChannel(105, []string{"key-b", "key-a"})
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:    model.TaskRouteSnapshotSchemaVersion,
		CredentialScoped: true,
		MultiKeyIndex:    1,
	}

	key, err := ResolveTaskCredential(channel, snapshot, "key-b")

	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "index mismatch")
}

func TestResolveTaskCredentialRejectsCurrentSnapshotWithoutCredentialScope(t *testing.T) {
	channel := newTaskCredentialTestChannel(106, []string{"key-a", "key-b"})
	snapshot := &model.TaskRouteSnapshot{SchemaVersion: model.TaskRouteSnapshotSchemaVersion}

	key, err := ResolveTaskCredential(channel, snapshot, "key-b")

	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "missing credential scope")
}

func TestResolveTaskChannelKeyRejectsMissingCredentialForCurrentSingleKeySnapshot(t *testing.T) {
	channel := &model.Channel{Id: 110, Key: "rotated-key"}
	task := &model.Task{
		TaskID: "task-missing-current-key",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: &model.TaskRouteSnapshot{SchemaVersion: model.TaskRouteSnapshotSchemaVersion},
		},
	}

	key, err := ResolveTaskChannelKey(channel, task)

	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "no persisted credential")
}

func TestResolveTaskCredentialRejectsAmbiguousLegacyDuplicateKey(t *testing.T) {
	channel := newTaskCredentialTestChannel(107, []string{"key-a", "key-a"})

	key, err := ResolveTaskCredential(channel, nil, "key-a")

	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "ambiguous")
}

func TestResolveTaskCredentialChecksCapturedIndexStatus(t *testing.T) {
	channel := newTaskCredentialTestChannel(108, []string{"key-a", "key-b"})
	channel.ChannelInfo.MultiKeyStatusList = map[int]int{1: common.ChannelStatusAutoDisabled}
	snapshot := &model.TaskRouteSnapshot{
		SchemaVersion:    model.TaskRouteSnapshotSchemaVersion,
		CredentialScoped: true,
		MultiKeyIndex:    1,
	}

	key, err := ResolveTaskCredential(channel, snapshot, "key-b")

	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "disabled")
}

func TestResolveTaskCredentialAllowsUniqueLegacyKeyAfterReorder(t *testing.T) {
	channel := newTaskCredentialTestChannel(109, []string{"key-b", "key-a"})

	key, err := ResolveTaskCredential(channel, nil, "key-a")

	require.NoError(t, err)
	require.Equal(t, "key-a", key)
}

func TestResolveTaskChannelKeyRejectsAmbiguousLegacyMultiKeyTask(t *testing.T) {
	channel := &model.Channel{
		Id:   102,
		Key:  "key-a\nkey-b",
		Keys: []string{"key-a", "key-b"},
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	task := &model.Task{TaskID: "legacy-task"}

	key, err := ResolveTaskChannelKey(channel, task)

	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "no persisted credential")
}

func TestResolveTaskChannelKeyRejectsRemovedPersistedKey(t *testing.T) {
	channel := &model.Channel{
		Id:   103,
		Key:  "key-a\nkey-b",
		Keys: []string{"key-a", "key-b"},
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
	}
	task := &model.Task{TaskID: "task-removed", PrivateData: model.TaskPrivateData{Key: "key-c"}}

	key, err := ResolveTaskChannelKey(channel, task)

	require.Error(t, err)
	require.Empty(t, key)
	require.Contains(t, err.Error(), "no longer available")
}

func TestResolveTaskChannelKeyFallsBackOnlyForSingleKeyChannel(t *testing.T) {
	channel := &model.Channel{Id: 104, Key: "key-single"}
	task := &model.Task{TaskID: "legacy-single"}

	key, err := ResolveTaskChannelKey(channel, task)

	require.NoError(t, err)
	require.Equal(t, "key-single", key)
}

func TestParseNewAPITaskPollingEnvelopeRequiresPersistedTaskIdentity(t *testing.T) {
	providerBody, err := common.Marshal(map[string]any{
		"code": dto.TaskSuccessCode,
		"data": map[string]any{
			"task_id": "provider-task",
			"status":  "success",
			"url":     "https://provider.example.test/result.mp4",
		},
	})
	require.NoError(t, err)
	parsed, ok := parseNewAPITaskPollingEnvelope(providerBody)
	require.False(t, ok)
	require.Nil(t, parsed)
}

func TestParseNewAPITaskPollingEnvelopeAcceptsPersistedTaskShape(t *testing.T) {
	body, err := common.Marshal(dto.TaskResponse[model.Task]{
		Code: dto.TaskSuccessCode,
		Data: model.Task{
			ID:        901,
			TaskID:    "public-task",
			Platform:  constant.TaskPlatformSuno,
			UserId:    42,
			Group:     "default",
			ChannelId: 201,
			Action:    constant.TaskActionGenerate,
			Status:    model.TaskStatusSuccess,
		},
	})
	require.NoError(t, err)
	parsed, ok := parseNewAPITaskPollingEnvelope(body)
	require.True(t, ok)
	require.NotNil(t, parsed)
	require.Equal(t, int64(901), parsed.ID)
	require.Equal(t, "public-task", parsed.TaskID)
}

func TestParseNewAPITaskPollingEnvelopePreservesTaskDtoResultURL(t *testing.T) {
	providerData := json.RawMessage(`{"response":{"video":"https://cdn.example/provider.mp4"}}`)
	body, err := common.Marshal(dto.TaskResponse[any]{
		Code: dto.TaskSuccessCode,
		Data: dto.TaskDto{
			ID:        902,
			TaskID:    "public-task-with-result",
			Platform:  string(constant.TaskPlatformSuno),
			UserId:    42,
			Group:     "default",
			ChannelId: 201,
			Action:    constant.TaskActionGenerate,
			Status:    string(model.TaskStatusSuccess),
			ResultURL: "https://cdn.example/provider.mp4",
			Data:      providerData,
		},
	})
	require.NoError(t, err)

	parsed, ok := parseNewAPITaskPollingEnvelope(body)
	require.True(t, ok)
	require.NotNil(t, parsed)
	require.Equal(t, "https://cdn.example/provider.mp4", parsed.GetResultURL())
	require.Equal(t, providerData, parsed.Data)
}

func TestReadTaskPollingResponseBodyEnforcesBound(t *testing.T) {
	body, err := readTaskPollingResponseBodyWithLimit(strings.NewReader("12345"), 4)
	require.Error(t, err)
	require.Nil(t, body)
	require.Contains(t, err.Error(), "exceeds 4 bytes")

	body, err = readTaskPollingResponseBodyWithLimit(strings.NewReader("1234"), 4)
	require.NoError(t, err)
	require.Equal(t, []byte("1234"), body)

	_, err = ReadTaskPollingResponseBody(nil)
	require.Error(t, err)
}

func TestApplyTaskPollingPayloadPreservesExistingDataForEmptyNewAPIEnvelope(t *testing.T) {
	oldData := json.RawMessage(`{"provider":"payload"}`)
	task := &model.Task{Data: oldData}

	applyTaskPollingPayload(task, true, nil, []byte(`{"code":"success","data":{}}`))

	require.Equal(t, oldData, task.Data)
	applyTaskPollingPayload(task, true, []byte(`null`), []byte(`{"code":"success","data":null}`))
	require.Equal(t, oldData, task.Data)
	applyTaskPollingPayload(task, true, []byte(`{}`), []byte(`{"code":"success","data":{}}`))
	require.Equal(t, oldData, task.Data)
}

func TestRedactVideoResponseBodyRedactsNestedSecretsAndInlinePayloads(t *testing.T) {
	longVideo := strings.Repeat("a", 300)
	body, err := common.Marshal(map[string]any{
		"response": map[string]any{
			"video":              "https://cdn.example/video.mp4",
			"bytesBase64Encoded": "large-secret-payload",
			"authorization":      "Bearer secret-token",
			"nested":             map[string]any{"bytes_base64_encoded": "nested-payload", "api_key": "nested-secret"},
			"inline":             map[string]any{"video": longVideo},
		},
	})
	require.NoError(t, err)

	redacted := RedactVideoResponseBody(body)
	var got map[string]any
	require.NoError(t, common.Unmarshal(redacted, &got))
	response, ok := got["response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "https://cdn.example/video.mp4", response["video"])
	require.NotContains(t, response, "bytesBase64Encoded")
	require.NotContains(t, response, "authorization")
	nested, ok := response["nested"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, nested, "bytes_base64_encoded")
	require.NotContains(t, nested, "api_key")
	inline, ok := response["inline"].(map[string]any)
	require.True(t, ok)
	require.Len(t, inline["video"].(string), 259)
}

func TestRedactVideoResponseBodyOmitsMalformedPayload(t *testing.T) {
	body := []byte(`<html>Authorization: Bearer provider-secret</html>`)

	redacted := RedactVideoResponseBody(body)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(redacted, &payload))
	require.Equal(t, true, payload["response_body_omitted"])
	require.Equal(t, float64(len(body)), payload["response_body_bytes"])
	require.NotContains(t, string(redacted), "provider-secret")
}

func TestRedactVideoResponseBodyBoundsArbitraryStringsInNestedArrays(t *testing.T) {
	largeText := strings.Repeat("x", maxPersistedTaskPayloadStringBytes+128)
	largeURL := "https://cdn.example/" + strings.Repeat("u", maxPersistedTaskPayloadURLBytes+128)
	body, err := common.Marshal(map[string]any{
		"status":        "completed",
		"diagnostics":   largeText,
		"array":         []any{largeText},
		"short_url":     "https://cdn.example/video.mp4",
		"oversized_url": largeURL,
	})
	require.NoError(t, err)

	redacted := RedactVideoResponseBody(body)
	var got map[string]any
	require.NoError(t, common.Unmarshal(redacted, &got))
	require.Equal(t, "completed", got["status"])
	require.Equal(t, "https://cdn.example/video.mp4", got["short_url"])
	require.Len(t, got["diagnostics"].(string), maxPersistedTaskPayloadStringBytes)
	require.Len(t, got["oversized_url"].(string), maxPersistedTaskPayloadURLBytes)
	array := got["array"].([]any)
	require.Len(t, array[0].(string), maxPersistedTaskPayloadStringBytes)
}

func TestParseNewAPITaskPollingEnvelopeRejectsDifferentExpectedUpstreamTask(t *testing.T) {
	body, err := common.Marshal(dto.TaskResponse[model.Task]{
		Code: dto.TaskSuccessCode,
		Data: model.Task{
			ID:        901,
			TaskID:    "remote-task-a",
			Platform:  constant.TaskPlatform("201"),
			UserId:    42,
			Group:     "default",
			ChannelId: 701,
			Action:    constant.TaskActionGenerate,
			Status:    model.TaskStatusSuccess,
		},
	})
	require.NoError(t, err)

	parsed, ok := parseNewAPITaskPollingEnvelope(body, &model.Task{
		TaskID: "local-task",
		Action: constant.TaskActionGenerate,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "remote-task-b",
		},
	})

	require.False(t, ok)
	require.Nil(t, parsed)
}

func TestParseNewAPITaskPollingEnvelopeAcceptsExpectedUpstreamTaskAndAction(t *testing.T) {
	body, err := common.Marshal(dto.TaskResponse[model.Task]{
		Code: dto.TaskSuccessCode,
		Data: model.Task{
			ID:        901,
			TaskID:    "remote-task-a",
			Platform:  constant.TaskPlatform("201"),
			UserId:    42,
			Group:     "default",
			ChannelId: 701,
			Action:    constant.TaskActionGenerate,
			Status:    model.TaskStatusSuccess,
		},
	})
	require.NoError(t, err)

	parsed, ok := parseNewAPITaskPollingEnvelope(body, &model.Task{
		TaskID: "local-task",
		Action: constant.TaskActionGenerate,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "remote-task-a",
		},
	})

	require.True(t, ok)
	require.NotNil(t, parsed)
	require.Equal(t, "remote-task-a", parsed.TaskID)
}

func TestRedactTaskPollingErrorMessageUsesPersistedCredentialOnResolutionFailure(t *testing.T) {
	message := "persisted credential key-removed is no longer available"
	got := redactTaskPollingErrorMessage(message, "key-removed")
	require.Equal(t, "persisted credential [redacted] is no longer available", got)
}

func TestFormatTaskPollingFailureLogDoesNotSerializePrivateData(t *testing.T) {
	task := &model.Task{
		TaskID:     "task-private-log",
		ChannelId:  201,
		Platform:   constant.TaskPlatform("201"),
		Action:     constant.TaskActionGenerate,
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "upstream rejected credential secret-key-201",
		PrivateData: model.TaskPrivateData{
			Key: "secret-key-201",
		},
	}

	logLine := formatTaskPollingFailureLog(task)

	require.Contains(t, logLine, "task_id=task-private-log")
	require.Contains(t, logLine, "channel_id=201")
	require.Contains(t, logLine, "[redacted]")
	require.NotContains(t, logLine, "secret-key-201")
	require.NotContains(t, logLine, "private_data")
}

func TestFormatTaskResultDebugLogDoesNotSerializeResultURLs(t *testing.T) {
	result := &relaycommon.TaskInfo{
		TaskID:           "task-result-log",
		Status:           "SUCCESS",
		Progress:         "100%",
		Url:              "https://cdn.example.test/video.mp4?sig=secret-signature",
		RemoteUrl:        "https://cdn.example.test/remote.mp4?token=remote-secret",
		Reason:           "provider rejected credential secret-key-201",
		CompletionTokens: 3,
		TotalTokens:      7,
	}

	logLine := formatTaskResultDebugLog(result, "secret-key-201")

	require.Contains(t, logLine, "task_id=task-result-log")
	require.Contains(t, logLine, "status=\"SUCCESS\"")
	require.Contains(t, logLine, "has_url=true")
	require.Contains(t, logLine, "completion_tokens=3")
	require.Contains(t, logLine, "total_tokens=7")
	require.Contains(t, logLine, "[redacted]")
	require.NotContains(t, logLine, "cdn.example.test")
	require.NotContains(t, logLine, "secret-signature")
	require.NotContains(t, logLine, "remote-secret")
	require.NotContains(t, logLine, "secret-key-201")
}

func TestMergeTaskPollingFailureReasonPreservesDiagnosticsForSparseFailure(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		incoming string
		want     string
	}{
		{name: "incoming reason wins", existing: "local reason", incoming: "provider reason", want: "provider reason"},
		{name: "existing reason survives empty provider reason", existing: "local reason", incoming: "", want: "local reason"},
		{name: "whitespace provider reason is absent", existing: "local reason", incoming: "  \t", want: "local reason"},
		{name: "default reason is explicit", existing: "", incoming: "", want: defaultTaskPollingFailureReason},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, mergeTaskPollingFailureReason(tt.existing, tt.incoming))
		})
	}
}

func TestResolveTaskPollingTaskSupportsAllReferenceForms(t *testing.T) {
	byDatabase := &model.Task{ID: 41, TaskID: "public-41", PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-41"}}
	byUpstream := &model.Task{ID: 42, TaskID: "public-42", PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-42"}}
	byLegacy := &model.Task{ID: 43, TaskID: "legacy-43"}
	tasks := map[string]*model.Task{
		"task-db:41":       byDatabase,
		"task-upstream:42": byUpstream,
		"legacy-key":       byLegacy,
	}

	tests := map[string]*model.Task{
		"task-db:41":                byDatabase,
		"task-upstream:upstream-41": byDatabase,
		"upstream-41":               byDatabase,
		"public-41":                 byDatabase,
		"task-upstream:upstream-42": byUpstream,
		"upstream-42":               byUpstream,
		"public-42":                 byUpstream,
		"legacy-43":                 byLegacy,
	}
	for ref, expected := range tests {
		ref, expected := ref, expected
		t.Run(ref, func(t *testing.T) {
			got, err := resolveTaskPollingTask(ref, tasks)
			require.NoError(t, err)
			require.Same(t, expected, got)
		})
	}
}

func TestResolveTaskPollingTaskRejectsAmbiguousUpstreamReference(t *testing.T) {
	first := &model.Task{ID: 51, TaskID: "public-51", PrivateData: model.TaskPrivateData{UpstreamTaskID: "same-upstream"}}
	second := &model.Task{ID: 52, TaskID: "public-52", PrivateData: model.TaskPrivateData{UpstreamTaskID: "same-upstream"}}
	tasks := map[string]*model.Task{
		"task-db:51": first,
		"task-db:52": second,
	}

	got, err := resolveTaskPollingTask("same-upstream", tasks)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrTaskPollingReferenceAmbiguous)
}

func TestResolveTaskPollingTaskPrefersDirectMapHit(t *testing.T) {
	direct := &model.Task{ID: 61, TaskID: "public-direct", PrivateData: model.TaskPrivateData{UpstreamTaskID: "same-ref"}}
	other := &model.Task{ID: 62, TaskID: "public-other", PrivateData: model.TaskPrivateData{UpstreamTaskID: "same-ref"}}
	tasks := map[string]*model.Task{
		"same-ref":   direct,
		"task-db:62": other,
	}

	got, err := resolveTaskPollingTask("same-ref", tasks)
	require.NoError(t, err)
	require.Same(t, direct, got)
}

func newVideoPollingSnapshotForTest(t *testing.T, channelID int, channelType int) *model.TaskRouteSnapshot {
	t.Helper()
	platform, ok := taskAdapterPlatformForChannelType(channelType)
	require.True(t, ok)
	snapshot, err := BuildTaskRouteSnapshot(nil, &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "client-video",
		UpstreamRequestPath: "/v1/videos",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       channelType,
			ChannelId:         channelID,
			ChannelBaseUrl:    "https://submit.example.test",
			UpstreamModelName: "provider-video",
		},
	}, platform, constant.TaskActionGenerate)
	require.NoError(t, err)
	return snapshot
}

func TestGroupVideoTaskRefsByAdapterUsesTaskRouteSnapshot(t *testing.T) {
	openAI := &model.Task{
		ID:        901,
		ChannelId: 901,
		Platform:  "outer-platform",
		TaskID:    "openai-task",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: newVideoPollingSnapshotForTest(t, 901, constant.ChannelTypeOpenAI),
		},
	}
	sora := &model.Task{
		ID:        902,
		ChannelId: 902,
		Platform:  "outer-platform",
		TaskID:    "sora-task",
		PrivateData: model.TaskPrivateData{
			RouteSnapshot: newVideoPollingSnapshotForTest(t, 902, constant.ChannelTypeSora),
		},
	}

	grouped, groupingErrors := groupVideoTaskRefsByAdapter("outer-platform", map[int][]string{
		901: {"task-db:901"},
		902: {"task-db:902"},
	}, map[string]*model.Task{
		"task-db:901": openAI,
		"task-db:902": sora,
	})

	require.Empty(t, groupingErrors)
	require.Equal(t, []string{"task-db:901"}, grouped[videoTaskPollingGroupKey{channelID: 901, platform: "1"}])
	require.Equal(t, []string{"task-db:902"}, grouped[videoTaskPollingGroupKey{channelID: 902, platform: "55"}])
}

func TestGroupVideoTaskRefsByAdapterRejectsChannelMismatch(t *testing.T) {
	task := &model.Task{ID: 903, ChannelId: 904, Platform: "1", TaskID: "mismatched-task"}
	grouped, groupingErrors := groupVideoTaskRefsByAdapter("1", map[int][]string{
		903: {"task-db:903"},
	}, map[string]*model.Task{
		"task-db:903": task,
	})

	require.Empty(t, grouped)
	require.Len(t, groupingErrors, 1)
	require.Contains(t, groupingErrors[0].Error(), "belongs to channel 904")
}

func TestRunSunoTaskPollingBatchesContinuesAfterFailureWithoutCredentialLeak(t *testing.T) {
	batches := []sunoTaskPollingBatch{
		{key: sunoTaskPollingBatchKey{credential: "secret-a", model: "public-a", effective: "provider-a", group: "default"}, taskIDs: []string{"task-a"}},
		{key: sunoTaskPollingBatchKey{credential: "secret-b", model: "public-b", effective: "provider-b", group: "default"}, taskIDs: []string{"task-b", "task-c"}},
	}
	var called []string
	err := runSunoTaskPollingBatches(context.Background(), 701, batches, func(batch sunoTaskPollingBatch) error {
		called = append(called, batch.key.credential)
		return errors.New("upstream rejected credential " + batch.key.credential)
	})

	require.Error(t, err)
	require.Equal(t, []string{"secret-a", "secret-b"}, called)
	message := err.Error()
	require.NotContains(t, message, "secret-a")
	require.NotContains(t, message, "secret-b")
	require.Contains(t, message, `model="public-a"`)
	require.Contains(t, message, `effective_model="provider-b"`)
	require.Contains(t, message, "task_count=2")
}

func TestSunoTaskPollingBatchKeySeparatesHeaderPolicyAndProfileIdentity(t *testing.T) {
	base := sunoTaskPollingBatchKey{
		credential:              "credential",
		model:                   "public-model",
		effective:               "provider/model",
		group:                   "default",
		baseURL:                 "https://suno.example.test",
		proxy:                   "",
		operation:               "task.suno.music.poll",
		path:                    "/suno/fetch",
		fingerprint:             "route-a",
		snapshotFingerprint:     "snapshot-a",
		profile:                 "profile-a@1",
		headerPolicyFingerprint: "header-policy-a",
	}

	require.NotEqual(t, base, sunoTaskPollingBatchKey{
		credential:              base.credential,
		model:                   base.model,
		effective:               base.effective,
		group:                   base.group,
		baseURL:                 base.baseURL,
		proxy:                   base.proxy,
		operation:               base.operation,
		path:                    base.path,
		fingerprint:             base.fingerprint,
		snapshotFingerprint:     base.snapshotFingerprint,
		profile:                 base.profile,
		headerPolicyFingerprint: "header-policy-b",
	})
	require.NotEqual(t, base, sunoTaskPollingBatchKey{
		credential:              base.credential,
		model:                   base.model,
		effective:               base.effective,
		group:                   base.group,
		baseURL:                 base.baseURL,
		proxy:                   base.proxy,
		operation:               base.operation,
		path:                    base.path,
		fingerprint:             base.fingerprint,
		snapshotFingerprint:     base.snapshotFingerprint,
		profile:                 "profile-b@1",
		headerPolicyFingerprint: base.headerPolicyFingerprint,
	})

	secondSnapshot := base
	secondSnapshot.snapshotFingerprint = "snapshot-b"
	require.NotEqual(t, base, secondSnapshot)
}

func TestSunoTaskPollingBatchKeyGroupsOnlyIdenticalRouteIdentity(t *testing.T) {
	first := sunoTaskPollingBatchKey{
		credential:              "credential",
		model:                   "public-model",
		effective:               "provider/model",
		group:                   "default",
		baseURL:                 "https://suno.example.test",
		operation:               "task.suno.music.poll",
		path:                    "/suno/fetch",
		fingerprint:             "route",
		snapshotFingerprint:     "snapshot",
		profile:                 "profile@1",
		headerPolicyFingerprint: "header-policy",
	}
	second := first

	idsByKey := map[sunoTaskPollingBatchKey][]string{}
	idsByKey[first] = append(idsByKey[first], "task-a")
	idsByKey[second] = append(idsByKey[second], "task-b")

	require.Len(t, idsByKey, 1)
	require.Equal(t, []string{"task-a", "task-b"}, idsByKey[first])
}

func TestSunoTaskPollingBatchKeySeparatesProviderRuntimeSnapshotFingerprint(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UsingGroup:          "default",
		OriginModelName:     "suno_music",
		UpstreamRequestPath: "/suno/submit/music",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeSunoAPI,
			ChannelId:         702,
			ChannelBaseUrl:    "https://suno.example.test",
			UpstreamModelName: "suno-v3",
		},
	}
	firstSnapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatformSuno, constant.SunoActionMusic)
	require.NoError(t, err)
	secondSnapshot, err := BuildTaskRouteSnapshot(nil, info, constant.TaskPlatformSuno, constant.SunoActionMusic)
	require.NoError(t, err)

	secondSnapshot.ProviderRuntime.Other = "provider-runtime-changed"
	secondSnapshot.SnapshotFingerprint = secondSnapshot.ComputeFingerprint()

	channel := &model.Channel{Id: 702, Type: constant.ChannelTypeSunoAPI}
	_, _, _, _, firstSnapshotFingerprint, firstProfile, firstHeaderPolicyFingerprint, err := sunoTaskRouteBatchIdentity(channel, &model.Task{
		PrivateData: model.TaskPrivateData{RouteSnapshot: firstSnapshot},
	})
	require.NoError(t, err)
	_, _, _, _, secondSnapshotFingerprint, secondProfile, secondHeaderPolicyFingerprint, err := sunoTaskRouteBatchIdentity(channel, &model.Task{
		PrivateData: model.TaskPrivateData{RouteSnapshot: secondSnapshot},
	})
	require.NoError(t, err)

	require.NotEqual(t, firstSnapshot.SnapshotFingerprint, secondSnapshot.SnapshotFingerprint)
	require.NotEqual(t, firstSnapshotFingerprint, secondSnapshotFingerprint)
	require.Equal(t, firstProfile, secondProfile)
	require.Equal(t, firstHeaderPolicyFingerprint, secondHeaderPolicyFingerprint)
}

func TestRecognizedTaskPollingStatus(t *testing.T) {
	for _, status := range []model.TaskStatus{model.TaskStatusSubmitted, model.TaskStatusQueued, model.TaskStatusInProgress, model.TaskStatusSuccess, model.TaskStatusFailure} {
		if !isRecognizedTaskPollingStatus(status) {
			t.Fatalf("expected status %q to be recognized", status)
		}
	}
	if isRecognizedTaskPollingStatus(model.TaskStatus("UNKNOWN_STATUS")) {
		t.Fatal("unknown task status must not be recognized")
	}
}

func TestShouldApplyTaskStatusRejectsTerminalReversal(t *testing.T) {
	require.True(t, ShouldApplyTaskStatus(model.TaskStatusInProgress, model.TaskStatusSuccess))
	require.True(t, ShouldApplyTaskStatus(model.TaskStatusInProgress, model.TaskStatusFailure))
	require.True(t, ShouldApplyTaskStatus(model.TaskStatusSuccess, model.TaskStatusSuccess))
	require.True(t, ShouldApplyTaskStatus(model.TaskStatusFailure, model.TaskStatusFailure))
	require.False(t, ShouldApplyTaskStatus(model.TaskStatusSuccess, model.TaskStatusFailure))
	require.False(t, ShouldApplyTaskStatus(model.TaskStatusFailure, model.TaskStatusSuccess))
	require.False(t, ShouldApplyTaskStatus(model.TaskStatusInProgress, model.TaskStatusQueued))
}

func TestApplyTaskResultURLPreservesExistingResultWhenProviderOmitsURL(t *testing.T) {
	tests := []struct {
		name       string
		existing   string
		incoming   string
		wantResult string
	}{
		{name: "direct URL", existing: "https://cdn.example/video.mp4", incoming: "", wantResult: "https://cdn.example/video.mp4"},
		{name: "data URL", existing: "data:video/mp4;base64,AAAA", incoming: "", wantResult: "data:video/mp4;base64,AAAA"},
		{name: "proxy URL", existing: "https://gateway.example/v1/videos/task-result/content", incoming: "", wantResult: "https://gateway.example/v1/videos/task-result/content"},
		{name: "new direct URL", existing: "", incoming: "https://cdn.example/new-video.mp4", wantResult: "https://cdn.example/new-video.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &model.Task{TaskID: "task-result", PrivateData: model.TaskPrivateData{ResultURL: tt.existing}}
			ApplyTaskResultURL(task, tt.incoming)
			require.Equal(t, tt.wantResult, task.GetResultURL())
		})
	}
}

func TestNormalizeSunoTaskStatusAcceptsWireValues(t *testing.T) {
	tests := map[string]model.TaskStatus{
		"submitted":  model.TaskStatusSubmitted,
		"queueing":   model.TaskStatusQueued,
		"processing": model.TaskStatusInProgress,
		"success":    model.TaskStatusSuccess,
		"failed":     model.TaskStatusFailure,
	}
	for wire, want := range tests {
		t.Run(wire, func(t *testing.T) {
			if got := normalizeSunoTaskStatus(wire); got != want {
				t.Fatalf("normalizeSunoTaskStatus(%q) = %q, want %q", wire, got, want)
			}
		})
	}
}

func TestHasRecognizedSunoPollingStatusRequiresRequestedTask(t *testing.T) {
	expected := map[string]struct{}{"task-a": {}}
	require.False(t, hasRecognizedSunoPollingStatus(nil, expected))
	require.False(t, hasRecognizedSunoPollingStatus([]dto.SunoDataResponse{{TaskID: "task-other", Status: "success"}}, expected))
	require.False(t, hasRecognizedSunoPollingStatus([]dto.SunoDataResponse{{TaskID: "task-a", Status: "unknown"}}, expected))
	require.True(t, hasRecognizedSunoPollingStatus([]dto.SunoDataResponse{{TaskID: "task-a", Status: "processing"}}, expected))
}

func TestApplySunoTaskResponseDoesNotEraseExistingDataOnSparsePoll(t *testing.T) {
	oldData := json.RawMessage(`{"audio_url":"https://cdn.example/old.mp3"}`)
	task := &model.Task{
		TaskID:     "suno-sparse-data",
		Status:     model.TaskStatusInProgress,
		Progress:   "60%",
		Data:       oldData,
		SubmitTime: 100,
	}

	require.NoError(t, applySunoTaskResponse(context.Background(), task, dto.SunoDataResponse{
		TaskID: "suno-sparse-data",
		Status: "processing",
	}))

	require.Equal(t, oldData, task.Data)
	require.Equal(t, "60%", task.Progress)
}

func TestTaskNeedsUpdateComparesSunoDataByJSONSemantics(t *testing.T) {
	t.Run("different values are not hidden by byte sorting", func(t *testing.T) {
		task := &model.Task{
			Status: model.TaskStatusInProgress,
			Data:   json.RawMessage(`{"a":1,"b":2}`),
		}
		response := dto.SunoDataResponse{
			Data: json.RawMessage(`{"a":2,"b":1}`),
		}
		require.True(t, taskNeedsUpdate(task, response))
	})

	t.Run("object field order is insignificant", func(t *testing.T) {
		task := &model.Task{
			Status: model.TaskStatusInProgress,
			Data:   json.RawMessage(`{"a":1,"b":2}`),
		}
		response := dto.SunoDataResponse{
			Data: json.RawMessage(` { "b": 2, "a": 1 } `),
		}
		require.False(t, taskNeedsUpdate(task, response))
	})
}

func TestApplySunoTaskResponseRefundsOnlyCASWinner(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 701, 701, 701
	const initialQuota, taskQuota = 10000, 3000
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, "suno-cas-key", 5000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, taskQuota, tokenID, BillingSourceWallet, 0)
	task.TaskID = "suno-cas-" + time.Now().Format("150405.000000")
	task.Status = model.TaskStatusInProgress
	require.NoError(t, model.DB.Create(task).Error)
	stale := *task

	response := dto.SunoDataResponse{
		TaskID:     task.TaskID,
		Status:     "failed",
		FailReason: "upstream rejected task",
		Data:       json.RawMessage(`{"error":"rejected"}`),
	}
	require.NoError(t, applySunoTaskResponse(ctx, task, response))
	require.NoError(t, applySunoTaskResponse(ctx, &stale, response))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	require.EqualValues(t, model.TaskStatusFailure, persisted.Status)
	require.Equal(t, "100%", persisted.Progress)
	require.NotZero(t, persisted.FinishTime)
	require.Equal(t, initialQuota+taskQuota, getUserQuota(t, userID))
	require.Equal(t, 5000+taskQuota, getTokenRemainQuota(t, tokenID))
	require.Equal(t, int64(1), countLogs(t))
}

func TestMarkTaskPollingFailureRefundsOnlyCASWinner(t *testing.T) {
	truncate(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 702, 702, 702
	const initialQuota, taskQuota = 10000, 3000
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, "video-cas-key", 5000)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, taskQuota, tokenID, BillingSourceWallet, 0)
	task.TaskID = "video-cas-" + time.Now().Format("150405.000000")
	task.Status = model.TaskStatusInProgress
	require.NoError(t, model.DB.Create(task).Error)
	stale := *task

	require.NoError(t, markTaskPollingFailure(ctx, task, "任务缺少上游任务 ID"))
	require.NoError(t, markTaskPollingFailure(ctx, &stale, "任务缺少上游任务 ID"))

	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	require.EqualValues(t, model.TaskStatusFailure, persisted.Status)
	require.Equal(t, "100%", persisted.Progress)
	require.NotZero(t, persisted.FinishTime)
	require.Equal(t, "任务缺少上游任务 ID", persisted.FailReason)
	require.Equal(t, initialQuota+taskQuota, getUserQuota(t, userID))
	require.Equal(t, 5000+taskQuota, getTokenRemainQuota(t, tokenID))
	require.Equal(t, int64(1), countLogs(t))
}
