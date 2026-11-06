package common

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

type verificationValue struct {
	code string
	time time.Time
}

const (
	EmailVerificationPurpose = "v"
	PasswordResetPurpose     = "r"

	// 验证码在 Redis 中的键前缀。键里不放明文邮箱：键名可能出现在慢查询与
	// 监控里，用 purpose+key 的 SHA-256 摘要避免泄漏用户邮箱。
	verificationRedisKeyPrefix = "verification_code:"
)

var (
	verificationMutex sync.Mutex
	verificationMap   map[string]verificationValue
	// 内存 map 只是单机/Redis 故障时的回退，不作为容量限制依据；Redis 侧
	// 用键 TTL 控制过期。
	verificationMapMaxSize     = 10
	VerificationValidMinutes   = 10
	verificationRedisOpTimeout = 2 * time.Second
)

func GenerateVerificationCode(length int) string {
	code := uuid.New().String()
	code = strings.Replace(code, "-", "", -1)
	if length == 0 {
		return code
	}
	return code[:length]
}

// verificationRedisClient 返回可用的 Redis 客户端；未启用或未初始化时返回 nil。
func verificationRedisClient() *redis.Client {
	if !RedisEnabled || RDB == nil {
		return nil
	}
	return RDB
}

func verificationRedisKey(key string, purpose string) string {
	sum := sha256.Sum256([]byte(purpose + "\x00" + key))
	return verificationRedisKeyPrefix + hex.EncodeToString(sum[:])
}

func RegisterVerificationCodeWithKey(key string, code string, purpose string) {
	verificationMutex.Lock()
	verificationMap[purpose+key] = verificationValue{
		code: code,
		time: time.Now(),
	}
	if len(verificationMap) > verificationMapMaxSize {
		removeExpiredPairs()
	}
	verificationMutex.Unlock()

	client := verificationRedisClient()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), verificationRedisOpTimeout)
	defer cancel()
	if err := client.Set(ctx, verificationRedisKey(key, purpose), code,
		time.Duration(VerificationValidMinutes)*time.Minute).Err(); err != nil {
		SysLog("failed to store verification code in redis: " + err.Error())
	}
}

func VerifyCodeWithKey(key string, code string, purpose string) bool {
	if client := verificationRedisClient(); client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), verificationRedisOpTimeout)
		stored, err := client.Get(ctx, verificationRedisKey(key, purpose)).Result()
		cancel()
		switch {
		case err == nil:
			return subtle.ConstantTimeCompare([]byte(stored), []byte(code)) == 1
		case errors.Is(err, redis.Nil):
			// Redis 是权威存储：键不存在说明验证码已过期或已被校验消费，
			// 不能再回退到本进程内存里可能残留的副本。
			return false
		default:
			// Redis 故障时退回本进程内存，单实例仍可完成注册/改密。
			SysLog("failed to read verification code from redis: " + err.Error())
		}
	}

	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	value, okay := verificationMap[purpose+key]
	now := time.Now()
	if !okay || int(now.Sub(value.time).Seconds()) >= VerificationValidMinutes*60 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value.code), []byte(code)) == 1
}

func DeleteKey(key string, purpose string) {
	verificationMutex.Lock()
	delete(verificationMap, purpose+key)
	verificationMutex.Unlock()

	client := verificationRedisClient()
	if client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), verificationRedisOpTimeout)
	defer cancel()
	if err := client.Del(ctx, verificationRedisKey(key, purpose)).Err(); err != nil {
		SysLog("failed to delete verification code from redis: " + err.Error())
	}
}

// no lock inside, so the caller must lock the verificationMap before calling!
func removeExpiredPairs() {
	now := time.Now()
	for key := range verificationMap {
		if int(now.Sub(verificationMap[key].time).Seconds()) >= VerificationValidMinutes*60 {
			delete(verificationMap, key)
		}
	}
}

func init() {
	verificationMutex.Lock()
	defer verificationMutex.Unlock()
	verificationMap = make(map[string]verificationValue)
}
