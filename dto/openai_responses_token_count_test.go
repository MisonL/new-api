package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

const testImageGenerationResultPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAF/gL+3nM/1QAAAABJRU5ErkJggg=="

func TestOpenAIResponsesRequestTokenCountMetaIncludesEffectiveAdditionalTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Tools: common.RawMessage(`[{"type":"function","name":"top_level_tool","description":"TOP_LEVEL_TOOL_MARKER","parameters":{"type":"object"}}]`),
		Input: common.RawMessage(`[
			{"type":"additional_tools","role":"developer","tools":[
				{"type":"custom","name":"additional_tool","description":"ADDITIONAL_TOOL_MARKER","format":{"type":"text"}}
			]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	meta := req.GetTokenCountMeta()

	require.Contains(t, meta.CombineText, "TOP_LEVEL_TOOL_MARKER")
	require.Contains(t, meta.CombineText, "ADDITIONAL_TOOL_MARKER")
	require.Equal(t, 2, meta.ToolsCount)
	require.Equal(t, 1, strings.Count(meta.CombineText, "TOP_LEVEL_TOOL_MARKER"))
	require.Equal(t, 1, strings.Count(meta.CombineText, "ADDITIONAL_TOOL_MARKER"))
}

func TestOpenAIResponsesRequestTokenCountMetaIncludesLoadedToolSearchTools(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"tool_search_call","call_id":"call_search","execution":"client","arguments":"{\"query\":\"deploy\"}"},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":[
				{"type":"function","name":"loaded_deploy","description":"LOADED_TOOL_SEARCH_TOKEN_MARKER","parameters":{"type":"object"}}
			]}
		]`),
	}

	meta := req.GetTokenCountMeta()

	require.Contains(t, meta.CombineText, "LOADED_TOOL_SEARCH_TOKEN_MARKER")
	require.Equal(t, 1, meta.ToolsCount)
	require.Equal(t, 1, strings.Count(meta.CombineText, "LOADED_TOOL_SEARCH_TOKEN_MARKER"))
}

func TestOpenAIResponsesRequestTokenCountMetaIncludesLoadedToolSearchNamespace(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"tool_search_call","call_id":"call_search","execution":"client","arguments":"{\"query\":\"calendar\"}"},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","execution":"client","tools":[
				{"type":"namespace","name":"mcp__calendar","description":"LOADED_TOOL_SEARCH_NAMESPACE_TOKEN_MARKER","tools":[
					{"type":"function","name":"create_event","defer_loading":true,"parameters":{"type":"object"}}
				]}
			]}
		]`),
	}

	meta := req.GetTokenCountMeta()

	require.Contains(t, meta.CombineText, "LOADED_TOOL_SEARCH_NAMESPACE_TOKEN_MARKER")
	require.Equal(t, 1, meta.ToolsCount)
	require.Equal(t, 1, strings.Count(meta.CombineText, "LOADED_TOOL_SEARCH_NAMESPACE_TOKEN_MARKER"))
}

func TestOpenAIResponsesRequestTokenCountMetaIncludesMCPToolSnapshotOnce(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"mcp_list_tools","server_label":"calendar","tools":[
				{"name":"create_event","description":"MCP_LIST_TOOL_DESCRIPTION_MARKER","input_schema":{"type":"object"}}
			]},
			{"type":"mcp_call","name":"create_event","arguments":"{\"title\":\"MCP_CALL_ARGUMENT_MARKER\"}","output":"MCP_CALL_OUTPUT_MARKER","error":null}
		]`),
	}

	meta := req.GetTokenCountMeta()

	for _, marker := range []string{
		"MCP_LIST_TOOL_DESCRIPTION_MARKER",
		"MCP_CALL_ARGUMENT_MARKER",
		"MCP_CALL_OUTPUT_MARKER",
	} {
		require.Contains(t, meta.CombineText, marker)
		require.Equal(t, 1, strings.Count(meta.CombineText, marker))
	}
}

