package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func pageQueryFor(t *testing.T, target string) *PageInfo {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", target, nil)
	return GetPageQuery(c)
}

// 负 page_size 曾经原样透传：gorm 的 clause.Limit 只在 *limit >= 0 时输出
// LIMIT，负值等于整表返回。管理员列表接口一次 page_size=-1 就是全表扫描。
func TestGetPageQueryClampsNegativePageSize(t *testing.T) {
	for _, target := range []string{
		"/api/user/search?page_size=-1",
		"/api/user/search?page_size=-100000",
		"/api/user/search?ps=-1",
		"/api/user/search?size=-1",
	} {
		pageInfo := pageQueryFor(t, target)
		if pageInfo.PageSize != ItemsPerPage {
			t.Fatalf("%s: negative page size must fall back to the default, got %d", target, pageInfo.PageSize)
		}
	}
}

func TestGetPageQueryKeepsPositivePageSize(t *testing.T) {
	if got := pageQueryFor(t, "/api/x?page_size=37").PageSize; got != 37 {
		t.Fatalf("expected 37, got %d", got)
	}
}

func TestGetPageQueryStillCapsOversizedPageSize(t *testing.T) {
	if got := pageQueryFor(t, "/api/x?page_size=100000").PageSize; got != 100 {
		t.Fatalf("expected the 100 cap, got %d", got)
	}
}

// 起始下标也不能为负：gorm 对负 offset 生成的 SQL 在部分方言下报错。
func TestGetPageQueryClampsNegativeStartIndex(t *testing.T) {
	pageInfo := pageQueryFor(t, "/api/x?p=-5&page_size=10")
	if pageInfo.GetStartIdx() < 0 {
		t.Fatalf("start index must not be negative, got %d (page=%d)", pageInfo.GetStartIdx(), pageInfo.Page)
	}
}
