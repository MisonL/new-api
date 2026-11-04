package common

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

var RDB *redis.Client
var RedisEnabled = true

func RedisKeyCacheSeconds() int {
	return SyncFrequency
}

// InitRedisClient This function is called after init()
func InitRedisClient() (err error) {
	if os.Getenv("REDIS_CONN_STRING") == "" {
		RedisEnabled = false
		SysLog("REDIS_CONN_STRING not set, Redis is not enabled")
		return nil
	}
	if os.Getenv("SYNC_FREQUENCY") == "" {
		SysLog("SYNC_FREQUENCY not set, use default value 60")
		SyncFrequency = 60
	}
	SysLog("Redis is enabled")
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	opt.PoolSize = GetEnvOrDefault("REDIS_POOL_SIZE", 10)
	RDB = redis.NewClient(opt)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = RDB.Ping(ctx).Result()
	if err != nil {
		FatalLog("Redis ping test failed: " + err.Error())
	}
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis connected to %s", opt.Addr))
		SysLog(fmt.Sprintf("Redis database: %d", opt.DB))
	}
	return err
}

func ParseRedisOption() *redis.Options {
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	return opt
}

func RedisDelKey(key string) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis DEL Key: key=%s", key))
	}
	ctx := context.Background()
	return RDB.Del(ctx, key).Err()
}

func RedisHSetObj(key string, obj interface{}, expiration time.Duration) error {
	return RedisHSetObjSkippingFields(key, obj, expiration, nil)
}

// RedisHSetObjSkippingFields 与 RedisHSetObj 相同，但跳过 skip 中的字段。
// 用于把“只能增量维护”的字段（例如用户额度）从整对象回写里排除，
// 避免旧快照覆盖并发 HINCRBY 的结果。
func RedisHSetObjSkippingFields(key string, obj interface{}, expiration time.Duration, skip map[string]struct{}) error {
	if DebugEnabled {
		// 缓存对象里可能有凭据（UserBase.Setting 的 webhook_secret/gotify_token
		// 等），DEBUG 日志不能原样落盘。
		SysLog(fmt.Sprintf("Redis HSET: key=%s, obj=%s, expiration=%v", key, MaskSensitiveInfo(fmt.Sprintf("%+v", obj)), expiration))
	}
	ctx := context.Background()

	data := make(map[string]interface{})

	// 使用反射遍历结构体字段
	v := reflect.ValueOf(obj).Elem()
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		value := v.Field(i)
		if _, skipped := skip[field.Name]; skipped {
			continue
		}

		// Skip DeletedAt field
		if field.Type.String() == "gorm.DeletedAt" {
			continue
		}

		// 处理指针类型
		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				data[field.Name] = ""
				continue
			}
			value = value.Elem()
		}

		// 处理布尔类型
		if value.Kind() == reflect.Bool {
			data[field.Name] = strconv.FormatBool(value.Bool())
			continue
		}

		// 其他类型直接转换为字符串
		data[field.Name] = fmt.Sprintf("%v", value.Interface())
	}

	txn := RDB.TxPipeline()
	txn.HSet(ctx, key, data)

	// 只有在 expiration 大于 0 时才设置过期时间
	if expiration > 0 {
		txn.Expire(ctx, key, expiration)
	}

	_, err := txn.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute transaction: %w", err)
	}
	return nil
}

var hSetFieldIfAbsentScript = redis.NewScript(`
if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 1 then
  return 0
end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[2])
if tonumber(ARGV[3]) > 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[3])
end
return 1
`)

var hIncrByIfPresentScript = redis.NewScript(`
if redis.call('HEXISTS', KEYS[1], ARGV[1]) == 0 then
  return 0
end
redis.call('HINCRBY', KEYS[1], ARGV[1], ARGV[2])
return 1
`)

