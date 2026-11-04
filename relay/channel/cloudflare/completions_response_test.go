package cloudflare

import (
	"net/http"
	"testing"

	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// /v1/completions 的请求侧转换存在，响应侧却没有任何分支：DoResponse 裸
// return 后 usage 为 nil 接口，compatible_handler 的 usage.(*dto.Usage)
// 直接 panic（500），且 panic 路径不会退预扣费。这里钉住显式拒绝行为。
func TestDoResponseRejectsCompletionsWithExplicitError(t *testing.T) {
	adaptor := &Adaptor{}
	usage, apiErr := adaptor.DoResponse(nil, nil, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeCompletions,
	})
	require.Nil(t, usage)
	require.NotNil(t, apiErr, "必须返回明确错误而不是 nil usage")
	require.Equal(t, http.StatusNotImplemented, apiErr.StatusCode)
}
