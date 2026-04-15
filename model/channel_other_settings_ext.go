package model

import "github.com/QuantumNous/new-api/common"

type responsesBootstrapRecoveryOtherSettings struct {
	Enabled bool `json:"responses_stream_bootstrap_recovery_enabled"`
}

func IsResponsesBootstrapRecoveryEnabledInOtherSettings(raw string) bool {
	if raw == "" {
		return false
	}
	settings := responsesBootstrapRecoveryOtherSettings{}
	if err := common.UnmarshalJsonStr(raw, &settings); err != nil {
		return false
	}
	return settings.Enabled
}

func IsChannelResponsesBootstrapRecoveryEnabled(channel *Channel) bool {
	if channel == nil {
		return false
	}
	return IsResponsesBootstrapRecoveryEnabledInOtherSettings(channel.OtherSettings)
}
