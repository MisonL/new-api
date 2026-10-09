package common

import (
	"io"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func TestReadAllWithLimitReturnsBodyWithinLimit(t *testing.T) {
	body, err := ReadAllWithLimit(strings.NewReader("hello"), 16)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))
}

func TestReadAllWithLimitAcceptsBodyExactlyAtLimit(t *testing.T) {
	payload := strings.Repeat("x", 16)
	body, err := ReadAllWithLimit(strings.NewReader(payload), 16)
	require.NoError(t, err)
	require.Len(t, body, 16)
}

func TestReadAllWithLimitRejectsBodyOverLimit(t *testing.T) {
	payload := strings.Repeat("x", 17)
	body, err := ReadAllWithLimit(strings.NewReader(payload), 16)
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds")
	require.Nil(t, body)
}

func TestReadAllWithLimitStopsReadingAtLimitPlusOne(t *testing.T) {
	payload := strings.Repeat("x", 1<<20)
	reader := &countingReader{reader: strings.NewReader(payload)}
	_, err := ReadAllWithLimit(reader, 16)
	require.Error(t, err)
	require.Equal(t, int64(17), reader.read)
}

func TestReadAllWithLimitRejectsNilReader(t *testing.T) {
	_, err := ReadAllWithLimit(nil, 16)
	require.Error(t, err)
}

func TestReadAllWithLimitRejectsNonPositiveLimit(t *testing.T) {
	_, err := ReadAllWithLimit(strings.NewReader("hello"), 0)
	require.Error(t, err)
}

func TestReadAllWithLimitRejectsMaxInt64Limit(t *testing.T) {
	// At MaxInt64 the limit+1 probe overflows to a negative count, so a
	// body one byte over the limit would be read as empty and silently
	// accepted. The function must refuse the bound rather than truncate.
	body, err := ReadAllWithLimit(strings.NewReader("hello"), math.MaxInt64)
	require.Error(t, err)
	require.Nil(t, body)
}

func TestReadAllWithLimitAcceptsLimitJustBelowMaxInt64(t *testing.T) {
	// The rejection is exactly at MaxInt64; MaxInt64-1 still probes safely.
	body, err := ReadAllWithLimit(strings.NewReader("hello"), math.MaxInt64-1)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))
}
