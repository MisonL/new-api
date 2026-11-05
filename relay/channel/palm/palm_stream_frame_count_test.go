package palm

import (
	"io"
	"net/http"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// palmStreamHandler 自管读取：此前既不维护 ReceivedResponseCount 也不写首帧
// 时间，中断流在消费日志里永远落不到 partial 分类。
func newPalmStreamInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		IsStream:     true,
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "palm-model"},
	}
}

func TestPalmStreamCountsDeliveredFrame(t *testing.T) {
	c, _ := newPalmStreamTestContext(t)
	info := newPalmStreamInfo()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(`{"candidates":[{"content":"hi"}]}`)),
	}

	apiErr, _ := palmStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	// 首帧时间的写入由 SetFirstResponseTimeAt 的 isFirstResponse 门控，该字段
	// 只由生产构造函数置位；包外夹具无法伪造，计数是这里可断言的口径。
	require.Equal(t, 1, info.ReceivedResponseCount, "包装成帧下发后必须计数")
}
