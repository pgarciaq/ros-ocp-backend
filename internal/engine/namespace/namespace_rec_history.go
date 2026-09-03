package namespace

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultHistoryLimit = 30
	maxHistoryLimit     = 90
)

// HistoryResourceValues are recommended or current request/limit for one resource.
type HistoryResourceValues struct {
	RequestMillicores *int64 `json:"request_millicores,omitempty"`
	LimitMillicores   *int64 `json:"limit_millicores,omitempty"`
	RequestKiB        *int64 `json:"request_kib,omitempty"`
	LimitKiB          *int64 `json:"limit_kib,omitempty"`
}

// HistoryUtilization captures variation percentages stored on each snapshot.
type HistoryUtilization struct {
	RequestVariationPercent *float32 `json:"request_variation_percent,omitempty"`
	LimitVariationPercent   *float32 `json:"limit_variation_percent,omitempty"`
}

// RecommendationHistoryRow is one historical namespace recommendation snapshot
// for a single resource (cpu or memory).
type RecommendationHistoryRow struct {
	Resource           string                `json:"resource"`
	RecommendationType string                `json:"recommendation_type"`
	Term               string                `json:"term"`
	RecordedAt         time.Time             `json:"recorded_at"`
	Recommended        HistoryResourceValues `json:"recommended"`
	Current            HistoryResourceValues `json:"current"`
	Utilization        *HistoryUtilization   `json:"utilization,omitempty"`
	ConfidenceLevel    *float32              `json:"confidence_level,omitempty"`
	NotificationCodes  []int16               `json:"notification_codes"`
}

// ListRecommendationHistory returns historical snapshots for a namespace,
// expanded to one row per resource (cpu, memory).
func ListRecommendationHistory(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, namespace string,
	terms, engines []string,
	limit int,
) ([]RecommendationHistoryRow, error) {
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	if limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}

	dbTerms := normalizeTerms(terms)
	dbEngines := normalizeEngines(engines)

	query := `
		SELECT term, engine, created_at,
			rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib,
			current_cpu_request_millicores, current_cpu_limit_millicores,
			current_memory_request_kib, current_memory_limit_kib,
			variation_cpu_request_pct, variation_cpu_limit_pct,
			variation_memory_request_pct, variation_memory_limit_pct,
			confidence_level, notification_codes
		FROM historical_namespace_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2::uuid AND namespace_name = $3
		  AND term IS NOT NULL`
	args := []any{orgID, clusterUUID, namespace}
	argN := 4
	if len(dbTerms) > 0 {
		query += fmt.Sprintf(" AND term = ANY($%d)", argN)
		args = append(args, dbTerms)
		argN++
	}
	if len(dbEngines) > 0 {
		query += fmt.Sprintf(" AND engine = ANY($%d)", argN)
		args = append(args, dbEngines)
		argN++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argN)
	args = append(args, limit)

	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list namespace rec history: %w", err)
	}
	defer rows.Close()

	var out []RecommendationHistoryRow
	for rows.Next() {
		var (
			term, engine         string
			recordedAt           time.Time
			recCPUReq, recCPULim *int64
			recMemReq, recMemLim *int64
			curCPUReq, curCPULim *int64
			curMemReq, curMemLim *int64
			varCPUReq, varCPULim *float32
			varMemReq, varMemLim *float32
			confidence           *float32
			notificationCodes    []int16
		)
		if err := rows.Scan(
			&term, &engine, &recordedAt,
			&recCPUReq, &recCPULim, &recMemReq, &recMemLim,
			&curCPUReq, &curCPULim, &curMemReq, &curMemLim,
			&varCPUReq, &varCPULim, &varMemReq, &varMemLim,
			&confidence, &notificationCodes,
		); err != nil {
			return nil, fmt.Errorf("scan namespace rec history: %w", err)
		}

		apiTerm := TermToAPI(term)
		out = append(out,
			cpuRow(apiTerm, engine, recordedAt,
				recCPUReq, recCPULim, curCPUReq, curCPULim, varCPUReq, varCPULim,
				confidence, notificationCodes),
			memoryRow(apiTerm, engine, recordedAt,
				recMemReq, recMemLim, curMemReq, curMemLim, varMemReq, varMemLim,
				confidence, notificationCodes),
		)
	}
	return out, rows.Err()
}

func cpuRow(
	term, engine string,
	recordedAt time.Time,
	recReq, recLim, curReq, curLim *int64,
	varReq, varLim *float32,
	confidence *float32,
	notificationCodes []int16,
) RecommendationHistoryRow {
	if notificationCodes == nil {
		notificationCodes = []int16{}
	}
	row := RecommendationHistoryRow{
		Resource:           "cpu",
		RecommendationType: engine,
		Term:               term,
		RecordedAt:         recordedAt,
		Recommended: HistoryResourceValues{
			RequestMillicores: recReq,
			LimitMillicores:   recLim,
		},
		Current: HistoryResourceValues{
			RequestMillicores: curReq,
			LimitMillicores:   curLim,
		},
		ConfidenceLevel:   confidence,
		NotificationCodes: notificationCodes,
	}
	if varReq != nil || varLim != nil {
		row.Utilization = &HistoryUtilization{
			RequestVariationPercent: varReq,
			LimitVariationPercent:   varLim,
		}
	}
	return row
}

func memoryRow(
	term, engine string,
	recordedAt time.Time,
	recReq, recLim, curReq, curLim *int64,
	varReq, varLim *float32,
	confidence *float32,
	notificationCodes []int16,
) RecommendationHistoryRow {
	if notificationCodes == nil {
		notificationCodes = []int16{}
	}
	row := RecommendationHistoryRow{
		Resource:           "memory",
		RecommendationType: engine,
		Term:               term,
		RecordedAt:         recordedAt,
		Recommended: HistoryResourceValues{
			RequestKiB: recReq,
			LimitKiB:   recLim,
		},
		Current: HistoryResourceValues{
			RequestKiB: curReq,
			LimitKiB:   curLim,
		},
		ConfidenceLevel:   confidence,
		NotificationCodes: notificationCodes,
	}
	if varReq != nil || varLim != nil {
		row.Utilization = &HistoryUtilization{
			RequestVariationPercent: varReq,
			LimitVariationPercent:   varLim,
		}
	}
	return row
}

// TermToAPI converts internal DB term names to API-facing names.
func TermToAPI(term string) string {
	switch strings.TrimSpace(term) {
	case "short":
		return "short_term"
	case "medium":
		return "medium_term"
	case "long":
		return "long_term"
	default:
		return term
	}
}

// TermFromAPI converts API term names to internal DB names.
func TermFromAPI(term string) string {
	t := strings.TrimSpace(term)
	if strings.HasSuffix(t, "_term") {
		return strings.TrimSuffix(t, "_term")
	}
	return t
}

func normalizeTerms(terms []string) []string {
	if len(terms) == 0 {
		return nil
	}
	out := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, t := range terms {
		db := TermFromAPI(t)
		if db == "" {
			continue
		}
		if _, ok := seen[db]; ok {
			continue
		}
		seen[db] = struct{}{}
		out = append(out, db)
	}
	return out
}

func normalizeEngines(engines []string) []string {
	if len(engines) == 0 {
		return nil
	}
	out := make([]string, 0, len(engines))
	seen := make(map[string]struct{}, len(engines))
	for _, e := range engines {
		e = strings.TrimSpace(strings.ToLower(e))
		if e != "cost" && e != "performance" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// ParseHistoryLimit parses the limit query parameter for namespace history.
func ParseHistoryLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultHistoryLimit, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("invalid limit")
	}
	return limit, nil
}
