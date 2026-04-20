package model

import "github.com/QuantumNous/new-api/common"

const (
	PaymentProviderStripe = "stripe"
	PaymentProviderCreem  = "creem"
	PaymentProviderWaffo  = "waffo"
	PaymentProviderEpay   = "epay"
)

func paymentMethodMatchesProvider(method string, provider string) bool {
	if method == "" || provider == "" {
		return false
	}

	switch provider {
	case PaymentProviderEpay:
		return method != PaymentProviderStripe && method != PaymentProviderCreem && method != PaymentProviderWaffo
	default:
		return method == provider
	}
}

func commonUsingPostgreSQL() bool {
	return common.UsingPostgreSQL
}
