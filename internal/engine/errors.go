package engine

import "errors"

// Sentinel errors for typed error checking in callers (prefer errors.Is over string matching).

// ErrFieldsLocked is returned by UpdateSnapshotSettings when the update
// attempts to modify fields locked by environment variables.
var ErrFieldsLocked = errors.New("fields locked by environment variable")

// ErrPartitionMissing is returned by DB operations when the target
// partition does not exist (e.g., monthly partition not yet created).
var ErrPartitionMissing = errors.New("no partition")
