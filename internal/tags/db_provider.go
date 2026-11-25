package tags

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	kokuEnabledTagKeysTable = "reporting_enabledtagkeys"
	// Legacy values source, deliberately unused (#868):
	// reporting_ocptags_values' cluster/namespace arrays are rebuilt per
	// summarization run from that run's report periods only, so for a
	// key/value shared by two clusters the last-summarized cluster owns
	// them (#815). GetTagValues reads the label summaries below instead.

	podLabelSummaryTable     = "reporting_ocpusagepodlabel_summary"
	storageLabelSummaryTable = "reporting_ocpstoragevolumelabel_summary"
	reportPeriodTable        = "reporting_ocpusagereportperiod"
)

// DBTagProvider reads tag keys and values directly from Koku tenant tables in the shared PostgreSQL.
type DBTagProvider struct {
	pool *pgxpool.Pool
}

// NewDBTagProvider constructs a direct-database tag provider.
func NewDBTagProvider(pool *pgxpool.Pool) *DBTagProvider {
	return &DBTagProvider{pool: pool}
}

func (p *DBTagProvider) GetEnabledTagKeys(ctx context.Context, orgID string) ([]string, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("db tag provider is not configured")
	}
	schema, err := TenantSchema(orgID)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
		SELECT key
		FROM %s
		WHERE enabled = true AND provider_type = 'OCP'
		ORDER BY key`, pgx.Identifier{schema, kokuEnabledTagKeysTable}.Sanitize())

	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query enabled tag keys for org %q: %w", orgID, err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan enabled tag key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled tag keys: %w", err)
	}
	return keys, nil
}

func (p *DBTagProvider) GetTagValues(ctx context.Context, orgID string, key string) ([]string, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("db tag provider is not configured")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("tag key is required")
	}
	schema, err := TenantSchema(orgID)
	if err != nil {
		return nil, err
	}

	// #868: the label summaries are the values source — the same source the
	// #815 filter predicate reads (ADR-0353), NOT reporting_ocptags_values
	// (whose arrays hold only the last-summarized cluster for any shared
	// key/value). The month-snapped window mirrors the api-mode push, so db
	// mode and api mode report the same catalog.
	winStart, winEnd := LabelSummaryWindow()
	podTable := pgx.Identifier{schema, podLabelSummaryTable}.Sanitize()
	volTable := pgx.Identifier{schema, storageLabelSummaryTable}.Sanitize()
	periodTable := pgx.Identifier{schema, reportPeriodTable}.Sanitize()

	summaryValues := func(table string) string {
		return fmt.Sprintf(`
			SELECT DISTINCT UNNEST(ls.values)
			FROM %s ls
			JOIN %s rp ON rp.id = ls.report_period_id
			WHERE ls.key = $1
			  AND rp.report_period_start >= $2::date
			  AND rp.report_period_start < $3::date`, table, periodTable)
	}
	query := fmt.Sprintf(`
		SELECT DISTINCT value FROM (
			%s
			UNION
			%s
		) AS all_values(value)
		ORDER BY value`,
		summaryValues(podTable), summaryValues(volTable))

	rows, err := p.pool.Query(ctx, query,
		key, winStart.Format("2006-01-02"), winEnd.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query tag values for org %q key %q: %w", orgID, key, err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan tag value: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tag values: %w", err)
	}
	return values, nil
}
