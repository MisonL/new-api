package common

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

// totp.Validate 只比对当前及相邻窗口，不记录已使用的步进：同一个 6 位码在
// 有效期内可以无限次通过。身份认证场景必须在验证通过后记录已用步进，
// 之后同一或更早的步进一律拒绝。
func TestValidateTOTPStep_ConsumesEachCodeOnce(t *testing.T) {
	key, err := GenerateTOTPSecret("replay-user")
	require.NoError(t, err)
	secret := key.Secret()

	now := time.Now().UTC()
	code, err := totp.GenerateCode(secret, now)
	require.NoError(t, err)

	// 首次使用：返回步进并视为有效。
	step, ok := ValidateTOTPStep(secret, code, now, 0, false)
	require.True(t, ok)
	require.Greater(t, step, int64(0))

	// 已用过该步进：同一码必须失效。
	_, ok = ValidateTOTPStep(secret, code, now, step, true)
	require.False(t, ok, "a consumed TOTP code must not be accepted again")

	// 更早的码也一并失效（记录的是已用步进上限）。
	earlier := now.Add(-30 * time.Second)
	earlierCode, err := totp.GenerateCode(secret, earlier)
	require.NoError(t, err)
	_, ok = ValidateTOTPStep(secret, earlierCode, earlier, step, true)
	require.False(t, ok, "codes from steps at or before the consumed step must be refused")

	// 下一个步进的码仍然可用，并且返回更大的步进。
	later := now.Add(30 * time.Second)
	laterCode, err := totp.GenerateCode(secret, later)
	require.NoError(t, err)
	laterStep, ok := ValidateTOTPStep(secret, laterCode, later, step, true)
	require.True(t, ok)
	require.Greater(t, laterStep, step)
}

// 尚未记录过步进时（历史记录或刚启用），当前窗口的码必须可用。
func TestValidateTOTPStepAcceptsFirstUse(t *testing.T) {
	key, err := GenerateTOTPSecret("first-use-user")
	require.NoError(t, err)
	code, err := totp.GenerateCode(key.Secret(), time.Now().UTC())
	require.NoError(t, err)

	step, ok := ValidateTOTPStep(key.Secret(), code, time.Now().UTC(), 0, false)
	require.True(t, ok)
	require.Greater(t, step, int64(0))
}

func TestValidateTOTPStepRejectsMalformedCode(t *testing.T) {
	key, err := GenerateTOTPSecret("malformed-user")
	require.NoError(t, err)
	for _, code := range []string{"", "12345", "abcdef", "1234567"} {
		_, ok := ValidateTOTPStep(key.Secret(), code, time.Now().UTC(), 0, false)
		require.False(t, ok, "malformed code %q must be refused", code)
	}
}
