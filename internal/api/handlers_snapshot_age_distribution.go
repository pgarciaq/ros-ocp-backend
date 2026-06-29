package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/redhatinsights/ros-ocp-backend/internal/db"
)

// SnapshotAgeBucket represents a single age bucket in the histogram response.
type SnapshotAgeBucket struct {
	Label   string `json:"label"`
	MinDays int    `json:"min_days"`
	MaxDays *int   `json:"max_days"` // nil for the last (unbounded) bucket
	Count   int    `json:"count"`
}

// SnapshotAgeDistributionResponse is the top-level response for the age distribution endpoint.
type SnapshotAgeDistributionResponse struct {
	Buckets []SnapshotAgeBucket `json:"buckets"`
	Total   int                 `json:"total"`
}

// GetSnapshotAgeDistribution handles GET /recommendations/openshift/snapshots/age-distribution.
// Returns a histogram of snapshot counts grouped by age buckets.
func GetSnapshotAgeDistribution(c echo.Context) error {
	xrhid, err := requireXRHID(c)
	if err != nil {
		return err
	}
	orgID := xrhid.Identity.OrgID
	hlog := requestLogger(c, orgID)

	pool := db.GetPool()
	if pool == nil {
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "database connection unavailable",
		})
	}

	boundaries, parseErr := parseSnapshotAgeBoundaries(c.QueryParam("bucket_boundaries"))
	if parseErr != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{
			"status":  "error",
			"message": parseErr.Error(),
		})
	}

	ctx := c.Request().Context()

	query, args := buildSnapshotAgeDistributionQuery(orgID, boundaries)
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		hlog.Errorf("snapshot age distribution query failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot age distribution",
		})
	}
	defer rows.Close()

	counts := make(map[int]int)
	for rows.Next() {
		var bucketIdx, count int
		if scanErr := rows.Scan(&bucketIdx, &count); scanErr != nil {
			hlog.Errorf("scanning snapshot age distribution row: %v", scanErr)
			return c.JSON(http.StatusServiceUnavailable, echo.Map{
				"status":  "error",
				"message": "unable to read snapshot age distribution",
			})
		}
		counts[bucketIdx] = count
	}
	if err := rows.Err(); err != nil {
		hlog.Errorf("snapshot age distribution row iteration failed: %v", err)
		return c.JSON(http.StatusServiceUnavailable, echo.Map{
			"status":  "error",
			"message": "unable to fetch snapshot age distribution",
		})
	}

	numBuckets := len(boundaries) + 1
	buckets := make([]SnapshotAgeBucket, numBuckets)
	total := 0

	for i := range numBuckets {
		buckets[i] = buildAgeBucket(i, boundaries)
		buckets[i].Count = counts[i]
		total += buckets[i].Count
	}

	setRecommendationNoStore(c)
	return c.JSON(http.StatusOK, SnapshotAgeDistributionResponse{
		Buckets: buckets,
		Total:   total,
	})
}

// parseSnapshotAgeBoundaries parses the bucket_boundaries query parameter.
// Expected format: comma-separated positive integers in ascending order.
// Returns default boundaries [7, 30, 90] if empty.
func parseSnapshotAgeBoundaries(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []int{7, 30, 90}, nil
	}

	parts := strings.Split(raw, ",")
	boundaries := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("bucket_boundaries must be comma-separated positive integers")
		}
		if v <= 0 {
			return nil, fmt.Errorf("bucket_boundaries must be positive integers")
		}
		boundaries = append(boundaries, v)
	}

	for i := 1; i < len(boundaries); i++ {
		if boundaries[i] <= boundaries[i-1] {
			return nil, fmt.Errorf("bucket_boundaries must be in strictly ascending order")
		}
	}

	return boundaries, nil
}

// buildSnapshotAgeDistributionQuery builds the SQL query for bucketing snapshot ages.
func buildSnapshotAgeDistributionQuery(orgID string, boundaries []int) (string, []interface{}) {
	args := []interface{}{orgID}

	caseParts := make([]string, 0, len(boundaries)+1)
	for i, b := range boundaries {
		args = append(args, b)
		caseParts = append(caseParts, fmt.Sprintf("WHEN age_days < $%d THEN %d", i+2, i))
	}
	caseParts = append(caseParts, fmt.Sprintf("ELSE %d", len(boundaries)))

	query := `
		SELECT bucket_idx, COUNT(*)::int AS count
		FROM (
			SELECT CASE ` + strings.Join(caseParts, " ") + ` END AS bucket_idx
			FROM snapshot_recommendation_sets
			WHERE org_id = $1
		) bucketed
		GROUP BY bucket_idx
		ORDER BY bucket_idx`

	return query, args
}

// buildAgeBucket constructs label and range metadata for a single bucket.
func buildAgeBucket(idx int, boundaries []int) SnapshotAgeBucket {
	numBuckets := len(boundaries) + 1

	if idx == 0 {
		maxDays := boundaries[0] - 1
		return SnapshotAgeBucket{
			Label:   fmt.Sprintf("<%d days", boundaries[0]),
			MinDays: 0,
			MaxDays: &maxDays,
		}
	}

	if idx == numBuckets-1 {
		return SnapshotAgeBucket{
			Label:   fmt.Sprintf("%d+ days", boundaries[idx-1]),
			MinDays: boundaries[idx-1],
			MaxDays: nil,
		}
	}

	maxDays := boundaries[idx] - 1
	return SnapshotAgeBucket{
		Label:   fmt.Sprintf("%d-%d days", boundaries[idx-1], boundaries[idx]),
		MinDays: boundaries[idx-1],
		MaxDays: &maxDays,
	}
}
