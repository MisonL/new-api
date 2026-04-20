package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
)

func TestVerifyCreemSignatureRejectsBlankSecretEvenInTestMode(t *testing.T) {
	previousTestMode := setting.CreemTestMode
	setting.CreemTestMode = true
	t.Cleanup(func() {
		setting.CreemTestMode = previousTestMode
	})

	if verifyCreemSignature(`{"id":"evt_1"}`, "deadbeef", "") {
		t.Fatal("expected blank webhook secret to be rejected even in test mode")
	}
}
