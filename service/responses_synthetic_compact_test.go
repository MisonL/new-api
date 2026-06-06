package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
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

func openSyntheticCompactServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	safeName := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return '_'
	}, t.Name())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=private", safeName)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(io.Discard, "", 0), gormlogger.Config{
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.SyntheticCompactStateRecord{}))
	return db
}

func withoutSyntheticCompactTestDB(t *testing.T) {
	t.Helper()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = nil
}

func withSyntheticCompactTestRedis(t *testing.T, fn func()) {
	t.Helper()
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	server := miniredis.RunT(t)
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
	fn()
}

func withBrokenSyntheticCompactTestRedis(t *testing.T, fn func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{
		Addr:         addr,
		MaxRetries:   0,
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		_ = listener.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
	fn()
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

func TestNormalizeResponsesInputItemsDropsBlankTopLevelString(t *testing.T) {
	items := normalizeResponsesInputItems(common.RawMessage(`"   "`))

	require.Empty(t, items)
}

func TestBuildSyntheticCompactSummaryRequestUsesStoredSummaryAndClearsSyntheticPreviousID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

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
			{"type":"evil_call","call_id":"call_3","output":"must stay hidden"},
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
	require.NotContains(t, body, "must stay hidden")
}

func TestVisibleResponsesInputPartsIncludesCustomToolCallOutputContent(t *testing.T) {
	parts := visibleResponsesInputParts(common.RawMessage(`[
		{"type":"message","role":"assistant","content":[
			{"type":"custom_tool_call_output","call_id":"call_custom","output":"custom result"}
		]}
	]`))

	require.Equal(t, []string{"[assistant] call_id=call_custom\noutput=custom result"}, parts)
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

func TestSplitSyntheticCompactTextPartsReturnsNilForEmptyText(t *testing.T) {
	require.Nil(t, splitSyntheticCompactTextParts(""))
}

func TestSplitSyntheticCompactTextPartsHandlesExactBoundary(t *testing.T) {
	text := strings.Repeat("x", syntheticCompactTextPartMax)

	parts := splitSyntheticCompactTextParts(text)

	require.Len(t, parts, 1)
	require.Equal(t, text, parts[0])
}

func TestSplitSyntheticCompactTextPartsSplitsOneByteOver(t *testing.T) {
	text := strings.Repeat("x", syntheticCompactTextPartMax+1)

	parts := splitSyntheticCompactTextParts(text)

	require.Len(t, parts, 2)
	require.Len(t, parts[0], syntheticCompactTextPartMax)
	require.Len(t, parts[1], 1)
}

func TestSplitSyntheticCompactTextPartsHandlesInvalidUTF8(t *testing.T) {
	text := string(bytes.Repeat([]byte{0x80}, syntheticCompactTextPartMax+1))

	parts := splitSyntheticCompactTextParts(text)

	require.Len(t, parts, 2)
	for _, part := range parts {
		require.LessOrEqual(t, len(part), syntheticCompactTextPartMax)
	}
}

func TestApplySyntheticCompactStateInjectsSummaryAndRemovesMarker(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

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
	withoutSyntheticCompactTestDB(t)

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
	withoutSyntheticCompactTestDB(t)

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
	withoutSyntheticCompactTestDB(t)

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
	withoutSyntheticCompactTestDB(t)

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_newapi_synthcmp_missing",
		Input:              common.RawMessage(`"continue"`),
	}

	_, _, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestBuildSyntheticCompactSummaryRequestIgnoresDatabaseErrorForUpstreamPreviousID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	require.NoError(t, model.DB.Migrator().DropTable(&model.SyntheticCompactStateRecord{}))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_upstream_previous",
		Input:              common.RawMessage(`"continue"`),
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.Equal(t, "resp_upstream_previous", got.PreviousResponseID)
	require.Contains(t, string(got.Input), "existing previous_response_id context")
}

func TestBuildSyntheticCompactSummaryRequestIgnoresNonSyntheticMarkerID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	require.NoError(t, model.DB.Migrator().DropTable(&model.SyntheticCompactStateRecord{}))

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_upstream_previous"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.NotContains(t, string(got.Input), "resp_upstream_previous")
	require.Contains(t, string(got.Input), "Visible conversation to compact")
	require.Contains(t, string(got.Input), "[user] continue")
}

