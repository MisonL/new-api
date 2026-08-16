package common

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// BodyStorage 请求体存储接口
type BodyStorage interface {
	io.ReadSeeker
	io.Closer
	// Bytes 获取全部内容
	Bytes() ([]byte, error)
	// Size 获取数据大小
	Size() int64
	// IsDisk 是否是磁盘存储
	IsDisk() bool
}

// ErrStorageClosed 存储已关闭错误
var ErrStorageClosed = fmt.Errorf("body storage is closed")

var ErrRequiredDiskBodyStorageCapacity = errors.New("required disk body storage capacity exhausted")
var ErrRequiredDiskBodyStorageAdmission = fmt.Errorf("%w: unknown-length spool admission wait exceeded", ErrRequiredDiskBodyStorageCapacity)
var ErrRequiredDiskBodyStorageReserve = errors.New("required disk body storage reserve unavailable")
var ErrRequiredDiskBodyStorageWrite = errors.New("required disk body storage write failed")

var requiredDiskBodyStorageAdmissionWait = 30 * time.Second

// RequiredDiskBodyStorageOptions defines the hard limits for a body that must
// stay on disk. It is intended for authenticated protocol paths where a
// memory fallback would be unsafe.
type RequiredDiskBodyStorageOptions struct {
	MaxBytes     int64
	MaxDiskBytes int64
	MinFreeBytes int64
	// ExpectedBytes is a trusted positive body length when one is available.
	// Non-positive values use the unknown-length admission path.
	ExpectedBytes int64
}

type requiredDiskBodyStorageReservation struct {
	reserved int64
}

var requiredDiskBodyStorageReservationMu sync.Mutex
var requiredDiskBodyStoragePendingBytes int64
var requiredDiskBodyStorageStatsPath string
var requiredDiskBodyStorageSpoolAdmission = make(chan struct{}, 1)

func synchronizeRequiredDiskBodyStorageStats() {
	cacheDir := GetDiskCacheDir()
	requiredDiskBodyStorageReservationMu.Lock()
	defer requiredDiskBodyStorageReservationMu.Unlock()
	if requiredDiskBodyStoragePendingBytes != 0 || requiredDiskBodyStorageStatsPath == cacheDir {
		return
	}
	SyncDiskCacheStats()
	requiredDiskBodyStorageStatsPath = cacheDir
}

func (r *requiredDiskBodyStorageReservation) reserve(bytes int64, maxDiskBytes int64) error {
	if bytes <= 0 {
		return nil
	}
	requiredDiskBodyStorageReservationMu.Lock()
	defer requiredDiskBodyStorageReservationMu.Unlock()
	if maxDiskBytes <= 0 {
		return ErrRequiredDiskBodyStorageCapacity
	}
	currentUsage := GetDiskCacheStats().CurrentDiskUsageBytes
	if currentUsage > maxDiskBytes || bytes > maxDiskBytes-currentUsage-requiredDiskBodyStoragePendingBytes {
		return ErrRequiredDiskBodyStorageCapacity
	}
	requiredDiskBodyStoragePendingBytes += bytes
	r.reserved += bytes
	return nil
}

func (r *requiredDiskBodyStorageReservation) release(bytes int64) {
	if bytes <= 0 {
		return
	}
	requiredDiskBodyStorageReservationMu.Lock()
	defer requiredDiskBodyStorageReservationMu.Unlock()
	if bytes > r.reserved {
		bytes = r.reserved
	}
	r.reserved -= bytes
	requiredDiskBodyStoragePendingBytes -= bytes
	if requiredDiskBodyStoragePendingBytes < 0 {
		requiredDiskBodyStoragePendingBytes = 0
	}
}

func (r *requiredDiskBodyStorageReservation) releaseAll() {
	r.release(r.reserved)
}

func validateRequiredDiskBodyStorageOptions(options RequiredDiskBodyStorageOptions) error {
	if options.MaxBytes <= 0 || options.MaxDiskBytes <= 0 {
		return ErrRequiredDiskBodyStorageCapacity
	}
	if options.MinFreeBytes < 0 {
		return ErrRequiredDiskBodyStorageReserve
	}
	if options.ExpectedBytes > options.MaxBytes {
		return ErrRequestBodyTooLarge
	}
	return nil
}

