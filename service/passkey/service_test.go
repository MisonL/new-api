package passkey

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

func TestResolveOriginsPreservesRequestHostPort(t *testing.T) {
	settings := &system_setting.PasskeySettings{}
	tests := []struct {
		name       string
		url        string
		host       string
		wantOrigin string
		wantRPID   string
	}{
		{
			name:       "http loopback",
			url:        "http://localhost:3000/api/user/passkey/verify/begin",
			host:       "localhost:3000",
			wantOrigin: "http://localhost:3000",
			wantRPID:   "localhost",
		},
		{
			name:       "https loopback",
			url:        "https://localhost:8443/api/user/passkey/verify/begin",
			host:       "localhost:8443",
			wantOrigin: "https://localhost:8443",
			wantRPID:   "localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			req.Host = tt.host

			origins, err := resolveOrigins(req, settings)
			if err != nil {
				t.Fatalf("resolveOrigins() error = %v", err)
			}
			if len(origins) != 1 || origins[0] != tt.wantOrigin {
				t.Fatalf("resolveOrigins() = %v, want [%s]", origins, tt.wantOrigin)
			}

			rpID, err := resolveRPID(req, settings, origins)
			if err != nil {
				t.Fatalf("resolveRPID() error = %v", err)
			}
			if rpID != tt.wantRPID {
				t.Fatalf("resolveRPID() = %q, want %q", rpID, tt.wantRPID)
			}
		})
	}
}

