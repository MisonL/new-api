package model

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// parsedLogIndexNames returns the index names GORM derives from the Log
// struct tags.
func parsedLogIndexNames(t *testing.T) map[string]struct{} {
	t.Helper()
	s, err := schema.Parse(&Log{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("failed to parse Log schema: %v", err)
	}
	names := make(map[string]struct{})
	for name := range s.ParseIndexes() {
		names[name] = struct{}{}
	}
	return names
}
