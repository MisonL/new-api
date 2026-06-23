package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
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

type redisCommandCountHook struct {
	mu     sync.Mutex
	counts map[string]int
}

func newRedisCommandCountHook() *redisCommandCountHook {
	return &redisCommandCountHook{counts: make(map[string]int)}
}

func encryptSyntheticCompactSummaryForTestWithAAD(t *testing.T, summary string, aad []byte) string {
	t.Helper()
	block, err := aes.NewCipher(syntheticCompactSummaryKey())
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, gcm.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)
	sealed := gcm.Seal(nonce, nonce, []byte(summary), aad)
	return base64.RawURLEncoding.EncodeToString(sealed)
}

func (hook *redisCommandCountHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	hook.mu.Lock()
	hook.counts[strings.ToLower(cmd.Name())]++
	hook.mu.Unlock()
	return ctx, nil
}

func (hook *redisCommandCountHook) AfterProcess(context.Context, redis.Cmder) error {
	return nil
}

func (hook *redisCommandCountHook) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	hook.mu.Lock()
	for _, cmd := range cmds {
		hook.counts[strings.ToLower(cmd.Name())]++
	}
	hook.mu.Unlock()
	return ctx, nil
}

func (hook *redisCommandCountHook) AfterProcessPipeline(context.Context, []redis.Cmder) error {
	return nil
}

func (hook *redisCommandCountHook) Count(name string) int {
	hook.mu.Lock()
	defer hook.mu.Unlock()
	return hook.counts[strings.ToLower(name)]
}

func requireEventuallySyntheticCompactRecord(t *testing.T, id string) model.SyntheticCompactStateRecord {
	t.Helper()
	var record model.SyntheticCompactStateRecord
	require.Eventually(t, func() bool {
		err := model.DB.Where("id = ?", id).First(&record).Error
		return err == nil
	}, time.Second, 10*time.Millisecond)
	return record
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
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.SyntheticCompactStateRecord{}))
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

func withoutRedisForSyntheticCompactTest(t *testing.T) {
	t.Helper()
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = false
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})
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
	items := decodeSyntheticCompactTestMessages(t, got.Input)
	require.Len(t, items, 2)
	require.Equal(t, "developer", items[0].Role)
	require.Equal(t, "user", items[1].Role)
	require.Contains(t, items[0].Content[0].Text, "CONTEXT CHECKPOINT COMPACTION")
	require.Contains(t, items[0].Content[0].Text, "Current task:")
	require.Contains(t, items[0].Content[0].Text, "Remaining work:")
	require.Contains(t, items[0].Content[0].Text, "Do not output an acknowledgement")
	require.Contains(t, items[1].Content[0].Text, "Previous synthetic summary:\nPrior synthetic summary.")
	require.Contains(t, items[1].Content[0].Text, "Visible conversation to compact:\n[user] Continue the task.")
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
	require.Contains(t, body, "Visible conversation to compact")
	require.Contains(t, body, "/tmp/result.txt")
	require.Contains(t, body, "Continue the task.")
	require.Contains(t, body, "CONTEXT CHECKPOINT COMPACTION")
	require.Contains(t, body, "Remaining work")
}

func TestBuildSyntheticCompactSummaryRequestLimitsLongInputWithUpstreamPreviousID(t *testing.T) {
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
	body := string(got.Input)
	require.Contains(t, body, "Visible conversation to compact")
	require.Contains(t, body, "[truncated earlier visible input]")
	require.Contains(t, body, "long visible context")
	require.Less(t, len(body), 45000)
}

func TestBuildSyntheticCompactSummaryRequestLimitsLongInputWithoutPreviousID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	longText := strings.Repeat("long visible context ", 16384)
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
	body := string(got.Input)
	require.Contains(t, body, "Visible conversation to compact")
	require.Contains(t, body, "[truncated earlier visible input]")
	require.Contains(t, body, "long visible context")
	require.Less(t, len(body), syntheticCompactVisibleTextMax+4096)
}

func TestBuildSyntheticCompactSummaryRequestUsesBaseModelForCompactModel(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5-openai-compact",
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue the task."}]}
		]`),
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.5", got.Model)
	require.Contains(t, string(got.Input), "Visible conversation to compact")
}

func TestBuildSyntheticCompactSummaryRequestLogsPreparedSummaryMetadata(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	var logs bytes.Buffer
	common.LogWriterMu.Lock()
	previousWriter := gin.DefaultWriter
	gin.DefaultWriter = &logs
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = previousWriter
		common.LogWriterMu.Unlock()
	})

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5.5-openai-compact",
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Sensitive visible body should stay out of logs."}]}
		]`),
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.5", got.Model)
	logText := logs.String()
	require.Contains(t, logText, "responses synthetic compact summary request prepared")
	require.Contains(t, logText, "original_model=gpt-5.5-openai-compact")
	require.Contains(t, logText, "summary_model=gpt-5.5")
	require.Contains(t, logText, "visible_parts=1")
	require.Contains(t, logText, "upstream_previous_response_id=false")
	require.NotContains(t, logText, "Sensitive visible body")
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

