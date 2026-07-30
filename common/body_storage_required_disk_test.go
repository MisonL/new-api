package common

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestCreateRequiredDiskBodyStorageWithContextCancelsWhileWaitingForAdmission(t *testing.T) {
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

	releaseFirstReader := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseFirstReader) })
	}
	firstReader := &blockingRequiredDiskBodyStorageReader{
		started: make(chan struct{}),
		release: releaseFirstReader,
		payload: []byte("x"),
	}
	options := RequiredDiskBodyStorageOptions{
		MaxBytes:     8,
		MaxDiskBytes: 16,
		MinFreeBytes: 0,
	}
	var firstStorage BodyStorage
	var firstErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		firstStorage, firstErr = CreateRequiredDiskBodyStorageFromReaderWithContext(context.Background(), firstReader, options)
	}()
	t.Cleanup(func() {
		release()
		waitGroup.Wait()
		if firstStorage != nil {
			_ = firstStorage.Close()
		}
	})

	select {
	case <-firstReader.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first required disk body storage reader")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiterResult := make(chan error, 1)
	go func() {
		storage, err := CreateRequiredDiskBodyStorageFromReaderWithContext(ctx, bytes.NewReader([]byte("second")), options)
		if storage != nil {
			_ = storage.Close()
		}
		waiterResult <- err
	}()
	cancel()

	select {
	case err := <-waiterResult:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for cancelled required disk body storage admission")
	}

	release()
	waitGroup.Wait()
	require.NoError(t, firstErr)
	require.NotNil(t, firstStorage)
	require.NoError(t, firstStorage.Close())
	firstStorage = nil
}

func TestCreateRequiredDiskBodyStorageUnknownLengthAdmissionWaitIsBounded(t *testing.T) {
	cachePath := t.TempDir()
	previousConfig := GetDiskCacheConfig()
	previousStatsPath := requiredDiskBodyStorageStatsPath
	previousAdmissionWait := requiredDiskBodyStorageAdmissionWait
	SetDiskCacheConfig(DiskCacheConfig{Path: cachePath})
	ResetDiskCacheUsage()
	requiredDiskBodyStorageStatsPath = ""
	requiredDiskBodyStorageAdmissionWait = 50 * time.Millisecond
	t.Cleanup(func() {
		SetDiskCacheConfig(previousConfig)
		SyncDiskCacheStats()
		requiredDiskBodyStorageStatsPath = previousStatsPath
		requiredDiskBodyStorageAdmissionWait = previousAdmissionWait
	})

	releaseFirstReader := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseFirstReader) })
	}
	firstReader := &blockingRequiredDiskBodyStorageReader{
		started: make(chan struct{}),
		release: releaseFirstReader,
		payload: []byte("x"),
	}
	options := RequiredDiskBodyStorageOptions{
		MaxBytes:     8,
		MaxDiskBytes: 16,
		MinFreeBytes: 0,
	}
	var firstStorage BodyStorage
	var firstErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		firstStorage, firstErr = CreateRequiredDiskBodyStorageFromReaderWithContext(context.Background(), firstReader, options)
	}()
	t.Cleanup(func() {
		release()
		waitGroup.Wait()
		if firstStorage != nil {
			_ = firstStorage.Close()
		}
	})

	select {
	case <-firstReader.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first required disk body storage reader")
	}

	storage, err := CreateRequiredDiskBodyStorageFromReaderWithContext(
		context.Background(),
		bytes.NewReader([]byte("second")),
		options,
	)
	if storage != nil {
		_ = storage.Close()
	}
	require.Nil(t, storage)
	require.ErrorIs(t, err, ErrRequiredDiskBodyStorageAdmission)
	require.ErrorIs(t, err, ErrRequiredDiskBodyStorageCapacity)

	release()
	waitGroup.Wait()
	require.NoError(t, firstErr)
	require.NotNil(t, firstStorage)
	_ = firstStorage.Close()
	firstStorage = nil
}

func TestCreateRequiredDiskBodyStorageKnownLengthSpoolsConcurrentlyWithinCapacity(t *testing.T) {
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

	payload := []byte("12345678")
	releaseReaders := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseReaders) })
	}
	readers := []*blockingRequiredDiskBodyStorageReader{
		{started: make(chan struct{}), release: releaseReaders, payload: payload},
		{started: make(chan struct{}), release: releaseReaders, payload: payload},
	}
	options := RequiredDiskBodyStorageOptions{
		MaxBytes:      int64(len(payload)),
		MaxDiskBytes:  int64(len(payload) * len(readers)),
		MinFreeBytes:  0,
		ExpectedBytes: int64(len(payload)),
	}

	results := make(chan error, len(readers))
	storages := make([]BodyStorage, 0, len(readers))
	var storagesMu sync.Mutex
	var waitGroup sync.WaitGroup
	for _, reader := range readers {
		waitGroup.Add(1)
		go func(reader io.Reader) {
			defer waitGroup.Done()
			storage, err := CreateRequiredDiskBodyStorageFromReaderWithContext(context.Background(), reader, options)
			if storage != nil {
				storagesMu.Lock()
				storages = append(storages, storage)
				storagesMu.Unlock()
			}
			results <- err
		}(reader)
	}
	t.Cleanup(func() {
		release()
		waitGroup.Wait()
		storagesMu.Lock()
		defer storagesMu.Unlock()
		for _, storage := range storages {
			_ = storage.Close()
		}
	})

	for _, reader := range readers {
		select {
		case <-reader.started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for known-length required disk spools to run concurrently")
		}
	}
	requiredDiskBodyStorageReservationMu.Lock()
	pendingBytes := requiredDiskBodyStoragePendingBytes
	requiredDiskBodyStorageReservationMu.Unlock()
	require.Equal(t, options.MaxDiskBytes, pendingBytes)

	release()
	waitGroup.Wait()
	for range readers {
		require.NoError(t, <-results)
	}
}