func TestApplySyntheticCompactStateRestoresFromDatabaseAfterMemoryMiss(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	state := SyntheticCompactState{
		ID:        "resp_newapi_synthcmp_db",
		Model:     "gpt-5",
		Summary:   "Database compact state.",
		UserID:    10,
		TokenID:   20,
		Group:     "default",
		CreatedAt: time.Now().Unix(),
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	var record model.SyntheticCompactStateRecord
	require.NoError(t, model.DB.Where("id = ?", state.ID).First(&record).Error)
	require.NotContains(t, string(record.SummaryCiphertext), state.Summary)
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: state.ID,
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue."}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, got.PreviousResponseID)
	require.Contains(t, string(got.Input), "Database compact state.")
	_, cached := syntheticCompactMemoryStore.Load(state.ID)
	require.True(t, cached)

	require.NoError(t, model.DB.Migrator().DropTable(&model.SyntheticCompactStateRecord{}))
	got, applied, err = ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}, req)
	require.NoError(t, err)
	require.True(t, applied)
	require.Contains(t, string(got.Input), "Database compact state.")
}

func TestStoreSyntheticCompactStatePopulatesMemoryWhenRedisEnabled(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_redis_memory",
			Model:   "gpt-5",
			Summary: "Redis-backed compact state.",
		}
		require.NoError(t, storeSyntheticCompactState(context.Background(), state))

		got, found := loadSyntheticCompactStateFromMemory(state.ID)

		require.True(t, found)
		require.Equal(t, state.Summary, got.Summary)
	})
}

func TestStoreSyntheticCompactStateEncryptsRedisValue(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_redis_encrypted",
			Model:   "gpt-5",
			Summary: "Redis value must not store this summary in plaintext.",
		}
		require.NoError(t, storeSyntheticCompactState(context.Background(), state))

		raw, err := common.RDB.Get(context.Background(), syntheticCompactRedisKey(state.ID)).Result()
		require.NoError(t, err)
		require.NotContains(t, raw, state.Summary)

		resetSyntheticCompactMemoryStoreForTest()
		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.Summary, got.Summary)
	})
}

func TestLoadSyntheticCompactStateKeepsRedisExpiryInMemory(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_redis_expiry",
			Model:   "gpt-5",
			Summary: "Redis expiry should be preserved in memory.",
		}
		data, err := common.Marshal(state)
		require.NoError(t, err)
		ttl := 2 * time.Minute
		require.NoError(t, common.RDB.Set(context.Background(), syntheticCompactRedisKey(state.ID), string(data), ttl).Err())

		_, found, err := loadSyntheticCompactState(context.Background(), state.ID)

		require.NoError(t, err)
		require.True(t, found)
		value, cached := syntheticCompactMemoryStore.Load(state.ID)
		require.True(t, cached)
		entry, ok := value.(syntheticCompactMemoryEntry)
		require.True(t, ok)
		require.True(t, entry.expiresAt.After(time.Now().Add(ttl-2*time.Second)))
		require.True(t, entry.expiresAt.Before(time.Now().Add(ttl+2*time.Second)))
	})
}

func TestLoadSyntheticCompactStateAcceptsLegacyPlainRedisValue(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_redis_legacy_plain",
			Model:   "gpt-5",
			Summary: "Legacy plain Redis state.",
		}
		data, err := common.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, common.RDB.Set(context.Background(), syntheticCompactRedisKey(state.ID), string(data), time.Minute).Err())

		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.Summary, got.Summary)
	})
}

func TestStoreSyntheticCompactStateReturnsDatabaseErrorEvenWhenRedisAvailable(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	require.NoError(t, model.DB.Migrator().DropTable(&model.SyntheticCompactStateRecord{}))

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_db_required",
			Model:   "gpt-5",
			Summary: "Database write must succeed.",
		}

		err := storeSyntheticCompactState(context.Background(), state)

		require.Error(t, err)
		require.Contains(t, err.Error(), "store synthetic compact state in database")
		_, found := loadSyntheticCompactStateFromMemory(state.ID)
		require.False(t, found)
	})
}

