package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The /api router group has no request-body ingress limit, so a handler that
// buffers the raw body must carry its own ceiling. Before the bound existed,
// an oversized body was read in full and the request only failed later, for a
// reason unrelated to its size.
func TestWaffoWebhookRejectsOversizedBody(t *testing.T) {
	enableWaffoWebhookForTest(t)
	gin.SetMode(gin.TestMode)

	body := bytes.Repeat([]byte("a"), int(maxWaffoWebhookBodyBytes)+1)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", bytes.NewReader(body))

	WaffoWebhook(c)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code,
		"an oversized webhook body must be rejected by the body bound, got %d with body %q", rec.Code, rec.Body.String())
}

// Control: a body within the limit must reach the normal handler flow. With
// dummy credentials that flow ends in SDK initialisation or signature
// verification, so the assertion only pins that the size bound did not fire.
func TestWaffoWebhookAcceptsBodyWithinLimit(t *testing.T) {
	enableWaffoWebhookForTest(t)
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", strings.NewReader(`{"eventType":"PING"}`))

	WaffoWebhook(c)

	require.NotEqual(t, http.StatusRequestEntityTooLarge, rec.Code,
		"a body within the limit must not be rejected by the size bound")
}

// Same class on the Creem subscription pay endpoint. The observable is the
// handler's existing read-error branch: before the bound, an oversized body
// was read and then failed JSON validation instead.
func TestSubscriptionRequestCreemPayRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"x":"` + strings.Repeat("a", int(maxCreemPayRequestBodyBytes)) + `"}`
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/creem/pay", strings.NewReader(body))

	SubscriptionRequestCreemPay(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "read query error",
		"an oversized pay request must fail on the bounded read, got %q", rec.Body.String())
}

// Control: a small body passes the read and fails later on validation, which
// proves the bound is not rejecting legitimate payloads.
func TestSubscriptionRequestCreemPayAcceptsBodyWithinLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/subscription/creem/pay", strings.NewReader(`{"x":"y"}`))

	SubscriptionRequestCreemPay(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "read query error",
		"a small body must pass the bounded read")
	require.Contains(t, rec.Body.String(), "参数错误")
}

func enableWaffoWebhookForTest(t *testing.T) {
	t.Helper()
	originalEnabled, originalSandbox := setting.WaffoEnabled, setting.WaffoSandbox
	originalAPIKey, originalPrivateKey, originalPublicCert := setting.WaffoApiKey, setting.WaffoPrivateKey, setting.WaffoPublicCert
	t.Cleanup(func() {
		setting.WaffoEnabled, setting.WaffoSandbox = originalEnabled, originalSandbox
		setting.WaffoApiKey, setting.WaffoPrivateKey, setting.WaffoPublicCert = originalAPIKey, originalPrivateKey, originalPublicCert
	})
	setting.WaffoEnabled = true
	setting.WaffoSandbox = false
	setting.WaffoApiKey = "api"
	setting.WaffoPrivateKey = "private"
	setting.WaffoPublicCert = "public"
}
