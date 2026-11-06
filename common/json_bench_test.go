package common_test

import (
	jsonv1 "encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// 这些基准覆盖网关每请求都会经过的 JSON 路径：请求体解码、流式 usage 帧
// 解析、大响应解码、map 透传解码。V1 是旧引擎基线，V2 走 common.Unmarshal
// （encoding/json/v2）。数字随机器变化，只用于同机对比回归。

const benchChatRequestJSON = `{
  "model": "gpt-5",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant."},
    {"role": "user", "content": "Summarize the following article in five bullet points."}
  ],
  "stream": true,
  "stream_options": {"include_usage": true},
  "max_tokens": 2048,
  "temperature": 0.7,
  "top_p": 1,
  "stop": ["\n\n"],
  "metadata": {"trace_id": "abc-123"},
  "tools": [
    {"type": "function", "function": {"name": "lookup", "description": "Look up a record", "parameters": {"type": "object", "properties": {"id": {"type": "string"}}}}}
  ]
}`

var benchUsageFrameJSON = []byte(`{"id":"chatcmpl-abc","object":"chat.completion.chunk","created":1750000000,"model":"gpt-5","choices":[],"usage":{"prompt_tokens":1234,"completion_tokens":567,"total_tokens":1801,"prompt_tokens_details":{"cached_tokens":800}}}`)

var benchLargeResponseJSON = func() []byte {
	content := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 800)
	return []byte(`{"id":"chatcmpl-large","object":"chat.completion","created":1750000000,"model":"gpt-5",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"` + content + `"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":1024,"completion_tokens":2048,"total_tokens":3072}}`)
}()

func BenchmarkUnmarshalChatRequestV1(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchChatRequestJSON)))
	for i := 0; i < b.N; i++ {
		var req dto.GeneralOpenAIRequest
		if err := jsonv1.Unmarshal([]byte(benchChatRequestJSON), &req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalChatRequestV2(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchChatRequestJSON)))
	for i := 0; i < b.N; i++ {
		var req dto.GeneralOpenAIRequest
		if err := common.Unmarshal([]byte(benchChatRequestJSON), &req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalUsageFrameV1(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchUsageFrameJSON)))
	for i := 0; i < b.N; i++ {
		var resp struct {
			Usage dto.Usage `json:"usage"`
		}
		if err := jsonv1.Unmarshal(benchUsageFrameJSON, &resp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalUsageFrameV2(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchUsageFrameJSON)))
	for i := 0; i < b.N; i++ {
		var resp struct {
			Usage dto.Usage `json:"usage"`
		}
		if err := common.Unmarshal(benchUsageFrameJSON, &resp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalLargeResponseV1(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchLargeResponseJSON)))
	for i := 0; i < b.N; i++ {
		var resp dto.TextResponse
		if err := jsonv1.Unmarshal(benchLargeResponseJSON, &resp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalLargeResponseV2(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchLargeResponseJSON)))
	for i := 0; i < b.N; i++ {
		var resp dto.TextResponse
		if err := common.Unmarshal(benchLargeResponseJSON, &resp); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalMapLargeV1(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchLargeResponseJSON)))
	for i := 0; i < b.N; i++ {
		var value map[string]any
		if err := jsonv1.Unmarshal(benchLargeResponseJSON, &value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalMapLargeV2(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchLargeResponseJSON)))
	for i := 0; i < b.N; i++ {
		var value map[string]any
		if err := common.Unmarshal(benchLargeResponseJSON, &value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalLargeResponse(b *testing.B) {
	var resp dto.TextResponse
	if err := common.Unmarshal(benchLargeResponseJSON, &resp); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := common.Marshal(&resp); err != nil {
			b.Fatal(err)
		}
	}
}
