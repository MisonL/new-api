package common

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// TrustedProxiesAuto is the TRUSTED_PROXIES value that asks the process to
// derive its own proxy network instead of naming it.
const TrustedProxiesAuto = "auto"

// ResolveTrustedProxies turns the TRUSTED_PROXIES setting into the list handed
// to gin's SetTrustedProxies.
//
// An empty value returns nil, which disables proxy trust entirely: the
// forwarding headers are ignored and every client is identified by its socket
// address.
//
// The literal "auto" returns the IPv4 subnets of this process's own network
// interfaces. In a container the reverse proxy reaches the process from the
// container's own subnet, so that subnet is exactly the set of addresses whose
// forwarding headers may be believed. This is the setting a container
// deployment wants: it needs no knowledge of the host's bridge network, which
// Docker assigns and can change when the network is recreated.
//
// Any other value is a comma-separated list of IPs or CIDRs, returned as
// written so that a typo surfaces as an error from SetTrustedProxies rather
// than as silently narrowed trust.
func ResolveTrustedProxies(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if strings.EqualFold(trimmed, TrustedProxiesAuto) {
		return deriveOwnSubnets()
	}

	parts := strings.Split(trimmed, ",")
	proxies := make([]string, 0, len(parts))
	for _, part := range parts {
		if entry := strings.TrimSpace(part); entry != "" {
			proxies = append(proxies, entry)
		}
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("TRUSTED_PROXIES=%q contains no usable entry", raw)
	}
	return proxies, nil
}

// deriveOwnSubnets collects the IPv4 subnet of every usable interface.
//
// The result is sorted so that the value is stable across restarts and can be
// compared between runs.
func deriveOwnSubnets() ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to list network interfaces: %w", err)
	}

	seen := make(map[string]struct{})
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, fmt.Errorf("failed to read addresses of interface %s: %w", iface.Name, err)
		}
		for _, addr := range addrs {
			network, ok := addr.(*net.IPNet)
			if !ok || network.IP.To4() == nil {
				continue
			}
			ones, bits := network.Mask.Size()
			if bits != 32 || ones == 0 {
				continue
			}
			seen[fmt.Sprintf("%s/%d", network.IP.Mask(network.Mask).String(), ones)] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil, fmt.Errorf("TRUSTED_PROXIES=%s found no non-loopback IPv4 interface; set an explicit list instead", TrustedProxiesAuto)
	}

	subnets := make([]string, 0, len(seen))
	for subnet := range seen {
		subnets = append(subnets, subnet)
	}
	sort.Strings(subnets)
	return subnets, nil
}
