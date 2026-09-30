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

// auto must resolve differently on a container and on a bare host. Loopback is
// trusted in both, because a same-host proxy connects from there either way; the
// container's own subnet is trusted only inside a container. On a bare host the
// host's LAN interfaces must never be trusted, or a remote caller could set its
// own X-Forwarded-For and pick the address the rate limiters key on.
func TestResolveTrustedProxiesAutoMatchesTheEnvironment(t *testing.T) {
	proxies, err := ResolveTrustedProxies(TrustedProxiesAuto)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertLoopbackTrusted(t, proxies, "auto")

	if detectContainerMarker() == "" {
		assertExactlyLoopback(t, proxies, "on a host auto")
		return
	}

	// In a container every entry must be either loopback or one of this
	// container's own subnets; the host's other networks must not appear.
	allowed := map[string]struct{}{}
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
				allowed[fmt.Sprintf("%s/%d", network.IP.Mask(network.Mask).String(), ones)] = struct{}{}
			}
		}
	}
	nonLoopback := 0
	for _, subnet := range proxies {
		if isLoopbackEntry(subnet) {
			continue
		}
		nonLoopback++
		if _, ok := allowed[subnet]; !ok {
			t.Fatalf("in a container auto returned %s, not one of the container subnets %v", subnet, allowed)
		}
	}
	if nonLoopback == 0 {
		t.Fatalf("in a container auto returned only loopback (%v); the container subnet is missing", proxies)
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

// Both branches must be exercised explicitly. Relying on whichever branch the
// test runner happens to be in would leave the other unverified.
func TestDeriveTrustedProxiesBranches(t *testing.T) {
	original := detectContainerMarker
	t.Cleanup(func() { detectContainerMarker = original })

	detectContainerMarker = func() string { return "/.dockerenv" }
	containerResult, err := deriveTrustedProxies()
	if err != nil {
		t.Fatalf("container branch returned error: %v", err)
	}
	assertLoopbackTrusted(t, containerResult, "container branch")
	nonLoopback := 0
	for _, subnet := range containerResult {
		if !isLoopbackEntry(subnet) {
			nonLoopback++
		}
	}
	if nonLoopback == 0 {
		t.Fatalf("container branch returned %v; it must also trust the container subnet", containerResult)
	}

	detectContainerMarker = func() string { return "" }
	hostResult, err := deriveTrustedProxies()
	if err != nil {
		t.Fatalf("host branch returned error: %v", err)
	}
	assertExactlyLoopback(t, hostResult, "host branch")
}

// The returned slice must not alias the package-level loopback list, or a caller
// appending to it would corrupt the next derivation.
func TestDeriveTrustedProxiesHostBranchDoesNotAliasLoopback(t *testing.T) {
	original := detectContainerMarker
	t.Cleanup(func() { detectContainerMarker = original })
	detectContainerMarker = func() string { return "" }

	// Snapshot the expected value first: comparing against loopbackProxies
	// afterwards would compare the list against itself once it is aliased.
	expected := append([]string(nil), loopbackProxies...)

	first, err := deriveTrustedProxies()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := range first {
		first[i] = "mutated"
	}

	if !reflect.DeepEqual(loopbackProxies, expected) {
		t.Fatalf("a caller mutating the result changed the package-level list: got %v, want %v", loopbackProxies, expected)
	}
}

// ownIPv4Subnets must return the network address, not the interface address.
// A /22 interface address such as 10.0.90.200 would otherwise be trusted as
// 10.0.90.200/22, a range gin treats differently from 10.0.88.0/22.
func TestOwnIPv4SubnetsReturnsMaskedNetworkAddress(t *testing.T) {
	subnets, err := ownIPv4Subnets()
	if err != nil {
		t.Skipf("no usable interface on this host: %v", err)
	}
	if len(subnets) == 0 {
		t.Fatal("ownIPv4Subnets returned nothing")
	}
	for _, subnet := range subnets {
		// ParseCIDR masks its result, so compare the address as written against
		// the masked network address instead of against the parsed one.
		host, network, err := net.ParseCIDR(subnet)
		if err != nil {
			t.Fatalf("ownIPv4Subnets returned unparsable entry %q: %v", subnet, err)
		}
		if !host.Equal(network.IP) {
			t.Fatalf("ownIPv4Subnets returned %s with host bits set; want the masked network address %s", subnet, network.IP)
		}
	}
}

func isLoopbackEntry(entry string) bool {
	for _, loopback := range loopbackProxies {
		if entry == loopback {
			return true
		}
	}
	return false
}

func assertLoopbackTrusted(t *testing.T, proxies []string, label string) {
	t.Helper()
	for _, loopback := range loopbackProxies {
		found := false
		for _, subnet := range proxies {
			if subnet == loopback {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s returned %v; loopback %s must always be trusted for a same-host reverse proxy", label, proxies, loopback)
		}
	}
}

func assertExactlyLoopback(t *testing.T, proxies []string, label string) {
	t.Helper()
	if len(proxies) != len(loopbackProxies) {
		t.Fatalf("%s returned %v, want exactly the loopback ranges", label, proxies)
	}
	for _, subnet := range proxies {
		if !isLoopbackEntry(subnet) {
			t.Fatalf("%s returned %s; a bare host must not trust its LAN", label, subnet)
		}
	}
}
