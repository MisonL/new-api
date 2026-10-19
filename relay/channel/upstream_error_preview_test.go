package channel

import (
	"os"
	"regexp"
	"testing"
)

// 上游错误体拼进用户可见错误消息时必须经过 common.LocalUpstreamErrorPreview
// （脱敏 + 长度上限），否则 8MB 的错误页会整段回显给调用方。
func TestAdaptersUseBoundedUpstreamErrorPreview(t *testing.T) {
	files := []string{
		"ollama/stream.go",
		"ollama/relay-ollama.go",
		"dify/relay-dify.go",
		"replicate/adaptor.go",
		"gemini/relay-gemini.go",
	}
	safePreviewCall := regexp.MustCompile(`common\.LocalUpstreamErrorPreview\(string\((body|responseBody|respBody)\)\)`)
	rawBody := regexp.MustCompile(`fmt\.Errorf\([^\n]*string\((body|responseBody|respBody)\)`)
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		// 先摘掉已经包好的调用，再找仍然裸拼上游响应体的 Errorf。
		scrubbed := safePreviewCall.ReplaceAllString(string(src), "")
		if rawBody.MatchString(scrubbed) {
			t.Fatalf("%s still interpolates a raw upstream body into an error message", file)
		}
	}
}
