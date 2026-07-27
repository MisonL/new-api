package common

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateRequiredDiskBodyStorageSynchronizesExistingCacheUsage(t *testing.T) {
	cachePath := t.TempDir()
	previousConfig := GetDiskCacheConfig()
	previousStatsPath := requiredDiskBodyStorageStatsPath
	SetDiskCacheConfig(DiskCacheConfig{Path: cachePath})
	ResetDiskCacheUsage()
	requiredDiskBodyStorageStatsPath = ""
	t.Cleanup(func() {
		SetDiskCacheConfig(previousConfig)
		SyncDiskCacheStats()
		requiredDiskBodyStorageStatsPath = previousStatsPath
	})

	require.NoError(t, EnsureDiskCacheDir())
	existingBodyPath := filepath.Join(GetDiskCacheDir(), "body-existing.tmp")
	require.NoError(t, os.WriteFile(existingBodyPath, bytes.Repeat([]byte("x"), 2<<20), 0600))

	storage, err := CreateRequiredDiskBodyStorageFromReader(
		bytes.NewReader(bytes.Repeat([]byte("y"), 2<<20)),
		RequiredDiskBodyStorageOptions{
			MaxBytes:     4 << 20,
			MaxDiskBytes: 3 << 20,
			MinFreeBytes: 0,
		},
	)
	require.Nil(t, storage)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRequiredDiskBodyStorageCapacity))
}
