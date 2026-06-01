package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

type syntheticCompactTestContent struct {
	Text string `json:"text"`
}

type syntheticCompactTestMessage struct {
	Role    string                        `json:"role"`
	Content []syntheticCompactTestContent `json:"content"`
}

func decodeSyntheticCompactTestMessages(t *testing.T, input common.RawMessage) []syntheticCompactTestMessage {
	t.Helper()
	var items []syntheticCompactTestMessage
	require.NoError(t, common.Unmarshal(input, &items))
	return items
}

func TestBuildSyntheticCompactSummaryRequestRejectsOpaqueOnlyInput(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[{"type":"compaction","encrypted_content":"opaque"}]`),
	}

	_, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "visible input")
}

func TestBuildSyntheticCompactSummaryRequestUsesStoredSummaryAndClearsSyntheticPreviousID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_prev",
		Model:   "gpt-5",
		Summary: "Prior synthetic summary.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: state.ID,
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue the task."}]}
		]`),
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.Empty(t, got.PreviousResponseID)
	require.JSONEq(t, `[
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.\nInclude:\n- Current progress and key decisions made\n- Important context, constraints, or user preferences\n- What remains to be done (clear next steps)\n- Any critical data, examples, or references needed to continue\nBe concise, structured, and focused on helping the next LLM seamlessly continue the work.\nDo not invent facts. Return only the compact summary text."}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"Previous synthetic summary:\nPrior synthetic summary.\n\nVisible conversation to compact:\n[user] Continue the task."}]}
	]`, string(got.Input))
}

func TestBuildSyntheticCompactSummaryRequestPreservesUpstreamPreviousID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_previous",
		Input: common.RawMessage(`[
			{"type":"function_call_output","call_id":"call_1","output":"{\"path\":\"/tmp/result.txt\",\"ok\":true}"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue the task."}]}
		]`),
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.Equal(t, "resp_previous", got.PreviousResponseID)
	body := string(got.Input)
	require.Contains(t, body, "Use the existing previous_response_id context as the source of truth for the compaction.")
	require.Contains(t, body, "CONTEXT CHECKPOINT COMPACTION")
	require.Contains(t, body, "What remains to be done")
	require.NotContains(t, body, "Visible conversation to compact")
	require.NotContains(t, body, "/tmp/result.txt")
	require.NotContains(t, body, "Continue the task.")
}

func TestBuildSyntheticCompactSummaryRequestDoesNotReplayLongInputWithUpstreamPreviousID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	longText := strings.Repeat("long visible context ", 4096)
	input, err := common.Marshal([]map[string]any{
		{
			"type": "message",
			"role": "user",
			"content": []map[string]string{
				{"type": "input_text", "text": longText},
			},
		},
	})
	require.NoError(t, err)
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_previous",
		Input:              input,
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.Equal(t, "resp_previous", got.PreviousResponseID)
	require.NotContains(t, string(got.Input), "long visible context")
}

func TestBuildSyntheticCompactSummaryRequestIncludesToolCallMetadata(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"function_call","call_id":"call_1","name":"edit_file","arguments":"{\"path\":\"/tmp/result.txt\"}"},
			{"type":"custom_tool_call","call_id":"call_2","name":"shell","input":"go test ./service"},
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"Continue after tools."},
				{"type":"function_call_output","call_id":"call_1","output":"ok"}
			]}
		]`),
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	body := string(got.Input)
	require.Contains(t, body, "name=edit_file")
	require.Contains(t, body, "call_id=call_1")
	require.Contains(t, body, "arguments={\\\"path\\\":\\\"/tmp/result.txt\\\"}")
	require.Contains(t, body, "name=shell")
	require.Contains(t, body, "input=go test ./service")
	require.Contains(t, body, "output=ok")
}

func TestBuildSyntheticCompactSummaryRequestHandlesMixedInputArrayItems(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			"Standalone string input.",
			42,
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Object message input."}]}
		]`),
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	body := string(got.Input)
	require.Contains(t, body, "[input] Standalone string input.")
	require.Contains(t, body, "[user] Object message input.")
	require.NotContains(t, body, "requires visible input")
}

