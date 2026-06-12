package dashboard_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const dashboardConfigMapPath = "../../dashboards/grafana-dashboard-insights-rosocp-general.configmap.yaml"

var (
	metricNameFromSourceRE = regexp.MustCompile(`Name:\s*"(ros[a-z0-9_]*|kruize_[a-z0-9_]*)"`)
	metricTokenRE          = regexp.MustCompile(`[a-zA-Z_:][a-zA-Z0-9_:]*`)
)

var externalMetrics = map[string]bool{
	"aws_kafka_sum_offset_lag_sum":                      true,
	"aws_rds_free_storage_space_average":                true,
	"kube_pod_container_status_last_terminated_reason":  true,
	"kube_pod_container_status_restarts_total":          true,
	"container_memory_working_set_bytes":                true,
	"rosocp_request_duration_seconds":                   true,
	"rosocp_requests_total":                             true,
	"rosocp_response_size_bytes":                        true,
	"rosocp_request_size_bytes":                         true,
}

var promqlKeywords = map[string]bool{
	"sum": true, "rate": true, "increase": true, "histogram_quantile": true, "clamp_min": true,
	"by": true, "le": true, "without": true, "on": true, "group_left": true, "group_right": true,
	"avg": true, "min": true, "max": true, "count": true, "stddev": true, "topk": true, "bottomk": true,
	"quantile": true, "absent": true, "changes": true, "resets": true, "irate": true, "delta": true,
	"idelta": true, "deriv": true, "predict_linear": true, "time": true, "vector": true,
	"label_replace": true, "label_join": true, "sort": true, "sort_desc": true, "floor": true,
	"ceil": true, "round": true, "abs": true, "exp": true, "ln": true, "log2": true, "log10": true,
	"sqrt": true, "sgn": true, "timestamp": true, "bool": true, "and": true, "or": true, "unless": true,
	"offset": true, "__auto": true, "__range": true, "__rate_interval": true,
}

var promqlLabelNames = map[string]bool{
	"pod": true, "container": true, "namespace": true, "reason": true, "job": true, "code": true,
	"stage": true, "operation": true, "type": true, "phase": true, "endpoint": true, "sa_name": true,
	"status": true, "recommendation_type": true, "report_type": true, "error_class": true,
	"error_type": true, "model_name": true, "topic": true, "consumer_group": true,
	"dbinstance_identifier": true, "method": true, "host": true, "path": true, "handler": true,
}

type dashboardData struct {
	configMapYAML map[string]interface{}
	dashboardJSON map[string]interface{}
}

func loadDashboard(t *testing.T) dashboardData {
	t.Helper()

	raw, err := os.ReadFile(dashboardConfigMapPath)
	if err != nil {
		t.Fatalf("read dashboard ConfigMap: %v", err)
	}

	var configMap map[string]interface{}
	if err := yaml.Unmarshal(raw, &configMap); err != nil {
		t.Fatalf("parse dashboard ConfigMap YAML: %v", err)
	}

	dataSection, ok := configMap["data"].(map[string]interface{})
	if !ok {
		t.Fatal("ConfigMap missing data section")
	}

	rosocpJSON, ok := dataSection["ROSOCP.json"].(string)
	if !ok || rosocpJSON == "" {
		t.Fatal("ConfigMap data missing ROSOCP.json")
	}

	var dashboard map[string]interface{}
	if err := json.Unmarshal([]byte(rosocpJSON), &dashboard); err != nil {
		t.Fatalf("parse ROSOCP.json: %v", err)
	}

	return dashboardData{
		configMapYAML: configMap,
		dashboardJSON: dashboard,
	}
}

func extractPanels(root map[string]interface{}) []map[string]interface{} {
	var panels []map[string]interface{}

	topLevel, ok := root["panels"].([]interface{})
	if !ok {
		return panels
	}

	var walk func([]interface{})
	walk = func(items []interface{}) {
		for _, item := range items {
			panel, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			panels = append(panels, panel)

			if nested, ok := panel["panels"].([]interface{}); ok {
				walk(nested)
			}
		}
	}

	walk(topLevel)
	return panels
}

func panelDatasourceUID(panel map[string]interface{}) (string, bool) {
	ds, ok := panel["datasource"]
	if !ok {
		return "", false
	}

	switch typed := ds.(type) {
	case map[string]interface{}:
		uid, ok := typed["uid"].(string)
		return uid, ok
	case string:
		return typed, true
	default:
		return "", false
	}
}

func targetDatasourceUID(target map[string]interface{}) (string, bool) {
	ds, ok := target["datasource"]
	if !ok {
		return "", false
	}

	switch typed := ds.(type) {
	case map[string]interface{}:
		uid, ok := typed["uid"].(string)
		return uid, ok
	case string:
		return typed, true
	default:
		return "", false
	}
}

func isAllowedDatasourceUID(uid string) bool {
	if uid == "grafana" {
		return true
	}
	return strings.HasPrefix(uid, "$") || strings.HasPrefix(uid, "${")
}