func TestStoreSyntheticCompactStateRejectsOversizedSummary(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_oversized",
		Model:   "gpt-5",
		Summary: strings.Repeat("x", syntheticCompactSummaryMax+1),
	}

	err := storeSyntheticCompactState(context.Background(), state)

	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds max size")
}

func TestStoreSyntheticCompactStateReturnsRedisErrorWhenDatabaseUnavailable(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = nil

	withBrokenSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_redis_no_db",
			Model:   "gpt-5",
			Summary: "Redis failure without database.",
		}

		err := storeSyntheticCompactState(context.Background(), state)

		require.Error(t, err)
		require.Contains(t, err.Error(), "store synthetic compact state in redis")
		_, found := loadSyntheticCompactStateFromMemory(state.ID)
		require.False(t, found)
	})
}

func TestStoreSyntheticCompactStateUsesDatabaseFallbackWhenRedisFails(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withBrokenSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_redis_db_fallback",
			Model:   "gpt-5",
			Summary: "Redis failure with database fallback.",
		}

		require.NoError(t, storeSyntheticCompactState(context.Background(), state))
		var count int64
		require.NoError(t, model.DB.Model(&model.SyntheticCompactStateRecord{}).Where("id = ?", state.ID).Count(&count).Error)
		require.EqualValues(t, 1, count)
		_, found := loadSyntheticCompactStateFromMemory(state.ID)
		require.True(t, found)
	})
}

func TestLoadSyntheticCompactStateReturnsCanceledContextBeforeDatabaseFallback(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_canceled_load",
		Model:   "gpt-5",
		Summary: "Stored state should not be loaded through canceled context.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	resetSyntheticCompactMemoryStoreForTest()

	withSyntheticCompactTestRedis(t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, found, err := loadSyntheticCompactState(ctx, state.ID)

		require.ErrorIs(t, err, context.Canceled)
		require.False(t, found)
		require.Nil(t, got)
	})
}

func TestLoadSyntheticCompactStateUsesDatabaseFallbackWhenRedisFails(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_load_redis_db_fallback",
		Model:   "gpt-5",
		Summary: "Redis load failure recovers through database.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	resetSyntheticCompactMemoryStoreForTest()

	withBrokenSyntheticCompactTestRedis(t, func() {
		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.Summary, got.Summary)
	})
}

func TestLoadSyntheticCompactStateReturnsNotFoundWhenRedisFailsAndDatabaseMisses(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withBrokenSyntheticCompactTestRedis(t, func() {
		got, found, err := loadSyntheticCompactState(context.Background(), "resp_newapi_synthcmp_missing")

		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, got)
	})
}

func TestLoadSyntheticCompactStateUsesDatabaseFallbackWhenRedisPayloadIsCorrupt(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_corrupt_redis",
			Model:   "gpt-5",
			Summary: "Database state survives corrupt Redis payload.",
		}
		require.NoError(t, storeSyntheticCompactState(context.Background(), state))
		resetSyntheticCompactMemoryStoreForTest()
		require.NoError(t, common.RDB.Set(context.Background(), syntheticCompactRedisKey(state.ID), "{", syntheticCompactTTL).Err())

		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.Summary, got.Summary)
	})
}

func TestApplySyntheticCompactStateReturnsDatabaseErrorForSyntheticPreviousID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	require.NoError(t, model.DB.Migrator().DropTable(&model.SyntheticCompactStateRecord{}))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_newapi_synthcmp_db_error",
		Input:              common.RawMessage(`"continue"`),
	}

	_, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.Error(t, err)
	require.Contains(t, err.Error(), "synthetic_compact_state_records")
	require.False(t, applied)
}

