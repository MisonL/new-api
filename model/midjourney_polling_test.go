package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGetAllUnFinishTasksFiltersTerminalStatusAndLimits(t *testing.T) {
	db := openTestDB(t, &Midjourney{})
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
	})

	tasks := []Midjourney{
		{Id: 1, MjId: "success-progress-stale", Progress: "50%", Status: "SUCCESS"},
		{Id: 2, MjId: "failure-progress-stale", Progress: "50%", Status: "FAILURE"},
		{Id: 3, MjId: "active-older", Progress: "10%", Status: "IN_PROGRESS"},
		{Id: 4, MjId: "active-newer", Progress: "20%", Status: "SUBMITTED"},
	}
	require.NoError(t, db.Create(&tasks).Error)

	result := GetAllUnFinishTasks(1)
	require.Len(t, result, 1)
	require.Equal(t, "active-older", result[0].MjId)
}

func TestGetAllUnFinishTasksKeepsNonTerminalRowsWithCompletedProgress(t *testing.T) {
	db := openTestDB(t, &Midjourney{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	task := &Midjourney{MjId: "completed-progress-without-status", Progress: "100%", Status: ""}
	require.NoError(t, db.Create(task).Error)

	result := GetAllUnFinishTasks(10)
	require.Len(t, result, 1)
	require.Equal(t, task.MjId, result[0].MjId)
}

func TestGetTimedOutUnfinishedMidjourneyTasksUsesSubmitTimeAndTerminalStatus(t *testing.T) {
	db := openTestDB(t, &Midjourney{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	now := time.Now().UnixMilli()
	tasks := []Midjourney{
		{MjId: "stale-active", Progress: "40%", Status: "IN_PROGRESS", SubmitTime: now - int64(2*time.Hour/time.Millisecond)},
		{MjId: "fresh-active", Progress: "40%", Status: "IN_PROGRESS", SubmitTime: now - int64(10*time.Minute/time.Millisecond)},
		{MjId: "stale-success", Progress: "40%", Status: "SUCCESS", SubmitTime: now - int64(2*time.Hour/time.Millisecond)},
		{MjId: "stale-without-submit-time", Progress: "40%", Status: "IN_PROGRESS"},
	}
	require.NoError(t, db.Create(&tasks).Error)

	result := GetTimedOutUnfinishedMidjourneyTasks(now-int64(time.Hour/time.Millisecond), 10)
	require.Len(t, result, 1)
	require.Equal(t, "stale-active", result[0].MjId)
}

func TestMidjourneyPrivateDataPersistsConcreteCredential(t *testing.T) {
	db := openTestDB(t, &Midjourney{})
	previousDB := DB
	DB = db
	t.Cleanup(func() {
		DB = previousDB
	})

	original := &Midjourney{
		MjId:        "mj-private-key",
		Status:      "IN_PROGRESS",
		Progress:    "10%",
		PrivateData: TaskPrivateData{Key: "key-b"},
	}
	require.NoError(t, db.Create(original).Error)

	var loaded Midjourney
	require.NoError(t, db.First(&loaded, original.Id).Error)
	require.Equal(t, "key-b", loaded.PrivateData.Key)
}

func TestTaskPrivateDataDoesNotPersistPlaintextCredential(t *testing.T) {
	db := openTestDB(t, &Midjourney{})
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB })

	original := &Midjourney{
		MjId:        "mj-encrypted-key",
		Status:      "IN_PROGRESS",
		Progress:    "10%",
		PrivateData: TaskPrivateData{Key: "key-b"},
	}
	require.NoError(t, db.Create(original).Error)

	var raw struct {
		PrivateData []byte `gorm:"column:private_data"`
	}
	require.NoError(t, db.Table("midjourneys").Select("private_data").Where("id = ?", original.Id).Scan(&raw).Error)
	require.NotEmpty(t, raw.PrivateData)
	require.NotContains(t, string(raw.PrivateData), "key-b")
	require.Contains(t, string(raw.PrivateData), "key_ciphertext")

	var loaded Midjourney
	require.NoError(t, db.First(&loaded, original.Id).Error)
	require.Equal(t, "key-b", loaded.PrivateData.Key)
}

func TestTaskPrivateDataReadsLegacyPlaintextCredential(t *testing.T) {
	var data TaskPrivateData
	require.NoError(t, data.Scan([]byte(`{"key":"legacy-key"}`)))
	require.Equal(t, "legacy-key", data.Key)
}

func TestTaskPrivateDataScanAcceptsStringAndClearsNull(t *testing.T) {
	data := TaskPrivateData{Key: "stale-key"}
	require.NoError(t, data.Scan(`{"key":"string-key"}`))
	require.Equal(t, "string-key", data.Key)

	require.NoError(t, data.Scan(nil))
	require.Equal(t, TaskPrivateData{}, data)
}

func TestTaskPrivateDataScanRejectsUnsupportedValueType(t *testing.T) {
	var data TaskPrivateData
	require.ErrorContains(t, data.Scan(123), "unsupported task private data value type int")
}

func TestTaskPrivateDataValuePreservesUndecodedProxyCiphertext(t *testing.T) {
	const proxy = "http://proxy.example.test:8080"
	ciphertext, err := EncryptTaskRouteProxy(proxy)
	require.NoError(t, err)
	headerCiphertext, err := EncryptTaskRouteHeaderPolicy(nil)
	require.NoError(t, err)

	snapshot := &TaskRouteSnapshot{
		SchemaVersion:           TaskRouteSnapshotSchemaVersion,
		HeaderPolicyCaptured:    true,
		HeaderPolicyCiphertext:  headerCiphertext,
		HeaderPolicyFingerprint: ComputeTaskRouteHeaderPolicyFingerprint(nil),
		ProxyCaptured:           true,
		ProxyCiphertext:         ciphertext,
		ProxyFingerprint:        ComputeTaskRouteProxyFingerprint(proxy),
		SnapshotFingerprint:     "stale",
	}
	privateData := TaskPrivateData{RouteSnapshot: snapshot}
	value, err := privateData.Value()
	require.NoError(t, err)

	var stored map[string]any
	require.NoError(t, common.Unmarshal(value.([]byte), &stored))
	route, ok := stored["route_snapshot"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, ciphertext, route["proxy_ciphertext"])
	require.Equal(t, ComputeTaskRouteProxyFingerprint(proxy), route["proxy_fingerprint"])
	require.NotEqual(t, "stale", route["snapshot_fingerprint"])
}

func TestTaskPrivateDataValuePreservesExplicitEmptyProxy(t *testing.T) {
	ciphertext, err := EncryptTaskRouteProxy("http://proxy.example.test:8080")
	require.NoError(t, err)
	headerCiphertext, err := EncryptTaskRouteHeaderPolicy(nil)
	require.NoError(t, err)

	snapshot := &TaskRouteSnapshot{
		SchemaVersion:           TaskRouteSnapshotSchemaVersion,
		HeaderPolicyCaptured:    true,
		HeaderPolicyCiphertext:  headerCiphertext,
		HeaderPolicyFingerprint: ComputeTaskRouteHeaderPolicyFingerprint(nil),
		ProxyCaptured:           true,
		ProxyCiphertext:         ciphertext,
		ProxyFingerprint:        ComputeTaskRouteProxyFingerprint(""),
		SnapshotFingerprint:     "stale",
		proxyDecoded:            true,
	}
	value, err := (TaskPrivateData{RouteSnapshot: snapshot}).Value()
	require.NoError(t, err)

	var stored map[string]any
	require.NoError(t, common.Unmarshal(value.([]byte), &stored))
	route, ok := stored["route_snapshot"].(map[string]any)
	require.True(t, ok)
	newCiphertext, ok := route["proxy_ciphertext"].(string)
	require.True(t, ok)
	require.NotEmpty(t, newCiphertext)
	require.NotEqual(t, ciphertext, newCiphertext)
	decoded, err := DecryptTaskRouteProxy(newCiphertext)
	require.NoError(t, err)
	require.Empty(t, decoded)
	require.Equal(t, ComputeTaskRouteProxyFingerprint(""), route["proxy_fingerprint"])
}

func TestTaskPrivateDataValueRoundTripsDecodedEmptyProxy(t *testing.T) {
	snapshot := newCurrentTaskRouteSnapshotForValidationTest(t)
	raw, err := (TaskPrivateData{RouteSnapshot: snapshot}).Value()
	require.NoError(t, err)

	var loaded TaskPrivateData
	require.NoError(t, loaded.Scan(raw))
	require.NotNil(t, loaded.RouteSnapshot)
	require.True(t, loaded.RouteSnapshot.proxyDecoded)
	require.Empty(t, loaded.RouteSnapshot.Proxy)
	require.NotEmpty(t, loaded.RouteSnapshot.ProxyCiphertext)

	updated, err := loaded.Value()
	require.NoError(t, err)
	var reloaded TaskPrivateData
	require.NoError(t, reloaded.Scan(updated))
	require.NotNil(t, reloaded.RouteSnapshot)
	require.NoError(t, reloaded.RouteSnapshot.Validate())
	require.Empty(t, reloaded.RouteSnapshot.Proxy)
}
