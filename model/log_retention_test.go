package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestLogRetentionUnknownFilterDoesNotMatchAllLogs(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create([]Log{
		{Type: LogTypeUnknown, CreatedAt: 100},
		{Type: LogTypeConsume, CreatedAt: 100},
		{Type: LogTypeError, CreatedAt: 100},
	}).Error)

	count, err := CountLogsByFilter(LogFilter{
		LogType:      LogTypeUnknown,
		LogTypeSet:   true,
		EndTimestamp: 200,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)

	deleted, err := DeleteLogsByFilterBatches(context.Background(), LogFilter{
		LogType:      LogTypeUnknown,
		LogTypeSet:   true,
		EndTimestamp: 200,
	}, 10, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var remaining []Log
	require.NoError(t, LOG_DB.Order("type asc").Find(&remaining).Error)
	require.Len(t, remaining, 2)
	require.Equal(t, LogTypeConsume, remaining[0].Type)
	require.Equal(t, LogTypeError, remaining[1].Type)
}

func TestDeleteLogsByFilterBatchesRespectsBatchBudget(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create([]Log{
		{Type: LogTypeConsume, CreatedAt: 100},
		{Type: LogTypeConsume, CreatedAt: 100},
		{Type: LogTypeConsume, CreatedAt: 100},
	}).Error)

	deleted, err := DeleteLogsByFilterBatches(context.Background(), LogFilter{
		LogType:      LogTypeConsume,
		EndTimestamp: 200,
	}, 2, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, deleted)

	count, err := CountLogsByFilter(LogFilter{LogType: LogTypeConsume})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestDeleteLogsByFilterBatchesAllowsUnlimitedBatches(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create([]Log{
		{Type: LogTypeConsume, CreatedAt: 100},
		{Type: LogTypeConsume, CreatedAt: 100},
		{Type: LogTypeConsume, CreatedAt: 100},
	}).Error)

	deleted, err := DeleteLogsByFilterBatches(context.Background(), LogFilter{
		LogType:      LogTypeConsume,
		EndTimestamp: 200,
	}, 2, -1)
	require.NoError(t, err)
	require.EqualValues(t, 3, deleted)

	count, err := CountLogsByFilter(LogFilter{LogType: LogTypeConsume})
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestDeleteLogsByFilterBatchesStopsWhenDeleteMakesNoProgress(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create(&Log{Type: LogTypeConsume, CreatedAt: 100}).Error)
	callbackName := "test:block_log_delete"
	require.NoError(t, LOG_DB.Callback().Delete().Before("gorm:delete").Register(
		callbackName,
		func(tx *gorm.DB) {
			if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Log" {
				tx.Statement.SQL.Reset()
				tx.Statement.Vars = nil
				tx.Statement.AddClause(clause.Where{
					Exprs: []clause.Expression{clause.Expr{SQL: "1 = 0"}},
				})
			}
		},
	))
	t.Cleanup(func() {
		require.NoError(t, LOG_DB.Callback().Delete().Remove(callbackName))
	})

	deleted, err := DeleteLogsByFilterBatches(context.Background(), LogFilter{
		LogType:      LogTypeConsume,
		EndTimestamp: 200,
	}, 1, -1)

	require.Error(t, err)
	require.Contains(t, err.Error(), "made no progress")
	require.Zero(t, deleted)
}

func TestCountLogsWithPayloadAuditFieldsByFilterScopesPayloadKind(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create([]Log{
		{
			Type:      LogTypeConsume,
			CreatedAt: 100,
			Other: common.MapToJsonStr(map[string]any{
				"request_content": "request-body",
			}),
		},
		{
			Type:      LogTypeConsume,
			CreatedAt: 100,
			Other: common.MapToJsonStr(map[string]any{
				"response_content": "response-body",
			}),
		},
		{
			Type:      LogTypeConsume,
			CreatedAt: 300,
			Other: common.MapToJsonStr(map[string]any{
				"request_content": "new-request-body",
			}),
		},
	}).Error)

	requestCount, err := CountLogsWithPayloadAuditFieldsByFilter(LogFilter{EndTimestamp: 200}, true, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, requestCount)

	responseCount, err := CountLogsWithPayloadAuditFieldsByFilter(LogFilter{EndTimestamp: 200}, false, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, responseCount)
}

func TestCountLogsWithPayloadAuditFieldsByFilterRequiresExactPayloadKey(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create([]Log{
		{
			Type:      LogTypeConsume,
			CreatedAt: 100,
			Other: common.MapToJsonStr(map[string]any{
				"not_request_content": "not-a-payload",
			}),
		},
		{
			Type:      LogTypeConsume,
			CreatedAt: 100,
			Other: common.MapToJsonStr(map[string]any{
				"request_content": "payload",
			}),
		},
	}).Error)

	count, err := CountLogsWithPayloadAuditFieldsByFilter(LogFilter{EndTimestamp: 200}, true, false)

	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestClearPayloadAuditFieldsByFilterBatchesRespectsBatchBudget(t *testing.T) {
	truncateTables(t)

	payload := common.MapToJsonStr(map[string]any{
		"request_content": "body",
	})
	require.NoError(t, LOG_DB.Create([]Log{
		{Type: LogTypeConsume, CreatedAt: 100, Other: "{}"},
		{Type: LogTypeConsume, CreatedAt: 100, Other: "{}"},
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
	}).Error)

	updated, err := ClearPayloadAuditFieldsByFilterBatches(
		context.Background(),
		LogFilter{EndTimestamp: 200},
		true,
		false,
		2,
		1,
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated)

	remaining, err := CountLogsWithPayloadAuditFieldsByFilter(LogFilter{EndTimestamp: 200}, true, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, remaining)
}

func TestClearPayloadAuditFieldsByFilterBatchesDefaultsZeroMaxBatchesToOne(t *testing.T) {
	truncateTables(t)

	payload := common.MapToJsonStr(map[string]any{
		"request_content": "body",
	})
	require.NoError(t, LOG_DB.Create([]Log{
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
	}).Error)

	updated, err := ClearPayloadAuditFieldsByFilterBatches(
		context.Background(),
		LogFilter{EndTimestamp: 200},
		true,
		false,
		2,
		0,
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated)

	remaining, err := CountLogsWithPayloadAuditFieldsByFilter(LogFilter{EndTimestamp: 200}, true, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, remaining)
}

func TestClearPayloadAuditFieldsByFilterClearsAllMatchingBatches(t *testing.T) {
	truncateTables(t)

	payload := common.MapToJsonStr(map[string]any{
		"request_content": "body",
	})
	require.NoError(t, LOG_DB.Create([]Log{
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
		{Type: LogTypeConsume, CreatedAt: 100, Other: payload},
	}).Error)

	updated, err := ClearPayloadAuditFieldsByFilter(
		context.Background(),
		LogFilter{EndTimestamp: 200},
		true,
		false,
		2,
	)
	require.NoError(t, err)
	require.EqualValues(t, 3, updated)

	remaining, err := CountLogsWithPayloadAuditFieldsByFilter(LogFilter{EndTimestamp: 200}, true, false)
	require.NoError(t, err)
	require.Zero(t, remaining)
}
