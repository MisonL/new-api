package relay

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	openairelay "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesLiteDisablesRawPassThrough(t *testing.T) {
	raw := []byte(`{"model":"gpt-6-astra","input":"hello","parallel_tool_calls":true}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("X-OpenAI-Internal-Codex-Responses-Lite", "true")
	storage, err := common.CreateBodyStorage(raw)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })
	c.Set(common.KeyBodyStorage, storage)
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, common.Unmarshal(raw, request))
	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeResponses,
		OriginModelName: request.Model,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
		},
	}

	body, _, apiErr := buildOpenAIResponsesRequestBody(c, info, &openairelay.Adaptor{}, request, false)
	require.Nil(t, apiErr)
	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	var fields map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(bodyBytes, &fields))
	require.JSONEq(t, `false`, string(fields["parallel_tool_calls"]))
}
