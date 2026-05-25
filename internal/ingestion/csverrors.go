package ingestion

import "errors"

// Sentinel errors for CSV parsing validation failures. Used on error paths only
// so the happy path avoids fmt.Errorf allocations.
var (
	errInvalidCoreValue     = errors.New("invalid core value")
	errNegativeCoreValue    = errors.New("negative core value")
	errInvalidByteValue     = errors.New("invalid byte value")
	errNegativeByteValue    = errors.New("negative byte value")
	errInvalidFloatValue    = errors.New("invalid float value")
	errMissingRequiredColumn = errors.New("missing required column")
	errInvalidIntervalStart = errors.New("invalid interval_start")
	errInvalidIntervalEnd   = errors.New("invalid interval_end")
)
