package util

import (
	"sync"
)

// LRUCache provides a fixed-size Least Recently Used (LRU) cache that automatically
// evicts the oldest entries when the cache exceeds its maximum size.
// It is safe for concurrent use by multiple goroutines.
type LRUCache struct {
	name     string
	mappings map[string]interface{} // key -> value
	order    []string               // tracks insertion order for LRU eviction
	mu       sync.RWMutex
	maxSize  int
}

// NewLRUCache creates a new LRU cache with the specified maximum size.
// If maxSize is <= 0, defaults to 1024.
func NewLRUCache(name string, maxSize int) *LRUCache {
	if maxSize <= 0 {
		maxSize = 1024 // default size
	}
	return &LRUCache{
		name:     name,
		mappings: make(map[string]interface{}),
		order:    make([]string, 0),
		maxSize:  maxSize,
	}
}

// Set stores a key-value pair in the cache, with automatic LRU eviction
// when the cache size exceeds maxSize.
func (c *LRUCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.mappings[key] = value
	c.order = append(c.order, key)

	// Evict the oldest entries until cache size does not exceed maxSize
	for len(c.mappings) > c.maxSize && len(c.order) > 0 {
		oldestKey := c.order[0]
		delete(c.mappings, oldestKey)
		c.order = c.order[1:]
	}
}

// Get retrieves a value from the cache by key.
// Returns the value and true if the key exists, otherwise returns nil and false.
func (c *LRUCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.mappings[key]
	return value, ok
}

// Exists checks if a key exists in the cache and returns the value.
// Returns the value and true if the key exists, otherwise returns nil and false.
func (c *LRUCache) Exists(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	value, ok := c.mappings[key]
	return value, ok
}

// Size returns the current number of entries in the cache.
func (c *LRUCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.mappings)
}

// MaxSize returns the maximum size of the cache.
func (c *LRUCache) MaxSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.maxSize
}

// SetMaxSize sets the maximum size of the cache.
func (c *LRUCache) SetMaxSize(maxSize int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxSize = maxSize
}

// Clear removes all entries from the cache.
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mappings = make(map[string]interface{})
	c.order = make([]string, 0)
}
