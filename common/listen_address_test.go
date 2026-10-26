package common

import "testing"

// 默认必须保持只给端口、绑全部网卡的老行为：容器部署依赖这一点。
func TestResolveListenAddressDefaultsToAllInterfaces(t *testing.T) {
	t.Setenv("HOST", "")
	if got := ResolveListenAddress("3000"); got != ":3000" {
		t.Fatalf("expected :3000, got %q", got)
	}
}

func TestResolveListenAddressHonorsHost(t *testing.T) {
	t.Setenv("HOST", "127.0.0.1")
	if got := ResolveListenAddress("3000"); got != "127.0.0.1:3000" {
		t.Fatalf("expected 127.0.0.1:3000, got %q", got)
	}
}

func TestResolveListenAddressTrimsWhitespace(t *testing.T) {
	t.Setenv("HOST", "  127.0.0.1  ")
	if got := ResolveListenAddress("3000"); got != "127.0.0.1:3000" {
		t.Fatalf("expected trimmed host, got %q", got)
	}
}

// 桌面端用 ::1 之类的 IPv6 地址时，必须补方括号，否则 JoinHostPort 语义
// 会被 net.Listen 拒绝或解析成错误的地址。
func TestResolveListenAddressBracketsIPv6(t *testing.T) {
	t.Setenv("HOST", "::1")
	if got := ResolveListenAddress("3000"); got != "[::1]:3000" {
		t.Fatalf("expected [::1]:3000, got %q", got)
	}
}
