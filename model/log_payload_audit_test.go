package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestClearPayloadAuditFieldsByFilterRemovesOnlySelectedPayloadFields(t *testing.T) {
	truncateTables(t)

	requestAndResponse := common.MapToJsonStr(map[string]any{
		"request_content":          `{"input":"hello"}`,
		"request_content_type":     "application/json",
		"request_content_bytes":    17,
		"request_content_truncated": true,
		"response_content":         `{"output":"world"}`,
		"response_content_type":    "application/json",
		"response_content_bytes":   18,
	})
	otherUserPayload := common.MapToJsonStr(map[string]any{
		"request_content": `{"input":"keep"}`,
	})

	require.NoError(t, DB.Create(&Log{
		UserId:    1,
		Username:  "alice",
		Type:      LogTypeConsume,
		CreatedAt: 100,
		Other:     requestAndResponse,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId:    2,
		Username:  "bob",
		Type:      LogTypeConsume,
		CreatedAt: 101,
		Other:     otherUserPayload,
	}).Error)

	updated, err := ClearPayloadAuditFieldsByFilter(
		context.Background(),
		LogFilter{UserId: 1},
		true,
		false,
		20,
	)
	require.NoError(t, err)
	require.EqualValues(t, 1, updated)

	var cleared Log
	require.NoError(t, DB.Where("user_id = ?", 1).First(&cleared).Error)
	clearedMap, err := common.StrToMap(cleared.Other)
	require.NoError(t, err)
	require.NotContains(t, clearedMap, "request_content")
	require.NotContains(t, clearedMap, "request_content_type")
	require.NotContains(t, clearedMap, "request_content_bytes")
	require.NotContains(t, clearedMap, "request_content_truncated")
	require.Equal(t, `{"output":"world"}`, clearedMap["response_content"])
	require.Equal(t, "application/json", clearedMap["response_content_type"])

	var untouched Log
	require.NoError(t, DB.Where("user_id = ?", 2).First(&untouched).Error)
	untouchedMap, err := common.StrToMap(untouched.Other)
	require.NoError(t, err)
	require.Equal(t, `{"input":"keep"}`, untouchedMap["request_content"])
}

func TestDeleteLogsByFilterRespectsChannelFilter(t *testing.T) {
	truncateTables(t)

	require.NoError(t, DB.Create(&Log{
		UserId:    1,
		Username:  "alice",
		Type:      LogTypeConsume,
		ChannelId: 11,
		CreatedAt: 100,
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId:    1,
		Username:  "alice",
		Type:      LogTypeConsume,
		ChannelId: 22,
		CreatedAt: 101,
	}).Error)

	deleted, err := DeleteLogsByFilter(context.Background(), LogFilter{
		UserId:  1,
		Channel: 11,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	var count int64
	require.NoError(t, DB.Model(&Log{}).Where("user_id = ?", 1).Count(&count).Error)
	require.EqualValues(t, 1, count)

	var remaining Log
	require.NoError(t, DB.Where("user_id = ?", 1).First(&remaining).Error)
	require.Equal(t, 22, remaining.ChannelId)
}
