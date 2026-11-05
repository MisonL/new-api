package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// JWT-direct 的上游失败错误此前把响应体原样拼进错误信息：IdP 在错误响应里
// 回带 token、密码或整页 HTML 时，这些内容会经 OAuth 失败页回显给浏览器。
// 与 CAS 校验失败同一口径：折叠空白、遮蔽敏感键值、截断到 200 字节。
func TestJWTDirectUserInfoErrorRedactsUpstreamBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"access_token":"super-secret-value","error":"bad token"}`))
	}))
	defer server.Close()

	provider := NewJWTDirectProvider(&model.CustomOAuthProvider{
		Name:             "Acme SSO",
		Slug:             "acme-sso",
		Enabled:          true,
		UserInfoEndpoint: server.URL,
	})

	_, err := provider.fetchUserInfoClaims(context.Background(), "opaque-token")

	require.Error(t, err)
	require.Contains(t, err.Error(), "userinfo request failed")
	require.Contains(t, err.Error(), "401")
	require.NotContains(t, err.Error(), "super-secret-value",
		"上游响应体中的敏感值不得回显给客户端：%s", err.Error())
}

func TestJWTDirectJWKSErrorRedactsAndTruncatesUpstreamBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`<html><body>id_token: super-secret-value ` + strings.Repeat("x", 600) + `</body></html>`))
	}))
	defer server.Close()

	_, err := fetchJWKSDocument(context.Background(), server.URL)

	require.Error(t, err)
	require.Contains(t, err.Error(), "jwks request failed")
	require.NotContains(t, err.Error(), "super-secret-value",
		"上游响应体中的敏感值不得回显给客户端：%s", err.Error())
	require.LessOrEqual(t, len(err.Error()), 300,
		"错误信息必须截断，不能把整段上游响应体带回客户端：%d", len(err.Error()))
}

func TestJWTDirectTicketAcquireErrorRedactsUpstreamBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"password":"super-secret-value","message":"upstream down"}`))
	}))
	defer server.Close()

	provider := NewJWTDirectProvider(&model.CustomOAuthProvider{
		Name:                      "Acme SSO",
		Slug:                      "acme-sso",
		Enabled:                   true,
		JWTAcquireMode:            model.CustomJWTAcquireModeTicketExchange,
		TicketExchangeURL:         server.URL,
		TicketExchangeMethod:      model.CustomTicketExchangeMethodGET,
		TicketExchangePayloadMode: model.CustomTicketExchangePayloadModeQuery,
	})

	_, err := provider.performTicketAcquireRequest(context.Background(), "ticket-1", "", "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "ticket acquire failed")
	require.NotContains(t, err.Error(), "super-secret-value",
		"上游响应体中的敏感值不得回显给客户端：%s", err.Error())
}
