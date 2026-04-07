// Kruize vs Native Engine Comparison Tool
//
// This CLI tool compares resource optimization recommendations produced by two
// independent engines that share the same input data:
//
//   - Native Go engine: ingestion.ProcessCSVToDigests -> engine.RecommendAllWorkloads
//   - Kruize (Java):    createPerformanceProfile -> createExperiment -> updateResults -> updateRecommendations
//
// Prerequisites:
//   - Build the Kruize image: cd ~/dev/koku/autotune && ./build.sh -i kruize:local
//   - Generate nise data:     nise report ocp --ros-ocp-info --static-report-file <yaml> --ocp-cluster-id <id> --insights-upload <dir> --daily-reports
//   - Docker daemon running (testcontainers spins up PostgreSQL and Kruize containers)
//
// Usage:
//
//	go run ./cmd/compare/ <nise-ros-csv-path> [cluster-id] [org-id] [cluster-uuid]
//
// Output: comparison.csv with side-by-side recommendations and percentage differences.
package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/types/kruizePayload"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <nise-ros-csv-path> [cluster-id] [org-id] [cluster-uuid]\n", os.Args[0])
		os.Exit(1)
	}
	csvPath := os.Args[1]
	clusterID := "comparison-cluster-1"
	clusterUUID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	orgID := "org1234567"
	if len(os.Args) > 2 {
		clusterID = os.Args[2]
	}
	if len(os.Args) > 3 {
		orgID = os.Args[3]
	}
	if len(os.Args) > 4 {
		clusterUUID = os.Args[4]
	}

	ctx := context.Background()

	fmt.Println("=== Kruize vs Native Engine Comparison Tool ===")
	fmt.Printf("CSV: %s\nCluster: %s (UUID: %s)\nOrg: %s\n\n", csvPath, clusterID, clusterUUID, orgID)

	// Step 1: Transform nise CSV to native format
	fmt.Println("[1/7] Transforming nise CSV to native engine format...")
	nativeCSV, err := transformNiseCSV(csvPath)
	if err != nil {
		fatal("transform CSV: %v", err)
	}
	fmt.Printf("  Transformed %d data rows\n", bytes.Count(nativeCSV, []byte("\n"))-1)

	// Step 2: Start native PostgreSQL
	fmt.Println("[2/7] Starting native engine PostgreSQL...")
	nativePool, nativeTeardown, err := startNativePostgres(ctx)
	if err != nil {
		fatal("native postgres: %v", err)
	}
	defer nativeTeardown()
	fmt.Println("  Native PostgreSQL ready")

	// Step 3: Run native engine pipeline
	fmt.Println("[3/7] Running native engine pipeline...")
	nativeRecs, err := runNativeEngine(ctx, nativePool, nativeCSV, orgID, clusterUUID)
	if err != nil {
		fatal("native engine: %v", err)
	}
	fmt.Printf("  Native engine produced %d recommendations\n", len(nativeRecs))

	// Step 4: Start Kruize + its PostgreSQL
	fmt.Println("[4/7] Starting Kruize + PostgreSQL...")
	kruizeURL, kruizeTeardown, err := startKruize(ctx)
	if err != nil {
		fatal("kruize: %v", err)
	}
	defer kruizeTeardown()
	fmt.Printf("  Kruize ready at %s\n", kruizeURL)

	// Step 5: Run Kruize pipeline
	fmt.Println("[5/7] Running Kruize pipeline...")
	kruizeRecs, err := runKruizePipeline(ctx, kruizeURL, csvPath, clusterUUID, orgID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  WARNING: Kruize pipeline error: %v\n", err)
		kruizeRecs = nil
	} else {
		fmt.Printf("  Kruize produced %d recommendations\n", len(kruizeRecs))
	}

	// Step 6: Generate comparison
	fmt.Println("[6/7] Generating comparison spreadsheet...")
	outputPath := "comparison.csv"
	if err := writeComparison(outputPath, nativeRecs, kruizeRecs, clusterID); err != nil {
		fatal("comparison output: %v", err)
	}
	fmt.Printf("  Written to %s\n", outputPath)

	// Step 7: Print summary
	fmt.Println("[7/7] Summary:")
	printSummary(nativeRecs, kruizeRecs)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
	os.Exit(1)
}

