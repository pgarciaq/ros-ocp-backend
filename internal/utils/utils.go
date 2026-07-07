package utils

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	rosdb "github.com/redhatinsights/ros-ocp-backend/internal/db"
	rosdebug "github.com/redhatinsights/ros-ocp-backend/internal/debug"
	"github.com/redhatinsights/ros-ocp-backend/internal/health"
	"github.com/redhatinsights/ros-ocp-backend/internal/httpclient"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/sirupsen/logrus"
)

var log *logrus.Entry = logging.GetLogger()
var cfg *config.Config = config.GetConfig()

var csvDownloadTransport = httpclient.SharedTransport()

var csvDownloadHTTPClientSingleton *http.Client

func csvMaxBodyBytes() int64 {
	n := config.GetConfig().CSVMaxBodyBytes
	if n <= 0 {
		return 104857600 // 100 MiB
	}
	return n
}

func csvDownloadHTTPClient() *http.Client {
	if csvDownloadHTTPClientSingleton != nil {
		return csvDownloadHTTPClientSingleton
	}
	timeoutSecs := config.GetConfig().CSVDownloadTimeoutSecs
	if timeoutSecs <= 0 {
		timeoutSecs = 120
	}
	csvDownloadHTTPClientSingleton = &http.Client{
		Timeout:   time.Duration(timeoutSecs) * time.Second,
		Transport: csvDownloadTransport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("CSV download redirects are disabled")
		},
	}
	return csvDownloadHTTPClientSingleton
}

func getCSVHTTPResponse(rawURL string) (*http.Response, error) {
	u, err := validateCSVDownloadURL(rawURL)
	if err != nil {
		return nil, err
	}
	resp, err := csvDownloadHTTPClient().Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("fetch CSV: %w", err)
	}
	return resp, nil
}

// HTTPClient is the shared HTTP client for lightweight outbound requests
// (health checks, RBAC, experiment creation). The timeout is driven by
// GLOBAL_HTTP_CLIENT_TIMEOUT_SECS (default 30s) to prevent indefinite
// hangs when downstream services are slow or unresponsive. See FLPATH-3407.
//
// Heavy Kruize calls (/updateResults, /updateRecommendations) should continue to use HTTPClient.
// ReadCSVFromUrl / ReadCSVBodyFromUrl use a dedicated bounded client (see csvDownloadHTTPClient).
// TODO(FLPATH-3407): add per-endpoint Prometheus histogram to measure
// Kruize API latency, then set per-call timeouts:
//
//	kruizeAPIDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
//	    Name:    "rosocp_kruize_api_duration_seconds",
//	    Help:    "Latency of outbound Kruize API calls in seconds",
//	    Buckets: []float64{0.5, 1, 5, 10, 30, 60, 120, 300},
//	}, []string{"path"})
var HTTPClient = newHTTPClient(cfg.GlobalHTTPClientTimeoutSecs)

const minHTTPTimeoutSecs = 1

func newHTTPClient(timeoutSecs int) *http.Client {
	if timeoutSecs < minHTTPTimeoutSecs {
		log.Warnf("GLOBAL_HTTP_CLIENT_TIMEOUT_SECS=%d is below minimum; using %ds", timeoutSecs, minHTTPTimeoutSecs)
		timeoutSecs = minHTTPTimeoutSecs
	}
	return httpclient.NewClient(time.Duration(timeoutSecs) * time.Second)
}

func Setup_kruize_performance_profile() {
	// This func needs to be revisited once kruize implements this API
	// Refer - https://github.com/kruize/autotune/blob/mvp_demo/src/main/java/com/autotune/analyzer/Analyzer.java#L50
	list_performance_profile_url := cfg.KruizeUrl + "/listPerformanceProfiles"
	for i := 0; i < 5; i++ {
		log.Infof("Fetching performance profile list")
		response, err := HTTPClient.Get(list_performance_profile_url)
		if err != nil {
			log.Errorf("An Error Occured %v \n", err)
		} else {
			defer func() {
				_ = response.Body.Close()
			}()
			create_performance_profile_url := cfg.KruizeUrl + "/createPerformanceProfile"
			postBody, err := os.ReadFile("./resource_optimization_openshift.json")
			if err != nil {
				log.Fatalf("Kruize performance profile file reading error: %v", err)
			}
			res, e := HTTPClient.Post(create_performance_profile_url, "application/json", bytes.NewBuffer(postBody))
			if e != nil {
				log.Errorf("unable to create performance profile in kruize: %v \n", e)
			}
			defer func() {
				_ = res.Body.Close()
			}()
			if res.StatusCode == 201 {
				log.Infof("Performance profile created successfully")
				return
			}
			if res.StatusCode == 409 {
				log.Infof("Performance Profile already exist")
				return
			}
			bodyBytes, _ := io.ReadAll(res.Body)
			data := map[string]interface{}{}
			if err := json.Unmarshal(bodyBytes, &data); err != nil {
				log.Fatalf("Kruize: cannot unmarshal performance profile response: %v", err)
			}
		}
		log.Infof("sleeping for 10 Seconds")
		time.Sleep(10 * time.Second)
	}

}

