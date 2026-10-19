package relay

import (
	"fmt"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// RealtimeMaxMessageBytes 限制实时中继单个 WebSocket 消息的大小。
//
// gorilla 默认不限制读大小：一个持有 token 的客户端发一帧 100MB 文本帧，
// 进程就按帧大小分配内存，并发几条即可打爆堆。16MB 足够容纳实时音频分片与
// 会话配置事件，同时把单连接的可控内存钉死。
const RealtimeMaxMessageBytes int64 = 16 << 20

// applyRealtimeReadLimit 给与客户端、上游两侧的连接设同一上限。
func applyRealtimeReadLimit(conn *websocket.Conn) {
	if conn == nil {
		return
	}
	conn.SetReadLimit(RealtimeMaxMessageBytes)
}

func WssHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	// Realtime has no JSON request body for the normal relay mapping helper to
	// consume. Apply the same request-scoped mapping after channel selection so
	// the handshake URL and all subsequent session events use the selected
	// upstream model instead of the client-facing model.
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	//var requestBody io.Reader
	//firstWssRequest, _ := c.Get("first_wss_request")
	//requestBody = bytes.NewBuffer(firstWssRequest.([]byte))

	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, info, nil)
	if err != nil {
		return types.NewError(err, types.ErrorCodeDoRequestFailed)
	}

	if resp != nil {
		info.TargetWs = resp.(*websocket.Conn)
		applyRealtimeReadLimit(info.TargetWs)
		defer info.TargetWs.Close()
	}

	usage, newAPIError := adaptor.DoResponse(c, nil, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	service.PostWssConsumeQuota(c, info, info.UpstreamModelName, usage.(*dto.RealtimeUsage), "")
	return nil
}
