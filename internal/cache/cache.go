package cache

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type Item struct {
	Value     []byte
	ExpiresAt time.Time
}

type entry struct {
	key       string
	value     []byte
	expiresAt time.Time
	elem      *list.Element
}

type Cache struct {
	mu       sync.Mutex
	items    map[string]*entry
	lru      *list.List
	capacity int
}

func New(capacity int) *Cache {
	if capacity <= 0 {
		capacity = 1
	}
	return &Cache{
		items:    make(map[string]*entry, capacity),
		lru:      list.New(),
		capacity: capacity,
	}
}

func (c *Cache) Get(_ context.Context, key string) (Item, error) {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		return Item{}, ErrNotFound
	}
	if !e.expiresAt.IsZero() && now.After(e.expiresAt) {
		c.removeEntryLocked(e)
		return Item{}, ErrNotFound
	}
	c.lru.MoveToFront(e.elem)

	v := make([]byte, len(e.value))
	copy(v, e.value)
	return Item{Value: v, ExpiresAt: e.expiresAt}, nil
}

func (c *Cache) Set(_ context.Context, key string, value []byte, ttl time.Duration) {
	now := time.Now()
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.items[key]; ok {
		e.value = cloneBytes(value)
		e.expiresAt = expiresAt
		c.lru.MoveToFront(e.elem)
		return
	}

	e := &entry{key: key, value: cloneBytes(value), expiresAt: expiresAt}
	e.elem = c.lru.PushFront(e)
	c.items[key] = e

	for len(c.items) > c.capacity {
		back := c.lru.Back()
		if back == nil {
			break
		}
		be := back.Value.(*entry)
		c.removeEntryLocked(be)
	}
}

func (c *Cache) Delete(_ context.Context, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.items[key]; ok {
		c.removeEntryLocked(e)
	}
}

func (c *Cache) CleanupExpired(now time.Time, maxScan int) int {
	if maxScan <= 0 {
		maxScan = int(^uint(0) >> 1)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	scanned := 0
	for e := c.lru.Back(); e != nil && scanned < maxScan; {
		prev := e.Prev()
		en := e.Value.(*entry)
		if !en.expiresAt.IsZero() && now.After(en.expiresAt) {
			c.removeEntryLocked(en)
			removed++
		}
		scanned++
		e = prev
	}
	return removed
}

func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *Cache) removeEntryLocked(e *entry) {
	delete(c.items, e.key)
	c.lru.Remove(e.elem)
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
