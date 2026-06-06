package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

var casTicketGuardTestMu sync.Mutex

// resetCASTicketGuardForTest resets the global casTicketGuard. DO NOT use t.Parallel()
// in tests that call this helper because the guard is process-global state.
func resetCASTicketGuardForTest(t *testing.T) {
	t.Helper()
	casTicketGuardTestMu.Lock()
	previous := casTicketGuard
	t.Cleanup(func() {
		casTicketGuard = previous
		casTicketGuardTestMu.Unlock()
	})
	t.Log("resetCASTicketGuardForTest resets global casTicketGuard; do not use t.Parallel() with these tests")
	casTicketGuard = newInMemoryCASTicketGuard(casTicketReplayTTL)
}

func expireInMemoryCASTicketForTest(guard *inMemoryCASTicketGuard, key string) {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	guard.entries[key] = time.Now().Add(-time.Second)
}

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

func createCASProviderWithServiceURLForTest(t *testing.T, validateURL string, serviceURL string) *model.CustomOAuthProvider {
	t.Helper()

	provider := createCASProviderForTest(t, validateURL)
	provider.ServiceURL = serviceURL
	if err := model.UpdateCustomOAuthProvider(provider); err != nil {
		t.Fatalf("failed to update cas provider service url: %v", err)
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

func TestHandleCustomOAuthCASStartFallsBackToRequestOriginWhenConfigured(t *testing.T) {
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
	previousForwardEnabled, hadForwardEnabled := common.OptionMap[customOAuthTrustForwardedHeadersOption]
	previousTrustedCIDRs, hadTrustedCIDRs := common.OptionMap[customOAuthTrustedProxyCIDRsOption]
	common.OptionMap["ServerAddress"] = ""
	common.OptionMap[customOAuthTrustForwardedHeadersOption] = "true"
	common.OptionMap[customOAuthTrustedProxyCIDRsOption] = "127.0.0.1/32"
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		system_setting.ServerAddress = previousServerAddress
		common.OptionMapRWMutex.Lock()
		if hadConfiguredAddress {
			common.OptionMap["ServerAddress"] = previousConfiguredAddress
		} else {
			delete(common.OptionMap, "ServerAddress")
		}
		if hadForwardEnabled {
			common.OptionMap[customOAuthTrustForwardedHeadersOption] = previousForwardEnabled
		} else {
			delete(common.OptionMap, customOAuthTrustForwardedHeadersOption)
		}
		if hadTrustedCIDRs {
			common.OptionMap[customOAuthTrustedProxyCIDRsOption] = previousTrustedCIDRs
		} else {
			delete(common.OptionMap, customOAuthTrustedProxyCIDRsOption)
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
	resetCASTicketGuardForTest(t)
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

func TestHandleCustomOAuthCASCallbackRejectsReplayTicketBeforeValidation(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	resetCASTicketGuardForTest(t)
	handlerErrors := newAsyncHandlerErrorSink(2)
	defer handlerErrors.failIfAny(t)

	var validationCalls int32
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&validationCalls, 1) > 1 {
			handlerErrors.reportf("expected replay ticket to be rejected before upstream validation")
			http.Error(w, "unexpected replay validation", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationSuccess>
    <cas:user>cas-user-replay</cas:user>
    <cas:attributes>
      <cas:loginid>cas-user-replay</cas:loginid>
      <cas:userName>CAS Replay User</cas:userName>
      <cas:mailbox>cas-replay@example.com</cas:mailbox>
      <cas:group>engineering</cas:group>
    </cas:attributes>
  </cas:authenticationSuccess>
</cas:serviceResponse>`))
	}))
	defer validationServer.Close()

	createCASProviderForTest(t, validationServer.URL)

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
	callbackURL := server.URL + "/api/auth/external/acme-sso/cas/callback?ticket=ST-REPLAY-1&state=" + state

	firstResp, err := client.Get(callbackURL)
	if err != nil {
		t.Fatalf("failed to execute first cas callback: %v", err)
	}
	defer firstResp.Body.Close()
	var firstResponse oauthJWTAPIResponse
	if err := common.DecodeJson(firstResp.Body, &firstResponse); err != nil {
		t.Fatalf("failed to decode first cas callback response: %v", err)
	}
	if !firstResponse.Success {
		t.Fatalf("expected first cas callback success, got %s", firstResponse.Message)
	}

	secondResp, err := client.Get(callbackURL)
	if err != nil {
		t.Fatalf("failed to execute replay cas callback: %v", err)
	}
	defer secondResp.Body.Close()
	var secondResponse oauthJWTAPIResponse
	if err := common.DecodeJson(secondResp.Body, &secondResponse); err != nil {
		t.Fatalf("failed to decode replay cas callback response: %v", err)
	}
	if secondResponse.Success {
		t.Fatal("expected replay cas callback to fail")
	}
	if got := atomic.LoadInt32(&validationCalls); got != 1 {
		t.Fatalf("expected one upstream validation call, got %d", got)
	}
}

func TestHandleCustomOAuthCASCallbackReleasesTicketWhenLocalLoginFails(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	resetCASTicketGuardForTest(t)
	handlerErrors := newAsyncHandlerErrorSink(2)
	defer handlerErrors.failIfAny(t)

	disabledUser := createUserWithEmailForTest(t, "cas-disabled-merge", "cas-disabled@example.com")
	disabledUser.Status = common.UserStatusDisabled
	if err := model.DB.Model(disabledUser).Update("status", common.UserStatusDisabled).Error; err != nil {
		t.Fatalf("failed to disable merge target user: %v", err)
	}

	var validationCalls int32
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&validationCalls, 1)
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationSuccess>
    <cas:user>cas-disabled-replay</cas:user>
    <cas:attributes>
      <cas:loginid>cas-disabled-replay</cas:loginid>
      <cas:userName>CAS Disabled User</cas:userName>
      <cas:mailbox>cas-disabled@example.com</cas:mailbox>
      <cas:group>engineering</cas:group>
    </cas:attributes>
  </cas:authenticationSuccess>
</cas:serviceResponse>`))
	}))
	defer validationServer.Close()

	provider := createCASProviderForTest(t, validationServer.URL)
	provider.AutoMergeByEmail = true
	if err := model.UpdateCustomOAuthProvider(provider); err != nil {
		t.Fatalf("failed to enable cas email merge: %v", err)
	}

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
	callbackURL := server.URL + "/api/auth/external/acme-sso/cas/callback?ticket=ST-RESOLVED-LOCAL-FAIL&state=" + state

	firstResp, err := client.Get(callbackURL)
	if err != nil {
		t.Fatalf("failed to execute first cas callback: %v", err)
	}
	defer firstResp.Body.Close()
	var firstResponse oauthJWTAPIResponse
	if err := common.DecodeJson(firstResp.Body, &firstResponse); err != nil {
		t.Fatalf("failed to decode first cas callback response: %v", err)
	}
	if firstResponse.Success {
		t.Fatal("expected disabled merged user cas callback to fail")
	}
	if model.IsProviderUserIdTaken(provider.Id, "cas-disabled-replay") {
		t.Fatal("expected disabled merged user not to receive cas binding")
	}

	secondResp, err := client.Get(callbackURL)
	if err != nil {
		t.Fatalf("failed to execute replay cas callback: %v", err)
	}
	defer secondResp.Body.Close()
	var secondResponse oauthJWTAPIResponse
	if err := common.DecodeJson(secondResp.Body, &secondResponse); err != nil {
		t.Fatalf("failed to decode replay cas callback response: %v", err)
	}
	if secondResponse.Success {
		t.Fatal("expected retried local login to still fail for disabled user")
	}
	if got := atomic.LoadInt32(&validationCalls); got != 2 {
		t.Fatalf("expected failed local login to release ticket for a retry, got %d upstream validation calls", got)
	}
}