// RedisHSetFieldIfAbsent 只在字段不存在时写入，返回是否真的写入。
// 用户额度这类字段由增量扣减维护：用读库得到的旧快照整值回写会覆盖
// 并发 HINCRBY 的结果，把已扣掉的额度在缓存里复活。
func RedisHSetFieldIfAbsent(key, field string, value interface{}, expiration time.Duration) (bool, error) {
	ttlSeconds := int64(0)
	if expiration > 0 {
		ttlSeconds = int64(expiration / time.Second)
	}
	res, err := hSetFieldIfAbsentScript.Run(context.Background(), RDB,
		[]string{key}, field, fmt.Sprintf("%v", value), ttlSeconds).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

// RedisHIncrByIfPresent 只在字段已存在时增量更新，返回是否更新。
// HINCRBY 在空哈希上会写出一个只有增量、没有基数的值（例如 -100），
// 那与真实余额无关；宁可跳过，让下一次读取回源数据库重新建立基数。
func RedisHIncrByIfPresent(key, field string, delta int64) (bool, error) {
	res, err := hIncrByIfPresentScript.Run(context.Background(), RDB,
		[]string{key}, field, delta).Int()
	if err != nil {
		return false, err
	}
	return res == 1, nil
}

func RedisHGetObj(key string, obj interface{}) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HGETALL: key=%s", key))
	}
	ctx := context.Background()

	result, err := RDB.HGetAll(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to load hash from Redis: %w", err)
	}

	if len(result) == 0 {
		return fmt.Errorf("key %s not found in Redis", key)
	}

	// Handle both pointer and non-pointer values
	val := reflect.ValueOf(obj)
	if val.Kind() != reflect.Ptr {
		return fmt.Errorf("obj must be a pointer to a struct, got %T", obj)
	}

	v := val.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("obj must be a pointer to a struct, got pointer to %T", v.Interface())
	}

	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldName := field.Name
		if value, ok := result[fieldName]; ok {
			fieldValue := v.Field(i)

			// Handle pointer types
			if fieldValue.Kind() == reflect.Ptr {
				if value == "" {
					continue
				}
				if fieldValue.IsNil() {
					fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
				}
				fieldValue = fieldValue.Elem()
			}

			// Enhanced type handling for Token struct
			switch fieldValue.Kind() {
			case reflect.String:
				fieldValue.SetString(value)
			case reflect.Int, reflect.Int64:
				intValue, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse int field %s: %w", fieldName, err)
				}
				fieldValue.SetInt(intValue)
			case reflect.Bool:
				boolValue, err := strconv.ParseBool(value)
				if err != nil {
					return fmt.Errorf("failed to parse bool field %s: %w", fieldName, err)
				}
				fieldValue.SetBool(boolValue)
			case reflect.Struct:
				// Special handling for gorm.DeletedAt
				if fieldValue.Type().String() == "gorm.DeletedAt" {
					if value != "" {
						timeValue, err := time.Parse(time.RFC3339, value)
						if err != nil {
							return fmt.Errorf("failed to parse DeletedAt field %s: %w", fieldName, err)
						}
						fieldValue.Set(reflect.ValueOf(gorm.DeletedAt{Time: timeValue, Valid: true}))
					}
				}
			default:
				return fmt.Errorf("unsupported field type: %s for field %s", fieldValue.Kind(), fieldName)
			}
		}
	}

	return nil
}

func RedisHIncrBy(key, field string, delta int64) error {
	if DebugEnabled {
		SysLog(fmt.Sprintf("Redis HINCRBY: key=%s, field=%s, delta=%d", key, field, delta))
	}
	ttlCmd := RDB.TTL(context.Background(), key)
	ttl, err := ttlCmd.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	ctx := context.Background()
	if ttl > 0 {
		txn := RDB.TxPipeline()

		incrCmd := txn.HIncrBy(ctx, key, field, delta)
		if err := incrCmd.Err(); err != nil {
			return err
		}

		txn.Expire(ctx, key, ttl)

		_, err = txn.Exec(ctx)
		return err
	}

	// key 不存在（ttl == -2）或没有过期时间（-1）。此前这里直接 return nil，
	// 调用方据此认为扣减成功，实际什么都没写：缓存额度会长期高于真实值。
	// 重建这个字段，并补上默认 TTL，避免留下永不过期的 key。
	defaultTTL := time.Duration(RedisKeyCacheSeconds()) * time.Second
	if defaultTTL <= 0 {
		defaultTTL = time.Minute
	}
	txn := RDB.TxPipeline()
	txn.HIncrBy(ctx, key, field, delta)
	txn.Expire(ctx, key, defaultTTL)
	_, err = txn.Exec(ctx)
	return err
}

func RedisHSetField(key, field string, value interface{}) error {
	if DebugEnabled {
		// 字段值可能是整段用户设置（含 webhook_secret/gotify_token），先脱敏。
		SysLog(fmt.Sprintf("Redis HSET field: key=%s, field=%s, value=%s", key, field, MaskSensitiveInfo(fmt.Sprintf("%v", value))))
	}
	ttlCmd := RDB.TTL(context.Background(), key)
	ttl, err := ttlCmd.Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("failed to get TTL: %w", err)
	}

	ctx := context.Background()
	if ttl > 0 {
		txn := RDB.TxPipeline()

		hsetCmd := txn.HSet(ctx, key, field, value)
		if err := hsetCmd.Err(); err != nil {
			return err
		}

		txn.Expire(ctx, key, ttl)

		_, err = txn.Exec(ctx)
		return err
	}

	// 与 RedisHIncrBy 同理：key 缺失时静默丢弃会让回写"成功"却没生效。
	defaultTTL := time.Duration(RedisKeyCacheSeconds()) * time.Second
	if defaultTTL <= 0 {
		defaultTTL = time.Minute
	}
	txn := RDB.TxPipeline()
	txn.HSet(ctx, key, field, value)
	txn.Expire(ctx, key, defaultTTL)
	_, err = txn.Exec(ctx)
	return err
}
