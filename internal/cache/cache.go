package cache

import (
	"container/list"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type Entry struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	ExpiresAt  time.Time
}

func (e *Entry) IsExpired() bool {
	return time.Now().After(e.ExpiresAt)
}

type Stats struct {
	Hits      int64
	Misses    int64
	Entries   int
	SizeBytes int64
	MaxBytes  int64
}

type Cache interface {
	Get(key string) (*Entry, bool)
	// GetStale returns an entry regardless of expiry — used for stale-if-error.
	GetStale(key string) (*Entry, bool)
	Set(key string, entry *Entry)
	Delete(key string)
	DeleteByPrefix(prefix string)
	Stats() Stats
}

type lruItem struct {
	key   string
	entry *Entry
	size  int64
}

type MemoryCache struct {
	mu      sync.RWMutex
	items   map[string]*list.Element
	order   *list.List
	size    int64
	maxSize int64
	hits    atomic.Int64
	misses  atomic.Int64
}

func New(maxSizeMB int) *MemoryCache {
	return &MemoryCache{
		items:   make(map[string]*list.Element),
		order:   list.New(),
		maxSize: int64(maxSizeMB) * 1024 * 1024,
	}
}

func (c *MemoryCache) GetStale(key string) (*Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruItem).entry, true
}

func (c *MemoryCache) Get(key string) (*Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		c.misses.Add(1)
		return nil, false
	}

	item := el.Value.(*lruItem)
	if item.entry.IsExpired() {
		c.misses.Add(1)
		return nil, false
	}

	// Move to front (most recently used)
	c.order.MoveToFront(el)
	c.hits.Add(1)
	return item.entry, true
}

func (c *MemoryCache) Set(key string, entry *Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entrySize := entryBytes(entry)

	// Update existing
	if el, ok := c.items[key]; ok {
		old := el.Value.(*lruItem)
		c.size -= old.size
		old.entry = entry
		old.size = entrySize
		c.size += entrySize
		c.order.MoveToFront(el)
		return
	}

	// Evict until there's space
	for c.size+entrySize > c.maxSize && c.order.Len() > 0 {
		c.removeElement(c.order.Back())
	}

	item := &lruItem{key: key, entry: entry, size: entrySize}
	el := c.order.PushFront(item)
	c.items[key] = el
	c.size += entrySize
}

func (c *MemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.removeElement(el)
	}
}

func (c *MemoryCache) DeleteByPrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, el := range c.items {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			c.removeElement(el)
		}
	}
}

func (c *MemoryCache) Stats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Stats{
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		Entries:   len(c.items),
		SizeBytes: c.size,
		MaxBytes:  c.maxSize,
	}
}

func (c *MemoryCache) removeElement(el *list.Element) {
	item := el.Value.(*lruItem)
	c.order.Remove(el)
	delete(c.items, item.key)
	c.size -= item.size
}

func entryBytes(e *Entry) int64 {
	n := int64(len(e.Body))
	for k, vals := range e.Header {
		n += int64(len(k))
		for _, v := range vals {
			n += int64(len(v))
		}
	}
	return n + 64 // overhead estimate
}
