package common

import (
	"strconv"
	"strings"
)

const (
	DatabaseTypeMySQL      = "mysql"
	DatabaseTypeSQLite     = "sqlite"
	DatabaseTypePostgreSQL = "postgres"
)

var UsingSQLite = false
var UsingPostgreSQL = false
var LogSqlType = DatabaseTypeSQLite // Default to SQLite for logging SQL queries
var UsingMySQL = false
var UsingClickHouse = false

var SQLitePath = "one-api.db?_busy_timeout=30000"

// DefaultSQLiteBusyTimeout 是 SQLite 连接等待写锁的默认时长（毫秒）。
//
// 网关会在同一进程里并发写同一个库：请求路径扣费、后台批量更新器、看板聚合
// 落盘。没有 busy_timeout 时，后到的写入立即拿到 SQLITE_BUSY 而不是等待。
const DefaultSQLiteBusyTimeout = 30000

// NormalizeSQLitePath 为外部传入的 SQLite DSN 补上默认 busy_timeout。
//
// SQLITE_PATH 会整体覆盖默认 DSN，而桌面端传的是纯文件路径，默认值里的
// _busy_timeout=30000 因此丢失。这里只在调用方没有显式给出该选项时补默认值；
// 内存库不受影响（不存在文件级写锁竞争），显式 DSN 原样保留。
func NormalizeSQLitePath(path string) string {
	if path == "" || isSQLiteMemoryDSN(path) {
		return path
	}
	if sqliteDSNHasOption(path, "_busy_timeout") || sqliteDSNHasOption(path, "_pragma") {
		return path
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + "_busy_timeout=" + strconv.Itoa(DefaultSQLiteBusyTimeout)
}

func isSQLiteMemoryDSN(path string) bool {
	lower := strings.ToLower(path)
	return lower == ":memory:" || strings.Contains(lower, "mode=memory") || strings.Contains(lower, ":memory:")
}

// sqliteDSNHasOption 按查询参数名精确匹配，避免 busy_timeout 出现在文件名里
// 也被当成已配置。
func sqliteDSNHasOption(path string, name string) bool {
	index := strings.Index(path, "?")
	if index < 0 {
		return false
	}
	for _, param := range strings.Split(path[index+1:], "&") {
		key := param
		if eq := strings.Index(param, "="); eq >= 0 {
			key = param[:eq]
		}
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return true
		}
	}
	return false
}
