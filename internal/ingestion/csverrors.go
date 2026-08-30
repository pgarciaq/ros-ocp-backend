package ingestion

import "errors"

// Sentinel errors for CSV parsing validation failures. Used on error paths only
// so the happy path avoids fmt.Errorf allocations. Container, namespace, PVC,
// VM, sidecar, and snapshot parse live in librobne/csv; these sentinels remain
// for CoreToMillicores / BytesToKiB used by the cluster-quota parser.
var (
	errInvalidCoreValue  = errors.New("invalid core value")
	errNegativeCoreValue = errors.New("negative core value")
	errInvalidByteValue  = errors.New("invalid byte value")
	errNegativeByteValue = errors.New("negative byte value")
)