func ensureRequiredDiskBodyStorageReserve(options RequiredDiskBodyStorageOptions) error {
	if err := EnsureDiskCacheDir(); err != nil {
		return fmt.Errorf("%w: %v", ErrRequiredDiskBodyStorageReserve, err)
	}
	info := GetDiskSpaceInfo()
	if info.Free == 0 || info.Free < uint64(options.MinFreeBytes) {
		return fmt.Errorf("%w: free space is unavailable", ErrRequiredDiskBodyStorageReserve)
	}
	return nil
}

func ensureRequiredDiskBodyStorageWriteReserve(options RequiredDiskBodyStorageOptions, bytes int64) error {
	if bytes <= 0 {
		return nil
	}
	if uint64(bytes) > ^uint64(0)-uint64(options.MinFreeBytes) {
		return ErrRequiredDiskBodyStorageReserve
	}
	info := GetDiskSpaceInfo()
	requiredFreeBytes := uint64(options.MinFreeBytes) + uint64(bytes)
	if info.Free < requiredFreeBytes {
		return fmt.Errorf("%w: available=%d required=%d", ErrRequiredDiskBodyStorageReserve, info.Free, requiredFreeBytes)
	}
	return nil
}

// CreateRequiredDiskBodyStorageFromReader spools a body directly to a private
// disk file. Unknown-length callers without a request context get a bounded
// admission wait instead of waiting behind another spool indefinitely. Callers
// that can propagate cancellation should use
// CreateRequiredDiskBodyStorageFromReaderWithContext.
func CreateRequiredDiskBodyStorageFromReader(reader io.Reader, options RequiredDiskBodyStorageOptions) (BodyStorage, error) {
	return createRequiredDiskBodyStorageFromReaderWithContext(
		context.Background(),
		reader,
		options,
		requiredDiskBodyStorageAdmissionWait,
	)
}

// CreateRequiredDiskBodyStorageFromReaderWithContext spools a body directly to
// a private disk file. It never falls back to memory. A trusted positive
// ExpectedBytes value reserves the entire body before writing, allowing bodies
// within the aggregate capacity to spool concurrently. Unknown-length streams
// hold a single admission through their write phase so concurrent requests
// cannot each reserve a partial body, exhaust the same aggregate budget, and
// all fail. Waiting callers have a bounded admission wait and can leave the
// queue earlier when their context is cancelled.
func CreateRequiredDiskBodyStorageFromReaderWithContext(ctx context.Context, reader io.Reader, options RequiredDiskBodyStorageOptions) (BodyStorage, error) {
	return createRequiredDiskBodyStorageFromReaderWithContext(ctx, reader, options, requiredDiskBodyStorageAdmissionWait)
}

func createRequiredDiskBodyStorageFromReaderWithContext(ctx context.Context, reader io.Reader, options RequiredDiskBodyStorageOptions, unknownLengthAdmissionWait time.Duration) (BodyStorage, error) {
	if reader == nil {
		return nil, fmt.Errorf("%w: reader is required", ErrRequiredDiskBodyStorageWrite)
	}
	if err := validateRequiredDiskBodyStorageOptions(options); err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if options.ExpectedBytes <= 0 {
		if err := acquireRequiredDiskBodyStorageSpoolAdmission(ctx, unknownLengthAdmissionWait); err != nil {
			return nil, err
		}
		defer func() { <-requiredDiskBodyStorageSpoolAdmission }()
	}

	if err := ensureRequiredDiskBodyStorageReserve(options); err != nil {
		return nil, err
	}
	synchronizeRequiredDiskBodyStorageStats()

	reservation := &requiredDiskBodyStorageReservation{}
	if options.ExpectedBytes > 0 {
		if err := ensureRequiredDiskBodyStorageWriteReserve(options, options.ExpectedBytes); err != nil {
			return nil, err
		}
		if err := reservation.reserve(options.ExpectedBytes, options.MaxDiskBytes); err != nil {
			return nil, err
		}
	}

	filePath, file, err := CreateDiskCacheFile(DiskCacheTypeBody)
	if err != nil {
		reservation.releaseAll()
		return nil, fmt.Errorf("%w: %v", ErrRequiredDiskBodyStorageWrite, err)
	}

	success := false
	defer func() {
		if success {
			return
		}
		reservation.releaseAll()
		_ = file.Close()
		_ = os.Remove(filePath)
	}()

	buffer := make([]byte, 128*1024)
	var written int64
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			if int64(n) > options.MaxBytes-written {
				return nil, ErrRequestBodyTooLarge
			}
			if err := ensureRequiredDiskBodyStorageWriteReserve(options, int64(n)); err != nil {
				return nil, err
			}
			nextWritten := written + int64(n)
			if additionalReservation := nextWritten - reservation.reserved; additionalReservation > 0 {
				if err := reservation.reserve(additionalReservation, options.MaxDiskBytes); err != nil {
					return nil, err
				}
			}
			writeN, writeErr := file.Write(buffer[:n])
			if writeN != n || writeErr != nil {
				return nil, fmt.Errorf("%w: %v", ErrRequiredDiskBodyStorageWrite, writeErr)
			}
			written += int64(writeN)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read required disk body storage: %w", readErr)
		}
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("%w: seek temp file: %v", ErrRequiredDiskBodyStorageWrite, err)
	}
	IncrementDiskFiles(written)
	reservation.releaseAll()
	success = true
	return &diskStorage{
		file:     file,
		filePath: filePath,
		size:     written,
	}, nil
}

