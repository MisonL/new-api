package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Go 的 mime/multipart 会把 RFC 2231 filename* 的 %0D%0A 解码成真实 CRLF，
// 直接拼进 Content-Disposition 即可在发往上游的 multipart 中注入额外 part 头。
func TestSanitizeMultipartFilenameStripsCRLF(t *testing.T) {
	require.Equal(t, "aX-Injected: yes.png",
		SanitizeMultipartFilename("a\r\nX-Injected: yes.png"))
	require.Equal(t, "plain.png", SanitizeMultipartFilename("plain.png"))
}

func TestSanitizeMultipartFilenameStripsControlChars(t *testing.T) {
	require.Equal(t, "ab", SanitizeMultipartFilename("a\x00\x07b"))
	require.Equal(t, "file", SanitizeMultipartFilename("\r\n\x00"))
	require.Equal(t, "file", SanitizeMultipartFilename(""))
}

func TestSanitizeMultipartFieldNameStripsCRLF(t *testing.T) {
	require.Equal(t, "image", SanitizeMultipartFieldName("image"))
	require.Equal(t, "aX-Injected: yes", SanitizeMultipartFieldName("a\r\nX-Injected: yes"))
	require.Equal(t, "field", SanitizeMultipartFieldName("\r\n"))
}
