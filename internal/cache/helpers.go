package cache

import (
	"strings"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

// RemoveByPrefix removes all entries whose key starts with prefix from an expirable LRU cache.
func RemoveByPrefix[K ~string, V any](cache *expirable.LRU[K, V], prefix K) {
	for _, k := range cache.Keys() {
		if strings.HasPrefix(string(k), string(prefix)) {
			cache.Remove(k)
		}
	}
}
