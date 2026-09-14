package vidu

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyForReferenceActionAlwaysUsesViduQ2(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: "reference prompt",
		Images: []string{"image-a", "image-b", "image-c"},
		Metadata: map[string]interface{}{
			"model":      "attacker-model",
			"model_name": "attacker-model-name",
		},
	})
	info := &relaycommon.RelayInfo{
		OriginModelName: "vidu2.0",
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "vidu2.0",
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionReferenceGenerate},
	}

	body, err := (&TaskAdaptor{}).BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &payload))
	require.Equal(t, "viduq2", payload["model"])
	require.Equal(t, "viduq2", info.ChannelMeta.UpstreamModelName)
}
