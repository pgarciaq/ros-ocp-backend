package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func generateTestRows(numContainers int) []NativeRecommendationRow {
	terms := []string{"short", "medium", "long"}
	engines := []string{"cost", "performance"}
	now := time.Now()
	cpu := int64(100)
	mem := int64(1024)

	rows := make([]NativeRecommendationRow, 0, numContainers*6)
	for i := range numContainers {
		for _, term := range terms {
			for _, engine := range engines {
				rows = append(rows, NativeRecommendationRow{
					ClusterUUID:      "cluster-uuid-1",
					Namespace:        fmt.Sprintf("namespace-%d", i%20),
					Workload:         fmt.Sprintf("deployment-%d", i),
					WorkloadType:     "Deployment",
					ContainerName:    fmt.Sprintf("container-%d", i),
					Term:             term,
					Engine:           engine,
					RecCPURequestMC:  &cpu,
					RecMemRequestKiB: &mem,
					SourceID:         "source-1",
					ClusterAlias:     "test-cluster",
					LastReported:     now,
					UpdatedAt:        now,
					IdleState:        "active",
				})
			}
		}
	}
	return rows
}

func BenchmarkAssembleNativeResults(b *testing.B) {
	logrus.SetLevel(logrus.PanicLevel)
	defer logrus.SetLevel(logrus.InfoLevel)

	for _, numContainers := range []int{50, 200, 1000, 5000} {
		rows := generateTestRows(numContainers)
		b.Run(fmt.Sprintf("containers=%d", numContainers), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				assembleNativeResults(rows, "", false)
			}
		})
	}
}
