package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeneralOpenAIRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"stream":false,
		"max_tokens":0,
		"max_completion_tokens":0,
		"top_p":0,
		"top_k":0,
		"n":0,
		"frequency_penalty":0,
		"presence_penalty":0,
		"seed":0,
		"logprobs":false,
		"top_logprobs":0,
		"dimensions":0,
		"return_images":false,
		"return_related_questions":false
	}`)

	var req GeneralOpenAIRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_completion_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_k").Exists())
	require.True(t, gjson.GetBytes(encoded, "n").Exists())
	require.True(t, gjson.GetBytes(encoded, "frequency_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "presence_penalty").Exists())
	require.True(t, gjson.GetBytes(encoded, "seed").Exists())
	require.True(t, gjson.GetBytes(encoded, "logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_logprobs").Exists())
	require.True(t, gjson.GetBytes(encoded, "dimensions").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_images").Exists())
	require.True(t, gjson.GetBytes(encoded, "return_related_questions").Exists())
}

func TestOpenAIResponsesRequestPreserveExplicitZeroValues(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-4.1",
		"max_output_tokens":0,
		"max_tool_calls":0,
		"stream":false,
		"top_p":0
	}`)

	var req OpenAIResponsesRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "max_output_tokens").Exists())
	require.True(t, gjson.GetBytes(encoded, "max_tool_calls").Exists())
	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "top_p").Exists())
}

func TestOpenAIResponsesRequestPreservesExplicitEmptyReasoningScalars(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"reasoning":{
			"effort":"",
			"summary":""
		}
	}`)

	var req OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(raw, &req))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(encoded, "reasoning.effort").Exists())
	require.Equal(t, "", gjson.GetBytes(encoded, "reasoning.effort").String())
	require.True(t, gjson.GetBytes(encoded, "reasoning.summary").Exists())
	require.Equal(t, "", gjson.GetBytes(encoded, "reasoning.summary").String())
}

func TestOpenAIResponsesRequestPreservesUnknownFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.5",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}],
		"background":true,
		"sub2api_session_id":"session-1",
		"custom_payload":{"nested":1}
	}`)

	var req OpenAIResponsesRequest
	err := common.Unmarshal(raw, &req)
	require.NoError(t, err)

	require.Equal(t, "gpt-5.5", req.Model)
	require.Contains(t, req.Extra, "background")
	require.Contains(t, req.Extra, "sub2api_session_id")
	require.Contains(t, req.Extra, "custom_payload")

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "background").Bool())
	require.Equal(t, "session-1", gjson.GetBytes(encoded, "sub2api_session_id").String())
	require.Equal(t, int64(1), gjson.GetBytes(encoded, "custom_payload.nested").Int())
}

