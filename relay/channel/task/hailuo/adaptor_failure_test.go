package hailuo

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// BaseResp.StatusCode 非成功表示这次查询本身失败（任务不存在、鉴权失败、
// 上游错误）。旧实现先把 Status 置为 FAILURE，随后又被状态字符串的 switch
// 覆盖：状态串为空或不可识别时落到 default 分支改回 IN_PROGRESS，任务于是
// 永远停在"进行中"，只能等超时清扫兜底，期间每轮都在重复请求上游。
func TestParseTaskResultKeepsFailureOnBaseRespError(t *testing.T) {
	adaptor := &TaskAdaptor{}

	cases := map[string]string{
		"empty status":         `{"task_id":"t1","status":"","base_resp":{"status_code":1004,"status_msg":"invalid task"}}`,
		"processing status":    `{"task_id":"t1","status":"Processing","base_resp":{"status_code":1004,"status_msg":"invalid task"}}`,
		"unrecognized status":  `{"task_id":"t1","status":"WeirdState","base_resp":{"status_code":1004,"status_msg":"invalid task"}}`,
		"success status field": `{"task_id":"t1","status":"Success","file_id":"f1","base_resp":{"status_code":1004,"status_msg":"invalid task"}}`,
	}

	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := adaptor.ParseTaskResultWithContext(context.Background(), "http://127.0.0.1:1", "key", []byte(body), "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, model.TaskStatusFailure, result.Status,
				"base_resp 失败必须保持 FAILURE，不能被状态字符串 switch 覆盖回进行中")
			require.Equal(t, "100%", result.Progress)
			require.NotEmpty(t, result.Reason)
		})
	}
}
