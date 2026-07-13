package operation_setting

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

const DefaultUpstreamRateLimitRetryLimit = 1
const MaxUpstreamRateLimitRetryLimit = 10

var upstreamRateLimitRetryLimit atomic.Int64

func init() {
	SetUpstreamRateLimitRetryLimit(DefaultUpstreamRateLimitRetryLimit)
}

func GetUpstreamRateLimitRetryLimit() int {
	return int(upstreamRateLimitRetryLimit.Load())
}

func SetUpstreamRateLimitRetryLimit(limit int) {
	upstreamRateLimitRetryLimit.Store(int64(limit))
}

func NormalizeUpstreamRateLimitRetryLimit(value string) (string, error) {
	limit, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || limit < 0 || limit > MaxUpstreamRateLimitRetryLimit {
		return "", fmt.Errorf("UpstreamRateLimitRetryLimit must be between 0 and %d", MaxUpstreamRateLimitRetryLimit)
	}
	return strconv.Itoa(limit), nil
}