func TestOpenAIResponsesRequestTokenCountMetaIncludesDirectAndMessageInputAudio(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"input_audio","input_audio":{"data":"DIRECT_AUDIO_DATA","format":"wav"}},
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"transcribe the attached audio"},
				{"type":"input_audio","input_audio":{"data":"MESSAGE_AUDIO_DATA","format":"mp3"}}
			]}
		]`),
	}

	inputs := req.ParseInput()
	meta := req.GetTokenCountMeta()

	require.Len(t, inputs, 3)
	require.Equal(t, "input_audio", inputs[0].Type)
	require.Equal(t, "DIRECT_AUDIO_DATA", inputs[0].AudioData)
	require.Equal(t, "wav", inputs[0].AudioFormat)
	require.Equal(t, "input_text", inputs[1].Type)
	require.Equal(t, "input_audio", inputs[2].Type)
	require.Equal(t, "MESSAGE_AUDIO_DATA", inputs[2].AudioData)
	require.Equal(t, "mp3", inputs[2].AudioFormat)

	require.Contains(t, meta.CombineText, "transcribe the attached audio")
	require.Len(t, meta.Files, 2)
	require.Equal(t, types.FileTypeAudio, meta.Files[0].FileType)
	require.Equal(t, "DIRECT_AUDIO_DATA", meta.Files[0].GetRawData())
	require.Equal(t, types.FileTypeAudio, meta.Files[1].FileType)
	require.Equal(t, "MESSAGE_AUDIO_DATA", meta.Files[1].GetRawData())
}

func TestOpenAIResponsesCompactionRequestTokenCountMetaUsesResponsesMediaSemantics(t *testing.T) {
	request := &OpenAIResponsesCompactionRequest{
		Model:          "gpt-5.6-sol",
		PromptCacheKey: common.RawMessage(`"cache-key-must-not-be-counted"`),
		Tools: common.RawMessage(`[
			{"type":"function","name":"lookup","description":"COMPACT_TOOL_MARKER","parameters":{"type":"object"}}
		]`),
		Input: common.RawMessage(`[
			{"type":"input_audio","input_audio":{"data":"COMPACT_AUDIO_DATA","format":"wav"}},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"compact audio context"}]}
		]`),
	}

	meta := request.GetTokenCountMeta()

	require.Contains(t, meta.CombineText, "compact audio context")
	require.Contains(t, meta.CombineText, "COMPACT_TOOL_MARKER")
	require.NotContains(t, meta.CombineText, "COMPACT_AUDIO_DATA")
	require.NotContains(t, meta.CombineText, "cache-key-must-not-be-counted")
	require.Equal(t, 1, meta.ToolsCount)
	require.Len(t, meta.Files, 1)
	require.Equal(t, types.FileTypeAudio, meta.Files[0].FileType)
	require.Equal(t, "COMPACT_AUDIO_DATA", meta.Files[0].GetRawData())
}

func TestOpenAIResponsesCompactionRequestTokenCountMetaKeepsSemanticHistory(t *testing.T) {
	request := &OpenAIResponsesCompactionRequest{
		Instructions:      common.RawMessage(`"COMPACT_INSTRUCTIONS_MARKER"`),
		ParallelToolCalls: common.RawMessage(`true`),
		Text:              common.RawMessage(`{"format":{"type":"text","name":"COMPACT_TEXT_MARKER"}}`),
		Reasoning: &Reasoning{
			Effort:  "high",
			Summary: "auto",
			Extra: map[string]common.RawMessage{
				"future_reasoning_option": common.RawMessage(`"COMPACT_REASONING_MARKER"`),
			},
		},
		Input: common.RawMessage(`[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"COMPACT_REASONING_HISTORY_MARKER"}],"encrypted_content":"COMPACT_ENCRYPTED_CONTEXT_MARKER"},
			{"type":"function_call","call_id":"call_function","namespace":"COMPACT_FUNCTION_NAMESPACE_MARKER","name":"COMPACT_FUNCTION_NAME_MARKER","arguments":"{\"query\":\"COMPACT_FUNCTION_ARGUMENT_MARKER\"}"},
			{"type":"function_call_output","call_id":"call_function","output":"COMPACT_FUNCTION_OUTPUT_MARKER"},
			{"type":"custom_tool_call","call_id":"call_custom","name":"COMPACT_CUSTOM_NAME_MARKER","input":"COMPACT_CUSTOM_INPUT_MARKER"},
			{"type":"custom_tool_call_output","call_id":"call_custom","output":[{"type":"input_text","text":"COMPACT_CUSTOM_OUTPUT_MARKER"}]},
			{"type":"tool_search_call","call_id":"call_search","arguments":{"query":"COMPACT_TOOL_SEARCH_ARGUMENT_MARKER"}},
			{"type":"tool_search_output","call_id":"call_search","status":"completed","tools":[{"type":"function","name":"loaded","description":"COMPACT_LOADED_TOOL_MARKER","parameters":{"type":"object"}}]},
			{"type":"input_audio","input_audio":{"data":"COMPACT_AUDIO_DATA","format":"wav"}},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"COMPACT_MESSAGE_OUTPUT_MARKER"}]}
		]`),
	}

	meta := request.GetTokenCountMeta()

	for _, marker := range []string{
		"COMPACT_INSTRUCTIONS_MARKER",
		"COMPACT_TEXT_MARKER",
		"COMPACT_REASONING_HISTORY_MARKER",
		"COMPACT_FUNCTION_NAMESPACE_MARKER",
		"COMPACT_FUNCTION_NAME_MARKER",
		"COMPACT_FUNCTION_ARGUMENT_MARKER",
		"COMPACT_FUNCTION_OUTPUT_MARKER",
		"COMPACT_CUSTOM_NAME_MARKER",
		"COMPACT_CUSTOM_INPUT_MARKER",
		"COMPACT_CUSTOM_OUTPUT_MARKER",
		"COMPACT_TOOL_SEARCH_ARGUMENT_MARKER",
		"COMPACT_LOADED_TOOL_MARKER",
		"COMPACT_MESSAGE_OUTPUT_MARKER",
	} {
		require.Contains(t, meta.CombineText, marker)
	}
	require.NotContains(t, meta.CombineText, "COMPACT_REASONING_MARKER")
	require.NotContains(t, meta.CombineText, "COMPACT_ENCRYPTED_CONTEXT_MARKER")
	require.NotContains(t, meta.CombineText, "COMPACT_AUDIO_DATA")
	require.Equal(t, 1, meta.ToolsCount)
	require.Len(t, meta.Files, 1)
	require.Equal(t, types.FileTypeAudio, meta.Files[0].FileType)
}

func TestOpenAIResponsesRequestTokenCountMetaKeepsStructuredToolOutputSemantics(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{
				"type":"custom_tool_call",
				"call_id":"call_custom",
				"namespace":"CUSTOM_TOOL_NAMESPACE_MARKER",
				"name":"CUSTOM_TOOL_NAME_MARKER",
				"input":{"command":"CUSTOM_OBJECT_INPUT_MARKER","nested":{"value":1}}
			},
			{
				"type":"custom_tool_call_output",
				"call_id":"call_custom",
				"output":[
					{"type":"input_text","text":"STRUCTURED_TOOL_TEXT_MARKER"},
					{"type":"output_text","text":"STRUCTURED_TOOL_OUTPUT_TEXT_MARKER"},
					{"type":"input_image","image_url":{"url":"https://example.com/STRUCTURED_TOOL_IMAGE_MARKER.png","detail":"low"}},
					{"type":"input_audio","input_audio":{"data":"STRUCTURED_TOOL_AUDIO_MARKER","format":"wav"}},
					{"type":"input_audio","audio_url":"data:audio/mpeg;base64,STRUCTURED_TOOL_AUDIO_URL_MARKER"},
					{"type":"input_file","file_data":"STRUCTURED_TOOL_FILE_DATA_MARKER"},
					{"type":"encrypted_content","encrypted_content":"STRUCTURED_TOOL_ENCRYPTED_MARKER"}
				]
			},
			{
				"type":"function_call_output",
				"call_id":"call_function",
				"output":[{"type":"input_text","text":"FUNCTION_OUTPUT_TEXT_MARKER"}]
			}
		]`),
	}

	meta := req.GetTokenCountMeta()

	for _, marker := range []string{
		"CUSTOM_TOOL_NAMESPACE_MARKER",
		"CUSTOM_TOOL_NAME_MARKER",
		`{"command":"CUSTOM_OBJECT_INPUT_MARKER","nested":{"value":1}}`,
		"STRUCTURED_TOOL_TEXT_MARKER",
		"STRUCTURED_TOOL_OUTPUT_TEXT_MARKER",
		"FUNCTION_OUTPUT_TEXT_MARKER",
	} {
		require.Contains(t, meta.CombineText, marker)
	}
	require.NotContains(t, meta.CombineText, "STRUCTURED_TOOL_ENCRYPTED_MARKER")
	require.NotContains(t, meta.CombineText, "STRUCTURED_TOOL_AUDIO_MARKER")
	require.NotContains(t, meta.CombineText, "STRUCTURED_TOOL_IMAGE_MARKER")
	require.Len(t, meta.Files, 4)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	require.Equal(t, "https://example.com/STRUCTURED_TOOL_IMAGE_MARKER.png", meta.Files[0].GetRawData())
	require.Equal(t, "low", meta.Files[0].Detail)
	require.Equal(t, types.FileTypeAudio, meta.Files[1].FileType)
	require.Equal(t, "STRUCTURED_TOOL_AUDIO_MARKER", meta.Files[1].GetRawData())
	require.Equal(t, types.FileTypeAudio, meta.Files[2].FileType)
	require.Equal(t, "data:audio/mpeg;base64,STRUCTURED_TOOL_AUDIO_URL_MARKER", meta.Files[2].GetRawData())
	require.Equal(t, types.FileTypeFile, meta.Files[3].FileType)
	require.Equal(t, "STRUCTURED_TOOL_FILE_DATA_MARKER", meta.Files[3].GetRawData())
}

