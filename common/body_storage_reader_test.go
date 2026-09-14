package common

import (
	"bytes"
	"io"
	"testing"
)

type fallbackBodyStorage struct {
	*bytes.Reader
	data   []byte
	closed bool
}

func (s *fallbackBodyStorage) Close() error {
	s.closed = true
	return nil
}

func (s *fallbackBodyStorage) Bytes() ([]byte, error) {
	return s.data, nil
}

func (s *fallbackBodyStorage) Size() int64 {
	return int64(len(s.data))
}

func (s *fallbackBodyStorage) IsDisk() bool {
	return false
}

func TestNewBodyStorageReaderFallbackDoesNotCloseSharedStorage(t *testing.T) {
	data := []byte("replay body")
	storage := &fallbackBodyStorage{Reader: bytes.NewReader(data), data: data}

	reader, err := NewBodyStorageReader(storage)
	if err != nil {
		t.Fatalf("NewBodyStorageReader() error = %v", err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("replay reader read error = %v", err)
	}
	if string(body) != string(data) {
		t.Fatalf("replay reader content = %q, want %q", body, data)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("replay reader Close() error = %v", err)
	}
	if storage.closed {
		t.Fatal("replay reader closed the shared body storage")
	}

	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("shared storage Seek() error = %v", err)
	}
	body, err = io.ReadAll(storage)
	if err != nil {
		t.Fatalf("shared storage read error = %v", err)
	}
	if string(body) != "replay body" {
		t.Fatalf("shared storage content = %q, want %q", body, "replay body")
	}
}
