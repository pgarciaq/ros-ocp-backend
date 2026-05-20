package engine

import (
	"errors"
	"strings"
)

// Sentinel errors for typed error checking in callers (prefer errors.Is over string matching).

// ErrFieldsLocked is returned by UpdateSnapshotSettings when the update
// attempts to modify fields locked by environment variables.
var ErrFieldsLocked = errors.New("fields locked by environment variable")

// ErrPartitionMissing is returned by DB operations when the target
// partition does not exist (e.g., monthly partition not yet created).
var ErrPartitionMissing = errors.New("no partition")

// LockedFieldsFromError extracts the locked field names from an
// ErrFieldsLocked-wrapped error. Returns nil if the error doesn't
// contain parseable field names.
func LockedFieldsFromError(err error) []string {
	if err == nil {
		return nil
	}
	msg := err.Error()
	prefix := ErrFieldsLocked.Error() + ": "
	if !strings.HasPrefix(msg, prefix) {
		return nil
	}
	raw := strings.TrimPrefix(msg, prefix)
	raw = strings.Trim(raw, "[]")
	if raw == "" {
		return nil
	}
	return strings.Fields(raw)
}
