package service

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestPrepareCodexLocalCompactionIngressRequestPreservesSmallBody(t *testing.T) {
	original := []byte(`{
		"model":"gpt-5.6-sol",
		"stream":true,
		"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"SMALL_COMPACTION_CONTEXT"}]}],
		"tools":[{"type":"function","name":"keep_for_normal_request","parameters":{"type":"object"}}],
		"additional_tools":[{"type":"custom","name":"keep_for_normal_request"}],
		"future_compaction_option":{"enabled":true}
	}`)

	body, reduced, info, err := PrepareCodexLocalCompactionIngressRequest(bytes.NewReader(original))
	require.NoError(t, err)
	require.False(t, reduced)
	require.Equal(t, int64(len(original)), info.OriginalRequestBytes)
	require.Equal(t, original, body)
}

func TestPrepareCodexLocalCompactionIngressRequestReducesLargeReplayWithoutReplayingMediaOrTools(t *testing.T) {
	const (
		mediaItemCount = 17
		mediaItemBytes = 16 * 1024 * 1024
	)

	body, reduced, info, err := PrepareCodexLocalCompactionIngressRequest(
		newLargeCodexLocalCompactionReplayReader(mediaItemCount, mediaItemBytes),
	)

	require.NoError(t, err)
	require.True(t, reduced)
	require.Greater(t, info.OriginalRequestBytes, int64(256<<20))
	require.LessOrEqual(t, len(body), CodexLocalCompaction413RecoverySafeBodyMaxBytes)
	require.Equal(t, mediaItemCount+3, info.InputItemCount)
	require.True(t, info.EmbeddedMediaOmitted)

	var reducedRequest dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(body, &reducedRequest))
	require.Equal(t, "gpt-5.6-sol", reducedRequest.Model)
	require.NotNil(t, reducedRequest.Stream)
	require.True(t, *reducedRequest.Stream)
	require.NotNil(t, reducedRequest.Reasoning)
	require.JSONEq(t, `"all_turns"`, string(reducedRequest.Reasoning.Context))
	require.JSONEq(t, `"summary"`, string(reducedRequest.Reasoning.Mode))
	require.NotNil(t, reducedRequest.StreamOptions)
	require.True(t, reducedRequest.StreamOptions.IncludeUsage)
	require.Contains(t, string(reducedRequest.Input), "LATEST_VISIBLE_CONTEXT_MUST_SURVIVE")
	require.NotContains(t, string(reducedRequest.Input), "data:image/png;base64,")
	require.NotContains(t, string(reducedRequest.Input), "LARGE_TOOL_SCHEMA_MUST_NOT_SURVIVE")
	require.Empty(t, reducedRequest.Tools)
	require.NotContains(t, string(body), "TOP_LEVEL_TOOL_SCHEMA_MUST_NOT_SURVIVE")
	require.NotContains(t, string(body), "TOP_LEVEL_ADDITIONAL_TOOL_MUST_NOT_SURVIVE")
	require.JSONEq(t, `{"enabled":true}`, string(reducedRequest.Extra["future_compaction_option"]))

	var inputItems []map[string]common.RawMessage
	require.NoError(t, common.Unmarshal(reducedRequest.Input, &inputItems))
	require.Len(t, inputItems, 1)
	var inputType string
	require.NoError(t, common.Unmarshal(inputItems[0]["type"], &inputType))
	require.Equal(t, "message", inputType)
}

func TestPrepareCodexLocalCompactionIngressRequestSkipsOversizedDiscardedTools(t *testing.T) {
	const oversizedToolSchemaBytes = 33 * 1024 * 1024

	body, reduced, info, err := PrepareCodexLocalCompactionIngressRequest(
		newLargeCodexLocalCompactionOversizedToolsReader(oversizedToolSchemaBytes),
	)

	require.NoError(t, err)
	require.True(t, reduced)
	require.Greater(t, info.OriginalRequestBytes, int64(oversizedToolSchemaBytes))
	require.LessOrEqual(t, len(body), CodexLocalCompaction413RecoverySafeBodyMaxBytes)

	var reducedRequest dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(body, &reducedRequest))
	require.Contains(t, string(reducedRequest.Input), "LATEST_VISIBLE_CONTEXT_MUST_SURVIVE")
	require.Empty(t, reducedRequest.Tools)
	require.NotContains(t, string(body), "OVERSIZED_TOOL_SCHEMA_MUST_NOT_SURVIVE")
}

func TestPrepareCodexLocalCompactionIngressRequestSkipsOversizedInputAdditionalTools(t *testing.T) {
	const oversizedToolSchemaBytes = 33 * 1024 * 1024

	body, reduced, info, err := PrepareCodexLocalCompactionIngressRequest(
		newLargeCodexLocalCompactionOversizedInputAdditionalToolsReader(oversizedToolSchemaBytes),
	)

	require.NoError(t, err)
	require.True(t, reduced)
	require.Greater(t, info.OriginalRequestBytes, int64(oversizedToolSchemaBytes))
	require.Equal(t, 2, info.InputItemCount)
	require.LessOrEqual(t, len(body), CodexLocalCompaction413RecoverySafeBodyMaxBytes)

	var reducedRequest dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal(body, &reducedRequest))
	require.Contains(t, string(reducedRequest.Input), "LATEST_VISIBLE_CONTEXT_MUST_SURVIVE")
	require.NotContains(t, string(body), "OVERSIZED_INPUT_ADDITIONAL_TOOL_MUST_NOT_SURVIVE")
}

