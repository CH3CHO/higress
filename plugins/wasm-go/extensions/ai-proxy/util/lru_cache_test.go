package util

import (
	"testing"
)

func TestLRUCacheSet(t *testing.T) {
	cache := NewLRUCache("test-cache", 3)

	tests := []struct {
		name     string
		key      string
		value    interface{}
		expected interface{}
	}{
		{"set string value", "key1", "value1", "value1"},
		{"set int value", "key2", 42, 42},
		{"set nil value", "key3", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache.Set(tt.key, tt.value)
			val, ok := cache.Get(tt.key)
			if !ok {
				t.Errorf("expected key %q to exist in cache", tt.key)
			}
			if val != tt.expected {
				t.Errorf("expected value %v, got %v", tt.expected, val)
			}
		})
	}
}

func TestLRUCacheGet(t *testing.T) {
	cache := NewLRUCache("test-cache", 3)
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")

	tests := []struct {
		name      string
		key       string
		expectVal interface{}
		expectOk  bool
	}{
		{"get existing key", "key1", "value1", true},
		{"get non-existing key", "key3", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := cache.Get(tt.key)
			if ok != tt.expectOk {
				t.Errorf("expected ok=%v, got %v", tt.expectOk, ok)
			}
			if ok && val != tt.expectVal {
				t.Errorf("expected value %v, got %v", tt.expectVal, val)
			}
		})
	}
}

func TestLRUCacheEviction(t *testing.T) {
	cache := NewLRUCache("test-cache", 3)

	// Add 3 entries
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Set("key3", "value3")

	if cache.Size() != 3 {
		t.Errorf("expected cache size 3, got %d", cache.Size())
	}

	// Add a 4th entry, should evict the oldest (key1)
	cache.Set("key4", "value4")

	if cache.Size() != 3 {
		t.Errorf("expected cache size 3 after eviction, got %d", cache.Size())
	}

	// Verify key1 was evicted
	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected key1 to be evicted")
	}

	// Verify other keys still exist
	_, ok = cache.Get("key2")
	if !ok {
		t.Error("expected key2 to still exist")
	}

	_, ok = cache.Get("key3")
	if !ok {
		t.Error("expected key3 to still exist")
	}

	_, ok = cache.Get("key4")
	if !ok {
		t.Error("expected key4 to exist")
	}
}

func TestLRUCacheSize(t *testing.T) {
	cache := NewLRUCache("test-cache", 2)

	if cache.Size() != 0 {
		t.Errorf("expected initial size 0, got %d", cache.Size())
	}

	cache.Set("key1", "value1")
	if cache.Size() != 1 {
		t.Errorf("expected size 1, got %d", cache.Size())
	}

	cache.Set("key2", "value2")
	if cache.Size() != 2 {
		t.Errorf("expected size 2, got %d", cache.Size())
	}

	// Adding a 3rd entry should evict the oldest
	cache.Set("key3", "value3")
	if cache.Size() != 2 {
		t.Errorf("expected size 2 after eviction, got %d", cache.Size())
	}
}

func TestLRUCacheMaxSize(t *testing.T) {
	cache := NewLRUCache("test-cache", 5)

	if cache.MaxSize() != 5 {
		t.Errorf("expected maxSize 5, got %d", cache.MaxSize())
	}

	cache.SetMaxSize(10)
	if cache.MaxSize() != 10 {
		t.Errorf("expected maxSize 10 after update, got %d", cache.MaxSize())
	}
}

func TestLRUCacheClear(t *testing.T) {
	cache := NewLRUCache("test-cache", 3)
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")

	if cache.Size() != 2 {
		t.Errorf("expected size 2 before clear, got %d", cache.Size())
	}

	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", cache.Size())
	}

	_, ok := cache.Get("key1")
	if ok {
		t.Error("expected key1 to be cleared")
	}
}

func TestLRUCacheDefaultMaxSize(t *testing.T) {
	// Test with non-positive maxSize, should default to 1024
	cache := NewLRUCache("test-cache", 0)
	if cache.MaxSize() != 1024 {
		t.Errorf("expected default maxSize 1024, got %d", cache.MaxSize())
	}

	cache = NewLRUCache("test-cache", -1)
	if cache.MaxSize() != 1024 {
		t.Errorf("expected default maxSize 1024, got %d", cache.MaxSize())
	}
}
