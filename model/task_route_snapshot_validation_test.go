package model

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"io"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func encryptRawTaskRouteHeaderPolicyForTest(t *testing.T, payload string) string {
	t.Helper()
	block, err := aes.NewCipher(taskRouteHeaderPolicyEncryptionKey())
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)
	sealed := gcm.Seal(nonce, nonce, []byte(payload), []byte(taskRouteHeaderPolicyAAD))
	return taskPrivateKeyCipherPrefix + base64.RawURLEncoding.EncodeToString(sealed)
}

func newCurrentTaskRouteSnapshotForValidationTest(t *testing.T) *TaskRouteSnapshot {
	t.Helper()
	base := relaycommon.ChannelRouteIdentity{
		RequestedModel:         "client-model",
		EffectiveUpstreamModel: "provider-model",
		FinalProtocol:          "openai",
		Operation:              "task.openai.generate",
		UpstreamPath:           "/v1/videos",
		ChannelID:              201,
	}.Normalized()
	poll := base
	poll.Operation = base.Operation + ".poll"
	poll.UpstreamPath = "/v1/videos/status"
	poll.Fingerprint = ""
	poll = poll.Normalized()
	content := base
	content.Operation = base.Operation + ".content"
	content.UpstreamPath = "/v1/videos/content"
	content.Fingerprint = ""
	content = content.Normalized()

	snapshot := &TaskRouteSnapshot{
		SchemaVersion:           TaskRouteSnapshotSchemaVersion,
		Group:                   "default",
		BaseURL:                 "https://submit.example.test",
		RequestedModel:          base.RequestedModel,
		EffectiveUpstreamModel:  base.EffectiveUpstreamModel,
		Protocol:                base.FinalProtocol,
		Operation:               base.Operation,
		PollingOperation:        poll.Operation,
		ContentOperation:        content.Operation,
		SubmitPath:              base.UpstreamPath,
		PollingPath:             poll.UpstreamPath,
		ContentPath:             content.UpstreamPath,
		RouteFingerprint:        base.Fingerprint,
		PollingRouteFingerprint: poll.Fingerprint,
		ContentRouteFingerprint: content.Fingerprint,
		RouteScopeVersion:       "v1:test",
		ChannelID:               201,
		ChannelType:             constant.ChannelTypeOpenAI,
		AdapterPlatform:         "1",
		AdapterIdentity:         "task-adapter/v1/1/1",
		ProviderRuntime:         &TaskRouteProviderSnapshot{},
		ProxyCaptured:           true,
		ProxyCiphertext:         mustEncryptTaskRouteProxyForValidationTest(t, ""),
		ProxyFingerprint:        ComputeTaskRouteProxyFingerprint(""),
	}
	headerCiphertext, err := EncryptTaskRouteHeaderPolicy(nil)
	require.NoError(t, err)
	snapshot.HeaderPolicyCaptured = true
	snapshot.HeaderPolicyCiphertext = headerCiphertext
	snapshot.HeaderPolicyFingerprint = ComputeTaskRouteHeaderPolicyFingerprint(nil)
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	return snapshot
}

func mustEncryptTaskRouteProxyForValidationTest(t *testing.T, proxy string) string {
	t.Helper()
	ciphertext, err := EncryptTaskRouteProxy(proxy)
	require.NoError(t, err)
	return ciphertext
}

func TestTaskPrivateDataScanRejectsCurrentSnapshotWithoutCapturedProxy(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.ProxyCaptured = false
	snapshot.ProxyFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	payload, err := common.Marshal(TaskPrivateData{RouteSnapshot: snapshot})
	require.NoError(t, err)

	var scanned TaskPrivateData
	err = scanned.Scan(payload)
	require.Error(t, err)
	require.ErrorContains(t, err, "proxy identity is incomplete")
}

func TestTaskRouteSnapshotRejectsMissingProxyCiphertext(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.ProxyCiphertext = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	err := snapshot.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "proxy identity is incomplete")
}

func TestTaskRouteSnapshotRejectsTamperedProxyCiphertext(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	tampered, err := EncryptTaskRouteProxy("http://other-proxy.example.test:8080")
	require.NoError(t, err)
	snapshot.ProxyCiphertext = tampered

	err = snapshot.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "proxy fingerprint mismatch")
}

func TestTaskRouteSnapshotRejectsTamperedHeaderPolicyCiphertext(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	tampered, err := EncryptTaskRouteHeaderPolicy(map[string]string{"X-Injected": "replacement"})
	require.NoError(t, err)
	snapshot.HeaderPolicyCiphertext = tampered

	err = snapshot.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "header policy fingerprint mismatch")
}