func TestBuildSyntheticCompactSummaryRequestSplitsLargeVisibleInput(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	longText := strings.Repeat("界", syntheticCompactTextPartMax/3+1024)
	input, err := common.Marshal([]map[string]any{
		{
			"type": "message",
			"role": "user",
			"content": []map[string]string{
				{"type": "input_text", "text": longText},
			},
		},
	})
	require.NoError(t, err)
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: input,
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	var items []struct {
		Role    string `json:"role"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	require.NoError(t, common.Unmarshal(got.Input, &items))
	require.Len(t, items, 2)
	require.Equal(t, "user", items[1].Role)
	require.Greater(t, len(items[1].Content), 1)
	for _, part := range items[1].Content {
		require.LessOrEqual(t, len(part.Text), syntheticCompactTextPartMax)
		require.True(t, strings.Contains(part.Text, "界"))
	}
}

func TestApplySyntheticCompactStateInjectsSummaryAndRemovesMarker(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_prev",
		Model:   "gpt-5",
		Summary: "Stored compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: state.ID,
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_prev"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"What is next?"}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, got.PreviousResponseID)
	items := decodeSyntheticCompactTestMessages(t, got.Input)
	require.Len(t, items, 2)
	require.Equal(t, "developer", items[0].Role)
	require.Len(t, items[0].Content, 1)
	require.Contains(t, items[0].Content[0].Text, "Stored compact state.")
	require.Contains(t, items[0].Content[0].Text, "If post-compact input is only repeated setup")
	require.Equal(t, "user", items[1].Role)
	require.Len(t, items[1].Content, 1)
	require.Equal(t, "What is next?", items[1].Content[0].Text)
}

func TestApplySyntheticCompactStateUsesHandoffPromptAfterRepeatedSetup(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_prev",
		Model:   "gpt-5",
		Summary: "Current pending user request: diagnose why synthetic compact caused memory loss.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: state.ID,
		Input: common.RawMessage(`[
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"AGENTS.md instructions for /Volumes/Work/code/new-api"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"收到。我会按 project-doc 中的 new-api 仓库规则执行。"}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.True(t, applied)
	body := string(got.Input)
	require.Contains(t, body, "Another language model started to solve this problem")
	require.Contains(t, body, "avoid duplicating work")
	require.Contains(t, body, "Current pending user request: diagnose why synthetic compact caused memory loss.")
	require.Contains(t, body, "If post-compact input is only repeated setup or repository instructions from the client")
	require.Contains(t, body, "continue the latest pending task from the summary")
	require.Contains(t, body, "AGENTS.md instructions")
	require.Less(t, strings.Index(body, "If post-compact input is only repeated setup"), strings.Index(body, "AGENTS.md instructions"))
}

func TestApplySyntheticCompactStateClearsNonSyntheticPreviousID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_prev",
		Model:   "gpt-5",
		Summary: "Stored compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_upstream_previous",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_prev"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"What is next?"}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, got.PreviousResponseID)
	require.Contains(t, string(got.Input), "Stored compact state.")
}

func TestApplySyntheticCompactStateHandlesMixedInputArrayItems(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_prev",
		Model:   "gpt-5",
		Summary: "Stored compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			"Continue from this string.",
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_prev"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"And this object."}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, got.PreviousResponseID)
	items := decodeSyntheticCompactTestMessages(t, got.Input)
	require.Len(t, items, 3)
	require.Equal(t, "developer", items[0].Role)
	require.Len(t, items[0].Content, 1)
	require.Contains(t, items[0].Content[0].Text, "Stored compact state.")
	require.Contains(t, items[0].Content[0].Text, "If post-compact input is only repeated setup")
	require.Equal(t, "user", items[1].Role)
	require.Len(t, items[1].Content, 1)
	require.Equal(t, "Continue from this string.", items[1].Content[0].Text)
	require.Equal(t, "user", items[2].Role)
	require.Len(t, items[2].Content, 1)
	require.Equal(t, "And this object.", items[2].Content[0].Text)
}

func TestApplySyntheticCompactStateRejectsMissingReferencedState(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_newapi_synthcmp_missing",
		Input:              common.RawMessage(`"continue"`),
	}

	_, _, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestApplySyntheticCompactStateRejectsMultipleMarkers(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_one"},
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_two"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	_, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.ErrorIs(t, err, ErrSyntheticCompactMultipleMarkers)
	require.False(t, applied)
}

func TestApplySyntheticCompactStateScopeAllowsDifferentChannel(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:          "resp_newapi_synthcmp_scoped",
		Model:       "gpt-5.5-openai-compact",
		Summary:     "Scoped compact state.",
		UserID:      10,
		TokenID:     20,
		Group:       "default",
		ChannelID:   163,
		ChannelType: 1,
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	scope := SyntheticCompactStateScope{
		UserID:      10,
		TokenID:     20,
		Group:       "default",
		ChannelID:   164,
		ChannelType: 1,
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), scope, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, got.PreviousResponseID)
	require.Contains(t, string(got.Input), "Scoped compact state.")
}

