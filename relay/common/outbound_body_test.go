package common

import (
	"bytes"
	"io"
	"testing"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestNewOutboundJSONBodyReleasesDiskStorage(t *testing.T) {
	previousConfig := rootcommon.GetDiskCacheConfig()
	rootcommon.SetDiskCacheConfig(rootcommon.DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 1,
		MaxSizeMB:   16,
		Path:        t.TempDir(),
	})
	t.Cleanup(func() {
		rootcommon.SetDiskCacheConfig(previousConfig)
	})

	data := bytes.Repeat([]byte("x"), 2<<20)
	body, size, getBody, closer, err := NewOutboundJSONBody(data)

	require.NoError(t, err)
	require.Equal(t, int64(len(data)), size)
	storage, ok := closer.(rootcommon.BodyStorage)
	require.True(t, ok)
	require.True(t, storage.IsDisk())
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, data, got)
	replay, err := getBody()
	require.NoError(t, err)
	replayPrefix := make([]byte, 9)
	_, err = io.ReadFull(replay, replayPrefix)
	require.NoError(t, err)
	require.Equal(t, data[:9], replayPrefix)
	require.NoError(t, replay.Close())
	require.NoError(t, closer.Close())
	_, err = storage.Bytes()
	require.ErrorIs(t, err, rootcommon.ErrStorageClosed)
	_, err = getBody()
	require.ErrorIs(t, err, rootcommon.ErrStorageClosed)
}

func assertIndependentReplayReaders(t *testing.T, payload []byte, body io.Reader, getBody func() (io.ReadCloser, error)) {
	t.Helper()
	half := len(payload) / 2

	primaryHead := make([]byte, half)
	_, err := io.ReadFull(body, primaryHead)
	require.NoError(t, err)
	require.Equal(t, payload[:half], primaryHead)

	a, err := getBody()
	require.NoError(t, err)
	b, err := getBody()
	require.NoError(t, err)

	aHead := make([]byte, half)
	_, err = io.ReadFull(a, aHead)
	require.NoError(t, err)
	require.Equal(t, payload[:half], aHead)

	bAll, err := io.ReadAll(b)
	require.NoError(t, err)
	require.NoError(t, b.Close())
	require.Equal(t, payload, bAll)

	aRest, err := io.ReadAll(a)
	require.NoError(t, err)
	require.NoError(t, a.Close())
	require.Equal(t, payload[half:], aRest)

	primaryRest, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, payload[half:], primaryRest)
}

func TestNewOutboundJSONBodyReplayReadersAreIndependentInMemory(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"model":"test-model","input":"abcdefghijklmnopqrstuvwxyz"}`)
	body, _, getBody, closer, err := NewOutboundJSONBody(payload)
	require.NoError(t, err)
	defer closer.Close()

	assertIndependentReplayReaders(t, payload, body, getBody)
	require.NoError(t, closer.Close())
	_, err = getBody()
	require.ErrorIs(t, err, rootcommon.ErrStorageClosed)
}

func TestNewOutboundJSONBodyReplayReadersAreIndependentOnDisk(t *testing.T) {
	previousConfig := rootcommon.GetDiskCacheConfig()
	rootcommon.SetDiskCacheConfig(rootcommon.DiskCacheConfig{
		Enabled:     true,
		ThresholdMB: 0,
		MaxSizeMB:   64,
		Path:        t.TempDir(),
	})
	t.Cleanup(func() { rootcommon.SetDiskCacheConfig(previousConfig) })

	payload := []byte(`{"model":"test-model","input":"abcdefghijklmnopqrstuvwxyz"}`)
	body, _, getBody, closer, err := NewOutboundJSONBody(payload)
	require.NoError(t, err)
	storage, ok := closer.(rootcommon.BodyStorage)
	require.True(t, ok)
	require.True(t, storage.IsDisk())

	assertIndependentReplayReaders(t, payload, body, getBody)
	require.NoError(t, closer.Close())
	_, err = getBody()
	require.ErrorIs(t, err, rootcommon.ErrStorageClosed)
}