func TestEncryptTaskRouteHeaderPolicyRejectsCaseInsensitiveDuplicateHeaders(t *testing.T) {
	_, err := EncryptTaskRouteHeaderPolicy(map[string]string{
		"User-Agent": "client-a",
		"user-agent": "client-b",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "duplicate header names")
}

func TestDecryptTaskRouteHeaderPolicyRejectsCaseInsensitiveDuplicateHeaders(t *testing.T) {
	ciphertext := encryptRawTaskRouteHeaderPolicyForTest(t, `{"User-Agent":"client-a","user-agent":"client-b"}`)

	_, err := DecryptTaskRouteHeaderPolicy(ciphertext)
	require.Error(t, err)
	require.ErrorContains(t, err, "duplicate header names")
}

func TestEncryptTaskRouteHeaderPolicyCanonicalizesHeaderNames(t *testing.T) {
	ciphertext, err := EncryptTaskRouteHeaderPolicy(map[string]string{
		" x-request-id ": "request-1",
		"content-type":   "application/json",
	})
	require.NoError(t, err)

	decoded, err := DecryptTaskRouteHeaderPolicy(ciphertext)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"X-Request-Id": "request-1",
		"Content-Type": "application/json",
	}, decoded)
}

func TestTaskRouteSnapshotRejectsFutureSchemaVersion(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.SchemaVersion = TaskRouteSnapshotSchemaVersion + 1

	err := snapshot.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "newer than supported version")
}

func TestTaskRouteSnapshotRejectsMissingHeaderPolicyCapture(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.HeaderPolicyCaptured = false
	snapshot.HeaderPolicyCiphertext = ""
	snapshot.HeaderPolicyFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	err := snapshot.Validate()

	require.Error(t, err)
	require.ErrorContains(t, err, "header policy capture is incomplete")
}

func TestTaskRouteSnapshotSchema5ValidatesCapturedHeaderPolicyWithoutProviderRuntime(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.SchemaVersion = TaskRouteSnapshotHeaderPolicySchemaVersion
	snapshot.ProviderRuntime = nil
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	require.NoError(t, snapshot.Validate())

	snapshot.HeaderPolicyCaptured = false
	snapshot.HeaderPolicyCiphertext = ""
	snapshot.HeaderPolicyFingerprint = ""
	require.ErrorContains(t, snapshot.Validate(), "header policy capture is incomplete")
}

func TestTaskPrivateDataScanRejectsInvalidSchema5HeaderPolicy(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.SchemaVersion = TaskRouteSnapshotHeaderPolicySchemaVersion
	snapshot.ProviderRuntime = nil
	snapshot.HeaderPolicyFingerprint = "tampered"
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	payload, err := common.Marshal(TaskPrivateData{RouteSnapshot: snapshot})
	require.NoError(t, err)

	var scanned TaskPrivateData
	err = scanned.Scan(payload)
	require.Error(t, err)
	require.ErrorContains(t, err, "header policy fingerprint mismatch")
}

func TestTaskRouteSnapshotRejectsAutoGroup(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.Group = "auto"
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()

	err := snapshot.Validate()
	require.Error(t, err)
	require.ErrorContains(t, err, "group must be concrete")
}

func TestTaskPrivateDataScanRejectsFutureSchemaVersion(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.SchemaVersion = TaskRouteSnapshotSchemaVersion + 1
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	payload, err := common.Marshal(TaskPrivateData{RouteSnapshot: snapshot})
	require.NoError(t, err)

	var scanned TaskPrivateData
	err = scanned.Scan(payload)
	require.Error(t, err)
	require.ErrorContains(t, err, "newer than supported version")
}

