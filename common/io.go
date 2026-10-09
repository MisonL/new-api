package common

import (
	"errors"
	"fmt"
	"io"
	"math"
)

// ReadAllWithLimit reads a response body with an explicit byte limit. An
// oversized body returns an error instead of the truncated prefix that
// io.LimitReader alone would silently produce, so callers never parse or
// persist a partial payload as if it were complete.
func ReadAllWithLimit(reader io.Reader, limit int64) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("response body reader is nil")
	}
	if limit <= 0 {
		return nil, errors.New("response body limit must be positive")
	}
	if limit == math.MaxInt64 {
		// Detection needs a probe byte at limit+1; at MaxInt64 that offset does
		// not exist, so accepting the call would mean truncating a body one byte
		// over the limit without reporting it. Refuse instead of degrading.
		return nil, errors.New("response body limit must be below MaxInt64")
	}
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	return body, nil
}
