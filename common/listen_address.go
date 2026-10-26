package common

import (
	"net"
	"os"
	"strings"
)

// ResolveListenAddress builds the HTTP server listen address from HOST and the
// effective port. An unset or blank HOST keeps the historical ":port" form,
// which binds every interface — container and bare-metal deployments rely on
// it. Setting HOST narrows the bind, which is what the desktop sidecar does so
// a laptop on a shared network does not expose the console to the LAN.
func ResolveListenAddress(port string) string {
	host := strings.TrimSpace(os.Getenv("HOST"))
	if host == "" {
		return ":" + port
	}
	return net.JoinHostPort(host, port)
}