// transformNiseCSV reads a nise-generated ROS CSV (operator format) and renames
// columns to match the native engine's expected format. For example,
// "cpu_request_container_avg" becomes "cpu_request" and "workload" becomes
// "workload_name". Units are preserved (CPU in cores, memory in bytes).
func transformNiseCSV(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}

	colMap := map[string]string{
		"interval_start":                 "interval_start",
		"interval_end":                   "interval_end",
		"namespace":                      "namespace",
		"workload":                       "workload_name",
		"workload_type":                  "workload_type",
		"container_name":                 "container_name",
		"cpu_request_container_avg":      "cpu_request",
		"cpu_limit_container_avg":        "cpu_limit",
		"cpu_usage_container_avg":        "cpu_usage",
		"cpu_throttle_container_avg":     "cpu_throttle",
		"memory_request_container_avg":   "mem_request",
		"memory_limit_container_avg":     "mem_limit",
		"memory_usage_container_avg":     "mem_usage",
		"memory_rss_usage_container_avg": "mem_rss",
	}

	// Build index: find which source columns we need
	srcIndices := map[string]int{}
	for i, h := range header {
		if _, ok := colMap[h]; ok {
			srcIndices[h] = i
		}
	}

	// Verify required columns exist
	required := []string{"interval_start", "interval_end", "namespace", "workload", "container_name",
		"cpu_request_container_avg", "cpu_usage_container_avg", "memory_request_container_avg", "memory_usage_container_avg"}
	for _, r := range required {
		if _, ok := srcIndices[r]; !ok {
			return nil, fmt.Errorf("missing required column %q in nise CSV", r)
		}
	}

	// Build output header in a deterministic order
	nativeHeader := []string{"interval_start", "interval_end", "namespace", "workload_name",
		"workload_type", "container_name", "cpu_request", "cpu_limit", "cpu_usage",
		"cpu_throttle", "mem_request", "mem_limit", "mem_usage", "mem_rss"}

	// Reverse-map: native column name -> nise column name
	nativeToNise := map[string]string{}
	for nise, native := range colMap {
		nativeToNise[native] = nise
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	writer.Write(nativeHeader)

	rowCount := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading row: %w", err)
		}

		row := make([]string, len(nativeHeader))
		for i, nativeCol := range nativeHeader {
			niseCol := nativeToNise[nativeCol]
			if idx, ok := srcIndices[niseCol]; ok && idx < len(record) {
				row[i] = record[idx]
			}
		}
		writer.Write(row)
		rowCount++
	}
	writer.Flush()

	return buf.Bytes(), nil
}

// startNativePostgres spins up a PostgreSQL 16 testcontainer, applies schema
// migrations, and creates monthly partitions for daily_container_digests covering
// 6 months back to 3 months ahead of the current date.
func startNativePostgres(ctx context.Context) (*pgxpool.Pool, func(), error) {
	pgC, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("rosocp"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start postgres: %w", err)
	}

	connStr, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		pgC.Terminate(ctx)
		return nil, nil, fmt.Errorf("connection string: %w", err)
	}

	// Run migrations
	migrationsDir, err := filepath.Abs("migrations")
	if err != nil {
		pgC.Terminate(ctx)
		return nil, nil, fmt.Errorf("migrations path: %w", err)
	}
	m, err := migrate.New("file://"+migrationsDir, connStr)
	if err != nil {
		pgC.Terminate(ctx)
		return nil, nil, fmt.Errorf("init migrate: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		pgC.Terminate(ctx)
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		pgC.Terminate(ctx)
		return nil, nil, fmt.Errorf("connect pool: %w", err)
	}

	// Create partitions for the past 6 months + future 3 months to cover any nise data range
	_, err = pool.Exec(ctx, `
		DO $$ DECLARE
			month_start DATE; month_end DATE; part_name TEXT;
		BEGIN
			FOR i IN -6..3 LOOP
				month_start := date_trunc('month', CURRENT_DATE) + (i || ' months')::interval;
				month_end   := month_start + '1 month'::interval;
				part_name   := 'daily_container_digests_' || to_char(month_start, 'YYYYMM');
				IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
					EXECUTE format(
						'CREATE TABLE IF NOT EXISTS %I PARTITION OF daily_container_digests FOR VALUES FROM (%L) TO (%L)',
						part_name, month_start, month_end
					);
				END IF;
			END LOOP;
		END $$;
	`)
	if err != nil {
		pool.Close()
		pgC.Terminate(ctx)
		return nil, nil, fmt.Errorf("create partitions: %w", err)
	}

	teardown := func() {
		pool.Close()
		pgC.Terminate(ctx)
	}
	return pool, teardown, nil
}