func TestSyntheticCompactDatabaseRecordUsesEncryptedSummary(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_encrypted",
		Model:   "gpt-5",
		Summary: "Sensitive compact summary.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	var record model.SyntheticCompactStateRecord
	require.NoError(t, model.DB.Where("id = ?", state.ID).First(&record).Error)
	require.NotEmpty(t, record.SummaryCiphertext)
	require.NotContains(t, string(record.SummaryCiphertext), state.Summary)

	resetSyntheticCompactMemoryStoreForTest()
	got, found, err := loadSyntheticCompactState(context.Background(), state.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, state.Summary, got.Summary)
}

func TestLoadSyntheticCompactStateKeepsDatabaseExpiryInMemory(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_db_expiry",
		Model:   "gpt-5",
		Summary: "Near expiry compact summary.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	expiresAt := time.Now().Add(time.Minute).Unix()
	require.NoError(t, model.DB.Model(&model.SyntheticCompactStateRecord{}).
		Where("id = ?", state.ID).
		Update("expires_at", expiresAt).Error)
	resetSyntheticCompactMemoryStoreForTest()

	_, found, err := loadSyntheticCompactState(context.Background(), state.ID)
	require.NoError(t, err)
	require.True(t, found)

	value, cached := syntheticCompactMemoryStore.Load(state.ID)
	require.True(t, cached)
	entry, ok := value.(syntheticCompactMemoryEntry)
	require.True(t, ok)
	require.Equal(t, time.Unix(expiresAt, 0), entry.expiresAt)
}

func TestSyntheticCompactDatabaseRecordRejectsTamperedScopeAAD(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_tampered_scope",
		Model:   "gpt-5",
		Summary: "Sensitive compact summary.",
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	resetSyntheticCompactMemoryStoreForTest()
	require.NoError(t, model.DB.Model(&model.SyntheticCompactStateRecord{}).
		Where("id = ?", state.ID).
		Update("user_id", 11).Error)

	_, found, err := loadSyntheticCompactState(context.Background(), state.ID)

	require.Error(t, err)
	require.False(t, found)
}

