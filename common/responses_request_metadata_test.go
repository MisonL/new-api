package common

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanResponsesRequestMetadataCapturesFinalScalarFieldsAndMergesOpaqueInputState(t *testing.T) {
	body := []byte(`{
		"model":"first-model",
		"group":"first-group",
		"stream":false,
		"max_output_tokens":1,
		"previous_response_id":"resp_first",
		"input":[{"type":"reasoning","encrypted_content":"old-state"}],
		"tools":[{"type":"function","name":"old_tool"}],
		"model":"gpt-5.6-sol",
		"group":"priority-group",
		"stream":true,
		"max_output_tokens":4096,
		"previous_response_id":"resp_latest",
		"input":[
			{"type":"reasoning","encrypted_content":"  opaque\u002dstate  "},
			{"type":"message","content":[{"type":"input_text","text":"continue"}]}
		],
		"tools":{"future_tool_shape":true}
	}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", metadata.Model)
	require.Equal(t, "priority-group", metadata.Group)
	require.NotNil(t, metadata.Stream)
	require.True(t, *metadata.Stream)
	require.NotNil(t, metadata.MaxOutputTokens)
	require.Equal(t, uint(4096), *metadata.MaxOutputTokens)
	require.Equal(t, "resp_latest", metadata.PreviousResponseID)
	require.True(t, metadata.InputPresent)
	require.Equal(t, ResponsesJSONValueKindArray, metadata.InputKind)
	require.True(t, metadata.ToolsPresent)
	require.Equal(t, ResponsesJSONValueKindObject, metadata.ToolsKind)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Equal(t, []string{
		responsesMetadataFingerprint("old-state"),
		responsesMetadataFingerprint("opaque-state"),
	}, metadata.EncryptedContentFingerprints)
	require.Equal(t, int64(len(body)), metadata.BodySize)
	require.Equal(t, []byte(`"gpt-5.6-sol"`), body[metadata.ModelLocation.Start:metadata.ModelLocation.End])
}

func TestScanResponsesRequestMetadataRetainsOnlyFinalRequestShapeFields(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{"type":"additional_tools","tools":[{"type":"function","name":"first_secret"}]}],
		"tools":[{"type":"function","name":"first_top_level_secret"}],
		"reasoning":{"context":"all_turns","mode":"responses"},
		"include":["reasoning.encrypted_content"],
		"tool_choice":{"type":"function","name":"first_choice_secret"},
		"parallel_tool_calls":true,
		"store":true,
		"input":[{"type":"message","content":"final secret"}],
		"tools":null,
		"reasoning":null,
		"include":null,
		"tool_choice":null,
		"parallel_tool_calls":null,
		"store":null
	}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.Equal(t, ResponsesJSONValueKindArray, metadata.InputKind)
	require.True(t, metadata.Shape.InputItemCountKnown)
	require.Equal(t, 1, metadata.Shape.InputItemCount)
	require.Equal(t, map[string]int{"message": 1}, metadata.Shape.InputItemTypes)
	require.False(t, metadata.Shape.HasAdditionalTools)
	require.False(t, metadata.Shape.AdditionalToolCountKnown)
	require.Empty(t, metadata.Shape.AdditionalToolTypes)
	require.Equal(t, ResponsesJSONValueKindNull, metadata.ToolsKind)
	require.False(t, metadata.Shape.TopLevelToolCountKnown)
	require.Empty(t, metadata.Shape.TopLevelToolTypes)
	require.False(t, metadata.Shape.HasReasoning)
	require.False(t, metadata.Shape.ReasoningDetailsKnown)
	require.False(t, metadata.Shape.HasReasoningContext)
	require.False(t, metadata.Shape.HasReasoningMode)
	require.False(t, metadata.Shape.HasInclude)
	require.False(t, metadata.Shape.HasToolChoice)
	require.False(t, metadata.Shape.HasParallelToolCalls)
	require.False(t, metadata.Shape.HasStore)
}

func TestScanResponsesRequestMetadataRecordsBoundedOpaqueRequestShape(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"stream":false,
		"reasoning":{"context":"all_turns","mode":"responses","secret":"reasoning secret"},
		"include":["reasoning.encrypted_content"],
		"tool_choice":{"type":"function","name":"choice secret"},
		"parallel_tool_calls":true,
		"store":false,
		"tools":[
			{"type":"function","name":"top level secret"},
			{"type":"custom","name":"top level custom secret","format":{"type":"grammar","syntax":"lark","definition":"grammar secret"}}
		],
		"input":[
			{"type":"additional_tools","tools":[
				{"type":"custom","name":"additional custom secret","format":{"type":"grammar","syntax":"lark","definition":"additional grammar secret"}},
				{"type":"namespace","name":"namespace secret"},
				{"type":"tool_search","name":"search secret"}
			]},
			{"type":"reasoning","encrypted_content":"opaque encrypted state"},
			{"type":"function_call","name":"call secret","arguments":"arguments secret"},
			{"type":"compaction_trigger"},
			{"type":"message","content":"prompt secret"}
		]
	}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.Equal(t, int64(len(body)), metadata.BodySize)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.True(t, metadata.Shape.InputItemCountKnown)
	require.Equal(t, 5, metadata.Shape.InputItemCount)
	require.Equal(t, map[string]int{
		"additional_tools":   1,
		"compaction_trigger": 1,
		"function_call":      1,
		"message":            1,
		"reasoning":          1,
	}, metadata.Shape.InputItemTypes)
	require.True(t, metadata.Shape.HasAdditionalTools)
	require.True(t, metadata.Shape.AdditionalToolCountKnown)
	require.Equal(t, 3, metadata.Shape.AdditionalToolCount)
	require.Equal(t, map[string]int{
		"custom":      1,
		"namespace":   1,
		"tool_search": 1,
	}, metadata.Shape.AdditionalToolTypes)
	require.Equal(t, map[string]int{"grammar": 1}, metadata.Shape.AdditionalCustomFormatTypes)
	require.Equal(t, map[string]int{"lark": 1}, metadata.Shape.AdditionalCustomGrammarSyntaxes)
	require.True(t, metadata.Shape.TopLevelToolCountKnown)
	require.Equal(t, 2, metadata.Shape.TopLevelToolCount)
	require.Equal(t, map[string]int{"custom": 1, "function": 1}, metadata.Shape.TopLevelToolTypes)
	require.Equal(t, map[string]int{"grammar": 1}, metadata.Shape.TopLevelCustomFormatTypes)
	require.Equal(t, map[string]int{"lark": 1}, metadata.Shape.TopLevelCustomGrammarSyntaxes)
	require.True(t, metadata.Shape.HasCompactionTrigger)
	require.True(t, metadata.Shape.HasReasoning)
	require.True(t, metadata.Shape.ReasoningDetailsKnown)
	require.True(t, metadata.Shape.HasReasoningContext)
	require.True(t, metadata.Shape.HasReasoningMode)
	require.True(t, metadata.Shape.HasInclude)
	require.True(t, metadata.Shape.HasToolChoice)
	require.True(t, metadata.Shape.HasParallelToolCalls)
	require.True(t, metadata.Shape.HasStore)

	encoded, marshalErr := Marshal(metadata)
	require.NoError(t, marshalErr)
	for _, secret := range []string{
		"reasoning secret",
		"choice secret",
		"top level secret",
		"top level custom secret",
		"grammar secret",
		"additional custom secret",
		"additional grammar secret",
		"namespace secret",
		"search secret",
		"opaque encrypted state",
		"call secret",
		"arguments secret",
		"prompt secret",
	} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestScanResponsesRequestMetadataUsesFinalAdditionalToolsField(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{
			"type":"additional_tools",
			"tools":[{"type":"function","name":"first_secret"}],
			"tools":[{"type":"custom","name":"final_secret","format":{"type":"grammar","syntax":"lark"}}]
		}]
	}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.Shape.HasAdditionalTools)
	require.True(t, metadata.Shape.AdditionalToolCountKnown)
	require.Equal(t, 1, metadata.Shape.AdditionalToolCount)
	require.Equal(t, map[string]int{"custom": 1}, metadata.Shape.AdditionalToolTypes)
	require.Equal(t, map[string]int{"grammar": 1}, metadata.Shape.AdditionalCustomFormatTypes)
	require.Equal(t, map[string]int{"lark": 1}, metadata.Shape.AdditionalCustomGrammarSyntaxes)
}

func TestScanResponsesRequestMetadataDetectsNestedAndNonStringEncryptedContent(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "nested encrypted content",
			body: `{"model":"gpt-5.6-sol","input":{"content":[{"encrypted_content":"nested-state"}]}}`,
		},
		{
			name: "non string encrypted content",
			body: `{"model":"gpt-5.6-sol","input":[{"type":"compaction","encrypted_content":{"version":2}}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			metadata, err := ScanResponsesRequestMetadata(strings.NewReader(tc.body), int64(len(tc.body)))

			require.NoError(t, err)
			require.True(t, metadata.HasOpaqueEncryptedContent)
		})
	}
}