func TestCreateRequiredDiskBodyStorageKnownLengthRejectsBodyBeyondDeclaredLimits(t *testing.T) {
	testCases := []struct {
		name         string
		maxBytes     int64
		maxDiskBytes int64
		expectedErr  error
	}{
		{
			name:         "disk capacity",
			maxBytes:     8,
			maxDiskBytes: 6,
			expectedErr:  ErrRequiredDiskBodyStorageCapacity,
		},
		{
			name:         "request body limit",
			maxBytes:     6,
			maxDiskBytes: 16,
			expectedErr:  ErrRequestBodyTooLarge,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
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

			storage, err := CreateRequiredDiskBodyStorageFromReader(
				bytes.NewReader([]byte("12345678")),
				RequiredDiskBodyStorageOptions{
					MaxBytes:      testCase.maxBytes,
					MaxDiskBytes:  testCase.maxDiskBytes,
					MinFreeBytes:  0,
					ExpectedBytes: 4,
				},
			)
			require.Nil(t, storage)
			require.ErrorIs(t, err, testCase.expectedErr)

			entries, readErr := os.ReadDir(GetDiskCacheDir())
			require.NoError(t, readErr)
			require.Empty(t, entries)
		})
	}
}

func TestCreateRequiredDiskBodyStorageWithContextCleansPartialWriteAfterCancellation(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	storage, err := CreateRequiredDiskBodyStorageFromReaderWithContext(
		ctx,
		&cancelAfterPartialRequiredDiskBodyStorageReader{payload: []byte("half"), cancel: cancel},
		RequiredDiskBodyStorageOptions{
			MaxBytes:      8,
			MaxDiskBytes:  8,
			MinFreeBytes:  0,
			ExpectedBytes: 8,
		},
	)

	require.Nil(t, storage)
	require.ErrorIs(t, err, context.Canceled)
	entries, readErr := os.ReadDir(GetDiskCacheDir())
	require.NoError(t, readErr)
	require.Empty(t, entries)
	requiredDiskBodyStorageReservationMu.Lock()
	pendingBytes := requiredDiskBodyStoragePendingBytes
	requiredDiskBodyStorageReservationMu.Unlock()
	require.Zero(t, pendingBytes)
}

func TestCreateRequiredDiskBodyStorageUnknownLengthAdmissionReleasesAfterReadFailure(t *testing.T) {
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

	readFailure := errors.New("reader failed")
	options := RequiredDiskBodyStorageOptions{
		MaxBytes:     8,
		MaxDiskBytes: 16,
		MinFreeBytes: 0,
	}
	storage, err := createRequiredDiskBodyStorageFromReaderWithContext(
		context.Background(),
		errorRequiredDiskBodyStorageReader{err: readFailure},
		options,
		time.Second,
	)
	require.Nil(t, storage)
	require.ErrorIs(t, err, readFailure)

	storage, err = createRequiredDiskBodyStorageFromReaderWithContext(
		context.Background(),
		bytes.NewReader([]byte("second")),
		options,
		50*time.Millisecond,
	)
	require.NoError(t, err)
	require.NotNil(t, storage)
	require.NoError(t, storage.Close())
}

func TestCreateRequiredDiskBodyStorageKnownLengthActualBytesRespectExistingStorageCapacity(t *testing.T) {
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

	payload := []byte("12345678")
	options := RequiredDiskBodyStorageOptions{
		MaxBytes:      int64(len(payload)),
		MaxDiskBytes:  12,
		MinFreeBytes:  0,
		ExpectedBytes: 4,
	}
	first, err := CreateRequiredDiskBodyStorageFromReader(bytes.NewReader(payload), options)
	require.NoError(t, err)
	require.NotNil(t, first)
	defer func() { _ = first.Close() }()

	second, err := CreateRequiredDiskBodyStorageFromReader(bytes.NewReader(payload), options)
	require.Nil(t, second)
	require.ErrorIs(t, err, ErrRequiredDiskBodyStorageCapacity)
	requiredDiskBodyStorageReservationMu.Lock()
	pendingBytes := requiredDiskBodyStoragePendingBytes
	requiredDiskBodyStorageReservationMu.Unlock()
	require.Zero(t, pendingBytes)

	entries, readErr := os.ReadDir(GetDiskCacheDir())
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	require.NoError(t, first.Close())
	entries, readErr = os.ReadDir(GetDiskCacheDir())
	require.NoError(t, readErr)
	require.Empty(t, entries)
}

type blockingRequiredDiskBodyStorageReader struct {
	started chan struct{}
	release <-chan struct{}
	payload []byte
	offset  int
}

type cancelAfterPartialRequiredDiskBodyStorageReader struct {
	payload []byte
	cancel  context.CancelFunc
	read    bool
}

type errorRequiredDiskBodyStorageReader struct {
	err error
}

func (r errorRequiredDiskBodyStorageReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

func (r *cancelAfterPartialRequiredDiskBodyStorageReader) Read(target []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	n := copy(target, r.payload)
	r.cancel()
	return n, nil
}

func (r *blockingRequiredDiskBodyStorageReader) Read(target []byte) (int, error) {
	if r.offset >= len(r.payload) {
		return 0, io.EOF
	}
	if r.offset == 0 {
		close(r.started)
		<-r.release
	}
	n := copy(target, r.payload[r.offset:])
	r.offset += n
	return n, nil
}
