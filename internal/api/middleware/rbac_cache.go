package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type rbacCacheEntry struct {
	permissions map[string][]string
	expiresAt   time.Time
}

var (
	rbacPermCache sync.Map
)

func rbacIdentityCacheKey(encodedIdentity string) string {
	if len(encodedIdentity) <= 32 {
		return encodedIdentity
	}
	sum := sha256.Sum256([]byte(encodedIdentity))
	return hex.EncodeToString(sum[:16])
}

func getCachedRBACPermissions(cacheKey string) (map[string][]string, bool) {
	if v, ok := rbacPermCache.Load(cacheKey); ok {
		entry := v.(rbacCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.permissions, true
		}
		rbacPermCache.Delete(cacheKey)
	}
	return nil, false
}

func storeCachedRBACPermissions(cacheKey string, permissions map[string][]string, ttl time.Duration) {
	if ttl <= 0 || permissions == nil {
		return
	}
	// Store a shallow copy so callers cannot mutate cached maps.
	copied := make(map[string][]string, len(permissions))
	for k, v := range permissions {
		copied[k] = append([]string(nil), v...)
	}
	rbacPermCache.Store(cacheKey, rbacCacheEntry{
		permissions: copied,
		expiresAt:   time.Now().Add(ttl),
	})
}

// ClearRBACPermissionCacheForTest removes all cached RBAC entries.
func ClearRBACPermissionCacheForTest() {
	rbacPermCache.Range(func(k, _ any) bool {
		rbacPermCache.Delete(k)
		return true
	})
}