func acquireRequiredDiskBodyStorageSpoolAdmission(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		select {
		case requiredDiskBodyStorageSpoolAdmission <- struct{}{}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case requiredDiskBodyStorageSpoolAdmission <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ErrRequiredDiskBodyStorageAdmission
	}
}

// memoryStorage 内存存储实现
type memoryStorage struct {
	data   []byte
	reader *bytes.Reader
	size   int64
	closed int32
	mu     sync.Mutex
}

func newMemoryStorage(data []byte) *memoryStorage {
	size := int64(len(data))
	IncrementMemoryBuffers(size)
	return &memoryStorage{
		data:   data,
		reader: bytes.NewReader(data),
		size:   size,
	}
}

func (m *memoryStorage) Read(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if atomic.LoadInt32(&m.closed) == 1 {
		return 0, ErrStorageClosed
	}
	return m.reader.Read(p)
}

func (m *memoryStorage) Seek(offset int64, whence int) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if atomic.LoadInt32(&m.closed) == 1 {
		return 0, ErrStorageClosed
	}
	return m.reader.Seek(offset, whence)
}

func (m *memoryStorage) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if atomic.CompareAndSwapInt32(&m.closed, 0, 1) {
		DecrementMemoryBuffers(m.size)
	}
	return nil
}

func (m *memoryStorage) Bytes() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if atomic.LoadInt32(&m.closed) == 1 {
		return nil, ErrStorageClosed
	}
	return m.data, nil
}

func (m *memoryStorage) Size() int64 {
	return m.size
}

func (m *memoryStorage) IsDisk() bool {
	return false
}

// diskStorage 磁盘存储实现
type diskStorage struct {
	file     *os.File
	filePath string
	size     int64
	closed   int32
	mu       sync.Mutex
}

func newDiskStorage(data []byte, cachePath string) (*diskStorage, error) {
	// 使用统一的缓存目录管理
	filePath, file, err := CreateDiskCacheFile(DiskCacheTypeBody)
	if err != nil {
		return nil, err
	}

	// 写入数据
	n, err := file.Write(data)
	if err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to write to temp file: %w", err)
	}

	// 重置文件指针
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to seek temp file: %w", err)
	}

	size := int64(n)
	IncrementDiskFiles(size)

	return &diskStorage{
		file:     file,
		filePath: filePath,
		size:     size,
	}, nil
}

func newDiskStorageFromReader(reader io.Reader, maxBytes int64, cachePath string) (*diskStorage, error) {
	// 使用统一的缓存目录管理
	filePath, file, err := CreateDiskCacheFile(DiskCacheTypeBody)
	if err != nil {
		return nil, err
	}

	// 从 reader 读取并写入文件
	written, err := io.Copy(file, io.LimitReader(reader, maxBytes+1))
	if err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to write to temp file: %w", err)
	}

	if written > maxBytes {
		file.Close()
		os.Remove(filePath)
		return nil, ErrRequestBodyTooLarge
	}

	// 重置文件指针
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		os.Remove(filePath)
		return nil, fmt.Errorf("failed to seek temp file: %w", err)
	}

	IncrementDiskFiles(written)

	return &diskStorage{
		file:     file,
		filePath: filePath,
		size:     written,
	}, nil
}