func TestSyntheticCompactDatabaseRecordRejectsWrongCryptoSecret(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	originSecret := common.CryptoSecret
	t.Cleanup(func() {
		model.DB = originDB
		common.CryptoSecret = originSecret
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	common.CryptoSecret = "synthetic-compact-test-secret-a"

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_wrong_secret",
		Model:   "gpt-5",
		Summary: "Sensitive compact summary.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	resetSyntheticCompactMemoryStoreForTest()

	common.CryptoSecret = "synthetic-compact-test-secret-b"
	_, found, err := loadSyntheticCompactState(context.Background(), state.ID)

	require.Error(t, err)
	require.False(t, found)
}

func TestApplySyntheticCompactStateRejectsExpiredDatabaseFallback(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	state := SyntheticCompactState{
		ID:        "resp_newapi_synthcmp_db_expired",
		Model:     "gpt-5",
		Summary:   "Expired database compact state.",
		CreatedAt: time.Now().Add(-2 * syntheticCompactTTL).Unix(),
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	require.NoError(t, model.DB.Model(&model.SyntheticCompactStateRecord{}).
		Where("id = ?", state.ID).
		Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}

	_, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.ErrorIs(t, err, ErrSyntheticCompactStateNotFound)
	require.False(t, applied)
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
	withoutSyntheticCompactTestDB(t)

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
	withoutSyntheticCompactTestDB(t)

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
	withoutSyntheticCompactTestDB(t)

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

func TestApplySyntheticCompactStateScopeRejectsMissingScopeBinding(t *testing.T) {
	cases := []struct {
		name    string
		state   SyntheticCompactState
		scope   SyntheticCompactStateScope
		wantErr string
	}{
		{
			name: "missing user",
			state: SyntheticCompactState{
				ID:      "resp_newapi_synthcmp_missing_user",
				Model:   "gpt-5",
				Summary: "Scoped compact state.",
				UserID:  10,
				TokenID: 20,
				Group:   "default",
			},
			scope:   SyntheticCompactStateScope{TokenID: 20, Group: "default"},
			wantErr: "different user",
		},
		{
			name: "missing token",
			state: SyntheticCompactState{
				ID:      "resp_newapi_synthcmp_missing_token",
				Model:   "gpt-5",
				Summary: "Scoped compact state.",
				UserID:  10,
				TokenID: 20,
				Group:   "default",
			},
			scope:   SyntheticCompactStateScope{UserID: 10, Group: "default"},
			wantErr: "different token",
		},
		{
			name: "missing group",
			state: SyntheticCompactState{
				ID:      "resp_newapi_synthcmp_missing_group",
				Model:   "gpt-5",
				Summary: "Scoped compact state.",
				UserID:  10,
				TokenID: 20,
				Group:   "default",
			},
			scope:   SyntheticCompactStateScope{UserID: 10, TokenID: 20},
			wantErr: "different group",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetSyntheticCompactMemoryStoreForTest()
			withoutSyntheticCompactTestDB(t)
			require.NoError(t, storeSyntheticCompactState(context.Background(), tc.state))

			req := dto.OpenAIResponsesRequest{
				Model:              "gpt-5",
				PreviousResponseID: tc.state.ID,
				Input:              common.RawMessage(`"continue"`),
			}

			_, applied, err := ApplySyntheticCompactState(context.Background(), tc.scope, req)

			require.Error(t, err)
			require.True(t, applied)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestBuildSyntheticCompactSummaryRequestScopeRejectsDifferentModel(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

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
	withoutSyntheticCompactTestDB(t)

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

func TestBuildSyntheticCompactResponseRequiresScope(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	resp := dto.OpenAIResponsesResponse{
		ID:        "resp_upstream",
		Object:    "response",
		CreatedAt: 1710000000,
		Model:     "gpt-5",
		Output: []dto.ResponsesOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "Synthetic summary text."},
				},
			},
		},
	}

	_, _, err := BuildSyntheticCompactResponse(context.Background(), SyntheticCompactStateScope{}, "gpt-5", resp)

	require.ErrorIs(t, err, ErrSyntheticCompactStateScopeRequired)
}

func TestLoadSyntheticCompactStateDeletesExpiredMemoryEntry(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

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

func TestSyntheticCompactMemoryStoreEnforcesEntryLimit(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	t.Cleanup(resetSyntheticCompactMemoryStoreForTest)

	for i := 0; i < syntheticCompactMemoryEntriesMax+1; i++ {
		id := fmt.Sprintf("resp_newapi_synthcmp_memory_%d", i)
		syntheticCompactMemoryStore.Store(id, syntheticCompactMemoryEntry{
			state:     SyntheticCompactState{ID: id, Model: "gpt-5", Summary: "summary"},
			expiresAt: time.Now().Add(time.Hour),
		})
	}

	var count int
	syntheticCompactMemoryStore.Range(func(_, _ any) bool {
		count++
		return true
	})
	require.Equal(t, syntheticCompactMemoryEntriesMax, count)
	// With no reads or updates, the bounded store evicts by insertion order.
	_, oldestStored := syntheticCompactMemoryStore.Load("resp_newapi_synthcmp_memory_0")
	require.False(t, oldestStored)
	_, newestStored := syntheticCompactMemoryStore.Load(fmt.Sprintf("resp_newapi_synthcmp_memory_%d", syntheticCompactMemoryEntriesMax))
	require.True(t, newestStored)
}

func TestSyntheticCompactMemoryStoreConcurrentWritesEnforceEntryLimit(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	t.Cleanup(resetSyntheticCompactMemoryStoreForTest)

	var wg sync.WaitGroup
	for i := 0; i < syntheticCompactMemoryEntriesMax+32; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("resp_newapi_synthcmp_memory_concurrent_%d", i)
			syntheticCompactMemoryStore.Store(id, syntheticCompactMemoryEntry{
				state:     SyntheticCompactState{ID: id, Model: "gpt-5", Summary: "summary"},
				expiresAt: time.Now().Add(time.Hour),
			})
		}()
	}
	wg.Wait()

	var count int
	syntheticCompactMemoryStore.Range(func(_, _ any) bool {
		count++
		return true
	})
	require.Equal(t, syntheticCompactMemoryEntriesMax, count)
}
