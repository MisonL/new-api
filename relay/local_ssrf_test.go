package relay

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

// TestMain keeps relay package fake upstreams behind the real SSRF transport
// while allowing httptest.Server's randomly assigned loopback ports.
func TestMain(m *testing.M) {
	setting := system_setting.GetFetchSetting()
	previous := *setting
	*setting = system_setting.FetchSetting{
		EnableSSRFProtection:   true,
		AllowPrivateIp:         true,
		DomainFilterMode:       false,
		IpFilterMode:           false,
		AllowedPorts:           []string{"1-65535"},
		ApplyIPFilterForDomain: true,
	}
	code := m.Run()
	*setting = previous
	os.Exit(code)
}
