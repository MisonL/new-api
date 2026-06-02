package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

type customOAuthCASCallbackRequest struct {
	State  string `form:"state" json:"state"`
	Ticket string `form:"ticket" json:"ticket"`
}

func HandleCustomOAuthCASStart(c *gin.Context) {
	providerConfig, provider := loadCustomCASProvider(c)
	if provider == nil {
		return
	}

	session := sessions.Default(c)
	state := strings.TrimSpace(c.Query("state"))
	sessionState, ok := session.Get("oauth_state").(string)
	if state == "" || !ok || strings.TrimSpace(sessionState) == "" || state != sessionState {
		common.ApiErrorI18n(c, i18n.MsgOAuthStateInvalid)
		return
	}

	callbackURL, err := buildCustomOAuthBrowserCallbackURL(c.Request, providerConfig.Slug, state)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgOAuthTokenFailed, providerParams(providerConfig.Name))
		return
	}
	serviceURL, err := providerConfig.GetCASRequiredServiceURL(callbackURL)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgOAuthTokenFailed, providerParams(providerConfig.Name))
		return
	}
	loginURL, err := provider.BuildLoginURL(serviceURL)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgOAuthTokenFailed, providerParams(providerConfig.Name))
		return
	}
	c.Redirect(http.StatusFound, loginURL)
}

func HandleCustomOAuthCASCallback(c *gin.Context) {
	providerConfig, provider := loadCustomCASProvider(c)
	if provider == nil {
		return
	}
	audit := newCustomOAuthJWTAuditInfo(providerConfig)

	var req customOAuthCASCallbackRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		audit.FailureReason = "invalid_request"
		recordCustomOAuthJWTAudit(audit)
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	session := sessions.Default(c)
	state := strings.TrimSpace(req.State)
	sessionState, ok := session.Get("oauth_state").(string)
	if state == "" || !ok || strings.TrimSpace(sessionState) == "" || state != sessionState {
		audit.FailureReason = "invalid_state"
		recordCustomOAuthJWTAudit(audit)
		common.ApiErrorI18n(c, i18n.MsgOAuthStateInvalid)
		return
	}
	if strings.TrimSpace(req.Ticket) == "" {
		audit.FailureReason = "missing_cas_ticket"
		recordCustomOAuthJWTAudit(audit)
		common.ApiErrorI18n(c, i18n.MsgOAuthTicketMissing)
		return
	}

	callbackURL, err := buildCustomOAuthBrowserCallbackURL(c.Request, providerConfig.Slug, state)
	if err != nil {
		audit.FailureReason = "invalid_callback_url"
		recordCustomOAuthJWTAudit(audit)
		common.ApiErrorI18n(c, i18n.MsgOAuthTokenFailed, providerParams(providerConfig.Name))
		return
	}
	serviceURL, err := providerConfig.GetCASRequiredServiceURL(callbackURL)
	if err != nil {
		audit.FailureReason = "invalid_service_url"
		recordCustomOAuthJWTAudit(audit)
		common.ApiErrorI18n(c, i18n.MsgOAuthTokenFailed, providerParams(providerConfig.Name))
		return
	}

	releaseTicket, err := reserveCASTicket(providerConfig.Id, req.Ticket, serviceURL)
	if err != nil {
		if isCASTicketReplayError(err) {
			audit.FailureReason = "cas_ticket_replay"
			recordCustomOAuthJWTAudit(audit)
			common.ApiErrorI18n(c, i18n.MsgOAuthTicketReplayed)
			return
		}
		audit.FailureReason = "cas_ticket_guard_error"
		recordCustomOAuthJWTAudit(audit)
		common.SysError("failed to reserve CAS ticket: " + err.Error())
		common.ApiErrorI18n(c, i18n.MsgOAuthTokenFailed, providerParams(providerConfig.Name))
		return
	}
	if releaseTicket == nil {
		audit.FailureReason = "cas_ticket_guard_error"
		recordCustomOAuthJWTAudit(audit)
		common.SysError("failed to reserve CAS ticket: release callback is nil")
		common.ApiErrorI18n(c, i18n.MsgOAuthTokenFailed, providerParams(providerConfig.Name))
		return
	}
	shouldReleaseTicket := true
	defer func() {
		if shouldReleaseTicket {
			releaseTicket()
		}
	}()

	identity, err := provider.ResolveIdentityFromTicket(c.Request.Context(), req.Ticket, serviceURL)
	if err != nil {
		if audit != nil && audit.FailureReason == "" {
			audit.FailureReason = oauthAuditFailureReason(err)
		}
		recordCustomOAuthJWTAudit(audit)
		handleCustomOAuthJWTLoginError(c, err)
		return
	}

	result, audit, err := completeCustomOAuthIdentityLogin(
		c,
		providerConfig,
		provider,
		session,
		identity.User,
		identity.Group,
		identity.Role,
		audit,
	)
	if err != nil {
		if audit != nil && audit.FailureReason == "" {
			audit.FailureReason = oauthAuditFailureReason(err)
		}
		recordCustomOAuthJWTAudit(audit)
		handleCustomOAuthJWTLoginError(c, err)
		return
	}

	if finalizeCustomOAuthIdentityLogin(c, provider, result, audit) {
		shouldReleaseTicket = false
	}
}

func loadCustomCASProvider(c *gin.Context) (*model.CustomOAuthProvider, *oauth.CASProvider) {
	providerName := c.Param("provider")
	providerConfig, err := model.GetCustomOAuthProviderBySlug(providerName)
	if err != nil || providerConfig == nil || !providerConfig.IsCAS() {
		common.ApiErrorI18n(c, i18n.MsgOAuthUnknownProvider)
		return nil, nil
	}
	if !providerConfig.Enabled {
		common.ApiErrorI18n(c, i18n.MsgOAuthNotEnabled, providerParams(providerConfig.Name))
		return nil, nil
	}
	return providerConfig, oauth.NewCASProvider(providerConfig)
}
