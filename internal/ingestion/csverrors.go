package ingestion

import "errors"

// Sentinel errors for CSV parsing validation failures. Used on error paths only
// so the happy path avoids fmt.Errorf allocations. Entity CSV parse lives in
// librobne/csv; these sentinels remain for CoreToMillicores / BytesToKiB tests.
var (
	errInvalidCoreValue  = errors.New("invalid core value")
	errNegativeCoreValue = errors.New("negative core value")
	errInvalidByteValue  = errors.New("invalid byte value")
	errNegativeByteValue = errors.New("negative byte value")
)
