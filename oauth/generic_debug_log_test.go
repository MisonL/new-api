package oauth

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// captureDebugLog 把 DEBUG 级日志重定向到返回的缓冲区。
func captureDebugLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buffer := &bytes.Buffer{}
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = buffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})
	return buffer
}

func withDebugLogging(t *testing.T) {
	t.Helper()
	original := common.DebugEnabled
	common.DebugEnabled = true
	t.Cleanup(func() { common.DebugEnabled = original })
}

// DEBUG=true 用于排障，但 token 端点的响应体本身就是凭据：把前 500 字节原样
// 写进日志，等于把 access_token/refresh_token/id_token 落到磁盘上的
// oneapi-*.log。日志要保留可诊断性（端点、状态码、scope、错误串），凭据必须脱敏。
func TestGenericOAuthExchangeTokenDebugLogRedactsCredentials(t *testing.T) {
	withDebugLogging(t)
	buffer := captureDebugLog(t)

	const secret = "ya29.super-secret-access-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + secret + `","token_type":"Bearer","refresh_token":"refresh-secret","id_token":"id-secret","scope":"openid"}`))
	}))
	defer srv.Close()

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:          "Generic",
		Slug:          "generic-debug-log-test",
		TokenEndpoint: srv.URL,
	})

	token, err := provider.ExchangeToken(context.Background(), "code", nil)
	if err != nil {
		t.Fatalf("expected exchange to succeed, got %v", err)
	}
	if token.AccessToken != secret {
		t.Fatalf("expected access token %q, got %q", secret, token.AccessToken)
	}

	logged := buffer.String()
	if !strings.Contains(logged, "ExchangeToken response body") {
		t.Fatalf("expected the debug log to keep the response-body diagnostic, got %q", logged)
	}
	for _, leaked := range []string{secret, "refresh-secret", "id-secret"} {
		if strings.Contains(logged, leaked) {
			t.Fatalf("debug log leaked the credential %q: %s", leaked, logged)
		}
	}
	if !strings.Contains(logged, "openid") {
		t.Fatalf("expected non-secret debug context to survive, got %q", logged)
	}
}

// userinfo 响应同样可能带凭据字段（token 回显、client_secret 等），走同一条
// 截断后直写的日志路径。
func TestGenericOAuthUserInfoDebugLogRedactsCredentials(t *testing.T) {
	withDebugLogging(t)
	buffer := captureDebugLog(t)

	const secret = "userinfo-echoed-credential"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"42","name":"Tester","email":"tester@example.com","access_token":"` + secret + `"}`))
	}))
	defer srv.Close()

	provider := NewGenericOAuthProvider(&model.CustomOAuthProvider{
		Name:             "Generic",
		Slug:             "generic-userinfo-debug-log-test",
		UserInfoEndpoint: srv.URL,
		UserIdField:      "sub",
		UsernameField:    "name",
		EmailField:       "email",
	})

	user, err := provider.GetUserInfo(context.Background(), &OAuthToken{AccessToken: "token", TokenType: "Bearer"})
	if err != nil {
		t.Fatalf("expected userinfo to succeed, got %v", err)
	}
	if user.ProviderUserID != "42" {
		t.Fatalf("expected provider user id 42, got %q", user.ProviderUserID)
	}

	logged := buffer.String()
	if !strings.Contains(logged, "GetUserInfo response body") {
		t.Fatalf("expected the debug log to keep the response-body diagnostic, got %q", logged)
	}
	if strings.Contains(logged, secret) {
		t.Fatalf("debug log leaked the credential %q: %s", secret, logged)
	}
}
