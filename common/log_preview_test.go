package common

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestLocalLogPreview(t *testing.T) {
	shortText := "upstream error"
	if got := LocalLogPreview(shortText); got != shortText {
		t.Fatalf("expected short text unchanged, got %q", got)
	}

	longText := strings.Repeat("a", LocalLogContentLimit+1)
	got := LocalLogPreview(longText)
	longTextMarker := fmt.Sprintf(
		"[truncated, original_length=%d, limit=%d]",
		len(longText),
		LocalLogContentLimit,
	)
	if len(got) <= LocalLogContentLimit {
		t.Fatalf("expected truncated marker to be appended, got length %d", len(got))
	}
	if !strings.HasPrefix(got, strings.Repeat("a", LocalLogContentLimit)) {
		t.Fatalf("expected preview to keep first %d bytes", LocalLogContentLimit)
	}
	if !strings.Contains(got, longTextMarker) {
		t.Fatalf("expected truncated marker, got %q", got)
	}

	multibyteText := strings.Repeat("界", LocalLogContentLimit/3) + "界"
	got = LocalLogPreview(multibyteText)
	multibyteMarker := fmt.Sprintf(
		"[truncated, original_length=%d, limit=%d]",
		len(multibyteText),
		LocalLogContentLimit,
	)
	if !utf8.ValidString(got) {
		t.Fatalf("expected preview to remain valid utf-8")
	}
	if !strings.Contains(got, multibyteMarker) {
		t.Fatalf("expected multibyte truncated marker, got %q", got)
	}
}

func TestLocalLogPreviewKeepsFullContentWhenDebugEnabled(t *testing.T) {
	oldDebug := DebugEnabled
	DebugEnabled = true
	t.Cleanup(func() {
		DebugEnabled = oldDebug
	})

	longText := strings.Repeat("a", LocalLogContentLimit+1)
	if got := LocalLogPreview(longText); got != longText {
		t.Fatalf("expected debug mode to keep full content")
	}
}

func TestLocalLogPreviewTruncatesInvalidUTF8ByByte(t *testing.T) {
	invalid := strings.Repeat("a", LocalLogContentLimit-1) + string([]byte{0xff}) + "suffix"
	expectedMarker := fmt.Sprintf(
		"[truncated, contains invalid UTF-8, original_length=%d, limit=%d]",
		len(invalid),
		LocalLogContentLimit,
	)

	got := LocalLogPreview(invalid)
	if !utf8.ValidString(got) {
		t.Fatalf("expected invalid utf-8 preview output to be valid utf-8")
	}
	if !strings.HasPrefix(got, strings.Repeat("a", LocalLogContentLimit-1)+"?") {
		t.Fatalf("expected invalid utf-8 preview to replace invalid bytes, got %q", got)
	}
	if !strings.Contains(got, expectedMarker) {
		t.Fatalf("expected invalid utf-8 truncated marker, got %q", got)
	}
}

// 适配器把上游错误体拼进用户可见的错误消息时使用 LocalUpstreamErrorPreview：
// 必须脱敏（与日志同一套规则）并有固定长度上限，不能把 8MB 的错误页原样
// 回显给调用方。DEBUG 也不能放开这两个限制。
func TestLocalUpstreamErrorPreviewMasksAndBounds(t *testing.T) {
	long := strings.Repeat("x", LocalUpstreamErrorMessageLimit+500)
	got := LocalUpstreamErrorPreview("upstream 500: " + long)
	require.LessOrEqual(t, len(got), LocalUpstreamErrorMessageLimit+boundedPreviewMarkerReserve)
	require.Contains(t, got, "[truncated")

	secret := LocalUpstreamErrorPreview(`{"client_secret":"cs_live_abc123","detail":"boom"}`)
	require.NotContains(t, secret, "cs_live_abc123")
	require.Contains(t, secret, "boom")
}

func TestLocalUpstreamErrorPreviewKeepsShortContent(t *testing.T) {
	require.Equal(t, "invalid model", LocalUpstreamErrorPreview(" invalid model "))
}
