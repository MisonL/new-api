package service

// convert_golden_test.go pins the byte-level output of the local protocol
// conversion routes, so that replacing them with relaykit in stage 4 has a
// baseline to diff against. The upstream relaykit snapshots
// (relaykit/relayconvert/testdata/golden, 24 files) record relaykit's own
// behaviour and therefore cannot detect a local regression.
//
// Run with -update to regenerate testdata/golden:
//
//	go test ./service -run TestGolden -update
//
// Volatile values (UUID-shaped ids, unix timestamps) are normalized before
// comparison so the snapshots are deterministic.

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "update golden files")

const goldenDir = "testdata/golden"

var (
	goldenHex32Re     = regexp.MustCompile(`[0-9a-f]{32}`)
	goldenTimestampRe = regexp.MustCompile(`("created(_at)?"\s*:\s*)\d{9,}`)
)

func normalizeGolden(data []byte) []byte {
	data = goldenHex32Re.ReplaceAll(data, []byte("<uuid>"))
	data = goldenTimestampRe.ReplaceAll(data, []byte(`${1}0`))
	return data
}

func marshalGolden(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	return append(normalizeGolden(data), '\n')
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(goldenDir, name+".golden.json")
	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "golden file missing, run: go test ./service -run TestGolden -update")
	require.Equal(t, string(want), string(got), "conversion output drifted from golden snapshot %s", path)
}

func checkEventsGolden(t *testing.T, name string, events []any) {
	t.Helper()
	got := marshalGolden(t, map[string]any{"events": events})
	path := filepath.Join(goldenDir, name+".golden.json")
	if *updateGolden {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "golden file missing, run: go test ./service -run TestGolden -update")
	var snapshot map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(want, &snapshot))
	eventsRaw, ok := snapshot["events"]
	require.True(t, ok, "stream golden snapshot %s has no events", path)
	require.Equal(t, string(marshalGolden(t, map[string]json.RawMessage{"events": eventsRaw})), string(got),
		"conversion events drifted from golden snapshot %s", path)
}

// goldenRelayInfo mirrors the host defaults the conversion functions read
// (setting/model_setting): Claude thinking adapter enabled with 0.8 budget,
// Gemini at 0.6. The upstream snapshots use 0.6 for both, so this baseline is
// deliberately not copied from there.
func goldenRelayInfo(originModel string, channelType int, upstreamModel string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: originModel,
		RelayFormat:     types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       channelType,
			UpstreamModelName: upstreamModel,
		},
		StartTime:             time.Unix(0, 0),
		ShouldIncludeUsage:    true,
		UserSetting:           dto.UserSetting{},
		IsStream:              false,
		DisablePing:           true,
		ReceivedResponseCount: 0,
		// The streaming converters read this state machine; production sets it
		// in GenRelayInfoClaude (relay/common/relay_info.go:539).
		ClaudeConvertInfo: &relaycommon.ClaudeConvertInfo{
			LastMessagesType: relaycommon.LastMessageTypeNone,
		},
	}
}

