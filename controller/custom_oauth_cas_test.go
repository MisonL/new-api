package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

func createCASProviderForTest(t *testing.T, validateURL string) *model.CustomOAuthProvider {
	t.Helper()

	provider := &model.CustomOAuthProvider{
		Name:                   "CAS SSO",
		Slug:                   "acme-sso",
		Kind:                   model.CustomOAuthProviderKindCAS,
		Enabled:                true,
		CASServerURL:           "https://cas.example.com/cas",
		ValidateURL:            validateURL,
		UserIdField:            "authenticationSuccess.user",
		UsernameField:          "authenticationSuccess.attributes.loginid",
		DisplayNameField:       "authenticationSuccess.attributes.userName",
		EmailField:             "authenticationSuccess.attributes.mailbox",
		GroupField:             "authenticationSuccess.attributes.group",
		GroupMapping:           `{"engineering":"vip"}`,
		RoleField:              "authenticationSuccess.attributes.role",
		RoleMapping:            `{"platform-admin":"admin"}`,
		AutoRegister:           true,
		GroupMappingMode:       model.CustomOAuthMappingModeExplicitOnly,
		RoleMappingMode:        model.CustomOAuthMappingModeExplicitOnly,
		SyncUsernameOnLogin:    true,
		SyncDisplayNameOnLogin: true,
		SyncEmailOnLogin:       true,
	}
	if err := model.CreateCustomOAuthProvider(provider); err != nil {
		t.Fatalf("failed to create cas provider: %v", err)
	}
	return provider
}

func TestHandleCustomOAuthCASStartRedirectsToCASLogin(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	createCASProviderForTest(t, "https://cas.example.com/cas/serviceValidate")

	router := newCustomOAuthJWTRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	previousServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = server.URL
	t.Cleanup(func() {
		system_setting.ServerAddress = previousServerAddress
	})

	client := newTestHTTPClient(t)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	state := fetchOAuthStateForTest(t, client, server.URL)

	req, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/auth/external/acme-sso/cas/start?state="+state,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to build cas start request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to execute cas start request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	if !strings.HasPrefix(location, "https://cas.example.com/cas/login?") {
		t.Fatalf("unexpected cas login redirect: %q", location)
	}
	if !strings.Contains(location, "service=") {
		t.Fatalf("expected service query param in cas redirect, got %q", location)
	}
	expectedService := url.QueryEscape(server.URL + "/oauth/acme-sso?state=" + state)
	if !strings.Contains(location, expectedService) {
		t.Fatalf("expected callback service url in cas redirect, got %q", location)
	}
}

func TestHandleCustomOAuthCASStartFallsBackToRequestOriginWhenServerAddressUnset(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	createCASProviderForTest(t, "https://cas.example.com/cas/serviceValidate")

	router := newCustomOAuthJWTRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	previousServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = ""
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousConfiguredAddress, hadConfiguredAddress := common.OptionMap["ServerAddress"]
	common.OptionMap["ServerAddress"] = ""
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		system_setting.ServerAddress = previousServerAddress
		common.OptionMapRWMutex.Lock()
		if hadConfiguredAddress {
			common.OptionMap["ServerAddress"] = previousConfiguredAddress
		} else {
			delete(common.OptionMap, "ServerAddress")
		}
		common.OptionMapRWMutex.Unlock()
	})

	client := newTestHTTPClient(t)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	state := fetchOAuthStateForTest(t, client, server.URL)

	req, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/auth/external/acme-sso/cas/start?state="+state,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to build cas start request: %v", err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "login.example.com")
	req.Header.Set("X-Forwarded-Prefix", "/new-api")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to execute cas start request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	expectedService := url.QueryEscape("https://login.example.com/new-api/oauth/acme-sso?state=" + state)
	if !strings.Contains(location, expectedService) {
		t.Fatalf("expected request-derived callback service url in cas redirect, got %q", location)
	}
}

func TestHandleCustomOAuthCASCallbackCreatesUser(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	handlerErrors := newAsyncHandlerErrorSink(2)
	defer handlerErrors.failIfAny(t)

	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ticket"); got != "ST-CAS-123" {
			handlerErrors.reportf("expected ticket query param ST-CAS-123, got %q", got)
			http.Error(w, "unexpected ticket query param", http.StatusBadRequest)
			return
		}
		if got := r.URL.Query().Get("service"); !strings.Contains(got, "/oauth/acme-sso?state=") {
			handlerErrors.reportf("expected service callback url to contain oauth callback, got %q", got)
			http.Error(w, "unexpected service callback url", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationSuccess>
    <cas:user>cas-user-1</cas:user>
    <cas:attributes>
      <cas:loginid>cas-user</cas:loginid>
      <cas:userName>CAS User</cas:userName>
      <cas:mailbox>cas-user@example.com</cas:mailbox>
      <cas:group>engineering</cas:group>
      <cas:role>platform-admin</cas:role>
    </cas:attributes>
  </cas:authenticationSuccess>
</cas:serviceResponse>`))
	}))
	defer validationServer.Close()

	provider := createCASProviderForTest(t, validationServer.URL)

	router := newCustomOAuthJWTRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	previousServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = server.URL
	t.Cleanup(func() {
		system_setting.ServerAddress = previousServerAddress
	})

	client := newTestHTTPClient(t)
	state := fetchOAuthStateForTest(t, client, server.URL)

	req, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/auth/external/acme-sso/cas/callback?ticket=ST-CAS-123&state="+state,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to build cas callback request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to execute cas callback request: %v", err)
	}
	defer resp.Body.Close()

	var response oauthJWTAPIResponse
	if err := common.DecodeJson(resp.Body, &response); err != nil {
		t.Fatalf("failed to decode cas callback response: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected cas callback success, got message: %s", response.Message)
	}

	var loginData oauthJWTLoginResponse
	if err := common.Unmarshal(response.Data, &loginData); err != nil {
		t.Fatalf("failed to decode cas callback login payload: %v", err)
	}
	if loginData.Username != "cas-user" || loginData.Role != common.RoleAdminUser || loginData.Group != "vip" {
		t.Fatalf("unexpected cas login response: %+v", loginData)
	}
	if !model.IsProviderUserIdTaken(provider.Id, "cas-user-1") {
		t.Fatal("expected cas binding to be created")
	}
}

func TestHandleCustomOAuthCASCallbackRejectsInvalidState(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	createCASProviderForTest(t, "https://cas.example.com/cas/serviceValidate")

	router := newCustomOAuthJWTRouter(t)
	server := httptest.NewServer(router)
	defer server.Close()

	previousServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = server.URL
	t.Cleanup(func() {
		system_setting.ServerAddress = previousServerAddress
	})

	client := newTestHTTPClient(t)
	_ = fetchOAuthStateForTest(t, client, server.URL)

	req, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/auth/external/acme-sso/cas/callback?ticket=ST-CAS-123&state=bad-state",
		nil,
	)
	if err != nil {
		t.Fatalf("failed to build invalid-state cas callback request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to execute invalid-state cas callback request: %v", err)
	}
	defer resp.Body.Close()

	var response oauthJWTAPIResponse
	if err := common.DecodeJson(resp.Body, &response); err != nil {
		t.Fatalf("failed to decode invalid-state cas callback response: %v", err)
	}
	if response.Success {
		t.Fatal("expected invalid-state cas callback to fail")
	}
}