func TestVisibleResponsesInputPartsSkipsCompactionSummary(t *testing.T) {
	parts := visibleResponsesInputParts(common.RawMessage(`[
		{"type":"compaction_summary","encrypted_content":"opaque","content":[{"type":"input_text","text":"must stay hidden"}]}
	]`))

	require.Empty(t, parts)
}

func TestVisibleResponsesInputPartsSkipsContextCompaction(t *testing.T) {
	parts := visibleResponsesInputParts(common.RawMessage(`[
		{"type":"context_compaction","encrypted_content":"opaque","content":[{"type":"input_text","text":"must stay hidden"}]}
	]`))

	require.Empty(t, parts)
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

	longText := strings.Repeat("界", syntheticCompactVisibleTextMax/3+1024)
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
	require.Len(t, items[1].Content, 1)
	for _, part := range items[1].Content {
		require.LessOrEqual(t, len(part.Text), syntheticCompactTextPartMax)
		require.LessOrEqual(t, len(part.Text), syntheticCompactVisibleTextMax+4096)
		require.Contains(t, part.Text, "[truncated earlier visible input]")
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

func TestApplySyntheticCompactStateAcceptsCompactionSummaryMarker(t *testing.T) {
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
			{"type":"compaction_summary","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_prev"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"What is next?"}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Contains(t, string(got.Input), "Stored compact state.")
	require.NotContains(t, string(got.Input), "compaction_summary")
	require.NotContains(t, string(got.Input), "newapi.synthetic.compact:resp_newapi_synthcmp_prev")
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
	require.Contains(t, body, "If post-compact input is only repeated setup, repository instructions, AGENTS.md content")
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

func TestApplySyntheticCompactStateOrVisibleOnlyDropsMissingStateWhenVisibleInputExists(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	missingID := syntheticCompactIDPrefix + localID + "_missing_visible"
	marker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + localID + ":" + missingID
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: missingID,
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + marker + `"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue with visible context"}]}
		]`),
	}

	got, applied, visibleOnly, err := ApplySyntheticCompactStateOrVisibleOnly(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.False(t, applied)
	require.True(t, visibleOnly)
	require.Empty(t, got.PreviousResponseID)
	require.Contains(t, string(got.Input), "continue with visible context")
	require.NotContains(t, string(got.Input), missingID)
	require.NotContains(t, string(got.Input), "newapi.synthetic.compact")
}

func TestApplySyntheticCompactStateOrVisibleOnlyRejectsMissingStateWithoutVisibleInput(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	missingID := syntheticCompactIDPrefix + localID + "_missing_empty"
	marker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + localID + ":" + missingID
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + marker + `"}
		]`),
	}

	_, applied, visibleOnly, err := ApplySyntheticCompactStateOrVisibleOnly(context.Background(), SyntheticCompactStateScope{}, req)

	require.ErrorIs(t, err, ErrSyntheticCompactStateNotFound)
	require.False(t, applied)
	require.False(t, visibleOnly)
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

func TestBuildSyntheticCompactSummaryRequestVisibleOnlyDropsUpstreamPreviousID(t *testing.T) {
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
		Input: common.RawMessage(`[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue after overflow"}]}
		]`),
	}
	ctx := context.WithValue(context.Background(), constant.ContextKeyResponsesCompactVisibleOnly, true)

	got, err := BuildSyntheticCompactSummaryRequest(ctx, SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.Empty(t, got.PreviousResponseID)
	require.Contains(t, string(got.Input), "Visible conversation to compact")
	require.Contains(t, string(got.Input), "[user] continue after overflow")
	require.NotContains(t, string(got.Input), "existing previous_response_id context")
	require.NotContains(t, string(got.Input), "resp_upstream_previous")
}

func TestSetSyntheticCompactApplyInfoClearsBooleanContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	SetSyntheticCompactApplyInfo(c, SyntheticCompactApplyInfo{
		StateLookup:    "hit",
		MarkerKind:     "synthetic_summary",
		ScopeResult:    "hit",
		RouteDecision:  "state_restored",
		FallbackReason: "none",
		StateHash:      "hash",
		StateRestored:  true,
		ModelChanged:   true,
	})
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactStateRestored))
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactModelChanged))
	require.Equal(t, "synthetic_summary", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactMarkerKind))
	require.Equal(t, "hit", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactStateScopeResult))
	require.Equal(t, "state_restored", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
	require.Equal(t, "none", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactFallbackReason))
	require.Equal(t, "hash", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactStateHash))
	require.Equal(t, "state_restored", c.Request.Context().Value(constant.ContextKeyResponsesCompactRouteDecision))
	require.Equal(t, "hash", c.Request.Context().Value(constant.ContextKeyResponsesCompactStateHash))

	SetSyntheticCompactApplyInfo(c, SyntheticCompactApplyInfo{
		StateLookup:   "",
		StateRestored: false,
		ModelChanged:  false,
	})
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactStateRestored))
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyResponsesCompactModelChanged))
	require.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactStateLookup))
	require.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactMarkerKind))
	require.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactStateScopeResult))
	require.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactRouteDecision))
	require.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactFallbackReason))
	require.Equal(t, "", common.GetContextKeyString(c, constant.ContextKeyResponsesCompactStateHash))
	require.Equal(t, "", c.Request.Context().Value(constant.ContextKeyResponsesCompactRouteDecision))
	require.Equal(t, "", c.Request.Context().Value(constant.ContextKeyResponsesCompactStateHash))
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

