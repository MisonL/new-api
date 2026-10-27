package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 实时中继的两个连接此前既没有读超时也没有心跳。客户端拔网线、上游静默
// 挂死时，两个读协程永远阻塞在 ReadMessage 上，连接与计费会话都不结束。
func TestRealtimeKeepaliveClosesSilentPeer(t *testing.T) {
	prevInterval, prevWait := realtimePingInterval, realtimePongWait
	realtimePingInterval, realtimePongWait = 40*time.Millisecond, 120*time.Millisecond
	t.Cleanup(func() { realtimePingInterval, realtimePongWait = prevInterval, prevWait })

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// 不读取任何数据：ping 永远不会被处理，也就不会有 pong 回来。
		// 这正是"对端已消失但 TCP 连接还在"的形态。
		time.Sleep(2 * time.Second)
		conn.Close()
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	stop := StartRealtimeKeepalive(client, "test-peer")
	defer stop()

	// 不设额外读超时：连接上唯一的 deadline 必须来自心跳。没有心跳时
	// ReadMessage 会一直阻塞（服务端 2 秒后才关连接），本测试据此判定失败。
	readDone := make(chan error, 1)
	go func() {
		_, _, err := client.ReadMessage()
		readDone <- err
	}()

	select {
	case err := <-readDone:
		if err == nil {
			t.Fatalf("静默对端必须被心跳超时断开")
		}
	case <-time.After(800 * time.Millisecond):
		t.Fatalf("静默对端没有被心跳超时断开：ReadMessage 仍在阻塞，连接与计费会话不会结束")
	}
}

// 正常对端会回应 ping，读超时被 pong 持续顺延：空闲但不能被误杀。
func TestRealtimeKeepaliveKeepsRespondingPeer(t *testing.T) {
	prevInterval, prevWait := realtimePingInterval, realtimePongWait
	realtimePingInterval, realtimePongWait = 30*time.Millisecond, 150*time.Millisecond
	t.Cleanup(func() { realtimePingInterval, realtimePongWait = prevInterval, prevWait })

	serverGotMessage := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			select {
			case serverGotMessage <- struct{}{}:
			default:
			}
		}
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	stop := StartRealtimeKeepalive(client, "test-peer")
	defer stop()

	// 客户端同样需要读循环，pong 才会被处理并顺延超时。
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := client.ReadMessage(); err != nil {
				readErr <- err
				return
			}
		}
	}()

	time.Sleep(3 * time.Duration(realtimePongWait))
	select {
	case err := <-readErr:
		t.Fatalf("有回应的对端不应被心跳断开: %v", err)
	default:
	}
}
