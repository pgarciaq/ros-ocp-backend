package utils

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	rosdb "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
	"github.com/sirupsen/logrus"
)

var log *logrus.Entry = logging.GetLogger()
var cfg *config.Config = config.GetConfig()

const (
	envCSVDownloadTimeoutSecs = "ROS_CSV_DOWNLOAD_TIMEOUT_SECONDS"
	envCSVMaxBodyBytes        = "ROS_CSV_MAX_BODY_BYTES"
	envCSVAllowedHosts        = "ROS_CSV_ALLOWED_HOSTS"
	// Default max download size for Kafka-triggered CSV URLs. Native ingestion streams
	// from this bounded reader (See ReadCSVBodyFromUrl); legacy ReadCSVFromUrl still
	// loads the full parsed [][]string into memory and often duplicates into a dataframe,
	// so this cap is the main OOM defense for hostile or oversized payloads.
	// 512 MiB fits large daily cluster exports while bounding worst-case RSS.
	defaultCSVMaxBodyBytes = 512 * 1024 * 1024
)

func csvMaxBodyBytes() int64 {
	v := strings.TrimSpace(os.Getenv(envCSVMaxBodyBytes))
	if v == "" {
		return defaultCSVMaxBodyBytes
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return defaultCSVMaxBodyBytes
	}
	return n
}

func csvDownloadHTTPClient() *http.Client {
	timeoutSecs := 60
	if v := strings.TrimSpace(os.Getenv(envCSVDownloadTimeoutSecs)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			timeoutSecs = n
		}
	}
	tr := &http.Transport{
		DisableKeepAlives: true,
	}
	return &http.Client{
		Timeout:   time.Duration(timeoutSecs) * time.Second,
		Transport: tr,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("CSV download redirects are disabled")
		},
	}
}

func validateCSVDownloadURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse CSV URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("CSV URL scheme must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("CSV URL must include a host")
	}
	if allowed := strings.TrimSpace(os.Getenv(envCSVAllowedHosts)); allowed != "" {
		ok := false
		for _, h := range strings.Split(allowed, ",") {
			if strings.EqualFold(strings.TrimSpace(h), host) {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("CSV URL host %q is not allowed by ROS_CSV_ALLOWED_HOSTS", host)
		}
	}
	return u, nil
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
	return &http.Client{Timeout: time.Duration(timeoutSecs) * time.Second}
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
				log.Errorf("File reading error: %v \n", err)
				os.Exit(1)
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
				log.Errorf("can not unmarshal response data: %v", err)
				os.Exit(1)
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
	keys := make(map[T]bool)
	list := []T{}
	for _, entry := range x {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func Convert2DarrayToMap(arr [][]string) []map[string]interface{} {
	data := []map[string]interface{}{}
	for i := 1; i < len(arr); i++ {
		m := make(map[string]interface{})
		for j := 0; j < len(arr[0]); j++ {
			if metric, err := strconv.ParseFloat(arr[i][j], 64); err == nil {
				m[arr[0][j]] = metric
			} else {
				m[arr[0][j]] = arr[i][j]
			}
		}
		data = append(data, m)
	}
	return data
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

func Start_prometheus_server() {
	if cfg.PrometheusPort == "" {
		return
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
		if pool == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"error","checks":{"database":"pool_uninitialized"}}`))
			return
		}
		if err := pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"error","checks":{"database":%q}}`, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","checks":{"database":"ok"}}`))
	})
	_ = http.ListenAndServe(fmt.Sprintf(":%s", cfg.PrometheusPort), mux)
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
	if strings.Contains(fileName, "namespace") {
		return types.PayloadTypeNamespace
	}
	if strings.Contains(fileName, "snapshot") {
		return types.PayloadTypeSnapshot
	}
	if strings.Contains(fileName, "storage") {
		return types.PayloadTypeStorage
	}
	return types.PayloadTypeContainer
}
