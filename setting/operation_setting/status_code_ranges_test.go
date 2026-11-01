package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseHTTPStatusCodeRanges_CommaSeparated(t *testing.T) {
	ranges, err := ParseHTTPStatusCodeRanges("401,403,500-599")
	require.NoError(t, err)
	require.Equal(t, []StatusCodeRange{
		{Start: 401, End: 401},
		{Start: 403, End: 403},
		{Start: 500, End: 599},
	}, ranges)
}

func TestParseHTTPStatusCodeRanges_MergeAndNormalize(t *testing.T) {
	ranges, err := ParseHTTPStatusCodeRanges("500-505,504,401,403,402")
	require.NoError(t, err)
	require.Equal(t, []StatusCodeRange{
		{Start: 401, End: 403},
		{Start: 500, End: 505},
	}, ranges)
}

func TestParseHTTPStatusCodeRanges_Invalid(t *testing.T) {
	_, err := ParseHTTPStatusCodeRanges("99,600,foo,500-400,500-")
	require.Error(t, err)
}

func TestParseHTTPStatusCodeRanges_NoComma_IsInvalid(t *testing.T) {
	_, err := ParseHTTPStatusCodeRanges("401 403")
	require.Error(t, err)
}

func TestShouldDisableByStatusCode(t *testing.T) {
	orig := AutomaticDisableStatusCodeRanges()
	t.Cleanup(func() { SetAutomaticDisableStatusCodeRanges(orig) })

	SetAutomaticDisableStatusCodeRanges([]StatusCodeRange{
		{Start: 401, End: 403},
		{Start: 500, End: 599},
	})

	require.True(t, ShouldDisableByStatusCode(401))
	require.True(t, ShouldDisableByStatusCode(403))
	require.False(t, ShouldDisableByStatusCode(404))
	require.True(t, ShouldDisableByStatusCode(500))
	require.False(t, ShouldDisableByStatusCode(200))
}

func TestShouldRetryByStatusCode(t *testing.T) {
	orig := AutomaticRetryStatusCodeRanges()
	t.Cleanup(func() { SetAutomaticRetryStatusCodeRanges(orig) })

	SetAutomaticRetryStatusCodeRanges([]StatusCodeRange{
		{Start: 429, End: 429},
		{Start: 500, End: 599},
	})

	require.True(t, ShouldRetryByStatusCode(429))
	require.True(t, ShouldRetryByStatusCode(500))
	// 规则范围内没有代码级禁区：504/524 落入 500-599 后按配置重试。
	require.True(t, ShouldRetryByStatusCode(504))
	require.True(t, ShouldRetryByStatusCode(524))
	require.False(t, ShouldRetryByStatusCode(400))
	require.False(t, ShouldRetryByStatusCode(200))
}

// 出厂规则覆盖除 2xx 外的全部状态码，任何状态码都不再有代码级重试禁区；
// 是否重试完全由 AutomaticRetryStatusCodes 表达。
func TestShouldRetryByStatusCode_DefaultCoversEveryNonSuccessCode(t *testing.T) {
	for _, code := range []int{100, 199, 300, 399, 400, 401, 402, 408, 429, 500, 502, 504, 524, 599} {
		require.Truef(t, ShouldRetryByStatusCode(code), "状态码 %d 出厂应可重试", code)
	}
	for _, code := range []int{200, 201, 204, 299} {
		require.Falsef(t, ShouldRetryByStatusCode(code), "状态码 %d 出厂不应重试", code)
	}
	require.False(t, ShouldRetryByStatusCode(99))
	require.False(t, ShouldRetryByStatusCode(600))
}

// 迁移用这个字符串与数据库中的历史出厂值比对，格式必须与设置页保存的规范化格式一致。
func TestDefaultAutomaticRetryStatusCodesCanonicalForm(t *testing.T) {
	require.Equal(t, "100-199,300-599", DefaultAutomaticRetryStatusCodes())
}
