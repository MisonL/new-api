package model

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
)

// UserBase struct remains the same as it represents the cached data structure
type UserBase struct {
	Id       int    `json:"id"`
	Group    string `json:"group"`
	Email    string `json:"email"`
	Quota    int    `json:"quota"`
	Role     int    `json:"role"`
	Status   int    `json:"status"`
	Username string `json:"username"`
	Setting  string `json:"setting"`
	// SessionEpoch 参与会话吊销判定，必须随缓存一起回读，否则密码变更后
	// 在缓存 TTL 内仍会用旧的世代放行被吊销的会话。
	SessionEpoch int64 `json:"session_epoch"`
}

func (user *UserBase) WriteContext(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyUserGroup, user.Group)
	common.SetContextKey(c, constant.ContextKeyUserQuota, user.Quota)
	common.SetContextKey(c, constant.ContextKeyUserStatus, user.Status)
	common.SetContextKey(c, constant.ContextKeyUserEmail, user.Email)
	common.SetContextKey(c, constant.ContextKeyUserName, user.Username)
	common.SetContextKey(c, constant.ContextKeyUserSetting, user.GetSetting())
}

func (user *UserBase) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

// getUserCacheKey returns the key for user cache
func getUserCacheKey(userId int) string {
	return fmt.Sprintf("user:%d", userId)
}

// invalidateUserCache clears user cache
func invalidateUserCache(userId int) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisDelKey(getUserCacheKey(userId))
}

// InvalidateUserCache is the exported version of invalidateUserCache.
// 供 controller 等上层包在用户状态变更（如禁用、删除、角色变更）后主动清理缓存。
func InvalidateUserCache(userId int) error {
	return invalidateUserCache(userId)
}

// updateUserCache updates all user cache fields using hash.
//
// Quota 不在整对象回写里：它由 HINCRBY 增量维护，用读库快照覆盖会把
// 已扣减的额度在缓存里复活（最长一个 TTL）。额度只在字段缺失时用
// HSETNX 建立基数，已有值一律保留。
//
// SessionEpoch 同理不能整对象回写，但它需要的是「只增不减」而不是「只写一次」：
// 世代只会在改密码时递增，而这里可能写入调用方更早读到的旧快照。整值覆盖会把
// 已递增的世代调低，令刚被吊销的旧会话重新通过 stored == current 判定。
func updateUserCache(user User) error {
	if !common.RedisEnabled {
		return nil
	}

	base := user.ToBaseUser()
	ttl := time.Duration(common.RedisKeyCacheSeconds()) * time.Second
	if err := common.RedisHSetObjSkippingFields(
		getUserCacheKey(user.Id),
		base,
		ttl,
		map[string]struct{}{"Quota": {}, "SessionEpoch": {}},
	); err != nil {
		return err
	}
	if _, err := common.RedisHSetFieldIfAbsent(getUserCacheKey(user.Id), "Quota", base.Quota, ttl); err != nil {
		return err
	}
	if _, err := common.RedisHSetFieldIfGreater(getUserCacheKey(user.Id), "SessionEpoch", base.SessionEpoch, ttl); err != nil {
		return err
	}
	return nil
}

// GetUserCache gets complete user cache from hash
func GetUserCache(userId int) (userCache *UserBase, err error) {
	var user *User
	var fromDB bool
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) && user != nil {
			gopool.Go(func() {
				if err := updateUserCache(*user); err != nil {
					common.SysLog("failed to update user status cache: " + err.Error())
				}
			})
		}
	}()

	// Try getting from Redis first
	userCache, err = cacheGetUserBase(userId)
	if err == nil {
		return userCache, nil
	}

	// If Redis fails, get from DB
	fromDB = true
	user, err = GetUserById(userId, false)
	if err != nil {
		return nil, err // Return nil and error if DB lookup fails
	}

	// Create cache object from user data
	userCache = &UserBase{
		Id:           user.Id,
		Group:        user.Group,
		Quota:        user.Quota,
		Role:         user.Role,
		Status:       user.Status,
		Username:     user.Username,
		Setting:      user.Setting,
		Email:        user.Email,
		SessionEpoch: user.SessionEpoch,
	}

	return userCache, nil
}

