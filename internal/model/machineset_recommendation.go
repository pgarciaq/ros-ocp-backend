package model

// MachineSetRecommendation aggregates per-node recommendations for one MachineSet.
type MachineSetRecommendation struct {
	MachineSetName         string   `json:"machineset_name"`
	ClusterUUID            string   `json:"cluster_uuid"`
	ClusterAlias           string   `json:"cluster_alias,omitempty"`
	InstanceType           string   `json:"instance_type,omitempty"`
	CurrentNodeCount       int      `json:"current_node_count"`
	RecommendedNodeCount   int      `json:"recommended_node_count"`
	ExcessNodes            int      `json:"excess_nodes"`
	TotalMonthlySavingsUSD float64  `json:"total_monthly_savings_usd"`
	AvgCPUUtilization      float64  `json:"avg_cpu_utilization"`
	AvgMemoryUtilization   float64  `json:"avg_memory_utilization"`
	Nodes                  []string `json:"nodes"`
}

// MachineSetRecommendationMeta holds pagination metadata for MachineSet list responses.
type MachineSetRecommendationMeta struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// MachineSetRecommendationListResponse is the paginated list response for MachineSet aggregation.
type MachineSetRecommendationListResponse struct {
	Meta  MachineSetRecommendationMeta   `json:"meta"`
	Data  []MachineSetRecommendation   `json:"data"`
	Links PaginationLinks              `json:"links"`
}
