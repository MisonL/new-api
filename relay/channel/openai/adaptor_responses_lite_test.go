package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesLiteParallelToolCalls(t *testing.T) {
	for _, tc := range []struct{ name, header, value, want string }{
		{"lite_true", "true", "true", "false"},
		{"lite_false", "true", "false", "false"},
		{"lite_omitted", "true", "", "false"},
		{"lite_null", "true", "null", "false"},
		{"ordinary_true", "", "true", "true"},
		{"ordinary_omitted", "", "", ""},
		{"disabled_lite", "false", "true", "true"},
	} {
		for _, opaque := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/plain", true: "/opaque"}[opaque], func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request.Header.Set("X-OpenAI-Internal-Codex-Responses-Lite", tc.header)
				input := `[{"role":"user","content":"hello"}]`
				if opaque {
					input = `[{"type":"reasoning","id":"rs_test","encrypted_content":"opaque"}]`
				}
				info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}}
				converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{Model: "gpt-6-astra", Input: common.RawMessage(input), ParallelToolCalls: common.RawMessage(tc.value)})
				require.NoError(t, err)
				request := converted.(dto.OpenAIResponsesRequest)
				wire, err := common.Marshal(request)
				require.NoError(t, err)
				var fields map[string]common.RawMessage
				require.NoError(t, common.Unmarshal(wire, &fields))
				require.Equal(t, tc.want, string(fields["parallel_tool_calls"]))
			})
		}
	}
}