func TestOpenAIResponsesRequestPreservesCodexNestedFields(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"input":"continue",
		"reasoning":{
			"effort":"low",
			"summary":"none",
			"context":"all_turns",
			"mode":"auto",
			"future_reasoning_option":{"enabled":true}
		},
		"stream_options":{
			"include_usage":true,
			"include_obfuscation":false,
			"reasoning_summary_delivery":"sequential_cutoff",
			"future_stream_option":{"enabled":true}
		}
	}`)

	var req OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(raw, &req))
	require.NotNil(t, req.Reasoning)
	require.JSONEq(t, `"all_turns"`, string(req.Reasoning.Context))
	require.JSONEq(t, `"auto"`, string(req.Reasoning.Mode))
	require.JSONEq(t, `{"enabled":true}`, string(req.Reasoning.Extra["future_reasoning_option"]))
	require.NotNil(t, req.StreamOptions)
	require.JSONEq(t, `"sequential_cutoff"`, string(req.StreamOptions.Extra["reasoning_summary_delivery"]))
	require.JSONEq(t, `{"enabled":true}`, string(req.StreamOptions.Extra["future_stream_option"]))

	encoded, err := common.Marshal(req)
	require.NoError(t, err)
	require.JSONEq(t, `"all_turns"`, gjson.GetBytes(encoded, "reasoning.context").Raw)
	require.JSONEq(t, `"auto"`, gjson.GetBytes(encoded, "reasoning.mode").Raw)
	require.JSONEq(t, `{"enabled":true}`, gjson.GetBytes(encoded, "reasoning.future_reasoning_option").Raw)
	require.JSONEq(t, `"sequential_cutoff"`, gjson.GetBytes(encoded, "stream_options.reasoning_summary_delivery").Raw)
	require.JSONEq(t, `{"enabled":true}`, gjson.GetBytes(encoded, "stream_options.future_stream_option").Raw)
}

func TestResponsesDTOsPreserveExplicitNullReasoningAndStreamOptions(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"reasoning":{
			"effort":null,
			"summary":null,
			"mode":null,
			"context":null,
			"future_reasoning_option":null
		},
		"stream_options":{
			"include_usage":null,
			"include_obfuscation":null,
			"reasoning_summary_delivery":null,
			"future_stream_option":null
		}
	}`)

	var request OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(raw, &request))
	encoded, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"effort":null,"summary":null,"mode":null,"context":null,"future_reasoning_option":null}`, gjson.GetBytes(encoded, "reasoning").Raw)
	require.JSONEq(t, `{"include_usage":null,"include_obfuscation":null,"reasoning_summary_delivery":null,"future_stream_option":null}`, gjson.GetBytes(encoded, "stream_options").Raw)

	var compactRequest OpenAIResponsesCompactionRequest
	require.NoError(t, common.Unmarshal(raw, &compactRequest))
	converted := compactRequest.ToResponsesRequest()
	require.NotNil(t, converted)
	convertedEncoded, err := common.Marshal(converted)
	require.NoError(t, err)
	require.JSONEq(t, `{"effort":null,"summary":null,"mode":null,"context":null,"future_reasoning_option":null}`, gjson.GetBytes(convertedEncoded, "reasoning").Raw)
	require.JSONEq(t, `{"include_usage":null,"include_obfuscation":null,"reasoning_summary_delivery":null,"future_stream_option":null}`, gjson.GetBytes(convertedEncoded, "stream_options").Raw)
}

func TestStreamOptionsPreservesExplicitFalseFields(t *testing.T) {
	raw := []byte(`{
		"include_usage":false,
		"include_obfuscation":false,
		"future_stream_option":{"enabled":true}
	}`)

	var options StreamOptions
	require.NoError(t, common.Unmarshal(raw, &options))

	encoded, err := common.Marshal(options)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(encoded, "include_usage").Exists())
	require.False(t, gjson.GetBytes(encoded, "include_usage").Bool())
	require.True(t, gjson.GetBytes(encoded, "include_obfuscation").Exists())
	require.False(t, gjson.GetBytes(encoded, "include_obfuscation").Bool())
	require.JSONEq(t, `{"enabled":true}`, gjson.GetBytes(encoded, "future_stream_option").Raw)
}

func TestMessageParseContentAcceptsObjectVideoURLAndBothFileNameSpellings(t *testing.T) {
	message := Message{Content: []any{
		map[string]any{
			"type":      "video_url",
			"video_url": map[string]any{"url": "https://example.com/video.mp4"},
		},
		map[string]any{
			"type": "file",
			"file": map[string]any{"filename": "modern.pdf", "file_data": "data:application/pdf;base64,AA==", "mime_type": "application/pdf"},
		},
		map[string]any{
			"type": "file",
			"file": map[string]any{"file_name": "legacy.txt", "file_data": "data:text/plain;base64,AA==", "mimeType": "text/plain"},
		},
		map[string]any{
			"type":      "file",
			"filename":  "flat.txt",
			"file_data": "data:text/plain;base64,AA==",
			"mime_type": "text/plain",
		},
	}}

	parts := message.ParseContent()
	require.Len(t, parts, 4)
	require.Equal(t, "https://example.com/video.mp4", parts[0].GetVideoUrl().Url)
	require.Equal(t, "modern.pdf", parts[1].GetFile().FileName)
	require.Equal(t, "application/pdf", parts[1].GetFile().MimeType)
	require.Equal(t, "legacy.txt", parts[2].GetFile().FileName)
	require.Equal(t, "text/plain", parts[2].GetFile().MimeType)
	require.Equal(t, "flat.txt", parts[3].GetFile().FileName)
	require.Equal(t, "text/plain", parts[3].GetFile().MimeType)
}

func TestOpenAIResponsesCompactionRequestPreservesUnknownFieldsThroughResponsesRequest(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.5-openai-compact",
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]}],
		"tools":[{"type":"namespace","name":"codex_app","tools":[]}],
		"store":false,
		"stream":true,
		"background":false,
		"sub2api_session_id":"session-compact"
	}`)

	var compactReq OpenAIResponsesCompactionRequest
	err := common.Unmarshal(raw, &compactReq)
	require.NoError(t, err)

	require.JSONEq(t, `[{"type":"namespace","name":"codex_app","tools":[]}]`, string(compactReq.Tools))
	require.JSONEq(t, `false`, string(compactReq.Store))
	require.NotNil(t, compactReq.Stream)
	require.True(t, *compactReq.Stream)
	require.Contains(t, compactReq.Extra, "background")
	require.Contains(t, compactReq.Extra, "sub2api_session_id")

	responsesReq := compactReq.ToResponsesRequest()
	require.NotNil(t, responsesReq)
	require.JSONEq(t, `false`, string(responsesReq.Store))
	require.NotNil(t, responsesReq.Stream)
	require.True(t, *responsesReq.Stream)
	require.Nil(t, responsesReq.StreamOptions)
	require.Contains(t, responsesReq.Extra, "background")
	require.Contains(t, responsesReq.Extra, "sub2api_session_id")

	encoded, err := common.Marshal(responsesReq)
	require.NoError(t, err)

	require.True(t, gjson.GetBytes(encoded, "store").Exists())
	require.False(t, gjson.GetBytes(encoded, "store").Bool())
	require.True(t, gjson.GetBytes(encoded, "stream").Exists())
	require.True(t, gjson.GetBytes(encoded, "stream").Bool())
	require.False(t, gjson.GetBytes(encoded, "stream_options").Exists())
	require.True(t, gjson.GetBytes(encoded, "background").Exists())
	require.False(t, gjson.GetBytes(encoded, "background").Bool())
	require.Equal(t, "session-compact", gjson.GetBytes(encoded, "sub2api_session_id").String())
}