func (d *diskStorage) Read(p []byte) (n int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if atomic.LoadInt32(&d.closed) == 1 {
		return 0, ErrStorageClosed
	}
	return d.file.Read(p)
}

func (d *diskStorage) Seek(offset int64, whence int) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if atomic.LoadInt32(&d.closed) == 1 {
		return 0, ErrStorageClosed
	}
	return d.file.Seek(offset, whence)
}

func (d *diskStorage) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if atomic.CompareAndSwapInt32(&d.closed, 0, 1) {
		d.file.Close()
		os.Remove(d.filePath)
		DecrementDiskFiles(d.size)
	}
	return nil
}

func (d *diskStorage) Bytes() ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if atomic.LoadInt32(&d.closed) == 1 {
		return nil, ErrStorageClosed
	}

	// 保存当前位置
	currentPos, err := d.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	// 移动到开头
	if _, err := d.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	// 读取全部内容
	data := make([]byte, d.size)
	_, err = io.ReadFull(d.file, data)
	if err != nil {
		return nil, err
	}

	// 恢复位置
	if _, err := d.file.Seek(currentPos, io.SeekStart); err != nil {
		return nil, err
	}

	return data, nil
}

func (d *diskStorage) Size() int64 {
	return d.size
}

func (d *diskStorage) IsDisk() bool {
	return true
}

// CreateBodyStorage 根据数据大小创建合适的存储
func CreateBodyStorage(data []byte) (BodyStorage, error) {
	size := int64(len(data))
	threshold := GetDiskCacheThresholdBytes()

	// 检查是否应该使用磁盘缓存
	if IsDiskCacheEnabled() &&
		size >= threshold &&
		IsDiskCacheAvailable(size) {
		storage, err := newDiskStorage(data, GetDiskCachePath())
		if err != nil {
			// 如果磁盘存储失败，回退到内存存储
			SysError(fmt.Sprintf("failed to create disk storage, falling back to memory: %v", err))
			return newMemoryStorage(data), nil
		}
		return storage, nil
	}

	return newMemoryStorage(data), nil
}

// CreateBodyStorageFromReader 从 Reader 创建存储（用于大请求的流式处理）
func CreateBodyStorageFromReader(reader io.Reader, contentLength int64, maxBytes int64) (BodyStorage, error) {
	threshold := GetDiskCacheThresholdBytes()

	// 如果启用了磁盘缓存且内容长度超过阈值，直接使用磁盘存储
	if IsDiskCacheEnabled() &&
		contentLength > 0 &&
		contentLength >= threshold &&
		IsDiskCacheAvailable(contentLength) {
		storage, err := newDiskStorageFromReader(reader, maxBytes, GetDiskCachePath())
		if err != nil {
			if IsRequestBodyTooLargeError(err) {
				return nil, err
			}
			// 磁盘存储失败，reader 已被消费，无法安全回退
			// 直接返回错误而非尝试回退（因为 reader 数据已丢失）
			return nil, fmt.Errorf("disk storage creation failed: %w", err)
		}
		IncrementDiskCacheHits()
		return storage, nil
	}

	// 使用内存读取
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrRequestBodyTooLarge
	}

	storage, err := CreateBodyStorage(data)
	if err != nil {
		return nil, err
	}
	// 如果最终使用内存存储，记录内存缓存命中
	if !storage.IsDisk() {
		IncrementMemoryCacheHits()
	} else {
		IncrementDiskCacheHits()
	}
	return storage, nil
}

// ReaderOnly wraps an io.Reader to hide io.Closer, preventing http.NewRequest
// from type-asserting io.ReadCloser and closing the underlying BodyStorage.
func ReaderOnly(r io.Reader) io.Reader {
	return struct{ io.Reader }{r}
}

// CleanupOldCacheFiles 清理旧的缓存文件（用于启动时清理残留）
func CleanupOldCacheFiles() {
	// 使用统一的缓存管理
	CleanupOldDiskCacheFiles(5 * time.Minute)
}
