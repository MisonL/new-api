package common

import (
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	// 备用码配置
	BackupCodeLength = 8 // 备用码长度
	BackupCodeCount  = 4 // 生成备用码数量

	// 限制配置
	MaxFailAttempts = 5   // 最大失败尝试次数
	LockoutDuration = 300 // 锁定时间（秒）
)

// GenerateTOTPSecret 生成TOTP密钥和配置
func GenerateTOTPSecret(accountName string) (*otp.Key, error) {
	issuer := Get2FAIssuer()
	return totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
}

// ValidateTOTPCode 验证TOTP验证码
func ValidateTOTPCode(secret, code string) bool {
	// 清理验证码格式
	cleanCode := strings.ReplaceAll(code, " ", "")
	if len(cleanCode) != 6 {
		return false
	}

	// 验证验证码
	return totp.Validate(cleanCode, secret)
}

// totpPeriodSeconds 与 GenerateTOTPSecret 中使用的 30 秒周期保持一致。
const totpPeriodSeconds = 30

// ValidateTOTPStep 验证 TOTP 验证码，并返回它所属的时间步进。
//
// totp.Validate 只比对当前及相邻窗口，不记录已经用过的码：同一个 6 位码
// 在有效期内可以重复通过，一旦被截获即可重放。调用方应把返回的步进持久化，
// 之后以 lastUsedStep 传入；前一窗口的码只用于容忍时钟偏差，并且只有在
// 未使用过时才接受，因此 nextStep 始终前进，不会因偏慢的时钟回退。
func ValidateTOTPStep(secret, code string, now time.Time, lastUsedStep int64, hasLastUsedStep bool) (int64, bool) {
	cleanCode := strings.ReplaceAll(code, " ", "")
	if len(cleanCode) != 6 {
		return 0, false
	}
	now = now.UTC()
	opts := totp.ValidateOpts{
		Period:    totpPeriodSeconds,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}
	matches := make([]int64, 0, 3)
	for _, offset := range []int{0, -1, 1} {
		candidateAt := now.Add(time.Duration(offset*totpPeriodSeconds) * time.Second)
		valid, err := totp.ValidateCustom(cleanCode, secret, candidateAt, opts)
		if err == nil && valid {
			matches = append(matches, candidateAt.Unix()/totpPeriodSeconds)
		}
	}
	if len(matches) == 0 {
		return 0, false
	}

	currentStep := now.Unix() / totpPeriodSeconds
	nextStep := int64(0)
	for _, step := range matches {
		if hasLastUsedStep && step <= lastUsedStep {
			continue
		}
		// 只接受当前或更早一步的匹配；未来一步的窗口用于容忍用户设备时钟
		// 稍快，但记录它会让之后的正常码同样可用，因此不推进到未来。
		if step > currentStep {
			continue
		}
		if step > nextStep {
			nextStep = step
		}
	}
	if nextStep == 0 {
		return 0, false
	}
	return nextStep, true
}

// GenerateBackupCodes 生成备用恢复码
func GenerateBackupCodes() ([]string, error) {
	codes := make([]string, BackupCodeCount)

	for i := 0; i < BackupCodeCount; i++ {
		code, err := generateRandomBackupCode()
		if err != nil {
			return nil, err
		}
		codes[i] = code
	}

	return codes, nil
}

// generateRandomBackupCode 生成单个备用码
func generateRandomBackupCode() (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	code := make([]byte, BackupCodeLength)

	for i := range code {
		randomBytes := make([]byte, 1)
		_, err := rand.Read(randomBytes)
		if err != nil {
			return "", err
		}
		code[i] = charset[int(randomBytes[0])%len(charset)]
	}

	// 格式化为 XXXX-XXXX 格式
	return fmt.Sprintf("%s-%s", string(code[:4]), string(code[4:])), nil
}

// ValidateBackupCode 验证备用码格式
func ValidateBackupCode(code string) bool {
	// 移除所有分隔符并转为大写
	cleanCode := strings.ToUpper(strings.ReplaceAll(code, "-", ""))
	if len(cleanCode) != BackupCodeLength {
		return false
	}

	// 检查字符是否合法
	for _, char := range cleanCode {
		if !((char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return false
		}
	}

	return true
}

// NormalizeBackupCode 标准化备用码格式
func NormalizeBackupCode(code string) string {
	cleanCode := strings.ToUpper(strings.ReplaceAll(code, "-", ""))
	if len(cleanCode) == BackupCodeLength {
		return fmt.Sprintf("%s-%s", cleanCode[:4], cleanCode[4:])
	}
	return code
}

// HashBackupCode 对备用码进行哈希
func HashBackupCode(code string) (string, error) {
	normalizedCode := NormalizeBackupCode(code)
	return Password2Hash(normalizedCode)
}

// Get2FAIssuer 获取2FA发行者名称
func Get2FAIssuer() string {
	return SystemName
}

// getEnvOrDefault 获取环境变量或默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// ValidateNumericCode 验证数字验证码格式
func ValidateNumericCode(code string) (string, error) {
	// 移除空格
	code = strings.ReplaceAll(code, " ", "")

	if len(code) != 6 {
		return "", fmt.Errorf("验证码必须是6位数字")
	}

	// 检查是否为纯数字
	if _, err := strconv.Atoi(code); err != nil {
		return "", fmt.Errorf("验证码只能包含数字")
	}

	return code, nil
}

// GenerateQRCodeData 生成二维码数据
func GenerateQRCodeData(secret, username string) string {
	issuer := Get2FAIssuer()
	accountName := fmt.Sprintf("%s (%s)", username, issuer)
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s&issuer=%s&digits=6&period=30",
		issuer, accountName, secret, issuer)
}