func TestScanResponsesRequestMetadataRetainsOpaqueStateAcrossDuplicateEncryptedContentFields(t *testing.T) {
	const firstEncryptedContent = "first-opaque-state"
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[{
			"type":"reasoning",
			"encrypted_content":"` + firstEncryptedContent + `",
			"encrypted_content":null
		}],
		"input":[]
	}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Equal(t, []string{responsesMetadataFingerprint(firstEncryptedContent)}, metadata.EncryptedContentFingerprints)
	require.Equal(t, ResponsesJSONValueKindArray, metadata.InputKind)
}

func TestScanResponsesRequestMetadataMarksCompactionItemTypes(t *testing.T) {
	itemTypes := []string{
		"compaction",
		"context_compaction",
		"compaction_summary",
	}

	for _, itemType := range itemTypes {
		t.Run(itemType, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"` + itemType + `","encrypted_content":"opaque-state"}]}`)

			metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

			require.NoError(t, err)
			require.True(t, metadata.HasOpaqueEncryptedContent)
			require.True(t, metadata.HasCompactionItem)
		})
	}
}

func TestScanResponsesRequestMetadataDoesNotTreatNestedToolPayloadAsCompactionItem(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"function_call_output","call_id":"call_1","output":{"type":"compaction","encrypted_content":"nested-tool-state"}}]}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.False(t, metadata.HasCompactionItem)
}

func TestScanResponsesRequestMetadataDoesNotTreatNestedInputArrayAsDirectItems(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[[{"type":"compaction","encrypted_content":"nested-state"}]]}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.False(t, metadata.HasCompactionItem)
	require.False(t, metadata.Shape.InputItemCountKnown)
	require.Empty(t, metadata.Shape.InputItemTypes)
}

func TestScanResponsesRequestMetadataRejectsInvalidContractAndRestoresSource(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed JSON", body: `{"model":"gpt-5.6-sol","input":[}`},
		{name: "non string model", body: `{"model":7,"input":[]}`},
		{name: "non boolean stream", body: `{"model":"gpt-5.6-sol","stream":"true","input":[]}`},
		{name: "fractional max output tokens", body: `{"model":"gpt-5.6-sol","max_output_tokens":1.5,"input":[]}`},
		{name: "leading zero max output tokens", body: `{"model":"gpt-5.6-sol","max_output_tokens":01,"input":[]}`},
		{name: "trailing content", body: `{"model":"gpt-5.6-sol","input":[]} trailing`},
		{name: "nested trailing comma", body: `{"model":"gpt-5.6-sol","input":[],"future":{"nested":true,}}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := bytes.NewReader([]byte(tc.body))
			_, err := source.Seek(3, 0)
			require.NoError(t, err)

			_, err = ScanResponsesRequestMetadata(source, int64(len(tc.body)))

			require.Error(t, err)
			position, seekErr := source.Seek(0, 1)
			require.NoError(t, seekErr)
			require.Zero(t, position)
		})
	}
}

