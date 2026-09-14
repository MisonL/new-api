package common

import (
	"io"

	rootcommon "github.com/QuantumNous/new-api/common"
)

// NewOutboundJSONBody stores a marshaled upstream body and returns independent
// readers for transport-level replay. The caller owns closer and must close it
// after the upstream request finishes.
func NewOutboundJSONBody(data []byte) (body io.Reader, size int64, getBody func() (io.ReadCloser, error), closer io.Closer, err error) {
	storage, err := rootcommon.CreateBodyStorage(data)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	return rootcommon.ReaderOnly(storage), storage.Size(), func() (io.ReadCloser, error) {
		return rootcommon.NewBodyStorageReader(storage)
	}, storage, nil
}
