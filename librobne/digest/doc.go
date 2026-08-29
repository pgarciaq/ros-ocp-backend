// Package digest computes exact in-memory percentiles (sort + nearest-lower-rank).
// ComputeDigest and ComputeWeightedDigest operate on sample slices, not SQL
// daily rows. Not t-digest. The weighted path takes parallel weights; CSV
// ingest must pass a callback rather than import bhschedule.
package digest
