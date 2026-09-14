package common

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ChannelRouteIdentity is the immutable routing identity used by channel
// health. It is built before dispatch and carried through retries, so the
// health lookup and failure record cannot silently switch from the requested
// model to a mapped model (or from one protocol/path to another).
type ChannelRouteIdentity struct {
	RequestedModel         string `json:"requested_model,omitempty"`
	EffectiveUpstreamModel string `json:"effective_upstream_model,omitempty"`
	FinalProtocol          string `json:"final_protocol,omitempty"`
	// Operation distinguishes provider/action operations that intentionally
	// share the same HTTP path. It is part of the health identity and must be
	// stable across retries and asynchronous follow-up requests.
	Operation     string `json:"operation,omitempty"`
	UpstreamPath  string `json:"upstream_path,omitempty"`
	ChannelID     int    `json:"channel_id"`
	MultiKeyIndex int    `json:"multi_key_index,omitempty"`
	// CredentialScoped distinguishes the first concrete multi-key credential
	// (index 0) from a channel-level route, which also uses index 0.
	CredentialScoped bool   `json:"credential_scoped,omitempty"`
	Fingerprint      string `json:"fingerprint,omitempty"`
}

func (route ChannelRouteIdentity) Normalized() ChannelRouteIdentity {
	route.RequestedModel = strings.TrimSpace(route.RequestedModel)
	route.EffectiveUpstreamModel = strings.TrimSpace(route.EffectiveUpstreamModel)
	route.FinalProtocol = strings.TrimSpace(route.FinalProtocol)
	route.Operation = strings.TrimSpace(route.Operation)
	route.UpstreamPath = NormalizeRoutePath(route.UpstreamPath)
	if route.ChannelID < 0 {
		route.ChannelID = 0
	}
	if route.MultiKeyIndex < 0 {
		route.MultiKeyIndex = 0
	}
	if route.Fingerprint == "" {
		route.Fingerprint = route.ComputeFingerprint()
	}
	return route
}

func (route ChannelRouteIdentity) Canonical() string {
	route = route.Normalized()
	parts := []string{
		strconvOrEmpty(route.ChannelID),
		route.RequestedModel,
		route.EffectiveUpstreamModel,
		route.FinalProtocol,
	}
	if route.Operation != "" {
		parts = append(parts, route.Operation)
	}
	parts = append(parts, route.UpstreamPath, strconvOrEmpty(route.MultiKeyIndex))
	canonical := strings.Join(parts, "\x00")
	if route.CredentialScoped {
		canonical += "\x00credential"
	}
	return canonical
}

func (route ChannelRouteIdentity) ComputeFingerprint() string {
	digest := sha256.Sum256([]byte(route.CanonicalWithoutFingerprint()))
	return hex.EncodeToString(digest[:])
}

func (route ChannelRouteIdentity) CanonicalWithoutFingerprint() string {
	parts := []string{
		strconvOrEmpty(route.ChannelID),
		strings.TrimSpace(route.RequestedModel),
		strings.TrimSpace(route.EffectiveUpstreamModel),
		strings.TrimSpace(route.FinalProtocol),
	}
	if strings.TrimSpace(route.Operation) != "" {
		parts = append(parts, strings.TrimSpace(route.Operation))
	}
	parts = append(parts, NormalizeRoutePath(route.UpstreamPath), strconvOrEmpty(route.MultiKeyIndex))
	canonical := strings.Join(parts, "\x00")
	if route.CredentialScoped {
		canonical += "\x00credential"
	}
	return canonical
}

func NormalizeRoutePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if index := strings.IndexByte(path, '?'); index >= 0 {
		path = path[:index]
	}
	if index := strings.Index(path, "://"); index >= 0 {
		rest := path[index+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			path = rest[slash:]
		} else {
			path = "/"
		}
	}
	if strings.HasPrefix(path, "/pg/") {
		path = "/v1/" + strings.TrimPrefix(path, "/pg/")
	} else if path == "/pg" {
		path = "/v1"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func strconvOrEmpty(value int) string {
	return fmt.Sprintf("%d", value)
}
