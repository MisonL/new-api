package system_setting

import (
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type PasskeySettings struct {
	Enabled              bool   `json:"enabled"`
	RPDisplayName        string `json:"rp_display_name"`
	RPID                 string `json:"rp_id"`
	Origins              string `json:"origins"`
	AllowInsecureOrigin  bool   `json:"allow_insecure_origin"`
	UserVerification     string `json:"user_verification"`
	AttachmentPreference string `json:"attachment_preference"`
}

var defaultPasskeySettings = PasskeySettings{
	Enabled:              false,
	RPDisplayName:        common.SystemName,
	RPID:                 "",
	Origins:              "",
	AllowInsecureOrigin:  false,
	UserVerification:     "preferred",
	AttachmentPreference: "",
}

func init() {
	config.GlobalConfig.Register("passkey", &defaultPasskeySettings)
}

const defaultPasskeyOriginPlaceholder = "http://localhost:3000"

func shouldAutoDerivePasskeyOrigins(origins string) bool {
	trimmed := strings.TrimSpace(origins)
	return trimmed == "" || trimmed == "[]" || trimmed == defaultPasskeyOriginPlaceholder
}

func shouldAutoDerivePasskeyRPID(rpID string) bool {
	trimmed := strings.TrimSpace(rpID)
	return trimmed == "" || trimmed == "localhost" || trimmed == "localhost:3000"
}

func derivePasskeyRPIDFromServerAddress(serverAddr string) string {
	serverAddr = strings.TrimSpace(serverAddr)
	if serverAddr == "" {
		return ""
	}
	if parsed, err := url.Parse(serverAddr); err == nil {
		if host := strings.TrimSpace(parsed.Hostname()); host != "" {
			return host
		}
		if host := strings.TrimSpace(parsed.Host); host != "" {
			return host
		}
	}
	return serverAddr
}

func canDerivePasskeyFromServerAddress(serverAddr string) bool {
	trimmed := strings.TrimSpace(serverAddr)
	return trimmed != "" && trimmed != defaultPasskeyOriginPlaceholder
}

func GetPasskeySettingsForServerAddress(serverAddr string) *PasskeySettings {
	settings := defaultPasskeySettings

	if canDerivePasskeyFromServerAddress(serverAddr) {
		derivedServerAddress := strings.TrimSpace(serverAddr)
		if shouldAutoDerivePasskeyRPID(settings.RPID) {
			settings.RPID = derivePasskeyRPIDFromServerAddress(derivedServerAddress)
		}
		if shouldAutoDerivePasskeyOrigins(settings.Origins) {
			settings.Origins = derivedServerAddress
		}
	}

	return &settings
}

func GetPasskeySettings() *PasskeySettings {
	return GetPasskeySettingsForServerAddress(ServerAddress)
}
