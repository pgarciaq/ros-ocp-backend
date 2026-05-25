package api

import (
	"encoding/json"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

type benchListResponse struct {
	Meta struct {
		Count  int `json:"count"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	} `json:"meta"`
	Data []model.NativeContainerResult `json:"data"`
}

// sampleListResponse approximates a typical 20-item container recommendation list (~15KB).
func sampleListResponse() benchListResponse {
	data := make([]model.NativeContainerResult, 20)
	for i := range data {
		cpuReq := int64(250)
		cpuLim := int64(500)
		memReq := int64(512000)
		memLim := int64(1024000)
		conf := float32(0.85)
		data[i] = model.NativeContainerResult{
			ID:          "f47ac10b-58cc-4372-a567-0e02b2c3d479",
			ClusterUUID: "02059694-68ab-4d58-8809-de1e91f1d0e5",
			Project:     "openshift-operators",
			Workload:    "costmanagement-metrics-operator",
			Container:   "manager",
			Recommendations: map[string]model.TermRecommendation{
				"short_term": {
					Cost: &model.EngineRecommendation{
						CPURequestMillicores: &cpuReq,
						CPULimitMillicores:   &cpuLim,
						MemRequestKiB:        &memReq,
						MemLimitKiB:        &memLim,
						ConfidenceLevel:      &conf,
						NotificationCodes:    model.SmallintArray{1, 7},
					},
				},
			},
		}
	}
	var resp benchListResponse
	resp.Meta.Count = 20
	resp.Meta.Limit = 20
	resp.Data = data
	return resp
}

func BenchmarkJSONMarshal(b *testing.B) {
	payload := sampleListResponse()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(payload); err != nil {
			b.Fatal(err)
		}
	}
}
