package model

import "testing"

func TestRedactUserLogContentKeepsLeadingCompactionMessage(t *testing.T) {
	content := "Responses Compact mode=native setting=auto path=/v1/responses"
	if got := redactUserLogContent(content); got != content {
		t.Fatalf("leading compaction message was unexpectedly removed: %q", got)
	}
}

func TestRedactUserLogContentRemovesTrailingCompactionDetails(t *testing.T) {
	content := "request completed, Responses Compact mode=native setting=auto path=/v1/responses"
	if got := redactUserLogContent(content); got != "request completed" {
		t.Fatalf("unexpected redacted content: %q", got)
	}
}
