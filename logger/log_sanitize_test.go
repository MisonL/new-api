package logger

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Request-controlled values (model names, upstream error bodies) reach
// logHelper verbatim. An embedded newline would produce a line that is
// indistinguishable from a real entry, so the writer must neutralize it.
func TestLogHelperNeutralizesNewline(t *testing.T) {
	previousWriter := gin.DefaultWriter
	previousErrorWriter := gin.DefaultErrorWriter
	defer func() {
		gin.DefaultWriter = previousWriter
		gin.DefaultErrorWriter = previousErrorWriter
	}()

	var infoBuf bytes.Buffer
	var errorBuf bytes.Buffer
	gin.DefaultWriter = &infoBuf
	gin.DefaultErrorWriter = &errorBuf

	LogInfo(context.Background(), "model gpt-4\n[SYS] 2026/09/22 - 10:00:00 | forged entry")
	LogError(context.Background(), "upstream error\r\n[ERROR] forged entry")

	for name, buf := range map[string]*bytes.Buffer{"info": &infoBuf, "error": &errorBuf} {
		got := buf.String()
		if got == "" {
			t.Fatalf("%s writer received nothing", name)
		}
		if strings.Count(got, "\n") != 1 {
			t.Fatalf("%s writer emitted %d lines, want 1: %q", name, strings.Count(got, "\n"), got)
		}
	}
}