func TestScanResponsesRequestMetadataFingerprintsNestedToolOutputEncryptedContent(t *testing.T) {
	const encryptedContent = "nested-tool-output-state"
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"function_call_output","call_id":"call_1","output":[{"type":"encrypted_content","encrypted_content":"` + encryptedContent + `"}]}]}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Equal(t, []string{responsesMetadataFingerprint(encryptedContent)}, metadata.EncryptedContentFingerprints)
}

func TestScanResponsesRequestMetadataFingerprintsMultipleNestedEncryptedContents(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"function_call_output","output":[{"type":"encrypted_content","encrypted_content":"tool-output-state"},{"type":"encrypted_content","encrypted_content":"tool-result-state"}],"metadata":{"encrypted_content":"nested-metadata-state"}}]}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Equal(t, []string{
		responsesMetadataFingerprint("tool-output-state"),
		responsesMetadataFingerprint("tool-result-state"),
		responsesMetadataFingerprint("nested-metadata-state"),
	}, metadata.EncryptedContentFingerprints)
}

func TestScanResponsesRequestMetadataStreamsLargeEncryptedString(t *testing.T) {
	encryptedContent := strings.Repeat("x", 2<<20)
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"` + encryptedContent + `"}]}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Len(t, metadata.EncryptedContentFingerprints, 1)
	require.Equal(t, responsesMetadataFingerprint(encryptedContent), metadata.EncryptedContentFingerprints[0])
}

