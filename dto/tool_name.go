package dto

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

const ToolNameMaxLength = 64

var toolNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func IsValidToolName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && len(name) <= ToolNameMaxLength && toolNamePattern.MatchString(name)
}

// NormalizeToolName returns a deterministic ASCII alias accepted by the
// Responses and Chat Completions tool-name contracts. Valid names are kept
// unchanged; invalid or overlong names retain a readable prefix and receive a
// hash suffix so distinct client names do not silently collide.
func NormalizeToolName(name string) string {
	original := strings.TrimSpace(name)
	if IsValidToolName(original) {
		return original
	}

	const prefix = "newapi_tool_"
	sum := sha256.Sum256([]byte(original))
	suffix := "_" + hex.EncodeToString(sum[:5])
	readable := make([]byte, 0, len(original))
	for _, ch := range original {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '_', ch == '-':
			readable = append(readable, byte(ch))
		default:
			if len(readable) == 0 || readable[len(readable)-1] != '_' {
				readable = append(readable, '_')
			}
		}
	}
	readablePart := strings.Trim(readableString(readable), "_")
	base := prefix + readablePart
	maxBaseLength := ToolNameMaxLength - len(suffix)
	if len(base) > maxBaseLength {
		base = base[:maxBaseLength]
	}
	base = strings.TrimRight(base, "_")
	if base == "" {
		base = strings.TrimSuffix(prefix, "_")
	}
	return base + suffix
}

func readableString(value []byte) string {
	return string(value)
}