// runNativeEngine ingests the transformed CSV into daily digests via the native
// pipeline, then runs RecommendAllWorkloads to produce recommendations for all
// containers across short/medium/long terms and cost/performance profiles.
func runNativeEngine(ctx context.Context, pool *pgxpool.Pool, csvData []byte, orgID, clusterID string) ([]engine.ContainerRec, error) {
	if err := ingestion.ProcessCSVToDigests(ctx, pool, bytes.NewReader(csvData), orgID, clusterID); err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	// Determine date range from the digests
	var minDate, maxDate time.Time
	err := pool.QueryRow(ctx, `SELECT MIN(bucket_date), MAX(bucket_date) FROM daily_container_digests WHERE org_id=$1 AND cluster_uuid=$2`, orgID, clusterID).Scan(&minDate, &maxDate)
	if err != nil {
		return nil, fmt.Errorf("query date range: %w", err)
	}

	recs, err := engine.RecommendAllWorkloads(ctx, pool, orgID, clusterID, minDate, maxDate)
	if err != nil {
		return nil, fmt.Errorf("recommend: %w", err)
	}
	return recs, nil
}

// startKruize creates a shared Docker network, starts a dedicated PostgreSQL for
// Kruize, generates a cdappconfig.json with connection details, and launches the
// kruize:local container. Returns the Kruize API base URL.
func startKruize(ctx context.Context) (string, func(), error) {
	net, err := testcontainers.GenericNetwork(ctx, testcontainers.GenericNetworkRequest{
		NetworkRequest: testcontainers.NetworkRequest{Name: "kruize-compare-net"},
	})
	if err != nil {
		return "", nil, fmt.Errorf("create network: %w", err)
	}

	pgAlias := "kruize-db"

	kruizePG, err := postgres.Run(ctx, "postgres:16",
		postgres.WithDatabase("kruizeDB"),
		postgres.WithUsername("kruize"),
		postgres.WithPassword("kruize"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60*time.Second)),
		testcontainers.CustomizeRequest(testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Networks:       []string{"kruize-compare-net"},
				NetworkAliases: map[string][]string{"kruize-compare-net": {pgAlias}},
			},
		}),
	)
	if err != nil {
		net.Remove(ctx)
		return "", nil, fmt.Errorf("start kruize postgres: %w", err)
	}

	// Generate cdappconfig.json (same format as scripts/cdappconfig.json)
	cdAppConfig := map[string]any{
		"database": map[string]any{
			"adminPassword": "kruize",
			"adminUsername": "kruize",
			"hostname":      pgAlias,
			"name":          "kruizeDB",
			"password":      "kruize",
			"port":          5432,
			"username":      "kruize",
			"sslMode":       "disable",
		},
	}
	cdAppConfigJSON, _ := json.MarshalIndent(cdAppConfig, "", "  ")

	tmpDir, err := os.MkdirTemp("", "kruize-config-*")
	if err != nil {
		kruizePG.Terminate(ctx)
		net.Remove(ctx)
		return "", nil, fmt.Errorf("tmpdir: %w", err)
	}
	cdAppCfgPath := filepath.Join(tmpDir, "cdappconfig.json")
	os.WriteFile(cdAppCfgPath, cdAppConfigJSON, 0644)

	req := testcontainers.ContainerRequest{
		Image:        "kruize:local",
		ExposedPorts: []string{"8080/tcp"},
		Networks:     []string{"kruize-compare-net"},
		Env: map[string]string{
			"LOGGING_LEVEL":                 "info",
			"ROOT_LOGGING_LEVEL":            "error",
			"DB_CONFIG_FILE":                "/tmp/cdappconfig.json",
			"dbdriver":                      "jdbc:postgresql://",
			"database_name":                 "kruizeDB",
			"clustertype":                   "kubernetes",
			"k8stype":                       "minikube",
			"authtype":                      "",
			"monitoringagent":               "prometheus",
			"monitoringservice":             "prometheus-k8s",
			"monitoringendpoint":            "prometheus-k8s",
			"savetodb":                      "true",
			"local":                         "true",
			"LOG_ALL_HTTP_REQ_AND_RESPONSE": "true",
			"hibernate_dialect":             "org.hibernate.dialect.PostgreSQLDialect",
			"hibernate_driver":              "org.postgresql.Driver",
			"hibernate_c3p0minsize":         "2",
			"hibernate_c3p0maxsize":         "5",
			"hibernate_c3p0timeout":         "300",
			"hibernate_c3p0maxstatements":   "50",
			"hibernate_hbm2ddlauto":         "update",
			"hibernate_showsql":             "false",
			"hibernate_timezone":            "UTC",
			"plots":                         "true",
		},
		Files: []testcontainers.ContainerFile{
			{HostFilePath: cdAppCfgPath, ContainerFilePath: "/tmp/cdappconfig.json", FileMode: 0644},
		},
		WaitingFor: wait.ForHTTP("/health").WithPort("8080/tcp").WithStartupTimeout(180 * time.Second),
	}

	kruizeC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		// Try to get logs for debugging
		if kruizeC != nil {
			logs, _ := kruizeC.Logs(ctx)
			if logs != nil {
				logBytes, _ := io.ReadAll(logs)
				fmt.Fprintf(os.Stderr, "Kruize container logs:\n%s\n", string(logBytes))
			}
		}
		kruizePG.Terminate(ctx)
		net.Remove(ctx)
		os.RemoveAll(tmpDir)
		return "", nil, fmt.Errorf("start kruize: %w", err)
	}

	kruizeHost, _ := kruizeC.Host(ctx)
	kruizePort, _ := kruizeC.MappedPort(ctx, "8080")
	kruizeURL := fmt.Sprintf("http://%s:%s", kruizeHost, kruizePort.Port())

	teardown := func() {
		kruizeC.Terminate(ctx)
		kruizePG.Terminate(ctx)
		net.Remove(ctx)
		os.RemoveAll(tmpDir)
	}

	return kruizeURL, teardown, nil
}

