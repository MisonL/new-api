package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// telegramParamsWithAge builds widget-shaped auth params with an explicit
// auth_date, so the freshness guards can be exercised at chosen offsets.
func telegramParamsWithAge(telegramID string, authDate int64, token string) map[string][]string {
	params := map[string][]string{
		"id":         {telegramID},
		"first_name": {"Probe"},
		"auth_date":  {strconv.FormatInt(authDate, 10)},
	}
	params["hash"] = []string{telegramAuthHashFromMap(params, token)}
	return params
}

// telegramAuthHashFromMap signs a map[string][]string payload the way the
// production verifier expects.
func telegramAuthHashFromMap(params map[string][]string, token string) string {
	values := map[string][]string{}
	for key, v := range params {
		if key == "hash" || len(v) == 0 {
			continue
		}
		values[key] = v
	}
	return telegramAuthHashFromValues(values, token)
}

// telegramAuthHashFromValues is the same HMAC construction production uses,
// written against map[string][]string so the test does not depend on url.Values.
func telegramAuthHashFromValues(params map[string][]string, token string) string {
	items := make([]string, 0, len(params))
	for key, values := range params {
		if key == "hash" || len(values) == 0 {
			continue
		}
		items = append(items, key+"="+values[0])
	}
	sort.Strings(items)

	payload := ""
	for index, item := range items {
		if index > 0 {
			payload += "\n"
		}
		payload += item
	}

	sha256hash := sha256.New()
	_, _ = sha256hash.Write([]byte(token))
	hmacHash := hmac.New(sha256.New, sha256hash.Sum(nil))
	_, _ = hmacHash.Write([]byte(payload))
	return hex.EncodeToString(hmacHash.Sum(nil))
}

// telegramIDCounter keeps every test's signed payload distinct so the
// process-wide one-time-consumption set cannot leak a consumed signature into
// a sibling test that happens to build identical params.
var telegramIDCounter int64

func nextTelegramID() string {
	return "tg-probe-" + strconv.FormatInt(atomic.AddInt64(&telegramIDCounter, 1), 10)
}

func TestCheckTelegramAuthorizationAcceptsFreshSignature(t *testing.T) {
	token := "probe-bot-token"
	params := telegramParamsWithAge(nextTelegramID(), time.Now().Unix(), token)

	require.True(t, checkTelegramAuthorization(params, token),
		"a fresh, correctly signed payload must be accepted")
}

func TestCheckTelegramAuthorizationRejectsReplay(t *testing.T) {
	token := "probe-bot-token"
	params := telegramParamsWithAge(nextTelegramID(), time.Now().Unix(), token)

	require.True(t, checkTelegramAuthorization(params, token), "first use must be accepted")
	require.False(t, checkTelegramAuthorization(params, token),
		"a valid signature is a one-time credential; replay must be refused")
}

func TestCheckTelegramAuthorizationRejectsStaleAuthDate(t *testing.T) {
	token := "probe-bot-token"
	stale := time.Now().Add(-telegramMaxAuthAge - time.Minute).Unix()
	params := telegramParamsWithAge(nextTelegramID(), stale, token)

	require.False(t, checkTelegramAuthorization(params, token),
		"a signature older than telegramMaxAuthAge must be refused")
}

func TestCheckTelegramAuthorizationRejectsFutureAuthDate(t *testing.T) {
	token := "probe-bot-token"
	future := time.Now().Add(telegramClockSkew + time.Minute).Unix()
	params := telegramParamsWithAge(nextTelegramID(), future, token)

	require.False(t, checkTelegramAuthorization(params, token),
		"an auth_date beyond telegramClockSkew in the future must be refused")
}

func TestCheckTelegramAuthorizationRequiresAuthDate(t *testing.T) {
	token := "probe-bot-token"

	missing := map[string][]string{"id": {"1"}, "first_name": {"Probe"}}
	missing["hash"] = []string{telegramAuthHashFromMap(missing, token)}
	require.False(t, checkTelegramAuthorization(missing, token),
		"a payload without auth_date cannot be freshness-checked and must be refused")

	malformed := map[string][]string{"id": {"2"}, "auth_date": {"not-a-number"}}
	malformed["hash"] = []string{telegramAuthHashFromMap(malformed, token)}
	require.False(t, checkTelegramAuthorization(malformed, token),
		"an unparseable auth_date must be refused, not silently ignored")
}

func TestCheckTelegramAuthorizationRejectsEmptyToken(t *testing.T) {
	// Empty token degenerates the HMAC key to sha256(""), a public constant.
	// The guard must be fail-closed: refuse every payload, including one that
	// is correctly signed with that public key.
	params := telegramParamsWithAge(nextTelegramID(), time.Now().Unix(), "")

	require.False(t, checkTelegramAuthorization(params, ""),
		"an empty token must reject all payloads (fail-closed)")
	require.False(t, checkTelegramAuthorization(params, "   "),
		"a whitespace-only token must reject all payloads")
}

func TestCheckTelegramAuthorizationRejectsTamperedSignature(t *testing.T) {
	token := "probe-bot-token"
	params := telegramParamsWithAge(nextTelegramID(), time.Now().Unix(), token)

	tampered := map[string][]string{}
	for k, v := range params {
		tampered[k] = v
	}
	tampered["first_name"] = []string{"Mallory"}

	require.False(t, checkTelegramAuthorization(tampered, token),
		"mutating a signed field must invalidate the signature")
}

// A signed payload without "id" used to panic at params["id"][0]. The verifier
// accepts any signed key set, so the only thing standing between a malformed
// payload and a 500 was the index. telegramParamID must return empty instead,
// and both handlers must treat empty as "invalid request".
func TestTelegramParamIDHandlesMissingID(t *testing.T) {
	token := "probe-bot-token"
	missing := map[string][]string{
		"first_name": {"NoId"},
		"auth_date":  {strconv.FormatInt(time.Now().Unix(), 10)},
	}
	missing["hash"] = []string{telegramAuthHashFromMap(missing, token)}

	require.True(t, checkTelegramAuthorization(missing, token),
		"a correctly signed payload without id passes the HMAC check")
	require.Equal(t, "", telegramParamID(missing),
		"a missing id must yield the empty string, not panic on the index")
	require.Equal(t, "", telegramParamID(map[string][]string{}))
	require.Equal(t, "", telegramParamID(map[string][]string{"id": {}}))
	require.Equal(t, "123", telegramParamID(map[string][]string{"id": {"123"}}))
}
