// Package engine runs container recommendations in-process with no database pool.
// RecommendWorkloads takes KeyedDigest rows ordered by container key then
// BucketDate and calls emit every BatchSize containers (default 500). The batch
// backing array is reused — copy if retaining. ApplySavingsEstimates is a
// separate call after emit (container.ApplySavingsEstimates).
package engine
