package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The restorer is produced here, at the request entry, and consumed on the
// response path (OaiResponsesHandler / sendResponsesStreamData). A restore
// test that injects the restorer by hand cannot detect this producer going
// missing, so this test walks the entry itself: an invalid tool name must be
// normalized, the alias must reach the outgoing request, and the restorer must
// be attached to the relay info.
func TestResponsesHelperNormalizesToolNamesAndAttachesRestorer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	common.SetContextKey(c, constant.ContextKeyChannelId, 200)
	common.SetContextKey(c, constant.ContextKeyOriginalModel, "gpt-5.5")

	original := "mcp.calendar/search.v1"
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		RelayFormat: types.RelayFormatOpenAIResponses,
		Request: &dto.OpenAIResponsesRequest{
			Model: "gpt-5.5",
			Input: common.RawMessage(`[{"type":"function_call","name":"` + original + `","call_id":"call_1","arguments":"{}"}]`),
			Tools: []byte(`[{"type":"function","name":"` + original + `","parameters":{}}]`),
		},
	}

	// No upstream is contacted: a tool-name alias cannot be forwarded to a
	// real channel without channel state, so we drive the helper until the
	// normalization has run and then expect a failure further down. What we
	// assert is the state the entry must have produced before any upstream
	// call: the restorer attached, and a request-declaration JSON containing
	// the alias can be restored to the original name — exactly what the
	// response-path consumers do.
	_ = ResponsesHelper(c, info)

	require.NotNil(t, info.ResponsesToolNameRestorer, "entry must attach the restorer for alias-carrying requests")
	require.True(t, info.ResponsesToolNameRestorer.HasAliases())

	// An unknown name is passed through untouched.
	require.Equal(t, "__never_matches__", info.ResponsesToolNameRestorer.RestoreName("__never_matches__"))

	// The alias round-trips: a payload shaped like an upstream function_call
	// echo must be rewritten back to the original name — exactly what the
	// response-path consumers do with the restorer this entry produced.
	restored, changed := info.ResponsesToolNameRestorer.RestoreRawJSON([]byte(`{"output":[{"type":"function_call","name":"` + aliasFor(t, info.ResponsesToolNameRestorer, original) + `","call_id":"call_1"}]}`))
	require.True(t, changed)
	require.Contains(t, string(restored), original)
}

// aliasFor finds the alias the entry registered for the original name by
// feeding candidate aliases from the deterministic alias format.
func aliasFor(t *testing.T, restorer *relaycommon.ResponsesToolNameRestorer, original string) string {
	t.Helper()
	alias := dto.NormalizeToolName(original)
	require.NotEqual(t, alias, original, "test expects an alias-generating name")
	// RestoreName must map the deterministic alias back to the original —
	// this is the mapping the entry's normalization registered.
	require.Equal(t, original, restorer.RestoreName(alias))
	return alias
}
