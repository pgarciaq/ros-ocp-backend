-- COST-7274: Normalize existing workload_type values to lowercase.
-- New ingestion already stores lowercase via strings.ToLower() in csvparser.go,
-- but pre-existing rows may contain PascalCase values (e.g. "DaemonSet").

UPDATE daily_container_digests SET workload_type = LOWER(workload_type) WHERE workload_type != LOWER(workload_type);
UPDATE container_usage_samples SET workload_type = LOWER(workload_type) WHERE workload_type != LOWER(workload_type);
UPDATE recommendation_sets SET workload_type = LOWER(workload_type) WHERE workload_type != LOWER(workload_type);
UPDATE gpu_container_digests SET workload_type = LOWER(workload_type) WHERE workload_type != LOWER(workload_type);
UPDATE org_container_keys SET workload_type = LOWER(workload_type) WHERE workload_type != LOWER(workload_type);
UPDATE recommendation_quality SET workload_type = LOWER(workload_type) WHERE workload_type != LOWER(workload_type);
UPDATE workloads SET workload_type = LOWER(workload_type) WHERE workload_type != LOWER(workload_type);
