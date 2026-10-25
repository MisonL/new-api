package types

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

type RWMap[K comparable, V any] struct {
	data  map[K]V
	mutex sync.RWMutex
}

func (m *RWMap[K, V]) UnmarshalJSON(b []byte) error {
	loaded, err := decodeRWMapPayload[K, V](b)
	if err != nil {
		return err
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data = loaded
	return nil
}

// decodeRWMapPayload parses a JSON object into a fresh map, leaving the caller's
// existing data untouched when the payload is invalid. A nil result (JSON
// "null") is rejected rather than swapped in: a nil map turns every later Set
// into a panic.
func decodeRWMapPayload[K comparable, V any](payload []byte) (map[K]V, error) {
	loaded := make(map[K]V)
	if err := common.Unmarshal(payload, &loaded); err != nil {
		return nil, err
	}
	if loaded == nil {
		return nil, fmt.Errorf("payload must be a JSON object")
	}
	return loaded, nil
}

func (m *RWMap[K, V]) MarshalJSON() ([]byte, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return common.Marshal(m.data)
}

func NewRWMap[K comparable, V any]() *RWMap[K, V] {
	return &RWMap[K, V]{
		data: make(map[K]V),
	}
}

func (m *RWMap[K, V]) Get(key K) (V, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	value, exists := m.data[key]
	return value, exists
}

func (m *RWMap[K, V]) Set(key K, value V) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data[key] = value
}

func (m *RWMap[K, V]) AddAll(other map[K]V) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	for k, v := range other {
		m.data[k] = v
	}
}

func (m *RWMap[K, V]) Clear() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data = make(map[K]V)
}

// ReadAll returns a copy of the entire map.
func (m *RWMap[K, V]) ReadAll() map[K]V {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	copiedMap := make(map[K]V)
	for k, v := range m.data {
		copiedMap[k] = v
	}
	return copiedMap
}

func (m *RWMap[K, V]) Len() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.data)
}

// LoadFromJsonString replaces the map contents with the parsed payload.
//
// The payload is fully decoded into a fresh map before the existing data is
// touched, so an invalid document leaves the previous contents in place. The
// previous clear-then-decode order meant a truncated admin submission wiped the
// live ratio tables while returning an error to the caller, and the bad value
// had already been persisted, so the 60s option sync reloaded the empty table
// until someone wrote a valid value.
func LoadFromJsonString[K comparable, V any](m *RWMap[K, V], jsonStr string) error {
	loaded, err := decodeRWMapPayload[K, V]([]byte(jsonStr))
	if err != nil {
		return err
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data = loaded
	return nil
}

// LoadFromJsonStringWithCallback loads a JSON string into the RWMap and calls
// the callback on success. The callback runs only after the new contents are
// installed, and never on a parse failure.
func LoadFromJsonStringWithCallback[K comparable, V any](m *RWMap[K, V], jsonStr string, onSuccess func()) error {
	loaded, err := decodeRWMapPayload[K, V]([]byte(jsonStr))
	if err != nil {
		return err
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.data = loaded
	if onSuccess != nil {
		onSuccess()
	}
	return nil
}

// MarshalJSONString returns the JSON string representation of the RWMap.
func (m *RWMap[K, V]) MarshalJSONString() string {
	bytes, err := m.MarshalJSON()
	if err != nil {
		return "{}"
	}
	return string(bytes)
}
