package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

func newStripeWebhookTestContext(t *testing.T, body string, signature string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/pay/stripe/webhook", strings.NewReader(body))
	if signature != "" {
		ctx.Request.Header.Set("Stripe-Signature", signature)
	}
	return ctx, recorder
}

func TestStripeWebhookRejectsRequestsWhenWebhookSecretIsBlank(t *testing.T) {
	previousSecret := setting.StripeWebhookSecret
	setting.StripeWebhookSecret = ""
	t.Cleanup(func() {
		setting.StripeWebhookSecret = previousSecret
	})

	ctx, recorder := newStripeWebhookTestContext(t, `{}`, "")

	StripeWebhook(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected blank webhook secret to be rejected with 403, got %d", recorder.Code)
	}
}

func TestStripeWebhookRejectsInvalidSignature(t *testing.T) {
	previousSecret := setting.StripeWebhookSecret
	previousAPISecret := setting.StripeApiSecret
	previousPriceID := setting.StripePriceId
	setting.StripeWebhookSecret = "whsec_test_secret"
	setting.StripeApiSecret = "sk_test_secret"
	setting.StripePriceId = "price_test"
	t.Cleanup(func() {
		setting.StripeWebhookSecret = previousSecret
		setting.StripeApiSecret = previousAPISecret
		setting.StripePriceId = previousPriceID
	})

	ctx, recorder := newStripeWebhookTestContext(t, `{}`, "t=12345,v1=invalid")

	StripeWebhook(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid signature to be rejected with 400, got %d", recorder.Code)
	}
}
