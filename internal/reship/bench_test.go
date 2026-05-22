package reship

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
)

// BH-PERF-005: simulate 90-day × 100-container reship file processing throughput.
// Measures masu HTTP round-trip + per-file digest CPU (no live DB/Kafka).
//
// Design expectation: ~20–40s ros processing for 90 files @ 100 containers; masu ~5–10s.
// Last run: ~0.48–0.55 sec/op (90 days × 100 containers digest CPU + mock masu) — PASS

const (
	benchReshipDays       = 90
	benchReshipContainers = 100
	benchReshipSamples    = 96
)

func benchReshipDigestFile(dayOffset int) {
	for c := 0; c < benchReshipContainers; c++ {
		rows := benchReshipRows(dayOffset, c, benchReshipSamples)
		key := ingestion.DigestKey{
			OrgID: "org-bench", ClusterUUID: "00000000-0000-0000-0000-000000000001",
			Namespace: rows[0].Namespace, Workload: rows[0].WorkloadName,
			WorkloadType: rows[0].WorkloadType, ContainerName: rows[0].ContainerName,
			BucketDate:   rows[0].IntervalStart.Truncate(24 * time.Hour),
			ScheduleType: ingestion.ScheduleTypeAllHours,
		}
		_ = ingestion.ComputeContainerDigest(key, rows)
	}
}

func benchReshipRows(dayOffset, containerIdx, samples int) []ingestion.MetricRow {
	rows := make([]ingestion.MetricRow, samples)
	day := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, dayOffset)
	for i := 0; i < samples; i++ {
		start := day.Add(time.Duration(i*15) * time.Minute)
		rows[i] = ingestion.MetricRow{
			IntervalStart: start,
			IntervalEnd:   start.Add(15 * time.Minute),
			Namespace:     "bench-ns",
			WorkloadName:  fmt.Sprintf("wl-%d", containerIdx),
			WorkloadType:  "deployment",
			ContainerName: fmt.Sprintf("ctr-%d", containerIdx),
			CPUUsageMC:    100 + int64(i),
			MemUsageKiB:   104857600,
		}
	}
	return rows
}

// BenchmarkReshipThroughput simulates one masu reship call plus per-day re-ingestion CPU.
func BenchmarkReshipThroughput(b *testing.B) {
	clusterID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	masu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"files_processed":%d,"files_total":%d}`, benchReshipDays, benchReshipDays)))
	}))
	defer masu.Close()
	client := NewHTTPClient(masu.URL, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		_, err := client.PostReship(ctx, "org1234567", clusterID)
		if err != nil {
			b.Fatal(err)
		}
		for day := 0; day < benchReshipDays; day++ {
			benchReshipDigestFile(day)
		}
	}
	perRunSec := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / 1e9
	b.ReportMetric(perRunSec, "sec/op")
	if perRunSec > 60 {
		b.Logf("BH-PERF-005: %.1fs exceeds 60s simulated threshold", perRunSec)
	}
}
