package common

import (
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
)

// TrustedProxiesAuto is the TRUSTED_PROXIES value that asks the process to work
// out its own proxy addresses instead of being told them.
const TrustedProxiesAuto = "auto"

// containerMarkers are files and cgroup fragments that identify a container
// runtime. Beyond the loopback addresses both cases trust, a container also
// reaches its reverse proxy over the container network, and the bare host must
// not trust its LAN, so auto has to tell the two apart.
var containerMarkers = []string{
	"/.dockerenv",
	"/run/.containerenv",
}

var containerCgroupFragments = []string{
	"docker", "kubepods", "containerd", "podman", "lxc",
}

// ResolveTrustedProxies turns the TRUSTED_PROXIES setting into the list handed
// to gin's SetTrustedProxies.
//
// An empty value returns nil, which disables proxy trust entirely: forwarding
// headers are ignored and every client is identified by its socket address.
//
// The literal "auto" inspects this process's environment. Loopback is always
// trusted, because a reverse proxy on the same machine reaches the process from
// there in every deployment shape. Beyond that:
//
//   - In a container, the proxy also reaches the process from the container's
//     own subnet, so that subnet is trusted too. This needs no knowledge of the
//     host's bridge network, which the runtime assigns and may change.
//   - On a bare host, only loopback is trusted. Trusting the host's LAN
//     interfaces instead would be far too broad: a remote caller could set its
//     own X-Forwarded-For and pick the address the rate limiters key on.
//
// Any other value is a comma-separated list of IPs or CIDRs, returned as
// written so that a typo surfaces as an error from SetTrustedProxies rather
// than as silently narrowed trust. "auto" is recognized as a whole entry, so a
// stray comma around it ("auto,", ", auto") still selects auto rather than
// being passed to gin as the literal string "auto"; combining it with other
// entries is rejected, because the two readings (replace the derived set or add
// to it) differ and the setting documents auto as standing alone.
func ResolveTrustedProxies(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	parts := strings.Split(trimmed, ",")
	proxies := make([]string, 0, len(parts))
	autoEntries := 0
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, TrustedProxiesAuto) {
			autoEntries++
			continue
		}
		proxies = append(proxies, entry)
	}

	if autoEntries > 0 {
		if len(proxies) > 0 {
			return nil, fmt.Errorf("TRUSTED_PROXIES=%q mixes %q with explicit entries; use either %q alone or an explicit list", raw, TrustedProxiesAuto, TrustedProxiesAuto)
		}
		return deriveTrustedProxies()
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("TRUSTED_PROXIES=%q contains no usable entry", raw)
	}
	return proxies, nil
}

// loopbackProxies is the trust set for a same-host reverse proxy. In a container
// these addresses only accept connections from that container's own network
// namespace, so trusting them is safe there too; a container sharing the host's
// network namespace is the case that actually needs them.
var loopbackProxies = []string{"127.0.0.0/8", "::1/128"}

func deriveTrustedProxies() ([]string, error) {
	marker := detectContainerMarker()
	if marker == "" {
		SysLog(fmt.Sprintf("TRUSTED_PROXIES=%s: running on a host, trusting loopback for a same-host reverse proxy", TrustedProxiesAuto))
		return append([]string(nil), loopbackProxies...), nil
	}

	subnets, err := ownIPv4Subnets()
	if err != nil {
		return nil, err
	}
	SysLog(fmt.Sprintf("TRUSTED_PROXIES=%s: running in a container (%s), trusting loopback and the container subnet", TrustedProxiesAuto, marker))
	proxies := make([]string, 0, len(loopbackProxies)+len(subnets))
	proxies = append(proxies, loopbackProxies...)
	proxies = append(proxies, subnets...)
	return proxies, nil
}

// detectContainerMarker reports which signal identified a container, or an
// empty string on a bare host. It is a variable so tests can exercise both
// branches without the container marker files.
var detectContainerMarker = func() string {
	for _, marker := range containerMarkers {
		if _, err := os.Stat(marker); err == nil {
			return marker
		}
	}
	content, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	text := string(content)
	for _, fragment := range containerCgroupFragments {
		if strings.Contains(text, fragment) {
			return "cgroup:" + fragment
		}
	}
	return ""
}

// ownIPv4Subnets collects the IPv4 subnet of every usable interface.
//
// The result is sorted so that the value is stable across restarts and can be
// compared between runs.
func ownIPv4Subnets() ([]string, error) {
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
		return nil, fmt.Errorf("TRUSTED_PROXIES=%s found no non-loopback IPv4 interface in this container; set an explicit list instead", TrustedProxiesAuto)
	}

	subnets := make([]string, 0, len(seen))
	for subnet := range seen {
		subnets = append(subnets, subnet)
	}
	sort.Strings(subnets)
	return subnets, nil
}
