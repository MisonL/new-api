package openaicompat

import (
	"regexp"
	"sync"
)

var compiledRegexCache sync.Map // map[string]*regexp.Regexp

func matchAnyRegex(patterns []string, s string) bool {
	if s == "" {
		return false
	}
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		re, ok := compiledRegexCache.Load(pattern)
		if !ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				// Treat invalid patterns as non-matching to avoid breaking runtime traffic.
				continue
			}
			re = compiled
			compiledRegexCache.Store(pattern, re)
		}
		if re.(*regexp.Regexp).MatchString(s) {
			return true
		}
	}
	return false
}