func TestSyntheticCompactV2MarkerResolvesOnlyLocalInstance(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	state := SyntheticCompactState{
		ID:      syntheticCompactIDPrefix + localID + "_local",
		Model:   "gpt-5",
		Summary: "Local compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	marker, err := syntheticCompactMarker(context.Background(), state.ID)
	require.NoError(t, err)

	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + marker + `"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Contains(t, string(got.Input), "Local compact state.")
	require.NotContains(t, string(got.Input), marker)
}

func TestHasRemoteResponsesCompactionInputClassifiesRemoteAndLocalMarkers(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	remoteReq := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"opaque-native-compact"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	hasRemote, err := HasRemoteResponsesCompactionInput(context.Background(), remoteReq)
	require.NoError(t, err)
	require.True(t, hasRemote)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	state := SyntheticCompactState{
		ID:      syntheticCompactIDPrefix + localID + "_local_for_remote_detection",
		Model:   "gpt-5",
		Summary: "Local compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	marker, err := syntheticCompactMarker(context.Background(), state.ID)
	require.NoError(t, err)

	localReq := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + marker + `"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	hasRemote, err = HasRemoteResponsesCompactionInput(context.Background(), localReq)
	require.NoError(t, err)
	require.False(t, hasRemote)

	foreignID := "nffffffffffffffffffffffffffffffff"
	if foreignID == localID {
		foreignID = "neeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	}
	foreignMarker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + foreignID + ":" + syntheticCompactIDPrefix + foreignID + "_stale_for_remote_detection"
	foreignReq := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + foreignMarker + `"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}
	hasRemote, err = HasRemoteResponsesCompactionInput(context.Background(), foreignReq)
	require.NoError(t, err)
	require.False(t, hasRemote)
}

func TestHasRemoteResponsesCompactionInputRejectsMissingEncryptedContent(t *testing.T) {
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	hasRemote, err := HasRemoteResponsesCompactionInput(context.Background(), req)

	require.False(t, hasRemote)
	require.ErrorIs(t, err, ErrResponsesCompactionContentRequired)
}

func TestSyntheticCompactForeignV2MarkerIsLocalReferenceWithVisibleOnlyFallback(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	foreignID := "nffffffffffffffffffffffffffffffff"
	if foreignID == localID {
		foreignID = "neeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	}
	stateID := syntheticCompactIDPrefix + foreignID + "_foreign"
	marker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + foreignID + ":" + stateID
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + marker + `"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	hasReference, err := HasLocalSyntheticCompactReference(req)
	require.NoError(t, err)
	require.True(t, hasReference)

	got, applied, visibleOnly, err := ApplySyntheticCompactStateOrVisibleOnly(context.Background(), SyntheticCompactStateScope{}, req)
	require.NoError(t, err)
	require.False(t, applied)
	require.True(t, visibleOnly)
	require.Contains(t, string(got.Input), "continue")
	require.NotContains(t, string(got.Input), stateID)
}

func TestSyntheticCompactForeignV2MarkerRestoresFromDatabase(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	foreignID := "nffffffffffffffffffffffffffffffff"
	if foreignID == localID {
		foreignID = "neeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	}
	state := SyntheticCompactState{
		ID:             syntheticCompactIDPrefix + foreignID + "_foreign_db",
		Model:          "gpt-5",
		Summary:        "Cross-instance database compact state.",
		SourceInstance: foreignID,
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	resetSyntheticCompactMemoryStoreForTest()

	marker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + foreignID + ":" + state.ID
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + marker + `"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Contains(t, string(got.Input), "Cross-instance database compact state.")
	require.NotContains(t, string(got.Input), marker)
}

func TestSyntheticCompactMalformedV2MarkerWithoutIDInstanceIsNotLocalReference(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_legacy",
		Model:   "gpt-5",
		Summary: "Legacy compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	marker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + localID + ":" + state.ID
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + marker + `"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	hasReference, err := HasLocalSyntheticCompactReference(req)
	require.NoError(t, err)
	require.False(t, hasReference)

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)
	require.NoError(t, err)
	require.Contains(t, string(got.Input), "Visible conversation to compact")
	require.Contains(t, string(got.Input), "[user] continue")
	require.NotContains(t, string(got.Input), "Legacy compact state.")
}

func TestSyntheticCompactForeignPreviousResponseIDIsLocalSyntheticReference(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	foreignID := "nffffffffffffffffffffffffffffffff"
	if foreignID == localID {
		foreignID = "neeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	}
	stateID := syntheticCompactIDPrefix + foreignID + "_foreign"
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: stateID,
		Input:              common.RawMessage(`"continue"`),
	}

	hasReference, err := HasLocalSyntheticCompactReference(req)
	require.NoError(t, err)
	require.True(t, hasReference)

	got, applied, visibleOnly, err := ApplySyntheticCompactStateOrVisibleOnly(context.Background(), SyntheticCompactStateScope{}, req)
	require.NoError(t, err)
	require.False(t, applied)
	require.True(t, visibleOnly)
	require.Empty(t, got.PreviousResponseID)
	require.Contains(t, string(got.Input), "continue")
}

func TestApplySyntheticCompactStateVisibleOnlyFallbackInfoDoesNotClaimRestore(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	localID, err := syntheticCompactLocalInstanceID(context.Background())
	require.NoError(t, err)
	stateID := syntheticCompactIDPrefix + localID + "_missing_visible_only_info"
	marker := syntheticCompactMarkerPrefix + syntheticCompactMarkerVersion + ":" + localID + ":" + stateID
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"` + marker + `"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, applied, visibleOnly, info, err := ApplySyntheticCompactStateOrVisibleOnlyWithInfo(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.False(t, applied)
	require.True(t, visibleOnly)
	require.False(t, info.StateRestored)
	require.False(t, info.ModelChanged)
	require.Equal(t, "miss", info.StateLookup)
	require.Equal(t, model.SyntheticCompactStateKindSyntheticSummary, info.MarkerKind)
	require.Equal(t, "not_found", info.ScopeResult)
	require.Equal(t, "visible_only_fallback", info.RouteDecision)
	require.Equal(t, "state_not_found_visible_input", info.FallbackReason)
	require.Empty(t, info.StateHash)
	require.Contains(t, string(got.Input), "continue")
	require.NotContains(t, string(got.Input), stateID)
}

func TestSyntheticCompactLocalInstanceIDUpdatesInvalidStoredOption(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	resetSyntheticCompactInstanceForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactInstanceForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	require.NoError(t, model.DB.Create(&model.Option{
		Key:   syntheticCompactInstanceOptionKey,
		Value: "invalid-instance",
	}).Error)

	id, err := syntheticCompactLocalInstanceID(context.Background())

	require.NoError(t, err)
	require.True(t, syntheticCompactInstanceIDValid(id))
	var option model.Option
	require.NoError(t, model.DB.First(&option, "key = ?", syntheticCompactInstanceOptionKey).Error)
	require.Equal(t, id, option.Value)
}

func TestSyntheticCompactLocalInstanceIDConcurrentInitUsesOneID(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	resetSyntheticCompactInstanceForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactInstanceForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	const workers = 12
	results := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := syntheticCompactLocalInstanceID(context.Background())
			if err != nil {
				errs <- err
				return
			}
			results <- id
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
	seen := map[string]struct{}{}
	for id := range results {
		require.True(t, syntheticCompactInstanceIDValid(id))
		seen[id] = struct{}{}
	}
	require.Len(t, seen, 1)
	var options []model.Option
	require.NoError(t, model.DB.Find(&options, "key = ?", syntheticCompactInstanceOptionKey).Error)
	require.Len(t, options, 1)
	for id := range seen {
		require.Equal(t, id, options[0].Value)
	}
}

func TestSyntheticCompactOldMarkerCompatibility(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_legacy",
		Model:   "gpt-5",
		Summary: "Legacy compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	req := dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: common.RawMessage(`[
			{"type":"compaction","encrypted_content":"newapi.synthetic.compact:resp_newapi_synthcmp_legacy"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		]`),
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Contains(t, string(got.Input), "Legacy compact state.")
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
	_, applied, err = ApplySyntheticCompactState(context.Background(), SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}, req)
	require.Error(t, err)
	require.False(t, applied)
	require.Contains(t, err.Error(), "synthetic_compact_state_records")
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

func TestLoadSyntheticCompactStateUsesPersistentMemoryBeforeDatabaseWhenRedisEnabled(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_memory_before_db",
			Model:   "gpt-5",
			Summary: "Fresh memory should avoid database read amplification.",
		}
		require.NoError(t, storeSyntheticCompactState(context.Background(), state))
		require.NoError(t, model.DB.Migrator().DropTable(&model.SyntheticCompactStateRecord{}))

		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

		require.NoError(t, err)
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

func TestLoadSyntheticCompactStateFallsBackToRedisWhenDatabaseMisses(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:      "resp_newapi_synthcmp_redis_after_db_miss",
			Model:   "gpt-5",
			Summary: "Redis state survives database miss.",
		}
		record, err := syntheticCompactStateRecord(context.Background(), state, time.Now())
		require.NoError(t, err)
		data, err := common.Marshal(record)
		require.NoError(t, err)
		require.NoError(t, common.RDB.Set(context.Background(), syntheticCompactRedisKey(state.ID), string(data), syntheticCompactTTL).Err())
		resetSyntheticCompactMemoryStoreForTest()

		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.Summary, got.Summary)
		requireEventuallySyntheticCompactRecord(t, state.ID)

		require.NoError(t, common.RDB.Del(context.Background(), syntheticCompactRedisKey(state.ID)).Err())
		resetSyntheticCompactMemoryStoreForTest()
		got, found, err = loadSyntheticCompactState(context.Background(), state.ID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.Summary, got.Summary)
	})
}

func TestLoadSyntheticCompactStateBackfillsRedisRecordWithoutChangingStateHash(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:        "resp_newapi_synthcmp_redis_backfill_hash",
			Model:     "gpt-5",
			Summary:   "Redis record hash should remain stable on database backfill.",
			CreatedAt: time.Now().Add(-time.Minute).Unix(),
		}
		record, err := syntheticCompactStateRecord(context.Background(), state, time.Now())
		require.NoError(t, err)
		data, err := common.Marshal(record)
		require.NoError(t, err)
		require.NoError(t, common.RDB.Set(context.Background(), syntheticCompactRedisKey(state.ID), string(data), time.Minute).Err())
		resetSyntheticCompactMemoryStoreForTest()

		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, record.StateHash, got.StateHash)
		persisted := requireEventuallySyntheticCompactRecord(t, state.ID)
		require.Equal(t, record.ExpiresAt, persisted.ExpiresAt)
		require.Equal(t, record.StateHash, persisted.StateHash)
	})
}

func TestLoadSyntheticCompactStateBackfillCanRunAgainAfterCompletion(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:        "resp_newapi_synthcmp_redis_backfill_again",
			Model:     "gpt-5",
			Summary:   "Redis backfill can be scheduled again after completion.",
			CreatedAt: time.Now().Add(-time.Minute).Unix(),
		}
		record, err := syntheticCompactStateRecord(context.Background(), state, time.Now())
		require.NoError(t, err)
		data, err := common.Marshal(record)
		require.NoError(t, err)
		require.NoError(t, common.RDB.Set(context.Background(), syntheticCompactRedisKey(state.ID), string(data), time.Minute).Err())
		resetSyntheticCompactMemoryStoreForTest()

		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, record.StateHash, got.StateHash)
		requireEventuallySyntheticCompactRecord(t, state.ID)

		require.NoError(t, model.DB.Delete(&model.SyntheticCompactStateRecord{}, "id = ?", state.ID).Error)
		resetSyntheticCompactMemoryStoreForTest()

		got, found, err = loadSyntheticCompactState(context.Background(), state.ID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, record.StateHash, got.StateHash)
		requireEventuallySyntheticCompactRecord(t, state.ID)
	})
}

func TestLoadSyntheticCompactStateFallsBackToLegacyRedisWhenDatabaseMisses(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		state := SyntheticCompactState{
			ID:        "resp_newapi_synthcmp_legacy_redis_after_db_miss",
			Model:     "gpt-5",
			Summary:   "Legacy Redis state survives database miss.",
			CreatedAt: time.Now().Unix(),
		}
		data, err := common.Marshal(state)
		require.NoError(t, err)
		require.NoError(t, common.RDB.Set(context.Background(), syntheticCompactRedisKey(state.ID), string(data), syntheticCompactTTL).Err())
		resetSyntheticCompactMemoryStoreForTest()

		got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, state.Summary, got.Summary)
		requireEventuallySyntheticCompactRecord(t, state.ID)

		require.NoError(t, common.RDB.Del(context.Background(), syntheticCompactRedisKey(state.ID)).Err())
		resetSyntheticCompactMemoryStoreForTest()
		got, found, err = loadSyntheticCompactState(context.Background(), state.ID)
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

func TestLoadSyntheticCompactStateChecksRedisOnceWhenMissing(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	withSyntheticCompactTestRedis(t, func() {
		hook := newRedisCommandCountHook()
		common.RDB.AddHook(hook)

		got, found, err := loadSyntheticCompactState(context.Background(), "resp_newapi_synthcmp_missing_once")

		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, got)
		require.Equal(t, 1, hook.Count("get"))
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

func TestLoadSyntheticCompactStateFromRedisRejectsNullPayload(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withSyntheticCompactTestRedis(t, func() {
		id := "resp_newapi_synthcmp_null_redis"
		require.NoError(t, common.RDB.Set(context.Background(), syntheticCompactRedisKey(id), "null", syntheticCompactTTL).Err())

		got, found, err := loadSyntheticCompactStateFromRedis(context.Background(), id)

		require.Error(t, err)
		require.False(t, found)
		require.Nil(t, got)
		require.Contains(t, err.Error(), "missing id")
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

func TestSyntheticCompactDatabaseRecordStoresControlPlaneFields(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	createdAt := time.Now().Add(-time.Minute).Unix()
	state := SyntheticCompactState{
		ID:        "resp_newapi_synthcmp_control_plane",
		Model:     "gpt-5.5",
		Summary:   "Control plane fields are stored.",
		UserID:    10,
		TokenID:   20,
		Group:     "default",
		CreatedAt: createdAt,
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	var record model.SyntheticCompactStateRecord
	require.NoError(t, model.DB.Where("id = ?", state.ID).First(&record).Error)
	require.Equal(t, model.SyntheticCompactStateKindSyntheticSummary, record.Kind)
	require.Equal(t, "gpt-5.5", record.ModelAtCreation)
	require.Equal(t, model.SyntheticCompactOwnerScope(10, 20, "default"), record.OwnerScope)
	require.Equal(t, model.SyntheticCompactScopePolicyModelFlexible, record.ScopePolicy)
	require.NotEmpty(t, record.SourceInstance)
	require.NotEmpty(t, record.StateHash)
	require.Equal(t, createdAt, record.LastAccessAt)
}

func TestLoadSyntheticCompactStateUsesDatabaseTruthOverStaleMemory(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
		resetSyntheticCompactMemoryStoreForTest()
	})
	model.DB = openSyntheticCompactServiceTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_db_truth",
		Model:   "gpt-5",
		Summary: "Database truth summary.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))
	syntheticCompactMemoryStore.Store(state.ID, syntheticCompactMemoryEntry{
		state:     SyntheticCompactState{ID: state.ID, Model: "gpt-5", Summary: "Stale memory summary."},
		expiresAt: time.Now().Add(time.Hour),
	})

	got, found, err := loadSyntheticCompactState(context.Background(), state.ID)

	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "Database truth summary.", got.Summary)
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

	require.True(t, applied)
	require.Error(t, err)
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

func TestBuildSyntheticCompactSummaryRequestAllowsModelSwitchWithinScope(t *testing.T) {
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

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), SyntheticCompactStateScope{}, req)

	require.NoError(t, err)
	require.Equal(t, "gpt-4.1", got.Model)
	require.Contains(t, string(got.Input), "Scoped compact state.")
}

func TestApplySyntheticCompactStateAllowsModelSwitchWithinScope(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_apply_model_switch",
		Model:   "gpt-5.5",
		Summary: "Scoped compact state.",
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.4",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	scope := SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}

	got, applied, err := ApplySyntheticCompactState(context.Background(), scope, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.Empty(t, got.PreviousResponseID)
	require.Equal(t, "gpt-5.4", got.Model)
	require.Contains(t, string(got.Input), "Scoped compact state.")
}

func TestApplySyntheticCompactStateWithInfoRecordsModelSwitch(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	state := SyntheticCompactState{
		ID:              "resp_newapi_synthcmp_apply_model_switch_info",
		Model:           "gpt-5.5",
		ModelAtCreation: "gpt-5.5",
		Summary:         "Scoped compact state.",
		UserID:          10,
		TokenID:         20,
		Group:           "default",
		StateHash:       "hash-for-test",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.4",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	scope := SyntheticCompactStateScope{UserID: 10, TokenID: 20, Group: "default"}

	_, applied, info, err := ApplySyntheticCompactStateWithInfo(context.Background(), scope, req)

	require.NoError(t, err)
	require.True(t, applied)
	require.True(t, info.StateRestored)
	require.True(t, info.ModelChanged)
	require.Equal(t, "hit", info.StateLookup)
	require.Equal(t, "matched", info.ScopeResult)
	require.Equal(t, "state_restored", info.RouteDecision)
	require.Equal(t, model.SyntheticCompactStateKindSyntheticSummary, info.MarkerKind)
	require.NotEmpty(t, info.StateHash)
}

func TestBuildSyntheticCompactSummaryRequestPreservesRequestModelAfterModelSwitch(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	state := SyntheticCompactState{
		ID:      "resp_newapi_synthcmp_summary_fallback_model",
		Model:   "gpt-5.5-openai-compact",
		Summary: "Scoped compact state.",
	}
	require.NoError(t, storeSyntheticCompactState(context.Background(), state))

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.4",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage(`"continue"`),
	}
	scope := SyntheticCompactStateScope{
		Model: "gpt-5.5-openai-compact",
	}

	got, err := BuildSyntheticCompactSummaryRequest(context.Background(), scope, req)

	require.NoError(t, err)
	require.Equal(t, "gpt-5.4", got.Model)
	require.Contains(t, string(got.Input), "Scoped compact state.")
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
	output := []map[string]string{}
	require.NoError(t, common.Unmarshal(compactResp.Output, &output))
	require.Len(t, output, 1)
	require.Equal(t, "compaction", output[0]["type"])
	require.True(t, strings.HasPrefix(output[0]["encrypted_content"], syntheticCompactMarkerPrefix+syntheticCompactMarkerVersion+":"))

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

func TestStoreNativeOpaqueCompactStateStoresStrictOpaqueRecord(t *testing.T) {
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	withoutRedisForSyntheticCompactTest(t)

	scope := SyntheticCompactStateScope{
		UserID:      7,
		TokenID:     8,
		Group:       "default",
		Model:       "gpt-5.5",
		ChannelID:   90,
		ChannelType: 1,
	}
	state, err := StoreNativeOpaqueCompactState(context.Background(), scope, "gpt-5.5", "resp_native", "opaque-token", 1710000000)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, model.SyntheticCompactStateKindNativeOpaque, state.Kind)
	require.Equal(t, model.SyntheticCompactScopePolicyStrict, state.ScopePolicy)
	require.Equal(t, "resp_native", state.UpstreamResponseID)
	require.NotEmpty(t, state.StateHash)

	var record model.SyntheticCompactStateRecord
	require.NoError(t, model.DB.Where("id = ?", state.ID).First(&record).Error)
	require.Equal(t, model.SyntheticCompactStateKindNativeOpaque, record.Kind)
	require.Equal(t, model.SyntheticCompactScopePolicyStrict, record.ScopePolicy)
	require.Equal(t, model.SyntheticCompactOwnerScope(7, 8, "default"), record.OwnerScope)
	require.Equal(t, "resp_native", record.UpstreamResponseID)
	require.NotEmpty(t, record.StateHash)
	require.NotEmpty(t, record.SummaryCiphertext)
	require.NotEqual(t, "opaque-token", string(record.SummaryCiphertext))
}

func TestStoreNativeOpaqueCompactStateRejectsEmptyResponseID(t *testing.T) {
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	withoutRedisForSyntheticCompactTest(t)

	scope := SyntheticCompactStateScope{
		UserID:      7,
		TokenID:     8,
		Group:       "default",
		Model:       "gpt-5.5",
		ChannelID:   90,
		ChannelType: 1,
	}
	state, err := StoreNativeOpaqueCompactState(context.Background(), scope, "gpt-5.5", "   ", "opaque-token", 1710000000)

	require.Nil(t, state)
	require.Error(t, err)
	require.Contains(t, err.Error(), "response id is required")
}

func TestSyntheticCompactStateFromRecordReadsLegacyAADWithUpstreamResponseID(t *testing.T) {
	record := model.SyntheticCompactStateRecord{
		ID:                 "resp_newapi_nativecmp_legacy",
		Kind:               model.SyntheticCompactStateKindNativeOpaque,
		Model:              "gpt-5.5",
		ModelAtCreation:    "gpt-5.5",
		UserID:             7,
		TokenID:            8,
		Group:              "default",
		UpstreamResponseID: "resp_native_upstream",
		CreatedAt:          1710000000,
	}
	model.PrepareSyntheticCompactStateRecord(&record, 1710000000)
	legacyRecord := record
	legacyRecord.UpstreamResponseID = ""
	summaryCiphertext := encryptSyntheticCompactSummaryForTestWithAAD(t, "opaque-token", syntheticCompactSummaryLegacyAAD(legacyRecord))
	record.SummaryCiphertext = model.SyntheticCompactSummaryCiphertext(summaryCiphertext)
	model.PrepareSyntheticCompactStateRecord(&record, 1710000000)

	state, err := syntheticCompactStateFromRecord(&record)

	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, model.SyntheticCompactStateKindNativeOpaque, state.Kind)
	require.Equal(t, "opaque-token", state.Summary)
	require.Equal(t, "resp_native_upstream", state.UpstreamResponseID)
}

func TestSyntheticCompactStateFromRecordReadsLegacyAADWithEmptyUpstreamResponseID(t *testing.T) {
	record := model.SyntheticCompactStateRecord{
		ID:              "resp_newapi_synthcmp_legacy_empty_upstream",
		Kind:            model.SyntheticCompactStateKindSyntheticSummary,
		Model:           "gpt-5.5",
		ModelAtCreation: "gpt-5.5",
		UserID:          7,
		TokenID:         8,
		Group:           "default",
		CreatedAt:       1710000000,
	}
	model.PrepareSyntheticCompactStateRecord(&record, 1710000000)
	summaryCiphertext := encryptSyntheticCompactSummaryForTestWithAAD(t, "summary-token", syntheticCompactSummaryLegacyAAD(record))
	record.SummaryCiphertext = model.SyntheticCompactSummaryCiphertext(summaryCiphertext)
	model.PrepareSyntheticCompactStateRecord(&record, 1710000000)

	state, err := syntheticCompactStateFromRecord(&record)

	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, "summary-token", state.Summary)
	require.Empty(t, state.UpstreamResponseID)
}

func TestApplySyntheticCompactStateRejectsNativeOpaqueState(t *testing.T) {
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	withoutRedisForSyntheticCompactTest(t)

	scope := SyntheticCompactStateScope{
		UserID:      7,
		TokenID:     8,
		Group:       "default",
		Model:       "gpt-5.5",
		ChannelID:   90,
		ChannelType: 1,
	}
	state, err := StoreNativeOpaqueCompactState(context.Background(), scope, "gpt-5.5", "resp_native", "opaque-token", 1710000000)
	require.NoError(t, err)

	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage([]byte(`"continue"`)),
	}
	converted, applied, info, err := ApplySyntheticCompactStateWithInfo(context.Background(), scope, req)

	require.ErrorIs(t, err, ErrResponsesNativeOpaqueStateNotRestorable)
	require.False(t, applied)
	require.Equal(t, req.PreviousResponseID, converted.PreviousResponseID)
	require.Equal(t, "hit", info.StateLookup)
	require.Equal(t, model.SyntheticCompactStateKindNativeOpaque, info.MarkerKind)
	require.Equal(t, "strict", info.ScopeResult)
	require.Equal(t, "native_opaque_not_restorable", info.RouteDecision)
	require.Equal(t, "native_opaque_requires_upstream", info.FallbackReason)
	require.NotEmpty(t, info.StateHash)
}

func TestRestoreNativeOpaquePreviousResponseID(t *testing.T) {
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	withoutRedisForSyntheticCompactTest(t)

	scope := SyntheticCompactStateScope{
		UserID:      7,
		TokenID:     8,
		Group:       "default",
		Model:       "gpt-5.5",
		ChannelID:   90,
		ChannelType: 1,
	}
	state, err := StoreNativeOpaqueCompactState(context.Background(), scope, "gpt-5.5", "resp_native_upstream", "opaque-token", 1710000000)
	require.NoError(t, err)
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.5",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage([]byte(`"continue"`)),
	}

	converted, restored, info, err := RestoreNativeOpaquePreviousResponseID(context.Background(), scope, req)

	require.NoError(t, err)
	require.True(t, restored)
	require.Equal(t, "resp_native_upstream", converted.PreviousResponseID)
	require.Equal(t, "hit", info.StateLookup)
	require.Equal(t, model.SyntheticCompactStateKindNativeOpaque, info.MarkerKind)
	require.Equal(t, "native_opaque_previous_response_id_restored", info.RouteDecision)
	require.NotEmpty(t, info.StateHash)
}

func TestRestoreNativeOpaquePreviousResponseIDRejectsDifferentModel(t *testing.T) {
	originDB := model.DB
	t.Cleanup(func() {
		model.DB = originDB
	})
	model.DB = openSyntheticCompactServiceTestDB(t)
	withoutRedisForSyntheticCompactTest(t)

	scope := SyntheticCompactStateScope{
		UserID:      7,
		TokenID:     8,
		Group:       "default",
		Model:       "gpt-5.5",
		ChannelID:   90,
		ChannelType: 1,
	}
	state, err := StoreNativeOpaqueCompactState(context.Background(), scope, "gpt-5.5", "resp_native_upstream", "opaque-token", 1710000000)
	require.NoError(t, err)
	req := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.4",
		PreviousResponseID: state.ID,
		Input:              common.RawMessage([]byte(`"continue"`)),
	}

	converted, restored, info, err := RestoreNativeOpaquePreviousResponseID(context.Background(), scope, req)

	require.ErrorIs(t, err, ErrSyntheticCompactStateScopeMismatch)
	require.False(t, restored)
	require.Equal(t, state.ID, converted.PreviousResponseID)
	require.Equal(t, "hit", info.StateLookup)
	require.Equal(t, model.SyntheticCompactStateKindNativeOpaque, info.MarkerKind)
	require.Equal(t, "mismatch", info.ScopeResult)
	require.Equal(t, "scope_mismatch", info.FallbackReason)
}

func TestBuildSyntheticCompactResponseRejectsLostTaskSummaryForLargeInput(t *testing.T) {
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
					{Type: "output_text", Text: "Repository instructions received. There is no explicit task to continue."},
				},
			},
		},
		Usage: &dto.Usage{InputTokens: 31246, OutputTokens: 29, TotalTokens: 31275},
	}

	scope := SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	compactResp, usage, err := BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5.5-openai-compact", resp)

	require.Nil(t, compactResp)
	require.Nil(t, usage)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a recoverable handoff summary")
}

func TestBuildSyntheticCompactResponseRejectsTooShortSummaryForLargeInput(t *testing.T) {
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
					{Type: "output_text", Text: "Current task: continue. Remaining work: inspect and fix."},
				},
			},
		},
		Usage: &dto.Usage{InputTokens: 8192, OutputTokens: 32, TotalTokens: 8224},
	}

	scope := SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	compactResp, usage, err := BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5.5-openai-compact", resp)

	require.Nil(t, compactResp)
	require.Nil(t, usage)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short for large input")
}

func TestBuildSyntheticCompactResponseAcceptsStructuredSummaryForLargeInput(t *testing.T) {
	resetSyntheticCompactMemoryStoreForTest()
	withoutSyntheticCompactTestDB(t)

	summary := strings.Repeat(strings.Join([]string{
		"Current task:",
		"- Fix the synthetic compact resume path.",
		"Progress and decisions:",
		"- The issue is a compact summary quality failure, not a routing failure.",
		"Important context and constraints:",
		"- Preserve AGENTS.md constraints and existing worktree changes.",
		"Remaining work:",
		"- Patch service tests and run focused Go tests.",
		"Files, commands, and evidence:",
		"- service/responses_synthetic_compact.go and service/responses_synthetic_compact_test.go.",
		"",
	}, "\n"), 2)
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
					{Type: "output_text", Text: summary},
				},
			},
		},
		Usage: &dto.Usage{InputTokens: 8192, OutputTokens: 180, TotalTokens: 8372},
	}

	scope := SyntheticCompactStateScope{
		UserID:  10,
		TokenID: 20,
		Group:   "default",
	}
	compactResp, usage, err := BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5.5-openai-compact", resp)

	require.NoError(t, err)
	require.NotNil(t, compactResp)
	require.NotNil(t, usage)
	state, found, err := loadSyntheticCompactState(context.Background(), compactResp.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, strings.TrimSpace(summary), state.Summary)
}

func TestBuildSyntheticCompactResponseEstimatesUsageWhenUpstreamUsageMissing(t *testing.T) {
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
					{Type: "output_text", Text: "Synthetic summary text with enough content for estimated usage."},
				},
			},
		},
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
	require.NotNil(t, compactResp.Usage)
	require.Greater(t, usage.CompletionTokens, 0)
	require.Equal(t, usage.CompletionTokens, usage.TotalTokens)
	require.Equal(t, usage.CompletionTokens, usage.OutputTokens)
}

func TestBuildSyntheticCompactResponseUsesTotalOnlyUpstreamUsage(t *testing.T) {
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
		Usage: &dto.Usage{TotalTokens: 17},
	}

	scope := SyntheticCompactStateScope{
		UserID:      10,
		TokenID:     20,
		Group:       "default",
		ChannelID:   163,
		ChannelType: 1,
	}
	_, usage, err := BuildSyntheticCompactResponse(context.Background(), scope, "gpt-5", resp)

	require.NoError(t, err)
	require.Equal(t, 17, usage.CompletionTokens)
	require.Equal(t, 17, usage.TotalTokens)
	require.Equal(t, 17, usage.OutputTokens)
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
