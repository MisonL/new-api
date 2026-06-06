package service

import (
	"sync"
	"time"
)

type syntheticCompactMemoryStoreState struct {
	mu    sync.Mutex
	items map[string]syntheticCompactMemoryEntry
	order []string
}

var syntheticCompactMemoryStore = &syntheticCompactMemoryStoreState{
	items: make(map[string]syntheticCompactMemoryEntry),
}

func (store *syntheticCompactMemoryStoreState) Load(key any) (any, bool) {
	id, ok := key.(string)
	if !ok || id == "" {
		return nil, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, exists := store.items[id]
	if !exists {
		return nil, false
	}
	return entry, true
}

func (store *syntheticCompactMemoryStoreState) LoadFresh(id string, now time.Time) (syntheticCompactMemoryEntry, bool) {
	if id == "" {
		return syntheticCompactMemoryEntry{}, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, exists := store.items[id]
	if !exists {
		return syntheticCompactMemoryEntry{}, false
	}
	if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
		delete(store.items, id)
		store.compactOrderLocked()
		return syntheticCompactMemoryEntry{}, false
	}
	return entry, true
}

func (store *syntheticCompactMemoryStoreState) Store(key any, value any) {
	id, ok := key.(string)
	if !ok || id == "" {
		return
	}
	entry, ok := value.(syntheticCompactMemoryEntry)
	if !ok {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.items[id]; !exists {
		store.order = append(store.order, id)
	}
	store.items[id] = entry
	store.enforceLimitLocked()
}

func (store *syntheticCompactMemoryStoreState) Delete(key any) {
	id, ok := key.(string)
	if !ok {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.items, id)
	store.compactOrderLocked()
}

func (store *syntheticCompactMemoryStoreState) Range(fn func(key any, value any) bool) {
	store.mu.Lock()
	snapshot := make(map[string]syntheticCompactMemoryEntry, len(store.items))
	for id, entry := range store.items {
		snapshot[id] = entry
	}
	store.mu.Unlock()

	for id, entry := range snapshot {
		if !fn(id, entry) {
			return
		}
	}
}

func (store *syntheticCompactMemoryStoreState) Reset() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.items = make(map[string]syntheticCompactMemoryEntry)
	store.order = nil
}

func (store *syntheticCompactMemoryStoreState) enforceLimitLocked() {
	if len(store.items) <= syntheticCompactMemoryEntriesMax {
		return
	}
	store.compactOrderLocked()
	for len(store.items) > syntheticCompactMemoryEntriesMax && len(store.order) > 0 {
		id := store.order[0]
		copy(store.order, store.order[1:])
		store.order = store.order[:len(store.order)-1]
		delete(store.items, id)
	}
}

func (store *syntheticCompactMemoryStoreState) compactOrderLocked() {
	seen := make(map[string]bool, len(store.items))
	compacted := make([]string, 0, len(store.items))
	for _, id := range store.order {
		if _, exists := store.items[id]; !exists || seen[id] {
			continue
		}
		seen[id] = true
		compacted = append(compacted, id)
	}
	store.order = compacted
}
