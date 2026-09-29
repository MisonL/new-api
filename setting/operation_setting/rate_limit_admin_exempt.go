package operation_setting

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// RateLimitAdminExemptEnabled exempts administrators (role >= RoleAdminUser)
// from the global web/API rate limits and the per-user search/suggestion rate
// limits. It deliberately does not affect CriticalRateLimit, which protects
// login, registration, password reset and payment endpoints.
var rateLimitAdminExemptEnabled atomic.Bool

func GetRateLimitAdminExempt() bool {
	return rateLimitAdminExemptEnabled.Load()
}

func SetRateLimitAdminExempt(enabled bool) {
	rateLimitAdminExemptEnabled.Store(enabled)
}

// NormalizeRateLimitAdminExempt validates the string form used by the option
// store and the GLOBAL_RATE_LIMIT_ADMIN_EXEMPT environment variable.
func NormalizeRateLimitAdminExempt(value string) (string, error) {
	enabled, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return strconv.FormatBool(enabled), nil
}
