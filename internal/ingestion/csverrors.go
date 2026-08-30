package ingestion

import "errors"

// Sentinel errors for CSV parsing validation failures. Used on error paths only
// so the happy path avoids fmt.Errorf allocations. Container, namespace, and
// PVC storage parse live in librobne/csv; these sentinels remain for
// CoreToMillicores / BytesToKiB used by snapshot, cluster-quota, and VM sidecar parsers.
var (
	errInvalidCoreValue  = errors.New("invalid core value")
	errNegativeCoreValue = errors.New("negative core value")
	errInvalidByteValue  = errors.New("invalid byte value")
	errNegativeByteValue = errors.New("negative byte value")
)