// ReadCSVBodyFromUrl fetches a CSV URL and returns the response body as an
// io.ReadCloser wrapped with http.MaxBytesReader (limit ROS_CSV_MAX_BODY_BYTES).
// Data is not buffered entirely here—callers typically stream via csv.Reader—
// but each row still allocates; the byte cap limits download size only.
func ReadCSVBodyFromUrl(rawURL string) (io.ReadCloser, error) {
	resp, err := getCSVHTTPResponse(rawURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code %d when fetching CSV from %s", resp.StatusCode, rawURL)
	}
	return http.MaxBytesReader(nil, resp.Body, csvMaxBodyBytes()), nil
}

// ReadCSVFromUrl fetches a CSV URL and parses the entire file into [][]string via
// csv.Reader.ReadAll—peak memory is proportional to file size (plus CSV parsing overhead).
// Legacy Kruize paths often copy again into a dataframe. Prefer native ingestion with
// ReadCSVBodyFromUrl when possible; the download is still capped by MaxBytesReader.
func ReadCSVFromUrl(rawURL string) ([][]string, error) {
	resp, err := getCSVHTTPResponse(rawURL)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected status code %d when fetching CSV from %s", resp.StatusCode, rawURL)
	}

	reader := csv.NewReader(http.MaxBytesReader(nil, resp.Body, csvMaxBodyBytes()))
	data, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV: %w", err)
	}

	return data, nil
}

type uniqueTypes interface {
	int | float64 | string
}

func unique[T uniqueTypes](x []T) []T {
	keys := make(map[T]bool, len(x))
	list := make([]T, 0, len(x))
	for _, entry := range x {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func ConvertDateToISO8601(date string) (string, error) {
	const date_format = "2006-01-02 15:04:05 -0700 MST"
	t, err := time.Parse(date_format, date)
	if err != nil {
		return "", fmt.Errorf("ConvertDateToISO8601: unable to parse %q: %w", date, err)
	}
	return t.Format("2006-01-02T15:04:05.000Z"), nil
}

func ConvertStringToTime(data string) (time.Time, error) {
	dateTime, err := time.Parse("2006-01-02 15:04:05 -0700 MST", data)
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to convert string to time: %s", err)
	}
	return dateTime, nil

}

func ConvertISO8601StringToTime(data string) (time.Time, error) {
	dateTime, err := time.Parse("2006-01-02T15:04:05.000Z", data)
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to convert string to time: %s", err)
	}
	return dateTime, nil
}

func MaxIntervalEndTime(slice []string) (time.Time, error) {
	var converted_date_slice []time.Time
	for _, v := range slice {
		formated_date, err := ConvertStringToTime(v)
		if err != nil {
			return time.Time{}, fmt.Errorf("unable to convert string to time in a slice: %s", err)
		}
		converted_date_slice = append(converted_date_slice, formated_date)

	}
	var max time.Time
	max = converted_date_slice[0]
	for _, ele := range converted_date_slice {
		if max.Before(ele) {
			max = ele
		}
	}
	return max, nil
}

func findInStringSlice(str string, s []string) int {
	for i, e := range s {
		if e == str {
			return i
		}
	}
	return -1
}

func GenerateExperimentName(org_id, source_id, cluster_id, namespace, k8s_object_type, k8s_object_name string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s", org_id, source_id, cluster_id, namespace, k8s_object_type, k8s_object_name)

}

func GenerateNamespaceExperimentName(org_id, source_id, cluster_id, namespace string) string {
	return fmt.Sprintf("%s|%s|%s|namespace|%s", org_id, source_id, cluster_id, namespace)
}

