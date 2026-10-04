package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// putOption drives the real UpdateOption handler with one key/value pair and
// returns the decoded success flag plus the raw body.
func putOption(t *testing.T, db *gorm.DB, key string, rawValue string) (bool, string) {
	t.Helper()

	body := []byte(`{"key":"` + key + `","value":` + rawValue + `}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	UpdateOption(c)

	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Success, resp.Message
}

// restoreBooleanOptionGlobals snapshots and restores the process-wide switch
// globals that updateOptionMap writes. A guard test deliberately lets one
// request through (to prove the guard does not over-fire), and that request
// sets the corresponding global; without this the value leaks into every later
// test in the same process, which model.InitOptionMap then seeds into
// common.OptionMap.
func restoreBooleanOptionGlobals(t *testing.T) {
	t.Helper()

	snapshot := map[*bool]bool{
		&common.PasswordLoginEnabled:           common.PasswordLoginEnabled,
		&common.PasswordRegisterEnabled:        common.PasswordRegisterEnabled,
		&common.EmailVerificationEnabled:       common.EmailVerificationEnabled,
		&common.GitHubOAuthEnabled:             common.GitHubOAuthEnabled,
		&common.LinuxDOOAuthEnabled:            common.LinuxDOOAuthEnabled,
		&common.WeChatAuthEnabled:              common.WeChatAuthEnabled,
		&common.TelegramOAuthEnabled:           common.TelegramOAuthEnabled,
		&common.TurnstileCheckEnabled:          common.TurnstileCheckEnabled,
		&common.RegisterEnabled:                common.RegisterEnabled,
		&common.EmailDomainRestrictionEnabled:  common.EmailDomainRestrictionEnabled,
		&common.EmailAliasRestrictionEnabled:   common.EmailAliasRestrictionEnabled,
		&common.AutomaticDisableChannelEnabled: common.AutomaticDisableChannelEnabled,
		&common.AutomaticEnableChannelEnabled:  common.AutomaticEnableChannelEnabled,
		&common.GroupModelRouteHelperEnabled:   common.GroupModelRouteHelperEnabled,
		&common.LogConsumeEnabled:              common.LogConsumeEnabled,
		&common.DrawingEnabled:                 common.DrawingEnabled,
		&common.TaskEnabled:                    common.TaskEnabled,
		&common.DataExportEnabled:              common.DataExportEnabled,
		&common.SMTPSSLEnabled:                 common.SMTPSSLEnabled,
		&common.DisplayTokenStatEnabled:        common.DisplayTokenStatEnabled,
		&common.DefaultCollapseSidebar:         common.DefaultCollapseSidebar,
		&common.SMTPForceAuthLogin:             common.SMTPForceAuthLogin,
	}
	t.Cleanup(func() {
		for ptr, value := range snapshot {
			*ptr = value
		}
	})
}

// The prerequisite guards in UpdateOption compare the option value to "on"
// before model.UpdateOption canonicalizes it. Comparing the raw string to the
// literal "true" let every other spelling strconv.ParseBool accepts bypass
// them: sending "1" enabled GitHub login with no Client Id configured, because
// normalization later turned it into the "true" the consumer compares against.
// Each accepted spelling of "on" must be blocked when its prerequisite is
// missing, exactly like the canonical "true".
func TestUpdateOptionOAuthGuardsRejectEveryTrueSpelling(t *testing.T) {
	db := setupSetupControllerTestDB(t)
	restoreBooleanOptionGlobals(t)

	previousClientId := common.GitHubClientId
	common.GitHubClientId = ""
	t.Cleanup(func() {
		common.GitHubClientId = previousClientId
		db.Exec("DELETE FROM options")
	})

	for _, raw := range []string{`"true"`, `"True"`, `"TRUE"`, `"1"`, `"t"`, `" true "`} {
		ok, _ := putOption(t, db, "GitHubOAuthEnabled", raw)
		require.False(t, ok, "raw=%s must not enable GitHub OAuth without a Client Id", raw)
	}

	// A spelling that means "off" must not trip the guard, and neither must a
	// real "true" once the prerequisite is satisfied.
	for _, raw := range []string{`"false"`, `"0"`, `" f "`} {
		ok, msg := putOption(t, db, "GitHubOAuthEnabled", raw)
		require.True(t, ok, "raw=%s must be accepted (guard must not fire): %s", raw, msg)
	}

	common.GitHubClientId = "client-id"
	ok, msg := putOption(t, db, "GitHubOAuthEnabled", `"1"`)
	require.True(t, ok, "with a Client Id present the guard must not fire: %s", msg)
}

// The same guard shape protects every provider that needs a prerequisite. Pin
// the full set so a new one cannot be added with the old raw-string compare.
func TestUpdateOptionGuardsCoverEveryProvider(t *testing.T) {
	db := setupSetupControllerTestDB(t)
	restoreBooleanOptionGlobals(t)

	saved := map[string]string{
		"GitHubClientId":      common.GitHubClientId,
		"LinuxDOClientId":     common.LinuxDOClientId,
		"WeChatServerAddress": common.WeChatServerAddress,
		"TurnstileSiteKey":    common.TurnstileSiteKey,
		"TelegramBotToken":    common.TelegramBotToken,
	}
	savedWhitelist := common.EmailDomainWhitelist
	discordSettings := system_setting.GetDiscordSettings()
	savedDiscordClientId := discordSettings.ClientId
	oidcSettings := system_setting.GetOIDCSettings()
	savedOIDCClientId := oidcSettings.ClientId
	t.Cleanup(func() {
		common.GitHubClientId = saved["GitHubClientId"]
		common.LinuxDOClientId = saved["LinuxDOClientId"]
		common.WeChatServerAddress = saved["WeChatServerAddress"]
		common.TurnstileSiteKey = saved["TurnstileSiteKey"]
		common.TelegramBotToken = saved["TelegramBotToken"]
		common.EmailDomainWhitelist = savedWhitelist
		discordSettings.ClientId = savedDiscordClientId
		oidcSettings.ClientId = savedOIDCClientId
		db.Exec("DELETE FROM options")
	})

	common.GitHubClientId = ""
	common.LinuxDOClientId = ""
	common.WeChatServerAddress = ""
	common.TurnstileSiteKey = ""
	common.TelegramBotToken = ""
	common.EmailDomainWhitelist = nil
	discordSettings.ClientId = ""
	oidcSettings.ClientId = ""

	// Every key here has a prerequisite guard in UpdateOption. Discord and OIDC
	// are spelled with a dot ("discord.enabled", "oidc.enabled"), which is why an
	// earlier version of this list missed them even though the guard was present.
	for _, key := range []string{
		"GitHubOAuthEnabled",
		"discord.enabled",
		"oidc.enabled",
		"LinuxDOOAuthEnabled",
		"WeChatAuthEnabled",
		"TurnstileCheckEnabled",
		"TelegramOAuthEnabled",
		"EmailDomainRestrictionEnabled",
	} {
		ok, msg := putOption(t, db, key, `"1"`)
		require.False(t, ok, "%s must be blocked by its prerequisite guard when sent as \"1\"", key)
		require.NotEmpty(t, msg, "%s must explain why it was refused", key)
	}
}
