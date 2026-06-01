package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultNamespaceHistoryLimit = 30
	maxNamespaceHistoryLimit     = 90
)

// NamespaceHistoryResourceValues are recommended or current request/limit for one resource.
type NamespaceHistoryResourceValues struct {
	RequestMillicores *int64 `json:"request_millicores,omitempty"`
	LimitMillicores   *int64 `json:"limit_millicores,omitempty"`
	RequestKiB        *int64 `json:"request_kib,omitempty"`
	LimitKiB          *int64 `json:"limit_kib,omitempty"`
}

// NamespaceHistoryUtilization captures variation percentages stored on each snapshot.
type NamespaceHistoryUtilization struct {
	RequestVariationPercent *float32 `json:"request_variation_percent,omitempty"`
	LimitVariationPercent   *float32 `json:"limit_variation_percent,omitempty"`
}

// NamespaceRecommendationHistoryRow is one historical namespace recommendation snapshot
// for a single resource (cpu or memory).
type NamespaceRecommendationHistoryRow struct {
	Resource             string                          `json:"resource"`
	RecommendationType   string                          `json:"recommendation_type"`
	Term                 string                          `json:"term"`
	ScheduleType         string                          `json:"schedule_type,omitempty"`
	RecordedAt           time.Time                       `json:"recorded_at"`
	Recommended          NamespaceHistoryResourceValues  `json:"recommended"`
	Current              NamespaceHistoryResourceValues  `json:"current"`
	Utilization          *NamespaceHistoryUtilization    `json:"utilization,omitempty"`
	ConfidenceLevel      *float32                        `json:"confidence_level,omitempty"`
	NotificationCodes    []int16                         `json:"notification_codes,omitempty"`
}

// ListNamespaceRecommendationHistory returns historical snapshots for a namespace,
// expanded to one row per resource (cpu, memory).
func ListNamespaceRecommendationHistory(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID, namespace string,
	terms, engines []string,
	limit int,
) ([]NamespaceRecommendationHistoryRow, error) {
	if limit <= 0 {
		limit = defaultNamespaceHistoryLimit
	}
	if limit > maxNamespaceHistoryLimit {
		limit = maxNamespaceHistoryLimit
	}

	dbTerms := normalizeNamespaceHistoryTerms(terms)
	dbEngines := normalizeNamespaceHistoryEngines(engines)

	query := `
		SELECT term, engine, COALESCE(schedule_type::text, 'all_hours'),
			created_at,
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

	var out []NamespaceRecommendationHistoryRow
	for rows.Next() {
		var (
			term, engine, scheduleType string
			recordedAt                 time.Time
			recCPUReq, recCPULim       *int64
			recMemReq, recMemLim       *int64
			curCPUReq, curCPULim       *int64
			curMemReq, curMemLim       *int64
			varCPUReq, varCPULim       *float32
			varMemReq, varMemLim       *float32
			confidence                 *float32
			notificationCodes          []int16
		)
		if err := rows.Scan(
			&term, &engine, &scheduleType, &recordedAt,
			&recCPUReq, &recCPULim, &recMemReq, &recMemLim,
			&curCPUReq, &curCPULim, &curMemReq, &curMemLim,
			&varCPUReq, &varCPULim, &varMemReq, &varMemLim,
			&confidence, &notificationCodes,
		); err != nil {
			return nil, fmt.Errorf("scan namespace rec history: %w", err)
		}

		apiTerm := namespaceTermToAPI(term)
		out = append(out,
			namespaceHistoryCPURow(apiTerm, engine, scheduleType, recordedAt,
				recCPUReq, recCPULim, curCPUReq, curCPULim, varCPUReq, varCPULim,
				confidence, notificationCodes),
			namespaceHistoryMemoryRow(apiTerm, engine, scheduleType, recordedAt,
				recMemReq, recMemLim, curMemReq, curMemLim, varMemReq, varMemLim,
				confidence, notificationCodes),
		)
	}
	return out, rows.Err()
}

func namespaceHistoryCPURow(
	term, engine, scheduleType string,
	recordedAt time.Time,
	recReq, recLim, curReq, curLim *int64,
	varReq, varLim *float32,
	confidence *float32,
	notificationCodes []int16,
) NamespaceRecommendationHistoryRow {
	row := NamespaceRecommendationHistoryRow{
		Resource:           "cpu",
		RecommendationType: engine,
		Term:               term,
		ScheduleType:       scheduleType,
		RecordedAt:         recordedAt,
		Recommended: NamespaceHistoryResourceValues{
			RequestMillicores: recReq,
			LimitMillicores:   recLim,
		},
		Current: NamespaceHistoryResourceValues{
			RequestMillicores: curReq,
			LimitMillicores:   curLim,
		},
		ConfidenceLevel:   confidence,
		NotificationCodes: notificationCodes,
	}
	if varReq != nil || varLim != nil {
		row.Utilization = &NamespaceHistoryUtilization{
			RequestVariationPercent: varReq,
			LimitVariationPercent:   varLim,
		}
	}
	return row
}

func namespaceHistoryMemoryRow(
	term, engine, scheduleType string,
	recordedAt time.Time,
	recReq, recLim, curReq, curLim *int64,
	varReq, varLim *float32,
	confidence *float32,
	notificationCodes []int16,
) NamespaceRecommendationHistoryRow {
	row := NamespaceRecommendationHistoryRow{
		Resource:           "memory",
		RecommendationType: engine,
		Term:               term,
		ScheduleType:       scheduleType,
		RecordedAt:         recordedAt,
		Recommended: NamespaceHistoryResourceValues{
			RequestKiB: recReq,
			LimitKiB:   recLim,
		},
		Current: NamespaceHistoryResourceValues{
			RequestKiB: curReq,
			LimitKiB:   curLim,
		},
		ConfidenceLevel:   confidence,
		NotificationCodes: notificationCodes,
	}
	if varReq != nil || varLim != nil {
		row.Utilization = &NamespaceHistoryUtilization{
			RequestVariationPercent: varReq,
			LimitVariationPercent:   varLim,
		}
	}
	return row
}

func namespaceTermToAPI(term string) string {
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

func namespaceTermFromAPI(term string) string {
	t := strings.TrimSpace(term)
	if strings.HasSuffix(t, "_term") {
		return strings.TrimSuffix(t, "_term")
	}
	return t
}

func normalizeNamespaceHistoryTerms(terms []string) []string {
	if len(terms) == 0 {
		return nil
	}
	out := make([]string, 0, len(terms))
	seen := make(map[string]struct{}, len(terms))
	for _, t := range terms {
		db := namespaceTermFromAPI(t)
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

func normalizeNamespaceHistoryEngines(engines []string) []string {
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

// ParseNamespaceHistoryLimit parses the limit query parameter for namespace history.
func ParseNamespaceHistoryLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultNamespaceHistoryLimit, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("invalid limit")
	}
	return limit, nil
}
