package system_setting

import "testing"

func TestShouldAutoDerivePasskeyOrigins(t *testing.T) {
	testCases := []struct {
		name    string
		origins string
		want    bool
	}{
		{name: "empty", origins: "", want: true},
		{name: "empty array", origins: "[]", want: true},
		{name: "placeholder", origins: defaultPasskeyOriginPlaceholder, want: true},
		{name: "custom", origins: "https://newapi.example.com", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoDerivePasskeyOrigins(tc.origins); got != tc.want {
				t.Fatalf("shouldAutoDerivePasskeyOrigins(%q) = %v, want %v", tc.origins, got, tc.want)
			}
		})
	}
}

func TestShouldAutoDerivePasskeyRPID(t *testing.T) {
	testCases := []struct {
		name string
		rpID string
		want bool
	}{
		{name: "empty", rpID: "", want: true},
		{name: "localhost", rpID: "localhost", want: true},
		{name: "placeholder host", rpID: "localhost:3000", want: true},
		{name: "custom", rpID: "newapi.example.com", want: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoDerivePasskeyRPID(tc.rpID); got != tc.want {
				t.Fatalf("shouldAutoDerivePasskeyRPID(%q) = %v, want %v", tc.rpID, got, tc.want)
			}
		})
	}
}

func TestDerivePasskeyRPIDFromServerAddress(t *testing.T) {
	got := derivePasskeyRPIDFromServerAddress("https://127.0.0.1:13020")
	if got != "127.0.0.1" {
		t.Fatalf("derivePasskeyRPIDFromServerAddress() = %q, want %q", got, "127.0.0.1")
	}
}

func TestGetPasskeySettingsSkipsPlaceholderServerAddress(t *testing.T) {
	previousServerAddress := ServerAddress
	previousSettings := defaultPasskeySettings
	ServerAddress = defaultPasskeyOriginPlaceholder
	defaultPasskeySettings = PasskeySettings{
		RPID:    "",
		Origins: "",
	}
	defer func() {
		ServerAddress = previousServerAddress
		defaultPasskeySettings = previousSettings
	}()

	got := GetPasskeySettings()
	if got.RPID != "" {
		t.Fatalf("GetPasskeySettings().RPID = %q, want empty", got.RPID)
	}
	if got.Origins != "" {
		t.Fatalf("GetPasskeySettings().Origins = %q, want empty", got.Origins)
	}
}

func TestGetPasskeySettingsForServerAddressDerivesValues(t *testing.T) {
	previousSettings := defaultPasskeySettings
	defaultPasskeySettings = PasskeySettings{
		RPID:    "",
		Origins: "",
	}
	defer func() {
		defaultPasskeySettings = previousSettings
	}()

	got := GetPasskeySettingsForServerAddress("https://127.0.0.1:13020")
	if got.RPID != "127.0.0.1" {
		t.Fatalf("GetPasskeySettingsForServerAddress().RPID = %q, want %q", got.RPID, "127.0.0.1")
	}
	if got.Origins != "https://127.0.0.1:13020" {
		t.Fatalf("GetPasskeySettingsForServerAddress().Origins = %q, want %q", got.Origins, "https://127.0.0.1:13020")
	}
}
