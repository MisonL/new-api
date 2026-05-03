package model

func CompleteSubscriptionOrderWithPaymentMethod(tradeNo string, providerPayload string, expectedProvider string) error {
	return CompleteSubscriptionOrder(tradeNo, providerPayload, expectedProvider, "")
}

func ExpireSubscriptionOrderWithPaymentMethod(tradeNo string, expectedProvider string) error {
	return ExpireSubscriptionOrder(tradeNo, expectedProvider)
}
