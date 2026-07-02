package repo

import (
	"container/list"
	"sync"
	"time"
)

// LRUCache is a thread-safe, size-bounded, TTL-aware byte cache.
type LRUCache struct {
	mu       sync.Mutex
	maxBytes int64
	ttl      time.Duration
	curBytes int64
	items    map[string]*list.Element
	order    *list.List
}

type cacheEntry struct {
	key       string
	value     []byte
	size      int64
	expiresAt time.Time
}

// NewLRUCache creates a byte cache with max size in bytes and TTL per entry.
func NewLRUCache(maxBytes int64, ttl time.Duration) *LRUCache {
	return &LRUCache{
		maxBytes: maxBytes,
		ttl:      ttl,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Put inserts or updates a cache entry.
func (c *LRUCache) Put(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	size := int64(len(value))

	if size > c.maxBytes {
		return
	}

	if el, ok := c.items[key]; ok {
		c.removeElement(el)
	}

	for c.curBytes+size > c.maxBytes && c.order.Len() > 0 {
		c.removeElement(c.order.Back())
	}

	entry := &cacheEntry{
		key:       key,
		value:     value,
		size:      size,
		expiresAt: time.Now().Add(c.ttl),
	}
	el := c.order.PushFront(entry)
	c.items[key] = el
	c.curBytes += size
}

// Get retrieves a cached value. Returns (nil, false) on miss or expiry.
func (c *LRUCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return nil, false
	}

	entry := el.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.removeElement(el)
		return nil, false
	}

	c.order.MoveToFront(el)
	return entry.value, true
}

func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.removeElement(el)
	}
}

func (c *LRUCache) removeElement(el *list.Element) {
	entry := el.Value.(*cacheEntry)
	c.order.Remove(el)
	delete(c.items, entry.key)
	c.curBytes -= entry.size
}

// MetaCache is a thread-safe, size-bounded, TTL-aware cache for FileMeta.
type MetaCache struct {
	mu       sync.Mutex
	maxItems int
	ttl      time.Duration
	items    map[string]*list.Element
	order    *list.List
}

type metaEntry struct {
	key       string
	meta      *FileMeta
	expiresAt time.Time
}

// NewMetaCache creates a FileMeta cache with max items and TTL.
func NewMetaCache(maxItems int, ttl time.Duration) *MetaCache {
	return &MetaCache{
		maxItems: maxItems,
		ttl:      ttl,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Put inserts or updates a meta cache entry.
func (c *MetaCache) Put(key string, meta *FileMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.items[key]; ok {
		c.order.Remove(el)
		delete(c.items, key)
	}

	for len(c.items) >= c.maxItems && c.order.Len() > 0 {
		back := c.order.Back()
		entry := back.Value.(*metaEntry)
		c.order.Remove(back)
		delete(c.items, entry.key)
	}

	entry := &metaEntry{
		key:       key,
		meta:      meta,
		expiresAt: time.Now().Add(c.ttl),
	}
	el := c.order.PushFront(entry)
	c.items[key] = el
}

// Get retrieves a cached FileMeta. Returns (nil, false) on miss or expiry.
func (c *MetaCache) Get(key string) (*FileMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return nil, false
	}

	entry := el.Value.(*metaEntry)
	if time.Now().After(entry.expiresAt) {
		c.order.Remove(el)
		delete(c.items, entry.key)
		return nil, false
	}

	c.order.MoveToFront(el)
	return entry.meta, true
}
