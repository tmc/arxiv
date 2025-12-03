package arxiv

import (
	"container/list"
	"sync"
)

// LRUCache is a thread-safe LRU cache
type LRUCache struct {
	capacity int
	cache    map[string]*list.Element
	list     *list.List
	mu       sync.RWMutex
}

// entry holds a key-value pair
type entry struct {
	key   string
	value interface{}
}

// NewLRUCache creates a new LRU cache with the given capacity.
// LRU = Least Recently Used (eviction algorithm, not CPU-related).
// When cache is full, least recently accessed items are evicted.
func NewLRUCache(capacity int) *LRUCache {
	if capacity <= 0 {
		capacity = 500000 // Default: 500k entries (~500MB-1GB memory)
	}
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

// Get retrieves a value from the cache
func (lru *LRUCache) Get(key string) (interface{}, bool) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, ok := lru.cache[key]; ok {
		lru.list.MoveToFront(elem)
		return elem.Value.(*entry).value, true
	}
	return nil, false
}

// Put stores a value in the cache
func (lru *LRUCache) Put(key string, value interface{}) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, ok := lru.cache[key]; ok {
		elem.Value.(*entry).value = value
		lru.list.MoveToFront(elem)
		return
	}

	if lru.list.Len() >= lru.capacity {
		// Remove least recently used
		back := lru.list.Back()
		if back != nil {
			delete(lru.cache, back.Value.(*entry).key)
			lru.list.Remove(back)
		}
	}

	elem := lru.list.PushFront(&entry{key: key, value: value})
	lru.cache[key] = elem
}

// Delete removes a key from the cache
func (lru *LRUCache) Delete(key string) {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	if elem, ok := lru.cache[key]; ok {
		delete(lru.cache, key)
		lru.list.Remove(elem)
	}
}

// Clear removes all entries from the cache
func (lru *LRUCache) Clear() {
	lru.mu.Lock()
	defer lru.mu.Unlock()

	lru.cache = make(map[string]*list.Element)
	lru.list = list.New()
}

// Size returns the current number of entries
func (lru *LRUCache) Size() int {
	lru.mu.RLock()
	defer lru.mu.RUnlock()
	return len(lru.cache)
}

