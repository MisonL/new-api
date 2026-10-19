package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

// 同一个 TOTP 码只能使用一次。totp.Validate 本身不记录已用步进，若不持久化，
// 截获的码在 30 秒有效期内（含前后各一步容差）可以无限重放。
func TestValidateTOTPAndUpdateUsageRejectsReplay(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TwoFA{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM two_fas")
	})

	key, err := common.GenerateTOTPSecret("replay-model-user")
	require.NoError(t, err)
	twoFA := &TwoFA{UserId: 501, Secret: key.Secret(), IsEnabled: true}
	require.NoError(t, DB.Create(twoFA).Error)

	now := time.Now().UTC()
	code, err := totp.GenerateCode(key.Secret(), now)
	require.NoError(t, err)

	ok, err := twoFA.ValidateTOTPAndUpdateUsage(code)
	require.NoError(t, err)
	require.True(t, ok, "the first use must succeed")

	// 从数据库重新读，确认步进已落库。
	stored, err := GetTwoFAByUserId(501)
	require.NoError(t, err)
	require.Greater(t, stored.LastUsedStep, int64(0), "used step must be persisted")

	ok, err = stored.ValidateTOTPAndUpdateUsage(code)
	require.NoError(t, err)
	require.False(t, ok, "the same code must be refused on replay")
}

func TestValidateTOTPAndUpdateUsageAcceptsNextStep(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TwoFA{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM two_fas")
	})

	key, err := common.GenerateTOTPSecret("next-step-user")
	require.NoError(t, err)
	twoFA := &TwoFA{UserId: 502, Secret: key.Secret(), IsEnabled: true}
	require.NoError(t, DB.Create(twoFA).Error)

	now := time.Now().UTC()
	code, err := totp.GenerateCode(key.Secret(), now)
	require.NoError(t, err)
	ok, err := twoFA.ValidateTOTPAndUpdateUsage(code)
	require.NoError(t, err)
	require.True(t, ok)

	// 下一个窗口的新码仍然可用，且步进继续前进。
	futureStep := time.Unix((now.Unix()/30+1)*30, 0)
	nextCode, err := totp.GenerateCode(key.Secret(), futureStep)
	require.NoError(t, err)

	stored, err := GetTwoFAByUserId(502)
	require.NoError(t, err)
	previousStep := stored.LastUsedStep
	step, ok := common.ValidateTOTPStep(stored.Secret, nextCode, futureStep, stored.LastUsedStep, stored.LastUsedStep > 0)
	require.True(t, ok)
	require.Greater(t, step, previousStep)
}
