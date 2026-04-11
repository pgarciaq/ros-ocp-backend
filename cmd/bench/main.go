// Scale benchmark for the ros-ocp-backend native recommendation engine.
//
// Spins up a PostgreSQL container, seeds N containers x 30 days of digest
// data, runs the full pipeline (recommend + write + list query + detail query),
// and reports latency and memory for each scale tier.
//
// Usage:
//
//	go run ./cmd/bench/ [tiers...]
//
// Tiers are comma-separated container counts. Default: 100,1000,10000,50000.
// Use "100000" for the full 100K benchmark (takes several minutes).
package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormPG "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

const (
	benchOrgID   = "org-bench-1"
	clusterUUID  = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	clusterAlias = "bench-cluster"
	daysOfData   = 30
)

type tierResult struct {
	Containers    int
	SeedMS        int64
	RecommendMS   int64
	WriteMS       int64
	ListP50MS     float64
	ListP99MS     float64
	DetailMS      float64
	PeakRSSMB     float64
	RecsGenerated int
}

func main() {
	tiers := []int{100, 1000, 10000, 50000}
	if len(os.Args) > 1 {
		tiers = parseTiers(os.Args[1])
	}

	ctx := context.Background()
	fmt.Println("Starting PostgreSQL container...")
	pool, cleanup := startPostgres(ctx)
	defer cleanup()

	seedCluster(ctx, pool)

	var results []tierResult
	for _, n := range tiers {
		fmt.Printf("\n=== Benchmark: %d containers ===\n", n)
		r := runTier(ctx, pool, n)
		results = append(results, r)
		fmt.Printf("  Seed:      %d ms\n", r.SeedMS)
		fmt.Printf("  Recommend: %d ms  (%d recs)\n", r.RecommendMS, r.RecsGenerated)
		fmt.Printf("  Write:     %d ms\n", r.WriteMS)
		fmt.Printf("  List p50:  %.1f ms\n", r.ListP50MS)
		fmt.Printf("  List p99:  %.1f ms\n", r.ListP99MS)
		fmt.Printf("  Detail:    %.1f ms\n", r.DetailMS)
		fmt.Printf("  Peak RSS:  %.1f MB\n", r.PeakRSSMB)
	}

	fmt.Println("\n=== Summary ===")
	fmt.Printf("| %-12s | %-10s | %-12s | %-10s | %-10s | %-10s | %-10s | %-10s |\n",
		"Containers", "Seed (ms)", "Recommend", "Write (ms)", "List p50", "List p99", "Detail", "RSS (MB)")
	fmt.Println("|" + strings.Repeat("-", 107) + "|")
	for _, r := range results {
		fmt.Printf("| %-12d | %-10d | %-12d | %-10d | %-10.1f | %-10.1f | %-10.1f | %-10.1f |\n",
			r.Containers, r.SeedMS, r.RecommendMS, r.WriteMS,
			r.ListP50MS, r.ListP99MS, r.DetailMS, r.PeakRSSMB)
	}
}

func parseTiers(s string) []int {
	parts := strings.Split(s, ",")
	tiers := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "invalid tier %q, skipping\n", p)
			continue
		}
		tiers = append(tiers, n)
	}
	if len(tiers) == 0 {
		fmt.Fprintln(os.Stderr, "no valid tiers, using defaults")
		return []int{100, 1000, 10000}
	}
	return tiers
}

func startPostgres(ctx context.Context) (*pgxpool.Pool, func()) {
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("ros_bench"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: start postgres: %v\n", err)
		os.Exit(1)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: connection string: %v\n", err)
		os.Exit(1)
	}

	migrDir := migrationsPath()
	m, err := migrate.New("file://"+migrDir, connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: migrate new: %v\n", err)
		os.Exit(1)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		fmt.Fprintf(os.Stderr, "FATAL: migrate up: %v\n", err)
		os.Exit(1)
	}
	m.Close()

	// Create partitions for the digest table before seeding data.
	tmpPool, _ := pgxpool.NewWithConfig(ctx, func() *pgxpool.Config {
		c, _ := pgxpool.ParseConfig(connStr)
		return c
	}())
	createPartitions(ctx, tmpPool)
	tmpPool.Close()

	poolCfg, _ := pgxpool.ParseConfig(connStr)
	poolCfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: pool: %v\n", err)
		os.Exit(1)
	}

	// Initialize the GORM global so model.GetNativeRecommendations works.
	gormDB, err := gorm.Open(gormPG.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: gorm open: %v\n", err)
		os.Exit(1)
	}
	database.DB = gormDB

	return pool, func() {
		pool.Close()
		pgContainer.Terminate(ctx)
	}
}

