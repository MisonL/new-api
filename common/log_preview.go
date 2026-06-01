package common

import (
	"fmt"
	"unicode/utf8"
)

const localLogPreviewMaxBytes = 4 * 1024

func LocalLogPreview(text string) string {
	if len(text) <= localLogPreviewMaxBytes {
		return text
	}
	end := localLogPreviewMaxBytes
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	if end == 0 {
		end = localLogPreviewMaxBytes
	}
	return text[:end] + fmt.Sprintf("\n\n[truncated, original size: %d bytes]", len(text))
}
