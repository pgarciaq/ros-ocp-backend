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

func SetupKruizePerformanceProfile() {
	// This func needs to be revisited once kruize implements this API
	// Refer - https://github.com/kruize/autotune/blob/mvp_demo/src/main/java/com/autotune/analyzer/Analyzer.java#L50
	listPerformanceProfileUrl := cfg.KruizeUrl + "/listPerformanceProfiles"
	// Use the target version from config and strip 'v' prefix if present
	targetVersion := strings.TrimPrefix(cfg.KruizePerformanceProfileVersion, "v")

	for i := 0; i < 10; i++ {
		log.Infof("fetching performance profile list")
		response, err := HTTPClient.Get(listPerformanceProfileUrl)
		if err != nil {
			log.Errorf("an error occurred %v \n", err)
		} else {
			body, err := io.ReadAll(response.Body)
			if respBodyErr := response.Body.Close(); respBodyErr != nil {
				log.Errorf("error closing response body: %v", respBodyErr)
			}
			if err != nil {
				log.Errorf("error reading listPerformanceProfiles response: %v", err)
				time.Sleep(10 * time.Second)
				continue
			}

			if len(body) > 0 {
				var profiles []map[string]interface{}
				if err := json.Unmarshal(body, &profiles); err != nil {
					log.Errorf("error unmarshalling listPerformanceProfiles response: %v", err)
					continue
				} else if len(profiles) > 0 {
					var fetchedVersion string
					for _, profile := range profiles {
						log.Debugf("current performance profile version : %v", profile["profile_version"])
						fetchedVersion = fmt.Sprintf("%.1f", profile["profile_version"])
					}

					// Convert versions to float64 for comparison
					fetchedVersionFloat, fetchedErr := strconv.ParseFloat(fetchedVersion, 64)
					targetVersionFloat, targetErr := strconv.ParseFloat(targetVersion, 64)

					if fetchedErr != nil || targetErr != nil {
						log.Errorf("failed to parse version numbers for comparison (fetched: %v, target: %v)", fetchedVersion, targetVersion)
						return
					}

					// Check if already up to date
					if fetchedVersionFloat == targetVersionFloat {
						log.Infof("performance profile already up to date (version: %v)", fetchedVersion)
						return
					}

					// Version mismatch -> Update the profile if update flag is enabled
					// and the fetched version is less than the target version (prevent downgrades)
					if cfg.UpdateKruizePerfProfile && fetchedVersionFloat < targetVersionFloat {
						log.Infof("updating performance profile to supported version: %v", targetVersion)
						postBody, err := os.ReadFile("./resource_optimization_openshift.json")
						if err != nil {
							log.Errorf("file reading error: %v \n", err)
							return
						}

						// create the PUT request
						updatePerformanceProfileUrl := cfg.KruizeUrl + "/updatePerformanceProfile"
						req, err := http.NewRequest(http.MethodPut, updatePerformanceProfileUrl, bytes.NewReader(postBody))
						if err != nil {
							log.Errorf("failed to create PUT request: %v", err)
							return
						}
						req.Header.Set("Content-Type", "application/json")

						// call the updatePerformanceProfile API using PUT request
						log.Debugf("sending PUT request to: %s (len=%d bytes)", updatePerformanceProfileUrl, req.ContentLength)
						res, err := HTTPClient.Do(req)
						if err != nil {
							log.Errorf("PUT request failed: %v", err)
							return
						}
						defer func() {
							if respBodyErr := res.Body.Close(); respBodyErr != nil {
								log.Errorf("error closing response body: %v", respBodyErr)
							}
						}()

						bodyBytes, _ := io.ReadAll(res.Body)
						log.Debugf("response status: %d", res.StatusCode)
						log.Debugf("response body: %s", string(bodyBytes))

						if res.StatusCode == 200 {
							log.Infof("performance profile updated successfully from %v to %v", fetchedVersion, targetVersion)
							return
						}
						log.Errorf("failed to update performance profile (status=%d): %s", res.StatusCode, targetVersion)
						return
					}
					log.Infof("performance profile version mismatch (fetched: %v, target: %v), update and create not applicable", fetchedVersion, targetVersion)
					return
				}
			}

			// If profile list empty or not found -> create new profile
			createPerformanceProfileUrl := cfg.KruizeUrl + "/createPerformanceProfile"
			log.Infof("creating new performance profile...")
			postBody, err := os.ReadFile("./resource_optimization_openshift.json")
			if err != nil {
				log.Fatalf("Kruize performance profile file reading error: %v", err)
			}
			res, e := HTTPClient.Post(createPerformanceProfileUrl, "application/json", bytes.NewBuffer(postBody))
			if e != nil {
				log.Errorf("unable to create performance profile in kruize: %v \n", e)
				continue
			}
			defer func() {
				_ = res.Body.Close()
			}()
			if res.StatusCode == 201 {
				log.Infof("performance profile created successfully")
				return
			}
			if res.StatusCode == 409 {
				log.Infof("performance profile already exist")
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
	return types.PayloadTypeContainer
}
