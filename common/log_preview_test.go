package common

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLocalLogPreview(t *testing.T) {
	t.Parallel()

	shortText := "upstream error"
	if got := LocalLogPreview(shortText); got != shortText {
		t.Fatalf("expected short text unchanged, got %q", got)
	}

	longText := strings.Repeat("a", localLogPreviewMaxBytes+1)
	got := LocalLogPreview(longText)
	if len(got) <= localLogPreviewMaxBytes {
		t.Fatalf("expected truncated marker to be appended, got length %d", len(got))
	}
	if !strings.HasPrefix(got, strings.Repeat("a", localLogPreviewMaxBytes)) {
		t.Fatalf("expected preview to keep first %d bytes", localLogPreviewMaxBytes)
	}
	if !strings.Contains(got, "[truncated, original size: 4097 bytes]") {
		t.Fatalf("expected truncated marker, got %q", got)
	}

	multibyteText := strings.Repeat("界", localLogPreviewMaxBytes/3) + "界"
	got = LocalLogPreview(multibyteText)
	if !utf8.ValidString(got) {
		t.Fatalf("expected preview to remain valid utf-8")
	}
	if !strings.Contains(got, "[truncated, original size: 4098 bytes]") {
		t.Fatalf("expected multibyte truncated marker, got %q", got)
	}
}
