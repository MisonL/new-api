package relay

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// upstreamBodyReadPattern 匹配直接整段读取上游响应体的写法。这类读取没有
// 上限：上游返回超大响应时，每个并发请求各持一份完整响应体。仓库已有
// common.ReadAllWithLimit，所有上游响应读取都必须走它。
var upstreamBodyReadPattern = regexp.MustCompile(`io\.ReadAll\((resp|response|res|httpResp|upstreamResp)\.Body\)`)

// taskSubmitBodyBoundPath 是任务提交响应已经在 relay_task.go 里统一定界的
// 路径：每个 task adaptor 读 resp.Body 之前，relay_task 已经装好
// boundedTaskSubmitResponseBody（1 MiB 显式错误，不静默截断），因此那里的
// 直接读取不是无界读取。
const taskSubmitBodyBoundPath = "channel/task/"

func TestRelayHasNoUnboundedUpstreamBodyReads(t *testing.T) {
	var offenders []string

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasPrefix(filepath.ToSlash(path), taskSubmitBodyBoundPath) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(content), "\n") {
			if upstreamBodyReadPattern.MatchString(line) {
				offenders = append(offenders, path+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	require.NoError(t, err)

	require.Empty(t, offenders,
		"upstream response bodies must be read through common.ReadAllWithLimit:\n%s",
		strings.Join(offenders, "\n"))
}