func mustUnmarshalFixture(t *testing.T, raw string, v any) {
	t.Helper()
	require.NoError(t, json.Unmarshal([]byte(raw), v))
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const fixtureOpenAIChatRequest = `{
	"model": "gpt-test",
	"max_tokens": 1024,
	"stream": true,
	"messages": [
		{"role": "system", "content": "You are a helpful assistant."},
		{"role": "user", "content": "What is the weather in Paris?"},
		{"role": "assistant", "tool_calls": [{"id": "call_abc", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}}]},
		{"role": "tool", "tool_call_id": "call_abc", "content": "15 degrees"},
		{"role": "user", "content": "Summarize."}
	],
	"tools": [{"type": "function", "function": {"name": "get_weather", "description": "Get weather by city", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}}],
	"tool_choice": "auto"
}`

const fixtureClaudeRequest = `{
	"model": "claude-test",
	"max_tokens": 1024,
	"messages": [
		{"role": "user", "content": "What is the weather in Paris?"},
		{"role": "assistant", "content": [{"type": "tool_use", "id": "call_abc", "name": "get_weather", "input": {"city": "Paris"}}]},
		{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "call_abc", "content": "15 degrees"}]}
	],
	"tools": [{"name": "get_weather", "description": "Get weather by city", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}]
}`

const fixtureGeminiRequest = `{
	"model": "gemini-test",
	"contents": [
		{"role": "user", "parts": [{"text": "What is the weather in Paris?"}]},
		{"role": "model", "parts": [{"functionCall": {"name": "get_weather", "args": {"city": "Paris"}}}]},
		{"role": "user", "parts": [{"functionResponse": {"name": "get_weather", "response": {"temp": "15"}}}]}
	],
	"tools": [{"functionDeclarations": [{"name": "get_weather", "description": "Get weather by city", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}]}]
}`

const fixtureOpenAITextResponse = `{
	"id": "chatcmpl-fixed",
	"object": "chat.completion",
	"created": 1700000000,
	"model": "gpt-test",
	"choices": [{
		"index": 0,
		"message": {
			"role": "assistant",
			"content": "The answer is 42.",
			"reasoning_content": "Deep thought.",
			"tool_calls": [{"id": "call_abc", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Paris\"}"}}]
		},
		"finish_reason": "tool_calls"
	}],
	"usage": {
		"prompt_tokens": 10,
		"completion_tokens": 5,
		"total_tokens": 15,
		"prompt_tokens_details": {"cached_tokens": 3},
		"completion_tokens_details": {"reasoning_tokens": 2}
	}
}`

const fixtureClaudeResponse = `{
	"id": "msg_fixed",
	"type": "message",
	"role": "assistant",
	"model": "claude-test",
	"stop_reason": "tool_use",
	"content": [
		{"type": "thinking", "thinking": "Deep thought.", "signature": ""},
		{"type": "text", "text": "The answer is 42."},
		{"type": "tool_use", "id": "call_abc", "name": "get_weather", "input": {"city": "Paris"}}
	],
	"usage": {"input_tokens": 10, "output_tokens": 5}
}`

// ---------------------------------------------------------------------------
// Golden matrices
// ---------------------------------------------------------------------------

func TestGoldenRequestConversion(t *testing.T) {
	t.Run("openai_to_claude", func(t *testing.T) {
		var req dto.ClaudeRequest
		mustUnmarshalFixture(t, fixtureClaudeRequest, &req)
		info := goldenRelayInfo("claude-test", constant.ChannelTypeAnthropic, "claude-test")
		out, err := ClaudeToOpenAIRequest(req, info)
		require.NoError(t, err)
		checkGolden(t, "request/claude_to_openai", marshalGolden(t, out))
	})

	t.Run("gemini_to_openai", func(t *testing.T) {
		var req dto.GeminiChatRequest
		mustUnmarshalFixture(t, fixtureGeminiRequest, &req)
		info := goldenRelayInfo("gemini-test", constant.ChannelTypeGemini, "gemini-test")
		out, err := GeminiToOpenAIRequest(&req, info)
		require.NoError(t, err)
		checkGolden(t, "request/gemini_to_openai", marshalGolden(t, out))
	})
}

func TestGoldenResponseConversion(t *testing.T) {
	t.Run("openai_to_claude", func(t *testing.T) {
		var resp dto.OpenAITextResponse
		mustUnmarshalFixture(t, fixtureOpenAITextResponse, &resp)
		info := goldenRelayInfo("gpt-test", constant.ChannelTypeOpenAI, "gpt-test")
		out, err := ResponseOpenAI2ClaudeWithError(&resp, info)
		require.NoError(t, err)
		checkGolden(t, "response/openai_to_claude", marshalGolden(t, out))
	})

	// The DeepSeek gate is the reason this migration exists: reasoning content is
	// exposed as a Claude thinking block only for DeepSeek model names
	// (shouldExposeReasoningAsClaudeThinking, service/convert.go:533). A
	// non-DeepSeek baseline alone would pass even if the gate flipped to
	// always-on or always-off, so both sides of the gate are pinned.
	t.Run("openai_to_claude_deepseek_thinking", func(t *testing.T) {
		var resp dto.OpenAITextResponse
		mustUnmarshalFixture(t, fixtureOpenAITextResponse, &resp)
		info := goldenRelayInfo("deepseek-v4", constant.ChannelTypeDeepSeek, "deepseek-v4")
		out, err := ResponseOpenAI2ClaudeWithError(&resp, info)
		require.NoError(t, err)
		checkGolden(t, "response/openai_to_claude_deepseek", marshalGolden(t, out))
	})

	t.Run("openai_to_gemini", func(t *testing.T) {
		var resp dto.OpenAITextResponse
		mustUnmarshalFixture(t, fixtureOpenAITextResponse, &resp)
		info := goldenRelayInfo("gpt-test", constant.ChannelTypeOpenAI, "gpt-test")
		out, err := ResponseOpenAI2GeminiWithError(&resp, info)
		require.NoError(t, err)
		checkGolden(t, "response/openai_to_gemini", marshalGolden(t, out))
	})
}

func TestGoldenStreamConversion(t *testing.T) {
	chunks := []string{
		`{"id":"chatcmpl-fixed","object":"chat.completion.chunk","created":1700000000,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Deep "},"finish_reason":null}]}`,
		`{"id":"chatcmpl-fixed","object":"chat.completion.chunk","created":1700000000,"model":"gpt-test","choices":[{"index":0,"delta":{"reasoning_content":"thought."},"finish_reason":null}]}`,
		`{"id":"chatcmpl-fixed","object":"chat.completion.chunk","created":1700000000,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"The answer "},"finish_reason":null}]}`,
		`{"id":"chatcmpl-fixed","object":"chat.completion.chunk","created":1700000000,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"is 42."},"finish_reason":null}]}`,
		`{"id":"chatcmpl-fixed","object":"chat.completion.chunk","created":1700000000,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-fixed","object":"chat.completion.chunk","created":1700000000,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Paris\"}"}}]},"finish_reason":null}]}`,
		`{"id":"chatcmpl-fixed","object":"chat.completion.chunk","created":1700000000,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
	}

	t.Run("openai_to_claude", func(t *testing.T) {
		info := goldenRelayInfo("gpt-test", constant.ChannelTypeOpenAI, "gpt-test")
		info.IsStream = true
		var events []any
		for _, raw := range chunks {
			var chunk dto.ChatCompletionsStreamResponse
			mustUnmarshalFixture(t, raw, &chunk)
			out, err := StreamResponseOpenAI2ClaudeWithError(&chunk, info)
			require.NoError(t, err)
			for _, item := range out {
				events = append(events, item)
			}
		}
		checkEventsGolden(t, "stream/openai_to_claude", events)
	})

	// Same gate as the non-streaming case, on the streaming state machine:
	// reasoning chunks become content_block_start/delta of type thinking only
	// for DeepSeek model names. The non-DeepSeek run above drops them entirely,
	// so the two together pin both directions of the gate.
	t.Run("openai_to_claude_deepseek_thinking", func(t *testing.T) {
		info := goldenRelayInfo("deepseek-v4", constant.ChannelTypeDeepSeek, "deepseek-v4")
		info.IsStream = true
		var events []any
		for _, raw := range chunks {
			var chunk dto.ChatCompletionsStreamResponse
			mustUnmarshalFixture(t, raw, &chunk)
			out, err := StreamResponseOpenAI2ClaudeWithError(&chunk, info)
			require.NoError(t, err)
			for _, item := range out {
				events = append(events, item)
			}
		}
		checkEventsGolden(t, "stream/openai_to_claude_deepseek", events)
	})

	t.Run("openai_to_gemini", func(t *testing.T) {
		info := goldenRelayInfo("gpt-test", constant.ChannelTypeOpenAI, "gpt-test")
		info.IsStream = true
		var events []any
		for _, raw := range chunks {
			var chunk dto.ChatCompletionsStreamResponse
			mustUnmarshalFixture(t, raw, &chunk)
			out, err := StreamResponseOpenAI2GeminiWithError(&chunk, info)
			require.NoError(t, err)
			if out != nil {
				events = append(events, out)
			}
		}
		checkEventsGolden(t, "stream/openai_to_gemini", events)
	})
}
