package costdata

import (
	"container/list"
	"sync"
	"time"
)

type costCacheEntry struct {
	key       string
	data      *ClusterCostData
	expiresAt time.Time
}

// boundedCostCache is a mutex-protected LRU with TTL checked on access.
type boundedCostCache struct {
	mu      sync.Mutex
	maxSize int
	items   map[string]*list.Element
	order   *list.List
}

func newBoundedCostCache(maxSize int) *boundedCostCache {
	if maxSize <= 0 {
		maxSize = defaultCostCacheMaxEntries
	}
	return &boundedCostCache{
		maxSize: maxSize,
		items:   make(map[string]*list.Element),
		order:   list.New(),
	}
}

func (c *boundedCostCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (c *boundedCostCache) get(key string) (*ClusterCostData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil, false
	}
	entry := elem.Value.(*costCacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.removeElement(elem)
		return nil, false
	}
	c.order.MoveToFront(elem)
	return entry.data, true
}

func (c *boundedCostCache) put(key string, data *ClusterCostData, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*costCacheEntry)
		entry.data = data
		entry.expiresAt = time.Now().Add(ttl)
		c.order.MoveToFront(elem)
		costCacheSize.Set(float64(len(c.items)))
		return
	}

	entry := &costCacheEntry{
		key:       key,
		data:      data,
		expiresAt: time.Now().Add(ttl),
	}
	elem := c.order.PushFront(entry)
	c.items[key] = elem

	for len(c.items) > c.maxSize {
		c.evictOldest()
	}
	costCacheSize.Set(float64(len(c.items)))
}

func (c *boundedCostCache) delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.removeElement(elem)
		costCacheSize.Set(float64(len(c.items)))
	}
}

func (c *boundedCostCache) deletePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, elem := range c.items {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			c.removeElement(elem)
		}
	}
	costCacheSize.Set(float64(len(c.items)))
}

func (c *boundedCostCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.order.Init()
	costCacheSize.Set(0)
}

func (c *boundedCostCache) removeElement(elem *list.Element) {
	entry := elem.Value.(*costCacheEntry)
	delete(c.items, entry.key)
	c.order.Remove(elem)
}

func (c *boundedCostCache) evictOldest() {
	elem := c.order.Back()
	if elem == nil {
		return
	}
	c.removeElement(elem)
	costCacheEvictions.Inc()
}
