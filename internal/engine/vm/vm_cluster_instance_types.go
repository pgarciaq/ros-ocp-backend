package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const clusterInstanceTypesFilename = "cluster_instance_types.json"

// ClusterInstanceTypesPayload is the operator-uploaded cluster catalog document.
type ClusterInstanceTypesPayload struct {
	ClusterUUID   string                      `json:"cluster_uuid"`
	CollectedAt   time.Time                   `json:"collected_at"`
	InstanceTypes []ClusterInstanceTypeRecord `json:"instance_types"`
	Preferences   []ClusterPreferenceRecord   `json:"preferences"`
	VMPreferences map[string]string           `json:"vm_preferences"`
}

// ClusterInstanceTypeRecord is one cluster-defined VirtualMachineClusterInstancetype.
type ClusterInstanceTypeRecord struct {
	Name      string `json:"name"`
	Series    string `json:"series"`
	VCPU      int32  `json:"vcpu"`
	MemoryGiB int32  `json:"memory_gib"`
	GPUs      int32  `json:"gpus"`
}

// ParseClusterInstanceTypesJSON decodes cluster_instance_types.json from r.
func ParseClusterInstanceTypesJSON(r io.Reader) (ClusterInstanceTypesPayload, error) {
	var doc ClusterInstanceTypesPayload
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return ClusterInstanceTypesPayload{}, fmt.Errorf("decode cluster instance types: %w", err)
	}
	return doc, nil
}

func clusterRecordsToInstanceTypes(records []ClusterInstanceTypeRecord) []InstanceType {
	if len(records) == 0 {
		return nil
	}
	out := make([]InstanceType, 0, len(records))
	for _, rec := range records {
		if rec.Name == "" {
			continue
		}
		vcpu := rec.VCPU
		if vcpu < 1 {
			vcpu = 1
		}
		memGiB := rec.MemoryGiB
		if memGiB < 1 {
			memGiB = 1
		}
		out = append(out, InstanceType{
			Name:       rec.Name,
			Series:     NormalizeInstanceTypeSeries(rec.Series),
			VCPU:       vcpu,
			MemoryGiB:  memGiB,
			GPUs:       rec.GPUs,
			Selectable: true,
		})
	}
	return out
}

// UpsertClusterInstanceTypes replaces the catalog for (org_id, cluster_uuid).
func UpsertClusterInstanceTypes(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID, doc ClusterInstanceTypesPayload) error {
	collectedAt := doc.CollectedAt
	if collectedAt.IsZero() {
		collectedAt = time.Now().UTC()
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cluster instance types tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM cluster_instance_types WHERE org_id = $1 AND cluster_uuid = $2`, orgID, clusterUUID); err != nil {
		return fmt.Errorf("delete cluster instance types: %w", err)
	}

	for _, rec := range doc.InstanceTypes {
		if rec.Name == "" {
			continue
		}
		vcpu := rec.VCPU
		if vcpu < 1 {
			vcpu = 1
		}
		memGiB := rec.MemoryGiB
		if memGiB < 1 {
			memGiB = 1
		}
		series := NormalizeInstanceTypeSeries(rec.Series)
		if _, err := tx.Exec(ctx, `
			INSERT INTO cluster_instance_types (
				org_id, cluster_uuid, name, series, vcpu, memory_gib, gpus, collected_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, orgID, clusterUUID, rec.Name, series, vcpu, memGiB, rec.GPUs, collectedAt); err != nil {
			return fmt.Errorf("insert cluster instance type %q: %w", rec.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cluster instance types: %w", err)
	}

	if err := UpsertClusterVMPreferencesMeta(ctx, pool, orgID, clusterUUID, doc.Preferences, doc.VMPreferences, collectedAt); err != nil {
		return err
	}
	return nil
}

// QueryClusterInstanceTypes loads cluster-specific instance types for recommendation matching.
func QueryClusterInstanceTypes(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID) ([]InstanceType, error) {
	rows, err := pool.Query(ctx, `
		SELECT name, series, vcpu, memory_gib, gpus
		FROM cluster_instance_types
		WHERE org_id = $1 AND cluster_uuid = $2
		ORDER BY name
	`, orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("query cluster instance types: %w", err)
	}
	defer rows.Close()

	var out []InstanceType
	for rows.Next() {
		var (
			name      string
			series    string
			vcpu      int32
			memoryGiB int32
			gpus      int32
		)
		if err := rows.Scan(&name, &series, &vcpu, &memoryGiB, &gpus); err != nil {
			return nil, fmt.Errorf("scan cluster instance type: %w", err)
		}
		out = append(out, InstanceType{
			Name:       name,
			Series:     series,
			VCPU:       vcpu,
			MemoryGiB:  memoryGiB,
			GPUs:       gpus,
			Selectable: true,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListClusterInstanceTypes returns stored catalog rows for API responses.
func ListClusterInstanceTypes(ctx context.Context, pool *pgxpool.Pool, orgID string, clusterUUID uuid.UUID) ([]ClusterInstanceTypeRecord, time.Time, error) {
	rows, err := pool.Query(ctx, `
		SELECT name, series, vcpu, memory_gib, gpus, collected_at
		FROM cluster_instance_types
		WHERE org_id = $1 AND cluster_uuid = $2
		ORDER BY name
	`, orgID, clusterUUID)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("list cluster instance types: %w", err)
	}
	defer rows.Close()

	var (
		out         []ClusterInstanceTypeRecord
		collectedAt time.Time
	)
	for rows.Next() {
		var rec ClusterInstanceTypeRecord
		var collected time.Time
		if err := rows.Scan(&rec.Name, &rec.Series, &rec.VCPU, &rec.MemoryGiB, &rec.GPUs, &collected); err != nil {
			return nil, time.Time{}, fmt.Errorf("scan cluster instance type row: %w", err)
		}
		if collectedAt.IsZero() || collected.After(collectedAt) {
			collectedAt = collected
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	if collectedAt.IsZero() {
		collectedAt = time.Now().UTC()
	}
	return out, collectedAt, nil
}

// IsClusterInstanceTypesFile reports whether a Kafka file entry is the cluster catalog JSON.
func IsClusterInstanceTypesFile(fileName string) bool {
	return strings.Contains(fileName, clusterInstanceTypesFilename)
}

// IngestClusterInstanceTypesFromReader parses and persists cluster_instance_types.json.
func IngestClusterInstanceTypesFromReader(ctx context.Context, pool *pgxpool.Pool, r io.Reader, orgID, clusterUUID string) error {
	doc, err := ParseClusterInstanceTypesJSON(r)
	if err != nil {
		return err
	}
	clusterID, err := uuid.Parse(clusterUUID)
	if err != nil {
		return fmt.Errorf("parse cluster UUID: %w", err)
	}
	if doc.ClusterUUID != "" {
		if parsed, parseErr := uuid.Parse(doc.ClusterUUID); parseErr == nil {
			clusterID = parsed
		}
	}
	return UpsertClusterInstanceTypes(ctx, pool, orgID, clusterID, doc)
}
