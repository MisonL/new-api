package common

import "strings"

// credentialHeaderNames lists the headers that carry an upstream credential.
// A value under one of these names belongs to the caller, and copying it into
// an upstream request hands the caller's own key to a third party. Two code
// paths copy client headers upstream -- the wildcard/regex passthrough rule and
// the param_override pass_headers rule -- so the list lives here to keep them
// from drifting apart.
var credentialHeaderNames = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"api-key":             {},
	"x-api-key":           {},
	"x-goog-api-key":      {},
	"cookie":              {},
}

// IsCredentialHeader reports whether the named header carries a credential.
// Matching is case-insensitive and tolerates surrounding whitespace, matching
// how header names arrive from config files and HTTP.
func IsCredentialHeader(name string) bool {
	_, ok := credentialHeaderNames[strings.ToLower(strings.TrimSpace(name))]
	return ok
}
