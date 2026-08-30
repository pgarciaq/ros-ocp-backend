package ingestion

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// OperatorRosClusterQuotaCSVHeader is the exact header from
// koku-metrics-operator/internal/collector/types.go (rosClusterQuotaRow).csvHeader().
// Container ROS header contract lives in librobne/csv (csv_contract_test.go).
var OperatorRosClusterQuotaCSVHeader = []string{
	"report_period_start",
	"report_period_end",
	"interval_start",
	"interval_end",
	"cluster_quota_name",
	"cpu_request_hard",
	"cpu_request_used",
	"cpu_limit_hard",
	"cpu_limit_used",
	"memory_request_hard",
	"memory_request_used",
	"memory_limit_hard",
	"memory_limit_used",
	"storage_request_hard",
	"storage_request_used",
	"pods_hard",
	"pods_used",
	"object_count_hard",
	"object_count_used",
	"namespaces",
}

func TestCSVContract_OperatorClusterQuotaHeaderParseable(t *testing.T) {
	t.Parallel()

	csvBody := strings.Join(OperatorRosClusterQuotaCSVHeader, ",") + "\n"
	rows, err := ParseClusterQuotaCSVRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	assert.Empty(t, rows)
}
