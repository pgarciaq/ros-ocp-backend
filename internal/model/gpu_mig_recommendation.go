package model

// GPUMIGRecommendationEntry is one container-term row with a non-full_gpu MIG profile recommendation.
type GPUMIGRecommendationEntry struct {
	ClusterUUID           string  `json:"cluster_uuid"`
	Namespace             string  `json:"namespace"`
	Workload              string  `json:"workload"`
	Container             string  `json:"container"`
	Term                  string  `json:"term"`
	GPUModel              string  `json:"gpu_model"`
	NodeName              string  `json:"node_name,omitempty"`
	RecommendedGPUProfile string  `json:"recommended_gpu_profile"`
	CurrentGPUProfile     string  `json:"current_gpu_profile,omitempty"`
	Classification        string  `json:"gpu_classification"`
	Confidence            float32 `json:"confidence"`
	ConfidenceLevel       float32 `json:"confidence_level"`
	FBUsageMaxMiB         float32 `json:"fb_usage_max_mib"`
	TotalFBMiB            *int64  `json:"total_fb_mib,omitempty"`
	GPUIdleState          string  `json:"gpu_idle_state,omitempty"`
	GPUCount              int     `json:"gpu_count,omitempty"`
}

// GPUMIGListMeta paginates the MIG-focused GPU list.
type GPUMIGListMeta struct {
	Count      int      `json:"count"`
	Limit      int      `json:"limit"`
	Offset     int      `json:"offset"`
	HasNext    bool     `json:"has_next"`
	NextCursor string   `json:"next_cursor,omitempty"`
	Currency   string   `json:"currency"`
	Warnings   []string `json:"warnings,omitempty"`
}

// GPUMIGListResponse is returned by GET /recommendations/openshift/gpu/mig.
type GPUMIGListResponse struct {
	Meta     GPUMIGListMeta              `json:"meta"`
	Data     []GPUMIGRecommendationEntry `json:"data"`
	Warnings []string                    `json:"warnings,omitempty"`
}