func TestPrepareCodexLocalCompactionIngressRequestRejectsMalformedOversizedTopLevelTools(t *testing.T) {
	const oversizedToolSchemaBytes = 33 * 1024 * 1024

	_, _, _, err := PrepareCodexLocalCompactionIngressRequest(
		newLargeCodexLocalCompactionMalformedTopLevelToolsReader(oversizedToolSchemaBytes),
	)

	require.ErrorContains(t, err, "tools must be an array")
}

func TestPrepareCodexLocalCompactionIngressRequestRejectsOpaqueEncryptedReplayAfterLargePrefix(t *testing.T) {
	const oversizedToolSchemaBytes = 33 * 1024 * 1024

	body, reduced, _, err := PrepareCodexLocalCompactionIngressRequest(
		newLargeCodexLocalCompactionOpaqueReplayReader(oversizedToolSchemaBytes),
	)

	require.ErrorContains(t, err, "opaque encrypted_content")
	require.False(t, reduced)
	require.Nil(t, body)
}

func newLargeCodexLocalCompactionReplayReader(mediaItemCount, mediaItemBytes int) io.Reader {
	parts := []io.Reader{
		strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"reasoning":{"effort":"high","mode":"summary","context":"all_turns"},"stream_options":{"include_usage":true,"reasoning_summary_delivery":"detailed"},"tools":[{"type":"function","name":"LARGE_TOOL_SCHEMA_MUST_NOT_SURVIVE"}],"additional_tools":[{"type":"custom","name":"TOP_LEVEL_ADDITIONAL_TOOL_MUST_NOT_SURVIVE"}],"future_compaction_option":{"enabled":true},"input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"EARLIER_VISIBLE_CONTEXT"}]}`),
	}
	for index := 0; index < mediaItemCount; index++ {
		parts = append(parts,
			strings.NewReader(`,{"type":"custom_tool_call_output","call_id":"call_large_image","output":[{"type":"input_image","image_url":"data:image/png;base64,`),
			&repeatedByteReader{remaining: int64(mediaItemBytes), value: 'A'},
			strings.NewReader(`"}]}`),
		)
	}
	parts = append(parts,
		strings.NewReader(`,{"type":"additional_tools","tools":[{"type":"custom","name":"LARGE_TOOL_SCHEMA_MUST_NOT_SURVIVE"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_VISIBLE_CONTEXT_MUST_SURVIVE"}]}]}`),
	)
	return io.MultiReader(parts...)
}

func newLargeCodexLocalCompactionOversizedToolsReader(toolSchemaBytes int) io.Reader {
	return io.MultiReader(
		strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"tools":[{"type":"function","name":"OVERSIZED_TOOL_SCHEMA_MUST_NOT_SURVIVE","description":"`),
		&repeatedByteReader{remaining: int64(toolSchemaBytes), value: 'A'},
		strings.NewReader(`"}],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_VISIBLE_CONTEXT_MUST_SURVIVE"}]}]}`),
	)
}

func newLargeCodexLocalCompactionOversizedInputAdditionalToolsReader(toolSchemaBytes int) io.Reader {
	return io.MultiReader(
		strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"input":[{"tools":[{"type":"custom","name":"OVERSIZED_INPUT_ADDITIONAL_TOOL_MUST_NOT_SURVIVE","description":"`),
		&repeatedByteReader{remaining: int64(toolSchemaBytes), value: 'A'},
		strings.NewReader(`"}],"type":"additional_tools"},{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_VISIBLE_CONTEXT_MUST_SURVIVE"}]}]}`),
	)
}

func newLargeCodexLocalCompactionMalformedTopLevelToolsReader(toolSchemaBytes int) io.Reader {
	return io.MultiReader(
		strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"tools":{"description":"`),
		&repeatedByteReader{remaining: int64(toolSchemaBytes), value: 'A'},
		strings.NewReader(`"},"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"LATEST_VISIBLE_CONTEXT_MUST_SURVIVE"}]}]}`),
	)
}

func newLargeCodexLocalCompactionOpaqueReplayReader(toolSchemaBytes int) io.Reader {
	return io.MultiReader(
		strings.NewReader(`{"model":"gpt-5.6-sol","stream":true,"tools":[{"type":"function","name":"large_schema","description":"`),
		&repeatedByteReader{remaining: int64(toolSchemaBytes), value: 'A'},
		strings.NewReader(`"}],"input":[{"type":"reasoning","content":[{"type":"reasoning_text","encrypted_content":"opaque-reasoning-state"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`),
	)
}

type repeatedByteReader struct {
	remaining int64
	value     byte
}

func (reader *repeatedByteReader) Read(data []byte) (int, error) {
	if reader.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(data)) > reader.remaining {
		data = data[:reader.remaining]
	}
	for index := range data {
		data[index] = reader.value
	}
	reader.remaining -= int64(len(data))
	return len(data), nil
}
