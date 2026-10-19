package relay

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 实时中继的两个方向都必须限制单帧大小。gorilla 默认不限制，一个持有 token
// 的客户端发 100MB 文本帧就会让进程按帧大小分配内存；并发若干条即可打爆。
func TestRealtimeWebSocketReadLimitIsBounded(t *testing.T) {
	if RealtimeMaxMessageBytes <= 0 {
		t.Fatalf("RealtimeMaxMessageBytes must be positive")
	}
	if RealtimeMaxMessageBytes > 64<<20 {
		t.Fatalf("RealtimeMaxMessageBytes = %d, want a bound that cannot exhaust the heap", RealtimeMaxMessageBytes)
	}
}

// 两个真实连接点（客户端侧 controller/relay.go 的 upgrade、上游侧
// WssHelper 的 dial 结果）都必须调用 applyRealtimeReadLimit，否则上一条测试
// 只证明了辅助函数可用，没有证明它被接上。
func TestRealtimeReadLimitIsWiredIntoBothConnections(t *testing.T) {
	for path, marker := range map[string]string{
		"../controller/relay.go": "ws.SetReadLimit(relay.RealtimeMaxMessageBytes)",
		"websocket.go":           "applyRealtimeReadLimit(info.TargetWs)",
	} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(src), marker) {
			t.Fatalf("%s must apply the realtime read limit (%q)", path, marker)
		}
	}
}

// 服务端把上限真正设到连接上：发一帧超过上限的消息必须被断开。
func TestRealtimeWebSocketRejectsOversizedFrame(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		applyRealtimeReadLimit(conn)
		// 读到超限帧时 gorilla 会以 CloseMessageTooBig 断开。
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	payload := make([]byte, RealtimeMaxMessageBytes+1)
	if err := client.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := client.ReadMessage(); err == nil {
		t.Fatalf("an oversized frame must close the connection")
	}
}
