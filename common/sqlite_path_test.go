package common

import "testing"

// SQLITE_PATH 直接覆盖整个 DSN。默认值 one-api.db?_busy_timeout=30000 带着
// busy_timeout，而桌面端传入的是纯文件路径（<data>/new-api.db），于是桌面
// 部署的 SQLite 没有 busy 等待：后台批量更新、看板落盘与请求路径并发写同一
// 文件时，直接返回 SQLITE_BUSY 而不是等待重试。归一化必须在保留调用方显式
// 选项的前提下补上默认值。
func TestNormalizeSQLitePathAddsDefaultBusyTimeout(t *testing.T) {
	got := NormalizeSQLitePath("/Users/x/Library/Application Support/new-api/data/new-api.db")
	want := "/Users/x/Library/Application Support/new-api/data/new-api.db?_busy_timeout=30000"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeSQLitePathKeepsExplicitOptions(t *testing.T) {
	in := "file:one-api.db?_busy_timeout=5000&cache=shared"
	if got := NormalizeSQLitePath(in); got != in {
		t.Fatalf("explicit DSN must be preserved, got %q", got)
	}

	in = "one-api.db?mode=rwc"
	got := NormalizeSQLitePath(in)
	want := "one-api.db?mode=rwc&_busy_timeout=30000"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeSQLitePathLeavesMemoryDSNAlone(t *testing.T) {
	for _, in := range []string{
		":memory:",
		"file::memory:?cache=shared",
		"file:test_init?mode=memory&cache=shared",
	} {
		if got := NormalizeSQLitePath(in); got != in {
			t.Fatalf("memory DSN %q must not be rewritten, got %q", in, got)
		}
	}
}
