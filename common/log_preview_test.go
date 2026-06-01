package common

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLocalLogPreview(t *testing.T) {
	shortText := "upstream error"
	if got := LocalLogPreview(shortText); got != shortText {
		t.Fatalf("expected short text unchanged, got %q", got)
	}

	longText := strings.Repeat("a", LocalLogContentLimit+1)
	got := LocalLogPreview(longText)
	if len(got) <= LocalLogContentLimit {
		t.Fatalf("expected truncated marker to be appended, got length %d", len(got))
	}
	if !strings.HasPrefix(got, strings.Repeat("a", LocalLogContentLimit)) {
		t.Fatalf("expected preview to keep first %d bytes", LocalLogContentLimit)
	}
	if !strings.Contains(got, "[truncated, original_length=2049, limit=2048]") {
		t.Fatalf("expected truncated marker, got %q", got)
	}

	multibyteText := strings.Repeat("界", LocalLogContentLimit/3) + "界"
	got = LocalLogPreview(multibyteText)
	if !utf8.ValidString(got) {
		t.Fatalf("expected preview to remain valid utf-8")
	}
	if !strings.Contains(got, "[truncated, original_length=2049, limit=2048]") {
		t.Fatalf("expected multibyte truncated marker, got %q", got)
	}
}

func TestLocalLogPreviewKeepsFullContentWhenDebugEnabled(t *testing.T) {
	t.Parallel()

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