func migrationsPath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("unable to determine file path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

func createPartitions(ctx context.Context, pool *pgxpool.Pool) {
	now := time.Now().UTC()
	for m := -3; m <= 3; m++ {
		d := now.AddDate(0, m, 0)
		start := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		name := fmt.Sprintf("daily_container_digests_%s", start.Format("200601"))
		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
			name, start.Format("2006-01-02"), end.Format("2006-01-02"),
		)
		if _, err := pool.Exec(ctx, sql); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: create partition %s: %v\n", name, err)
		}
	}
}

func seedCluster(ctx context.Context, pool *pgxpool.Pool) {
	_, err := pool.Exec(ctx, `
		INSERT INTO rh_accounts (id, org_id) VALUES (1, $1)
		ON CONFLICT DO NOTHING`, benchOrgID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: seed rh_accounts: %v\n", err)
		os.Exit(1)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, $2, 1, now())
		ON CONFLICT DO NOTHING`, clusterUUID, clusterAlias)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: seed cluster: %v\n", err)
		os.Exit(1)
	}
}

func runTier(ctx context.Context, pool *pgxpool.Pool, nContainers int) tierResult {
	// Clean previous tier's data.
	pool.Exec(ctx, "TRUNCATE daily_container_digests CASCADE")
	pool.Exec(ctx, "TRUNCATE recommendation_sets CASCADE")

	baseDate := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	endDate := baseDate.AddDate(0, 0, daysOfData)

	// Seed digest data.
	t0 := time.Now()
	seedDigests(ctx, pool, nContainers, baseDate)
	seedMS := time.Since(t0).Milliseconds()

	// Recommend.
	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	t0 = time.Now()
	recs, err := engine.RecommendAllWorkloads(ctx, pool, benchOrgID, clusterUUID, baseDate, endDate, engine.OOMConfig{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ERROR recommend: %v\n", err)
		return tierResult{Containers: nContainers, SeedMS: seedMS}
	}
	recommendMS := time.Since(t0).Milliseconds()

	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)
	peakRSS := float64(m2.Sys-m1.Sys) / (1024 * 1024)
	if peakRSS < 0 {
		peakRSS = float64(m2.Sys) / (1024 * 1024)
	}

	// Write.
	t0 = time.Now()
	err = engine.WriteRecommendations(ctx, pool, recs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  ERROR write: %v\n", err)
	}
	writeMS := time.Since(t0).Milliseconds()

	// List query latency (run 20 iterations).
	listLatencies := benchmarkList(nContainers)
	sort.Float64s(listLatencies)
	listP50 := percentile(listLatencies, 0.50)
	listP99 := percentile(listLatencies, 0.99)

	// Detail query latency.
	detailMS := benchmarkDetail(nContainers, recs)

	return tierResult{
		Containers:    nContainers,
		SeedMS:        seedMS,
		RecommendMS:   recommendMS,
		WriteMS:       writeMS,
		ListP50MS:     listP50,
		ListP99MS:     listP99,
		DetailMS:      detailMS,
		PeakRSSMB:     peakRSS,
		RecsGenerated: len(recs),
	}
}

func seedDigests(ctx context.Context, pool *pgxpool.Pool, nContainers int, baseDate time.Time) {
	rng := rand.New(rand.NewSource(42))

	// pgx limits parameterized queries to 65535 params.
	// 27 columns per row → max ~2427 rows per batch → ~80 containers × 30 days.
	maxRowsPerBatch := 2400 / daysOfData
	if maxRowsPerBatch < 1 {
		maxRowsPerBatch = 1
	}
	sql := buildBulkInsertSQL()

	for start := 0; start < nContainers; start += maxRowsPerBatch {
		end := start + maxRowsPerBatch
		if end > nContainers {
			end = nContainers
		}

		args := make([]interface{}, 0, (end-start)*daysOfData*27)
		for i := start; i < end; i++ {
			ns := fmt.Sprintf("ns-%04d", i/100)
			wl := fmt.Sprintf("deploy-%05d", i)
			cn := "main"
			for d := 0; d < daysOfData; d++ {
				cpu := int64(50 + rng.Intn(900))
				mem := int64(65536 + rng.Intn(524288))
				date := baseDate.AddDate(0, 0, d)
				args = append(args,
					date, benchOrgID, clusterUUID, ns, wl, "deployment", cn,
					cpu-20, cpu+10,
					cpu-10, cpu, cpu+5, cpu+8, cpu+15,
					int64(5), int64(10),
					mem-1024, mem+512,
					mem-512, mem, mem+1024,
					mem-256, mem+512,
					int64(0), cpu-5, mem-256, int64(96),
				)
			}
		}

		_, err := pool.Exec(ctx, sql(end-start), args...)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  ERROR seed batch %d-%d: %v\n", start, end, err)
			return
		}
	}
}

func buildBulkInsertSQL() func(nContainers int) string {
	return func(nContainers int) string {
		var b strings.Builder
		b.WriteString(`INSERT INTO daily_container_digests (
			bucket_date, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			cpu_request_p50_mc, cpu_request_p95_mc,
			cpu_usage_p50_mc, cpu_usage_p95_mc, cpu_usage_p98_mc, cpu_usage_p99_mc, cpu_usage_max_mc,
			cpu_throttle_p95_mc, cpu_throttle_max_mc,
			memory_request_p50_kib, memory_request_p95_kib,
			memory_usage_p50_kib, memory_usage_p95_kib, memory_usage_max_kib,
			memory_rss_p95_kib, memory_rss_max_kib,
			oom_count_sum, cpu_usage_mean_mc, memory_usage_mean_kib, sample_count
		) VALUES `)

		paramIdx := 1
		totalRows := nContainers * daysOfData
		for row := 0; row < totalRows; row++ {
			if row > 0 {
				b.WriteString(",")
			}
			b.WriteString("(")
			for col := 0; col < 27; col++ {
				if col > 0 {
					b.WriteString(",")
				}
				b.WriteString(fmt.Sprintf("$%d", paramIdx))
				paramIdx++
			}
			b.WriteString(")")
		}
		b.WriteString(` ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, bucket_date)
			DO NOTHING`)
		return b.String()
	}
}

func benchmarkList(nContainers int) []float64 {
	iterations := 20
	if nContainers >= 50000 {
		iterations = 5
	}

	emptyPerms := map[string][]string{}
	opts := listoptions.ListOptions{
		Limit:    10,
		Offset:   0,
		OrderBy:  "cluster",
		OrderHow: "asc",
		Format:   "json",
	}
	qp := map[string]interface{}{
		"rs.updated_at >= ?": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		"rs.updated_at < ?":  time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	latencies := make([]float64, 0, iterations)
	for i := 0; i < iterations; i++ {
		t0 := time.Now()
		_, _, err := model.GetNativeRecommendations(benchOrgID, opts, qp, emptyPerms)
		elapsed := time.Since(t0).Seconds() * 1000
		if err != nil {
			fmt.Fprintf(os.Stderr, "  WARNING list query error: %v\n", err)
		}
		latencies = append(latencies, elapsed)
	}
	return latencies
}

func benchmarkDetail(nContainers int, recs []engine.ContainerRec) float64 {
	if len(recs) == 0 {
		return 0
	}

	// Pick a rec from the middle of the list.
	r := recs[len(recs)/2]
	id := model.NativeContainerID(r.ClusterUUID, r.Namespace, r.Workload, r.ContainerName)

	iterations := 50
	if nContainers >= 50000 {
		iterations = 10
	}

	var totalMS float64
	for i := 0; i < iterations; i++ {
		t0 := time.Now()
		_, err := model.GetNativeRecommendationByID(benchOrgID, id, map[string][]string{})
		elapsed := time.Since(t0).Seconds() * 1000
		if err != nil {
			fmt.Fprintf(os.Stderr, "  WARNING detail query error: %v\n", err)
		}
		totalMS += elapsed
	}
	return totalMS / float64(iterations)
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}
