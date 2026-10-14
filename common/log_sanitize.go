package common

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// SanitizeLogLine neutralizes raw line breaks in request-controlled text so a
// log entry cannot be split into a forged second entry. Newlines are replaced
// with their escaped form (readable in the log, unambiguous to parse back),
// carriage returns with an escape when standalone.
func SanitizeLogLine(line string) string {
	if !strings.ContainsAny(line, "\n\r") {
		return line
	}
	replacer := strings.NewReplacer(
		"\r\n", `\n`,
		"\n", `\n`,
		"\r", `\r`,
	)
	return replacer.Replace(line)
}

// LogPayloadDigest summarizes a request/response payload for audit logs
// without recording its content: a length plus a truncated SHA-256, so lines
// stay correlatable (same payload, same digest) while no secret body is
// written to disk.
func LogPayloadDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "len=" + strconv.Itoa(len(payload)) + "/sha256=" + hex.EncodeToString(sum[:6]) + fmt.Sprintf("")
}
