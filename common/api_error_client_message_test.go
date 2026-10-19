package common

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func apiErrorMessage(t *testing.T, err error) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ApiError(ctx, err)
	return recorder.Body.String()
}

// ApiError 把 err.Error() 原样放进响应体。数据库驱动的错误会带出内部路径
// （SQLite 的 data/new-api.db）、表名和 SQL 片段，这些只属于运维侧。
// 判据是错误签名，而不是错误是否来自某个包：业务错误必须原样保留。
func TestApiErrorHidesDatabaseErrorSignatures(t *testing.T) {
	cases := []string{
		"unable to open database file: data/new-api.db",
		"Error 1146 (42S02): Table 'newapi.logs' doesn't exist",
		`ERROR: relation "users" does not exist (SQLSTATE 42P01)`,
		"no such table: options",
		"no such column: users.quota",
		"database is locked (5) (SQLITE_BUSY)",
	}
	for _, raw := range cases {
		body := apiErrorMessage(t, errors.New(raw))
		require.NotContains(t, body, raw, "database internals must not reach the client")
		require.Contains(t, body, "success")
	}
}

func TestApiErrorKeepsBusinessMessages(t *testing.T) {
	for _, msg := range []string{
		"额度不足",
		"该用户尚未绑定 Passkey",
		"无效的令牌",
		"invalid user id",
		"用户不存在",
	} {
		body := apiErrorMessage(t, errors.New(msg))
		require.Contains(t, body, msg, "business message must survive")
	}
}

// gorm 会把驱动错误包一层，errors.Is/As 之外还要看文案链尾部。
func TestApiErrorHidesWrappedDatabaseErrors(t *testing.T) {
	wrapped := fmt.Errorf("query failed: %w", errors.New("SQLSTATE 42P01"))
	body := apiErrorMessage(t, wrapped)
	require.NotContains(t, body, "SQLSTATE")
	require.Contains(t, strings.ReplaceAll(body, `\"`, `"`), "服务暂时不可用")
}

// 兜底必须是固定文案，不能把原始错误再拼回去。
func TestApiErrorFallbackMessageIsStable(t *testing.T) {
	body := apiErrorMessage(t, errors.New("no such table: options"))
	require.Contains(t, strings.ReplaceAll(body, `\"`, `"`), "服务暂时不可用")
}