func TestOpenAIResponsesRequestTokenCountMetaKeepsInputFileSources(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"input_file","file_url":"https://example.com/INPUT_FILE_URL_MARKER.pdf"},
			{"type":"input_file","file_data":"INPUT_FILE_DATA_MARKER"},
			{"type":"input_file","file_id":"file_INPUT_FILE_ID_MARKER"},
			{"type":"input_file","file":{"file_data":"NESTED_INPUT_FILE_DATA_MARKER"}},
			{"type":"input_file","file":{"file_id":"file_NESTED_INPUT_FILE_ID_MARKER"}}
		]`),
	}

	meta := req.GetTokenCountMeta()

	require.Empty(t, meta.CombineText)
	require.Len(t, meta.Files, 5)
	require.Equal(t, types.FileTypeFile, meta.Files[0].FileType)
	require.Equal(t, "https://example.com/INPUT_FILE_URL_MARKER.pdf", meta.Files[0].GetRawData())
	require.Equal(t, types.FileTypeFile, meta.Files[1].FileType)
	require.Equal(t, "INPUT_FILE_DATA_MARKER", meta.Files[1].GetRawData())
	require.Equal(t, types.FileTypeFile, meta.Files[2].FileType)
	require.Nil(t, meta.Files[2].Source)
	require.Equal(t, types.FileTypeFile, meta.Files[3].FileType)
	require.Equal(t, "NESTED_INPUT_FILE_DATA_MARKER", meta.Files[3].GetRawData())
	require.Equal(t, types.FileTypeFile, meta.Files[4].FileType)
	require.Nil(t, meta.Files[4].Source)
}

func TestOpenAIResponsesRequestTokenCountMetaKeepsStringReasoningSummary(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"reasoning","summary":"STRING_REASONING_SUMMARY_MARKER","content":[{"type":"reasoning_text","text":"REASONING_CONTENT_MARKER"}],"encrypted_content":"REASONING_ENCRYPTED_MARKER"}
		]`),
	}

	meta := req.GetTokenCountMeta()

	require.Contains(t, meta.CombineText, "STRING_REASONING_SUMMARY_MARKER")
	require.Contains(t, meta.CombineText, "REASONING_CONTENT_MARKER")
	require.NotContains(t, meta.CombineText, "REASONING_ENCRYPTED_MARKER")
}

