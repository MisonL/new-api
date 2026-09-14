package operation_setting

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	DefaultChannelHealthCooldownSeconds   = 30
	DefaultChannelHealthProbeLeaseSeconds = 15
	MinChannelHealthCooldownSeconds       = 1
	MaxChannelHealthCooldownSeconds       = 300
	MinChannelHealthProbeLeaseSeconds     = 1
	MaxChannelHealthProbeLeaseSeconds     = 60
)

func NormalizeChannelHealthEnabled(value string) (string, error) {
	enabled, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("general_setting.channel_health_enabled must be a boolean")
	}
	return strconv.FormatBool(enabled), nil
}

func NormalizeChannelHealthCooldownSeconds(value string) (string, error) {
	return normalizeChannelHealthSeconds(
		"general_setting.channel_health_cooldown_seconds",
		value,
		MinChannelHealthCooldownSeconds,
		MaxChannelHealthCooldownSeconds,
	)
}

func NormalizeChannelHealthProbeLeaseSeconds(value string) (string, error) {
	return normalizeChannelHealthSeconds(
		"general_setting.channel_health_probe_lease_seconds",
		value,
		MinChannelHealthProbeLeaseSeconds,
		MaxChannelHealthProbeLeaseSeconds,
	)
}

func normalizeChannelHealthSeconds(key string, value string, min int, max int) (string, error) {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < min || seconds > max {
		return "", fmt.Errorf("%s must be between %d and %d", key, min, max)
	}
	return strconv.Itoa(seconds), nil
}
