package channel

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// DEBUG 日志中的上游 URL 不能带凭据：Vertex API-key 模式把渠道密钥放在 query
// (?key=AIza...)。这里直接约束源码，任何回归到裸打印 fullRequestURL 的写法都会失败。
func TestDebugUpstreamURLPrintIsMasked(t *testing.T) {
	src, err := os.ReadFile("api_request.go")
	if err != nil {
		t.Fatalf("read api_request.go: %v", err)
	}
	rawPrint := regexp.MustCompile(`println\("fullRequestURL:",\s*fullRequestURL\)`)
	if rawPrint.Match(src) {
		t.Fatalf("debug logging must print common2.MaskURLCredentials(fullRequestURL), not the raw URL")
	}
	if got := strings.Count(string(src), `println("fullRequestURL:", common2.MaskURLCredentials(fullRequestURL))`); got != 2 {
		t.Fatalf("expected both DoApiRequest and DoFormRequest to mask the debug URL, found %d", got)
	}
}
