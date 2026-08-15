package engine

import "time"

// WindowBounds returns start (inclusive) and end (exclusive) indices into rows
// for the last windowDays from endDate. Rows must be sorted by BucketDate
// (ascending). The caller slices rows[start:end] for a zero-copy window.
func WindowBounds(rows []DigestRow, endDate time.Time, windowDays int) (start, end int) {
	if len(rows) == 0 {
		return 0, 0
	}
	cutoffDay := endDate.AddDate(0, 0, -(windowDays - 1)).Truncate(24 * time.Hour)
	endDay := endDate.Truncate(24 * time.Hour)

	lo := 0
	hi := len(rows)
	for lo < hi {
		mid := (lo + hi) / 2
		if rows[mid].BucketDate.Before(cutoffDay) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	for i := lo; i < len(rows); i++ {
		if rows[i].BucketDate.After(endDay) {
			return lo, i
		}
	}
	return lo, len(rows)
}

// AggregatePodCounts computes min-of-mins, max-of-maxes, and weighted average
// of per-day pod count values across the term window's digest rows.
func AggregatePodCounts(rows []DigestRow) (pcMin, pcMax, pcAvg int64) {
	if len(rows) == 0 {
		return 0, 0, 0
	}
	hasAny := false
	for _, r := range rows {
		if r.PodCountMin > 0 || r.PodCountMax > 0 || r.PodCountAvg > 0 {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return 0, 0, 0
	}
	first := true
	var sumAvg int64
	var count int
	for _, r := range rows {
		if r.PodCountMax == 0 && r.PodCountMin == 0 && r.PodCountAvg == 0 {
			continue
		}
		if first || r.PodCountMin < pcMin {
			pcMin = r.PodCountMin
		}
		if first || r.PodCountMax > pcMax {
			pcMax = r.PodCountMax
		}
		sumAvg += r.PodCountAvg
		count++
		first = false
	}
	if count > 0 {
		pcAvg = (sumAvg + int64(count)/2) / int64(count)
	}
	return
}

// LatestReplicaCounts returns the desired and available replica counts from
// the most recent DigestRow that has a non-zero desired_replicas value.
func LatestReplicaCounts(rows []DigestRow) (desired, available int64) {
	var latestDate time.Time
	for _, r := range rows {
		if r.DesiredReplicas > 0 && r.BucketDate.After(latestDate) {
			latestDate = r.BucketDate
			desired = r.DesiredReplicas
			available = r.AvailableReplicas
		}
	}
	return desired, available
}

// SumOOMCounts totals OOMCountSum across rows.
func SumOOMCounts(rows []DigestRow) int64 {
	var total int64
	for _, r := range rows {
		total += r.OOMCountSum
	}
	return total
}

// IsStaleRecommendation marks a recommendation stale when the cluster has not
// reported within the threshold. Cluster activity takes precedence over
// per-container digest age.
func IsStaleRecommendation(now, latestDigestDate, clusterLastReported time.Time, threshold time.Duration) bool {
	if !clusterLastReported.IsZero() {
		return now.Sub(clusterLastReported) > threshold
	}
	return now.Sub(latestDigestDate.Truncate(24*time.Hour)) > threshold
}

// LatestDigest returns the DigestRow with the most recent BucketDate.
func LatestDigest(rows []DigestRow) DigestRow {
	if len(rows) == 0 {
		return DigestRow{}
	}
	best := rows[0]
	for _, r := range rows[1:] {
		if r.BucketDate.After(best.BucketDate) {
			best = r
		}
	}
	return best
}
