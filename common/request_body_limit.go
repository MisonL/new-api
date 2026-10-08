package common

import "github.com/QuantumNous/new-api/constant"

// defaultAnonymousRequestBodyLimitKB is the ceiling applied to anonymous
// request bodies when ANONYMOUS_REQUEST_BODY_LIMIT_KB is negative, so a typo
// cannot silently remove the protection.
const defaultAnonymousRequestBodyLimitKB = 512

// GetAnonymousRequestBodyLimitBytes returns the byte ceiling for request
// bodies on routes reachable without authentication. A negative setting falls
// back to the default, zero disables the limit, and a positive value is used
// as-is.
func GetAnonymousRequestBodyLimitBytes() int64 {
	limitKB := constant.AnonymousRequestBodyLimitKB
	if limitKB < 0 {
		limitKB = defaultAnonymousRequestBodyLimitKB
	}
	return int64(limitKB) << 10
}
