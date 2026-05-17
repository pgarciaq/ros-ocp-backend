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
}

// GPUMIGListMeta paginates the MIG-focused GPU list.
type GPUMIGListMeta struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// GPUMIGListResponse is returned by GET /recommendations/openshift/gpu/mig.
type GPUMIGListResponse struct {
	Meta     GPUMIGListMeta              `json:"meta"`
	Data     []GPUMIGRecommendationEntry `json:"data"`
	Warnings []string                    `json:"warnings,omitempty"`
}
