package openai

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// handleEchoUpgradeOnly 升级连接后把服务端连接交给测试：Upgrade 已 hijack
// 连接，处理函数返回不会关闭它。连接只能由接收方读取，测试端不再持有第二个
// 读者。
func handleEchoUpgradeOnly(upgrader websocket.Upgrader, connCh chan<- *websocket.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connCh <- conn
	}
}

// OpenaiRealtimeHandler 返回时两个读协程必须已经退出。此前函数在任一侧
// 关闭/出错后立即结算并返回，另一侧读协程仍阻塞在 ReadMessage 上：
// 上游继续存活时该协程会在结算之后继续读帧、计费（与 PostWssConsumeQuota
// 的终态结算竞争），且协程要等到 90 秒读超时才释放。
func TestOpenaiRealtimeHandlerJoinsReaderGoroutines(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	clientConnCh := make(chan *websocket.Conn, 1)
	clientServer := httptest.NewServer(handleEchoUpgradeOnly(upgrader, clientConnCh))
	defer clientServer.Close()

	targetConnCh := make(chan *websocket.Conn, 1)
	targetServer := httptest.NewServer(handleEchoUpgradeOnly(upgrader, targetConnCh))
	defer targetServer.Close()

	dial := func(server *httptest.Server) *websocket.Conn {
		url := "ws" + strings.TrimPrefix(server.URL, "http")
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		require.NoError(t, err)
		return conn
	}

	// 测试端同时持有客户端与上游两侧的拨号连接，保证两侧服务端连接都保持存活。
	testClientConn := dial(clientServer)
	defer testClientConn.Close()
	testTargetConn := dial(targetServer)
	defer testTargetConn.Close()

	info := &relaycommon.RelayInfo{
		ClientWs: <-clientConnCh,
		TargetWs: <-targetConnCh,
	}

	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())

	handlerReturned := make(chan struct{})
	go func() {
		_, _ = OpenaiRealtimeHandler(ginContext, info)
		close(handlerReturned)
	}()

	// 等两个读协程都进入 OpenaiRealtimeHandler 的闭包，避免读者尚未启动
	// 就断言导致假阳性。
	waitForReaderGoroutines(t)

	// 客户端断开是处理函数退出的正常触发方式；上游侧继续存活。
	require.NoError(t, testClientConn.Close())

	select {
	case <-handlerReturned:
	case <-time.After(10 * time.Second):
		t.Fatal("OpenaiRealtimeHandler did not return after client close")
	}

	// 处理函数返回后不允许任何读协程仍然存活。
	require.Eventually(t, func() bool {
		return countRealtimeReaderGoroutines() == 0
	}, 5*time.Second, 50*time.Millisecond,
		"a realtime reader goroutine outlived the handler")
}

func waitForReaderGoroutines(t *testing.T) {
	require.Eventually(t, func() bool {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		stacks := string(buf[:n])
		return strings.Contains(stacks, "OpenaiRealtimeHandler.func1") &&
			strings.Contains(stacks, "OpenaiRealtimeHandler.func2")
	}, 5*time.Second, 10*time.Millisecond)
}

func countRealtimeReaderGoroutines() int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), "OpenaiRealtimeHandler.func")
}
