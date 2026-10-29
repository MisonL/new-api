package common

import "strings"

// SanitizeMultipartFilename makes a client-supplied multipart filename safe to embed in an
// HTTP header value. Control characters (CR, LF, NUL, and other C0 controls) are stripped so a
// filename decoded from an RFC 2231 `filename*` parameter cannot inject extra header lines into
// the multipart part (Go's mime/multipart.CreatePart does not escape the value it is given).
// Returns "file" when nothing printable remains so callers never emit an empty filename.
func SanitizeMultipartFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "file"
	}
	return b.String()
}

// SanitizeMultipartFieldName applies the same control-character stripping to a multipart
// field (part) name. Client-controlled names can carry CRLF the same way filenames can
// (RFC 2231 `name*`), and the value is spliced into Content-Disposition by hand.
func SanitizeMultipartFieldName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "field"
	}
	return b.String()
}
