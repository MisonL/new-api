package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProcessChannelErrorRecordsRedactedResponsesRequestShape(t *testing.T) {
	db := setupChannelControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	previousErrorLogEnabled := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() {
		constant.ErrorLogEnabled = previousErrorLogEnabled
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	ctx.Set("id", 1)
	ctx.Set("token_name", "test-token")
	ctx.Set("original_model", "gpt-5.6-sol")
	ctx.Set("token_id", 5)
	ctx.Set("group", "default")
	ctx.Set("channel_id", 117)
	ctx.Set("channel_name", "test-channel")
	ctx.Set("channel_type", constant.ChannelTypeOpenAI)
	ctx.Set("username", "test-user")
	common.SetContextKey(ctx, constant.ContextKeyRequestStartTime, time.Now())
	common.SetContextKey(ctx, constant.ContextKeyRequestBodySize, int64(512))
	common.SetContextKey(ctx, constant.ContextKeyUserSetting, dto.UserSetting{RecordIpLog: false})

	relayInfo := &relaycommon.RelayInfo{
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.6-sol",
			Input: common.RawMessage(`[
				{"type":"additional_tools","tools":[{"type":"function","name":"secret_tool","parameters":{"type":"object"}}]},
				{"type":"message","role":"user","content":"secret prompt"}
			]`),
			ToolChoice: common.RawMessage(`{"type":"function","name":"secret_tool"}`),
		},
	}
	processChannelError(
		ctx,
		relayInfo,
		types.ChannelError{ChannelId: 117, ChannelType: constant.ChannelTypeOpenAI},
		types.NewErrorWithStatusCode(errors.New("upstream failed"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway),
	)

	var log model.Log
	require.NoError(t, db.Order("id DESC").First(&log).Error)
	var other map[string]interface{}
	require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
	shape, ok := other["responses_request_shape"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(512), shape["body_bytes"])
	require.Equal(t, true, shape["has_function_tools"])
	require.Equal(t, true, shape["has_additional_tools"])
	require.NotContains(t, log.Other, "secret prompt")
	require.NotContains(t, log.Other, "secret_tool")
}