func cacheGetUserBase(userId int) (*UserBase, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	cacheFields, err := common.RDB.HGetAll(context.Background(), getUserCacheKey(userId)).Result()
	if err != nil {
		return nil, err
	}
	if len(cacheFields) == 0 {
		return nil, fmt.Errorf("user cache is empty")
	}
	if _, ok := cacheFields["Role"]; !ok {
		return nil, fmt.Errorf("user cache is missing role")
	}
	var userCache UserBase
	// Try getting from Redis first
	err = common.RedisHGetObj(getUserCacheKey(userId), &userCache)
	if err != nil {
		return nil, err
	}
	if userCache.Id <= 0 || userCache.Username == "" || !common.IsValidateRole(userCache.Role) {
		return nil, fmt.Errorf("user cache is incomplete")
	}
	return &userCache, nil
}

// Add atomic quota operations using hash fields.
//
// 字段缺失时跳过而不是 HINCRBY：空哈希上的 HINCRBY 会创建只有增量的值
// （例如 -100），与真实余额无关。跳过之后下一次 GetUserQuota 回源数据库，
// 由 updateUserQuotaCache 用 HSETNX 建立正确基数。
func cacheIncrUserQuota(userId int, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	_, err := common.RedisHIncrByIfPresent(getUserCacheKey(userId), "Quota", delta)
	return err
}

func cacheDecrUserQuota(userId int, delta int64) error {
	return cacheIncrUserQuota(userId, -delta)
}

// Helper functions to get individual fields if needed
func getUserGroupCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Group, nil
}

func getUserQuotaCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Quota, nil
}

func getUserStatusCache(userId int) (int, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return 0, err
	}
	return cache.Status, nil
}

func getUserNameCache(userId int) (string, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return "", err
	}
	return cache.Username, nil
}

func getUserSettingCache(userId int) (dto.UserSetting, error) {
	cache, err := GetUserCache(userId)
	if err != nil {
		return dto.UserSetting{}, err
	}
	return cache.GetSetting(), nil
}

// New functions for individual field updates
func updateUserStatusCache(userId int, status bool) error {
	if !common.RedisEnabled {
		return nil
	}
	statusInt := common.UserStatusEnabled
	if !status {
		statusInt = common.UserStatusDisabled
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Status", fmt.Sprintf("%d", statusInt))
}

// updateUserQuotaCache 用读库快照建立额度基数，但只在字段缺失时写：
// 已有值可能已被并发 HINCRBY 调整，覆盖会把已扣额度复活。
func updateUserQuotaCache(userId int, quota int) error {
	if !common.RedisEnabled {
		return nil
	}
	ttl := time.Duration(common.RedisKeyCacheSeconds()) * time.Second
	_, err := common.RedisHSetFieldIfAbsent(getUserCacheKey(userId), "Quota", fmt.Sprintf("%d", quota), ttl)
	return err
}

func updateUserGroupCache(userId int, group string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Group", group)
}

func UpdateUserGroupCache(userId int, group string) error {
	return updateUserGroupCache(userId, group)
}

func updateUserNameCache(userId int, username string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Username", username)
}

func updateUserSettingCache(userId int, setting string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHSetField(getUserCacheKey(userId), "Setting", setting)
}

// GetUserLanguage returns the user's language preference from cache
// Uses the existing GetUserCache mechanism for efficiency
func GetUserLanguage(userId int) string {
	userCache, err := GetUserCache(userId)
	if err != nil {
		return ""
	}
	return userCache.GetSetting().Language
}

// InvalidateUserQuotaAfterRecharge 在充值/兑换直接写库后清掉用户缓存。
//
// 这些路径在事务里执行 `quota + ?`，不经过 IncreaseUserQuota 的缓存增量，
// 缓存里最长 60 秒（SyncFrequency）仍是充值前的值：用户付款成功后立刻发请求
// 会被 PreConsumeQuota 判为"额度不足"。这里在事务提交后失效整条哈希，
// 下一次读取回源到已提交的新值。
func InvalidateUserQuotaAfterRecharge(userId int) {
	if userId <= 0 {
		return
	}
	if err := InvalidateUserCache(userId); err != nil {
		common.SysLog("failed to invalidate user cache after recharge: " + err.Error())
	}
}