func TestOpenAIResponsesCompactionRequestPreservesCodexNestedFieldsThroughConversion(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"input":"compact",
		"reasoning":{
			"effort":"max",
			"context":"all_turns",
			"mode":"auto",
			"future_reasoning_option":{"enabled":true}
		},
		"stream_options":{
			"reasoning_summary_delivery":"sequential_cutoff",
			"future_stream_option":{"enabled":true}
		}
	}`)

	var compactReq OpenAIResponsesCompactionRequest
	require.NoError(t, common.Unmarshal(raw, &compactReq))
	responsesReq := compactReq.ToResponsesRequest()
	require.NotNil(t, responsesReq)
	require.NotNil(t, responsesReq.Reasoning)
	require.NotNil(t, responsesReq.StreamOptions)

	compactReq.Reasoning.Context[1] = 'x'
	compactReq.Reasoning.Extra["future_reasoning_option"][2] = 'x'
	compactReq.StreamOptions.Extra["reasoning_summary_delivery"][1] = 'x'
	compactReq.StreamOptions.Extra["future_stream_option"][2] = 'x'

	encoded, err := common.Marshal(responsesReq)
	require.NoError(t, err)
	require.JSONEq(t, `"all_turns"`, gjson.GetBytes(encoded, "reasoning.context").Raw)
	require.JSONEq(t, `{"enabled":true}`, gjson.GetBytes(encoded, "reasoning.future_reasoning_option").Raw)
	require.JSONEq(t, `"sequential_cutoff"`, gjson.GetBytes(encoded, "stream_options.reasoning_summary_delivery").Raw)
	require.JSONEq(t, `{"enabled":true}`, gjson.GetBytes(encoded, "stream_options.future_stream_option").Raw)
}

func TestOpenAIResponsesCompactionRequestToResponsesRequestClonesPointerFields(t *testing.T) {
	stream := true
	maxOutputTokens := uint(1024)
	topLogProbs := 0
	temperature := 0.2
	topP := 0.9
	maxToolCalls := uint(3)
	compactReq := OpenAIResponsesCompactionRequest{
		Model:           "gpt-5.5-openai-compact",
		MaxOutputTokens: &maxOutputTokens,
		TopLogProbs:     &topLogProbs,
		Reasoning:       &Reasoning{Effort: "medium", Summary: "auto"},
		Stream:          &stream,
		StreamOptions:   &StreamOptions{IncludeUsage: true},
		Temperature:     &temperature,
		TopP:            &topP,
		MaxToolCalls:    &maxToolCalls,
	}

	responsesReq := compactReq.ToResponsesRequest()
	require.NotNil(t, responsesReq)

	*compactReq.Stream = false
	*compactReq.MaxOutputTokens = 1
	*compactReq.TopLogProbs = 2
	compactReq.Reasoning.Effort = "low"
	compactReq.StreamOptions.IncludeUsage = false
	*compactReq.Temperature = 1
	*compactReq.TopP = 1
	*compactReq.MaxToolCalls = 9

	require.NotNil(t, responsesReq.Stream)
	require.True(t, *responsesReq.Stream)
	require.Equal(t, uint(1024), *responsesReq.MaxOutputTokens)
	require.Equal(t, 0, *responsesReq.TopLogProbs)
	require.Equal(t, "medium", responsesReq.Reasoning.Effort)
	require.NotNil(t, responsesReq.StreamOptions)
	require.True(t, responsesReq.StreamOptions.IncludeUsage)
	require.Equal(t, 0.2, *responsesReq.Temperature)
	require.Equal(t, 0.9, *responsesReq.TopP)
	require.Equal(t, uint(3), *responsesReq.MaxToolCalls)
}

func TestOpenAIResponsesCompactionRequestToResponsesRequestClonesRawAndNestedPresenceState(t *testing.T) {
	raw := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"type":"message","role":"user","content":"continue"}],
		"instructions":"keep instructions",
		"metadata":{"request":"keep metadata"},
		"reasoning":{"effort":null,"context":"all_turns","future_reasoning_option":{"enabled":true}},
		"stream_options":{"include_usage":null,"future_stream_option":{"enabled":true}}
	}`)

	var compactReq OpenAIResponsesCompactionRequest
	require.NoError(t, common.Unmarshal(raw, &compactReq))
	responsesReq := compactReq.ToResponsesRequest()
	require.NotNil(t, responsesReq)

	compactReq.Input[0] = 'x'
	compactReq.Instructions[0] = 'x'
	compactReq.Metadata[0] = 'x'
	compactReq.Reasoning.effortRaw[0] = '0'
	compactReq.StreamOptions.includeUsageRaw[0] = '0'

	encoded, err := common.Marshal(responsesReq)
	require.NoError(t, err)
	require.Equal(t, "message", gjson.GetBytes(encoded, "input.0.type").String())
	require.Equal(t, "keep instructions", gjson.GetBytes(encoded, "instructions").String())
	require.Equal(t, "keep metadata", gjson.GetBytes(encoded, "metadata.request").String())
	require.Equal(t, "null", gjson.GetBytes(encoded, "reasoning.effort").Raw)
	require.Equal(t, "null", gjson.GetBytes(encoded, "stream_options.include_usage").Raw)
	require.JSONEq(t, `{"enabled":true}`, gjson.GetBytes(encoded, "reasoning.future_reasoning_option").Raw)
	require.JSONEq(t, `{"enabled":true}`, gjson.GetBytes(encoded, "stream_options.future_stream_option").Raw)
}

func TestOpenAIResponsesRequestPreservesUnknownEmptyKey(t *testing.T) {
	var req OpenAIResponsesRequest
	err := common.Unmarshal([]byte(`{"model":"gpt-5.5","":true}`), &req)
	require.NoError(t, err)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, common.Unmarshal(encoded, &out))
	require.Equal(t, true, out[""])
}