func TestOpenAIResponsesRequestTokenCountMetaKeepsCodexToolHistorySemantics(t *testing.T) {
	req := &OpenAIResponsesRequest{
		Input: common.RawMessage(`[
			{"type":"local_shell_call","action":{"command":["printf CODEX_LOCAL_SHELL_COMMAND_MARKER"],"env":{"SECRET":"LOCAL_SHELL_SECRET_MARKER"}}},
			{"type":"local_shell_call_output","output":"LOCAL_SHELL_OUTPUT_MARKER"},
			{"type":"shell_call","action":{"commands":["echo CODEX_SHELL_COMMAND_MARKER"]}},
			{"type":"shell_call_output","output":[{"stdout":"SHELL_STDOUT_MARKER","stderr":"SHELL_STDERR_MARKER","outcome":{"type":"exit","exit_code":0}}]},
			{"type":"apply_patch_call","operation":{"type":"update_file","path":"file.txt","diff":"APPLY_PATCH_DIFF_MARKER"}},
			{"type":"apply_patch_call_output","output":"APPLY_PATCH_OUTPUT_MARKER"},
			{"type":"code_interpreter_call","code":"print('CODE_INTERPRETER_CODE_MARKER')","outputs":[{"type":"logs","logs":"CODE_INTERPRETER_LOG_MARKER"},{"type":"image","url":"https://example.com/CODE_INTERPRETER_IMAGE_MARKER.png"}]},
			{"type":"mcp_approval_request","name":"MCP_APPROVAL_NAME_MARKER","arguments":"{\"query\":\"MCP_APPROVAL_ARGUMENT_MARKER\"}"},
			{"type":"mcp_approval_response","approve":false,"reason":"MCP_APPROVAL_REASON_MARKER"},
			{"type":"mcp_call","name":"MCP_CALL_NAME_MARKER","arguments":"{\"query\":\"MCP_CALL_ARGUMENT_MARKER\"}","output":"MCP_CALL_OUTPUT_MARKER","error":"MCP_CALL_ERROR_MARKER"},
			{"type":"file_search_call","queries":["FILE_SEARCH_QUERY_MARKER"],"results":[{"text":"FILE_SEARCH_RESULT_MARKER","file_id":"file_FILE_SEARCH_ID_MARKER"}]},
			{"type":"multi_agent_call_output","output":[{"type":"output_text","text":"MULTI_AGENT_OUTPUT_MARKER"}]},
			{"type":"program","code":"PROGRAM_CODE_MARKER","fingerprint":"PROGRAM_FINGERPRINT_MARKER"},
			{"type":"program_output","result":"PROGRAM_RESULT_MARKER"},
				{"type":"image_generation_call","result":"` + testImageGenerationResultPNG + `"}
		]`),
	}

	meta := req.GetTokenCountMeta()

	for _, marker := range []string{
		"CODEX_LOCAL_SHELL_COMMAND_MARKER",
		"LOCAL_SHELL_OUTPUT_MARKER",
		"CODEX_SHELL_COMMAND_MARKER",
		"SHELL_STDOUT_MARKER",
		"SHELL_STDERR_MARKER",
		"APPLY_PATCH_DIFF_MARKER",
		"APPLY_PATCH_OUTPUT_MARKER",
		"CODE_INTERPRETER_CODE_MARKER",
		"CODE_INTERPRETER_LOG_MARKER",
		"MCP_APPROVAL_NAME_MARKER",
		"MCP_APPROVAL_ARGUMENT_MARKER",
		"MCP_APPROVAL_REASON_MARKER",
		"MCP_CALL_NAME_MARKER",
		"MCP_CALL_ARGUMENT_MARKER",
		"MCP_CALL_OUTPUT_MARKER",
		"MCP_CALL_ERROR_MARKER",
		"FILE_SEARCH_QUERY_MARKER",
		"FILE_SEARCH_RESULT_MARKER",
		"MULTI_AGENT_OUTPUT_MARKER",
		"PROGRAM_CODE_MARKER",
		"PROGRAM_RESULT_MARKER",
	} {
		require.Contains(t, meta.CombineText, marker)
	}
	require.NotContains(t, meta.CombineText, "LOCAL_SHELL_SECRET_MARKER")
	require.NotContains(t, meta.CombineText, "PROGRAM_FINGERPRINT_MARKER")
	require.NotContains(t, meta.CombineText, testImageGenerationResultPNG)
	require.Len(t, meta.Files, 2)
	require.Equal(t, types.FileTypeImage, meta.Files[0].FileType)
	require.Equal(t, "https://example.com/CODE_INTERPRETER_IMAGE_MARKER.png", meta.Files[0].GetRawData())
	require.Equal(t, types.FileTypeImage, meta.Files[1].FileType)
	require.Equal(t, testImageGenerationResultPNG, meta.Files[1].GetRawData())
}
