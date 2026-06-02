package api

const (
	quotaDefaultOrderBy  = "namespace"
	quotaDefaultOrderHow = "asc"
)

var quotaAllowedOrderBy = map[string]string{
	"namespace":                 "namespace",
	"quota_name":                "quota_name",
	"utilization":               `GREATEST(COALESCE(cpu_request_utilization_bp,0), COALESCE(cpu_limit_utilization_bp,0), COALESCE(memory_request_utilization_bp,0), COALESCE(memory_limit_utilization_bp,0), COALESCE(utilization_storage_request_bp,0), COALESCE(utilization_pods_bp,0))`,
	"estimated_monthly_savings": "estimated_savings_cents",
	"risk_level":                `CASE risk_level WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END`,
}

var clusterQuotaAllowedOrderBy = map[string]string{
	"cluster_quota_name":        "cluster_quota_name",
	"utilization":               `GREATEST(COALESCE(utilization_cpu_request_percent,0), COALESCE(utilization_memory_request_percent,0), COALESCE(utilization_storage_request_percent,0), COALESCE(utilization_pods_percent,0))`,
	"estimated_monthly_savings": "savings_dollars_monthly",
	"risk_level":                `CASE risk_level WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END`,
}

const clusterQuotaDefaultOrderBy = "cluster_quota_name"
