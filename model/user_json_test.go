package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// The admin user-list endpoints (GetAllUsers, SearchUsers, GetUserById) return
// model.User directly and only omit "password". Serialising access_token let any
// admin read the root user's management token and then authenticate as root,
// because ValidateAccessToken accepts it with no role or expiry check.
func TestUserJSONNeverExposesAccessToken(t *testing.T) {
	token := "abcdef0123456789abcdef0123456789"
	user := User{
		Id:          1,
		Username:    "root",
		Role:        100,
		AccessToken: &token,
	}

	raw, err := common.Marshal(user)
	require.NoError(t, err)

	var decoded map[string]interface{}
	require.NoError(t, common.Unmarshal(raw, &decoded))
	require.NotContains(t, decoded, "access_token",
		"access_token must never be serialised: admins would obtain root's token")
}

// The owner still needs to be able to read their own token; that endpoint
// returns it explicitly, so the Go field must remain readable.
func TestAccessTokenStillReadableInGo(t *testing.T) {
	token := "abcdef0123456789abcdef0123456789"
	user := User{AccessToken: &token}
	require.Equal(t, token, user.GetAccessToken())
}
