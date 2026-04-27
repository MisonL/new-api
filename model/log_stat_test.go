package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSumUsedQuotaUsesSingleFilteredConsumeStat(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)

	now := time.Now().Unix()
	logs := []*Log{
		{
			CreatedAt:        now - 10,
			Type:             LogTypeConsume,
			Username:         "alice",
			TokenName:        "codex-token",
			ModelName:        "gpt-5.5",
			Quota:            100,
			PromptTokens:     12,
			CompletionTokens: 8,
			ChannelId:        24,
			Group:            "default",
		},
		{
			CreatedAt:        now - 90,
			Type:             LogTypeConsume,
			Username:         "alice",
			TokenName:        "codex-token",
			ModelName:        "gpt-5.5",
			Quota:            60,
			PromptTokens:     30,
			CompletionTokens: 20,
			ChannelId:        24,
			Group:            "default",
		},
		{
			CreatedAt:        now - 5,
			Type:             LogTypeTopup,
			Username:         "alice",
			TokenName:        "codex-token",
			ModelName:        "gpt-5.5",
			Quota:            999,
			PromptTokens:     100,
			CompletionTokens: 100,
			ChannelId:        24,
			Group:            "default",
		},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)

	stat, err := SumUsedQuota(0, now-120, now+1, "gpt-5.5", "alice", "codex-token", 24, "default")

	require.NoError(t, err)
	require.Equal(t, 160, stat.Quota)
	require.Equal(t, 1, stat.Rpm)
	require.Equal(t, 20, stat.Tpm)
}
