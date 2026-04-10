package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const defaultServerAddressPlaceholder = "http://localhost:3000"

func shouldAutoDeriveServerAddress(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return trimmed == "" || trimmed == defaultServerAddressPlaceholder
}

func resolveServerAddressForRequest(r *http.Request) string {
	configured := strings.TrimSpace(system_setting.ServerAddress)
	if !shouldAutoDeriveServerAddress(configured) {
		return configured
	}

	if derived := strings.TrimSpace(deriveCustomOAuthBaseURLFromRequest(r)); derived != "" {
		return derived
	}

	return configured
}

func shouldPersistDerivedServerAddress(configured string, derived string) bool {
	return shouldAutoDeriveServerAddress(configured) &&
		strings.TrimSpace(derived) != "" &&
		strings.TrimSpace(derived) != strings.TrimSpace(configured)
}

func persistDerivedServerAddress(r *http.Request) error {
	derived := strings.TrimSpace(deriveCustomOAuthBaseURLFromRequest(r))
	if !shouldPersistDerivedServerAddress(system_setting.ServerAddress, derived) {
		return nil
	}

	return model.UpdateOption("ServerAddress", derived)
}