func scanMetricsFromCodebase(t *testing.T) map[string]bool {
	t.Helper()

	internalDir := filepath.Clean("..")
	known := make(map[string]bool)
	for name := range externalMetrics {
		known[name] = true
	}

	err := filepath.Walk(internalDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		for _, match := range metricNameFromSourceRE.FindAllStringSubmatch(string(content), -1) {
			known[match[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan internal metrics: %v", err)
	}

	return known
}

func metricLookupNames(name string) []string {
	candidates := []string{name}
	for _, suffix := range []string{"_bucket", "_sum", "_count"} {
		if strings.HasSuffix(name, suffix) {
			candidates = append(candidates, strings.TrimSuffix(name, suffix))
		}
	}
	return candidates
}

func metricExists(name string, known map[string]bool) bool {
	for _, candidate := range metricLookupNames(name) {
		if known[candidate] {
			return true
		}
	}
	return false
}

func extractMetricsFromExpr(expr string) []string {
	seen := make(map[string]bool)
	var metrics []string

	for _, token := range metricTokenRE.FindAllString(expr, -1) {
		if promqlKeywords[token] || promqlLabelNames[token] {
			continue
		}
		if !strings.Contains(token, "_") || len(token) < 8 {
			continue
		}
		if strings.HasPrefix(token, "$") || strings.HasPrefix(token, "__") {
			continue
		}
		if seen[token] {
			continue
		}
		seen[token] = true
		metrics = append(metrics, token)
	}

	return metrics
}

func TestDashboard_YAMLAndJSONParse(t *testing.T) {
	data := loadDashboard(t)
	if data.configMapYAML == nil || data.dashboardJSON == nil {
		t.Fatal("expected parsed dashboard data")
	}
}

func TestDashboard_PanelIDsUnique(t *testing.T) {
	data := loadDashboard(t)
	panels := extractPanels(data.dashboardJSON)

	seen := make(map[float64]int)
	for _, panel := range panels {
		id, ok := panel["id"].(float64)
		if !ok {
			continue
		}
		seen[id]++
	}

	var duplicates []string
	for id, count := range seen {
		if count > 1 {
			duplicates = append(duplicates, fmt.Sprintf("%v (%d times)", id, count))
		}
	}
	sort.Strings(duplicates)

	if len(duplicates) > 0 {
		t.Fatalf("duplicate panel IDs found: %s", strings.Join(duplicates, ", "))
	}
}

func TestDashboard_NoDatasourceHardcoded(t *testing.T) {
	data := loadDashboard(t)
	panels := extractPanels(data.dashboardJSON)

	var violations []string
	for _, panel := range panels {
		if uid, ok := panelDatasourceUID(panel); ok && !isAllowedDatasourceUID(uid) {
			violations = append(violations, fmt.Sprintf("panel id=%v datasource.uid=%q", panel["id"], uid))
		}

		targets, ok := panel["targets"].([]interface{})
		if !ok {
			continue
		}
		for _, targetItem := range targets {
			target, ok := targetItem.(map[string]interface{})
			if !ok {
				continue
			}
			if uid, ok := targetDatasourceUID(target); ok && !isAllowedDatasourceUID(uid) {
				violations = append(violations, fmt.Sprintf("panel id=%v target datasource.uid=%q", panel["id"], uid))
			}
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("hardcoded datasource UIDs found:\n%s", strings.Join(violations, "\n"))
	}
}

func TestDashboard_NoEmptyExpressions(t *testing.T) {
	data := loadDashboard(t)
	panels := extractPanels(data.dashboardJSON)

	var violations []string
	for _, panel := range panels {
		targets, ok := panel["targets"].([]interface{})
		if !ok {
			continue
		}
		for idx, targetItem := range targets {
			target, ok := targetItem.(map[string]interface{})
			if !ok {
				continue
			}
			expr, hasExpr := target["expr"]
			if !hasExpr {
				continue
			}
			exprStr, ok := expr.(string)
			if !ok || strings.TrimSpace(exprStr) == "" {
				violations = append(violations, fmt.Sprintf("panel id=%v target[%d] has empty expr", panel["id"], idx))
			}
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("empty PromQL expressions found:\n%s", strings.Join(violations, "\n"))
	}
}

func TestDashboard_MetricNamesExistInCodebase(t *testing.T) {
	data := loadDashboard(t)
	panels := extractPanels(data.dashboardJSON)
	knownMetrics := scanMetricsFromCodebase(t)

	unknown := make(map[string]bool)
	for _, panel := range panels {
		targets, ok := panel["targets"].([]interface{})
		if !ok {
			continue
		}
		for _, targetItem := range targets {
			target, ok := targetItem.(map[string]interface{})
			if !ok {
				continue
			}
			expr, ok := target["expr"].(string)
			if !ok || strings.TrimSpace(expr) == "" {
				continue
			}

			for _, metric := range extractMetricsFromExpr(expr) {
				if !metricExists(metric, knownMetrics) {
					unknown[metric] = true
				}
			}
		}
	}

	if len(unknown) > 0 {
		names := make([]string, 0, len(unknown))
		for name := range unknown {
			names = append(names, name)
		}
		sort.Strings(names)
		t.Fatalf("dashboard references unknown metrics:\n%s", strings.Join(names, "\n"))
	}
}

func TestDashboard_AllRowsHaveTitle(t *testing.T) {
	data := loadDashboard(t)
	panels := extractPanels(data.dashboardJSON)

	var violations []string
	for _, panel := range panels {
		if panel["type"] != "row" {
			continue
		}
		title, _ := panel["title"].(string)
		if strings.TrimSpace(title) == "" {
			violations = append(violations, fmt.Sprintf("row panel id=%v missing title", panel["id"]))
		}
	}

	if len(violations) > 0 {
		sort.Strings(violations)
		t.Fatalf("row panels without titles found:\n%s", strings.Join(violations, "\n"))
	}
}
