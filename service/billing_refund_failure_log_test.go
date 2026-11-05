package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 钱包退款是 quota += N 的非幂等操作，失败后不能重试，因此必须留下可检索的
// 持久化记录：只写 stderr 时，用户少退的预扣额度没有任何对账依据。
type failingRefundFunding struct{}

func (f *failingRefundFunding) Source() string       { return BillingSourceWallet }
func (f *failingRefundFunding) PreConsume(int) error { return nil }
func (f *failingRefundFunding) Settle(int) error     { return nil }
func (f *failingRefundFunding) Refund() error        { return errors.New("wallet refund db error") }

func TestBillingSessionRefundFailureWritesDurableLog(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	info := &relaycommon.RelayInfo{
		UserId:       42,
		TokenId:      7,
		RequestId:    "req-refund-failure",
		IsPlayground: true,
	}
	session := &BillingSession{
		relayInfo:        info,
		funding:          &failingRefundFunding{},
		preConsumedQuota: 500,
		tokenConsumed:    500,
	}

	session.Refund(c)

	var entry model.Log
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := model.LOG_DB.Where("type = ?", model.LogTypeError).First(&entry).Error; err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.Equal(t, model.LogTypeError, entry.Type,
		"退款失败必须留下持久化记录，而不是只写 stderr")
	require.Contains(t, entry.Content, "req-refund-failure", "记录必须带 request_id 便于对账")
	require.Contains(t, entry.Content, "wallet refund db error", "记录必须带上失败原因")
}
