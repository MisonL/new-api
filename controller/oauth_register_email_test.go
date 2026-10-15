package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newOAuthRegisterTestContext builds a gin context carrying a real session
// store, which findOrCreateOAuthUserWithOptions reads for the affiliate code.
func newOAuthRegisterTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/test/oauth-register", nil)
	store := cookie.NewStore([]byte("oauth-register-test-secret"))
	sessions.Sessions("session", store)(c)
	return c
}

func registerOAuthUserForTest(t *testing.T, email string, providerUserID string) (*model.User, error) {
	t.Helper()
	c := newOAuthRegisterTestContext(t)
	provider := &desktopOAuthTestProvider{
		name: "Probe Provider",
		user: &oauth.OAuthUser{
			ProviderUserID: providerUserID,
			Username:       "probe-" + providerUserID,
			DisplayName:    "Probe User",
			Email:          email,
		},
	}
	result, err := findOrCreateOAuthUserWithOptions(c, provider, provider.user, sessions.Default(c), oauthFindOrCreateOptions{
		AllowAutoRegister:     true,
		AllowAutoMergeByEmail: false,
		InitialRole:           common.RoleCommonUser,
	})
	if err != nil {
		return nil, err
	}
	return result.User, nil
}

// The IdP asserts an email the local database already holds. Because the email
// column is not unique, writing it unconditionally creates a second row with
// the same address; ResetUserPasswordByEmail then rewrites the password of both
// rows and returns the plaintext to whoever requests the reset. The new account
// must therefore be created without claiming an address that is already taken.
func TestOAuthAutoRegisterDoesNotClaimTakenEmail(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	existing := &model.User{
		Id:       7001,
		Username: "email-owner",
		Password: "password123",
		Email:    "owned@example.com",
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
		AffCode:  "aff-7001",
	}
	require.NoError(t, model.DB.Create(existing).Error)

	created, err := registerOAuthUserForTest(t, "owned@example.com", "probe-provider-user-1")
	require.NoError(t, err)
	require.NotZero(t, created.Id)
	require.Empty(t, created.Email,
		"a newly registered OAuth account must not claim an address that is already taken")

	// The decisive assertion: exactly one row may carry the address, otherwise
	// ResetUserPasswordByEmail would rewrite more than one account's password.
	var count int64
	require.NoError(t, model.DB.Unscoped().Model(&model.User{}).
		Where("email = ?", "owned@example.com").Count(&count).Error)
	require.EqualValues(t, 1, count,
		"the email column is not unique; registration must not create a second row with the same address")
}

// Control: an unclaimed address is still stored, so the guard above is not
// simply dropping every email.
func TestOAuthAutoRegisterKeepsUnclaimedEmail(t *testing.T) {
	setupCustomOAuthJWTControllerTestDB(t)

	created, err := registerOAuthUserForTest(t, "free@example.com", "probe-provider-user-2")
	require.NoError(t, err)
	require.Equal(t, "free@example.com", created.Email,
		"an unclaimed address must still be recorded on the new account")
}