func TestScanResponsesRequestMetadataDoesNotTreatWhitespaceOnlyEncryptedContentAsOpaque(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":" \t\n "}]}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.False(t, metadata.HasOpaqueEncryptedContent)
	require.Empty(t, metadata.EncryptedContentFingerprints)
}

func TestScanResponsesRequestMetadataLeavesFingerprintEmptyWhenTrailingWhitespaceExceedsBound(t *testing.T) {
	encryptedContent := "opaque-state" + strings.Repeat(" ", maxTopLevelJSONStringCaptureBytes+maxResponsesMetadataTrailingWhitespaceBytes+1)
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"` + encryptedContent + `"}]}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Empty(t, metadata.EncryptedContentFingerprints)
}

func TestScanResponsesRequestMetadataRejectsBodySizeMismatch(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[]}`)

	_, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)-1))

	require.Error(t, err)
}

func TestScanResponsesRequestMetadataAppliesNestingLimitWhenSkippingKnownField(t *testing.T) {
	var body strings.Builder
	body.WriteString(`{"model":"gpt-5.6-sol","input":`)
	for range 495 {
		body.WriteString(`{"nested":`)
	}
	body.WriteString(`{"type":`)
	body.WriteString(strings.Repeat("[", 10))
	body.WriteString("null")
	body.WriteString(strings.Repeat("]", 10))
	body.WriteByte('}')
	for range 495 {
		body.WriteByte('}')
	}
	body.WriteByte('}')

	_, err := ScanResponsesRequestMetadata(strings.NewReader(body.String()), int64(body.Len()))

	require.Error(t, err)
	require.Contains(t, err.Error(), "nesting depth")
}

func TestScanResponsesRequestMetadataAcceptsJSONSlashEscapesInEncryptedContent(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"reasoning","encrypted_content":"https:\/\/example.test\/opaque-state"}]}`)

	metadata, err := ScanResponsesRequestMetadata(bytes.NewReader(body), int64(len(body)))

	require.NoError(t, err)
	require.True(t, metadata.HasOpaqueEncryptedContent)
	require.Equal(
		t,
		[]string{responsesMetadataFingerprint("https://example.test/opaque-state")},
		metadata.EncryptedContentFingerprints,
	)
}

func TestScanResponsesRequestMetadataBoundsEncryptedContentFingerprints(t *testing.T) {
	const inputItems = 40
	var body strings.Builder
	body.WriteString(`{"model":"gpt-5.6-sol","input":[`)
	for index := 0; index < inputItems; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{"type":"reasoning","encrypted_content":"state-`)
		body.WriteString(strconv.Itoa(index))
		body.WriteString(`"}`)
	}
	body.WriteString(`]}`)

	metadata, err := ScanResponsesRequestMetadata(strings.NewReader(body.String()), int64(body.Len()))

	require.NoError(t, err)
	require.Len(t, metadata.EncryptedContentFingerprints, maxResponsesMetadataEncryptedContentFingerprints)
	require.True(t, metadata.EncryptedContentFingerprintsTruncated)
	require.Equal(t, responsesMetadataFingerprint("state-0"), metadata.EncryptedContentFingerprints[0])
	require.Equal(
		t,
		responsesMetadataFingerprint("state-"+strconv.Itoa(maxResponsesMetadataEncryptedContentFingerprints-1)),
		metadata.EncryptedContentFingerprints[len(metadata.EncryptedContentFingerprints)-1],
	)
}