func TestApplySyntheticCompactStateScopeRejectsDifferentToken(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_token",
		Model:   "gpt-5",
		Summary: "Scoped compact state.",
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	scope := SyntheticCompactStateScope{UserID: 10, TokenID: 21, Group: "default"}

	_, applied, err := ApplySyntheticCompactState(context.Background(), scope, req)

	require.Error(t, err)
	require.True(t, applied)
	require.Contains(t, err.Error(), "different token")
}

func TestApplySyntheticCompactStateScopeRejectsDifferentGroup(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_group",
		Model:   "gpt-5",
		Summary: "Scoped compact state.",
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	scope := SyntheticCompactStateScope{UserID: 10, TokenID: 20, Group: "premium"}

	_, applied, err := ApplySyntheticCompactState(context.Background(), scope, req)

	require.Error(t, err)
	require.True(t, applied)
	require.Contains(t, err.Error(), "different group")
}

func TestBuildSyntheticCompactSummaryRequestScopeRejectsDifferentModel(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_model",
		Model:   "gpt-5",
		Summary: "Scoped compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-4.1",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}

	_, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "different model")
}

func TestBuildSyntheticCompactResponseStoresSummaryAndReturnsMarker(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	resp := dto.OpenAIResponsesResponse{
		ID:        "resp_upstream",
		Object:    "response",
		CreatedAt: 1710000000,
		Model:     "gpt-5",
		Output: []dto.ResponsesOutput{
			{
				Type:   "message",
				Role:   "assistant",
				Status: "completed",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "Synthetic summary text."},
				},
			},
		},
		Usage: &dto.Usage{InputTokens: 10, OutputTokens: 4, TotalTokens: 14},
	}

	scope := SyntheticCompactStateScope{
		UserID:      10,
		TokenID:     20,
		Group:       "default",
		ChannelID:   163,
		ChannelType: 1,
	}
	compactResp, usage, err := BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5", resp)

	require.NoError(t, err)
	require.Equal(t, "response", compactResp.Object)
	require.Equal(t, 10, usage.PromptTokens)
	require.Equal(t, 4, usage.CompletionTokens)
	require.JSONEq(t, `[
		{"type":"compaction","encrypted_content":"newapi.synthetic.compact:`+compactResp.ID+`"}
	]`, string(compactResp.Output))

	state, found, err := loadSyntheticCompactState(context.Background(), compactResp.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "Synthetic summary text.", state.Summary)
	require.Equal(t, "gpt-5", state.Model)
	require.Equal(t, 10, state.UserID)
	require.Equal(t, 20, state.TokenID)
	require.Equal(t, "default", state.Group)
	require.Equal(t, 163, state.ChannelID)
	require.Equal(t, 1, state.ChannelType)
}

func TestLoadSyntheticCompactStateDeletesExpiredMemoryEntry(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_expired",
		Model:   "gpt-5",
		Summary: "Expired compact state.",
	}
	syntheticCompactMemoryStore.Store(state.ID, syntheticCompactMemoryEntry{
		state:     state,
		expiresAt: time.Now().Add(-time.Minute),
	})

	got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, got)
	_, stillStored := syntheticCompactMemoryStore.Load(state.ID)
	require.False(t, stillStored)
}

func TestPruneExpiredSyntheticCompactMemoryKeepsValidEntry(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	expired := SyntheticCompactState{ID: "resp_newapi_synthcmp_expired", Model: "gpt-5", Summary: "Expired."}
	valid := SyntheticCompactState{ID: "resp_newapi_synthcmp_valid", Model: "gpt-5", Summary: "Valid."}
	now := time.Now()
	syntheticCompactMemoryStore.Store(expired.ID, syntheticCompactMemoryEntry{state: expired, expiresAt: now.Add(-time.Minute)})
	syntheticCompactMemoryStore.Store(valid.ID, syntheticCompactMemoryEntry{state: valid, expiresAt: now.Add(time.Minute)})

	pruneExpiredSyntheticCompactMemory(now)

	_, expiredStored := syntheticCompactMemoryStore.Load(expired.ID)
	_, validStored := syntheticCompactMemoryStore.Load(valid.ID)
	require.False(t, expiredStored)
	require.True(t, validStored)
}
