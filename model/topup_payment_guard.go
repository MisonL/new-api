package model

import (
	"github.com/QuantumNous/new-api/common"
)

func ExpirePendingTopUpByTradeNoForPaymentMethod(tradeNo string, expectedProvider string) error {
	return UpdatePendingTopUpStatus(tradeNo, expectedProvider, common.TopUpStatusExpired)
}

func MarkPendingTopUpFailedByTradeNoForPaymentMethod(tradeNo string, expectedProvider string) error {
	return UpdatePendingTopUpStatus(tradeNo, expectedProvider, common.TopUpStatusFailed)
}