func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func Start_prometheus_server() error {
	if cfg.PrometheusPort == "" {
		return nil
	}
	log.Info("Starting prometheus http server")
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	if cfg.EnablePprof {
		log.Warn("pprof endpoints enabled on prometheus port — do NOT use in production")
		rosdebug.RegisterMuxPprof(mux)
	}
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		pool := rosdb.GetPool()
		result := health.RunReadyzChecks(ctx, pool)
		if !result.OK {
			for name, status := range result.Checks {
				if status != "ok" {
					log.Warnf("readyz: %s check failed: %s", name, status)
				}
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			body, _ := json.Marshal(map[string]interface{}{"status": "error", "checks": result.Checks})
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusOK)
		body, _ := json.Marshal(map[string]interface{}{"status": "ok", "checks": result.Checks})
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		result := health.RunHealthzChecks(ctx)
		if !result.OK {
			w.WriteHeader(http.StatusServiceUnavailable)
			body, _ := json.Marshal(result)
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusOK)
		body, _ := json.Marshal(result)
		_, _ = w.Write(body)
	})
	if err := http.ListenAndServe(fmt.Sprintf(":%s", cfg.PrometheusPort), mux); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("ListenAndServe prometheus: %w", err)
	}
	return nil
}

func NeedRecommOnFirstOfMonth(dbDate time.Time, maxEndTime time.Time) bool {
	if isItFirstOfMonth(maxEndTime) && getDate(maxEndTime).After(getDate(dbDate)) {
		return true
	}
	return false
}

func getDate(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
}

func isItFirstOfMonth(d time.Time) bool {
	_, _, day := d.Date()
	return day == 1
}

func DetermineCSVType(fileName string) types.PayloadType {
	base := filepath.Base(fileName)

	type rule struct {
		pattern string
		ptype   types.PayloadType
	}
	// Ordered longest-match-first to avoid false positives.
	rules := []rule{
		{"ros-openshift-cluster-quota-", types.PayloadTypeClusterQuota},
		{"ros-openshift-namespace-", types.PayloadTypeNamespace},
		{"ros-openshift-vm-gpu-device-", types.PayloadTypeVMGPU},
		{"ros-openshift-vm-usage-", types.PayloadTypeVM},
		{"ocp_ros_vm_gpu_device", types.PayloadTypeVMGPU},
		{"ros-openshift-snapshot-", types.PayloadTypeSnapshot},
		{"ros-openshift-storage-", types.PayloadTypeStorage},
		{"ocp_ros_cluster_quota", types.PayloadTypeClusterQuota},
		{"ocp_ros_namespace", types.PayloadTypeNamespace},
		{"ocp_ros_vm_usage", types.PayloadTypeVM},
		{"ocp_snapshot_inventory", types.PayloadTypeSnapshot},
		{"ocp_storage_usage", types.PayloadTypeStorage},
	}
	// Prefix match first (operator-generated filenames).
	for _, r := range rules {
		if strings.HasPrefix(base, r.pattern) {
			return r.ptype
		}
	}
	// Contains fallback for nise-generated filenames with date/UUID prefixes
	// (e.g. "May-2026-UUID-ocp_ros_cluster_quota.csv").
	for _, r := range rules {
		if strings.Contains(base, r.pattern) {
			return r.ptype
		}
	}

	// Operator cost-pipeline files forwarded to ROS via Koku ROS_EXTRA_PATTERNS.
	// Must be checked before the blanket cm-openshift-* rejection below.
	cmRosForwarded := []rule{
		{"cm-openshift-snapshot-inventory", types.PayloadTypeSnapshot},
		{"cm-openshift-storage-usage", types.PayloadTypeStorage},
	}
	for _, r := range cmRosForwarded {
		if strings.Contains(base, r.pattern) {
			return r.ptype
		}
	}

	// Reject remaining cost management CSVs (cm-openshift-*) AFTER matching all ROS
	// patterns. This ordering is security-relevant: in restricted network mode
	// (docs.redhat.com/cost_management_service, "restricted network") users
	// manually handle tarballs, so filenames are attacker-controllable.
	// Matching ROS patterns first prevents a crafted filename from causing
	// legitimate ROS data to be silently dropped.
	// Uses Contains because operator filenames carry a UUID prefix
	// (e.g. "d684644b-...-cm-openshift-pod-usage-202606.3.csv").
	if strings.Contains(base, "cm-openshift-") {
		return types.PayloadTypeUnknown
	}

	return types.PayloadTypeContainer
}