func TestTaskInsertRejectsInvalidCurrentRouteSnapshot(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Task{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.HeaderPolicyCaptured = false
	snapshot.HeaderPolicyCiphertext = ""
	snapshot.HeaderPolicyFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	task := &Task{
		TaskID:    "invalid-snapshot-insert",
		ChannelId: snapshot.ChannelID,
		PrivateData: TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}

	err := task.Insert()
	require.Error(t, err)
	require.ErrorContains(t, err, "route snapshot validation failed")
	var count int64
	require.NoError(t, db.Model(&Task{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestTaskInsertRejectsRouteSnapshotChannelMismatch(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Task{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	task := &Task{
		TaskID:    "mismatched-snapshot-insert",
		ChannelId: snapshot.ChannelID + 1,
		PrivateData: TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}

	err := task.Insert()
	require.Error(t, err)
	require.ErrorContains(t, err, "task route snapshot channel mismatch")
}

func TestTaskUpdateWithStatusRejectsInvalidCurrentRouteSnapshot(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Task{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.HeaderPolicyCaptured = false
	snapshot.HeaderPolicyCiphertext = ""
	snapshot.HeaderPolicyFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	task := &Task{
		ID:        1,
		TaskID:    "invalid-snapshot-cas-update",
		Status:    TaskStatusInProgress,
		ChannelId: snapshot.ChannelID,
		PrivateData: TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}

	won, err := task.UpdateWithStatus(TaskStatusInProgress)
	require.Error(t, err)
	require.False(t, won)
	// 校验必须在落库前失败：非法快照不得留下任何行。
	var count int64
	require.NoError(t, db.Model(&Task{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestTaskBulkUpdateAllowsOnlyNonLifecycleFields(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Task{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	task := &Task{TaskID: "bulk-safe-task", Progress: "10%", Status: TaskStatusInProgress}
	require.NoError(t, db.Create(task).Error)

	err := TaskBulkUpdate([]string{" bulk-safe-task "}, map[string]any{
		"progress":    "80%",
		"fail_reason": "provider retry",
	})
	require.NoError(t, err)

	var loaded Task
	require.NoError(t, db.First(&loaded, task.ID).Error)
	require.Equal(t, "80%", loaded.Progress)
	require.Equal(t, "provider retry", loaded.FailReason)

	err = TaskBulkUpdateByID([]int64{task.ID}, map[string]any{"status": TaskStatusSuccess})
	require.ErrorContains(t, err, "field \"status\" is not allowed")
	require.NoError(t, db.First(&loaded, task.ID).Error)
	require.NotEqual(t, TaskStatusSuccess, loaded.Status)
}

func TestTaskBulkUpdateValidatesAllRowsBeforeWriting(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Task{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	validTask := &Task{TaskID: "bulk-valid-task", Progress: "10%"}
	require.NoError(t, db.Create(validTask).Error)
	invalidSnapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	invalidSnapshot.HeaderPolicyCaptured = false
	invalidSnapshot.HeaderPolicyCiphertext = ""
	invalidSnapshot.HeaderPolicyFingerprint = ""
	invalidSnapshot.SnapshotFingerprint = invalidSnapshot.ComputeFingerprint()
	invalidTask := &Task{
		TaskID:    "bulk-invalid-task",
		Progress:  "20%",
		ChannelId: invalidSnapshot.ChannelID,
		PrivateData: TaskPrivateData{
			RouteSnapshot: invalidSnapshot,
		},
	}
	require.NoError(t, db.Create(invalidTask).Error)

	err := TaskBulkUpdateByID([]int64{validTask.ID, invalidTask.ID}, map[string]any{"progress": "90%"})
	require.Error(t, err)
	require.ErrorContains(t, err, "load tasks before bulk update")

	var persisted Task
	require.NoError(t, db.First(&persisted, validTask.ID).Error)
	require.Equal(t, "10%", persisted.Progress)
}

func TestInitTaskUsesSelectedChannelMetadataChannelID(t *testing.T) {
	info := &relaycommon.RelayInfo{
		UserId: 42,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 201,
			ApiKey:    "selected-key",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task-forward-channel-id"},
	}
	task := InitTask(constant.TaskPlatform("201"), info)

	require.Equal(t, 201, task.ChannelId)
	require.Equal(t, "selected-key", task.PrivateData.Key)
}

func TestInitTaskSelectedChannelIdentitySurvivesInsertAndReload(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Task{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	info := &relaycommon.RelayInfo{
		UserId: 42,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 201,
			ApiKey:    "selected-key",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task-forward-channel-id-db"},
	}
	task := InitTask(constant.TaskPlatform("201"), info)
	require.NoError(t, task.Insert())

	var reloaded Task
	require.NoError(t, db.Where("task_id = ?", task.TaskID).First(&reloaded).Error)
	require.Equal(t, 201, reloaded.ChannelId)
	require.Equal(t, 42, reloaded.UserId)
	require.Equal(t, "selected-key", reloaded.PrivateData.Key)
}

func TestMidjourneyInsertRejectsInvalidCurrentRouteSnapshot(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Midjourney{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.HeaderPolicyCaptured = false
	snapshot.HeaderPolicyCiphertext = ""
	snapshot.HeaderPolicyFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	midjourney := &Midjourney{
		MjId:      "invalid-snapshot-mj-insert",
		ChannelId: snapshot.ChannelID,
		PrivateData: TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}

	err := midjourney.Insert()
	require.Error(t, err)
	require.ErrorContains(t, err, "route snapshot validation failed")
	var count int64
	require.NoError(t, db.Model(&Midjourney{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestMidjourneyUpdateRejectsInvalidCurrentRouteSnapshot(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Midjourney{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.HeaderPolicyCaptured = false
	snapshot.HeaderPolicyCiphertext = ""
	snapshot.HeaderPolicyFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	midjourney := &Midjourney{
		Id:        1,
		MjId:      "invalid-snapshot-mj-update",
		Status:    "IN_PROGRESS",
		ChannelId: snapshot.ChannelID,
		PrivateData: TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}

	err := midjourney.Update()
	require.Error(t, err)
	require.ErrorContains(t, err, "route snapshot validation failed")
	var count int64
	require.NoError(t, db.Model(&Midjourney{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestMidjourneyUpdateWithStatusRejectsInvalidCurrentRouteSnapshot(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Midjourney{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.HeaderPolicyCaptured = false
	snapshot.HeaderPolicyCiphertext = ""
	snapshot.HeaderPolicyFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	midjourney := &Midjourney{
		Id:        1,
		MjId:      "invalid-snapshot-mj-cas-update",
		Status:    "IN_PROGRESS",
		ChannelId: snapshot.ChannelID,
		PrivateData: TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}

	won, err := midjourney.UpdateWithStatus("IN_PROGRESS")
	require.Error(t, err)
	require.False(t, won)
}

func TestMidjourneyUpdateProgressRejectsInvalidCurrentRouteSnapshot(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Midjourney{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	snapshot.HeaderPolicyCaptured = false
	snapshot.HeaderPolicyCiphertext = ""
	snapshot.HeaderPolicyFingerprint = ""
	snapshot.SnapshotFingerprint = snapshot.ComputeFingerprint()
	task := &Midjourney{
		MjId:      "invalid-snapshot-mj-progress",
		Progress:  "10%",
		ChannelId: snapshot.ChannelID,
		PrivateData: TaskPrivateData{
			RouteSnapshot: snapshot,
		},
	}
	require.NoError(t, db.Create(task).Error)

	err := UpdateProgress(task.Id, "90%")
	require.Error(t, err)
	require.ErrorContains(t, err, "load Midjourney task")

	var persisted struct {
		Progress string
	}
	require.NoError(t, db.Table("midjourneys").Select("progress").Where("id = ?", task.Id).Scan(&persisted).Error)
	require.Equal(t, "10%", persisted.Progress)
}

func TestMidjourneyBulkUpdateAllowsOnlyNonLifecycleFields(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Midjourney{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	task := &Midjourney{MjId: "bulk-safe-mj", Progress: "10%"}
	require.NoError(t, db.Create(task).Error)

	err := MjBulkUpdate([]string{" bulk-safe-mj "}, map[string]any{
		"progress":    "80%",
		"description": "finished",
	})
	require.NoError(t, err)

	var loaded Midjourney
	require.NoError(t, db.First(&loaded, task.Id).Error)
	require.Equal(t, "80%", loaded.Progress)
	require.Equal(t, "finished", loaded.Description)

	err = MjBulkUpdateByTaskIds([]int{task.Id}, map[string]any{"status": "SUCCESS"})
	require.ErrorContains(t, err, "field \"status\" is not allowed")
	require.NoError(t, db.First(&loaded, task.Id).Error)
	require.NotEqual(t, "SUCCESS", loaded.Status)
}

func TestMidjourneyBulkUpdateValidatesAllRowsBeforeWriting(t *testing.T) {
	previousDB, previousLogDB := DB, LOG_DB
	db := openTestDB(t, &Midjourney{})
	DB, LOG_DB = db, db
	t.Cleanup(func() { DB, LOG_DB = previousDB, previousLogDB })

	validTask := &Midjourney{MjId: "bulk-valid-mj", Progress: "10%"}
	require.NoError(t, db.Create(validTask).Error)
	invalidSnapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	invalidSnapshot.HeaderPolicyCaptured = false
	invalidSnapshot.HeaderPolicyCiphertext = ""
	invalidSnapshot.HeaderPolicyFingerprint = ""
	invalidSnapshot.SnapshotFingerprint = invalidSnapshot.ComputeFingerprint()
	invalidTask := &Midjourney{
		MjId:      "bulk-invalid-mj",
		Progress:  "20%",
		ChannelId: invalidSnapshot.ChannelID,
		PrivateData: TaskPrivateData{
			RouteSnapshot: invalidSnapshot,
		},
	}
	require.NoError(t, db.Create(invalidTask).Error)

	err := MjBulkUpdateByTaskIds([]int{validTask.Id, invalidTask.Id}, map[string]any{"progress": "90%"})
	require.Error(t, err)
	require.ErrorContains(t, err, "load Midjourney tasks before bulk update")

	var persisted struct {
		Progress string
	}
	require.NoError(t, db.Table("midjourneys").Select("progress").Where("id = ?", validTask.Id).Scan(&persisted).Error)
	require.Equal(t, "10%", persisted.Progress)
}
