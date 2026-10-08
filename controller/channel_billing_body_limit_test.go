package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// GetResponseBody fetches JSON from an upstream the gateway does not control:
// billing endpoints and /v1/models lists. The read needs an explicit ceiling
// so a broken or hostile endpoint cannot grow the process heap without bound.
func TestGetResponseBodyRejectsOversizedUpstreamBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, maxUpstreamJSONResponseBodyBytes+1))
	}))
	defer srv.Close()

	channel := &model.Channel{Id: 1, Name: "oversize-probe"}

	body, err := GetResponseBody(http.MethodGet, srv.URL, channel, http.Header{})

	require.Nil(t, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
}

// Control case: a response within the limit is returned untouched, so the
// ceiling cannot break ordinary billing and model-list fetches.
func TestGetResponseBodyAcceptsBodyWithinLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"}]}`))
	}))
	defer srv.Close()

	channel := &model.Channel{Id: 1, Name: "within-limit-probe"}

	body, err := GetResponseBody(http.MethodGet, srv.URL, channel, http.Header{})

	require.NoError(t, err)
	require.Equal(t, `{"data":[{"id":"gpt-4o"}]}`, string(body))
}

// The same helper must keep failing loudly when the upstream answers with a
// non-200 status, so the size bound cannot mask the existing error contract.
func TestGetResponseBodyStillRejectsNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	channel := &model.Channel{Id: 1, Name: "status-probe"}

	body, err := GetResponseBody(http.MethodGet, srv.URL, channel, http.Header{})

	require.Nil(t, body)
	require.Error(t, err)
	require.Contains(t, err.Error(), "status code")
	require.Contains(t, err.Error(), "401", "error should carry the upstream status, got %q", err.Error())
}
