package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// @openai/codex 的缩写元数据实测约 5.15 MiB（registry 返回 5,397,293 字节），
// 超过旧的 4 MiB 上限：版本刷新每 10 分钟失败一次并写一条
// "metadata exceeds maximum size"，面板里该包的版本选项永远是空。
// 上限必须覆盖真实体积，同时保留一个有意义的封顶防止无界读取。

// buildLargeNpmMetadataPayload 生成带 padding 的合法元数据，体积落在
// (minBytes, npmRegistryMetadataMaxBytes) 区间内。
func buildLargeNpmMetadataPayload(t *testing.T, minBytes int) []byte {
	t.Helper()
	padding := strings.Repeat("x", 1024)
	content := `{"dist-tags":{"latest":"9.9.9999"},"versions":{`
	var entries []string
	for size, i := len(content), 0; size < minBytes; i++ {
		entry := fmt.Sprintf(`"9.9.%d":{"padding":%q}`, i, padding)
		entries = append(entries, entry)
		size += len(entry) + 1 // 每个元素间一个逗号
	}
	payload := []byte(content + strings.Join(entries, ",") + "}}")
	require.Greater(t, len(payload), minBytes, "测试载荷必须超过目标体积")
	return payload
}

func TestFetchNpmCLIVersionOptionsAcceptsMetadataAboveLegacyLimit(t *testing.T) {
	payload := buildLargeNpmMetadataPayload(t, 5<<20)
	require.Greater(t, len(payload), 4<<20, "载荷需超过旧的 4 MiB 上限")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	options, err := fetchNpmCLIVersionOptions(context.Background(), "@openai/codex", server.Client(), server.URL)

	require.NoError(t, err, "5 MiB 级的真实元数据必须能解析，不能因体积被判失败")
	require.NotEmpty(t, options, "大元数据必须仍然产出版本选项")
	require.Equal(t, NpmCLIVersionLatestAlias, options[0].Value)
}

// 上限仍然要对真正的异常体积生效，不能退化成无界读取。
func TestDecodeNpmPackageMetadataRejectsMetadataAboveNewLimit(t *testing.T) {
	oversized := strings.NewReader(strings.Repeat("a", npmRegistryMetadataMaxBytes+1))

	var metadata npmPackageMetadata
	err := decodeNpmPackageMetadata(oversized, &metadata)

	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum size")
}
