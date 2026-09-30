package common

import (
	"fmt"
	"net"
	"reflect"
	"testing"
)

func TestResolveTrustedProxiesEmptyDisablesTrust(t *testing.T) {
	for _, raw := range []string{"", "   ", "\t"} {
		proxies, err := ResolveTrustedProxies(raw)
		if err != nil {
			t.Fatalf("ResolveTrustedProxies(%q) returned error: %v", raw, err)
		}
		if proxies != nil {
			t.Fatalf("ResolveTrustedProxies(%q) = %v, want nil so that proxy trust is disabled", raw, proxies)
		}
	}
}

func TestResolveTrustedProxiesExplicitList(t *testing.T) {
	proxies, err := ResolveTrustedProxies(" 10.0.0.1 , 172.16.0.0/12 ,, 192.168.1.5 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"10.0.0.1", "172.16.0.0/12", "192.168.1.5"}
	if !reflect.DeepEqual(proxies, want) {
		t.Fatalf("got %v, want %v", proxies, want)
	}
}

// An explicit value is passed through unchanged so that a typo surfaces as an
// error from SetTrustedProxies instead of silently narrowing the trust set.
func TestResolveTrustedProxiesKeepsInvalidEntries(t *testing.T) {
	proxies, err := ResolveTrustedProxies("not-an-ip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(proxies, []string{"not-an-ip"}) {
		t.Fatalf("got %v, want the entry passed through", proxies)
	}
}

func TestResolveTrustedProxiesRejectsListOfOnlySeparators(t *testing.T) {
	if _, err := ResolveTrustedProxies(" , , "); err == nil {
		t.Fatal("a list with no usable entry should be an error, not silently empty trust")
	}
}

// auto must resolve to this machine's own subnets: in a container the reverse
// proxy arrives from that subnet, so it is exactly the set whose forwarding
// headers may be believed.
func TestResolveTrustedProxiesAutoDerivesOwnSubnets(t *testing.T) {
	proxies, err := ResolveTrustedProxies(TrustedProxiesAuto)
	if err != nil {
		t.Skipf("this host has no non-loopback IPv4 interface: %v", err)
	}
	if len(proxies) == 0 {
		t.Fatal("auto returned no subnets")
	}

	expected := map[string]struct{}{}
	interfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("failed to list interfaces: %v", err)
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			network, ok := addr.(*net.IPNet)
			if !ok || network.IP.To4() == nil {
				continue
			}
			ones, bits := network.Mask.Size()
			if bits == 32 && ones > 0 {
				// InterfaceAddrs reports the address with its host bits set
				// (10.0.90.200/22); auto normalizes to the network address
				// (10.0.88.0/22). Compare in the normalized form.
				expected[fmt.Sprintf("%s/%d", network.IP.Mask(network.Mask).String(), ones)] = struct{}{}
			}
		}
	}
	if len(expected) == 0 {
		t.Skip("no IPv4 interface to compare against")
	}
	for _, subnet := range proxies {
		if _, ok := expected[subnet]; !ok {
			t.Fatalf("auto returned %s, which is not one of this host's subnets %v", subnet, expected)
		}
	}

	// Each derived subnet must actually contain one of this host's addresses.
	for _, subnet := range proxies {
		_, network, err := net.ParseCIDR(subnet)
		if err != nil {
			t.Fatalf("auto returned unparseable subnet %q: %v", subnet, err)
		}
		contained := false
		for ip := range expected {
			parsed, _, _ := net.ParseCIDR(ip)
			if parsed != nil && network.Contains(parsed) {
				contained = true
				break
			}
		}
		if !contained {
			t.Fatalf("derived subnet %s contains none of this host's addresses", subnet)
		}
	}
}

func TestResolveTrustedProxiesAutoIsCaseInsensitive(t *testing.T) {
	for _, raw := range []string{"auto", "AUTO", "Auto", "  aUtO  "} {
		proxies, err := ResolveTrustedProxies(raw)
		if err != nil {
			// Only acceptable when this host has no usable interface.
			t.Skipf("auto derivation unavailable on this host: %v", err)
		}
		if len(proxies) == 0 {
			t.Fatalf("ResolveTrustedProxies(%q) returned no subnets", raw)
		}
	}
}
