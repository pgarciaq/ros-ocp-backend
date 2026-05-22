package reship

import (
	"sync"
	"time"
)

const defaultLockTTL = time.Hour

type lockKey struct {
	orgID       string
	clusterUUID string
}

type clusterLockState struct {
	mu     sync.Mutex
	held   bool
	heldAt time.Time
}

// LockCoordinator enforces single-flight reship per (org_id, cluster_uuid) with trailing reship.
type LockCoordinator struct {
	mu   sync.Mutex
	keys map[lockKey]*clusterLockState
	ttl  time.Duration
}

// NewLockCoordinator creates an in-memory lock table.
func NewLockCoordinator(ttl time.Duration) *LockCoordinator {
	if ttl <= 0 {
		ttl = defaultLockTTL
	}
	return &LockCoordinator{
		keys: make(map[lockKey]*clusterLockState),
		ttl:  ttl,
	}
}

// Acquire attempts to take the per-cluster lock. When false, the caller must not run reship
// (another reship is in flight for this cluster).
func (lc *LockCoordinator) Acquire(orgID, clusterUUID string) (release func(), acquired bool) {
	key := lockKey{orgID: orgID, clusterUUID: clusterUUID}
	now := time.Now().UTC()

	lc.mu.Lock()
	state, ok := lc.keys[key]
	if !ok {
		state = &clusterLockState{}
		lc.keys[key] = state
	}
	lc.mu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()

	if state.held && now.Sub(state.heldAt) < lc.ttl {
		return func() {}, false
	}

	state.held = true
	state.heldAt = now

	return func() {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.held = false
	}, true
}

// ForceExpire simulates lock TTL expiry for tests.
func (lc *LockCoordinator) ForceExpire(orgID, clusterUUID string, heldSince time.Time) {
	key := lockKey{orgID: orgID, clusterUUID: clusterUUID}
	lc.mu.Lock()
	state, ok := lc.keys[key]
	if !ok {
		state = &clusterLockState{held: true}
		lc.keys[key] = state
	} else {
		state.held = true
	}
	lc.mu.Unlock()

	state.mu.Lock()
	defer state.mu.Unlock()
	state.heldAt = heldSince
}