func TestResolveOriginsAndRPIDHostVariants(t *testing.T) {
	// DO NOT RUN IN PARALLEL: this test mutates global system_setting.ServerAddress.
	tests := []struct {
		name       string
		url        string
		host       string
		serverAddr string
		settings   system_setting.PasskeySettings
		wantOrigin string
		wantRPID   string
		wantErr    bool
	}{
		{
			name:       "localhost with non-standard port",
			url:        "http://localhost:3000/api/user/passkey/verify/begin",
			host:       "localhost:3000",
			wantOrigin: "http://localhost:3000",
			wantRPID:   "localhost",
		},
		{
			name:       "ipv4 loopback with port",
			url:        "http://127.0.0.1:3000/api/user/passkey/verify/begin",
			host:       "127.0.0.1:3000",
			wantOrigin: "http://127.0.0.1:3000",
			wantRPID:   "127.0.0.1",
		},
		{
			name:       "ipv6 loopback with port",
			url:        "http://[::1]:3000/api/user/passkey/verify/begin",
			host:       "[::1]:3000",
			wantOrigin: "http://[::1]:3000",
			wantRPID:   "::1",
		},
		{
			name:       "https host without port",
			url:        "https://console.example.com/api/user/passkey/verify/begin",
			host:       "console.example.com",
			wantOrigin: "https://console.example.com",
			wantRPID:   "console.example.com",
		},
		{
			name:       "https host with custom port",
			url:        "https://console.example.com:8443/api/user/passkey/verify/begin",
			host:       "console.example.com:8443",
			wantOrigin: "https://console.example.com:8443",
			wantRPID:   "console.example.com",
		},
		{
			name:       "server address fallback preserves port",
			url:        "/api/user/passkey/verify/begin",
			host:       "",
			serverAddr: "http://localhost:3000",
			wantOrigin: "http://localhost:3000",
			wantRPID:   "localhost",
		},
		{
			name:       "https server address fallback preserves scheme",
			url:        "/api/user/passkey/verify/begin",
			host:       "",
			serverAddr: "https://console.example.com:8443",
			wantOrigin: "https://console.example.com:8443",
			wantRPID:   "console.example.com",
		},
		{
			name:    "empty host without fallback errors",
			url:     "/api/user/passkey/verify/begin",
			host:    "",
			wantErr: true,
		},
		{
			name:       "configured origin drives rp id",
			url:        "http://localhost:3000/api/user/passkey/verify/begin",
			host:       "localhost:3000",
			settings:   system_setting.PasskeySettings{Origins: "https://auth.example.com:9443"},
			wantOrigin: "https://auth.example.com:9443",
			wantRPID:   "auth.example.com",
		},
		{
			name:       "configured localhost origin allowed without insecure toggle",
			url:        "http://localhost:3000/api/user/passkey/verify/begin",
			host:       "localhost:3000",
			settings:   system_setting.PasskeySettings{Origins: "http://localhost:3000"},
			wantOrigin: "http://localhost:3000",
			wantRPID:   "localhost",
		},
		{
			name:       "configured ipv4 loopback origin allowed without insecure toggle",
			url:        "http://127.0.0.1:3000/api/user/passkey/verify/begin",
			host:       "127.0.0.1:3000",
			settings:   system_setting.PasskeySettings{Origins: "http://127.0.0.1:3000"},
			wantOrigin: "http://127.0.0.1:3000",
			wantRPID:   "127.0.0.1",
		},
		{
			name:       "configured ipv4 loopback alias accepts current localhost origin",
			url:        "http://localhost:3000/api/user/passkey/verify/begin",
			host:       "localhost:3000",
			settings:   system_setting.PasskeySettings{Origins: "http://127.0.0.1:3000", RPID: "127.0.0.1"},
			wantOrigin: "http://127.0.0.1:3000",
			wantRPID:   "localhost",
		},
		{
			name:       "configured loopback alias does not accept different port",
			url:        "http://localhost:3001/api/user/passkey/verify/begin",
			host:       "localhost:3001",
			settings:   system_setting.PasskeySettings{Origins: "http://127.0.0.1:3000", RPID: "127.0.0.1"},
			wantOrigin: "http://127.0.0.1:3000",
			wantRPID:   "127.0.0.1",
		},
		{
			name:       "configured loopback alias does not accept different scheme",
			url:        "https://localhost:3000/api/user/passkey/verify/begin",
			host:       "localhost:3000",
			settings:   system_setting.PasskeySettings{Origins: "http://127.0.0.1:3000", RPID: "127.0.0.1"},
			wantOrigin: "http://127.0.0.1:3000",
			wantRPID:   "127.0.0.1",
		},
		{
			name:       "ipv4 loopback subnet with port",
			url:        "http://127.1.2.3:3000/api/user/passkey/verify/begin",
			host:       "127.1.2.3:3000",
			wantOrigin: "http://127.1.2.3:3000",
			wantRPID:   "127.1.2.3",
		},
		{
			name:       "configured ipv6 loopback origin allowed without insecure toggle",
			url:        "http://[::1]:3000/api/user/passkey/verify/begin",
			host:       "[::1]:3000",
			settings:   system_setting.PasskeySettings{Origins: "http://[::1]:3000"},
			wantOrigin: "http://[::1]:3000",
			wantRPID:   "::1",
		},
		{
			name:     "configured http non-loopback origin rejected without insecure toggle",
			url:      "http://localhost:3000/api/user/passkey/verify/begin",
			host:     "localhost:3000",
			settings: system_setting.PasskeySettings{Origins: "http://console.example.com"},
			wantErr:  true,
		},
		{
			name:       "server address fallback rejects non loopback http host",
			url:        "/api/user/passkey/verify/begin",
			host:       "",
			serverAddr: "http://console.example.com",
			wantErr:    true,
		},
		{
			name:       "request origin overrides non loopback http server address",
			url:        "http://127.0.0.1:3001/api/user/passkey/verify/begin",
			host:       "127.0.0.1:3001",
			serverAddr: "http://new-api-dev-isolated-new-api-1:3000",
			wantOrigin: "http://127.0.0.1:3001",
			wantRPID:   "127.0.0.1",
		},
		{
			name:       "request origin drives default rp id",
			url:        "http://localhost:3000/api/user/passkey/verify/begin",
			host:       "localhost:3000",
			wantOrigin: "http://localhost:3000",
			wantRPID:   "localhost",
		},
	}

	previousServerAddress := system_setting.ServerAddress
	t.Cleanup(func() {
		system_setting.ServerAddress = previousServerAddress
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system_setting.ServerAddress = tt.serverAddr
			req := httptest.NewRequest("GET", tt.url, nil)
			req.Host = tt.host

			origins, err := resolveOrigins(req, &tt.settings)
			if tt.wantErr {
				if err == nil {
					t.Fatal("resolveOrigins() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveOrigins() error = %v", err)
			}
			if len(origins) == 0 || origins[0] != tt.wantOrigin {
				t.Fatalf("resolveOrigins() = %v, want first origin %s", origins, tt.wantOrigin)
			}

			rpID, err := resolveRPID(req, &tt.settings, origins)
			if err != nil {
				t.Fatalf("resolveRPID() error = %v", err)
			}
			if rpID != tt.wantRPID {
				t.Fatalf("resolveRPID() = %q, want %q", rpID, tt.wantRPID)
			}
		})
	}
}

func TestResolveRPIDRequiresOrigin(t *testing.T) {
	req := httptest.NewRequest("GET", "https://localhost/api/user/passkey/verify/begin", nil)
	if _, err := resolveRPID(req, &system_setting.PasskeySettings{}, nil); err == nil {
		t.Fatal("resolveRPID() expected error for empty origins")
	}
}

func TestHostWithoutPortKeepsInvalidBracketedHost(t *testing.T) {
	if got := hostWithoutPort("[not-an-ipv6]"); got != "[not-an-ipv6]" {
		t.Fatalf("hostWithoutPort() = %q, want invalid bracketed host unchanged", got)
	}
}
