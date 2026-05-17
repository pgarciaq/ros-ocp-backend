package model

// GPUStrategySummary counts for one GPU optimization strategy with a link to the listing endpoint.
type GPUStrategySummary struct {
	Count int    `json:"count"`
	Link  string `json:"link"`
}

// GPUSummaryResponse aggregates GPU recommendation inventory for an organization.
type GPUSummaryResponse struct {
	MIG                 GPUStrategySummary `json:"mig"`
	Timeslicing         GPUStrategySummary `json:"timeslicing"`
	TotalGPUsAnalyzed   int                `json:"total_gpus_analyzed"`
	ClustersWithGPUData int                `json:"clusters_with_gpu_data"`
}