func TestReserveCASTicketFailsWhenRedisClientMissing(t *testing.T) {
	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	releaseTicket, err := reserveCASTicket(1, "ST-MISSING-REDIS", "https://example.com/callback")
	if err == nil {
		if releaseTicket != nil {
			releaseTicket()
		}
		t.Fatal("expected missing redis client to fail")
	}
	if isCASTicketReplayError(err) {
		t.Fatalf("expected guard infrastructure error, got replay error: %v", err)
	}
}

func TestReserveCASTicketUsesRedisReservation(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer server.Close()

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	releaseTicket, err := reserveCASTicket(1, "ST-REDIS-1", "https://example.com/callback")
	if err != nil {
		t.Fatalf("expected first redis reservation to succeed: %v", err)
	}
	releaseReplay, err := reserveCASTicket(1, "ST-REDIS-1", "https://example.com/callback")
	if err == nil {
		releaseReplay()
		t.Fatal("expected duplicate redis reservation to fail")
	}
	if !isCASTicketReplayError(err) {
		t.Fatalf("expected replay error, got %v", err)
	}

	releaseTicket()
	releaseAfterRelease, err := reserveCASTicket(1, "ST-REDIS-1", "https://example.com/callback")
	if err != nil {
		t.Fatalf("expected redis reservation after release to succeed: %v", err)
	}
	releaseAfterRelease()
}