// kruizeRec holds one Kruize recommendation for comparison.
type kruizeRec struct {
	Namespace string
	Workload  string
	Container string
	Term      string
	Engine    string
	CPUReqMC  int64
	CPULimMC  int64
	MemReqKiB int64
	MemLimKiB int64
}

// runKruizePipeline drives the full Kruize recommendation lifecycle:
// 1. Creates a performance profile from resource_optimization_openshift.json
// 2. Parses the nise CSV to discover workloads and group data by interval
// 3. Creates one Kruize experiment per workload
// 4. Sends metric data for each 15-minute interval via updateResults
// 5. Fetches final recommendations via updateRecommendations
func runKruizePipeline(ctx context.Context, kruizeURL, niseCSVPath, clusterUUID, orgID string) ([]kruizeRec, error) {
	client := &http.Client{Timeout: 120 * time.Second}

	// Step 1: Create performance profile
	fmt.Println("  Creating performance profile...")
	profileJSON, err := os.ReadFile("resource_optimization_openshift.json")
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	resp, err := client.Post(kruizeURL+"/createPerformanceProfile", "application/json", bytes.NewReader(profileJSON))
	if err != nil {
		return nil, fmt.Errorf("create profile: %w", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		fmt.Printf("  Profile creation status: %d - %s\n", resp.StatusCode, string(body))
	}

	// Step 2: Parse nise CSV to build experiments
	f, err := os.Open(niseCSVPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	colIdx := map[string]int{}
	for i, h := range header {
		colIdx[h] = i
	}

	type workloadKey struct {
		namespace, workload, workloadType string
	}
	type intervalData struct {
		containers []map[string]interface{}
	}

	workloadContainers := map[workloadKey]map[string]string{}
	intervalGroups := map[string]*intervalData{} // key: interval_end

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		col := func(name string) string {
			if idx, ok := colIdx[name]; ok && idx < len(record) {
				return record[idx]
			}
			return ""
		}

		ns := col("namespace")
		workload := col("workload")
		wlType := col("workload_type")
		container := col("container_name")
		intervalEnd := col("interval_end")
		imageName := col("image_name")

		wk := workloadKey{ns, workload, wlType}
		if workloadContainers[wk] == nil {
			workloadContainers[wk] = map[string]string{}
		}
		workloadContainers[wk][container] = imageName

		if intervalGroups[intervalEnd] == nil {
			intervalGroups[intervalEnd] = &intervalData{}
		}

		row := map[string]interface{}{
			"namespace":       ns,
			"k8s_object_type": wlType,
			"k8s_object_name": workload,
			"container_name":  container,
			"image_name":      imageName,
			"interval_start":  col("interval_start"),
			"interval_end":    intervalEnd,
		}
		// Add all metric columns
		metricCols := []string{
			"cpu_request_container_avg", "cpu_request_container_sum",
			"cpu_limit_container_avg", "cpu_limit_container_sum",
			"cpu_usage_container_avg", "cpu_usage_container_min", "cpu_usage_container_max", "cpu_usage_container_sum",
			"cpu_throttle_container_avg", "cpu_throttle_container_max", "cpu_throttle_container_sum",
			"memory_request_container_avg", "memory_request_container_sum",
			"memory_limit_container_avg", "memory_limit_container_sum",
			"memory_usage_container_avg", "memory_usage_container_min", "memory_usage_container_max", "memory_usage_container_sum",
			"memory_rss_usage_container_avg", "memory_rss_usage_container_min", "memory_rss_usage_container_max", "memory_rss_usage_container_sum",
		}
		for _, mc := range metricCols {
			row[mc] = col(mc)
		}

		intervalGroups[intervalEnd].containers = append(intervalGroups[intervalEnd].containers, row)
	}

	// Step 3: Create experiments (one per workload)
	experimentNames := map[workloadKey]string{}
	for wk, containers := range workloadContainers {
		expName := fmt.Sprintf("%s|%s|%s|%s", orgID, clusterUUID, wk.namespace, wk.workload)
		experimentNames[wk] = expName

		containerList := []map[string]string{}
		for cn, img := range containers {
			containerList = append(containerList, map[string]string{
				"container_name":       cn,
				"container_image_name": img,
			})
		}

		payload, err := kruizePayload.GetCreateExperimentPayload(
			expName,
			fmt.Sprintf("%s;%s", orgID, clusterUUID),
			containerList,
			map[string]string{
				"namespace":       wk.namespace,
				"k8s_object_type": wk.workloadType,
				"k8s_object_name": wk.workload,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("create experiment payload: %w", err)
		}

		fmt.Printf("  Creating experiment: %s\n", expName)
		resp, err := client.Post(kruizeURL+"/createExperiment", "application/json", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create experiment %s: %w", expName, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 201 {
			fmt.Printf("    Status %d: %s\n", resp.StatusCode, truncate(string(body), 200))
		}
	}

	// Step 4: Update results - build Kruize payloads directly
	fmt.Println("  Sending metrics to Kruize...")

	// Sort intervals chronologically
	intervalKeys := make([]string, 0, len(intervalGroups))
	for k := range intervalGroups {
		intervalKeys = append(intervalKeys, k)
	}
	sort.Strings(intervalKeys)

	var maxEndTime time.Time
	sentCount := 0
	for _, intervalEnd := range intervalKeys {
		ig := intervalGroups[intervalEnd]

		byWorkload := map[workloadKey][]map[string]interface{}{}
		for _, row := range ig.containers {
			wk := workloadKey{
				namespace:    row["namespace"].(string),
				workload:     row["k8s_object_name"].(string),
				workloadType: row["k8s_object_type"].(string),
			}
			byWorkload[wk] = append(byWorkload[wk], row)
		}

		for wk, rows := range byWorkload {
			expName := experimentNames[wk]

			iStart, _ := parseNiseTimestamp(rows[0]["interval_start"].(string))
			iEnd, _ := parseNiseTimestamp(intervalEnd)
			iStartISO := iStart.UTC().Format("2006-01-02T15:04:05.000Z")
			iEndISO := iEnd.UTC().Format("2006-01-02T15:04:05.000Z")

			// Build container metrics manually
			var containers []map[string]any
			for _, row := range rows {
				c := buildKruizeContainerMetrics(row)
				containers = append(containers, c)
			}

			payload := []map[string]any{{
				"version":             "1.0",
				"experiment_name":     expName,
				"interval_start_time": iStartISO,
				"interval_end_time":   iEndISO,
				"kubernetes_objects": []map[string]any{{
					"type":       wk.workloadType,
					"name":       wk.workload,
					"namespace":  wk.namespace,
					"containers": containers,
				}},
			}}

			payloadJSON, _ := json.Marshal(payload)
			resp, err := client.Post(kruizeURL+"/updateResults", "application/json", bytes.NewReader(payloadJSON))
			if err != nil {
				fmt.Printf("    WARNING: updateResults failed for %s: %v\n", expName, err)
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 201 {
				sentCount++
			} else if resp.StatusCode != 400 || sentCount == 0 {
				// Only log first few 400s to avoid flooding
				fmt.Printf("    WARNING: updateResults %d for %s: %s\n", resp.StatusCode, expName, truncate(string(body), 200))
			}
		}

		t, err := parseNiseTimestamp(intervalEnd)
		if err == nil && t.After(maxEndTime) {
			maxEndTime = t
		}
	}
	fmt.Printf("  Sent %d metric batches successfully\n", sentCount)

	if maxEndTime.IsZero() {
		return nil, fmt.Errorf("no valid interval timestamps found")
	}

	// Step 5: Fetch recommendations
	fmt.Println("  Fetching Kruize recommendations...")
	var results []kruizeRec

	for wk, expName := range experimentNames {
		endTimeStr := maxEndTime.UTC().Format("2006-01-02T15:04:05.000Z")
		reqURL := fmt.Sprintf("%s/updateRecommendations?experiment_name=%s&interval_end_time=%s",
			kruizeURL, expName, endTimeStr)

		req, _ := http.NewRequest("POST", reqURL, nil)
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("    WARNING: updateRecommendations failed for %s: %v\n", expName, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 400 {
			fmt.Printf("    Kruize returned 400 for %s: %s\n", expName, truncate(string(body), 200))
			continue
		}

		var listRecs []kruizePayload.ListRecommendations
		if err := json.Unmarshal(body, &listRecs); err != nil {
			fmt.Printf("    WARNING: unmarshal failed for %s: %v\n", expName, err)
			continue
		}

		for _, lr := range listRecs {
			for _, ko := range lr.Kubernetes_objects {
				for _, cont := range ko.Containers {
					recs := extractKruizeTermRecs(cont.Recommendations, wk.namespace, wk.workload, cont.Container_name)
					results = append(results, recs...)
				}
			}
		}
	}

	return results, nil
}

// buildKruizeContainerMetrics converts a single nise CSV row into the nested
// JSON structure that Kruize's /updateResults endpoint expects, mapping nise
// column names (e.g. cpu_usage_container_avg) to Kruize metric names
// (e.g. cpuUsage) with aggregation_info (sum, avg, min, max).
func buildKruizeContainerMetrics(row map[string]interface{}) map[string]any {
	s := func(key string) string {
		if v, ok := row[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	metricsMap := []struct {
		name                           string
		sumKey, avgKey, minKey, maxKey string
		format                         string
	}{
		{"cpuRequest", "cpu_request_container_sum", "cpu_request_container_avg", "", "", "cores"},
		{"cpuLimit", "cpu_limit_container_sum", "cpu_limit_container_avg", "", "", "cores"},
		{"cpuUsage", "cpu_usage_container_sum", "cpu_usage_container_avg", "cpu_usage_container_min", "cpu_usage_container_max", "cores"},
		{"cpuThrottle", "cpu_throttle_container_sum", "cpu_throttle_container_avg", "", "cpu_throttle_container_max", "cores"},
		{"memoryRequest", "memory_request_container_sum", "memory_request_container_avg", "", "", "bytes"},
		{"memoryLimit", "memory_limit_container_sum", "memory_limit_container_avg", "", "", "bytes"},
		{"memoryUsage", "memory_usage_container_sum", "memory_usage_container_avg", "memory_usage_container_min", "memory_usage_container_max", "bytes"},
		{"memoryRSS", "memory_rss_usage_container_sum", "memory_rss_usage_container_avg", "memory_rss_usage_container_min", "memory_rss_usage_container_max", "bytes"},
	}

	var metrics []map[string]any
	for _, m := range metricsMap {
		sumVal := s(m.sumKey)
		avgVal := s(m.avgKey)
		if sumVal == "" || avgVal == "" {
			continue
		}
		aggInfo := map[string]any{
			"sum":    sumVal,
			"avg":    avgVal,
			"format": m.format,
		}
		if m.minKey != "" {
			aggInfo["min"] = s(m.minKey)
		}
		if m.maxKey != "" {
			aggInfo["max"] = s(m.maxKey)
		}
		metrics = append(metrics, map[string]any{
			"name":    m.name,
			"results": map[string]any{"aggregation_info": aggInfo},
		})
	}

	return map[string]any{
		"container_image_name": s("image_name"),
		"container_name":       s("container_name"),
		"metrics":              metrics,
	}
}

func extractKruizeTermRecs(rec kruizePayload.Recommendation, ns, wl, container string) []kruizeRec {
	var results []kruizeRec

	for _, recData := range rec.Data {
		terms := map[string]kruizePayload.RecommendationTerm{
			"short_term":  recData.RecommendationTerms.Short_term,
			"medium_term": recData.RecommendationTerms.Medium_term,
			"long_term":   recData.RecommendationTerms.Long_term,
		}

		for termName, term := range terms {
			if term.RecommendationEngines == nil {
				continue
			}

			for _, engPair := range []struct {
				name string
				eng  kruizePayload.RecommendationEngineObject
			}{
				{"cost", term.RecommendationEngines.Cost},
				{"performance", term.RecommendationEngines.Performance},
			} {
				cfg := engPair.eng.Config
				kr := kruizeRec{
					Namespace: ns,
					Workload:  wl,
					Container: container,
					Term:      termName,
					Engine:    engPair.name,
					CPUReqMC:  coresToMillicores(cfg.Requests.Cpu.Amount),
					CPULimMC:  coresToMillicores(cfg.Limits.Cpu.Amount),
					MemReqKiB: bytesToKiB(cfg.Requests.Memory.Amount),
					MemLimKiB: bytesToKiB(cfg.Limits.Memory.Amount),
				}
				results = append(results, kr)
			}
		}
	}
	return results
}

func coresToMillicores(cores float64) int64 {
	return int64(math.Round(cores * 1000))
}

func bytesToKiB(b float64) int64 {
	if b == 0 {
		return 0
	}
	// Kruize may return MiB - check magnitude
	// If format is "MiB", multiply by 1024
	// Otherwise assume bytes
	return int64(math.Round(b / 1024))
}

type comparisonRow struct {
	ClusterID       string
	Namespace       string
	Workload        string
	Container       string
	Term            string
	Engine          string
	NativeCPUReqMC  int64
	NativeCPULimMC  int64
	NativeMemReqKiB int64
	NativeMemLimKiB int64
	KruizeCPUReqMC  int64
	KruizeCPULimMC  int64
	KruizeMemReqKiB int64
	KruizeMemLimKiB int64
	CPUReqDiffPct   string
	MemReqDiffPct   string
}

// writeComparison joins native and Kruize recommendations by (namespace, workload,
// container, term, engine), normalizing term names ("short_term" -> "short"), and
// writes a CSV with both engines' values and percentage differences.
func writeComparison(path string, nativeRecs []engine.ContainerRec, kruizeRecs []kruizeRec, clusterID string) error {
	type recKey struct {
		ns, wl, container, term, engine string
	}

	// Normalize term names: native uses "short"/"medium"/"long",
	// Kruize uses "short_term"/"medium_term"/"long_term"
	normTerm := func(t string) string {
		switch t {
		case "short", "short_term":
			return "short"
		case "medium", "medium_term":
			return "medium"
		case "long", "long_term":
			return "long"
		}
		return t
	}

	kruizeMap := map[recKey]*kruizeRec{}
	for i := range kruizeRecs {
		kr := &kruizeRecs[i]
		k := recKey{kr.Namespace, kr.Workload, kr.Container, normTerm(kr.Term), kr.Engine}
		kruizeMap[k] = kr
	}

	var rows []comparisonRow
	matched := map[recKey]bool{}
	for _, nr := range nativeRecs {
		nt := normTerm(nr.Term)
		cr := comparisonRow{
			ClusterID:       clusterID,
			Namespace:       nr.Namespace,
			Workload:        nr.Workload,
			Container:       nr.ContainerName,
			Term:            nt,
			Engine:          nr.Engine,
			NativeCPUReqMC:  nr.RecCPURequestMC,
			NativeCPULimMC:  nr.RecCPULimitMC,
			NativeMemReqKiB: nr.RecMemRequestKiB,
			NativeMemLimKiB: nr.RecMemLimitKiB,
		}

		k := recKey{nr.Namespace, nr.Workload, nr.ContainerName, nt, nr.Engine}
		if kr, ok := kruizeMap[k]; ok {
			cr.KruizeCPUReqMC = kr.CPUReqMC
			cr.KruizeCPULimMC = kr.CPULimMC
			cr.KruizeMemReqKiB = kr.MemReqKiB
			cr.KruizeMemLimKiB = kr.MemLimKiB
			cr.CPUReqDiffPct = diffPct(cr.NativeCPUReqMC, cr.KruizeCPUReqMC)
			cr.MemReqDiffPct = diffPct(cr.NativeMemReqKiB, cr.KruizeMemReqKiB)
			matched[k] = true
		} else {
			cr.CPUReqDiffPct = "N/A"
			cr.MemReqDiffPct = "N/A"
		}

		rows = append(rows, cr)
	}

	// Add kruize-only recs not matched to any native rec
	for _, kr := range kruizeRecs {
		k := recKey{kr.Namespace, kr.Workload, kr.Container, normTerm(kr.Term), kr.Engine}
		if !matched[k] {
			rows = append(rows, comparisonRow{
				ClusterID:       clusterID,
				Namespace:       kr.Namespace,
				Workload:        kr.Workload,
				Container:       kr.Container,
				Term:            normTerm(kr.Term),
				Engine:          kr.Engine,
				KruizeCPUReqMC:  kr.CPUReqMC,
				KruizeCPULimMC:  kr.CPULimMC,
				KruizeMemReqKiB: kr.MemReqKiB,
				KruizeMemLimKiB: kr.MemLimKiB,
				CPUReqDiffPct:   "N/A",
				MemReqDiffPct:   "N/A",
			})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	w.Write([]string{
		"cluster_id", "namespace", "workload", "container", "term", "engine",
		"native_cpu_request_mc", "native_cpu_limit_mc", "native_mem_request_kib", "native_mem_limit_kib",
		"kruize_cpu_request_mc", "kruize_cpu_limit_mc", "kruize_mem_request_kib", "kruize_mem_limit_kib",
		"cpu_request_diff_pct", "mem_request_diff_pct",
	})

	for _, r := range rows {
		w.Write([]string{
			r.ClusterID, r.Namespace, r.Workload, r.Container, r.Term, r.Engine,
			fmt.Sprintf("%d", r.NativeCPUReqMC), fmt.Sprintf("%d", r.NativeCPULimMC),
			fmt.Sprintf("%d", r.NativeMemReqKiB), fmt.Sprintf("%d", r.NativeMemLimKiB),
			fmt.Sprintf("%d", r.KruizeCPUReqMC), fmt.Sprintf("%d", r.KruizeCPULimMC),
			fmt.Sprintf("%d", r.KruizeMemReqKiB), fmt.Sprintf("%d", r.KruizeMemLimKiB),
			r.CPUReqDiffPct, r.MemReqDiffPct,
		})
	}
	w.Flush()
	return w.Error()
}

func diffPct(native, kruize int64) string {
	if kruize == 0 {
		if native == 0 {
			return "0.0%"
		}
		return "N/A"
	}
	pct := (float64(native) - float64(kruize)) / float64(kruize) * 100
	return fmt.Sprintf("%.1f%%", pct)
}

func printSummary(nativeRecs []engine.ContainerRec, kruizeRecs []kruizeRec) {
	fmt.Printf("  Native recommendations: %d\n", len(nativeRecs))
	fmt.Printf("  Kruize recommendations: %d\n", len(kruizeRecs))

	if len(nativeRecs) > 0 {
		fmt.Println("\n  Native engine results (first 5):")
		for i, r := range nativeRecs {
			if i >= 5 {
				break
			}
			fmt.Printf("    %s/%s/%s [%s/%s]: CPU req=%dmc lim=%dmc, Mem req=%dKiB lim=%dKiB\n",
				r.Namespace, r.Workload, r.ContainerName, r.Term, r.Engine,
				r.RecCPURequestMC, r.RecCPULimitMC, r.RecMemRequestKiB, r.RecMemLimitKiB)
		}
	}

	if len(kruizeRecs) > 0 {
		fmt.Println("\n  Kruize results (first 5):")
		for i, r := range kruizeRecs {
			if i >= 5 {
				break
			}
			fmt.Printf("    %s/%s/%s [%s/%s]: CPU req=%dmc lim=%dmc, Mem req=%dKiB lim=%dKiB\n",
				r.Namespace, r.Workload, r.Container, r.Term, r.Engine,
				r.CPUReqMC, r.CPULimMC, r.MemReqKiB, r.MemLimKiB)
		}
	}
}

func parseNiseTimestamp(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse timestamp %q", s)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func init() {
	// Ensure we run from the repo root so migrations/ and resource_optimization_openshift.json are found
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		if _, err := os.Stat("../../migrations"); err == nil {
			os.Chdir("../..")
		}
	}
}
