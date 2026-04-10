package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

func TestShouldAutoDeriveServerAddress(t *testing.T) {
	testCases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: true},
		{name: "placeholder", value: defaultServerAddressPlaceholder, want: true},
		{name: "custom", value: "https://newapi.example.com", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoDeriveServerAddress(tc.value); got != tc.want {
				t.Fatalf("shouldAutoDeriveServerAddress(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestResolveServerAddressForRequestUsesRequestHostWhenPlaceholder(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = defaultServerAddressPlaceholder
	defer func() {
		system_setting.ServerAddress = previous
	}()

	req := httptest.NewRequest("GET", "http://127.0.0.1:13020/api/status", nil)
	got := resolveServerAddressForRequest(req)
	want := "http://127.0.0.1:13020"
	if got != want {
		t.Fatalf("resolveServerAddressForRequest() = %q, want %q", got, want)
	}
}

func TestResolveServerAddressForRequestPrefersConfiguredValue(t *testing.T) {
	previous := system_setting.ServerAddress
	system_setting.ServerAddress = "https://newapi.example.com"
	defer func() {
		system_setting.ServerAddress = previous
	}()

	req := httptest.NewRequest("GET", "http://127.0.0.1:13020/api/status", nil)
	got := resolveServerAddressForRequest(req)
	want := "https://newapi.example.com"
	if got != want {
		t.Fatalf("resolveServerAddressForRequest() = %q, want %q", got, want)
	}
}

func TestShouldPersistDerivedServerAddress(t *testing.T) {
	testCases := []struct {
		name       string
		configured string
		derived    string
		want       bool
	}{
		{name: "empty config with derived", configured: "", derived: "http://127.0.0.1:13020", want: true},
		{name: "placeholder with derived", configured: defaultServerAddressPlaceholder, derived: "http://127.0.0.1:13020", want: true},
		{name: "custom configured", configured: "https://newapi.example.com", derived: "http://127.0.0.1:13020", want: false},
		{name: "empty derived", configured: "", derived: "", want: false},
		{name: "same as configured", configured: "http://127.0.0.1:13020", derived: "http://127.0.0.1:13020", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldPersistDerivedServerAddress(tc.configured, tc.derived); got != tc.want {
				t.Fatalf("shouldPersistDerivedServerAddress(%q, %q) = %v, want %v", tc.configured, tc.derived, got, tc.want)
			}
		})
	}
}