func TestReserveCASTicketRedisReleaseUsesOriginalClient(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer server.Close()

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()

	otherServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start replacement miniredis: %v", err)
	}
	defer otherServer.Close()
	otherClient := redis.NewClient(&redis.Options{Addr: otherServer.Addr()})
	defer otherClient.Close()

	previousRedisEnabled := common.RedisEnabled
	previousRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = client
	t.Cleanup(func() {
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRDB
	})

	releaseTicket, err := reserveCASTicket(1, "ST-REDIS-SWITCH", "https://example.com/callback")
	if err != nil {
		t.Fatalf("expected redis reservation to succeed: %v", err)
	}

	common.RDB = otherClient
	releaseTicket()

	common.RDB = client
	releaseAfterRelease, err := reserveCASTicket(1, "ST-REDIS-SWITCH", "https://example.com/callback")
	if err != nil {
		t.Fatalf("expected original redis reservation to be released: %v", err)
	}
	releaseAfterRelease()
}

func TestInMemoryCASTicketReleaseDoesNotRemoveNewReservation(t *testing.T) {
	guard := newInMemoryCASTicketGuard(time.Hour)
	releaseExpired, err := guard.reserve("cas-ticket-test-key")
	if err != nil {
		t.Fatalf("expected first reservation to succeed: %v", err)
	}

	expireInMemoryCASTicketForTest(guard, "cas-ticket-test-key")

	releaseCurrent, err := guard.reserve("cas-ticket-test-key")
	if err != nil {
		t.Fatalf("expected new reservation after expiry to succeed: %v", err)
	}
	releaseExpired()

	releaseReplay, err := guard.reserve("cas-ticket-test-key")
	if err == nil {
		releaseReplay()
		t.Fatal("expected old release callback to preserve current reservation")
	}
	if !isCASTicketReplayError(err) {
		t.Fatalf("expected replay error, got %v", err)
	}

	releaseCurrent()
	releaseAfterCurrent, err := guard.reserve("cas-ticket-test-key")
	if err != nil {
		t.Fatalf("expected current release callback to remove reservation: %v", err)
	}
	releaseAfterCurrent()
}

func TestHandleCustomOAuthCASCallbackUsesConfiguredServiceURLWithState(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	resetCASTicketGuardForTest(t)
	handlerErrors := newAsyncHandlerErrorSink(2)
	defer handlerErrors.failIfAny(t)

	expectedServicePrefix := "https://sso.example.com/callback"
	validationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("ticket"); got != "ST-CAS-456" {
			handlerErrors.reportf("expected ticket query param ST-CAS-456, got %q", got)
			http.Error(w, "unexpected ticket query param", http.StatusBadRequest)
			return
		}
		serviceValue := r.URL.Query().Get("service")
		if !strings.HasPrefix(serviceValue, expectedServicePrefix) {
			handlerErrors.reportf("expected configured service url prefix %q, got %q", expectedServicePrefix, serviceValue)
			http.Error(w, "unexpected service callback url", http.StatusBadRequest)
			return
		}
		if !strings.Contains(serviceValue, "state=") {
			handlerErrors.reportf("expected configured service url to include state, got %q", serviceValue)
			http.Error(w, "missing state in service callback url", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
  <cas:authenticationSuccess>
    <cas:user>cas-user-2</cas:user>
    <cas:attributes>
      <cas:loginid>cas-user-2</cas:loginid>
      <cas:userName>CAS User Two</cas:userName>
      <cas:mailbox>cas-user-2@example.com</cas:mailbox>
      <cas:group>engineering</cas:group>
    </cas:attributes>
  </cas:authenticationSuccess>
</cas:serviceResponse>`))
	}))
	defer validationServer.Close()

	provider := createCASProviderWithServiceURLForTest(
		t,
		validationServer.URL,
		expectedServicePrefix,
	)

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
		server.URL+"/api/auth/external/acme-sso/cas/callback?ticket=ST-CAS-456&state="+state,
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
		t.Fatalf("expected cas callback success with configured service url, got message: %s", response.Message)
	}
	if !model.IsProviderUserIdTaken(provider.Id, "cas-user-2") {
		t.Fatal("expected cas binding to be created for configured service url")
	}
}

func TestHandleCustomOAuthCASCallbackRejectsInvalidState(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	resetCASTicketGuardForTest(t)
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

func TestHandleCustomOAuthCASCallbackRejectsMissingTicket(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)
	resetCASTicketGuardForTest(t)
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
	state := fetchOAuthStateForTest(t, client, server.URL)

	req, err := http.NewRequest(
		http.MethodGet,
		server.URL+"/api/auth/external/acme-sso/cas/callback?state="+state,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to build missing-ticket cas callback request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to execute missing-ticket cas callback request: %v", err)
	}
	defer resp.Body.Close()

	var response oauthJWTAPIResponse
	if err := common.DecodeJson(resp.Body, &response); err != nil {
		t.Fatalf("failed to decode missing-ticket cas callback response: %v", err)
	}
	if response.Success {
		t.Fatal("expected missing-ticket cas callback to fail")
	}
}
