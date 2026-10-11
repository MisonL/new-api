package controller

import (
	"strings"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// validateOAuthState checks the OAuth `state` parameter against the value
// stored in the session.
func validateOAuthState(c *gin.Context, state string) bool {
	state = strings.TrimSpace(state)
	if state == "" {
		return false
	}

	session := sessions.Default(c)
	sessionState, ok := session.Get("oauth_state").(string)
	return ok && strings.TrimSpace(sessionState) != "" && state == sessionState
}

// consumeOAuthState removes the state nonce from the session.
//
// The state parameter is a single-use CSRF nonce: it is generated per
// authorization request and compared on the callback. Validating it without
// ever clearing it leaves it replayable for the lifetime of the session
// cookie, so an attacker who observes one callback URL (a referrer leak, a
// shared browser, a proxy log) can replay that callback for as long as the
// cookie lives.
//
// This is called on the SUCCESS path, not at validation time, on purpose:
//
//   - A failed attempt must stay retryable. The CAS flow documents and tests
//     this (TestHandleCustomOAuthCASCallbackReleasesTicketWhenLocalLoginFails
//     expects a second callback with the same state to reach upstream again), and
//     a user whose login fails at the provider should be able to press back and
//     retry without restarting from the login page.
//   - React StrictMode double-invokes the callback effect in development, so
//     spending the nonce before the request is known to succeed surfaces as a
//     spurious "invalid state" failure on a login that actually worked.
//
// Once a login or bind has completed, a replay of the same callback is what the
// nonce exists to stop, and that is exactly when this runs.
func consumeOAuthState(c *gin.Context) {
	session := sessions.Default(c)
	if session.Get("oauth_state") == nil {
		return
	}
	session.Delete("oauth_state")
	// A failed save leaves the nonce in place. That is the fail-safe direction:
	// the session cookie still holds the old value, so the nonce is not spent
	// and the next attempt can still validate against it.
	_ = session.Save()
}
