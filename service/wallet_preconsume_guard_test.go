package service

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func setupWalletGuardTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.New(log.New(io.Discard, "", 0), gormlogger.Config{
			LogLevel:                  gormlogger.Warn,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		}),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousRedis := common.RedisEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.RedisEnabled = previousRedis
	})
	return db
}

func newWalletGuardUser(t *testing.T, db *gorm.DB, username string, quota int) *model.User {
	t.Helper()
	user := &model.User{
		Username: username,
		Password: "12345678",
		Role:     1,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    quota,
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

// 钱包预扣费必须走带 SQL 守卫的扣减：准入检查与扣减非原子时，并发请求
// 都能通过"余额足够"的判定，随后各自扣减，把余额扣成负数。
func TestWalletFundingPreConsumeRejectsInsufficientBalance(t *testing.T) {
	db := setupWalletGuardTestDB(t)
	user := newWalletGuardUser(t, db, "wallet-guard-user", 100)

	funding := &WalletFunding{userId: user.Id}
	err := funding.PreConsume(200)

	require.ErrorIs(t, err, model.ErrUserQuotaInsufficient)
	var final model.User
	require.NoError(t, db.First(&final, user.Id).Error)
	require.Equal(t, 100, final.Quota, "a rejected reservation must not change the balance")
}

// 并发预扣费不得透支：1000 的余额只容得下两次 400 的预扣。
func TestWalletFundingConcurrentPreConsumeNeverOverdraws(t *testing.T) {
	db := setupWalletGuardTestDB(t)
	user := newWalletGuardUser(t, db, "wallet-guard-user-concurrent", 1000)

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
	)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			funding := &WalletFunding{userId: user.Id}
			if err := funding.PreConsume(400); err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
				return
			} else {
				require.ErrorIs(t, err, model.ErrUserQuotaInsufficient)
			}
		}()
	}
	wg.Wait()

	var final model.User
	require.NoError(t, db.First(&final, user.Id).Error)
	require.Equal(t, 2, accepted)
	require.GreaterOrEqual(t, final.Quota, 0, "wallet must never go negative, got %d", final.Quota)
}

// 余额不足必须在预扣阶段映射成 403 insufficient_user_quota，而不是 500，
// 否则客户端会把"额度不足"当成服务端故障重试。
func TestBillingSessionPreConsumeMapsInsufficientWalletTo403(t *testing.T) {
	db := setupWalletGuardTestDB(t)
	user := newWalletGuardUser(t, db, "wallet-guard-user-403", 100)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		UserId:       user.Id,
		IsPlayground: true,
		UserSetting:  dto.UserSetting{},
		ChannelMeta:  &relaycommon.ChannelMeta{},
	}
	info.UserQuota = 100

	session := &BillingSession{
		relayInfo: info,
		funding:   &WalletFunding{userId: user.Id},
	}
	apiErr := session.preConsume(c, 200)

	require.NotNil(t, apiErr)
	require.Equal(t, 403, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
}
